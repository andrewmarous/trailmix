package learning

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andrewmarous/trailmix/config"
	"github.com/andrewmarous/trailmix/vault"
	"github.com/gofrs/flock"
)

func workerFixture(t *testing.T, harness, policy string) (config.Config, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	c := config.Defaults(home)
	c.Vault.Path = filepath.Join(home, "vault")
	c.Learning.Harness = harness
	c.Learning.Publication = policy
	c.Learning.MaxTurns = 1
	v, _ := vault.Open(c.Vault.Path)
	if err := v.Init(); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(home, "source")
	timedPi(t, filepath.Join(source, "pi.jsonl"), "p", "2000-01-01T00:00:00Z")
	c.Learning.Sources = config.Sources{Pi: source}
	store := Store{Dir: c.Learning.StateDir}
	if _, err := store.Initialize(); err != nil {
		t.Fatal(err)
	}
	if err := store.Transaction(func(r *Registry) error {
		r.Sources = []Source{{Harness: "pi", Root: source}}
		return r.SetStartAt("1999-01-01T00:00:00Z")
	}); err != nil {
		t.Fatal(err)
	}
	resultFile := filepath.Join(home, "response.json")
	c.Learning.Executable = filepath.Join(home, harness)
	script := `#!/bin/sh
if [ "$1" = "--help" ] || [ "$2" = "--help" ]; then
 echo '--print --no-tools --no-session --no-extensions --no-context-files --no-mcp --no-skills --no-prompt-templates --no-themes --no-approve --ephemeral --output-schema --output-last-message --sandbox --skip-git-repo-check --disable'
 exit 0
fi
printf '%s\n' "$@" > '` + filepath.Join(home, "args") + `'
output=''
while [ "$#" -gt 0 ]; do
 if [ "$1" = "--output-last-message" ]; then shift; output="$1"; fi
 shift
done
cat > '` + filepath.Join(home, "input.json") + `'
if [ -n "$output" ]; then cp '` + resultFile + `' "$output"; else cat '` + resultFile + `'; fi
`
	if err := os.WriteFile(c.Learning.Executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	setResponse(t, resultFile, Result{Summary: "No reusable lesson.", Changes: []Change{}})
	return c, resultFile
}
func setResponse(t *testing.T, path string, result Result) {
	t.Helper()
	b, _ := json.Marshal(result)
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
}
func assertReviewed(t *testing.T, c config.Config, count int) {
	t.Helper()
	if err := (Store{Dir: c.Learning.StateDir}).Transaction(func(r *Registry) error {
		if r.Sessions[0].ReviewedTurns != count {
			t.Fatalf("wrong progress: %+v", r.Sessions[0])
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
func lessonResult() Result {
	return Result{Summary: "Verified retry procedure.", Changes: []Change{{Path: "procedures/retry.md", Content: "# Retry transient failures\n\nRetry only verified transient failures.\n", Evidence: []Evidence{{SessionID: "pi/p", Turn: 1}}}}}
}
func TestWorkerDrainsAllSessionsInBatches(t *testing.T) {
	c, _ := workerFixture(t, "pi", "automatic")
	timedPi(t, filepath.Join(c.Learning.Sources.Pi, "pi.jsonl"), "p", "2000-01-01T00:00:00Z", "2000-01-02T00:00:00Z")
	timedPi(t, filepath.Join(c.Learning.Sources.Pi, "second.jsonl"), "second", "2000-01-03T00:00:00Z")
	s, err := RunOnce(context.Background(), c)
	if err != nil || s.Status != "completed" {
		t.Fatalf("%+v %v", s, err)
	}
	if s.SessionsTargeted != 2 || s.SessionsCompleted != 2 || s.TurnsTargeted != 3 || s.TurnsCompleted != 3 || s.BatchesCompleted != 3 || s.TurnsRemaining != 0 {
		t.Fatalf("wrong invocation report: %+v", s)
	}
	if err := (Store{Dir: c.Learning.StateDir}).Transaction(func(r *Registry) error {
		if len(r.Sessions) != 2 || r.Sessions[0].ReviewedTurns != 2 || r.Sessions[1].ReviewedTurns != 1 {
			t.Fatalf("did not drain all sessions: %+v", r.Sessions)
		}
		if len(r.Runs) != 3 {
			t.Fatalf("expected three bounded batches, got %+v", r.Runs)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAdaptersAndBoundedWorker(t *testing.T) {
	for _, h := range []string{"pi", "codex"} {
		t.Run(h, func(t *testing.T) {
			c, _ := workerFixture(t, h, "automatic")
			if err := Doctor(context.Background(), c); err != nil {
				t.Fatal(err)
			}
			timedPi(t, filepath.Join(c.Learning.Sources.Pi, "pi.jsonl"), "p", "2000-01-01T00:00:00Z", "2000-01-02T00:00:00Z")
			s, err := RunOnce(context.Background(), c)
			if err != nil || s.Status != "completed" {
				t.Fatalf("%+v %v", s, err)
			}
			assertReviewed(t, c, 2)
			s, err = RunOnce(context.Background(), c)
			if err != nil || s.Status != "no_work" {
				t.Fatalf("%+v %v", s, err)
			}
			args, _ := os.ReadFile(filepath.Join(filepath.Dir(c.Learning.Executable), "args"))
			if h == "pi" && !strings.Contains(string(args), "--no-tools") {
				t.Fatal("Pi tools enabled")
			}
			if h == "codex" && (!strings.Contains(string(args), "read-only") || strings.Contains(string(args), "dangerously")) {
				t.Fatal("unsafe Codex arguments")
			}
		})
	}
}
func TestProposalApprovalAndAutomaticPublishing(t *testing.T) {
	for _, policy := range []string{"automatic", "proposals"} {
		t.Run(policy, func(t *testing.T) {
			c, response := workerFixture(t, "pi", policy)
			setResponse(t, response, lessonResult())
			s, err := RunOnce(context.Background(), c)
			if err != nil {
				t.Fatal(err)
			}
			if policy == "proposals" {
				if s.Status != "awaiting_approval" {
					t.Fatal(s)
				}
				assertReviewed(t, c, 0)
				if err = os.Remove(c.Learning.Executable); err != nil {
					t.Fatal(err)
				}
				again, err := RunOnce(context.Background(), c)
				if err != nil || again.RunID != s.RunID || again.Status != "awaiting_approval" {
					t.Fatalf("proposal was rerun: %+v %v", again, err)
				}
				if err = ApplyProposal(c, s.RunID); err != nil {
					t.Fatal(err)
				}
			} else if s.Status != "completed" {
				t.Fatal(s)
			}
			assertReviewed(t, c, 1)
			v, _ := vault.Open(c.Vault.Path)
			if _, err = v.Read("procedures/retry"); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestRecoversDurablePartiallyPublishedResult(t *testing.T) {
	c, _ := workerFixture(t, "pi", "automatic")
	var runID string
	var packets []Packet
	if err := (Store{Dir: c.Learning.StateDir}).Transaction(func(r *Registry) error {
		run, err := r.Begin("pi/p")
		if err != nil {
			return err
		}
		run.WorkerVault = c.Vault.Path
		runID = run.ID
		packets, err = r.Read(runID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	result := lessonResult()
	result.Changes[0].Content += "\n[[procedures/backoff]]\n"
	result.Changes = append(result.Changes, Change{Path: "procedures/backoff.md", Content: "# Backoff\n\nWait between retries.\n", Evidence: []Evidence{{SessionID: "pi/p", Turn: 1}}})
	proposal, err := makeProposal(c, runID, result, packets, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if err = writeJSON(proposalPath(c.Learning.StateDir, runID), proposal); err != nil {
		t.Fatal(err)
	}
	v, _ := vault.Open(c.Vault.Path)
	if err = v.Write(result.Changes[0].Path, []byte(result.Changes[0].Content), false); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(c.Learning.Executable); err != nil {
		t.Fatal(err)
	}
	status, err := RunOnce(context.Background(), c)
	if err != nil || status.Status != "completed" || status.RunID != runID {
		t.Fatalf("failed recovery: %+v %v", status, err)
	}
	assertReviewed(t, c, 1)
	if _, err = v.Navigate("procedures/retry"); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidResultsNeverAdvanceAndRetry(t *testing.T) {
	c, response := workerFixture(t, "pi", "automatic")
	if err := os.WriteFile(response, []byte("not JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := RunOnce(context.Background(), c); err == nil {
		t.Fatal("exit zero accepted without a result")
	}
	assertReviewed(t, c, 0)
	setResponse(t, response, Result{Summary: "No useful lesson.", Changes: []Change{}})
	if _, err := RunOnce(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	assertReviewed(t, c, 1)
}
func TestPublicationRejectsConcurrentEditAndBadLinks(t *testing.T) {
	c, response := workerFixture(t, "pi", "proposals")
	setResponse(t, response, lessonResult())
	s, err := RunOnce(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if err = AddGuidance(c.Vault.Path, "procedures/retry", "# A concurrent edit\n", false); err != nil {
		t.Fatal(err)
	}
	if err = ApplyProposal(c, s.RunID); err == nil {
		t.Fatal("overwrote concurrent edit")
	}
	assertReviewed(t, c, 0)
	// Reconcile the conflict and ensure broken links also prevent advancement.
	if err = os.Remove(filepath.Join(c.Vault.Path, "procedures/retry.md")); err != nil {
		t.Fatal(err)
	}
	p, err := loadProposal(c.Learning.StateDir, s.RunID)
	if err != nil {
		t.Fatal(err)
	}
	p.Result.Changes[0].Content += "\n[[missing/target]]\n"
	if err = writeJSON(proposalPath(c.Learning.StateDir, s.RunID), p); err != nil {
		t.Fatal(err)
	}
	if err = ApplyProposal(c, s.RunID); err == nil {
		t.Fatal("published broken links")
	}
	assertReviewed(t, c, 0)
}
func TestWorkerLockAndEvidenceValidation(t *testing.T) {
	c, response := workerFixture(t, "pi", "automatic")
	lock := flock.New(filepath.Join(c.Learning.StateDir, "worker.lock"))
	if err := lock.Lock(); err != nil {
		t.Fatal(err)
	}
	s, err := RunOnce(context.Background(), c)
	if err != nil || s.Status != "busy" {
		t.Fatalf("%+v %v", s, err)
	}
	_ = lock.Unlock()
	result := lessonResult()
	result.Changes[0].Evidence[0].Turn = 2
	setResponse(t, response, result)
	if _, err = RunOnce(context.Background(), c); err == nil {
		t.Fatal("accepted evidence outside bounded snapshot")
	}
	assertReviewed(t, c, 0)
}
func TestPublicationPreservesAttachmentLinks(t *testing.T) {
	c, response := workerFixture(t, "pi", "automatic")
	if err := os.WriteFile(filepath.Join(c.Vault.Path, "evidence.pdf"), []byte("attachment"), 0600); err != nil {
		t.Fatal(err)
	}
	result := lessonResult()
	result.Changes[0].Content += "\n[[evidence.pdf]]\n"
	setResponse(t, response, result)
	if _, err := RunOnce(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	assertReviewed(t, c, 1)
}

func TestWorkerUsesConfigInsteadOfRememberedSources(t *testing.T) {
	c, _ := workerFixture(t, "pi", "automatic")
	c.Learning.Sources = config.Sources{}
	s, err := RunOnce(context.Background(), c)
	if err != nil || s.Status != "no_work" {
		t.Fatalf("disabled sources still produced work: %+v %v", s, err)
	}
	c.Learning.Sources.Pi = filepath.Join(filepath.Dir(c.Vault.Path), "source")
	s, err = RunOnce(context.Background(), c)
	if err != nil || s.Status != "completed" {
		t.Fatalf("configured source was not reviewed: %+v %v", s, err)
	}
	assertReviewed(t, c, 1)
}

func TestAdapterTimeout(t *testing.T) {
	c, _ := workerFixture(t, "pi", "automatic")
	c.Learning.Timeout = "1s"
	if err := os.WriteFile(c.Learning.Executable, []byte("#!/bin/sh\nsleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := RunOnce(context.Background(), c); err == nil {
		t.Fatal("worker did not time out")
	}
	assertReviewed(t, c, 0)
}
