package retrieval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestEmbeddingResponseReordersNormalizesAndRejectsInvalidVectors(t *testing.T) {
	invalid := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/tokenize" {
			fmt.Fprint(w, `{"tokens":[1,2,3]}`)
			return
		}
		if r.URL.Path != "/v1/embeddings" || r.Header.Get("Authorization") != "Bearer test" {
			http.Error(w, "bad request", 400)
			return
		}
		var body struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if strings.Join(body.Input, ",") != "first,second" {
			t.Errorf("inputs: %v", body.Input)
		}
		a, b := make([]float32, Dimensions), make([]float32, Dimensions)
		a[0] = 3
		b[1] = 2
		if invalid {
			b = b[:10]
		}
		json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"index": 1, "embedding": b}, map[string]any{"index": 0, "embedding": a}}})
	}))
	defer server.Close()
	s := &Llama{endpoint: server.URL, key: "test", client: server.Client()}
	vectors, err := s.Embed(context.Background(), []string{"first", "second"})
	if err != nil {
		t.Fatal(err)
	}
	if vectors[0][0] != 1 || vectors[0][1] != 0 || vectors[1][1] != 1 {
		t.Fatalf("incorrect normalized ordering: %v %v", vectors[0][:2], vectors[1][:2])
	}
	invalid = true
	_, err = s.Embed(context.Background(), []string{"first", "second"})
	if err == nil || !strings.Contains(err.Error(), "dimensions") {
		t.Fatalf("invalid dimensions: %v", err)
	}
}

func TestTokenizerEnforcesCompleteInputBudgetAndRejectsMalformedOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Content    string `json:"content"`
			AddSpecial bool   `json:"add_special"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
			return
		}
		if r.URL.Path != "/tokenize" || !input.AddSpecial || r.Header.Get("Authorization") != "Bearer test" {
			t.Error("incorrect tokenizer request")
		}
		switch input.Content {
		case "fits":
			json.NewEncoder(w).Encode(map[string]any{"tokens": make([]int, 2032)})
		case "too large":
			json.NewEncoder(w).Encode(map[string]any{"tokens": make([]int, 2033)})
		default:
			fmt.Fprint(w, `{"tokens":["invalid"]}`)
		}
	}))
	defer server.Close()
	s := &Llama{endpoint: server.URL, key: "test", client: server.Client()}
	fits, err := s.Fits(context.Background(), "fits")
	if err != nil || !fits {
		t.Fatalf("within budget fits=%v: %v", fits, err)
	}
	fits, err = s.Fits(context.Background(), "too large")
	if err != nil || fits {
		t.Fatalf("over budget fits=%v: %v", fits, err)
	}
	_, err = s.Fits(context.Background(), "broken")
	if err == nil || !strings.Contains(err.Error(), "decode tokenizer token") {
		t.Fatalf("invalid tokens: %v", err)
	}
}

func TestRuntimeStartupFailureIncludesDiagnostics(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("shell fixture")
	}
	path := t.TempDir() + "/runtime"
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho 'unsupported model architecture' >&2\nexit 7\n"), 0700); err != nil {
		t.Fatal(err)
	}
	_, err := StartLlama(context.Background(), LlamaConfig{Executable: path, Model: "unused.gguf"})
	if err == nil || !strings.Contains(err.Error(), "unsupported model architecture") {
		t.Fatalf("startup failure: %v", err)
	}
}

func TestCancelStartupAllowsGracefulRuntimeExit(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("shell fixture")
	}
	dir := t.TempDir()
	path := dir + "/runtime"
	script := fmt.Sprintf("#!/bin/sh\ntrap 'sleep 0.1; echo graceful > %q; exit 0' INT\necho ready > %q\nwhile :; do sleep 0.05; done\n", dir+"/stopped", dir+"/ready")
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := StartLlama(ctx, LlamaConfig{Executable: path, Model: "unused.gguf"})
		finished <- err
	}()
	deadline := time.After(8 * time.Second)
	for {
		if _, err := os.Stat(dir + "/ready"); err == nil {
			break
		}
		select {
		case <-deadline:
			t.Fatal("runtime did not start")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation: %v", err)
		}
	case <-deadline:
		t.Fatal("startup cancellation did not finish")
	}
	body, err := os.ReadFile(dir + "/stopped")
	if err != nil || string(body) != "graceful\n" {
		t.Fatalf("runtime did not shut down gracefully: %q, %v", body, err)
	}
}

// Opt-in: downloads and inference are never part of ordinary tests.
func TestLiveQwenEmbeddings(t *testing.T) {
	executable, model := os.Getenv("TMIX_TEST_LLAMA"), os.Getenv("TMIX_TEST_MODEL")
	if executable == "" || model == "" {
		t.Skip("set TMIX_TEST_LLAMA and TMIX_TEST_MODEL for local inference")
	}
	s, err := StartLlama(context.Background(), LlamaConfig{Executable: executable, Model: model})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	vectors, err := s.Embed(context.Background(), []string{"Publication baseline checks prevent overwriting newer changes.", QueryInstruction + "How do I prevent concurrent overwrites?"})
	if err != nil {
		t.Fatal(err)
	}
	// A full two-passage batch must fit the configured shared batch/context.
	long := strings.Repeat(" safety", 2030)
	large, err := s.Embed(context.Background(), []string{long, long})
	if err != nil || len(large) != 2 {
		t.Fatalf("full-length batch: %v", err)
	}
	tooLong, err := s.Fits(context.Background(), strings.Repeat(" safety", 2100))
	if err != nil || tooLong {
		t.Fatalf("oversized input fits=%v: %v", tooLong, err)
	}
	var similarity float32
	for i, x := range vectors[0] {
		similarity += x * vectors[1][i]
	}
	if similarity < 0.3 {
		t.Fatalf("unexpectedly low related-passage similarity: %f", similarity)
	}
	s.Close()
	select {
	case <-s.done:
	default:
		t.Fatal("runtime still running after Close")
	}
	t.Log(fmt.Sprintf("dimensions=%d related_similarity=%.4f process_reaped=true", len(vectors[0]), similarity))
}
