package vault

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf8"
)

// WriteIfUnchanged holds the note lock across comparison and publication.
// Empty expectedDigest means the note must not exist. Identical new content is
// accepted to allow recovery after partially publishing a multi-note proposal.
func (v *Vault) WriteIfUnchanged(name string, content []byte, expectedDigest string) (err error) {
	p, err := v.note(name)
	if err != nil {
		return err
	}
	if !utf8.Valid(content) {
		return fmt.Errorf("note content must be UTF-8")
	}
	if err = os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	lock, err := v.lockNote(p)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lock.Unlock()) }()
	if info, e := os.Lstat(p); e == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("note is not a regular file: %s", name)
	}
	current, err := os.ReadFile(p)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if exists && string(current) == string(content) {
		return nil
	}
	actual := ""
	if exists {
		actual = fmt.Sprintf("%x", sha256.Sum256(current))
	}
	if actual != expectedDigest {
		return fmt.Errorf("note changed since review: %s", name)
	}
	return publish(p, content, exists)
}
