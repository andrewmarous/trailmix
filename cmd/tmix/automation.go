package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/andrewmarous/trailmix/config"
	"github.com/andrewmarous/trailmix/learning"
	"github.com/andrewmarous/trailmix/schedule"
)

func runConfig(args []string, path string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: tmix [--config PATH] config init | path | show")
	}
	switch args[0] {
	case "init":
		if err := config.Create(path); err != nil {
			return err
		}
		fmt.Fprintln(out, path)
		return nil
	case "path":
		fmt.Fprintln(out, path)
		return nil
	case "show":
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = out.Write(b)
		return err
	default:
		return fmt.Errorf("unknown config command %q", args[0])
	}
}
func runAutomation(args []string, root string, c config.Config, configPath string, out, diagnostics io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	root, err = filepath.Abs(config.Expand(root, home))
	if err != nil {
		return err
	}
	configuredVault := c.Vault.Path
	c.Vault.Path = root
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	switch args[0] {
	case "run":
		if len(args) != 1 {
			return fmt.Errorf("usage: tmix learning run")
		}
		status, err := learning.RunOnce(ctx, c)
		if e := json.NewEncoder(out).Encode(status); err == nil {
			err = e
		}
		return err
	case "doctor":
		if len(args) > 2 || (len(args) == 2 && args[1] != "--probe") {
			return fmt.Errorf("usage: tmix learning doctor [--probe]")
		}
		if err = learning.Doctor(ctx, c); err != nil {
			return err
		}
		if err = (learning.Store{Dir: c.Learning.StateDir}).Transaction(func(r *learning.Registry) error {
			if r.LearningStartAt == "" {
				return fmt.Errorf("run tmix init before learning")
			}
			return nil
		}); err != nil {
			return err
		}
		if len(args) == 2 {
			if err = learning.Probe(ctx, c); err != nil {
				return err
			}
		}
		return json.NewEncoder(out).Encode(map[string]any{"ready": true, "harness": c.Learning.Harness, "authentication": "checked during model invocation; use doctor --probe to test", "publication": c.Learning.Publication})
	case "accept":
		if len(args) != 2 {
			return fmt.Errorf("usage: tmix learning accept RUN")
		}
		if err = learning.ApplyProposal(c, args[1]); err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(map[string]string{"status": "completed", "run_id": args[1]})
	case "schedule":
		if len(args) != 2 {
			return fmt.Errorf("usage: tmix learning schedule enable | disable | status")
		}
		m, err := schedule.New(configPath)
		if err != nil {
			return err
		}
		switch args[1] {
		case "enable":
			if configuredVault != root {
				return fmt.Errorf("save vault.path in config.toml before scheduling; do not use a temporary --vault override")
			}
			if err = learning.Doctor(ctx, c); err != nil {
				return err
			}
			if err = (learning.Store{Dir: c.Learning.StateDir}).Transaction(func(r *learning.Registry) error {
				if r.LearningStartAt == "" {
					return fmt.Errorf("run tmix init before installing the scheduler")
				}
				return nil
			}); err != nil {
				return err
			}
			if _, err = os.Stat(c.Vault.Path); err != nil {
				return fmt.Errorf("initialize the configured vault first: %w", err)
			}
			interval, _ := time.ParseDuration(c.Learning.Schedule.Interval)
			if err = m.Enable(interval); err != nil {
				return err
			}
			fmt.Fprintln(diagnostics, "Learning enabled. Credentials must be available outside your interactive shell; run doctor --probe to test.")
		case "disable":
			if err = m.Disable(); err != nil {
				return err
			}
		case "status":
		default:
			return fmt.Errorf("unknown schedule action %q", args[1])
		}
		return json.NewEncoder(out).Encode(struct {
			Scheduler schedule.Status    `json:"scheduler"`
			LastJob   learning.JobStatus `json:"last_job"`
		}{m.Status(), learning.LoadJobStatus(c.Learning.StateDir)})
	}
	return fmt.Errorf("unknown learning automation command")
}
