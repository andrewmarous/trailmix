package retrieval

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andrewmarous/trailmix/vault"
)

func openIndex(path string) (*sql.DB, error) {
	return openIndexContext(context.Background(), path)
}

type fakeEmbedder struct {
	closed bool
	inputs []string
}

func (f *fakeEmbedder) Close() { f.closed = true }
func (f *fakeEmbedder) Fits(_ context.Context, input string) (bool, error) {
	return len(input) <= passageTokens, nil
}
func (f *fakeEmbedder) Embed(_ context.Context, inputs []string) ([][]float32, error) {
	f.inputs = append(f.inputs, inputs...)
	result := make([][]float32, len(inputs))
	for i, s := range inputs {
		result[i] = make([]float32, Dimensions)
		if strings.Contains(s, "baseline") {
			result[i][0] = 1
		} else {
			result[i][1] = 1
		}
	}
	return result, nil
}
func fixture(t *testing.T) (*vault.Vault, string) {
	t.Helper()
	root := t.TempDir()
	v, err := vault.Open(filepath.Join(root, "vault"))
	if err != nil {
		t.Fatal(err)
	}
	if err = v.Init(); err != nil {
		t.Fatal(err)
	}
	return v, filepath.Join(root, "state", "index.sqlite")
}
func note(t *testing.T, v *vault.Vault, path, body string) {
	t.Helper()
	if err := v.Write(path, []byte(body), true); err != nil {
		t.Fatal(err)
	}
}

