package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

// Lock serializes CLI mutations with repository updates. External editors do not use it.
func Lock(root string) (func(), error) {
	return LockContext(context.Background(), root)
}

// LockContext allows a CLI operation to stop waiting for another writer.
func LockContext(ctx context.Context, root string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	l := flock.New(filepath.Join(root, ".tmx-operation.lock"))
	if _, err := l.TryLockContext(ctx, 25*time.Millisecond); err != nil {
		return nil, err
	}
	return func() { _ = l.Unlock() }, nil
}

// Sync snapshots local changes, integrates remotely in an isolated worktree, and pushes.
// The caller must hold Lock for root.
func (g Git) Sync(root, remote string) error {
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("vault must be a directory, not a symlink")
	}
	remotes, err := g.load()
	if err != nil {
		return err
	}
	url, ok := remotes[remote]
	if !ok {
		return fmt.Errorf("remote %q is not configured; use tmix remote add", remote)
	}
	if err := validURL(url); err != nil {
		return err
	}
	// An empty local vault can adopt existing history without an unrelated initial commit.
	if _, err := os.Lstat(filepath.Join(root, ".git")); os.IsNotExist(err) {
		if _, err = g.command(root, "init", "--initial-branch=main"); err != nil {
			return err
		}
	}
	info, err := os.Lstat(filepath.Join(root, ".git"))
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("vault .git must be a directory")
	}
	branch, err := g.command(root, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(branch)) != "main" {
		return fmt.Errorf("sync requires the vault's main branch")
	}
	for _, name := range []string{"MERGE_HEAD", "rebase-merge", "rebase-apply", "CHERRY_PICK_HEAD"} {
		if _, err := os.Stat(filepath.Join(root, ".git", name)); err == nil {
			return fmt.Errorf("unfinished Git operation: %s", name)
		}
	}
	// ls-remote distinguishes an empty remote from a network/authentication failure.
	heads, err := g.command(root, "ls-remote", "--heads", url, "refs/heads/main")
	if err != nil {
		return err
	}
	hasRemote := len(heads) > 0
	if hasRemote {
		if _, err := g.command(root, "fetch", "--no-tags", url, "+refs/heads/main:refs/tmix/sync"); err != nil {
			return err
		}
	}
	_, headErr := g.command(root, "rev-parse", "--verify", "HEAD")
	if errors.Is(headErr, context.Canceled) || errors.Is(headErr, context.DeadlineExceeded) {
		return headErr
	}
	if headErr != nil && hasRemote {
		// Git refuses collisions with untracked local files. Never use reset --hard.
		if _, err := g.command(root, "merge", "--ff-only", "refs/tmix/sync"); err != nil {
			return err
		}
	}
	if err := g.snapshot(root); err != nil {
		if errors.Is(err, errNoContent) && !hasRemote {
			return g.syncSuccess(root, remote)
		}
		return err
	}
	if hasRemote {
		temp, err := os.MkdirTemp("", "trailmix-sync-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(temp)
		work := filepath.Join(temp, "vault")
		defer func() {
			// Cleanup must run even if add was interrupted, using its own deadline.
			ctx, cancel := context.WithTimeout(context.WithoutCancel(g.context()), 5*time.Second)
			defer cancel()
			cleanup := g
			cleanup.Context = ctx
			_, _ = cleanup.command(root, "worktree", "remove", "--force", work)
		}()
		if _, err := g.command(root, "worktree", "add", "--detach", work, "HEAD"); err != nil {
			return err
		}
		if _, err := g.command(work, "merge", "--no-edit", "refs/tmix/sync"); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			return fmt.Errorf("sync needs attention: remote changes could not merge; local memory is unchanged: %w", err)
		}
		merged, err := g.command(work, "rev-parse", "HEAD")
		if err != nil {
			return err
		}
		// Refuse publication if an external editor changed tracked files during integration.
		dirty, err := g.command(root, "diff", "HEAD", "--name-only")
		if err != nil {
			return err
		}
		if len(dirty) > 0 {
			return fmt.Errorf("vault changed during sync; retry on the next run")
		}
		if _, err := g.command(root, "merge", "--ff-only", strings.TrimSpace(string(merged))); err != nil {
			return err
		}
	}
	// Always push, even if the working tree is clean after an earlier failed push.
	if _, err := g.command(root, "push", url, "HEAD:refs/heads/main"); err != nil {
		return fmt.Errorf("%w; local changes remain committed; sync will retry", err)
	}
	return g.syncSuccess(root, remote)
}

func (g Git) syncSuccess(root, remote string) error {
	if err := g.context().Err(); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, ".tmx-last-sync"), []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0600); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(root, ".tmx-sync-failing"))
	fmt.Fprintln(g.Out, "Vault synced to", remote)
	return nil
}
