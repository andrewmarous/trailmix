package retrieval

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIndexStartupFailureReturnsDiagnostic(t *testing.T) {
	v, path := fixture(t)
	note(t, v, "evidence", "# Evidence\nKeep the source reference.\n")
	dir := t.TempDir()
	runtime, model := filepath.Join(dir, "runtime"), filepath.Join(dir, "model")
	if err := os.WriteFile(runtime, []byte("#!/bin/sh\necho 'unsupported model architecture' >&2\nexit 7\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(model, []byte("synthetic model"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Index(context.Background(), v, IndexOptions{Database: path, Runtime: LlamaConfig{Executable: runtime, Model: model}})
	if err == nil || !strings.Contains(err.Error(), "unsupported model architecture") {
		t.Fatalf("startup failure: %v", err)
	}
}

func TestCanceledHashAndDiscovery(t *testing.T) {
	v, _ := fixture(t)
	note(t, v, "evidence", "# Evidence\nKeep the source reference.\n")
	path := filepath.Join(t.TempDir(), "model")
	if err := os.WriteFile(path, []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	digest, err := fileHash(context.Background(), path)
	if err != nil || digest != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("hash %q: %v", digest, err)
	}
	notes, _, err := collect(context.Background(), v)
	if err != nil || len(notes) != 1 || notes[0].Path != "evidence.md" || notes[0].Body != "# Evidence\nKeep the source reference.\n" {
		t.Fatalf("notes %+v: %v", notes, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = fileHash(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled hash: %v", err)
	}
	if _, _, err = collect(ctx, v); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled discovery: %v", err)
	}
}
