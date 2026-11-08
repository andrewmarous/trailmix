package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andrewmarous/trailmix/learning"
)

func initializeTestLearning(t *testing.T, state string) {
	t.Helper()
	if _, err := (learning.Store{Dir: state}).Initialize(); err != nil {
		t.Fatal(err)
	}
	if err := (learning.Store{Dir: state}).Transaction(func(r *learning.Registry) error { r.LearningStartAt = "1999-01-01T00:00:00Z"; return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestSessionsCLIConfigurationAndProgress(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	initializeTestLearning(t, state)
	pi := filepath.Join(root, "pi")
	archive := filepath.Join(root, "archive")
	if err := os.MkdirAll(pi, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(archive, 0700); err != nil {
		t.Fatal(err)
	}
	transcript := `{"type":"session","version":3,"id":"one","cwd":"/work/oz"}
{"type":"message","id":"u1","parentId":null,"message":{"role":"user","content":"fix it"}}
{"type":"message","id":"a1","parentId":"u1","message":{"role":"assistant","content":"fixed","stopReason":"stop"},"timestamp":"2000-01-01T00:00:00Z"}
`
	path := filepath.Join(pi, "one.jsonl")
	if err := os.WriteFile(path, []byte(transcript), 0600); err != nil {
		t.Fatal(err)
	}
	call := func(input string, args ...string) ([]byte, error) {
		var out bytes.Buffer
		err := run(append([]string{"--vault", filepath.Join(root, "unused-vault"), "learning", "sessions", "--state-dir", state}, args...), strings.NewReader(input), &out, &out)
		return out.Bytes(), err
	}
	must := func(input string, args ...string) []byte {
		t.Helper()
		b, err := call(input, args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return b
	}
	must("", "--pi-root", pi, "--codex-root", "", "--codex-archive-root", archive, "scan")
	var registry learning.Registry
	if err := json.Unmarshal(must("", "status"), &registry); err != nil {
		t.Fatal(err)
	}
	if len(registry.Sources) != 2 || len(registry.Sessions) != 1 {
		t.Fatalf("bad discovery: %+v", registry)
	}
	// An override of one slot must not turn a disabled active root back on or
	// move the archive into the active slot.
	must("", "--pi-root", pi, "scan")
	json.Unmarshal(must("", "status"), &registry)
	if len(registry.Sources) != 2 || registry.Sources[1].Name != "codex-archive-root" {
		t.Fatalf("source configuration changed: %+v", registry.Sources)
	}
	var review learning.Run
	if err := json.Unmarshal(must("", "begin", "pi/one"), &review); err != nil {
		t.Fatal(err)
	}
	var packets []learning.Packet
	if err := json.Unmarshal(must("", "read", review.ID), &packets); err != nil {
		t.Fatal(err)
	}
	if len(packets) != 1 || packets[0].FromTurn != 1 {
		t.Fatalf("bad review: %+v", packets)
	}
	transcript += `{"type":"message","id":"u2","parentId":"a1","message":{"role":"user","content":"continue"}}
{"type":"message","id":"a2","parentId":"u2","message":{"role":"assistant","content":"done","stopReason":"stop"},"timestamp":"2000-01-01T00:00:00Z"}
`
	if err := os.WriteFile(path, []byte(transcript), 0600); err != nil {
		t.Fatal(err)
	}
	must("No reusable lesson.", "complete", review.ID)
	json.Unmarshal(must("", "status"), &registry)
	if registry.Sessions[0].ReviewedTurns != 1 || registry.Sessions[0].PendingTurns != 1 {
		t.Fatalf("resumed turn swallowed: %+v", registry.Sessions)
	}
	if _, err := os.Stat(filepath.Join(root, "unused-vault")); !os.IsNotExist(err) {
		t.Fatal("global session registry touched the vault")
	}
	// Reset retains history but clears the watermark, then the next scan clears
	// any reconciliation flag.
	must("", "reset", "pi/one")
	must("", "scan")
	json.Unmarshal(must("", "status"), &registry)
	if registry.Sessions[0].ReviewedTurns != 0 {
		t.Fatal("reset failed")
	}
}

func TestSessionsBeginPersistsCoverageErrors(t *testing.T) {
	var out bytes.Buffer
	state := filepath.Join(t.TempDir(), "state")
	initializeTestLearning(t, state)
	args := []string{"learning", "sessions", "--state-dir", state, "--pi-root", "", "--codex-root", "", "--codex-archive-root", "", "begin"}
	if err := run(args, strings.NewReader(""), &out, &out); err == nil {
		t.Fatal("empty run accepted")
	}
	out.Reset()
	if err := run([]string{"learning", "sessions", "--state-dir", state, "status"}, strings.NewReader(""), &out, &out); err != nil {
		t.Fatal(err)
	}
	var registry learning.Registry
	if err := json.Unmarshal(out.Bytes(), &registry); err != nil {
		t.Fatal(err)
	}
	if len(registry.Sources) != 0 {
		t.Fatal("disabled sources restored unexpectedly")
	}
}
