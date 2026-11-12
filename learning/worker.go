package learning

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/andrewmarous/trailmix/backup"
	"github.com/andrewmarous/trailmix/config"
	"github.com/andrewmarous/trailmix/vault"
	"github.com/gofrs/flock"
)

type JobStatus struct {
	NavigationID      string  `json:"navigation_id,omitempty"`
	Started           string  `json:"started"`
	Finished          string  `json:"finished,omitempty"`
	LastSuccess       string  `json:"last_success,omitempty"`
	Status            string  `json:"status"`
	RunID             string  `json:"run_id,omitempty"`
	ProposalPath      string  `json:"proposal_path,omitempty"`
	Error             string  `json:"error,omitempty"`
	CoverageErrors    int     `json:"coverage_errors"`
	SessionsTargeted  int     `json:"sessions_targeted"`
	SessionsCompleted int     `json:"sessions_completed"`
	BatchRanges       []Range `json:"batch_ranges,omitempty"`
	TurnsTargeted     int     `json:"turns_targeted"`
	TurnsCompleted    int     `json:"turns_completed"`
	BatchesCompleted  int     `json:"batches_completed"`
	TurnsRemaining    int     `json:"turns_remaining"`
}
type Proposal struct {
	Kind     string            `json:"kind,omitempty"`
	Sources  map[string]string `json:"source_sha256,omitempty"`
	RunID    string            `json:"run_id"`
	Vault    string            `json:"vault"`
	Policy   string            `json:"policy"`
	Result   Result            `json:"result"`
	Expected map[string]string `json:"expected_sha256"`
}

func proposalPath(dir, id string) string {
	return filepath.Join(dir, "proposals", fmt.Sprintf("%x.json", sha256.Sum256([]byte(id))))
}
func writeJSON(path string, value any) error {
	if info, e := os.Lstat(filepath.Dir(path)); e == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("state directory is a symlink")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("state path is a symlink: %s", path)
	}
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".tmx-job-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func LoadJobStatus(dir string) JobStatus {
	return loadJobStatus(dir, "last_job.json")
}

// LoadFailureStatus retains the last failed invocation across later successes.
func LoadFailureStatus(dir string) JobStatus {
	return loadJobStatus(dir, "last_failure.json")
}

func loadJobStatus(dir, name string) JobStatus {
	var s JobStatus
	path := filepath.Join(dir, name)
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s
	}
	if err == nil {
		err = json.Unmarshal(b, &s)
	}
	if err != nil {
		return JobStatus{Status: "failed", Error: fmt.Sprintf("read learning report %s: %v", path, err)}
	}
	return s
}
func loadProposal(dir, id string) (Proposal, error) {
	var p Proposal
	path := proposalPath(dir, id)
	for _, candidate := range []string{filepath.Dir(path), path} {
		if info, e := os.Lstat(candidate); e == nil && info.Mode()&os.ModeSymlink != 0 {
			return p, fmt.Errorf("proposal path is a symlink")
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return p, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 2*resultLimit+1))
	if err != nil {
		return p, err
	}
	if len(b) > 2*resultLimit {
		return p, fmt.Errorf("proposal exceeds size limit")
	}
	err = json.Unmarshal(b, &p)
	if err == nil && p.RunID != id {
		err = fmt.Errorf("proposal run ID mismatch")
	}
	return p, err
}

