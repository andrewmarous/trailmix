package learning

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func timedPi(t *testing.T, path, id string, dates ...string) {
	t.Helper()
	lines := []string{piHeader(id)}
	parent := ""
	for i, date := range dates {
		user, answer := fmt.Sprintf("u%d", i+1), fmt.Sprintf("a%d", i+1)
		lines = append(lines, piMessage(user, parent, "user", "request", ""))
		var record map[string]any
		_ = json.Unmarshal([]byte(piMessage(answer, user, "assistant", "done", "stop")), &record)
		record["timestamp"] = date
		b, _ := json.Marshal(record)
		lines = append(lines, string(b))
		parent = answer
	}
	writeLines(t, path, lines...)
}
func TestCutoffResumptionAndEarlierHistory(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "session.jsonl")
	dates := []string{"2026-01-01T00:00:00Z", "2026-01-02T00:00:00Z", "2026-01-03T00:00:00Z"}
	timedPi(t, path, "p", dates...)
	r := Registry{Version: 2, LearningStartAt: "2026-01-03T00:00:00Z"}
	if err := r.Scan([]Source{{Harness: "pi", Root: root}}); err != nil {
		t.Fatal(err)
	}
	assertCounts := func(before, reviewed, pending int) {
		t.Helper()
		s := r.session("pi/p")
		if s.BeforeCutoffTurns != before || s.ReviewedTurns != reviewed || s.PendingTurns != pending || s.Error != "" {
			t.Fatalf("bad counts: %+v", s)
		}
	}
	assertCounts(2, 0, 1)
	run, err := r.Begin()
	if err != nil {
		t.Fatal(err)
	}
	id := run.ID
	if len(run.Ranges) != 1 || run.Ranges[0].FromTurn != 3 {
		t.Fatalf("included backlog: %+v", run)
	}
	if err = r.SetStartAt("1999-01-01T00:00:00Z"); err == nil {
		t.Fatal("cutoff changed during active review")
	}
	dates = append(dates, "2026-01-04T00:00:00Z")
	timedPi(t, path, "p", dates...)
	_ = r.Scan(nil)
	if _, err = r.Close(id, "No reusable lesson.", false); err != nil {
		t.Fatal(err)
	}
	assertCounts(2, 1, 1)
	if err = r.SetStartAt("1999-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	assertCounts(0, 1, 3)
	run, err = r.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Ranges) != 2 || run.Ranges[0].FromTurn != 1 || run.Ranges[0].ThroughTurn != 2 || run.Ranges[1].FromTurn != 4 {
		t.Fatalf("lost prior progress: %+v", run)
	}
	if _, err = r.Close(run.ID, "Reviewed historical and resumed turns.", false); err != nil {
		t.Fatal(err)
	}
	assertCounts(0, 4, 0)
	if err = r.SetStartAt("2026-01-03T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	assertCounts(2, 2, 0)
	if err = r.SetStartAt("1999-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	assertCounts(0, 4, 0)
}
func TestCutoffUsesCompletionNotSessionCreation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "codex.jsonl")
	lines := codexLines("c")
	var last map[string]any
	_ = json.Unmarshal([]byte(lines[len(lines)-1]), &last)
	last["timestamp"] = "2026-01-03T00:00:00Z"
	b, _ := json.Marshal(last)
	lines[len(lines)-1] = string(b)
	writeLines(t, path, lines...)
	r := Registry{Version: 2, LearningStartAt: "2026-01-02T00:00:00Z"}
	if err := r.Scan([]Source{{Harness: "codex", Root: root}}); err != nil {
		t.Fatal(err)
	}
	if s := r.session("codex/c"); s.CompletedTurns != 1 || s.BeforeCutoffTurns != 0 || s.PendingTurns != 1 {
		t.Fatalf("resumed old session excluded: %+v", s)
	}
}
func TestCompletionTimestampRecheckedBeforeAdvancing(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "pi.jsonl")
	timedPi(t, path, "p", "2026-01-03T00:00:00Z")
	r := Registry{Version: 2, LearningStartAt: "2026-01-02T00:00:00Z"}
	if err := r.Scan([]Source{{Harness: "pi", Root: root}}); err != nil {
		t.Fatal(err)
	}
	run, err := r.Begin()
	if err != nil {
		t.Fatal(err)
	}
	timedPi(t, path, "p", "2026-01-01T00:00:00Z")
	if _, err = r.Close(run.ID, "reviewed", false); err == nil {
		t.Fatal("changed timestamp bypassed cutoff")
	}
	if r.Sessions[0].ReviewedTurns != 0 {
		t.Fatal("failed completion advanced progress")
	}
}

