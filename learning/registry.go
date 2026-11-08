package learning

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/andrewmarous/trailmix/backup"
)

type Source struct {
	Name    string   `json:"name"`
	Harness string   `json:"harness"`
	Root    string   `json:"root"`
	Status  string   `json:"status,omitempty"`
	Errors  []string `json:"errors,omitempty"`
}
type Session struct {
	ID                  string     `json:"id"`
	Harness             string     `json:"harness"`
	NativeID            string     `json:"native_id"`
	Path                string     `json:"path"`
	CWD                 string     `json:"cwd"`
	CompletedTurns      int        `json:"completed_turns"`
	ReviewedTurns       int        `json:"reviewed_turns"`
	PendingTurns        int        `json:"pending_turns"`
	BeforeCutoffTurns   int        `json:"before_cutoff_turns"`
	ReviewedSpans       []TurnSpan `json:"reviewed_spans,omitempty"`
	ReviewedThroughTurn int        `json:"reviewed_through_turn,omitempty"`
	ReviewedDigest      string     `json:"reviewed_digest,omitempty"`
	LastRun             string     `json:"last_run,omitempty"`
	Error               string     `json:"error,omitempty"`
}
type Range struct {
	SessionID   string `json:"session_id"`
	FromTurn    int    `json:"from_turn"`
	ThroughTurn int    `json:"through_turn"`
	Digest      string `json:"digest"`
}
type Run struct {
	WorkerVault string  `json:"worker_vault,omitempty"`
	ID          string  `json:"id"`
	Started     string  `json:"started"`
	Completed   string  `json:"completed,omitempty"`
	Outcome     string  `json:"outcome,omitempty"`
	Ranges      []Range `json:"ranges"`
}
type Registry struct {
	Version         int       `json:"version"`
	LearningStartAt string    `json:"learning_start_at,omitempty"`
	Sources         []Source  `json:"sources"`
	Sessions        []Session `json:"sessions"`
	Runs            []Run     `json:"runs"`
}
type Store struct{ Dir string }

