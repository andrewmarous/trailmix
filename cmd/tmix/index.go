package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/andrewmarous/trailmix/config"
	"github.com/andrewmarous/trailmix/retrieval"
	"github.com/andrewmarous/trailmix/vault"
)

func runIndex(args []string, root string, cfg config.Retrieval, out, diagnostics io.Writer) error {
	flags := flag.NewFlagSet("index", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	flags.StringVar(&root, "vault", root, "Markdown vault directory")
	executable := flags.String("runtime", cfg.Runtime, "installed llama.cpp server executable")
	model := flags.String("model", cfg.Model, "installed Qwen3-Embedding-0.6B GGUF file")
	state := flags.String("state-dir", cfg.StateDir, "local index directory (default: machine cache, scoped to vault)")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("index takes no positional arguments")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	root = config.Expand(root, home)
	v, err := vault.Open(root)
	if err != nil {
		return err
	}
	cfg.Runtime = config.Expand(*executable, home)
	cfg.Model = config.Expand(*model, home)
	runtime, err := retrievalRuntime(cfg)
	if err != nil {
		return err
	}
	database, err := retrievalDatabase(v, config.Expand(*state, home))
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	report, err := retrieval.Index(ctx, v, retrieval.IndexOptions{Mode: cfg.Mode, Database: database, Runtime: runtime})
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(report)
}
