package learning

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

func DefaultStateDir(home string) string {
	return filepath.Join(home, ".local", "state", "trailmix", "learning")
}

// Initialize belongs to tmix init, not discovery. The store transaction holds
// the machine-local lock while choosing and persisting the first cutoff.
func (s Store) Initialize() (string, error) {
	var start string
	err := s.Transaction(func(r *Registry) error {
		created := r.LearningStartAt == ""
		if created {
			if r.active() != nil {
				return fmt.Errorf("abort the active session review before initializing the learning cutoff")
			}
			r.LearningStartAt = time.Now().UTC().Format(time.RFC3339Nano)
		}
		start = r.LearningStartAt
		if r.Sources == nil {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			r.Sources = DefaultSources(home)
		}
		if created && len(r.Sessions) > 0 {
			return r.Scan(nil)
		}
		return nil
	})
	return start, err
}

func (r *Registry) requireCutoff() (time.Time, error) {
	if r.LearningStartAt == "" {
		return time.Time{}, fmt.Errorf("learning cutoff is not initialized; run tmix init (use --learning-state-dir for a custom registry)")
	}
	t, err := time.Parse(time.RFC3339Nano, r.LearningStartAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid learning_start_at: %w", err)
	}
	return t, nil
}

func (r *Registry) SetStartAt(value string) error {
	if _, err := r.requireCutoff(); err != nil {
		return err
	}
	if r.active() != nil {
		return fmt.Errorf("abort or complete the active review before changing the learning cutoff")
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return fmt.Errorf("start-at must be an RFC3339 timestamp with a timezone: %w", err)
	}
	r.LearningStartAt = t.UTC().Format(time.RFC3339Nano)
	return r.Scan(nil)
}

// Spans record successful reviews, not exclusions. Usually there is one span.
// Keeping exclusions separate allows an earlier cutoff to expose older turns
// without pretending the previously skipped history was reviewed.
type TurnSpan struct {
	FromTurn    int `json:"from_turn"`
	ThroughTurn int `json:"through_turn"`
}

func (s *Session) reviewed(turn int) bool {
	for _, span := range s.ReviewedSpans {
		if turn >= span.FromTurn && turn <= span.ThroughTurn {
			return true
		}
	}
	return false
}
func (s *Session) addReviewed(from, through int) {
	spans := append(s.ReviewedSpans, TurnSpan{FromTurn: from, ThroughTurn: through})
	sort.Slice(spans, func(i, j int) bool { return spans[i].FromTurn < spans[j].FromTurn })
	merged := []TurnSpan{}
	for _, span := range spans {
		if len(merged) > 0 && span.FromTurn <= merged[len(merged)-1].ThroughTurn+1 {
			if span.ThroughTurn > merged[len(merged)-1].ThroughTurn {
				merged[len(merged)-1].ThroughTurn = span.ThroughTurn
			}
		} else {
			merged = append(merged, span)
		}
	}
	s.ReviewedSpans = merged
	s.ReviewedThroughTurn = merged[len(merged)-1].ThroughTurn
}

func (r *Registry) eligible(turn Turn) (bool, error) {
	cutoff, err := r.requireCutoff()
	if err != nil {
		return false, err
	}
	completed, err := time.Parse(time.RFC3339Nano, turn.CompletedAt)
	if err != nil {
		return false, fmt.Errorf("turn %s has no valid completion timestamp", turn.ID)
	}
	return !completed.Before(cutoff), nil
}
func (r *Registry) refresh(s *Session, t transcript) error {
	s.CompletedTurns = len(t.Turns)
	s.BeforeCutoffTurns = 0
	s.ReviewedTurns = 0
	s.PendingTurns = 0
	if s.ReviewedThroughTurn > len(t.Turns) || (s.ReviewedThroughTurn > 0 && digest(t.Turns[:s.ReviewedThroughTurn]) != s.ReviewedDigest) {
		return fmt.Errorf("reviewed history or branch changed; explicit reset required")
	}
	for i, turn := range t.Turns {
		eligible, err := r.eligible(turn)
		if err != nil {
			return err
		}
		if !eligible {
			s.BeforeCutoffTurns++
			continue
		}
		if s.reviewed(i + 1) {
			s.ReviewedTurns++
		} else {
			s.PendingTurns++
		}
	}
	return nil
}

func (r *Registry) pendingRanges(s Session, t transcript, through int) ([]Range, error) {
	ranges := []Range{}
	start := 0
	flush := func(end int) {
		if start > 0 {
			ranges = append(ranges, Range{SessionID: s.ID, FromTurn: start, ThroughTurn: end, Digest: digest(t.Turns[:end])})
			start = 0
		}
	}
	for i, turn := range t.Turns[:through] {
		eligible, err := r.eligible(turn)
		if err != nil {
			return nil, err
		}
		if eligible && !s.reviewed(i+1) {
			if start == 0 {
				start = i + 1
			}
		} else {
			flush(i)
		}
	}
	flush(through)
	return ranges, nil
}
