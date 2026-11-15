package learning

import (
	"bytes"
	"context"
	"encoding/json"
)

// Codex emits reasoning, command and answer items rather than Pi turn events.
// Bound those observable steps as well as wall-clock time and captured output.
type consolidationActivity struct {
	output    *boundedOutput
	cancel    context.CancelFunc
	pending   []byte
	items     int
	exhausted bool
}

func (w *consolidationActivity) Write(b []byte) (int, error) {
	n, err := w.output.Write(b)
	w.pending = append(w.pending, b...)
	for {
		i := bytes.IndexByte(w.pending, '\n')
		if i < 0 {
			break
		}
		var event struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(w.pending[:i], &event) == nil && event.Type == "item.started" {
			w.items++
		}
		w.pending = w.pending[i+1:]
	}
	if w.items > 48 || w.output.overflow || len(w.pending) > 1024*1024 {
		w.exhausted = true
		w.pending = nil
		w.cancel()
	}
	return n, err
}
