package vault

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// Reference identifies a link and its resolved vault-relative file.
type Reference struct {
	Link
	Source string `json:"source"`
	Path   string `json:"path"`
}

// Navigation contains resolved outgoing references and backlinks.
type Navigation struct {
	Outgoing  []Reference `json:"outgoing"`
	Backlinks []Reference `json:"backlinks"`
}

// Navigate scans the vault without a persistent index. Every parsed link must
// resolve to exactly one visible regular file. Fragments are not validated.
// A broken link anywhere in the scan returns an error and no partial result.
func (v *Vault) Navigate(name string) (*Navigation, error) {
	requested, err := v.note(name)
	if err != nil {
		return nil, err
	}
	if _, err := v.Read(name); err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(v.Root, requested)
	if err != nil {
		return nil, err
	}
	relative = filepath.ToSlash(relative)
	files := []string{}
	err = filepath.WalkDir(v.Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != v.Root && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(v.Root, p)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	result := &Navigation{Outgoing: []Reference{}, Backlinks: []Reference{}}
	for _, source := range files {
		if !strings.EqualFold(filepath.Ext(source), ".md") {
			continue
		}
		content, err := v.Read(source)
		if err != nil {
			return nil, err
		}
		for _, link := range ParseLinks(string(content)) {
			target, err := resolveLink(source, link.Target, files)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", source, link.Line, err)
			}
			ref := Reference{Link: link, Source: source, Path: target}
			if source == relative {
				result.Outgoing = append(result.Outgoing, ref)
			}
			if target == relative {
				result.Backlinks = append(result.Backlinks, ref)
			}
		}
	}
	return result, nil
}

func resolveLink(source, target string, files []string) (string, error) {
	if target == "" {
		return source, nil
	}
	if strings.Contains(target, "\\") || strings.HasPrefix(target, "/") || filepath.IsAbs(target) || strings.Contains(target, ":") {
		return "", fmt.Errorf("unsafe link target %q", target)
	}
	for _, part := range strings.Split(target, "/") {
		if part == "" || strings.HasPrefix(part, ".") {
			return "", fmt.Errorf("unsafe link target %q", target)
		}
	}
	matches := []string{}
	for _, file := range files {
		candidate := file
		if !strings.Contains(target, "/") {
			candidate = filepath.Base(file)
		}
		if candidate == target || (filepath.Ext(target) == "" && strings.EqualFold(filepath.Ext(candidate), ".md") && strings.TrimSuffix(candidate, filepath.Ext(candidate)) == target) {
			matches = append(matches, file)
		}
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("missing link target %q", target)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("ambiguous link target %q: %s", target, strings.Join(matches, ", "))
	}
	return matches[0], nil
}
