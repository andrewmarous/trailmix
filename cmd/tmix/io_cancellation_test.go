package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/andrewmarous/trailmix/backup"
	"github.com/andrewmarous/trailmix/config"
)

func TestCanceledOperationLocks(t *testing.T) {
	for name, lock := range map[string]func(context.Context, string) (func(), error){"vault": backup.LockContext, "config": config.LockContext} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "locked")
			hold, err := lock(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			defer hold()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				unlock, err := lock(ctx, path)
				if unlock != nil {
					unlock()
				}
				done <- err
			}()
			cancel()
			select {
			case err = <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("lock cancellation: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("lock ignored cancellation")
			}
			hold()
			unlock, err := lock(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			unlock()
		})
	}
}

type failingInput struct{}

func (failingInput) Read(p []byte) (int, error) { return copy(p, "partial"), io.ErrUnexpectedEOF }
func TestCLIInputPreservesReadError(t *testing.T) {
	input := newCLIInput(failingInput{})
	defer input.Close()
	body, err := io.ReadAll(input)
	if string(body) != "partial" || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("input=%q error=%v", body, err)
	}
}

type cancelAtEOF struct {
	io.Reader
	cancel context.CancelFunc
	closed bool
}

func (r *cancelAtEOF) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF {
		r.cancel()
	}
	return n, err
}
func (r *cancelAtEOF) Close() error { r.closed = true; return nil }

func TestCanceledInputDoesNotPublishOrCloseCallerReader(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "vault")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("# Original\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input := &cancelAtEOF{Reader: strings.NewReader("# Partial\n"), cancel: cancel}
	err := runCommand(ctx, []string{"--config", filepath.Join(dir, "config.toml"), "--vault", root, "write", "--overwrite", "note"}, input, io.Discard, io.Discard)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled input: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "# Original\n" || input.closed {
		t.Fatalf("original=%q closed=%v error=%v", body, input.closed, err)
	}
}

func TestSignalCancelsBlockedStdinWithoutPublishingPartialNote(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "tmix")
	if out, err := exec.Command("go", "build", "-o", binary, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	root := filepath.Join(dir, "vault")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "note.md")
	if err := os.WriteFile(path, []byte("# Original\nKeep this note.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "--config", filepath.Join(dir, "config.toml"), "--vault", root, "write", "--overwrite", "note")
	cmd.Env = append(os.Environ(), "HOME="+dir, "XDG_CONFIG_HOME="+filepath.Join(dir, "config"))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	// More than a pipe buffer: completing the write proves the CLI consumed input.
	written := make(chan error, 1)
	go func() { _, err := input.Write([]byte(strings.Repeat("partial note\n", 65536))); written <- err }()
	select {
	case err = <-written:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("CLI did not read stdin")
	}
	if err = cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 130 {
			t.Fatalf("exit: %v\n%s", err, output.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("first signal did not unblock stdin")
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "# Original\nKeep this note.\n" {
		t.Fatalf("original note %q: %v", body, err)
	}
	if !strings.Contains(output.String(), "operation canceled") {
		t.Fatalf("missing cancellation diagnostic: %s", output.String())
	}
	// Normal EOF still publishes the complete replacement.
	retry := exec.Command(binary, "--config", filepath.Join(dir, "config.toml"), "--vault", root, "write", "--overwrite", "note")
	retry.Env = cmd.Env
	retry.Stdin = strings.NewReader("# Complete\nReplacement note.\n")
	if out, err := retry.CombinedOutput(); err != nil {
		t.Fatalf("retry: %v\n%s", err, out)
	}
	body, err = os.ReadFile(path)
	if err != nil || string(body) != "# Complete\nReplacement note.\n" {
		t.Fatalf("replacement %q: %v", body, err)
	}
}
