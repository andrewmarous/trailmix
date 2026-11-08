package learning

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	for i, line := range lines {
		var record map[string]any
		if json.Unmarshal([]byte(line), &record) == nil {
			if _, ok := record["timestamp"]; !ok {
				record["timestamp"] = "2000-01-01T00:00:00Z"
			}
			b, _ := json.Marshal(record)
			lines[i] = string(b)
		}
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
}
func piHeader(id string) string {
	return `{"type":"session","version":3,"id":"` + id + `","cwd":"/work/oz"}`
}
func piMessage(id, parent, role, content, stop string) string {
	p := any(parent)
	if parent == "" {
		p = nil
	}
	b, _ := json.Marshal(map[string]any{"type": "message", "id": id, "parentId": p, "message": map[string]any{"role": role, "content": content, "stopReason": stop}})
	return string(b)
}
func piLines(id string) []string {
	return []string{piHeader(id), piMessage("u1", "", "user", "fix it", ""), piMessage("a1", "u1", "assistant", "fixed", "stop")}
}

func TestPiTurnsAndBranches(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pi.jsonl")
	lines := []string{piHeader("pi-1"), piMessage("u1", "", "user", "first", ""),
		`{"type":"message","id":"tool","parentId":"u1","message":{"role":"assistant","stopReason":"toolUse","content":[{"type":"thinking","thinking":"private-reasoning"},{"type":"toolCall","id":"c1","name":"bash","arguments":{"command":"test"}}]}}`,
		`{"type":"message","id":"result","parentId":"tool","message":{"role":"toolResult","toolCallId":"c1","toolName":"bash","content":[{"type":"text","text":"observed-result"}]}}`,
		piMessage("a1", "result", "assistant", "done", "stop"), piMessage("u2", "a1", "user", "unfinished", ""), piMessage("a2", "u2", "assistant", "waiting", "toolUse")}
	writeLines(t, path, lines...)
	tr, err := parse(path, "pi")
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Turns) != 1 || len(tr.Turns[0].Events) != 4 {
		t.Fatalf("bad turns: %+v", tr)
	}
	b, _ := json.Marshal(tr.Turns)
	if strings.Contains(string(b), "private-reasoning") || !strings.Contains(string(b), "observed-result") {
		t.Fatalf("bad evidence %s", b)
	}
	// A second branch replaces the first active turn, not a chronological append.
	lines = append(lines, piMessage("branch-user", "", "user", "alternative", ""), piMessage("branch-answer", "branch-user", "assistant", "alternative done", "stop"))
	writeLines(t, path, lines...)
	tr, err = parse(path, "pi")
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Turns) != 1 || tr.Turns[0].ID != "branch-user" {
		t.Fatalf("branches mixed: %+v", tr)
	}
}

func codexLines(id string) []string {
	return []string{
		`{"type":"session_meta","payload":{"id":"` + id + `","cwd":"/work/oz","cli_version":"0.1"}}`,
		`{"type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"fix it"}]}}`,
		`{"type":"event_msg","payload":{"type":"user_message","message":"duplicate-user"}}`,
		`{"type":"response_item","payload":{"type":"reasoning","summary":[{"text":"private-reasoning"}]}}`,
		`{"type":"response_item","payload":{"type":"function_call","name":"bash","arguments":"test","call_id":"c1"}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","call_id":"c1","output":"observed-result"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"fixed"}]}}`,
		`{"type":"event_msg","payload":{"type":"task_complete","turn_id":"t1","last_agent_message":"duplicate-answer"}}`,
	}
}
func TestCodexTurns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codex.jsonl")
	lines := append(codexLines("cx"), `{"type":"event_msg","payload":{"type":"task_started","turn_id":"t2"}}`, `{"type":"response_item","payload":{"type":"message","role":"user","content":"pending"}}`)
	writeLines(t, path, lines...)
	tr, err := parse(path, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Turns) != 1 || len(tr.Turns[0].Events) != 4 {
		t.Fatalf("bad turns: %+v", tr)
	}
	b, _ := json.Marshal(tr.Turns)
	for _, bad := range []string{"duplicate-user", "duplicate-answer", "private-reasoning", "pending"} {
		if strings.Contains(string(b), bad) {
			t.Fatalf("leaked %s", bad)
		}
	}
	if !strings.Contains(string(b), "observed-result") {
		t.Fatal("lost tool evidence")
	}
}
func TestSourceErrorsAndPartialWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pi.jsonl")
	writeLines(t, path, piLines("p")...)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"type":`)
	_ = f.Close()
	tr, err := parse(path, "pi")
	if err != nil || len(tr.Turns) != 1 {
		t.Fatalf("partial append: %v %+v", err, tr)
	}
	f, _ = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	_, _ = f.WriteString("\n")
	_ = f.Close()
	if _, err = parse(path, "pi"); err == nil {
		t.Fatal("malformed complete line accepted")
	}
	writeLines(t, path, `{"type":"session","version":99,"id":"future"}`)
	if _, err = parse(path, "pi"); err == nil {
		t.Fatal("unknown Pi version accepted")
	}
	writeLines(t, path, `{"type":"session_meta","payload":{"id":"cx"}}`, `{"type":"response_item","payload":{"type":"message","role":"user","content":"hello"}}`)
	if _, err = parse(path, "codex"); err == nil {
		t.Fatal("unrecognized completion format accepted")
	}
}
func TestCodexFinalAnswerFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "codex.jsonl")
	writeLines(t, path, `{"type":"session_meta","payload":{"id":"cx"}}`, `{"type":"response_item","payload":{"type":"message","role":"user","content":"hello"}}`, `{"type":"response_item","payload":{"type":"message","role":"assistant","phase":"final_answer","content":"hi"}}`)
	tr, err := parse(path, "codex")
	if err != nil || len(tr.Turns) != 1 {
		t.Fatalf("fallback: %+v %v", tr, err)
	}
}
