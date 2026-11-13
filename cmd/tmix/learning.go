package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/andrewmarous/trailmix/config"
	"github.com/andrewmarous/trailmix/learning"
)

func runLearning(ctx context.Context, args []string, root string, cfg config.Config, configPath string, in io.Reader, out, diagnostics io.Writer) error {
	flags := flag.NewFlagSet("learning", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	flags.StringVar(&root, "vault", root, "vault directory")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			_, err = io.WriteString(out, usage)
			return err
		}
		return err
	}
	args = flags.Args()
	if len(args) == 0 || args[0] == "help" {
		_, err := io.WriteString(out, usage)
		return err
	}
	switch args[0] {
	case "run", "doctor", "schedule", "accept", "navigation":
		return runAutomation(ctx, args, root, cfg, configPath, out, diagnostics)
	case "sessions":
		return runSessions(ctx, append([]string{"--state-dir", cfg.Learning.StateDir}, args[1:]...), in, out, diagnostics, cfg.Learning.Sources)
	case "add":
		flags := flag.NewFlagSet("learning add", flag.ContinueOnError)
		flags.SetOutput(diagnostics)
		overwrite := flags.Bool("overwrite", false, "replace existing guidance")
		if err := flags.Parse(args[1:]); err != nil {
			if err == flag.ErrHelp {
				return nil
			}
			return err
		}
		if flags.NArg() != 1 {
			return fmt.Errorf("usage: tmix learning add [--overwrite] NOTE < guidance.md")
		}
		content, err := io.ReadAll(io.LimitReader(in, 64*1024+1))
		if canceled := ctx.Err(); canceled != nil {
			return canceled
		}
		if err != nil {
			return err
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		return learning.AddGuidance(config.Expand(root, home), flags.Arg(0), string(content), *overwrite)
	default:
		return fmt.Errorf("unknown learning command %q", args[0])
	}
}
