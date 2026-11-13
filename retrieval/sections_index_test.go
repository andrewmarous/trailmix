package retrieval

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestSectionIndexUsesSameInputAndUpdatesPointersWithoutReembedding(t *testing.T) {
	v, path := fixture(t)
	body := "# Safety\n\n## First\n\nCheck the baseline.\n\n## Second\n\nKeep the evidence.\n"
	note(t, v, "safety", body)
	f := &fakeEmbedder{}
	report, err := build(context.Background(), v, path, "model", func(context.Context) (embedder, error) { return f, nil })
	if err != nil || report.Passages != 2 {
		t.Fatalf("build %+v: %v", report, err)
	}
	db, err := openIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range f.inputs {
		var lexical, raw, sourceHash string
		var start, end, sectionStart, sectionEnd int
		err = db.QueryRow(`SELECT f.text,p.text,p.start_byte,p.end_byte,p.source_hash,p.section_start,p.section_end FROM passages p JOIN passages_fts f ON f.rowid=p.id WHERE p.hash=?`, hash([]byte(input))).Scan(&lexical, &raw, &start, &end, &sourceHash, &sectionStart, &sectionEnd)
		if err != nil || lexical != input || raw != body[start:end] || sourceHash != hash([]byte(body)) || sectionStart != start || sectionEnd != end {
			t.Fatalf("stored source/input mismatch: %v", err)
		}
	}
	var previousHash string
	var previousStart int
	err = db.QueryRow("SELECT hash,start_byte FROM passages ORDER BY id DESC LIMIT 1").Scan(&previousHash, &previousStart)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	changed := "# Safety\n\n## First\n\nAn expanded baseline check.\nPreserve concurrent updates.\n\n## Second\n\nKeep the evidence.\n"
	note(t, v, "safety", changed)
	report, err = build(context.Background(), v, path, "model", func(context.Context) (embedder, error) { return &fakeEmbedder{}, nil })
	if err != nil || report.Reused != 1 || report.Embedded != 1 {
		t.Fatalf("update %+v: %v", report, err)
	}
	db, err = openIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var start, end int
	var sourceHash string
	err = db.QueryRow("SELECT start_byte,end_byte,source_hash FROM passages WHERE hash=?", previousHash).Scan(&start, &end, &sourceHash)
	if err != nil || start == previousStart || changed[start:end] != "## Second\n\nKeep the evidence.\n" || sourceHash != hash([]byte(changed)) {
		t.Fatalf("updated pointer %d-%d: %v", start, end, err)
	}
}

func TestSchemaUpgradePreservesPreviousActivePassages(t *testing.T) {
	_, path := fixture(t)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`PRAGMA application_id=0x544d4958; PRAGMA user_version=1;
 CREATE TABLE passages(id INTEGER PRIMARY KEY,path TEXT NOT NULL,line INTEGER NOT NULL,text TEXT NOT NULL,hash TEXT NOT NULL);
 INSERT INTO passages VALUES(1,'safety.md',1,'Original evidence.','old');`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	db, err = openIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var raw, sourceHash string
	var start, end, version int
	err = db.QueryRow("SELECT text,start_byte,end_byte,source_hash FROM passages WHERE id=1").Scan(&raw, &start, &end, &sourceHash)
	if err != nil || raw != "Original evidence." || sourceHash != "" || start != 0 || end != 0 {
		t.Fatalf("upgraded row %q: %v", raw, err)
	}
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 2 {
		t.Fatalf("upgraded schema %d: %v", version, err)
	}
}
