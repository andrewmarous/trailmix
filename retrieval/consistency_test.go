package retrieval

import (
	"context"
	"strings"
	"testing"

	"github.com/andrewmarous/trailmix/vault"
)

type editingEmbedder struct {
	fakeEmbedder
	vault *vault.Vault
}

func (f *editingEmbedder) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if err := f.vault.Write("safety", []byte("# Safety\nChanged during inference."), true); err != nil {
		return nil, err
	}
	return f.fakeEmbedder.Embed(ctx, inputs)
}

func TestConcurrentNoteEditDoesNotReplaceActiveIndex(t *testing.T) {
	v, path := fixture(t)
	note(t, v, "safety", "# Safety\nOriginal baseline.")
	_, err := build(context.Background(), v, path, "model", func(context.Context) (embedder, error) { return &fakeEmbedder{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	note(t, v, "safety", "# Safety\nNew baseline.")
	f := &editingEmbedder{vault: v}
	_, err = build(context.Background(), v, path, "model", func(context.Context) (embedder, error) { return f, nil })
	if err == nil || !strings.Contains(err.Error(), "vault changed") || !f.closed {
		t.Fatalf("edit result: %v, closed %v", err, f.closed)
	}
	db, err := openIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	var text string
	err = db.QueryRow("SELECT text FROM passages WHERE path='safety.md'").Scan(&text)
	db.Close()
	if err != nil || text != "# Safety\nOriginal baseline." {
		t.Fatalf("active text %q: %v", text, err)
	}
	// A retry can use the completed batch without loading a model again.
	note(t, v, "safety", "# Safety\nNew baseline.")
	report, err := build(context.Background(), v, path, "model", func(context.Context) (embedder, error) { t.Fatal("retry started inference"); return nil, nil })
	if err != nil || report.Reused != 1 || report.Embedded != 0 {
		t.Fatalf("retry %+v: %v", report, err)
	}
}
