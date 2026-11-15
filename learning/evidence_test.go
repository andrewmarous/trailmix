package learning

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDisjointCitationResolution(t *testing.T) {
	c, _ := workerFixture(t, "pi", "automatic")
	timedPi(t, filepath.Join(c.Learning.Sources.Pi, "pi.jsonl"), "p", "2000-01-01T00:00:00Z", "2000-01-02T00:00:00Z", "2000-01-03T00:00:00Z", "2000-01-04T00:00:00Z")
	err := (Store{Dir: c.Learning.StateDir}).Transaction(func(r *Registry) error {
		if err := r.Scan(ConfiguredSources(c.Learning.Sources)); err != nil {
			return err
		}
		ref, err := parseCitation("trailmix://session/pi/p?turns=4,1,3-4")
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(ref.Turns, []int{1, 3, 4}) {
			t.Fatalf("turn set: %v", ref.Turns)
		}
		if got := evidenceURI(ref.SessionID, ref.Turns); got != "trailmix://session/pi/p?turns=1,3-4" {
			t.Fatalf("canonical URI: %s", got)
		}
		turns, err := r.resolveEvidence(ref)
		if err != nil {
			return err
		}
		if len(turns) != 3 || turns[1].CompletedAt != "2000-01-03T00:00:00Z" {
			t.Fatalf("resolved wrong turns: %+v", turns)
		}
		ch := Change{Path: "lesson.md", Content: "# Lesson\n[Evidence](trailmix://session/pi/p?turns=1,3-4)", Evidence: []Evidence{{SessionID: "pi/p", Turn: 3}}}
		if err := validateEvidenceReferences(r, []Change{ch}); err != nil {
			return err
		}
		ch.Content = "# Lesson\n[Evidence](trailmix://session/pi/p?turns=5)"
		if err := validateEvidenceReferences(r, []Change{ch}); err == nil || !strings.Contains(err.Error(), "only 4 completed turns") {
			t.Fatalf("bad range: %v", err)
		}
		ch.Content = "# Lesson\nNo machine citation."
		if err := validateEvidenceReferences(r, []Change{ch}); err == nil || !strings.Contains(err.Error(), "omits evidence") {
			t.Fatalf("missing pointer: %v", err)
		}
		if err := os.Remove(filepath.Join(c.Learning.Sources.Pi, "pi.jsonl")); err != nil {
			return err
		}
		ch.Content = "# Lesson\n[Evidence](trailmix://session/pi/p?turns=3)"
		if err := validateEvidenceReferences(r, []Change{ch}); err == nil || !strings.Contains(err.Error(), "evidence session pi/p") {
			t.Fatalf("missing source: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCitationRejectsAmbiguousOrUnsafeReferences(t *testing.T) {
	for _, raw := range []string{
		"trailmix://session/pi/p?turns=0", "trailmix://session/pi/p?turns=3-2", "trailmix://session/pi/p?turns=1-1001",
		"trailmix://session/pi/../p?turns=1", "trailmix://session/pi/p?turns=1&turns=2", "trailmix://session/pi/p?turns=1&extra=x",
		"trailmix://session/pi/p?turns=1#fragment", "trailmix://session/other/p?turns=1",
	} {
		if _, err := parseCitation(raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	ref, err := parseCitation("trailmix://session/codex/abc-123?turns=2,9-10")
	if err != nil || ref.SessionID != "codex/abc-123" || !reflect.DeepEqual(ref.Turns, []int{2, 9, 10}) {
		t.Fatalf("harness-independent reference: %+v %v", ref, err)
	}
}