func TestIndexRejectsUnrelatedDatabase(t *testing.T) {
	_, path := fixture(t)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("CREATE TABLE unrelated(value TEXT); INSERT INTO unrelated VALUES('keep me')"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	opened, err := openIndex(path)
	if opened != nil {
		opened.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "not a Trailmix index") {
		t.Fatalf("unrelated database: %v", err)
	}
	db, err = sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var value string
	if err = db.QueryRow("SELECT value FROM unrelated").Scan(&value); err != nil || value != "keep me" {
		t.Fatalf("original data %q: %v", value, err)
	}
}

func TestIndexPersistsVectorAndBM25ResultsAndReusesEmbeddings(t *testing.T) {
	v, path := fixture(t)
	note(t, v, "safety", "# Publication\nCheck the baseline before overwriting.\n")
	note(t, v, "music", "# Music\nMix the tracks.\n")
	f := &fakeEmbedder{}
	r, err := build(context.Background(), v, path, "model-a", func(context.Context) (embedder, error) { return f, nil })
	if err != nil {
		t.Fatal(err)
	}
	if r.Embedded != 2 || r.Passages != 2 || !f.closed {
		t.Fatalf("report %+v, closed %v", r, f.closed)
	}
	db, err := openIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	var got string
	err = db.QueryRow(`SELECT p.path FROM passages p JOIN embeddings e ON e.hash=p.hash AND e.model=(SELECT value FROM metadata WHERE key='model') ORDER BY vec_distance_cosine(e.vector, ?) LIMIT 1`, vectorBytes(append([]float32{1}, make([]float32, Dimensions-1)...))).Scan(&got)
	if err != nil || got != "safety.md" {
		t.Fatalf("vector result %q: %v", got, err)
	}
	err = db.QueryRow(`SELECT path FROM passages_fts WHERE passages_fts MATCH 'baseline' ORDER BY bm25(passages_fts) LIMIT 1`).Scan(&got)
	if err != nil || got != "safety.md" {
		t.Fatalf("lexical result %q: %v", got, err)
	}
	db.Close()
	r, err = build(context.Background(), v, path, "model-a", func(context.Context) (embedder, error) { t.Fatal("unchanged index started inference"); return nil, nil })
	if err != nil || r.Reused != 2 || r.Embedded != 0 {
		t.Fatalf("repeat %+v: %v", r, err)
	}
	note(t, v, "music", "# Music\nA changed arrangement.\n")
	r, err = build(context.Background(), v, path, "model-a", func(context.Context) (embedder, error) { return &fakeEmbedder{}, nil })
	if err != nil || r.Embedded != 1 || r.Reused != 1 {
		t.Fatalf("incremental %+v: %v", r, err)
	}
	if err = os.Remove(filepath.Join(v.Root, "safety.md")); err != nil {
		t.Fatal(err)
	}
	r, err = build(context.Background(), v, path, "model-a", func(context.Context) (embedder, error) { t.Fatal("deletion started inference"); return nil, nil })
	if err != nil || r.Passages != 1 {
		t.Fatalf("delete %+v: %v", r, err)
	}
	db, err = openIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err = db.QueryRow("SELECT count(*) FROM passages_fts WHERE passages_fts MATCH 'baseline'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("deleted lexical entries %d: %v", count, err)
	}
}

func TestFailedModelChangePreservesActiveIndexAndResumes(t *testing.T) {
	v, path := fixture(t)
	note(t, v, "a", "# A\nFirst evidence.")
	note(t, v, "b", "# B\nSecond evidence.")
	note(t, v, "c", "# C\nThird evidence.")
	_, err := build(context.Background(), v, path, "old", func(context.Context) (embedder, error) { return &fakeEmbedder{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	f := &cancelEmbedder{cancel: cancel}
	_, err = build(ctx, v, path, "new", func(context.Context) (embedder, error) { return f, nil })
	if !errors.Is(err, context.Canceled) || !f.closed {
		t.Fatalf("cancel %v, closed %v", err, f.closed)
	}
	db, err := openIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	var model string
	if err = db.QueryRow("SELECT value FROM metadata WHERE key='model'").Scan(&model); err != nil || model != "old" {
		t.Fatalf("active model %q: %v", model, err)
	}
	db.Close()
	f2 := &failSecondEmbedder{}
	_, err = build(context.Background(), v, path, "new", func(context.Context) (embedder, error) { return f2, nil })
	if err == nil || !strings.Contains(err.Error(), "inference unavailable") || !f2.closed {
		t.Fatalf("failure %v, closed %v", err, f2.closed)
	}
	r, err := build(context.Background(), v, path, "new", func(context.Context) (embedder, error) { return &fakeEmbedder{}, nil })
	if err != nil || r.Embedded != 1 || r.Reused != 2 {
		t.Fatalf("resume %+v: %v", r, err)
	}
}

type failSecondEmbedder struct {
	fakeEmbedder
	batches int
}

func (f *failSecondEmbedder) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	f.batches++
	if f.batches == 2 {
		return nil, errors.New("test inference unavailable")
	}
	return f.fakeEmbedder.Embed(ctx, inputs)
}

type cancelEmbedder struct {
	fakeEmbedder
	cancel context.CancelFunc
}

func (f *cancelEmbedder) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	v, e := f.fakeEmbedder.Embed(ctx, inputs)
	f.cancel()
	return v, e
}

func TestIndexRejectsUnsafeStateAndIgnoresHiddenAndSymlinkNotes(t *testing.T) {
	v, path := fixture(t)
	note(t, v, "good", "# Visible\nUseful evidence.")
	if err := os.WriteFile(filepath.Join(v.Root, ".private.md"), []byte("hidden"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(v.Root, "good.md"), filepath.Join(v.Root, "alias.md")); err != nil {
		t.Skip(err)
	}
	r, err := build(context.Background(), v, path, "model", func(context.Context) (embedder, error) { return &fakeEmbedder{}, nil })
	if err != nil || r.Passages != 1 {
		t.Fatalf("visible notes %+v: %v", r, err)
	}
	_, err = build(context.Background(), v, filepath.Join(v.Root, "index.sqlite"), "model", nil)
	if err == nil || !strings.Contains(err.Error(), "outside the vault") {
		t.Fatalf("inside vault: %v", err)
	}
	vaultAlias := filepath.Join(t.TempDir(), "vault-alias")
	if err = os.Symlink(v.Root, vaultAlias); err != nil {
		t.Fatal(err)
	}
	_, err = build(context.Background(), v, filepath.Join(vaultAlias, "nested", "index.sqlite"), "model", nil)
	if err == nil || !strings.Contains(err.Error(), "outside the vault") {
		t.Fatalf("aliased vault: %v", err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err = os.Symlink(filepath.Dir(path), alias); err != nil {
		t.Fatal(err)
	}
	_, err = build(context.Background(), v, filepath.Join(alias, "index.sqlite"), "model", nil)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink index: %v", err)
	}
}
