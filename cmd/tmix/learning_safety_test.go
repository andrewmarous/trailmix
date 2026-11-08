package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestLearningConcurrentRecords(t *testing.T) {
	root := t.TempDir()
	var wg sync.WaitGroup
	errors := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var out bytes.Buffer
			errors <- run([]string{"--vault", root, "learning", "record", fmt.Sprint(i)}, strings.NewReader("observed result"), &out, &out)
		}(i)
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if err := run([]string{"--vault", root, "learning", "status"}, strings.NewReader(""), &out, &out); err != nil {
		t.Fatal(err)
	}
	var ledger learningLedger
	if err := json.Unmarshal(out.Bytes(), &ledger); err != nil {
		t.Fatal(err)
	}
	if len(ledger.Sessions) != 12 {
		t.Fatalf("lost records: %d", len(ledger.Sessions))
	}
	out.Reset()
	if err := run([]string{"--vault", root, "search", "observed"}, strings.NewReader(""), &out, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "observed result") {
		t.Fatal("ledger evidence leaked into note search")
	}
}

func TestLearningPreservesCorruptLedger(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".learning")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "ledger.json")
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"--vault", root, "learning", "record", "a"}, strings.NewReader("evidence"), &out, &out); err == nil {
		t.Fatal("corruption ignored")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "broken" {
		t.Fatal("corrupt ledger overwritten")
	}
}
