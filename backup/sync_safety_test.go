package backup

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestSyncRefusesUnsafeStates(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	g := Git{Config: filepath.Join(dir, "remotes"), Out: io.Discard, Err: io.Discard}
	run := func(root string, args ...string) {
		t.Helper()
		if _, err := g.command(root, args...); err != nil {
			t.Fatal(err)
		}
	}
	run("", "config", "--global", "user.name", "Test")
	run("", "config", "--global", "user.email", "test@example.invalid")
	run("", "config", "--global", "commit.gpgsign", "false")
	remote := filepath.Join(dir, "remote.git")
	run("", "init", "--bare", remote)
	if err := g.Remote([]string{"add", "origin", remote}); err != nil {
		t.Fatal(err)
	}
	seed := filepath.Join(dir, "seed")
	_ = os.Mkdir(seed, 0700)
	_ = os.WriteFile(filepath.Join(seed, "note.md"), []byte("remote\n"), 0600)
	if err := g.Sync(seed, "origin"); err != nil {
		t.Fatal(err)
	}
	t.Run("untracked collision", func(t *testing.T) {
		root := filepath.Join(dir, "collision")
		_ = os.Mkdir(root, 0700)
		_ = os.WriteFile(filepath.Join(root, "note.md"), []byte("local\n"), 0600)
		if err := g.Sync(root, "origin"); err == nil {
			t.Fatal("accepted collision")
		}
		data, err := os.ReadFile(filepath.Join(root, "note.md"))
		if err != nil || string(data) != "local\n" {
			t.Fatal(string(data), err)
		}
	})
	t.Run("unrelated history", func(t *testing.T) {
		root := filepath.Join(dir, "unrelated")
		_ = os.Mkdir(root, 0700)
		_ = os.WriteFile(filepath.Join(root, "other.md"), []byte("local\n"), 0600)
		if err := g.snapshot(root); err != nil {
			t.Fatal(err)
		}
		if err := g.Sync(root, "origin"); err == nil {
			t.Fatal("accepted unrelated history")
		}
		if _, err := os.Stat(filepath.Join(root, "note.md")); !os.IsNotExist(err) {
			t.Fatal("published unrelated remote files", err)
		}
	})
	t.Run("wrong branch", func(t *testing.T) {
		run(seed, "switch", "-c", "other")
		if err := g.Sync(seed, "origin"); err == nil {
			t.Fatal("accepted wrong branch")
		}
		run(seed, "switch", "main")
	})
	t.Run("unfinished merge", func(t *testing.T) {
		path := filepath.Join(seed, ".git", "MERGE_HEAD")
		_ = os.WriteFile(path, []byte("unfinished"), 0600)
		defer os.Remove(path)
		if err := g.Sync(seed, "origin"); err == nil {
			t.Fatal("accepted unfinished merge")
		}
	})
}
