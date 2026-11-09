package learning

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func injectHarness(t *testing.T, path, script string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(b), "output=''", script+"\noutput=''", 1)), 0700); err != nil {
		t.Fatal(err)
	}
}

func TestDrainStopsOnFailureAndRetainsReportAfterRecovery(t *testing.T) {
	c, _ := workerFixture(t, "pi", "automatic")
	timedPi(t, filepath.Join(c.Learning.Sources.Pi, "pi.jsonl"), "p", "2000-01-01T00:00:00Z", "2000-01-02T00:00:00Z", "2000-01-03T00:00:00Z")
	counter := filepath.Join(filepath.Dir(c.Learning.Executable), "calls")
	injectHarness(t, c.Learning.Executable, `n=0
if [ -f '`+counter+`' ]; then n=$(cat '`+counter+`'); fi
n=$((n+1))
printf '%s' "$n" > '`+counter+`'
if [ "$n" = 2 ]; then exit 42; fi`)
	s, err := RunOnce(context.Background(), c)
	if err == nil || s.Status != "failed" || s.BatchesCompleted != 1 || s.TurnsCompleted != 1 || s.TurnsRemaining != 2 || !strings.Contains(s.Error, "exit status 42") {
		t.Fatalf("wrong partial failure: %+v %v", s, err)
	}
	assertReviewed(t, c, 1)
	failure := LoadFailureStatus(c.Learning.StateDir)
	if failure.RunID != s.RunID || failure.TurnsRemaining != 2 || failure.Error != s.Error {
		t.Fatalf("lost failure: %+v", failure)
	}
	s, err = RunOnce(context.Background(), c)
	if err != nil || s.Status != "completed" || s.TurnsCompleted != 2 || s.BatchesCompleted != 2 {
		t.Fatalf("retry: %+v %v", s, err)
	}
	assertReviewed(t, c, 3)
	s, err = RunOnce(context.Background(), c)
	if err != nil || s.Status != "no_work" {
		t.Fatalf("%+v %v", s, err)
	}
	if got := LoadFailureStatus(c.Learning.StateDir); got.Error != failure.Error || got.RunID != failure.RunID {
		t.Fatalf("success erased failure: %+v", got)
	}
	b, _ := os.ReadFile(counter)
	if string(b) != "4" {
		t.Fatalf("retried successful work or continued after failure: calls=%s", b)
	}
}

func TestDrainFreezesWorkBeforeHarnessExecution(t *testing.T) {
	c, _ := workerFixture(t, "pi", "automatic")
	expanded := filepath.Join(filepath.Dir(c.Learning.Executable), "expanded.jsonl")
	timedPi(t, expanded, "p", "2000-01-01T00:00:00Z", "2000-01-02T00:00:00Z")
	injectHarness(t, c.Learning.Executable, "cp '"+expanded+"' '"+filepath.Join(c.Learning.Sources.Pi, "pi.jsonl")+"'")
	s, err := RunOnce(context.Background(), c)
	if err != nil || s.TurnsCompleted != 1 || s.TurnsTargeted != 1 {
		t.Fatalf("snapshot grew: %+v %v", s, err)
	}
	assertReviewed(t, c, 1)
	s, err = RunOnce(context.Background(), c)
	if err != nil || s.Status != "completed" || s.TurnsCompleted != 1 {
		t.Fatalf("new work not deferred: %+v %v", s, err)
	}
	assertReviewed(t, c, 2)
}

func TestDrainExcludesFutureCompletions(t *testing.T) {
	c, _ := workerFixture(t, "pi", "automatic")
	timedPi(t, filepath.Join(c.Learning.Sources.Pi, "pi.jsonl"), "p", "2000-01-01T00:00:00Z", time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano))
	s, err := RunOnce(context.Background(), c)
	if err != nil || s.TurnsTargeted != 1 || s.TurnsCompleted != 1 {
		t.Fatalf("reviewed future work: %+v %v", s, err)
	}
	assertReviewed(t, c, 1)
}

