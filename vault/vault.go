// Package vault operates on a shared Markdown knowledge base.
package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

var Categories = []string{"concepts", "projects", "decisions", "experiences", "preferences", "procedures", "inbox"}

// ErrUnsafePath distinguishes rejected note locations from filesystem I/O failures.
var ErrUnsafePath = errors.New("unsafe vault path")

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
		return "", fmt.Errorf("%w: path must be relative to the vault", ErrUnsafePath)
	}
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: path escapes the vault", ErrUnsafePath)
	}
	full := filepath.Join(v.Root, clean)
	for p := full; ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%w: symlinks are not supported: %s", ErrUnsafePath, p)
		}
		if p == v.Root {
			break
		}
	}
	return full, nil
}

func (v *Vault) note(name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("%w: note path is required", ErrUnsafePath)
	}
	if filepath.Ext(name) == "" {
		name += ".md"
	}
	if !strings.EqualFold(filepath.Ext(name), ".md") {
		return "", fmt.Errorf("%w: notes must have a .md extension", ErrUnsafePath)
	}
	for _, part := range strings.Split(filepath.ToSlash(name), "/") {
		if strings.HasPrefix(part, ".") {
			return "", fmt.Errorf("%w: hidden and traversal paths are not allowed", ErrUnsafePath)
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
	info, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("note is not a regular file: %s", name)
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
}
