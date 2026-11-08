package learning

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/andrewmarous/trailmix/backup"
	"github.com/andrewmarous/trailmix/vault"
)

func validateChanges(changes []Change) error {
	seen := map[string]bool{}
	for _, change := range changes {
		name := change.Path
		if !strings.HasSuffix(name, ".md") || strings.ContainsAny(name, "\\:\x00") || filepath.IsAbs(name) || filepath.ToSlash(filepath.Clean(name)) != name {
			return fmt.Errorf("unsafe guidance path %q", name)
		}
		for _, part := range strings.Split(name, "/") {
			if part == "" || strings.HasPrefix(part, ".") {
				return fmt.Errorf("unsafe guidance path %q", name)
			}
		}
		if seen[name] {
			return fmt.Errorf("duplicate guidance path %q", name)
		}
		seen[name] = true
		if !utf8.ValidString(change.Content) || !strings.HasPrefix(strings.TrimSpace(change.Content), "# ") || len(change.Content) > 64*1024 {
			return fmt.Errorf("guidance needs a Markdown title and at most 64 KiB: %s", name)
		}
	}
	return nil
}

// AddGuidance publishes explicitly requested guidance without a model or proposal.
func AddGuidance(root, name, content string, overwrite bool) error {
	if filepath.Ext(name) == "" {
		name += ".md"
	}
	changes := []Change{{Path: name, Content: content}}
	if err := validateChanges(changes); err != nil {
		return err
	}
	v, err := vault.Open(root)
	if err != nil {
		return err
	}
	expected := ""
	body, err := v.Read(name)
	if err == nil {
		if !overwrite {
			return fmt.Errorf("guidance already exists; use --overwrite")
		}
		expected = fmt.Sprintf("%x", sha256.Sum256(body))
	} else if !os.IsNotExist(err) {
		return err
	}
	unlock, err := backup.Lock(v.Root)
	if err != nil {
		return err
	}
	defer unlock()
	return publishGuidance(v.Root, changes, map[string]string{name: expected})
}

// publishGuidance requires validated changes and the vault operation lock.
// Each note also checks its baseline under its own lock before publication.
func publishGuidance(root string, changes []Change, expected map[string]string) error {
	if len(changes) == 0 {
		return nil
	}
	notes, err := collectNotes(root, 16*1024*1024)
	if err != nil {
		return err
	}
	stage, err := os.MkdirTemp("", "tmix-guidance-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	staged, _ := vault.Open(stage)
	real, _ := vault.Open(root)
	for path, body := range notes {
		if err = staged.Write(path, []byte(body), false); err != nil {
			return err
		}
	}
	for _, change := range changes {
		baseline, ok := expected[change.Path]
		if !ok {
			return fmt.Errorf("guidance lacks a baseline")
		}
		actual := ""
		body, exists := notes[change.Path]
		if exists {
			actual = fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
		}
		if actual != baseline && (!exists || body != change.Content) {
			return fmt.Errorf("note changed since review: %s", change.Path)
		}
		if err = staged.Write(change.Path, []byte(change.Content), true); err != nil {
			return err
		}
	}
	// Link resolution needs attachment names, not their bodies.
	if err = filepath.WalkDir(root, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if path != root && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || d.Type()&os.ModeSymlink != 0 || strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		target := filepath.Join(stage, rel)
		if e = os.MkdirAll(filepath.Dir(target), 0700); e != nil {
			return e
		}
		return os.WriteFile(target, nil, 0600)
	}); err != nil {
		return err
	}
	// Navigate validates every link in the staged vault, regardless of this name.
	if _, err = staged.Navigate(changes[0].Path); err != nil {
		return fmt.Errorf("guidance links invalid: %w", err)
	}
	for _, change := range changes {
		if err = real.WriteIfUnchanged(change.Path, []byte(change.Content), expected[change.Path]); err != nil {
			return err
		}
	}
	return nil
}