func TestDrainRejectsChangedSnapshotAfterSuccessfulBatch(t *testing.T) {
	c, _ := workerFixture(t, "pi", "automatic")
	source := filepath.Join(c.Learning.Sources.Pi, "pi.jsonl")
	timedPi(t, source, "p", "2000-01-01T00:00:00Z", "2000-01-02T00:00:00Z")
	changed := filepath.Join(filepath.Dir(c.Learning.Executable), "changed.jsonl")
	timedPi(t, changed, "p", "2000-01-01T00:00:00Z", "2000-01-02T00:00:00Z")
	b, err := os.ReadFile(changed)
	if err != nil {
		t.Fatal(err)
	}
	n := strings.LastIndex(string(b), "done")
	if n < 0 {
		t.Fatal("missing fixture answer")
	}
	if err := os.WriteFile(changed, []byte(string(b[:n])+"changed answer"+string(b[n+4:])), 0600); err != nil {
		t.Fatal(err)
	}
	injectHarness(t, c.Learning.Executable, "cp '"+changed+"' '"+source+"'")
	s, err := RunOnce(context.Background(), c)
	if err == nil || s.Status != "failed" || s.TurnsCompleted != 1 || s.TurnsRemaining != 1 || !strings.Contains(s.Error, "snapshot changed") {
		t.Fatalf("changed evidence accepted: %+v %v", s, err)
	}
	assertReviewed(t, c, 1)
}

func TestCorruptFailureReportIsVisible(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "last_failure.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	s := LoadFailureStatus(dir)
	if s.Status != "failed" || !strings.Contains(s.Error, "read learning report") || !strings.Contains(s.Error, "unexpected end of JSON input") {
		t.Fatalf("silently lost report: %+v", s)
	}
}

func TestWorkerConfigurationFailureIsRetained(t *testing.T) {
	c, _ := workerFixture(t, "pi", "automatic")
	c.Learning.Timeout = "invalid"
	s, err := RunOnce(context.Background(), c)
	if err == nil || s.Status != "failed" || !strings.Contains(s.Error, "learning.timeout") {
		t.Fatalf("%+v %v", s, err)
	}
	if got := LoadFailureStatus(c.Learning.StateDir); got.Error != s.Error || got.Status != "failed" {
		t.Fatalf("lost configuration failure: %+v", got)
	}
}

func TestDrainTimeoutIsPerBatch(t *testing.T) {
	c, _ := workerFixture(t, "pi", "automatic")
	c.Learning.Timeout = "3s"
	timedPi(t, filepath.Join(c.Learning.Sources.Pi, "pi.jsonl"), "p", "2000-01-01T00:00:00Z", "2000-01-02T00:00:00Z")
	injectHarness(t, c.Learning.Executable, "sleep 2")
	s, err := RunOnce(context.Background(), c)
	if err != nil || s.BatchesCompleted != 2 || s.TurnsCompleted != 2 {
		t.Fatalf("shared timeout: %+v %v", s, err)
	}
	assertReviewed(t, c, 2)
}

func TestDrainCoverageErrorsAreNotSuccess(t *testing.T) {
	c, _ := workerFixture(t, "pi", "automatic")
	c.Learning.Sources.Codex = filepath.Join(filepath.Dir(c.Learning.Executable), "missing")
	s, err := RunOnce(context.Background(), c)
	if err == nil || s.Status != "failed" || s.CoverageErrors != 1 || s.TurnsCompleted != 1 || s.LastSuccess != "" {
		t.Fatalf("false full success: %+v %v", s, err)
	}
	assertReviewed(t, c, 1)
	if got := LoadFailureStatus(c.Learning.StateDir); got.CoverageErrors != 1 {
		t.Fatalf("lost coverage failure: %+v", got)
	}
}
