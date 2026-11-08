package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/andrewmarous/trailmix/backup"
)

// The ledger is separate from searchable notes. Sessions are immutable evidence
// packets, not automatic transcript exports. A run owns a fixed session snapshot.
type learningSession struct {
	ID          string `json:"id"`
	Evidence    string `json:"evidence"`
	Recorded    string `json:"recorded"`
	ProcessedBy string `json:"processed_by,omitempty"`
}
type learningRun struct {
	ID        string   `json:"id"`
	Sessions  []string `json:"sessions"`
	Started   string   `json:"started"`
	Completed string   `json:"completed,omitempty"`
	Outcome   string   `json:"outcome,omitempty"`
}
type learningLedger struct {
	Sessions []learningSession `json:"sessions"`
	Runs     []learningRun     `json:"runs"`
}

func runLearning(args []string, root string, in io.Reader, out, diagnostics io.Writer) error {
	flags := flag.NewFlagSet("learning", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	flags.StringVar(&root, "vault", root, "vault directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	args = flags.Args()
	if len(args) == 0 {
		return fmt.Errorf("usage: tmix learning [--vault PATH] status | record SESSION | begin | complete RUN | abort RUN | sessions help")
	}
	action := args[0]
	if action == "sessions" {
		return runSessions(args[1:], in, out, diagnostics)
	}
	want := 1
	if action == "record" || action == "complete" || action == "abort" {
		want = 2
	}
	if len(args) != want {
		return fmt.Errorf("invalid arguments for learning %s", action)
	}
	switch action {
	case "record", "status", "begin", "complete", "abort":
	default:
		return fmt.Errorf("unknown learning action %q", action)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if root == "~" {
		root = home
	} else if strings.HasPrefix(root, "~/") {
		root = filepath.Join(home, root[2:])
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	// Match vault safety: reject symlinks from the vault root through state.
	// System ancestors can be symlinks (for example, /var on macOS).
	dir := filepath.Join(root, ".learning")
	path := filepath.Join(dir, "ledger.json")
	for p := path; ; p = filepath.Dir(p) {
		info, e := os.Lstat(p)
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		if e == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks are not supported: %s", p)
		}
		if p == root {
			break
		}
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	unlock, err := backup.Lock(root)
	if err != nil {
		return err
	}
	defer unlock()
	ledger := learningLedger{Sessions: []learningSession{}, Runs: []learningRun{}}
	data, err := os.ReadFile(path)
	if err == nil {
		if err = json.Unmarshal(data, &ledger); err != nil {
			return fmt.Errorf("read learning ledger: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var result any
	switch action {
	case "status":
		result = ledger
	case "record":
		id := args[1]
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("session ID is required")
		}
		for _, s := range ledger.Sessions {
			if s.ID == id {
				return fmt.Errorf("session %q already recorded; use a new ID for new evidence", id)
			}
		}
		evidence, e := io.ReadAll(io.LimitReader(in, 1024*1024+1))
		if e != nil {
			return e
		}
		if len(evidence) > 1024*1024 {
			return fmt.Errorf("evidence exceeds 1 MiB")
		}
		if strings.TrimSpace(string(evidence)) == "" {
			return fmt.Errorf("session evidence is required on stdin")
		}
		s := learningSession{ID: id, Evidence: string(evidence), Recorded: now}
		ledger.Sessions = append(ledger.Sessions, s)
		result = s
	case "begin":
		for _, r := range ledger.Runs {
			if r.Completed == "" && r.Outcome == "" {
				return fmt.Errorf("run %s is active; resume or abort it", r.ID)
			}
		}
		ids := []string{}
		for _, s := range ledger.Sessions {
			if s.ProcessedBy == "" {
				ids = append(ids, s.ID)
			}
		}
		if len(ids) == 0 {
			return fmt.Errorf("no pending sessions")
		}
		r := learningRun{ID: now, Sessions: ids, Started: now}
		ledger.Runs = append(ledger.Runs, r)
		result = r
	case "complete", "abort":
		index := -1
		for i, r := range ledger.Runs {
			if r.ID == args[1] {
				index = i
				break
			}
		}
		if index < 0 {
			return fmt.Errorf("unknown learning run %q", args[1])
		}
		r := &ledger.Runs[index]
		if r.Completed != "" || r.Outcome != "" {
			return fmt.Errorf("run is already closed")
		}
		if action == "complete" {
			outcome, e := io.ReadAll(io.LimitReader(in, 65537))
			if e != nil {
				return e
			}
			if len(outcome) > 65536 || strings.TrimSpace(string(outcome)) == "" {
				return fmt.Errorf("supply a review outcome on stdin (maximum 64 KiB)")
			}
			r.Outcome = string(outcome)
			r.Completed = now
			for i := range ledger.Sessions {
				for _, id := range r.Sessions {
					if ledger.Sessions[i].ID == id {
						ledger.Sessions[i].ProcessedBy = r.ID
					}
				}
			}
		} else {
			r.Outcome = "aborted"
			r.Completed = now
		}
		result = r
	}
	if action != "status" {
		data, err = json.MarshalIndent(ledger, "", "  ")
		if err != nil {
			return err
		}
		f, e := os.CreateTemp(dir, ".ledger-*")
		if e != nil {
			return e
		}
		temp := f.Name()
		defer os.Remove(temp)
		if _, err = f.Write(data); err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if err = os.Rename(temp, path); err != nil {
			return err
		}
	}
	return json.NewEncoder(out).Encode(result)
}