// ConfiguredSources returns enabled roots from config, never from scan history.
func ConfiguredSources(settings config.Sources) []Source {
	sources := []Source{}
	for _, s := range []Source{
		{Name: "pi-root", Harness: "pi", Root: settings.Pi},
		{Name: "codex-root", Harness: "codex", Root: settings.Codex},
		{Name: "codex-archive-root", Harness: "codex", Root: settings.CodexArchive},
	} {
		if s.Root != "" {
			sources = append(sources, s)
		}
	}
	return sources
}
func collectNotes(root string, limit int) (map[string]string, error) {
	v, err := vault.Open(root)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("vault must be an initialized directory, not a symlink")
	}
	notes := map[string]string{}
	size := 0
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != root && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || d.Type()&os.ModeSymlink != 0 || !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("note is not a regular file: %s", rel)
		}
		if info.Size() > int64(limit-size) {
			return fmt.Errorf("existing notes exceed learning.max_input_bytes; increase the limit")
		}
		b, err := v.Read(rel)
		if err != nil {
			return err
		}
		size += len(b) + len(rel)
		if size > limit {
			return fmt.Errorf("existing notes exceed learning.max_input_bytes")
		}
		notes[rel] = string(b)
		return nil
	})
	return notes, err
}
func validateResult(r Result, packets []Packet) error {
	if strings.TrimSpace(r.Summary) == "" || len(r.Summary) > 4096 || r.Changes == nil || len(r.Changes) > 20 {
		return fmt.Errorf("invalid structured review result")
	}
	if err := validateChanges(r.Changes); err != nil {
		return err
	}
	for _, change := range r.Changes {
		if strings.HasPrefix(change.Path, "navigation/") {
			return fmt.Errorf("session review cannot change navigation maps")
		}
		if len(change.Evidence) == 0 || len(change.Evidence) > 100 {
			return fmt.Errorf("proposed note requires 1 to 100 pending-turn citations: %s", change.Path)
		}
		for _, e := range change.Evidence {
			if e.Note != "" {
				return fmt.Errorf("session review requires pending-turn evidence")
			}
			found := false
			for _, p := range packets {
				if e.SessionID == p.SessionID && e.Turn >= p.FromTurn && e.Turn <= p.ThroughTurn {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("proposed evidence is outside the pending snapshot")
			}
		}
	}
	return nil
}
func makeProposal(c config.Config, id string, r Result, packets []Packet, notes map[string]string) (Proposal, error) {
	p := Proposal{RunID: id, Vault: c.Vault.Path, Policy: c.Learning.Publication, Result: r, Expected: map[string]string{}}
	if err := validateResult(r, packets); err != nil {
		return p, err
	}
	for _, change := range r.Changes {
		hash := ""
		if body, ok := notes[change.Path]; ok {
			hash = fmt.Sprintf("%x", sha256.Sum256([]byte(body)))
		}
		p.Expected[change.Path] = hash
	}
	return p, nil
}

// RunOnce runs at most one session and max_turns pending turns. The worker lock
// spans model execution; the registry lock never does. Pending proposals stop
// automatic advancement until accepted or aborted.
func RunOnce(ctx context.Context, c config.Config) (status JobStatus, err error) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return status, fmt.Errorf("learning workers support macOS and Linux only")
	}
	store := Store{Dir: c.Learning.StateDir}
	if err = store.Transaction(func(r *Registry) error { _, e := r.requireCutoff(); return e }); err != nil {
		return status, err
	}
	lockPath := filepath.Join(store.Dir, "worker.lock")
	if info, e := os.Lstat(lockPath); e == nil && info.Mode()&os.ModeSymlink != 0 {
		return status, fmt.Errorf("worker lock is a symlink")
	}
	lock := flock.New(lockPath)
	locked, e := lock.TryLock()
	if e != nil {
		return status, e
	}
	if !locked {
		return JobStatus{Status: "busy"}, nil
	}
	defer lock.Unlock()
	previous := LoadJobStatus(store.Dir)
	status = JobStatus{Status: "running", Started: time.Now().UTC().Format(time.RFC3339Nano), LastSuccess: previous.LastSuccess}
	if err = writeJSON(filepath.Join(store.Dir, "last_job.json"), status); err != nil {
		return status, err
	}
	defer func() {
		status.Finished = time.Now().UTC().Format(time.RFC3339Nano)
		if err != nil {
			status.Status = "failed"
			status.Error = err.Error()
		} else if status.Status == "completed" || status.Status == "no_work" {
			status.LastSuccess = status.Finished
		}
		status.TurnsRemaining = status.TurnsTargeted - status.TurnsCompleted
		if err != nil {
			if e := writeJSON(filepath.Join(store.Dir, "last_failure.json"), status); e != nil {
				err = fmt.Errorf("%w; save failure report: %v", err, e)
				status.Error = err.Error()
			}
		}
		if e := writeJSON(filepath.Join(store.Dir, "last_job.json"), status); err == nil && e != nil {
			err = e
		}
	}()
	if err = c.ValidateLearning(); err != nil {
		return status, err
	}
	var packets []Packet
	err = store.Transaction(func(r *Registry) error {
		if active := r.active(); active != nil {
			if active.WorkerVault == "" {
				status.Status = "manual_review_active"
				status.RunID = active.ID
				return nil
			}
			if active.WorkerVault != c.Vault.Path {
				return fmt.Errorf("active learning run belongs to another vault: %s", active.WorkerVault)
			}
			status.RunID = active.ID
			if _, e := loadProposal(store.Dir, active.ID); e == nil {
				status.Status = "recovering"
				return nil
			} else if !os.IsNotExist(e) {
				return e
			}
			if _, e := r.Close(active.ID, "Worker interrupted before producing a result.", true); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		return status, err
	}
	if status.Status == "manual_review_active" {
		return status, nil
	}
	var work []Range
	coverageProblem := ""
	targets := map[string]int{}
	completed := map[string]int{}
	err = store.Transaction(func(r *Registry) error {
		if e := r.Scan(ConfiguredSources(c.Learning.Sources)); e != nil {
			return e
		}
		for _, s := range r.Sources {
			if s.Status != "ok" {
				status.CoverageErrors++
				if coverageProblem == "" && len(s.Errors) > 0 {
					coverageProblem = s.Errors[0]
				}
			}
		}
		for _, s := range r.Sessions {
			if s.Error == "" {
				continue
			}
			for _, source := range r.Sources {
				rel, e := filepath.Rel(source.Root, s.Path)
				if e == nil && source.Harness == s.Harness && filepath.IsLocal(rel) {
					status.CoverageErrors++
					if coverageProblem == "" {
						coverageProblem = s.ID + ": " + s.Error
					}
					break
				}
			}
		}
		before, _ := time.Parse(time.RFC3339Nano, status.Started)
		var e error
		work, e = r.captureWork(before)
		for _, item := range work {
			targets[item.SessionID] += item.ThroughTurn - item.FromTurn + 1
			status.TurnsTargeted += item.ThroughTurn - item.FromTurn + 1
		}
		status.SessionsTargeted = len(targets)
		return e
	})
	if err != nil {
		return status, err
	}
	// Capture workload before any model call so reports include the turns held
	// back by navigation. Recover interrupted lesson writes before mapping them.
	if status.Status != "recovering" {
		if err = maintainNavigation(ctx, c, &status, false); err != nil {
			return status, err
		}
		if status.Status == "awaiting_approval" {
			return status, nil
		}
	}
	for {
		if err = ctx.Err(); err != nil {
			return status, err
		}
		err = store.Transaction(func(r *Registry) error {
			var run *Run
			var e error
			if status.Status == "recovering" {
				run, e = r.run(status.RunID)
			} else {
				run, e = r.beginCaptured(c.Vault.Path, c.Learning.MaxTurns, work)
			}
			if e != nil {
				return e
			}
			if run == nil {
				return nil
			}
			status.RunID = run.ID
			status.BatchRanges = run.Ranges
			if status.Status != "recovering" {
				status.ProposalPath = ""
			}
			packets, e = r.Read(run.ID)
			return e
		})
		if err != nil {
			return status, err
		}
		if len(packets) == 0 {
			if status.CoverageErrors > 0 {
				return status, fmt.Errorf("learning coverage incomplete: %d source/session errors: %s; inspect tmix learning sessions status", status.CoverageErrors, coverageProblem)
			}
			if status.BatchesCompleted == 0 {
				status.Status = "no_work"
			} else {
				status.Status = "completed"
			}
			return status, nil
		}
		status.TurnsRemaining = status.TurnsTargeted - status.TurnsCompleted
		if err = writeJSON(filepath.Join(store.Dir, "last_job.json"), status); err != nil {
			return status, err
		}
		if err = runBatch(ctx, c, &status, packets); err != nil {
			return status, err
		}
		if status.Status == "awaiting_approval" {
			return status, nil
		}
		status.BatchesCompleted++
		for _, p := range packets {
			n := p.ThroughTurn - p.FromTurn + 1
			status.TurnsCompleted += n
			completed[p.SessionID] += n
			if completed[p.SessionID] == targets[p.SessionID] {
				status.SessionsCompleted++
			}
		}
		packets = nil
		status.Status = "running"
		if err = maintainNavigation(ctx, c, &status, false); err != nil {
			return status, err
		}
		if status.Status == "awaiting_approval" {
			return status, nil
		}
	}
}

func runBatch(ctx context.Context, c config.Config, status *JobStatus, packets []Packet) error {
	store := Store{Dir: c.Learning.StateDir}
	duration, _ := time.ParseDuration(c.Learning.Timeout)
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	if status.Status != "recovering" {
		// Leave failed snapshots owned and unreviewed for recovery next time.
		if err := ctx.Err(); err != nil {
			return err
		}
		notes, err := collectNotes(c.Vault.Path, c.Learning.MaxInputBytes)
		if err != nil {
			return err
		}
		input, err := json.Marshal(struct {
			Packets []Packet          `json:"packets"`
			Notes   map[string]string `json:"existing_notes"`
		}{packets, notes})
		if err != nil {
			return err
		}
		if len(input) > c.Learning.MaxInputBytes {
			return fmt.Errorf("review input exceeds learning.max_input_bytes; reduce max_turns or increase the input limit")
		}
		result, err := invokeHarness(ctx, c, input)
		if err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("learning batch interrupted (%v): %w", ctx.Err(), err)
			}
			return err
		}
		proposal, err := makeProposal(c, status.RunID, result, packets, notes)
		if err != nil {
			return err
		}
		if err = writeJSON(proposalPath(store.Dir, status.RunID), proposal); err != nil {
			return err
		}
	}
	p, err := loadProposal(store.Dir, status.RunID)
	if err != nil {
		return err
	}
	status.ProposalPath = proposalPath(store.Dir, status.RunID)
	if p.Policy != "proposals" && p.Policy != "automatic" {
		return fmt.Errorf("proposal has an invalid publication policy")
	}
	if p.Policy == "proposals" || c.Learning.Publication != "automatic" {
		status.Status = "awaiting_approval"
		return nil
	}
	return ApplyProposal(c, status.RunID)
}

