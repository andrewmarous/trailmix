// Package learning tracks completed conversation turns across local agent harnesses.
package learning

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// Event contains review evidence, not system prompts, reasoning, or usage records.
// Content remains untrusted source data and can contain sensitive information.
type Event struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}
type Turn struct {
	ID          string  `json:"id"`
	Events      []Event `json:"events"`
	CompletedAt string  `json:"completed_at,omitempty"`
}
type transcript struct {
	ID, CWD string
	Turns   []Turn
}
type entry struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	ID        string          `json:"id"`
	Parent    *string         `json:"parentId"`
	Version   int             `json:"version"`
	CWD       string          `json:"cwd"`
	Message   json.RawMessage `json:"message"`
	Payload   json.RawMessage `json:"payload"`
}

// Only complete JSONL records are consumed. An in-progress final line waits for
// the next scan. A malformed, newline-terminated record is an explicit error.
func readEntries(path string) ([]entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	r := bufio.NewReader(io.LimitReader(f, info.Size()))
	var entries []entry
	for line := 1; ; line++ {
		b, e := r.ReadBytes('\n')
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		if len(strings.TrimSpace(string(b))) == 0 {
			continue
		}
		var item entry
		if err = json.Unmarshal(b, &item); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		entries = append(entries, item)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no complete session header")
	}
	return entries, nil
}

func parse(path, harness string) (transcript, error) {
	es, err := readEntries(path)
	if err != nil {
		return transcript{}, err
	}
	switch harness {
	case "pi":
		return parsePi(es)
	case "codex":
		return parseCodex(es)
	default:
		return transcript{}, fmt.Errorf("unsupported harness %q", harness)
	}
}

func evidence(role string, content json.RawMessage) Event {
	// Pi content blocks can include private model reasoning. Keep only text,
	// user text, and tool invocations. Images/binary content are not copied.
	var blocks []map[string]json.RawMessage
	if json.Unmarshal(content, &blocks) == nil && blocks != nil {
		kept := []map[string]json.RawMessage{}
		for _, b := range blocks {
			var kind string
			_ = json.Unmarshal(b["type"], &kind)
			switch kind {
			case "text", "input_text", "output_text", "toolCall":
				kept = append(kept, b)
			}
		}
		content, _ = json.Marshal(kept)
	}
	if len(content) == 0 {
		content = json.RawMessage(`null`)
	}
	return Event{Role: role, Content: content}
}

func parsePi(es []entry) (transcript, error) {
	h := es[0]
	t := transcript{ID: h.ID, CWD: h.CWD, Turns: []Turn{}}
	if h.Type != "session" || h.ID == "" || (h.Version != 2 && h.Version != 3) {
		return t, fmt.Errorf("unsupported Pi session header/version %d", h.Version)
	}
	// Follow the last entry's parent chain. Sibling branches must not become
	// one fictitious conversation. Raw history is retained across compaction.
	nodes := map[string]entry{}
	leaf := ""
	for _, e := range es[1:] {
		if e.ID == "" {
			return t, fmt.Errorf("Pi entry has no ID")
		}
		if _, exists := nodes[e.ID]; exists {
			return t, fmt.Errorf("duplicate Pi entry ID %s", e.ID)
		}
		nodes[e.ID] = e
		leaf = e.ID
	}
	path := []entry{}
	seen := map[string]bool{}
	for leaf != "" {
		if seen[leaf] {
			return t, fmt.Errorf("cycle in Pi history")
		}
		seen[leaf] = true
		e, ok := nodes[leaf]
		if !ok {
			return t, fmt.Errorf("missing Pi parent %s", leaf)
		}
		path = append(path, e)
		leaf = ""
		if e.Parent != nil {
			leaf = *e.Parent
		}
	}
	var current *Turn
	for i := len(path) - 1; i >= 0; i-- {
		e := path[i]
		if e.Type != "message" {
			continue
		}
		var m struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
			Stop    string          `json:"stopReason"`
		}
		if err := json.Unmarshal(e.Message, &m); err != nil {
			return t, err
		}
		switch m.Role {
		case "user":
			current = &Turn{ID: e.ID, Events: []Event{evidence("user", m.Content)}}
		case "assistant", "toolResult", "bashExecution":
			if current == nil {
				continue
			}
			current.Events = append(current.Events, evidence(m.Role, e.MessageContent()))
			if m.Role == "assistant" && m.Stop == "stop" {
				current.CompletedAt = e.Timestamp
				t.Turns = append(t.Turns, *current)
				current = nil
			}
		}
	}
	return t, nil
}

