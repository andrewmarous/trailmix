package vault

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/gofrs/flock"
)

// lockNote uses a persistent sidecar: replacing the note must not replace its
// locking identity. Never unlink sidecars, including when releasing a lock.
func (v *Vault) lockNote(p string) (*flock.Flock, error) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return nil, fmt.Errorf("note locking supports only macOS and Linux")
	}
	path := filepath.Join(filepath.Dir(p), ".tmx-lock-"+filepath.Base(p))
	rel, err := filepath.Rel(v.Root, path)
	if err != nil {
		return nil, err
	}
	if _, err := v.safe(rel); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("note lock is not a regular file")
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	// os.OpenFile uses close-on-exec descriptors. A fresh Flock per operation
	// ensures independent calls contend, including calls in the same process.
	lock := flock.New(path, flock.SetFlag(os.O_CREATE|os.O_RDWR), flock.SetPermissions(0600))
	if err := lock.Lock(); err != nil {
		return nil, fmt.Errorf("lock note: %w", err)
	}
	return lock, nil
}
