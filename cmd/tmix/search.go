package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/andrewmarous/trailmix/config"
	"github.com/andrewmarous/trailmix/retrieval"
	"github.com/andrewmarous/trailmix/vault"
)

func retrievalDatabase(v *vault.Vault, state string) (string, error) {
	if state == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		state = filepath.Join(cache, "trailmix", "retrieval", fmt.Sprintf("%x", sha256.Sum256([]byte(v.Root))))
	}
	return filepath.Join(state, "index.sqlite"), nil
}

func retrievalRuntime(c config.Retrieval) (retrieval.LlamaConfig, error) {
	if c.Mode != "text" && c.Mode != "hybrid" {
		return retrieval.LlamaConfig{}, fmt.Errorf("retrieval.mode must be hybrid or text")
	}
	if c.Mode == "text" {
		return retrieval.LlamaConfig{}, nil
	}
	if c.Model == "" {
		return retrieval.LlamaConfig{}, fmt.Errorf("hybrid retrieval requires retrieval.model (installed Qwen3-Embedding-0.6B GGUF); set retrieval.mode = \"text\" for BM25 without a model")
	}
	executable, err := exec.LookPath(c.Runtime)
	if err != nil {
		return retrieval.LlamaConfig{}, fmt.Errorf("embedding runtime: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return retrieval.LlamaConfig{}, err
	}
	return retrieval.LlamaConfig{Executable: executable, Model: c.Model}, nil
}

func runSearch(ctx context.Context, v *vault.Vault, c config.Retrieval, query string, limit int, jsonOutput bool, out, diagnostics io.Writer) error {
	database, err := retrievalDatabase(v, c.StateDir)
	if err != nil {
		return err
	}
	runtime, err := retrievalRuntime(c)
	if err != nil {
		return err
	}
	report, err := retrieval.Search(ctx, v, retrieval.SearchOptions{Database: database, Mode: c.Mode, Query: query, Limit: limit, Runtime: runtime})
	if err != nil {
		return err
	}
	for _, warning := range report.Warnings {
		if _, err = fmt.Fprintln(diagnostics, "tmix: warning:", warning); err != nil {
			return err
		}
	}
	if jsonOutput {
		return json.NewEncoder(out).Encode(report.Results)
	}
	for _, r := range report.Results {
		if _, err = fmt.Fprintf(out, "%s:%d: %s\n", r.Path, r.Line, r.Snippet); err != nil {
			return err
		}
	}
	return nil
}
