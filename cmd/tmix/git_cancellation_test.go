package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSignalCancelsGitOperation(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "tmix")
	if out, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	for _, operation := range []string{"backup", "sync"} {
		t.Run(operation, func(t *testing.T) {
			dir := t.TempDir()
			root := filepath.Join(dir, "vault")
			bin := filepath.Join(dir, "bin")
			for _, path := range []string{filepath.Join(root, ".git"), bin} {
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			note := filepath.Join(root, "note.md")
			if err := os.WriteFile(note, []byte("# Original\nCommitted memory.\n"), 0600); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(root, ".tmx-last-sync")
			if err := os.WriteFile(marker, []byte("previous success\n"), 0600); err != nil {
				t.Fatal(err)
			}
			pidfile, log, release := filepath.Join(dir, "pid"), filepath.Join(dir, "commands"), filepath.Join(dir, "release")
			block := "push"
			if operation == "sync" {
				block = "ls-remote"
			}
			script := fmt.Sprintf("#!/bin/sh\necho \"$*\" >> %q\nif [ \"$1\" = %q ] && [ ! -f %q ]; then echo $$ > %q; exec sleep 60; fi\ncase \"$1\" in symbolic-ref) echo main;; rev-parse) echo fake-head;; esac\n", log, block, release, pidfile)
			if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			config := filepath.Join(dir, "config.toml")
			if err := os.WriteFile(config, []byte("[remotes]\norigin = \"fake-remote\"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			command := func() *exec.Cmd {
				cmd := exec.Command(binary, "--config", config, "--vault", root, operation)
				cmd.Env = append(os.Environ(), "HOME="+dir, "XDG_CONFIG_HOME="+filepath.Join(dir, "config"), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
				cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
				return cmd
			}
			cmd := command()
			var output bytes.Buffer
			cmd.Stdout, cmd.Stderr = &output, &output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			child := 0
			defer func() {
				_ = cmd.Process.Kill()
				if child != 0 {
					_ = syscall.Kill(-child, syscall.SIGKILL)
					_ = syscall.Kill(child, syscall.SIGKILL)
				}
			}()
			deadline := time.After(10 * time.Second)
			for child == 0 {
				if pid, err := os.ReadFile(pidfile); err == nil {
					child, _ = strconv.Atoi(strings.TrimSpace(string(pid)))
				}
				if child != 0 {
					break
				}
				select {
				case err := <-done:
					t.Fatalf("operation ended before cancellation: %v\n%s", err, output.String())
				case <-deadline:
					t.Fatal("Git did not start")
				case <-time.After(10 * time.Millisecond):
				}
			}
			started := time.Now()
			if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 130 {
					t.Fatalf("exit: %v\n%s", err, output.String())
				}
			case <-time.After(8 * time.Second):
				t.Fatal("Git ignored cancellation")
			}
			if err := syscall.Kill(child, 0); !errors.Is(err, syscall.ESRCH) {
				t.Fatalf("Git process %d still exists: %v", child, err)
			}
			child = 0
			commands, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			final := block + " fake-remote HEAD:refs/heads/main\n"
			if operation == "sync" {
				final = "ls-remote --heads fake-remote refs/heads/main\n"
			}
			if !strings.HasSuffix(string(commands), final) {
				t.Fatalf("unexpected follow-up Git commands: %s", commands)
			}
			body, err := os.ReadFile(note)
			if err != nil || string(body) != "# Original\nCommitted memory.\n" {
				t.Fatalf("note %q: %v", body, err)
			}
			previous, err := os.ReadFile(marker)
			if err != nil || string(previous) != "previous success\n" {
				t.Fatalf("success marker %q: %v", previous, err)
			}
			if !strings.Contains(output.String(), "operation canceled") {
				t.Fatalf("diagnostic: %s", output.String())
			}
			t.Logf("%s canceled and Git reaped in %s", operation, time.Since(started))
			if err := os.WriteFile(release, nil, 0600); err != nil {
				t.Fatal(err)
			}
			out, err := command().CombinedOutput()
			if err != nil {
				t.Fatalf("retry: %v\n%s", err, out)
			}
			success := "Vault backed up to origin"
			if operation == "sync" {
				success = "Vault synced to origin"
			}
			if !strings.Contains(string(out), success) {
				t.Fatalf("retry output: %s", out)
			}
		})
	}
}
