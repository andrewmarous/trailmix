package learning

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/andrewmarous/trailmix/config"
	"github.com/andrewmarous/trailmix/vault"
)

func consolidationFixture(t *testing.T, harness, policy string) (config.Config, string) {
	t.Helper()
	c, _ := workerFixture(t, harness, policy)
	if err := (Store{Dir: c.Learning.StateDir}).Transaction(func(r *Registry) error { return r.Scan(ConfiguredSources(c.Learning.Sources)) }); err != nil {
		t.Fatal(err)
	}
	v, _ := vault.Open(c.Vault.Path)
	if err := v.Write("procedures/retry.md", []byte("# Retry\n\nIncorrect broad rule.\n\nSession `pi/p`, turn 1.\n"), false); err != nil {
		t.Fatal(err)
	}
	return c, filepath.Join(filepath.Dir(c.Learning.Executable), "consolidation-response.json")
}
func correctedConsolidation() Result {
	r := lessonResult()
	r.Summary = "Narrow rule using original evidence."
	r.Completed = []string{"procedures/retry.md"}
	return r
}

func TestConsolidationPublishesAndRefreshesMapsWithoutReviewProgress(t *testing.T) {
	for _, h := range []string{"pi", "codex"} {
		t.Run(h, func(t *testing.T) {
			c, response := consolidationFixture(t, h, "automatic")
			setResponse(t, response, correctedConsolidation())
			status, err := Consolidate(context.Background(), c, nil)
			if err != nil || status.Status != "completed" || !status.ConsolidationCompleted {
				t.Fatalf("%+v %v", status, err)
			}
			body, err := os.ReadFile(filepath.Join(c.Vault.Path, "procedures/retry.md"))
			if err != nil || string(body) != lessonResult().Changes[0].Content {
				t.Fatalf("published %q %v", body, err)
			}
			assertReviewed(t, c, 0)
			s, err := loadConsolidationState(c)
			if err != nil || s.Active != nil || len(s.Pending) != 0 || s.SweepCursor != "procedures/retry.md" {
				t.Fatalf("completion: %+v %v", s, err)
			}
			nav, err := loadNavigationState(c)
			if err != nil || nav.Dirty {
				t.Fatalf("navigation not finished: %+v %v", nav, err)
			}
			p, err := loadProposal(c.Learning.StateDir, status.ConsolidationID)
			if err != nil || !p.Completed {
				t.Fatalf("proposal not durable: %+v %v", p, err)
			}
			if err = ApplyProposal(c, status.ConsolidationID); err != nil {
				t.Fatalf("completion not idempotent: %v", err)
			}
			args, _ := os.ReadFile(filepath.Join(filepath.Dir(c.Learning.Executable), "args"))
			// The final request is navigation. The consolidation input itself records
			// the explicit selected set and not arbitrary transcripts.
			input, _ := os.ReadFile(filepath.Join(filepath.Dir(c.Learning.Executable), "consolidation-input.json"))
			if !strings.Contains(string(input), `"selected":{"procedures/retry.md":`) {
				t.Fatalf("wrong selected input: %s; args %s", input, args)
			}
		})
	}
}