// MessageContent keeps tool call names/results and execution metadata, while
// excluding reasoning and provider bookkeeping from the review packet.
func (e entry) MessageContent() json.RawMessage {
	var m map[string]json.RawMessage
	_ = json.Unmarshal(e.Message, &m)
	kept := map[string]json.RawMessage{}
	for _, key := range []string{"content", "toolCallId", "toolName", "isError", "command", "output", "exitCode", "stopReason"} {
		if value, ok := m[key]; ok {
			if key == "content" {
				value = evidence("", value).Content
			}
			kept[key] = value
		}
	}
	b, _ := json.Marshal(kept)
	return b
}

func parseCodex(es []entry) (transcript, error) {
	t := transcript{Turns: []Turn{}}
	if es[0].Type != "session_meta" {
		return t, fmt.Errorf("missing Codex session_meta header")
	}
	var meta struct {
		ID        string `json:"id"`
		SessionID string `json:"session_id"`
		CWD       string `json:"cwd"`
	}
	if err := json.Unmarshal(es[0].Payload, &meta); err != nil {
		return t, err
	}
	t.ID = meta.ID
	if t.ID == "" {
		t.ID = meta.SessionID
	}
	t.CWD = meta.CWD
	if t.ID == "" {
		return t, fmt.Errorf("Codex session has no native ID")
	}
	var current *Turn
	started := false
	users := 0
	markers := false
	for _, e := range es[1:] {
		var p struct {
			Type    string          `json:"type"`
			Role    string          `json:"role"`
			TurnID  string          `json:"turn_id"`
			Content json.RawMessage `json:"content"`
			Phase   string          `json:"phase"`
		}
		if len(e.Payload) == 0 {
			continue
		}
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return t, err
		}
		if e.Type == "event_msg" {
			switch p.Type {
			case "task_started":
				markers = true
				started = true
				// Forked/delegated rollouts can record their user input before
				// task_started, after inherited parent context. Carry the latest
				// user input only when no response work has begun.
				events := []Event{}
				if current != nil && len(current.Events) > 0 {
					onlyUsers := true
					for _, event := range current.Events {
						if event.Role != "user" {
							onlyUsers = false
						}
					}
					if onlyUsers {
						events = append(events, current.Events[len(current.Events)-1])
					}
				}
				current = &Turn{ID: p.TurnID, Events: events}
			case "turn_aborted", "task_aborted":
				current = nil
				started = false
			case "task_complete":
				markers = true
				if current != nil && (p.TurnID == "" || current.ID == p.TurnID) {
					if hasUser(*current) {
						current.CompletedAt = e.Timestamp
						t.Turns = append(t.Turns, *current)
					}
					current = nil
					started = false
				}
			}
			// event_msg user/assistant/item_completed mirror response_item records.
			continue
		}
		if e.Type != "response_item" {
			continue
		}
		if p.Type == "message" && p.Role == "user" {
			users++
			if current == nil || (!started && hasUser(*current)) {
				current = &Turn{ID: fmt.Sprintf("user-%d", users), Events: []Event{}}
			}
			current.Events = append(current.Events, evidence("user", p.Content))
			continue
		}
		if current == nil {
			continue
		}
		switch p.Type {
		case "message":
			if p.Role == "assistant" {
				current.Events = append(current.Events, evidence("assistant", p.Content))
				// Older rollouts can explicitly label a final answer without task events.
				if !started && p.Phase == "final_answer" && hasUser(*current) {
					current.CompletedAt = e.Timestamp
					t.Turns = append(t.Turns, *current)
					current = nil
					markers = true
				}
			}
		case "function_call", "function_call_output", "custom_tool_call", "custom_tool_call_output":
			var body map[string]json.RawMessage
			_ = json.Unmarshal(e.Payload, &body)
			kept := map[string]json.RawMessage{}
			for _, k := range []string{"type", "name", "arguments", "input", "output", "call_id"} {
				if v, ok := body[k]; ok {
					kept[k] = v
				}
			}
			b, _ := json.Marshal(kept)
			current.Events = append(current.Events, Event{Role: p.Type, Content: b})
		}
	}
	if users > 0 && !markers {
		return t, fmt.Errorf("unsupported Codex completion format: no task events or final_answer phase")
	}
	return t, nil
}
func hasUser(t Turn) bool {
	for _, e := range t.Events {
		if e.Role == "user" {
			return true
		}
	}
	return false
}
func digest(turns []Turn) string {
	// Preserve compatibility with evidence digests from before timestamps
	// were retained. Eligibility is checked separately.
	type evidenceTurn struct {
		ID     string  `json:"id"`
		Events []Event `json:"events"`
	}
	items := make([]evidenceTurn, len(turns))
	for i, turn := range turns {
		items[i] = evidenceTurn{ID: turn.ID, Events: turn.Events}
	}
	b, _ := json.Marshal(items)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
