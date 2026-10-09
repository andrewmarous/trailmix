package backup

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteAndBackup(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	// Avoid the developer's global identity and Git configuration.
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	var out bytes.Buffer
	g := Git{Config: filepath.Join(dir, "config", "remotes.gitconfig"), Out: &out, Err: &out}
	remote := filepath.Join(dir, "remote.git")
	if _, err := g.command("", "init", "--bare", remote); err != nil {
		t.Fatal(err)
	}
	if err := g.Remote([]string{"add", "origin", remote}); err != nil {
		t.Fatal(err)
	}
	if err := g.Remote([]string{"add", "origin", remote}); err == nil {
		t.Fatal("duplicate accepted")
	}
	if err := g.Remote([]string{"rename", "origin", "archive"}); err != nil {
		t.Fatal(err)
	}
	if err := g.Remote([]string{"get-url", "archive"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), remote) {
		t.Fatal(out.String())
	}
	if err := g.Remote([]string{"set-url", "archive", remote}); err != nil {
		t.Fatal(err)
	}
	if err := g.Remote([]string{"rename", "archive", "origin"}); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "vault")
	if err := os.MkdirAll(filepath.Join(root, ".obsidian"), 0700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "note.md"), []byte("memory"), 0600)
	if err := os.WriteFile(filepath.Join(root, ".tmx-lock-note.md"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, ".obsidian", "workspace.json"), []byte("private UI"), 0600)
	if _, err := g.command(root, "init", "--initial-branch=main"); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"user.name": "Test", "user.email": "test@example.invalid", "commit.gpgsign": "false"} {
		if _, err := g.command(root, "config", key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := g.Backup(root, "origin"); err != nil {
		t.Fatal(err)
	}
	tree, err := g.command("", "--git-dir", remote, "ls-tree", "-r", "--name-only", "main")
	if err != nil {
		t.Fatal(err)
	}
	if string(tree) != "note.md\n" {
		t.Fatal(string(tree))
	}
	if err := g.Backup(root, "origin"); err != nil {
		t.Fatal("unchanged backup:", err)
	}
	os.Remove(filepath.Join(root, "note.md"))
	if err := g.Backup(root, "origin"); err != nil {
		t.Fatal("deletion:", err)
	}
	// A remote-only commit must cause a rejected push, not overwrite history.
	other := filepath.Join(dir, "other")
	if _, err := g.command("", "clone", "--branch", "main", remote, other); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"user.name": "Test", "user.email": "test@example.invalid", "commit.gpgsign": "false"} {
		if _, err := g.command(other, "config", key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(other, "external.md"), []byte("external"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "external"}, {"push", "origin", "main"}} {
		if _, err := g.command(other, args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "local.md"), []byte("local"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := g.Backup(root, "origin"); err == nil {
		t.Fatal("conflicting push accepted")
	}
	tree, err = g.command("", "--git-dir", remote, "ls-tree", "-r", "--name-only", "main")
	if err != nil || string(tree) != "external.md\n" {
		t.Fatal(string(tree), err)
	}
	if err := g.Remote([]string{"remove", "origin"}); err != nil {
		t.Fatal(err)
	}
	if err := g.Backup(root, "origin"); err == nil {
		t.Fatal("missing remote accepted")
	}
}