func ApplyProposal(c config.Config, id string) error {
	store := Store{Dir: c.Learning.StateDir}
	p, err := loadProposal(store.Dir, id)
	if err != nil {
		return err
	}
	if p.Vault != c.Vault.Path {
		return fmt.Errorf("proposal belongs to a different vault")
	}
	if p.Kind == "navigation" {
		if err = checkNavigationDirectories(c); err != nil {
			return err
		}
	}
	unlock, err := backup.Lock(c.Vault.Path)
	if err != nil {
		return err
	}
	defer unlock()
	if p.Kind == "navigation" {
		return applyNavigation(c, p)
	}
	if p.Kind != "" {
		return fmt.Errorf("unknown proposal kind %q", p.Kind)
	}
	return store.Transaction(func(r *Registry) error {
		run, err := r.run(id)
		if err != nil {
			return err
		}
		if run.Completed != "" {
			return fmt.Errorf("review run is already closed")
		}
		if run.WorkerVault != c.Vault.Path {
			return fmt.Errorf("review run does not belong to this worker vault")
		}
		packets, err := r.Read(id)
		if err != nil {
			return err
		}
		if err = validateResult(p.Result, packets); err != nil {
			return err
		}
		// Verify the source and compute progress before publication, but commit that
		// progress only after every write succeeds. Persisted proposals make partial
		// publication retryable without running the model again.
		if _, err = r.Close(id, p.Result.Summary, false); err != nil {
			return err
		}
		if err = queueNavigation(c, p.Result.Changes); err != nil {
			return err
		}
		return publishGuidance(c.Vault.Path, p.Result.Changes, p.Expected)
	})
}
