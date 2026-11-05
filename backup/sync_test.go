package backup

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncAcrossVaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	g := Git{Config: filepath.Join(dir, "remotes"), Out: io.Discard, Err: io.Discard}
	run := func(root string, args ...string) string {
		t.Helper()
		b, e := g.command(root, args...)
		if e != nil {
			t.Fatal(e)
		}
		return string(b)
	}
	run("", "config", "--global", "user.name", "Test")
	run("", "config", "--global", "user.email", "test@example.invalid")
	run("", "config", "--global", "commit.gpgsign", "false")
	remote := filepath.Join(dir, "remote.git")
	run("", "init", "--bare", remote)
	if e := g.Remote([]string{"add", "origin", remote}); e != nil {
		t.Fatal(e)
	}
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	for _, root := range []string{a, b} {
		if e := os.Mkdir(root, 0700); e != nil {
			t.Fatal(e)
		}
	}
	put := func(root, name, text string) {
		t.Helper()
		if e := os.WriteFile(filepath.Join(root, name), []byte(text), 0600); e != nil {
			t.Fatal(e)
		}
	}
	sync := func(root string) {
		t.Helper()
		if e := g.Sync(root, "origin"); e != nil {
			t.Fatal(e)
		}
	}
	put(a, "note.md", "initial\n")
	sync(a)
	sync(b)
	if got := run(b, "show", "HEAD:note.md"); got != "initial\n" {
		t.Fatal(got)
	}
	// Independent changes merge, and a clean second run is safe.
	put(a, "a.md", "from a\n")
	put(b, "b.md", "from b\n")
	sync(a)
	sync(b)
	sync(a)
	sync(a)
	if _, e := os.Stat(filepath.Join(a, "b.md")); e != nil {
		t.Fatal(e)
	}
	// Remote deletions reach the other vault.
	if e := os.Remove(filepath.Join(a, "a.md")); e != nil {
		t.Fatal(e)
	}
	sync(a)
	sync(b)
	if _, e := os.Stat(filepath.Join(b, "a.md")); !os.IsNotExist(e) {
		t.Fatal("deletion not synced", e)
	}
	// Conflicts never publish markers into live memory.
	put(a, "note.md", "version a\n")
	put(b, "note.md", "version b\n")
	sync(a)
	if e := g.Sync(b, "origin"); e == nil || !strings.Contains(e.Error(), "needs attention") {
		t.Fatal("expected conflict", e)
	}
	content, e := os.ReadFile(filepath.Join(b, "note.md"))
	if e != nil || string(content) != "version b\n" {
		t.Fatal(string(content), e)
	}
	if trees := run(b, "worktree", "list", "--porcelain"); strings.Count(trees, "worktree ") != 1 {
		t.Fatal(trees)
	}
	// Resolve locally, then retry the committed-but-unpushed snapshot with no new edits.
	put(b, "note.md", "version a\n")
	sync(b)
	put(b, "retry.md", "retry\n")
	if e := g.snapshot(b); e != nil {
		t.Fatal(e)
	}
	sync(b)
	if got := run("", "--git-dir", remote, "show", "main:retry.md"); got != "retry\n" {
		t.Fatal(got)
	}
	tree := run("", "--git-dir", remote, "ls-tree", "-r", "--name-only", "main")
	if strings.Contains(tree, ".tmx-") {
		t.Fatal(tree)
	}
}
