package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/andrewmarous/trailmix/config"
	"github.com/andrewmarous/trailmix/learning"
)

const sessionsUsage = `Usage: tmix learning sessions [options] COMMAND [ID]
Commands:
  config          Show the learning cutoff; optionally set --start-at
  scan            Discover Pi and Codex sessions and count completed turns
  status          Show sources, coverage errors, sessions, and review runs
  begin [SESSION] Scan and snapshot pending turns (optionally one session)
  read RUN        Print review evidence for a run (untrusted source data)
  complete RUN    Advance reviewed counts; supply review outcome on stdin
  abort RUN       Close a run without advancing reviewed counts
  reset SESSION   Reset a session's reviewed count after history reconciliation
Options (before COMMAND):
  --state-dir PATH           Local registry directory (independent of --vault)
  --start-at RFC3339         Set the learning cutoff with config (after tmix init)
  --pi-root PATH             Pi session root; empty disables Pi discovery
  --codex-root PATH          Codex session root; empty disables active discovery
  --codex-archive-root PATH  Codex archive root; empty disables archive discovery
Run tmix init before scanning or reviewing. Only turns completed on or after
learning_start_at are eligible. Earlier turns are excluded, not marked reviewed.
Source overrides apply only to this scan/begin. All output is JSON.
This command tracks review progress; it does not run a model or publish notes.
`

func runSessions(ctx context.Context, args []string, in io.Reader, out, diagnostics io.Writer, configured config.Sources) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("learning sessions", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	state := flags.String("state-dir", learning.DefaultStateDir(home), "local registry directory")
	startAt := flags.String("start-at", "", "learning cutoff, RFC3339 with timezone")
	flags.StringVar(&configured.Pi, "pi-root", configured.Pi, "Pi sessions root")
	flags.StringVar(&configured.Codex, "codex-root", configured.Codex, "Codex sessions root")
	flags.StringVar(&configured.CodexArchive, "codex-archive-root", configured.CodexArchive, "Codex archive root")
	if err = flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			_, err = io.WriteString(out, sessionsUsage)
		}
		return err
	}
	args = flags.Args()
	if len(args) == 0 || args[0] == "help" {
		_, err = io.WriteString(out, sessionsUsage)
		return err
	}
	action := args[0]
	count := 1
	switch action {
	case "read", "complete", "abort", "reset":
		count = 2
	case "scan", "status", "begin", "config":
	default:
		return fmt.Errorf("unknown sessions command %q", action)
	}
	if len(args) != count && !(action == "begin" && len(args) == 2) {
		return fmt.Errorf("invalid arguments; %s", sessionsUsage)
	}
	startProvided := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "start-at" {
			startProvided = true
		}
	})
	if startProvided && action != "config" {
		return fmt.Errorf("--start-at requires config")
	}
	sourceOverride := false
	flags.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "pi-root", "codex-root", "codex-archive-root":
			sourceOverride = true
		}
	})
	if sourceOverride && action != "scan" && action != "begin" {
		return fmt.Errorf("source overrides require scan or begin")
	}
	var outcome []byte
	if action == "complete" {
		outcome, err = io.ReadAll(io.LimitReader(in, 65537))
		if err != nil {
			return err
		}
		if len(outcome) > 65536 {
			return fmt.Errorf("review outcome exceeds 64 KiB")
		}
	}
	var result any
	var operationErr error
	store := learning.Store{Dir: *state}
	err = store.Transaction(func(r *learning.Registry) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if action == "scan" || action == "begin" {
			sources := learning.ConfiguredSources(configured)
			for i := range sources {
				sources[i].Root, err = filepath.Abs(config.Expand(sources[i].Root, home))
				if err != nil {
					return err
				}
			}
			if err = r.Scan(sources); err != nil {
				return err
			}
		}
		switch action {
		case "config":
			if startProvided {
				if err = r.SetStartAt(*startAt); err != nil {
					return err
				}
			}
			if r.LearningStartAt == "" {
				return fmt.Errorf("learning cutoff is not initialized; run tmix init")
			}
			result = map[string]string{"learning_start_at": r.LearningStartAt}
		case "scan", "status":
			if r.LearningStartAt == "" {
				fmt.Fprintln(diagnostics, "Learning cutoff is not initialized. Run tmix init before learning.")
			}
			result = r
		case "begin":
			// Preserve discovery/error coverage even when no review can start.
			result, operationErr = r.Begin(args[1:]...)
			return nil
		case "read":
			result, err = r.Read(args[1])
		case "complete":
			result, err = r.Close(args[1], string(outcome), false)
		case "abort":
			result, err = r.Close(args[1], "", true)
		case "reset":
			err = r.Reset(args[1])
			result = r
		}
		return err
	})
	if err != nil {
		return err
	}
	if operationErr != nil {
		return operationErr
	}
	return json.NewEncoder(out).Encode(result)
}
