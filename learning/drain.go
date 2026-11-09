package learning

import (
	"fmt"
	"time"
)

// captureWork freezes eligible ranges, including their evidence digests. Later
// source appends cannot enlarge this invocation's workload.
func (r *Registry) captureWork(before time.Time) ([]Range, error) {
	var work []Range
	for _, s := range r.Sessions {
		if s.Error != "" || s.PendingTurns == 0 {
			continue
		}
		t, err := parse(s.Path, s.Harness)
		if err != nil {
			return nil, err
		}
		if t.ID != s.NativeID || len(t.Turns) < s.CompletedTurns {
			return nil, fmt.Errorf("session %s changed during discovery", s.ID)
		}
		pending, err := r.pendingRanges(s, t, s.CompletedTurns)
		if err != nil {
			return nil, err
		}
		for _, item := range pending {
			start := 0
			for n := item.FromTurn; n <= item.ThroughTurn; n++ {
				completed, err := time.Parse(time.RFC3339Nano, t.Turns[n-1].CompletedAt)
				if err != nil {
					return nil, err
				}
				if !completed.After(before) {
					if start == 0 {
						start = n
					}
				} else if start != 0 {
					work = append(work, Range{SessionID: s.ID, FromTurn: start, ThroughTurn: n - 1, Digest: digest(t.Turns[:n-1])})
					start = 0
				}
			}
			if start != 0 {
				work = append(work, Range{SessionID: s.ID, FromTurn: start, ThroughTurn: item.ThroughTurn, Digest: item.Digest})
			}
		}
	}
	return work, nil
}

func (r *Registry) beginCaptured(root string, limit int, work []Range) (*Run, error) {
	if r.active() != nil {
		return nil, fmt.Errorf("a learning review is already active")
	}
	var ranges []Range
	session := ""
	for _, item := range work {
		s := r.session(item.SessionID)
		if s == nil {
			return nil, fmt.Errorf("missing session %s", item.SessionID)
		}
		start := item.FromTurn
		for start <= item.ThroughTurn && s.reviewed(start) {
			start++
		}
		if start > item.ThroughTurn {
			continue
		}
		if limit == 0 || (session != "" && session != item.SessionID) {
			break
		}
		_, t, err := r.loadRange(item)
		if err != nil {
			return nil, err
		}
		end := item.ThroughTurn
		if end-start+1 > limit {
			end = start + limit - 1
		}
		ranges = append(ranges, Range{SessionID: item.SessionID, FromTurn: start, ThroughTurn: end, Digest: digest(t.Turns[:end])})
		session = item.SessionID
		limit -= end - start + 1
	}
	if len(ranges) == 0 {
		return nil, nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	r.Runs = append(r.Runs, Run{ID: now, Started: now, WorkerVault: root, Ranges: ranges})
	return &r.Runs[len(r.Runs)-1], nil
}
