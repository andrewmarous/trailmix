package retrieval

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/andrewmarous/trailmix/vault"
)

type SearchOptions struct {
	Database string
	Mode     string
	Query    string
	Limit    int
	Runtime  LlamaConfig
}

type SearchReport struct {
	Results  []vault.Result
	Warnings []string
}

type candidate struct {
	id int
	passage
	score float64
}

// Search reads an existing index. It never builds, migrates, or repairs one.
func Search(ctx context.Context, v *vault.Vault, o SearchOptions) (SearchReport, error) {
	return search(ctx, v, o, func() (string, error) { return runtimeIdentity(o.Runtime) }, func(ctx context.Context) (embedder, error) { return StartLlama(ctx, o.Runtime) })
}

func openSearchIndex(ctx context.Context, path string) (*sql.DB, error) {
	if path == "" {
		return nil, fmt.Errorf("index database path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	for _, p := range []string{abs, abs + "-journal", abs + "-wal", abs + "-shm"} {
		if err = privatePath(p); err != nil {
			return nil, err
		}
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("open retrieval index; run tmix index if missing: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("index database is not a regular file: %s", abs)
	}
	u := url.URL{Scheme: "file", Path: abs}
	q := u.Query()
	q.Set("mode", "ro")
	q.Set("_pragma", "busy_timeout(5000)")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite3", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	var app, version int
	if err = db.QueryRowContext(ctx, "PRAGMA application_id").Scan(&app); err == nil {
		err = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version)
	}
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("open retrieval index; run tmix index if missing: %w", err)
	}
	if app != 0x544d4958 || version != 2 {
		db.Close()
		return nil, fmt.Errorf("not a current Trailmix index (application=%x schema=%d); run tmix index", app, version)
	}
	return db, nil
}

func searchMetadata(ctx context.Context, tx *sql.Tx, root string) (string, error) {
	var owner, format, model string
	err := tx.QueryRowContext(ctx, `SELECT (SELECT value FROM metadata WHERE key='vault'),(SELECT value FROM metadata WHERE key='format'),(SELECT value FROM metadata WHERE key='model')`).Scan(&owner, &format, &model)
	if err != nil {
		return "", fmt.Errorf("read active retrieval generation; run tmix index: %w", err)
	}
	if owner != root {
		return "", fmt.Errorf("index belongs to a different vault")
	}
	if format != passageFormat {
		return "", fmt.Errorf("unsupported passage format %q; run tmix index", format)
	}
	return model, nil
}

func search(ctx context.Context, v *vault.Vault, o SearchOptions, identity func() (string, error), start func(context.Context) (embedder, error)) (report SearchReport, err error) {
	report.Results = []vault.Result{}
	if strings.TrimSpace(o.Query) == "" || !utf8.ValidString(o.Query) {
		return report, fmt.Errorf("search query must be nonempty UTF-8 text")
	}
	if o.Limit < 1 || o.Limit > 100 {
		return report, fmt.Errorf("limit must be between 1 and 100")
	}
	if o.Mode != "text" && o.Mode != "hybrid" {
		return report, fmt.Errorf("retrieval mode must be hybrid or text")
	}
	info, err := os.Lstat(v.Root)
	if err != nil {
		return report, err
	}
	if !info.IsDir() {
		return report, fmt.Errorf("vault must be a directory, not a symlink")
	}
	root, err := filepath.EvalSymlinks(v.Root)
	if err != nil {
		return report, err
	}
	if o.Database == "" {
		return report, fmt.Errorf("index database path is required")
	}
	location, err := filepath.Abs(o.Database)
	if err != nil {
		return report, err
	}
	location, err = canonicalLocation(location)
	if err != nil {
		return report, err
	}
	rel, err := filepath.Rel(root, location)
	if err != nil {
		return report, err
	}
	if !isOutside(rel) {
		return report, fmt.Errorf("index database must be outside the vault")
	}
	db, err := openSearchIndex(ctx, o.Database)
	if err != nil {
		return report, err
	}
	defer db.Close()
	// Validate ownership before loading a model, but do not hold a read lock
	// during model startup or inference: builders must still be able to commit.
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return report, err
	}
	active, err := searchMetadata(ctx, tx, root)
	tx.Rollback()
	if err != nil {
		return report, err
	}
	var expected string
	var queryVector []float32
	if o.Mode == "hybrid" {
		if active == textIdentity {
			return report, fmt.Errorf("index has no embeddings; configure hybrid retrieval and run tmix index")
		}
		expected, err = identity()
		if err != nil {
			return report, err
		}
		if active != expected {
			return report, fmt.Errorf("index embedding identity does not match the configured model/runtime; run tmix index")
		}
		queryVector, err = embedQuery(ctx, o.Query, start)
		if err != nil {
			return report, err
		}
	}
	tx, err = db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return report, err
	}
	defer tx.Rollback()
	active, err = searchMetadata(ctx, tx, root)
	if err != nil {
		return report, err
	}
	if o.Mode == "hybrid" && active != expected {
		return report, fmt.Errorf("index embedding identity changed during search; retry with matching model/runtime")
	}
	lexical, err := rankedCandidates(ctx, tx, `SELECT p.id,p.path,p.line,p.start_byte,p.end_byte,p.source_hash,p.section_start,p.section_end
 FROM passages_fts JOIN passages p ON p.id=passages_fts.rowid WHERE passages_fts MATCH ?
 ORDER BY bm25(passages_fts),p.path,p.start_byte,p.id LIMIT 100`, ftsQuery(o.Query))
	if err != nil {
		return report, fmt.Errorf("BM25 search: %w", err)
	}
	var semantic []candidate
	if o.Mode == "hybrid" {
		semantic, err = rankedCandidates(ctx, tx, `SELECT p.id,p.path,p.line,p.start_byte,p.end_byte,p.source_hash,p.section_start,p.section_end
 FROM passages p JOIN embeddings e ON e.hash=p.hash AND e.model=?
 ORDER BY vec_distance_cosine(e.vector,?),p.path,p.start_byte,p.id LIMIT 100`, active, vectorBytes(queryVector))
		if err != nil {
			return report, fmt.Errorf("vector search: %w", err)
		}
	}
	// Both rankings now belong to the same generation; release SQLite before I/O.
	if err = tx.Commit(); err != nil {
		return report, err
	}
	ranked := fuse(lexical, semantic)
	seen := map[string]bool{}
	for _, c := range ranked {
		if err = ctx.Err(); err != nil {
			return report, err
		}
		if seen[c.Path] {
			continue
		}
		seen[c.Path] = true
		body, e := v.Read(c.Path)
		if errors.Is(e, os.ErrNotExist) || errors.Is(e, vault.ErrUnsafePath) {
			report.Warnings = append(report.Warnings, fmt.Sprintf("excluded %q: %v; run tmix index", c.Path, e))
			continue
		}
		if e != nil {
			return report, fmt.Errorf("validate search source %q: %w", c.Path, e)
		}
		if hash(body) != c.SourceHash {
			report.Warnings = append(report.Warnings, fmt.Sprintf("excluded changed note %q; run tmix index", c.Path))
			continue
		}
		if c.Start < 0 || c.End <= c.Start || c.End > len(body) || c.SectionStart < 0 || c.SectionStart > c.Start || c.SectionEnd < c.End || c.SectionEnd > len(body) || c.Line != 1+strings.Count(string(body[:c.Start]), "\n") {
			report.Warnings = append(report.Warnings, fmt.Sprintf("excluded invalid source pointer for %q; run tmix index", c.Path))
			continue
		}
		line, snippet := searchSnippet(o.Query, string(body[c.Start:c.End]), c.Line)
		report.Results = append(report.Results, vault.Result{Path: c.Path, Line: line, Snippet: snippet})
		if len(report.Results) == o.Limit {
			break
		}
	}
	if len(report.Results) == 0 && len(report.Warnings) > 0 {
		report.Warnings = append(report.Warnings, "no valid indexed matches remain; run tmix index")
	}
	return report, nil
}

