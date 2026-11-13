// Package retrieval builds machine-local search indexes from Markdown notes.
package retrieval

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const Dimensions = 1024

// Two inputs share the 4096-token batch. Reserve headroom for runtime framing.
const embeddingBatch = 2
const embeddingContext = 4096
const passageTokens = embeddingContext/embeddingBatch - 16
const QueryInstruction = "Instruct: Given a search query, retrieve relevant passages that answer the query\nQuery: "

// LlamaConfig describes an explicitly installed Qwen3 embedding runtime.
// Model must be a Qwen3-Embedding-0.6B GGUF, not a chat or reranking model.
type LlamaConfig struct{ Executable, Model string }

type tailLog struct {
	sync.Mutex
	data []byte
}

func (l *tailLog) Write(p []byte) (int, error) {
	l.Lock()
	defer l.Unlock()
	l.data = append(l.data, p...)
	if len(l.data) > 8192 {
		l.data = append([]byte(nil), l.data[len(l.data)-8192:]...)
	}
	return len(p), nil
}
func (l *tailLog) String() string { l.Lock(); defer l.Unlock(); return string(l.data) }

// Llama is owned by one operation. Close terminates and reaps its process.
type Llama struct {
	cmd           *exec.Cmd
	client        *http.Client
	endpoint, key string
	done          chan struct{}
	waitErr       error
	log           tailLog
	once          sync.Once
}

func StartLlama(ctx context.Context, c LlamaConfig) (*Llama, error) {
	if c.Executable == "" || c.Model == "" {
		return nil, fmt.Errorf("embedding runtime and model paths are required")
	}
	// The server cannot inherit a listener. Authentication protects the brief
	// close/bind race; readiness also verifies the child has not exited.
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	key := make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		return nil, err
	}
	s := &Llama{key: hex.EncodeToString(key), endpoint: fmt.Sprintf("http://127.0.0.1:%d", port), done: make(chan struct{})}
	s.client = &http.Client{Timeout: 2 * time.Minute, Transport: &http.Transport{Proxy: nil}}
	s.cmd = exec.CommandContext(ctx, c.Executable, "--model", c.Model, "--embedding", "--pooling", "last", "--embd-normalize", "2", "--ctx-size", fmt.Sprint(embeddingContext), "--batch-size", fmt.Sprint(embeddingContext), "--ubatch-size", fmt.Sprint(embeddingContext), "--parallel", "1", "--host", "127.0.0.1", "--port", fmt.Sprint(port))
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "LLAMA_") {
			s.cmd.Env = append(s.cmd.Env, e)
		}
	}
	s.cmd.Env = append(s.cmd.Env, "LLAMA_API_KEY="+s.key)
	s.cmd.Stdout = &s.log
	s.cmd.Stderr = &s.log
	s.cmd.WaitDelay = 5 * time.Second
	if err = s.cmd.Start(); err != nil {
		return nil, fmt.Errorf("start embedding runtime: %w", err)
	}
	go func() { s.waitErr = s.cmd.Wait(); close(s.done) }()
	ready, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return nil, fmt.Errorf("embedding runtime exited during startup: %v\n%s", s.waitErr, s.log.String())
		case <-ready.Done():
			s.Close()
			return nil, fmt.Errorf("embedding runtime readiness: %w\n%s", ready.Err(), s.log.String())
		case <-ticker.C:
			probe, stop := context.WithTimeout(ready, time.Second)
			req, _ := http.NewRequestWithContext(probe, http.MethodGet, s.endpoint+"/health", nil)
			req.Header.Set("Authorization", "Bearer "+s.key)
			response, e := s.client.Do(req)
			if e == nil {
				io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
				response.Body.Close()
			}
			stop()
			if e == nil && response.StatusCode == http.StatusOK {
				select {
				case <-s.done:
					return nil, fmt.Errorf("embedding runtime exited: %v", s.waitErr)
				default:
					return s, nil
				}
			}
		}
	}
}

func (s *Llama) Close() {
	s.once.Do(func() {
		defer s.client.CloseIdleConnections()
		select {
		case <-s.done:
			return
		default:
		}
		_ = s.cmd.Process.Signal(os.Interrupt)
		select {
		case <-s.done:
		case <-time.After(5 * time.Second):
			_ = s.cmd.Process.Kill()
			<-s.done
		}
	})
}

