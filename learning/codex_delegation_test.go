package learning

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexDelegatedInputBeforeTaskStarted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "delegate.jsonl")
	writeLines(t, path,
		`{"type":"session_meta","payload":{"id":"delegate"}}`,
		`{"type":"event_msg","payload":{"type":"task_started","turn_id":"parent"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":"parent task"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":"delegated task"}}`,
		`{"type":"event_msg","payload":{"type":"task_started","turn_id":"child","root_turn_id":"parent"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"assistant","phase":"final_answer","content":"completed delegated work"}}`,
		`{"type":"event_msg","payload":{"type":"task_complete","turn_id":"child"}}`)
	tr, err := parse(path, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Turns) != 1 || tr.Turns[0].ID != "child" {
		t.Fatalf("delegated turn lost: %+v", tr)
	}
	b, _ := json.Marshal(tr.Turns)
	if strings.Contains(string(b), "parent task") || !strings.Contains(string(b), "delegated task") {
		t.Fatalf("bad delegated evidence: %s", b)
	}
}