func embedQuery(ctx context.Context, query string, start func(context.Context) (embedder, error)) ([]float32, error) {
	runtime, err := start(ctx)
	if err != nil {
		return nil, fmt.Errorf("start query embedding runtime: %w", err)
	}
	defer runtime.Close()
	vectors, err := runtime.Embed(ctx, []string{QueryInstruction + query})
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return nil, fmt.Errorf("embed search query: %w", err)
	}
	if len(vectors) != 1 {
		return nil, fmt.Errorf("query embedding returned %d vectors, expected one", len(vectors))
	}
	if err = validateVector(vectors[0]); err != nil {
		return nil, fmt.Errorf("query embedding: %w", err)
	}
	return vectors[0], nil
}

func ftsQuery(query string) string {
	terms := strings.Fields(query)
	for i, t := range terms {
		terms[i] = "\"" + strings.ReplaceAll(t, "\"", "\"\"") + "\""
	}
	return strings.Join(terms, " OR ")
}

func rankedCandidates(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]candidate, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []candidate
	for rows.Next() {
		var c candidate
		if err = rows.Scan(&c.id, &c.Path, &c.Line, &c.Start, &c.End, &c.SourceHash, &c.SectionStart, &c.SectionEnd); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

func fuse(rankings ...[]candidate) []candidate {
	byID := map[int]candidate{}
	for _, ranking := range rankings {
		for rank, c := range ranking {
			previous := byID[c.id]
			c.score = previous.score + 1/float64(60+rank+1)
			byID[c.id] = c
		}
	}
	result := make([]candidate, 0, len(byID))
	for _, c := range byID {
		result = append(result, c)
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if a.score != b.score {
			return a.score > b.score
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Start != b.Start {
			return a.Start < b.Start
		}
		return a.id < b.id
	})
	return result
}

func snippetWords(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
}

func searchSnippet(query, raw string, startLine int) (int, string) {
	best, bestMatches := "", -1
	lineNumber := startLine
	terms := snippetWords(query)
	for i, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		matches := 0
		words := snippetWords(line)
		for _, term := range terms {
			if slices.Contains(words, term) {
				matches++
			}
		}
		if matches > bestMatches || (matches == bestMatches && strings.HasPrefix(best, "#") && !strings.HasPrefix(line, "#")) {
			best, bestMatches, lineNumber = line, matches, startLine+i
		}
	}
	runes := []rune(best)
	if len(runes) > 240 {
		best = string(runes[:240]) + "…"
	}
	return lineNumber, best
}
