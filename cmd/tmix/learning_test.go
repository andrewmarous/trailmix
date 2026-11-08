package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLearningSnapshots(t *testing.T) {
	root := t.TempDir()
	call := func(input string, args ...string) (string, error) {
		var out bytes.Buffer
		err := run(append([]string{"--vault", root, "learning"}, args...), strings.NewReader(input), &out, &out)
		return out.String(), err
	}
	must := func(input string, args ...string) string {
		t.Helper()
		out, err := call(input, args...)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	must("Verified first result", "record", "session-1")
	if _, err := call("changed", "record", "session-1"); err == nil {
		t.Fatal("duplicate accepted")
	}
	var first learningRun
	if err := json.Unmarshal([]byte(must("", "begin")), &first); err != nil {
		t.Fatal(err)
	}
	must("Verified second result", "record", "session-2")
	if _, err := call("", "begin"); err == nil {
		t.Fatal("overlapping run accepted")
	}
	if _, err := call("", "complete", first.ID); err == nil {
		t.Fatal("empty outcome accepted")
	}
	must("No reusable lesson; reviewed evidence.", "complete", first.ID)
	var ledger learningLedger
	if err := json.Unmarshal([]byte(must("", "status")), &ledger); err != nil {
		t.Fatal(err)
	}
	if ledger.Sessions[0].ProcessedBy != first.ID || ledger.Sessions[1].ProcessedBy != "" {
		t.Fatalf("bad snapshot: %+v", ledger)
	}
	var second learningRun
	json.Unmarshal([]byte(must("", "begin")), &second)
	if len(second.Sessions) != 1 || second.Sessions[0] != "session-2" {
		t.Fatalf("bad next run: %+v", second)
	}
	must("", "abort", second.ID)
	must("", "begin") // Aborted sessions remain pending.
}

func TestLearningRejectsUnsafeState(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(root, ".learning")); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"--vault", root, "learning", "status"}, strings.NewReader(""), &out, &out); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestLearningNoPending(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"--vault", t.TempDir(), "learning", "begin"}, strings.NewReader(""), &out, &out); err == nil {
		t.Fatal("empty run accepted")
	}
}