// Fits counts tokenizer output only up to the passage ceiling. Streaming avoids
// allocating a token array for a large section that will be split anyway.
func (s *Llama) Fits(ctx context.Context, input string) (bool, error) {
	payload, err := json.Marshal(map[string]any{"content": input, "add_special": true})
	if err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint+"/tokenize", bytes.NewReader(payload))
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+s.key)
	req.Header.Set("Content-Type", "application/json")
	response, err := s.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("tokenize request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false, fmt.Errorf("tokenize request: HTTP %d (request ID %q)", response.StatusCode, response.Header.Get("X-Request-ID"))
	}
	dec := json.NewDecoder(io.LimitReader(response.Body, 256*1024))
	if token, err := dec.Token(); err != nil || token != json.Delim('{') {
		return false, fmt.Errorf("invalid tokenizer response object: %v", err)
	}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return false, fmt.Errorf("decode tokenizer: %w", err)
		}
		if key != "tokens" {
			var ignored json.RawMessage
			if err := dec.Decode(&ignored); err != nil {
				return false, err
			}
			continue
		}
		if token, err := dec.Token(); err != nil || token != json.Delim('[') {
			return false, fmt.Errorf("invalid tokenizer tokens array: %v", err)
		}
		count := 0
		for dec.More() {
			var token int
			if err := dec.Decode(&token); err != nil {
				return false, fmt.Errorf("decode tokenizer token: %w", err)
			}
			count++
			if count > passageTokens {
				return false, nil
			}
		}
		if _, err := dec.Token(); err != nil {
			return false, fmt.Errorf("decode tokenizer array: %w", err)
		}
		if count == 0 {
			return false, fmt.Errorf("tokenizer returned no tokens")
		}
		return true, nil
	}
	return false, fmt.Errorf("tokenizer response missing tokens")
}

// Embed accepts at most two bounded passages. Query callers must supply the
// Qwen retrieval instruction; indexed document passages are not instructed.
func (s *Llama) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if len(inputs) == 0 || len(inputs) > embeddingBatch {
		return nil, fmt.Errorf("embedding batch must contain 1 or 2 passages")
	}
	for _, input := range inputs {
		if len(input) == 0 {
			return nil, fmt.Errorf("embedding input is empty")
		}
		fits, err := s.Fits(ctx, input)
		if err != nil {
			return nil, err
		}
		if !fits {
			return nil, fmt.Errorf("embedding input exceeds %d-token budget", passageTokens)
		}
	}
	payload, _ := json.Marshal(map[string]any{"input": inputs, "model": "qwen3-embedding-0.6b"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint+"/v1/embeddings", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.key)
	req.Header.Set("Content-Type", "application/json")
	response, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 256*1024+1))
	if err != nil {
		return nil, err
	}
	if len(body) > 256*1024 {
		return nil, fmt.Errorf("embedding response exceeds size limit")
	}
	if response.StatusCode != http.StatusOK {
		// Do not echo provider bodies, which can include private input passages.
		return nil, fmt.Errorf("embedding request: HTTP %d (request ID %q)", response.StatusCode, response.Header.Get("X-Request-ID"))
	}
	var result struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err = json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode embeddings: %w", err)
	}
	if len(result.Data) != len(inputs) {
		return nil, fmt.Errorf("embedding response count mismatch")
	}
	vectors := make([][]float32, len(inputs))
	for _, item := range result.Data {
		if item.Index < 0 || item.Index >= len(inputs) || vectors[item.Index] != nil {
			return nil, fmt.Errorf("invalid embedding response index")
		}
		if err = validateVector(item.Embedding); err != nil {
			return nil, err
		}
		vectors[item.Index] = item.Embedding
	}
	return vectors, nil
}

func validateVector(v []float32) error {
	if len(v) != Dimensions {
		return fmt.Errorf("expected %d embedding dimensions, received %d", Dimensions, len(v))
	}
	var norm float64
	for _, x := range v {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			return fmt.Errorf("non-finite embedding")
		}
		norm += float64(x) * float64(x)
	}
	if norm == 0 {
		return fmt.Errorf("zero embedding")
	}
	// Normalize at the storage boundary even if the runtime also normalizes.
	norm = math.Sqrt(norm)
	for i := range v {
		v[i] = float32(float64(v[i]) / norm)
	}
	return nil
}
