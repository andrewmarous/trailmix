package retrieval

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/andrewmarous/trailmix/vault"
	_ "github.com/asg017/sqlite-vec-go-bindings/ncruces"
	"github.com/gofrs/flock"
	_ "github.com/ncruces/go-sqlite3/driver"
)

type IndexOptions struct {
	Mode     string
	Database string
	Runtime  LlamaConfig
}
type IndexReport struct {
	Database string `json:"database"`
	Notes    int    `json:"notes"`
	Passages int    `json:"passages"`
	Embedded int    `json:"embedded"`
	Reused   int    `json:"reused"`
}

type embedder interface {
	Fits(context.Context, string) (bool, error)
	Embed(context.Context, []string) ([][]float32, error)
	Close()
}

func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("not a regular file: %s", path)
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// Index builds lexical passages and, in hybrid mode, embeddings. Inference
// starts only for uncached input sizing or vectors and is reaped before return.
// Existing active passages remain searchable until replacement commits.
func Index(ctx context.Context, v *vault.Vault, o IndexOptions) (IndexReport, error) {
	if o.Mode == "text" {
		return build(ctx, v, o.Database, textIdentity, nil)
	}
	if o.Mode != "" && o.Mode != "hybrid" {
		return IndexReport{}, fmt.Errorf("retrieval mode must be hybrid or text")
	}
	identity, err := runtimeIdentity(o.Runtime)
	if err != nil {
		return IndexReport{}, err
	}
	return build(ctx, v, o.Database, identity, func(ctx context.Context) (embedder, error) { return StartLlama(ctx, o.Runtime) })
}

const textIdentity = "text:" + passageFormat

func runtimeIdentity(c LlamaConfig) (string, error) {
	model, err := fileHash(c.Model)
	if err != nil {
		return "", fmt.Errorf("embedding model: %w", err)
	}
	runtime, err := fileHash(c.Executable)
	if err != nil {
		return "", fmt.Errorf("embedding runtime: %w", err)
	}
	return hash([]byte("qwen3-embedding-0.6b:last:l2:1024:" + passageFormat + ":" + model + ":" + runtime)), nil
}

// privatePath rejects symlinks at the database and its storage-directory boundary.
// Ancestors may include OS aliases such as macOS /var. Like vault operations,
// this assumes no adversarial concurrent filesystem mutation by the same user.
func privatePath(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for _, p := range []string{abs, filepath.Dir(abs)} {
		info, e := os.Lstat(p)
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		if e == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("index path is a symlink: %s", p)
		}
	}
	return nil
}

