package learning

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andrewmarous/trailmix/vault"
)

func navigationResult() Result {
	return Result{Summary: "Map retry knowledge.", Changes: []Change{{Path: "navigation/index.md", Content: "# Memory map\n\n- [[procedures/retry]] — Retry applicability.\n\nSearch for topics omitted here.\n", Evidence: []Evidence{{Note: "procedures/retry.md"}}}}}
}
func TestNavigationBootstrapPolicyAndProgress(t *testing.T) {
	for _, policy := range []string{"automatic", "proposals"} {
		t.Run(policy, func(t *testing.T) {
			c, _ := workerFixture(t, "pi", policy)
			v, _ := vault.Open(c.Vault.Path)
			if err := os.Remove(filepath.Join(c.Vault.Path, "navigation/index.md")); err != nil {
				t.Fatal(err)
			}
			if err := v.Write("procedures/retry.md", []byte("# Retry\n\nRetry transient failures.\n"), false); err != nil {
				t.Fatal(err)
			}
			setResponse(t, filepath.Join(filepath.Dir(c.Learning.Executable), "navigation-response.json"), navigationResult())
			s, err := RunOnce(context.Background(), c)
			if err != nil {
				t.Fatal(err)
			}
			if policy == "proposals" {
				if s.Status != "awaiting_approval" || !strings.HasPrefix(s.NavigationID, "navigation-") {
					t.Fatalf("%+v", s)
				}
				if err = os.Remove(c.Learning.Executable); err != nil {
					t.Fatal(err)
				}
				again, e := RunOnce(context.Background(), c)
				if e != nil || again.NavigationID != s.NavigationID {
					t.Fatalf("%+v %v", again, e)
				}
				if err = ApplyProposal(c, s.NavigationID); err != nil {
					t.Fatal(err)
				}
				// Bootstrap acceptance must not inventory or review sessions.
				if err = (Store{Dir: c.Learning.StateDir}).Transaction(func(r *Registry) error {
					if len(r.Runs) != 0 || len(r.Sessions) != 1 || r.Sessions[0].ReviewedTurns != 0 {
						t.Fatalf("navigation altered review progress: %+v", r)
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			} else {
				assertReviewed(t, c, 1)
			}
			body, e := v.Read("navigation/index")
			if e != nil || string(body) != navigationResult().Changes[0].Content {
				t.Fatalf("%s %v", body, e)
			}
		})
	}
}
func TestNavigationFailureRetainsLessonsAndRetriesBeforeLearning(t *testing.T) {
	c, response := workerFixture(t, "pi", "automatic")
	setResponse(t, response, lessonResult())
	v, _ := vault.Open(c.Vault.Path)
	if err := v.Write("unrelated/note", []byte("# Unrelated\n\nDo not select for a retry-map update.\n"), false); err != nil {
		t.Fatal(err)
	}
	navResponse := filepath.Join(filepath.Dir(c.Learning.Executable), "navigation-response.json")
	bad := navigationResult()
	bad.Changes[0].Content = "# Map\n" + strings.Repeat("x", 4096)
	setResponse(t, navResponse, bad)
	s, err := RunOnce(context.Background(), c)
	if err == nil || !strings.Contains(err.Error(), "4096") || s.TurnsCompleted != 1 || s.Status != "failed" {
		t.Fatalf("%+v %v", s, err)
	}
	assertReviewed(t, c, 1)
	if body, e := v.Read("procedures/retry"); e != nil || string(body) != lessonResult().Changes[0].Content {
		t.Fatalf("%s %v", body, e)
	}
	setResponse(t, navResponse, navigationResult())
	// With no new turns, pending navigation must still finish.
	s, err = RunOnce(context.Background(), c)
	if err != nil || s.Status != "no_work" {
		t.Fatalf("%+v %v", s, err)
	}
	body, e := v.Read("navigation/index")
	if e != nil || string(body) != navigationResult().Changes[0].Content {
		t.Fatalf("%s %v", body, e)
	}
	input, e := os.ReadFile(filepath.Join(filepath.Dir(c.Learning.Executable), "input.json"))
	if e != nil {
		t.Fatal(e)
	}
	var request struct {
		Notes   map[string]string `json:"existing_notes"`
		Changed []string          `json:"changed_notes"`
	}
	if e = json.Unmarshal(input[strings.LastIndexByte(string(input), '\n')+1:], &request); e != nil {
		t.Fatal(e)
	}
	if request.Notes["procedures/retry.md"] != lessonResult().Changes[0].Content || len(request.Notes) != 2 || len(request.Changed) != 1 || request.Changed[0] != "procedures/retry.md" {
		t.Fatalf("wrong incremental evidence: %+v", request)
	}
	state, e := loadNavigationState(c)
	if e != nil || state.Dirty || state.ProposalID != "" {
		t.Fatalf("%+v %v", state, e)
	}
}
func TestNavigationEmptyVaultAndPartialPublication(t *testing.T) {
	for _, h := range []string{"pi", "codex"} {
		t.Run(h, func(t *testing.T) {
			c, _ := workerFixture(t, h, "automatic")
			c.Learning.Sources.Pi = ""
			if err := os.Remove(filepath.Join(c.Vault.Path, navigationRoot)); err != nil {
				t.Fatal(err)
			}
			result := Result{Summary: "Create empty-vault map.", Changes: []Change{{Path: navigationRoot, Content: "# Memory map\n\nUse search for topics omitted here.\n", Evidence: []Evidence{}}}}
			setResponse(t, filepath.Join(filepath.Dir(c.Learning.Executable), "navigation-response.json"), result)
			s, err := RunOnce(context.Background(), c)
			if err != nil || s.Status != "no_work" {
				t.Fatalf("%+v %v", s, err)
			}
			v, _ := vault.Open(c.Vault.Path)
			body, e := v.Read(navigationRoot)
			if e != nil || string(body) != result.Changes[0].Content {
				t.Fatalf("%s %v", body, e)
			}
		})
	}
	c, _ := workerFixture(t, "pi", "proposals")
	v, _ := vault.Open(c.Vault.Path)
	if err := v.Write("procedures/retry", []byte("# Retry\n\nVerified.\n"), false); err != nil {
		t.Fatal(err)
	}
	result := navigationResult()
	result.Changes[0].Content = "# Memory map\n\n[[navigation/retries]]\n"
	result.Changes = append(result.Changes, Change{Path: "navigation/retries.md", Content: "# Retries\n\n[[procedures/retry]] — Retry applicability.\n", Evidence: []Evidence{{Note: "procedures/retry.md"}}})
	setResponse(t, filepath.Join(filepath.Dir(c.Learning.Executable), "navigation-response.json"), result)
	s, err := BuildNavigation(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a process stopping after the first note write.
	if err = v.Write(navigationRoot, []byte(result.Changes[0].Content), true); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(c.Learning.Executable); err != nil {
		t.Fatal(err)
	}
	if err = ApplyProposal(c, s.NavigationID); err != nil {
		t.Fatal(err)
	}
	body, e := v.Read("navigation/retries")
	if e != nil || string(body) != result.Changes[1].Content {
		t.Fatalf("%s %v", body, e)
	}
	links, e := v.Navigate(navigationRoot)
	if e != nil || len(links.Outgoing) != 1 || links.Outgoing[0].Path != "navigation/retries.md" {
		t.Fatalf("%+v %v", links, e)
	}
}

func TestNavigationRejectsSymlinkDirectoriesWithoutWritingThrough(t *testing.T) {
	for _, which := range []string{"vault", "state"} {
		t.Run(which, func(t *testing.T) {
			c, _ := workerFixture(t, "pi", "automatic")
			outside := t.TempDir()
			link := filepath.Join(t.TempDir(), "linked")
			if err := os.Symlink(outside, link); err != nil {
				t.Fatal(err)
			}
			if which == "vault" {
				c.Vault.Path = link
			} else {
				c.Learning.StateDir = link
			}
			if _, err := BuildNavigation(context.Background(), c); err == nil {
				t.Fatal("symlink accepted")
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 0 {
				t.Fatalf("wrote through symlink: %v %v", entries, err)
			}
		})
	}
	c, _ := workerFixture(t, "pi", "automatic")
	setResponse(t, filepath.Join(filepath.Dir(c.Learning.Executable), "navigation-response.json"), Result{Summary: "Refresh map.", Changes: []Change{{Path: navigationRoot, Content: "# Map\n\nUse search for omitted topics.\n", Evidence: []Evidence{}}}})
	if _, err := BuildNavigation(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	v, _ := vault.Open(c.Vault.Path)
	body, err := v.Read(navigationRoot)
	if err != nil || string(body) != "# Map\n\nUse search for omitted topics.\n" {
		t.Fatalf("%s %v", body, err)
	}
}

func TestNavigationTimeoutRetainsCauseAndPendingWork(t *testing.T) {
	c, _ := workerFixture(t, "pi", "automatic")
	c.Learning.Timeout = "1s"
	if err := os.WriteFile(c.Learning.Executable, []byte("#!/bin/sh\nsleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	s, err := BuildNavigation(context.Background(), c)
	if err == nil || !strings.Contains(err.Error(), "deadline exceeded") || s.Status != "failed" || s.NavigationID == "" {
		t.Fatalf("%+v %v", s, err)
	}
	// A failed invocation must not discard its pending maintenance.
	if err = DiscardNavigation(c, s.NavigationID); err != nil {
		t.Fatal(err)
	}
	state, e := loadNavigationState(c)
	if e != nil || !state.Dirty {
		t.Fatalf("%+v %v", state, e)
	}
}

func TestNavigationFailureStopsLaterBatches(t *testing.T) {
	c, response := workerFixture(t, "pi", "automatic")
	timedPi(t, filepath.Join(c.Learning.Sources.Pi, "pi.jsonl"), "p", "2000-01-01T00:00:00Z", "2000-01-02T00:00:00Z")
	setResponse(t, response, lessonResult())
	bad := navigationResult()
	bad.Changes[0].Content = "# Memory map\n\n[[missing/page]]\n"
	setResponse(t, filepath.Join(filepath.Dir(c.Learning.Executable), "navigation-response.json"), bad)
	s, err := RunOnce(context.Background(), c)
	if err == nil || s.BatchesCompleted != 1 || s.TurnsCompleted != 1 || s.TurnsRemaining != 1 {
		t.Fatalf("%+v %v", s, err)
	}
	assertReviewed(t, c, 1)
	// It must try pending navigation before beginning the second learning batch.
	s, err = RunOnce(context.Background(), c)
	if err == nil || s.RunID != "" || s.TurnsCompleted != 0 {
		t.Fatalf("%+v %v", s, err)
	}
	assertReviewed(t, c, 1)
}

func TestNavigationApprovalBlocksLaterBatches(t *testing.T) {
	c, response := workerFixture(t, "pi", "proposals")
	timedPi(t, filepath.Join(c.Learning.Sources.Pi, "pi.jsonl"), "p", "2000-01-01T00:00:00Z", "2000-01-02T00:00:00Z")
	setResponse(t, response, lessonResult())
	s, err := RunOnce(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if err = ApplyProposal(c, s.RunID); err != nil {
		t.Fatal(err)
	}
	setResponse(t, filepath.Join(filepath.Dir(c.Learning.Executable), "navigation-response.json"), navigationResult())
	s, err = RunOnce(context.Background(), c)
	if err != nil || s.Status != "awaiting_approval" || s.NavigationID == "" {
		t.Fatalf("%+v %v", s, err)
	}
	assertReviewed(t, c, 1)
	if err = ApplyProposal(c, s.NavigationID); err != nil {
		t.Fatal(err)
	}
	assertReviewed(t, c, 1)
	// No-op learning on the remaining turn completes without creating a proposal.
	c.Learning.Publication = "automatic"
	setResponse(t, response, Result{Summary: "No reusable lesson.", Changes: []Change{}})
	s, err = RunOnce(context.Background(), c)
	if err != nil || s.Status != "completed" || s.TurnsCompleted != 1 {
		t.Fatalf("%+v %v", s, err)
	}
	assertReviewed(t, c, 2)
}

func TestNavigationRejectsUnsafeChangesAndChangedSources(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Result)
		want   string
	}{
		{"outside", func(r *Result) { r.Changes[0].Path = "procedures/retry.md" }, "only change navigation/"},
		{"topic size", func(r *Result) {
			r.Changes = append(r.Changes, Change{Path: "navigation/topic.md", Content: "# Topic\n" + strings.Repeat("x", 8192), Evidence: []Evidence{{Note: "procedures/retry.md"}}})
		}, "8192"},
		{"evidence", func(r *Result) { r.Changes[0].Evidence = []Evidence{{Note: "missing.md"}} }, "invalid navigation evidence"},
		{"broken link", func(r *Result) { r.Changes[0].Content = "# Map\n\n[[missing/page]]\n" }, "links invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := workerFixture(t, "pi", "automatic")
			v, _ := vault.Open(c.Vault.Path)
			if err := v.Write("procedures/retry", []byte("# Retry\n\nVerified.\n"), false); err != nil {
				t.Fatal(err)
			}
			result := navigationResult()
			tc.mutate(&result)
			setResponse(t, filepath.Join(filepath.Dir(c.Learning.Executable), "navigation-response.json"), result)
			_, err := BuildNavigation(context.Background(), c)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%v", err)
			}
			body, e := v.Read("procedures/retry")
			if e != nil || string(body) != "# Retry\n\nVerified.\n" {
				t.Fatalf("%s %v", body, e)
			}
		})
	}
	c, _ := workerFixture(t, "pi", "proposals")
	v, _ := vault.Open(c.Vault.Path)
	if err := v.Write("procedures/retry", []byte("# Retry\n\nBefore.\n"), false); err != nil {
		t.Fatal(err)
	}
	setResponse(t, filepath.Join(filepath.Dir(c.Learning.Executable), "navigation-response.json"), navigationResult())
	s, err := BuildNavigation(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if err = v.Write("procedures/retry", []byte("# Retry\n\nAfter.\n"), true); err != nil {
		t.Fatal(err)
	}
	if err = ApplyProposal(c, s.NavigationID); err == nil || !strings.Contains(err.Error(), "source changed") {
		t.Fatalf("%v", err)
	}
	body, e := v.Read("navigation/index")
	if e != nil || string(body) != "# Memory map\n\nUse search to find relevant notes.\n" {
		t.Fatalf("%s %v", body, e)
	}
	if err = DiscardNavigation(c, s.NavigationID); err != nil {
		t.Fatal(err)
	}
	if err = ApplyProposal(c, s.NavigationID); err == nil {
		t.Fatal("discarded proposal accepted")
	}
	// A scheduled invocation regenerates pending navigation from the newer source.
	s, err = RunOnce(context.Background(), c)
	if err != nil || s.Status != "awaiting_approval" {
		t.Fatalf("%+v %v", s, err)
	}
	if err = ApplyProposal(c, s.NavigationID); err != nil {
		t.Fatal(err)
	}
	body, e = v.Read("procedures/retry")
	if e != nil || string(body) != "# Retry\n\nAfter.\n" {
		t.Fatalf("%s %v", body, e)
	}
}
