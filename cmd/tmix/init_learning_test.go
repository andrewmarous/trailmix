package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andrewmarous/trailmix/learning"
)

func TestInitOwnsMachineLearningCutoff(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	invoke := func(args ...string) (string, error) {
		var out bytes.Buffer
		err := run(args, strings.NewReader(""), &out, &out)
		return out.String(), err
	}
	if out, err := invoke("learning", "sessions", "scan"); err == nil || !strings.Contains(err.Error(), "tmix init") {
		t.Fatalf("scan did not require init: %s %v", out, err)
	}
	path := filepath.Join(learning.DefaultStateDir(home), "registry.json")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("scan created registry before init")
	}
	before := time.Now()
	out, err := invoke("--vault", filepath.Join(home, "vault-one"), "init")
	if err != nil {
		t.Fatal(err)
	}
	var first learning.Registry
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &first); err != nil {
		t.Fatal(err)
	}
	cutoff, err := time.Parse(time.RFC3339Nano, first.LearningStartAt)
	if err != nil {
		t.Fatal(err)
	}
	if cutoff.Before(before) || cutoff.After(time.Now()) || !strings.Contains(out, first.LearningStartAt) {
		t.Fatalf("unexpected initialization: %s", out)
	}
	if _, err = invoke("--vault", filepath.Join(home, "vault-two"), "init"); err != nil {
		t.Fatal(err)
	}
	var again learning.Registry
	b, _ = os.ReadFile(path)
	_ = json.Unmarshal(b, &again)
	if first.LearningStartAt != again.LearningStartAt {
		t.Fatal("second vault reset machine cutoff")
	}
	if _, err = invoke("learning", "sessions", "--start-at", "2000-01-01T00:00:00-05:00", "config"); err != nil {
		t.Fatal(err)
	}
	if _, err = invoke("--vault", filepath.Join(home, "vault-one"), "init"); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	_ = json.Unmarshal(b, &again)
	if again.LearningStartAt != "2000-01-01T05:00:00Z" {
		t.Fatal("init discarded explicit cutoff")
	}
	if _, err = invoke("learning", "sessions", "--start-at", "yesterday", "config"); err == nil {
		t.Fatal("invalid cutoff accepted")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(b) {
		t.Fatal("invalid config altered registry")
	}
}
func TestInitCustomLearningState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	state := filepath.Join(home, "custom-state")
	var out bytes.Buffer
	if err := run([]string{"--vault", filepath.Join(home, "vault"), "init", "--learning-state-dir", state}, strings.NewReader(""), &out, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(state, "registry.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(learning.DefaultStateDir(home)); !os.IsNotExist(err) {
		t.Fatal("custom initialization also touched default registry")
	}
}
