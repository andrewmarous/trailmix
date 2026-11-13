package retrieval

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/andrewmarous/trailmix/vault"
)

func TestTextIndexAndSearchNeedNoModelAndPreserveInterface(t *testing.T) {
	v, path := fixture(t)
	note(t, v, "safety", "# Safety\n\n## Checks\nCheck the baseline.\n\n## Evidence\nRetain baseline evidence.\n")
	note(t, v, "music", "# Music\nMix audio tracks.\n")
	report, err := Index(context.Background(), v, IndexOptions{Mode: "text", Database: path})
	if err != nil || report.Embedded != 0 || report.Passages != 3 {
		t.Fatalf("text build %+v: %v", report, err)
	}
	result, err := Search(context.Background(), v, SearchOptions{Database: path, Mode: "text", Query: "baseline missing", Limit: 10})
	want := []vault.Result{{Path: "safety.md", Line: 4, Snippet: "Check the baseline."}}
	if err != nil || !reflect.DeepEqual(result.Results, want) || len(result.Warnings) != 0 {
		t.Fatalf("text result %+v: %v", result, err)
	}
	for _, q := range []string{"\" OR * NOT baseline", `baseline:foo`, "!!!"} {
		if _, err = Search(context.Background(), v, SearchOptions{Database: path, Mode: "text", Query: q, Limit: 10}); err != nil {
			t.Fatalf("literal query %q: %v", q, err)
		}
	}
}

func TestSemanticQuerySnippetDoesNotTreatLettersAsWordMatches(t *testing.T) {
	line, snippet := searchSnippet("How can I avoid clobbering someone else's edits?", "# Publication safety\nCheck newer rules.\n", 1)
	if line != 2 || snippet != "Check newer rules." {
		t.Fatalf("snippet line=%d text=%q", line, snippet)
	}
}

