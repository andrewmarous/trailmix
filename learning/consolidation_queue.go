package learning

import "sort"

// Detect manual edits as well as worker writes. Remember observed inputs, not
// presumed correctness; the sweep revisits unchanged notes independently.
func refreshConsolidationQueue(s *consolidationState, notes map[string]string) {
	pending := make([]string, 0, len(s.Pending))
	seen := map[string]bool{}
	for _, p := range s.Pending {
		if _, ok := notes[p]; ok && substantive(p) && !seen[p] {
			pending = append(pending, p)
			seen[p] = true
		}
	}
	paths := make([]string, 0, len(notes))
	for p := range notes {
		if substantive(p) {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	known := map[string]string{}
	for _, p := range paths {
		hash := noteHash(notes[p])
		known[p] = hash
		if s.Known[p] != hash && !seen[p] {
			pending = append(pending, p)
			seen[p] = true
		}
	}
	s.Pending = pending
	s.Known = known
}
