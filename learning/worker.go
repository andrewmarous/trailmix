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
	Started        string `json:"started"`
	Finished       string `json:"finished,omitempty"`
	LastSuccess    string `json:"last_success,omitempty"`
	Status         string `json:"status"`
	RunID          string `json:"run_id,omitempty"`
	ProposalPath   string `json:"proposal_path,omitempty"`
	Error          string `json:"error,omitempty"`
	CoverageErrors int    `json:"coverage_errors"`
}
type Proposal struct {
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
	var s JobStatus
	b, err := os.ReadFile(filepath.Join(dir, "last_job.json"))
	if err == nil {
		_ = json.Unmarshal(b, &s)
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
func (v *Registry) beginWorker(root string, limit int) (*Run, error) {
	for _, s := range v.Sessions {
		if s.Error != "" || s.PendingTurns == 0 {
			continue
		}
		run, err := v.Begin(s.ID)
		if err != nil {
			return nil, err
		}
		kept := []Range{}
		left := limit
		for _, item := range run.Ranges {
			if left == 0 {
				break
			}
			size := item.ThroughTurn - item.FromTurn + 1
			if size > left {
				item.ThroughTurn = item.FromTurn + left - 1
				t, err := parse(s.Path, s.Harness)
				if err != nil {
					return nil, err
				}
				item.Digest = digest(t.Turns[:item.ThroughTurn])
				size = left
			}
			kept = append(kept, item)
			left -= size
		}
		run.Ranges = kept
		run.WorkerVault = root
		return run, nil
	}
	return nil, nil
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
		if len(change.Evidence) == 0 || len(change.Evidence) > 100 {
			return fmt.Errorf("proposed note requires 1 to 100 pending-turn citations: %s", change.Path)
		}
		for _, e := range change.Evidence {
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
	if err = c.ValidateLearning(); err != nil {
		return status, err
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
		if e := writeJSON(filepath.Join(store.Dir, "last_job.json"), status); err == nil && e != nil {
			err = e
		}
	}()
	duration, _ := time.ParseDuration(c.Learning.Timeout)
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
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
		if e := r.Scan(ConfiguredSources(c.Learning.Sources)); e != nil {
			return e
		}
		for _, s := range r.Sources {
			if s.Status != "ok" {
				status.CoverageErrors++
			}
		}
		for _, s := range r.Sessions {
			if s.Error != "" {
				status.CoverageErrors++
			}
		}
		run, e := r.beginWorker(c.Vault.Path, c.Learning.MaxTurns)
		if e != nil {
			return e
		}
		if run == nil {
			status.Status = "no_work"
			return nil
		}
		status.RunID = run.ID
		packets, e = r.Read(run.ID)
		return e
	})
	if err != nil {
		return status, err
	}
	if status.Status == "manual_review_active" || status.Status == "no_work" {
		return status, nil
	}
	if status.Status != "recovering" {
		// On failure leave the turn counts unchanged. A later worker safely aborts
		// and retries this owned snapshot if no durable proposal exists.
		if err = ctx.Err(); err != nil {
			return status, err
		}
		notes, e := collectNotes(c.Vault.Path, c.Learning.MaxInputBytes)
		if e != nil {
			return status, e
		}
		input, e := json.Marshal(struct {
			Packets []Packet          `json:"packets"`
			Notes   map[string]string `json:"existing_notes"`
		}{packets, notes})
		if e != nil {
			return status, e
		}
		if len(input) > c.Learning.MaxInputBytes {
			return status, fmt.Errorf("review input exceeds learning.max_input_bytes; reduce max_turns or increase the input limit")
		}
		result, e := invokeHarness(ctx, c, input)
		if e != nil {
			return status, e
		}
		proposal, e := makeProposal(c, status.RunID, result, packets, notes)
		if e != nil {
			return status, e
		}
		if e = writeJSON(proposalPath(store.Dir, status.RunID), proposal); e != nil {
			return status, e
		}
	}
	p, e := loadProposal(store.Dir, status.RunID)
	if e != nil {
		return status, e
	}
	status.ProposalPath = proposalPath(store.Dir, status.RunID)
	if p.Policy != "proposals" && p.Policy != "automatic" {
		return status, fmt.Errorf("proposal has an invalid publication policy")
	}
	if p.Policy == "proposals" {
		status.Status = "awaiting_approval"
		return status, nil
	}
	if c.Learning.Publication != "automatic" {
		status.Status = "awaiting_approval"
		return status, nil
	}
	if err = ApplyProposal(c, status.RunID); err != nil {
		return status, err
	}
	status.Status = "completed"
	return status, nil
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
	unlock, err := backup.Lock(c.Vault.Path)
	if err != nil {
		return err
	}
	defer unlock()
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
		return publishGuidance(c.Vault.Path, p.Result.Changes, p.Expected)
	})
}