func TestPiConsolidationEnablesOnlyReadTools(t *testing.T) {
	c, _ := workerFixture(t, "pi", "automatic")
	dir := t.TempDir()
	if _, err := invokeReviewIn(context.Background(), c, []byte(`{"task":"consolidation","selected":{"note.md":"abc"}}`), consolidationInstructions, consolidationSchema, dir); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(filepath.Join(filepath.Dir(c.Learning.Executable), "args"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(args), "--no-tools") || !strings.Contains(string(args), "--tools\nread,grep,find,ls\n") {
		t.Fatalf("consolidation must expose read-only tools: %s", args)
	}
}

func TestConsolidationRepairsGenerationErrors(t *testing.T) {
	c, response := consolidationFixture(t, "pi", "automatic")
	bad := correctedConsolidation()
	bad.Changes[0].Content = "# Retry\nDropped provenance."
	setResponse(t, response, bad)
	good := response + ".good"
	setResponse(t, good, correctedConsolidation())
	script, err := os.ReadFile(c.Learning.Executable)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(filepath.Dir(response), "attempted")
	old := "result='" + response + "'"
	replacement := old + "\n if [ -f '" + marker + "' ]; then result='" + good + "'; else touch '" + marker + "'; fi"
	if err = os.WriteFile(c.Learning.Executable, []byte(strings.Replace(string(script), old, replacement, 1)), 0700); err != nil {
		t.Fatal(err)
	}
	status, err := Consolidate(context.Background(), c, nil)
	if err != nil {
		t.Fatal(err)
	}
	input, err := os.ReadFile(filepath.Join(filepath.Dir(response), "consolidation-input.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(input), "drops provenance") || !strings.Contains(string(input), `"previous":`) {
		t.Fatalf("repair lacked actionable context: %s", input)
	}
	p, err := loadProposal(c.Learning.StateDir, status.ConsolidationID)
	if err != nil || p.Result.Summary != "Narrow rule using original evidence." {
		t.Fatalf("wrong repaired output: %+v %v", p, err)
	}
	body, _ := os.ReadFile(filepath.Join(c.Vault.Path, "procedures/retry.md"))
	if string(body) != lessonResult().Changes[0].Content {
		t.Fatalf("repair not published: %s", body)
	}
}

func TestConsolidationExhaustionAndDiscardPreservePendingWork(t *testing.T) {
	c, response := consolidationFixture(t, "pi", "automatic")
	bad := correctedConsolidation()
	bad.Completed = []string{"not-selected.md"}
	setResponse(t, response, bad)
	status, err := Consolidate(context.Background(), c, nil)
	if err == nil || !strings.Contains(err.Error(), "exhausted repair attempts") {
		t.Fatalf("%+v %v", status, err)
	}
	s, err := loadConsolidationState(c)
	if err != nil || s.Active.Attempts != 3 || len(s.Pending) != 1 {
		t.Fatalf("lost failed run: %+v %v", s, err)
	}
	body, _ := os.ReadFile(filepath.Join(c.Vault.Path, "procedures/retry.md"))
	if !strings.Contains(string(body), "Incorrect broad rule") {
		t.Fatalf("invalid output published: %s", body)
	}
	assertReviewed(t, c, 0)
	setResponse(t, response, correctedConsolidation())
	if _, err = Consolidate(context.Background(), c, nil); err == nil {
		t.Fatal("reset bounded attempts without disposition")
	}
	if err = DiscardConsolidation(c, status.ConsolidationID); err != nil {
		t.Fatal(err)
	}
	if _, err = Consolidate(context.Background(), c, nil); err != nil {
		t.Fatal(err)
	}
	body, _ = os.ReadFile(filepath.Join(c.Vault.Path, "procedures/retry.md"))
	if string(body) != lessonResult().Changes[0].Content {
		t.Fatalf("discard lost queued work: %s", body)
	}
}

func TestConsolidationApprovalRevalidatesEvidenceAndRecoversPartialWrites(t *testing.T) {
	c, response := consolidationFixture(t, "pi", "proposals")
	result := correctedConsolidation()
	result.Changes = append(result.Changes, Change{Path: "procedures/details.md", Content: "# Details\n\n[Evidence](trailmix://session/pi/p?turns=1)\n", Evidence: []Evidence{{SessionID: "pi/p", Turn: 1}}})
	setResponse(t, response, result)
	status, err := Consolidate(context.Background(), c, nil)
	if err != nil || status.Status != "awaiting_approval" {
		t.Fatalf("%+v %v", status, err)
	}
	// Simulate the first write succeeding before interruption.
	v, _ := vault.Open(c.Vault.Path)
	if err = v.Write(result.Changes[0].Path, []byte(result.Changes[0].Content), true); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(c.Learning.Executable); err != nil {
		t.Fatal(err)
	}
	if err = ApplyProposal(c, status.ConsolidationID); err != nil {
		t.Fatal(err)
	}
	b, err := v.Read("procedures/details")
	if err != nil || string(b) != result.Changes[1].Content {
		t.Fatalf("recovery: %s %v", b, err)
	}
	assertReviewed(t, c, 0)
}

func TestConsolidationSourceLossIsNotRepaired(t *testing.T) {
	c, response := consolidationFixture(t, "pi", "automatic")
	setResponse(t, response, correctedConsolidation())
	script, err := os.ReadFile(c.Learning.Executable)
	if err != nil {
		t.Fatal(err)
	}
	marker := "result='" + response + "'"
	replacement := marker + "\n rm '" + filepath.Join(c.Learning.Sources.Pi, "pi.jsonl") + "'"
	if err = os.WriteFile(c.Learning.Executable, []byte(strings.Replace(string(script), marker, replacement, 1)), 0700); err != nil {
		t.Fatal(err)
	}
	status, err := Consolidate(context.Background(), c, nil)
	if err == nil {
		t.Fatal("published after evidence disappeared")
	}
	s, loadErr := loadConsolidationState(c)
	if loadErr != nil || s.Active == nil || s.Active.Attempts != 1 {
		t.Fatalf("external failure retried: %+v %v (%v)", s, loadErr, err)
	}
	if status.ConsolidationCompleted {
		t.Fatal("reported incomplete review as completed")
	}
	body, _ := os.ReadFile(filepath.Join(c.Vault.Path, "procedures/retry.md"))
	if !strings.Contains(string(body), "Incorrect broad rule") {
		t.Fatalf("published: %s", body)
	}
}

func TestNavigationCannotRunAheadOfPendingConsolidation(t *testing.T) {
	c, response := consolidationFixture(t, "pi", "proposals")
	setResponse(t, response, correctedConsolidation())
	status, err := Consolidate(context.Background(), c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = BuildNavigation(context.Background(), c); err == nil || !strings.Contains(err.Error(), "finish or discard consolidation") {
		t.Fatalf("navigation bypassed pending run: %v", err)
	}
	resumed, err := Consolidate(context.Background(), c, nil)
	if err != nil || resumed.ConsolidationID != status.ConsolidationID || resumed.Status != "awaiting_approval" {
		t.Fatalf("resume: %+v %v", resumed, err)
	}
}

func TestConsolidationRejectsChangedSourcesBeforeAnyWrite(t *testing.T) {
	for _, source := range []string{"note", "session"} {
		t.Run(source, func(t *testing.T) {
			c, response := consolidationFixture(t, "pi", "proposals")
			setResponse(t, response, correctedConsolidation())
			status, err := Consolidate(context.Background(), c, nil)
			if err != nil {
				t.Fatal(err)
			}
			if source == "note" {
				v, _ := vault.Open(c.Vault.Path)
				err = v.Write("procedures/retry.md", []byte("# Concurrent edit\n"), true)
			} else {
				err = os.Remove(filepath.Join(c.Learning.Sources.Pi, "pi.jsonl"))
			}
			if err != nil {
				t.Fatal(err)
			}
			err = ApplyProposal(c, status.ConsolidationID)
			if err == nil {
				t.Fatal("published stale proposal")
			}
			body, _ := os.ReadFile(filepath.Join(c.Vault.Path, "procedures/retry.md"))
			if string(body) == lessonResult().Changes[0].Content {
				t.Fatalf("wrote invalidated proposal: %s", body)
			}
			assertReviewed(t, c, 0)
		})
	}
}

func TestConsolidationQueueBatchesAndDetectsManualChanges(t *testing.T) {
	c, _ := workerFixture(t, "pi", "automatic")
	v, _ := vault.Open(c.Vault.Path)
	for i := 0; i < 7; i++ {
		if err := v.Write(fmt.Sprintf("topic/%d.md", i), []byte(fmt.Sprintf("# Topic %d\n", i)), false); err != nil {
			t.Fatal(err)
		}
	}
	for batch, want := range [][]string{{"topic/4.md", "topic/5.md", "topic/6.md"}, {}} {
		if _, err := Consolidate(context.Background(), c, nil); err != nil {
			t.Fatal(err)
		}
		s, err := loadConsolidationState(c)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(s.Pending, want) {
			t.Fatalf("batch %d: pending %v, want %v", batch, s.Pending, want)
		}
	}
	if err := v.Write("topic/6.md", []byte("# Changed manually\n"), true); err != nil {
		t.Fatal(err)
	}
	if _, err := Consolidate(context.Background(), c, nil); err != nil {
		t.Fatal(err)
	}
	s, err := loadConsolidationState(c)
	if err != nil || s.Known["topic/6.md"] != noteHash("# Changed manually\n") || len(s.Pending) != 0 {
		t.Fatalf("manual edit not processed: %+v %v", s, err)
	}
	assertReviewed(t, c, 0)
}

func TestConsolidationReportsOversizedNotesWithoutBlockingSmallerWork(t *testing.T) {
	c, _ := workerFixture(t, "pi", "automatic")
	c.Learning.MaxInputBytes = 4096
	v, _ := vault.Open(c.Vault.Path)
	for path, body := range map[string]string{"a-big.md": "# Big\n" + strings.Repeat("x", 2200), "b-small.md": "# Small\n"} {
		if err := v.Write(path, []byte(body), false); err != nil {
			t.Fatal(err)
		}
	}
	status, err := Consolidate(context.Background(), c, nil)
	if err != nil || !status.ConsolidationCompleted || !reflect.DeepEqual(status.ConsolidationOversized, []string{"a-big.md"}) {
		t.Fatalf("oversized note blocked smaller review: %+v %v", status, err)
	}
	s, err := loadConsolidationState(c)
	if err != nil || !reflect.DeepEqual(s.Pending, []string{"a-big.md"}) {
		t.Fatalf("lost oversized work: %+v %v", s, err)
	}
}

func TestConsolidationBundleContainsOnlyCitedTurns(t *testing.T) {
	c, _ := consolidationFixture(t, "pi", "automatic")
	timedPi(t, filepath.Join(c.Learning.Sources.Pi, "pi.jsonl"), "p", "2000-01-01T00:00:00Z", "2000-01-02T00:00:00Z", "2000-01-03T00:00:00Z")
	notes := map[string]string{"lesson.md": "# Lesson\n[Evidence](trailmix://session/pi/p?turns=1,3)\n"}
	dir, hashes, err := consolidationBundle(c, notes)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if len(hashes) != 1 {
		t.Fatalf("unexpected source count: %v", hashes)
	}
	b, err := os.ReadFile(filepath.Join(dir, "evidence/index.json"))
	if err != nil {
		t.Fatal(err)
	}
	var index map[string]struct {
		File string `json:"file"`
	}
	if err = json.Unmarshal(b, &index); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(filepath.Join(dir, index["trailmix://session/pi/p?turns=1,3"].File))
	if err != nil {
		t.Fatal(err)
	}
	var turns []Turn
	if err = json.Unmarshal(b, &turns); err != nil {
		t.Fatal(err)
	}
	if len(turns) != 2 || turns[1].CompletedAt != "2000-01-03T00:00:00Z" {
		t.Fatalf("included unrelated turns: %s", b)
	}
}
