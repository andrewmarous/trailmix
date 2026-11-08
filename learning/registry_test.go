package learning

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestResumedSessionSnapshotAndArchive(t *testing.T) {
	root := t.TempDir()
	pi := filepath.Join(root, "pi")
	active := filepath.Join(root, "codex")
	archive := filepath.Join(root, "archive")
	path := filepath.Join(pi, "one.jsonl")
	lines := piLines("one")
	writeLines(t, path, lines...)
	codexPath := filepath.Join(active, "two.jsonl")
	writeLines(t, codexPath, codexLines("two")...)
	r := Registry{Version: 2, LearningStartAt: "1999-01-01T00:00:00Z"}
	sources := []Source{{Harness: "pi", Root: pi}, {Harness: "codex", Root: active}, {Harness: "codex", Root: archive}}
	if err := r.Scan(sources); err != nil {
		t.Fatal(err)
	}
	run, err := r.Begin()
	if err != nil {
		t.Fatal(err)
	}
	id := run.ID
	if len(run.Ranges) != 2 {
		t.Fatalf("missing harness: %+v", run)
	}
	if _, err = r.Begin(); err == nil {
		t.Fatal("overlapping run accepted")
	}
	packets, err := r.Read(id)
	if err != nil || len(packets) != 2 {
		t.Fatalf("bad packets %v", err)
	}
	lines = append(lines, piMessage("u2", "a1", "user", "more work", ""), piMessage("a2", "u2", "assistant", "done", "stop"))
	writeLines(t, path, lines...)
	if err = r.Scan(nil); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Close(id, "no lesson", false); err != nil {
		t.Fatal(err)
	}
	s := r.session("pi/one")
	if s.CompletedTurns != 2 || s.ReviewedTurns != 1 || s.PendingTurns != 1 {
		t.Fatalf("new turn swallowed: %+v", s)
	}
	// Archiving changes only location; the native identity and watermark survive.
	if err = os.Mkdir(archive, 0700); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(archive, "renamed.jsonl")
	if err = os.Rename(codexPath, dest); err != nil {
		t.Fatal(err)
	}
	if err = r.Scan(nil); err != nil {
		t.Fatal(err)
	}
	c := r.session("codex/two")
	if c.ReviewedTurns != 1 || c.Path != dest || c.Error != "" {
		t.Fatalf("archive lost state: %+v", c)
	}
	next, err := r.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Ranges) != 1 || next.Ranges[0].FromTurn != 2 {
		t.Fatalf("bad resume %+v", next)
	}
	nextID := next.ID
	packets, err = r.Read(nextID)
	if err != nil {
		t.Fatal(err)
	}
	if len(packets[0].Context) != 1 || len(packets[0].Turns) != 1 {
		t.Fatalf("bad review context %+v", packets)
	}
	if _, err = r.Close(nextID, "", false); err == nil {
		t.Fatal("empty outcome accepted")
	}
	if _, err = r.Close(nextID, "", true); err != nil {
		t.Fatal(err)
	}
	if s.ReviewedTurns != 1 {
		t.Fatal("abort advanced count")
	}
}
func TestRewriteAndBranchRequireReset(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "pi.jsonl")
	lines := piLines("p")
	writeLines(t, path, lines...)
	r := Registry{Version: 2, LearningStartAt: "1999-01-01T00:00:00Z"}
	_ = r.Scan([]Source{{Harness: "pi", Root: root}})
	run, err := r.Begin()
	if err != nil {
		t.Fatal(err)
	}
	id := run.ID
	// Same turn count, different branch. Count-only tracking must not accept it.
	lines = append(lines, piMessage("other-user", "", "user", "different", ""), piMessage("other-answer", "other-user", "assistant", "different done", "stop"))
	writeLines(t, path, lines...)
	if _, err = r.Close(id, "reviewed", false); err == nil {
		t.Fatal("changed snapshot accepted")
	}
	if _, err = r.Close(id, "", true); err != nil {
		t.Fatal(err)
	}
	_ = r.Scan(nil)
	run, err = r.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.Close(run.ID, "reviewed", false); err != nil {
		t.Fatal(err)
	}
	writeLines(t, path, piLines("p")...)
	_ = r.Scan(nil)
	if r.session("pi/p").Error == "" {
		t.Fatal("changed reviewed branch not flagged")
	}
	if _, err = r.Begin(); err == nil {
		t.Fatal("blocked session reviewed")
	}
	if err = r.Reset("pi/p"); err != nil {
		t.Fatal(err)
	}
	_ = r.Scan(nil)
	if _, err = r.Begin(); err != nil {
		t.Fatal(err)
	}
}
func TestCoverageAndDuplicates(t *testing.T) {
	root := t.TempDir()
	writeLines(t, filepath.Join(root, "a.jsonl"), piLines("p")...)
	writeLines(t, filepath.Join(root, "b.jsonl"), piHeader("p"), piMessage("u", "", "user", "other", ""), piMessage("a", "u", "assistant", "other", "stop"))
	writeLines(t, filepath.Join(root, "broken.jsonl"), "not json")
	r := Registry{Version: 2, LearningStartAt: "1999-01-01T00:00:00Z"}
	if err := r.Scan([]Source{{Harness: "pi", Root: root}, {Harness: "codex", Root: filepath.Join(root, "missing")}}); err != nil {
		t.Fatal(err)
	}
	if r.Sources[0].Status != "partial" || r.Sources[1].Status != "unavailable" || r.session("pi/p").Error == "" {
		t.Fatalf("coverage hidden: %+v", r)
	}
}
func TestStoreConcurrentUpdatesAndCorruption(t *testing.T) {
	store := Store{Dir: filepath.Join(t.TempDir(), "state")}
	var wg sync.WaitGroup
	errors := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errors <- store.Transaction(func(r *Registry) error { r.Sessions = append(r.Sessions, Session{ID: fmt.Sprint(i)}); return nil })
		}(i)
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Transaction(func(r *Registry) error {
		if len(r.Sessions) != 10 {
			t.Fatalf("lost updates: %d", len(r.Sessions))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.Dir, "registry.json")
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Transaction(func(r *Registry) error { return nil }); err == nil {
		t.Fatal("corrupt registry accepted")
	}
	b, _ := os.ReadFile(path)
	if string(b) != "broken" {
		t.Fatal("corrupt registry overwritten")
	}
}