// Transaction serializes registry access across all vaults on this computer.
// No transcript bodies are persisted. A corrupt state is never overwritten.
func (s Store) Transaction(fn func(*Registry) error) error {
	dir, err := filepath.Abs(s.Dir)
	if err != nil {
		return err
	}
	for _, p := range []string{dir, filepath.Join(dir, "registry.json"), filepath.Join(dir, ".tmx-operation.lock")} {
		info, e := os.Lstat(p)
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		if e == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks are not supported: %s", p)
		}
	}
	unlock, err := backup.Lock(dir)
	if err != nil {
		return err
	}
	defer unlock()
	path := filepath.Join(dir, "registry.json")
	r := Registry{Version: 2, Sessions: []Session{}, Runs: []Run{}}
	b, err := os.ReadFile(path)
	if err == nil {
		if err = json.Unmarshal(b, &r); err != nil {
			return fmt.Errorf("read session registry: %w", err)
		}
		if r.Version == 1 {
			// The original watermark represented a reviewed prefix with no
			// exclusions. Retain that evidence when adding cutoff support.
			for i := range r.Sessions {
				if r.Sessions[i].ReviewedTurns > 0 {
					r.Sessions[i].addReviewed(1, r.Sessions[i].ReviewedTurns)
				}
			}
			r.Version = 2
		}
		if r.Version != 2 {
			return fmt.Errorf("unsupported registry version %d", r.Version)
		}
		if err = r.validate(); err != nil {
			return fmt.Errorf("invalid session registry: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err = fn(&r); err != nil {
		return err
	}
	b, err = json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".registry-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, path)
}

func (r *Registry) validate() error {
	if r.LearningStartAt != "" {
		if _, err := r.requireCutoff(); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for _, s := range r.Sessions {
		if s.ID == "" || seen[s.ID] || s.CompletedTurns < 0 || s.ReviewedTurns < 0 || s.PendingTurns < 0 {
			return fmt.Errorf("invalid or duplicate session %q", s.ID)
		}
		if s.BeforeCutoffTurns < 0 || s.ReviewedThroughTurn < 0 {
			return fmt.Errorf("invalid progress for %s", s.ID)
		}
		end := 0
		for _, span := range s.ReviewedSpans {
			if span.FromTurn <= end || span.ThroughTurn < span.FromTurn {
				return fmt.Errorf("invalid reviewed span for %s", s.ID)
			}
			end = span.ThroughTurn
		}
		if end != s.ReviewedThroughTurn {
			return fmt.Errorf("invalid reviewed boundary for %s", s.ID)
		}
		seen[s.ID] = true
	}
	runs := map[string]bool{}
	active := 0
	for _, run := range r.Runs {
		if run.ID == "" || runs[run.ID] {
			return fmt.Errorf("invalid or duplicate run %q", run.ID)
		}
		runs[run.ID] = true
		if run.Completed == "" {
			active++
		}
		for _, item := range run.Ranges {
			if !seen[item.SessionID] || item.FromTurn < 1 || item.ThroughTurn < item.FromTurn || item.Digest == "" {
				return fmt.Errorf("invalid range in run %s", run.ID)
			}
		}
	}
	if active > 1 {
		return fmt.Errorf("multiple active review runs")
	}
	return nil
}

func DefaultSources(home string) []Source {
	return []Source{
		{Name: "pi-root", Harness: "pi", Root: filepath.Join(home, ".pi", "agent", "sessions")},
		{Name: "codex-root", Harness: "codex", Root: filepath.Join(home, ".codex", "sessions")},
		{Name: "codex-archive-root", Harness: "codex", Root: filepath.Join(home, ".codex", "archived_sessions")},
	}
}

// Scan inventories configured roots without copying their transcripts. Missing
// roots and malformed files remain visible in coverage/errors, not successes.
func (r *Registry) Scan(sources []Source) error {
	if _, err := r.requireCutoff(); err != nil {
		return err
	}
	if sources != nil {
		r.Sources = sources
	}
	for _, source := range r.Sources {
		if source.Harness != "pi" && source.Harness != "codex" {
			return fmt.Errorf("unsupported harness %q", source.Harness)
		}
		if !filepath.IsAbs(source.Root) {
			return fmt.Errorf("source root must be absolute: %s", source.Root)
		}
	}
	for i := range r.Sessions {
		r.Sessions[i].Error = "session not found in latest scan"
	}
	found := map[string]string{}
	problems := map[string]string{}
	for i := range r.Sources {
		source := &r.Sources[i]
		source.Errors = nil
		source.Status = "ok"
		info, err := os.Lstat(source.Root)
		if err != nil {
			source.Status = "unavailable"
			source.Errors = append(source.Errors, err.Error())
			continue
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			source.Status = "unavailable"
			source.Errors = append(source.Errors, "source root must be a directory, not a symlink")
			continue
		}
		err = filepath.WalkDir(source.Root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				source.Errors = append(source.Errors, fmt.Sprintf("%s: %v", path, walkErr))
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				source.Errors = append(source.Errors, "skipped symlink: "+path)
				return nil
			}
			if d.IsDir() || filepath.Ext(path) != ".jsonl" {
				return nil
			}
			t, e := parse(path, source.Harness)
			if t.ID == "" {
				if e != nil {
					source.Errors = append(source.Errors, fmt.Sprintf("%s: %v", path, e))
				}
				return nil
			}
			id := source.Harness + "/" + t.ID
			s := r.session(id)
			if s == nil {
				r.Sessions = append(r.Sessions, Session{ID: id, Harness: source.Harness, NativeID: t.ID})
				s = &r.Sessions[len(r.Sessions)-1]
			}
			if e != nil {
				if s.Path == "" {
					s.Path = path
					s.CWD = t.CWD
				}
				s.Error = e.Error()
				problems[id] = s.Error
				source.Errors = append(source.Errors, fmt.Sprintf("%s: %v", path, e))
				return nil
			}
			fingerprint := digest(t.Turns)
			if previous, ok := found[id]; ok {
				if previous != fingerprint {
					s.Error = "conflicting files with the same native session ID"
					problems[id] = s.Error
					source.Errors = append(source.Errors, id+": "+s.Error)
				}
				return nil
			}
			found[id] = fingerprint
			s.Path = path
			s.CWD = t.CWD
			s.Error = ""
			if err := r.refresh(s, t); err != nil {
				s.Error = err.Error()
			}
			return nil
		})
		if err != nil {
			source.Errors = append(source.Errors, err.Error())
		}
		if len(source.Errors) > 0 {
			source.Status = "partial"
		}
	}
	for i := range r.Sessions {
		if problem := problems[r.Sessions[i].ID]; problem != "" {
			r.Sessions[i].Error = problem
		}
	}
	sort.Slice(r.Sessions, func(i, j int) bool { return r.Sessions[i].ID < r.Sessions[j].ID })
	return nil
}
func (r *Registry) session(id string) *Session {
	for i := range r.Sessions {
		if r.Sessions[i].ID == id {
			return &r.Sessions[i]
		}
	}
	return nil
}
func (r *Registry) run(id string) (*Run, error) {
	for i := range r.Runs {
		if r.Runs[i].ID == id {
			return &r.Runs[i], nil
		}
	}
	return nil, fmt.Errorf("unknown session review run %q", id)
}
func (r *Registry) active() *Run {
	for i := range r.Runs {
		if r.Runs[i].Completed == "" {
			return &r.Runs[i]
		}
	}
	return nil
}
func (r *Registry) Begin(sessionIDs ...string) (*Run, error) {
	if _, err := r.requireCutoff(); err != nil {
		return nil, err
	}
	if len(sessionIDs) > 1 {
		return nil, fmt.Errorf("begin accepts at most one session ID")
	}
	if len(sessionIDs) == 1 && r.session(sessionIDs[0]) == nil {
		return nil, fmt.Errorf("unknown session %q", sessionIDs[0])
	}
	if active := r.active(); active != nil {
		return nil, fmt.Errorf("run %s is active; read, complete, or abort it", active.ID)
	}
	ranges := []Range{}
	for _, s := range r.Sessions {
		if len(sessionIDs) == 1 && s.ID != sessionIDs[0] {
			continue
		}
		if s.Error != "" || s.PendingTurns == 0 {
			continue
		}
		t, err := parse(s.Path, s.Harness)
		if err != nil {
			return nil, err
		}
		if t.ID != s.NativeID || len(t.Turns) < s.CompletedTurns || (s.ReviewedThroughTurn > 0 && digest(t.Turns[:s.ReviewedThroughTurn]) != s.ReviewedDigest) {
			return nil, fmt.Errorf("session %s changed; scan again", s.ID)
		}
		pending, err := r.pendingRanges(s, t, s.CompletedTurns)
		if err != nil {
			return nil, err
		}
		ranges = append(ranges, pending...)
	}
	if len(ranges) == 0 {
		return nil, fmt.Errorf("no pending completed turns; inspect sessions status for source errors")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	r.Runs = append(r.Runs, Run{ID: now, Started: now, Ranges: ranges})
	return &r.Runs[len(r.Runs)-1], nil
}

type Packet struct {
	SessionID   string `json:"session_id"`
	CWD         string `json:"cwd"`
	FromTurn    int    `json:"from_turn"`
	ThroughTurn int    `json:"through_turn"`
	Context     []Turn `json:"context,omitempty"`
	Turns       []Turn `json:"turns"`
}

func (r *Registry) loadRange(item Range) (*Session, transcript, error) {
	if item.FromTurn < 1 || item.ThroughTurn < item.FromTurn {
		return nil, transcript{}, fmt.Errorf("invalid turn range")
	}
	s := r.session(item.SessionID)
	if s == nil {
		return nil, transcript{}, fmt.Errorf("missing session %s", item.SessionID)
	}
	t, err := parse(s.Path, s.Harness)
	if err != nil {
		return nil, t, err
	}
	if s.Error != "" || t.ID != s.NativeID || len(t.Turns) < item.ThroughTurn || digest(t.Turns[:item.ThroughTurn]) != item.Digest {
		return nil, t, fmt.Errorf("snapshot changed for %s; abort, scan, and reconcile history", s.ID)
	}
	return s, t, nil
}
func (r *Registry) Read(id string) ([]Packet, error) {
	run, err := r.run(id)
	if err != nil {
		return nil, err
	}
	packets := []Packet{}
	for _, item := range run.Ranges {
		s, t, e := r.loadRange(item)
		if e != nil {
			return nil, e
		}
		start := item.FromTurn - 1
		contextStart := start - 1
		if contextStart < 0 {
			contextStart = 0
		}
		packets = append(packets, Packet{SessionID: s.ID, CWD: s.CWD, FromTurn: item.FromTurn, ThroughTurn: item.ThroughTurn, Context: t.Turns[contextStart:start], Turns: t.Turns[start:item.ThroughTurn]})
	}
	return packets, nil
}
func (r *Registry) Close(id, outcome string, abort bool) (*Run, error) {
	if !abort {
		if _, err := r.requireCutoff(); err != nil {
			return nil, err
		}
	}
	run, err := r.run(id)
	if err != nil {
		return nil, err
	}
	if run.Completed != "" {
		return nil, fmt.Errorf("run is already closed")
	}
	if !abort {
		if strings.TrimSpace(outcome) == "" {
			return nil, fmt.Errorf("a review outcome is required on stdin")
		}
		// Validate every source before changing any watermark.
		for _, item := range run.Ranges {
			_, t, e := r.loadRange(item)
			if e != nil {
				return nil, e
			}
			for _, turn := range t.Turns[item.FromTurn-1 : item.ThroughTurn] {
				eligible, e := r.eligible(turn)
				if e != nil {
					return nil, e
				}
				if !eligible {
					return nil, fmt.Errorf("snapshot turn is now before the learning cutoff; abort and scan again")
				}
			}
		}
		for _, item := range run.Ranges {
			s := r.session(item.SessionID)
			s.addReviewed(item.FromTurn, item.ThroughTurn)
			if item.ThroughTurn == s.ReviewedThroughTurn {
				s.ReviewedDigest = item.Digest
			}
			s.LastRun = id
		}
		for _, item := range run.Ranges {
			s := r.session(item.SessionID)
			t, e := parse(s.Path, s.Harness)
			if e != nil {
				return nil, e
			}
			if e = r.refresh(s, t); e != nil {
				return nil, e
			}
		}
	} else {
		outcome = "aborted"
	}
	run.Completed = time.Now().UTC().Format(time.RFC3339Nano)
	run.Outcome = outcome
	return run, nil
}
func (r *Registry) Reset(id string) error {
	if r.active() != nil {
		return fmt.Errorf("abort the active run before resetting a session")
	}
	s := r.session(id)
	if s == nil {
		return fmt.Errorf("unknown session %q", id)
	}
	s.ReviewedTurns = 0
	s.ReviewedThroughTurn = 0
	s.ReviewedSpans = nil
	s.ReviewedDigest = ""
	s.LastRun = ""
	s.PendingTurns = s.CompletedTurns - s.BeforeCutoffTurns
	return nil
}