func TestInitializeRejectsActiveLegacyReview(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := Store{Dir: filepath.Join(t.TempDir(), "state")}
	if err := store.Transaction(func(r *Registry) error { r.Runs = []Run{{ID: "old-active-run"}}; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Initialize(); err == nil {
		t.Fatal("initialized cutoff under an old active review")
	}
	if err := store.Transaction(func(r *Registry) error { _, err := r.Close("old-active-run", "", true); return err }); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Initialize(); err != nil {
		t.Fatal(err)
	}
}

func TestInitializeOnceUnderConcurrentCalls(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := Store{Dir: filepath.Join(t.TempDir(), "state")}
	before := time.Now()
	results := make(chan string, 10)
	errors := make(chan error, 10)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); start, err := store.Initialize(); results <- start; errors <- err }()
	}
	wg.Wait()
	close(results)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	first := ""
	for value := range results {
		if first == "" {
			first = value
		} else if value != first {
			t.Fatal("initializations chose different timestamps")
		}
	}
	timestamp, err := time.Parse(time.RFC3339Nano, first)
	if err != nil {
		t.Fatal(err)
	}
	if timestamp.Before(before) || timestamp.After(time.Now()) {
		t.Fatal("cutoff is not initialization time")
	}
	if err = store.Transaction(func(r *Registry) error { return r.SetStartAt("2001-01-01T00:00:00-05:00") }); err != nil {
		t.Fatal(err)
	}
	again, err := store.Initialize()
	if err != nil {
		t.Fatal(err)
	}
	if again != "2001-01-01T05:00:00Z" {
		t.Fatalf("init reset override: %s", again)
	}
}
func TestMissingCutoffRequiresInitAndLegacyMigration(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "source", "pi.jsonl")
	timedPi(t, path, "p", "2000-01-01T00:00:00Z")
	tr, err := parse(path, "pi")
	if err != nil {
		t.Fatal(err)
	}
	store := Store{Dir: filepath.Join(root, "state")}
	if err = os.MkdirAll(store.Dir, 0700); err != nil {
		t.Fatal(err)
	}
	old := Registry{Version: 1, Sources: []Source{{Harness: "pi", Root: filepath.Dir(path)}}, Sessions: []Session{{ID: "pi/p", NativeID: "p", Harness: "pi", Path: path, CompletedTurns: 1, ReviewedTurns: 1, ReviewedDigest: digest(tr.Turns)}}}
	b, _ := json.Marshal(old)
	registryPath := filepath.Join(store.Dir, "registry.json")
	if err = os.WriteFile(registryPath, b, 0600); err != nil {
		t.Fatal(err)
	}
	if err = store.Transaction(func(r *Registry) error { return r.Scan(nil) }); err == nil {
		t.Fatal("scan initialized cutoff implicitly")
	}
	unchanged, _ := os.ReadFile(registryPath)
	if string(unchanged) != string(b) {
		t.Fatal("failed scan changed state")
	}
	start, err := store.Initialize()
	if err != nil {
		t.Fatal(err)
	}
	if start == "" {
		t.Fatal("missing initialization timestamp")
	}
	if err = store.Transaction(func(r *Registry) error {
		if r.Version != 2 || len(r.Sessions[0].ReviewedSpans) != 1 {
			t.Fatal("legacy progress not migrated")
		}
		if r.Sessions[0].ReviewedTurns != 0 || r.Sessions[0].BeforeCutoffTurns != 1 {
			t.Fatal("old reviewed turn not separated from cutoff")
		}
		if err := r.SetStartAt("1999-01-01T00:00:00Z"); err != nil {
			return err
		}
		if r.Sessions[0].ReviewedTurns != 1 || r.Sessions[0].PendingTurns != 0 {
			t.Fatal("legacy review lost after earlier cutoff")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
