// Package vault operates on a shared Markdown knowledge base.
package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

var Categories = []string{"concepts", "projects", "decisions", "experiences", "preferences", "procedures", "inbox"}

type Vault struct{ Root string }

func Open(root string) (*Vault, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &Vault{Root: abs}, nil
}

// safe rejects symlinks in the root and every existing component inside it.
// Operations assume the filesystem is not concurrently modified by an adversary.
func (v *Vault) safe(rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("path must be relative to the vault")
	}
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes the vault")
	}
	full := filepath.Join(v.Root, clean)
	for p := full; ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("symlinks are not supported: %s", p)
		}
		if p == v.Root {
			break
		}
	}
	return full, nil
}

func (v *Vault) note(name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("note path is required")
	}
	if filepath.Ext(name) == "" {
		name += ".md"
	}
	if !strings.EqualFold(filepath.Ext(name), ".md") {
		return "", fmt.Errorf("notes must have a .md extension")
	}
	for _, part := range strings.Split(filepath.ToSlash(name), "/") {
		if strings.HasPrefix(part, ".") {
			return "", fmt.Errorf("hidden and traversal paths are not allowed")
		}
	}
	return v.safe(name)
}

func (v *Vault) Init() error {
	for _, dir := range append([]string{".", ".obsidian"}, Categories...) {
		p, err := v.safe(dir)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(p, 0700); err != nil {
			return err
		}
	}
	return nil
}

func (v *Vault) Read(name string) ([]byte, error) {
	p, err := v.note(name)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}

func (v *Vault) Write(name string, content []byte, overwrite bool) (err error) {
	p, err := v.note(name)
	if err != nil {
		return err
	}
	if !utf8.Valid(content) {
		return fmt.Errorf("note content must be UTF-8")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	lock, err := v.lockNote(p)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lock.Unlock()) }()
	return publish(p, content, overwrite)
}

// publish requires the caller to hold the note lock.
func publish(p string, content []byte, overwrite bool) error {
	mode := fs.FileMode(0600)
	if info, err := os.Stat(p); err == nil {
		if !overwrite {
			return fmt.Errorf("note already exists; use --overwrite")
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("note is not a regular file")
		}
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".tmx-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := f.Chmod(mode); err != nil {
		return err
	}
	if _, err := f.Write(content); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if overwrite {
		return os.Rename(f.Name(), p)
	}
	// Linking publishes the completed file without replacing an existing note.
	return os.Link(f.Name(), p)
}

// Append preserves the input exactly. Callers provide any desired separators.
// The note lock covers the entire read-modify-write operation.
func (v *Vault) Append(name string, content []byte) (err error) {
	p, err := v.note(name)
	if err != nil {
		return err
	}
	if !utf8.Valid(content) {
		return fmt.Errorf("note content must be UTF-8")
	}
	lock, err := v.lockNote(p)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lock.Unlock()) }()
	existing, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	if !utf8.Valid(existing) {
		return fmt.Errorf("note content must be UTF-8")
	}
	return publish(p, append(existing, content...), true)
}

type Result struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Snippet string `json:"snippet"`
	score   int
}

func snippet(s string) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > 240 {
		return string(r[:240]) + "…"
	}
	return string(r)
}

// Search matches all whitespace-separated terms across a note's path and body.
// Path and heading matches rank before body-only matches; one result per note.
func (v *Vault) Search(query string, limit int) ([]Result, error) {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return nil, fmt.Errorf("search query is required")
	}
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("limit must be between 1 and 100")
	}
	if _, err := v.safe("."); err != nil {
		return nil, err
	}
	results := []Result{}
	err := filepath.WalkDir(v.Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != v.Root && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 || d.IsDir() || !strings.EqualFold(filepath.Ext(p), ".md") {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(v.Root, p)
		rel = filepath.ToSlash(rel)
		body := strings.ToLower(string(data))
		path := strings.ToLower(rel)
		for _, term := range terms {
			if !strings.Contains(path+"\n"+body, term) {
				return nil
			}
		}
		r := Result{Path: rel, Snippet: rel}
		for _, term := range terms {
			if strings.Contains(path, term) {
				r.score += 10
			}
		}
		best := -1
		for i, line := range strings.Split(string(data), "\n") {
			matches := 0
			for _, term := range terms {
				if strings.Contains(strings.ToLower(line), term) {
					matches++
				}
			}
			if matches > 0 && strings.HasPrefix(strings.TrimSpace(line), "#") {
				r.score += matches * 5
			}
			if matches > best && matches > 0 {
				best = matches
				r.Line = i + 1
				r.Snippet = snippet(line)
			}
		}
		results = append(results, r)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].score != results[j].score {
			return results[i].score > results[j].score
		}
		return results[i].Path < results[j].Path
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}
