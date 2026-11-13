package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCanceledBackgroundSyncDoesNotMarkFailure(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("SSH_AUTH_SOCK", "test-only")
	root := filepath.Join(dir, "vault")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	g := Git{Context: ctx, Remotes: map[string]string{"origin": filepath.Join(dir, "missing.git")}}
	if err := g.Background(root, "origin"); !errors.Is(err, context.Canceled) {
		t.Fatalf("background cancellation: %v", err)
	}
	marker := filepath.Join(root, ".tmx-sync-failing")
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("cancellation marked sync as failing: %v", err)
	}
	g.Context = context.Background()
	if err := g.Background(root, "origin"); err == nil {
		t.Fatal("missing remote succeeded")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("real failure was not recorded: %v", err)
	}
}

func TestCanceledGitReapsTransportChild(t *testing.T) {
	dir := t.TempDir()
	ready := filepath.Join(dir, "transport")
	script := fmt.Sprintf("#!/bin/sh\ntrap 'wait; exit 0' TERM\nsleep 60 &\necho $! > %q\nwait\n", ready)
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g := Git{Context: ctx}
	done := make(chan error, 1)
	go func() { _, err := g.command("", "fetch"); done <- err }()
	child := 0
	deadline := time.After(5 * time.Second)
	for child == 0 {
		if pid, err := os.ReadFile(ready); err == nil {
			child, _ = strconv.Atoi(strings.TrimSpace(string(pid)))
		}
		if child != 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("Git transport did not start")
		case <-time.After(10 * time.Millisecond):
		}
	}
	defer func() {
		if child != 0 {
			_ = syscall.Kill(child, syscall.SIGKILL)
		}
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("command: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("Git did not cancel")
	}
	if err := syscall.Kill(child, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("transport still exists: %v", err)
	}
	child = 0
}

func TestCanceledSyncCleansWorktreeAndPreservesLocalMemory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	remote, root := filepath.Join(dir, "remote.git"), filepath.Join(dir, "vault")
	g := Git{Remotes: map[string]string{"origin": remote}, Out: io.Discard, Err: io.Discard}
	run := func(args ...string) string {
		t.Helper()
		out, err := g.command(root, args...)
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	if err = os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	run("config", "--global", "user.name", "Test")
	run("config", "--global", "user.email", "test@example.invalid")
	run("config", "--global", "commit.gpgsign", "false")
	run("init", "--bare", remote)
	note := filepath.Join(root, "note.md")
	if err = os.WriteFile(note, []byte("# Original\nKeep committed memory.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = g.Backup(root, "origin"); err != nil {
		t.Fatal(err)
	}
	original := run("rev-parse", "HEAD")
	marker := filepath.Join(root, ".tmx-last-sync")
	if err = os.WriteFile(marker, []byte("previous success\n"), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "bin")
	if err = os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(dir, "ready")
	script := fmt.Sprintf("#!/bin/sh\nif [ \"$*\" = 'merge --no-edit refs/tmix/sync' ]; then echo ready > %q; exec sleep 60; fi\nexec %q \"$@\"\n", ready, realGit)
	if err = os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	operation := g
	operation.Context = ctx
	done := make(chan error, 1)
	go func() { done <- operation.Sync(root, "origin") }()
	deadline := time.After(10 * time.Second)
	for {
		if _, err = os.Stat(ready); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("sync ended before merge: %v", err)
		case <-deadline:
			t.Fatal("sync did not reach merge")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if trees := run("worktree", "list", "--porcelain"); strings.Count(trees, "worktree ") != 2 {
		t.Fatalf("missing isolated worktree: %s", trees)
	}
	cancel()
	select {
	case err = <-done:
		if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "needs attention") {
			t.Fatalf("cancel: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("sync did not cancel")
	}
	if trees := run("worktree", "list", "--porcelain"); strings.Count(trees, "worktree ") != 1 {
		t.Fatalf("worktree leaked after cancellation: %s", trees)
	}
	if head := run("rev-parse", "HEAD"); head != original {
		t.Fatalf("local HEAD changed: %s", head)
	}
	body, err := os.ReadFile(note)
	if err != nil || string(body) != "# Original\nKeep committed memory.\n" {
		t.Fatalf("note %q: %v", body, err)
	}
	previous, err := os.ReadFile(marker)
	if err != nil || string(previous) != "previous success\n" {
		t.Fatalf("success marker %q: %v", previous, err)
	}
	// The context-free copy remains usable and can finish the retry.
	if err = os.Remove(ready); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(bin, "git"), []byte(fmt.Sprintf("#!/bin/sh\nexec %q \"$@\"\n", realGit)), 0700); err != nil {
		t.Fatal(err)
	}
	if err = g.Sync(root, "origin"); err != nil {
		t.Fatal(err)
	}
}