func openIndex(path string) (*sql.DB, error) {
	u := url.URL{Scheme: "file", Path: path}
	q := u.Query()
	q.Set("_pragma", "busy_timeout(5000)")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite3", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	// Reject unrelated databases before making any schema changes.
	var application, version, tables int
	if err = db.QueryRow("PRAGMA application_id").Scan(&application); err == nil {
		err = db.QueryRow("PRAGMA user_version").Scan(&version)
	}
	if err == nil && application == 0 {
		err = db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'").Scan(&tables)
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	const applicationID = 0x544d4958 // TMIX
	if (application == 0 && tables != 0) || (application != 0 && application != applicationID) {
		db.Close()
		return nil, fmt.Errorf("not a Trailmix index: %s", path)
	}
	if version < 0 || version > 2 {
		db.Close()
		return nil, fmt.Errorf("unsupported retrieval schema version %d", version)
	}
	tx, err := db.Begin()
	if err != nil {
		db.Close()
		return nil, err
	}
	_, err = tx.Exec(`
 CREATE TABLE IF NOT EXISTS metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS embeddings (model TEXT NOT NULL, hash TEXT NOT NULL, vector BLOB NOT NULL, PRIMARY KEY(model,hash));
 CREATE TABLE IF NOT EXISTS passages (id INTEGER PRIMARY KEY, path TEXT NOT NULL, line INTEGER NOT NULL, text TEXT NOT NULL, hash TEXT NOT NULL, start_byte INTEGER NOT NULL DEFAULT 0, end_byte INTEGER NOT NULL DEFAULT 0, end_line INTEGER NOT NULL DEFAULT 0, source_hash TEXT NOT NULL DEFAULT '', section_start INTEGER NOT NULL DEFAULT 0, section_end INTEGER NOT NULL DEFAULT 0);
 CREATE VIRTUAL TABLE IF NOT EXISTS passages_fts USING fts5(path, text);
 PRAGMA application_id=0x544d4958;
 PRAGMA user_version=2;
 `)
	if err != nil {
		tx.Rollback()
		db.Close()
		return nil, err
	}
	if version == 1 && err == nil {
		_, err = tx.Exec(`ALTER TABLE passages ADD COLUMN start_byte INTEGER NOT NULL DEFAULT 0;
		ALTER TABLE passages ADD COLUMN end_byte INTEGER NOT NULL DEFAULT 0;
		ALTER TABLE passages ADD COLUMN end_line INTEGER NOT NULL DEFAULT 0;
		ALTER TABLE passages ADD COLUMN source_hash TEXT NOT NULL DEFAULT '';
		ALTER TABLE passages ADD COLUMN section_start INTEGER NOT NULL DEFAULT 0;
		ALTER TABLE passages ADD COLUMN section_end INTEGER NOT NULL DEFAULT 0;`)
		if err != nil {
			tx.Rollback()
			db.Close()
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func build(ctx context.Context, v *vault.Vault, path, identity string, start func(context.Context) (embedder, error)) (report IndexReport, err error) {
	if path == "" {
		return report, fmt.Errorf("index database path is required")
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return report, err
	}
	for _, p := range []string{path, path + ".lock", path + "-journal", path + "-wal", path + "-shm"} {
		if err = privatePath(p); err != nil {
			return report, err
		}
	}
	path, err = canonicalLocation(path)
	if err != nil {
		return report, err
	}
	root, err := filepath.EvalSymlinks(v.Root)
	if err != nil {
		return report, err
	}
	report.Database = path
	// Resolve ancestor aliases before checking the sync boundary.
	rel, e := filepath.Rel(root, path)
	if e != nil {
		return report, e
	}
	if !filepath.IsAbs(rel) && !isOutside(rel) {
		return report, fmt.Errorf("index database must be outside the vault")
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return report, err
	}
	lock := flock.New(path + ".lock")
	locked, e := lock.TryLock()
	if e != nil {
		return report, e
	}
	if !locked {
		return report, fmt.Errorf("index update already running")
	}
	defer lock.Unlock()
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return report, e
	}
	f.Close()
	if err = os.Chmod(path, 0600); err != nil {
		return report, err
	}
	db, e := openIndex(path)
	if e != nil {
		return report, e
	}
	defer db.Close()
	var owner string
	e = db.QueryRowContext(ctx, "SELECT value FROM metadata WHERE key='vault'").Scan(&owner)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return report, e
	}
	if e == nil && owner != root {
		return report, fmt.Errorf("index belongs to a different vault")
	}
	if _, err = db.ExecContext(ctx, "INSERT OR IGNORE INTO metadata(key,value) VALUES('vault',?)", root); err != nil {
		return report, err
	}
	sources, snapshot, e := collect(v)
	if e != nil {
		return report, e
	}
	var matches int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM metadata WHERE (key='snapshot' AND value=?) OR (key='model' AND value=?) OR (key='format' AND value=?)`, snapshot, identity, passageFormat).Scan(&matches); err != nil {
		return report, err
	}
	if matches == 3 {
		err = db.QueryRowContext(ctx, "SELECT count(*),count(DISTINCT path) FROM passages").Scan(&report.Passages, &report.Notes)
		if identity != textIdentity {
			report.Reused = report.Passages
		}
		return report, err
	}
	var runtime embedder
	defer func() {
		if runtime != nil {
			runtime.Close()
		}
	}()
	ensureRuntime := func() error {
		if runtime == nil {
			runtime, err = start(ctx)
		}
		return err
	}
	fits := func(ctx context.Context, input string) (bool, error) {
		if identity == textIdentity {
			return true, ctx.Err()
		}
		var exists int
		e := db.QueryRowContext(ctx, "SELECT 1 FROM embeddings WHERE model=? AND hash=?", identity, hash([]byte(input))).Scan(&exists)
		if e == nil {
			return true, nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return false, e
		}
		if e = ensureRuntime(); e != nil {
			return false, e
		}
		return runtime.Fits(ctx, input)
	}
	var passages []passage
	for _, source := range sources {
		parts, e := splitNote(ctx, source, fits)
		if e != nil {
			return report, e
		}
		passages = append(passages, parts...)
		if len(parts) > 0 {
			report.Notes++
		}
	}
	report.Passages = len(passages)
	for offset := 0; identity != textIdentity && offset < len(passages); {
		if err = ctx.Err(); err != nil {
			return report, err
		}
		var pending []passage
		for offset < len(passages) && len(pending) < embeddingBatch {
			p := passages[offset]
			offset++
			var exists int
			e = db.QueryRowContext(ctx, "SELECT 1 FROM embeddings WHERE model=? AND hash=?", identity, p.Hash).Scan(&exists)
			if e == nil {
				report.Reused++
				continue
			}
			if !errors.Is(e, sql.ErrNoRows) {
				return report, e
			}
			pending = append(pending, p)
		}
		if len(pending) == 0 {
			continue
		}
		if err = ensureRuntime(); err != nil {
			return report, err
		}
		inputs := make([]string, len(pending))
		for i, p := range pending {
			inputs[i] = p.Input
		}
		vectors, e := runtime.Embed(ctx, inputs)
		if e != nil {
			return report, e
		}
		if len(vectors) != len(inputs) {
			return report, fmt.Errorf("embedding response count mismatch")
		}
		for _, vector := range vectors {
			if err = validateVector(vector); err != nil {
				return report, err
			}
		}
		tx, e := db.BeginTx(ctx, nil)
		if e != nil {
			return report, e
		}
		for i, p := range pending {
			_, e = tx.ExecContext(ctx, "INSERT OR IGNORE INTO embeddings(model,hash,vector) VALUES(?,?,?)", identity, p.Hash, vectorBytes(vectors[i]))
			if e != nil {
				break
			}
		}
		if e != nil {
			tx.Rollback()
			return report, e
		}
		if err = tx.Commit(); err != nil {
			return report, err
		}
		report.Embedded += len(pending)
	}
	// A concurrent edit invalidates activation, but cached vectors remain reusable.
	_, current, e := collect(v)
	if e != nil {
		return report, e
	}
	if current != snapshot {
		return report, fmt.Errorf("vault changed during indexing; rerun to resume")
	}
	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		return report, e
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "DELETE FROM passages; DELETE FROM passages_fts;"); err != nil {
		return report, err
	}
	for i, p := range passages {
		if _, err = tx.ExecContext(ctx, "INSERT INTO passages(id,path,line,text,hash,start_byte,end_byte,end_line,source_hash,section_start,section_end) VALUES(?,?,?,?,?,?,?,?,?,?,?)", i+1, p.Path, p.Line, p.Text, p.Hash, p.Start, p.End, p.EndLine, p.SourceHash, p.SectionStart, p.SectionEnd); err != nil {
			return report, err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO passages_fts(rowid,path,text) VALUES(?,?,?)", i+1, p.Path, p.Input); err != nil {
			return report, err
		}
	}
	for key, value := range map[string]string{"model": identity, "snapshot": snapshot, "format": passageFormat} {
		if _, err = tx.ExecContext(ctx, "INSERT INTO metadata(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", key, value); err != nil {
			return report, err
		}
	}
	if err = tx.Commit(); err != nil {
		return report, err
	}
	return report, nil
}

// canonicalLocation resolves existing ancestors, including OS directory aliases.
func canonicalLocation(path string) (string, error) {
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		suffix = append(suffix, filepath.Base(path))
		parent := filepath.Dir(path)
		if parent == path {
			return "", err
		}
		path = parent
	}
}

func isOutside(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func vectorBytes(vector []float32) []byte {
	b := make([]byte, 4*len(vector))
	for i, x := range vector {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(x))
	}
	return b
}
