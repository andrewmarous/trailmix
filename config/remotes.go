package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
	"github.com/pelletier/go-toml/v2"
)

// SaveRemotes replaces the legacy remote configuration on the next explicit
// remote mutation. It canonicalizes TOML formatting; all typed settings survive.
func Lock(path string) (func(), error) {
	return LockContext(context.Background(), path)
}

// LockContext lets a CLI remote operation cancel while waiting for the lock.
func LockContext(ctx context.Context, path string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	lock := flock.New(path + ".lock")
	if _, err := lock.TryLockContext(ctx, 25*time.Millisecond); err != nil {
		return nil, err
	}
	return func() { _ = lock.Unlock() }, nil
}
func SaveRemotes(path string, remotes map[string]string, alreadyLocked ...bool) error {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("edit symlinked config manually instead of using remote mutation commands")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if len(alreadyLocked) == 0 || !alreadyLocked[0] {
		unlock, err := Lock(path)
		if err != nil {
			return err
		}
		defer unlock()
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	c, err := Load(path, home)
	if err != nil {
		return err
	}
	c.Remotes = remotes
	b, err := toml.Marshal(c)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".tmix-config-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