func TestHybridFusesRanksRatherThanRawScores(t *testing.T) {
	v, path := fixture(t)
	note(t, v, "a", "# A\nbaseline baseline baseline baseline baseline.\n")
	note(t, v, "b", "# B\nCheck the baseline.\n")
	note(t, v, "c", "# C\nKeep unrelated evidence.\n")
	_, err := build(context.Background(), v, path, "model", func(context.Context) (embedder, error) { return &fakeEmbedder{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	db, err := openIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	for path, xy := range map[string][2]float32{"a.md": {0.1, 0.99}, "b.md": {1, 0}, "c.md": {0.9, 0.1}} {
		vector := make([]float32, Dimensions)
		vector[0], vector[1] = xy[0], xy[1]
		_, err = db.Exec("UPDATE embeddings SET vector=? WHERE hash=(SELECT hash FROM passages WHERE path=?)", vectorBytes(vector), path)
		if err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	runtime := &fakeEmbedder{}
	result, err := search(context.Background(), v, SearchOptions{Database: path, Mode: "hybrid", Query: "baseline", Limit: 3}, func() (string, error) { return "model", nil }, func(context.Context) (embedder, error) { return runtime, nil })
	want := []vault.Result{{Path: "b.md", Line: 2, Snippet: "Check the baseline."}, {Path: "a.md", Line: 2, Snippet: "baseline baseline baseline baseline baseline."}, {Path: "c.md", Line: 2, Snippet: "Keep unrelated evidence."}}
	if err != nil || !reflect.DeepEqual(result.Results, want) || !runtime.closed {
		t.Fatalf("fused results %+v, closed=%v: %v", result, runtime.closed, err)
	}
	if !reflect.DeepEqual(runtime.inputs, []string{QueryInstruction + "baseline"}) {
		t.Fatalf("query embedding input: %v", runtime.inputs)
	}
}

func TestSearchSkipsStaleDeletedAndUnsafeNotesButReturnsValidSources(t *testing.T) {
	v, path := fixture(t)
	for _, name := range []string{"changed", "deleted", "linked", "valid"} {
		note(t, v, name, "# Safety\nCheck the baseline.\n")
	}
	_, err := Index(context.Background(), v, IndexOptions{Mode: "text", Database: path})
	if err != nil {
		t.Fatal(err)
	}
	note(t, v, "changed", "# Safety\nA different rule.\n")
	if err = os.Remove(filepath.Join(v.Root, "deleted.md")); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(v.Root, "linked.md")); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(filepath.Join(v.Root, "valid.md"), filepath.Join(v.Root, "linked.md")); err != nil {
		t.Fatal(err)
	}
	result, err := Search(context.Background(), v, SearchOptions{Database: path, Mode: "text", Query: "baseline", Limit: 10})
	want := []vault.Result{{Path: "valid.md", Line: 2, Snippet: "Check the baseline."}}
	if err != nil || !reflect.DeepEqual(result.Results, want) || len(result.Warnings) != 3 {
		t.Fatalf("validated result %+v: %v", result, err)
	}
	if err = os.Remove(filepath.Join(v.Root, "valid.md")); err != nil {
		t.Fatal(err)
	}
	result, err = Search(context.Background(), v, SearchOptions{Database: path, Mode: "text", Query: "baseline", Limit: 10})
	if err != nil || len(result.Results) != 0 || len(result.Warnings) != 5 || result.Warnings[4] != "no valid indexed matches remain; run tmix index" {
		t.Fatalf("all stale %+v: %v", result, err)
	}
}

func TestSearchIsReadOnlyAndRejectsMismatchedIndexes(t *testing.T) {
	v, path := fixture(t)
	_, err := Search(context.Background(), v, SearchOptions{Database: path, Mode: "text", Query: "baseline", Limit: 10})
	if err == nil || !strings.Contains(err.Error(), "run tmix index") {
		t.Fatalf("missing index: %v", err)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("search created database: %v", err)
	}
	note(t, v, "safety", "# Safety\nCheck the baseline.\n")
	_, err = build(context.Background(), v, path, "model", func(context.Context) (embedder, error) { return &fakeEmbedder{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = search(context.Background(), v, SearchOptions{Database: path, Mode: "hybrid", Query: "baseline", Limit: 10}, func() (string, error) { return "different model", nil }, func(context.Context) (embedder, error) { t.Fatal("mismatch started runtime"); return nil, nil })
	if err == nil || !strings.Contains(err.Error(), "identity does not match") {
		t.Fatalf("model mismatch: %v", err)
	}
	result, err := Search(context.Background(), v, SearchOptions{Database: path, Mode: "text", Query: "baseline", Limit: 10})
	if err != nil || len(result.Results) != 1 || result.Results[0].Path != "safety.md" {
		t.Fatalf("text on hybrid index %+v: %v", result, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("search modified database: %v", err)
	}
	other, err := vault.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = Search(context.Background(), other, SearchOptions{Database: path, Mode: "text", Query: "baseline", Limit: 10})
	if err == nil || !strings.Contains(err.Error(), "different vault") {
		t.Fatalf("vault mismatch: %v", err)
	}
	for _, options := range []SearchOptions{
		{Database: path, Mode: "text", Query: " ", Limit: 10},
		{Database: path, Mode: "text", Query: "baseline", Limit: 0},
		{Database: path, Mode: "other", Query: "baseline", Limit: 10},
	} {
		if _, err = Search(context.Background(), v, options); err == nil {
			t.Fatalf("invalid search accepted: %+v", options)
		}
	}
	db, err := openIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("PRAGMA user_version=1")
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	before, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Search(context.Background(), v, SearchOptions{Database: path, Mode: "text", Query: "baseline", Limit: 10})
	if err == nil || !strings.Contains(err.Error(), "schema=1") {
		t.Fatalf("obsolete schema accepted: %v", err)
	}
	after, err = os.ReadFile(path)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("search migrated obsolete database: %v", err)
	}
}

func TestSearchFailsOnSourceReadErrors(t *testing.T) {
	v, path := fixture(t)
	note(t, v, "safety", "# Safety\nCheck the baseline.\n")
	if _, err := Index(context.Background(), v, IndexOptions{Mode: "text", Database: path}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(v.Root, "safety.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(v.Root, "safety.md"), 0700); err != nil {
		t.Fatal(err)
	}
	_, err := Search(context.Background(), v, SearchOptions{Database: path, Mode: "text", Query: "baseline", Limit: 10})
	if err == nil || !strings.Contains(err.Error(), "validate search source") || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("source read error: %v", err)
	}
}

type rebuildingQueryEmbedder struct {
	fakeEmbedder
	rebuild func() error
}

func (f *rebuildingQueryEmbedder) Embed(ctx context.Context, input []string) ([][]float32, error) {
	if err := f.rebuild(); err != nil {
		return nil, err
	}
	return f.fakeEmbedder.Embed(ctx, input)
}

func TestSearchAllowsIndexReplacementDuringInference(t *testing.T) {
	for _, replacement := range []string{"model", "different"} {
		t.Run(replacement, func(t *testing.T) {
			v, path := fixture(t)
			note(t, v, "safety", "# Safety\nCheck the baseline.\n")
			if _, err := build(context.Background(), v, path, "model", func(context.Context) (embedder, error) { return &fakeEmbedder{}, nil }); err != nil {
				t.Fatal(err)
			}
			runtime := &rebuildingQueryEmbedder{rebuild: func() error {
				note(t, v, "safety", "# Safety\nA revised baseline.\n")
				_, err := build(context.Background(), v, path, replacement, func(context.Context) (embedder, error) { return &fakeEmbedder{}, nil })
				return err
			}}
			result, err := search(context.Background(), v, SearchOptions{Database: path, Mode: "hybrid", Query: "baseline", Limit: 10}, func() (string, error) { return "model", nil }, func(context.Context) (embedder, error) { return runtime, nil })
			if !runtime.closed {
				t.Fatal("query runtime remains open")
			}
			if replacement == "different" {
				if err == nil || !strings.Contains(err.Error(), "identity changed") {
					t.Fatalf("replacement mismatch: %v", err)
				}
				return
			}
			want := []vault.Result{{Path: "safety.md", Line: 2, Snippet: "A revised baseline."}}
			if err != nil || !reflect.DeepEqual(result.Results, want) {
				t.Fatalf("new generation %+v: %v", result, err)
			}
		})
	}
}

func TestLiveHybridSearch(t *testing.T) {
	executable, model := os.Getenv("TMIX_TEST_LLAMA"), os.Getenv("TMIX_TEST_MODEL")
	if executable == "" || model == "" {
		t.Skip("set TMIX_TEST_LLAMA and TMIX_TEST_MODEL for local inference")
	}
	v, path := fixture(t)
	note(t, v, "safety", "# Publication safety\nCheck publication baselines before overwriting newer changes.\n")
	note(t, v, "music", "# Music\nMix audio tracks and adjust the equalizer.\n")
	runtime := LlamaConfig{Executable: executable, Model: model}
	if _, err := Index(context.Background(), v, IndexOptions{Database: path, Runtime: runtime}); err != nil {
		t.Fatal(err)
	}
	result, err := Search(context.Background(), v, SearchOptions{Database: path, Mode: "hybrid", Query: "How can I avoid clobbering someone else's edits?", Limit: 2, Runtime: runtime})
	want := vault.Result{Path: "safety.md", Line: 2, Snippet: "Check publication baselines before overwriting newer changes."}
	if err != nil || len(result.Results) != 2 || !reflect.DeepEqual(result.Results[0], want) {
		t.Fatalf("live hybrid result %+v: %v", result, err)
	}
	t.Logf("first=%s line=%d results=%d warnings=%d", result.Results[0].Path, result.Results[0].Line, len(result.Results), len(result.Warnings))
}

type failingQueryEmbedder struct{ fakeEmbedder }

func (f *failingQueryEmbedder) Embed(context.Context, []string) ([][]float32, error) {
	return nil, errors.New("test query inference failed")
}

func TestQueryRuntimeClosesOnFailureAndCancellation(t *testing.T) {
	f := &failingQueryEmbedder{}
	_, err := embedQuery(context.Background(), "baseline", func(context.Context) (embedder, error) { return f, nil })
	if err == nil || !strings.Contains(err.Error(), "test query inference failed") || !f.closed {
		t.Fatalf("query failure %v, closed=%v", err, f.closed)
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &cancelEmbedder{cancel: cancel}
	_, err = embedQuery(ctx, "baseline", func(context.Context) (embedder, error) { return c, nil })
	if !errors.Is(err, context.Canceled) || !c.closed {
		t.Fatalf("cancelled query %v closed=%v", err, c.closed)
	}
}
