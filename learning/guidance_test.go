package learning

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestGuidanceValidatesWholeBatchBeforePublishing(t *testing.T) {
	c, response := workerFixture(t, "pi", "automatic")
	result := lessonResult()
	result.Changes[0].Content += "\n[[procedures/details]]\n"
	result.Changes = append(result.Changes, Change{
		Path: "procedures/details.md", Content: "# Details\n\n[[missing]]\n",
		Evidence: []Evidence{{SessionID: "pi/p", Turn: 1}},
	})
	setResponse(t, response, result)
	if _, err := RunOnce(context.Background(), c); err == nil {
		t.Fatal("published a batch with a broken link in the second note")
	}
	assertReviewed(t, c, 0)
	for _, name := range []string{"retry.md", "details.md"} {
		if _, err := os.Stat(filepath.Join(c.Vault.Path, "procedures", name)); !os.IsNotExist(err) {
			t.Fatalf("published part of an invalid batch: %s %v", name, err)
		}
	}
	result.Changes[1].Content = "# Details\n\n[[procedures/retry]]\n"
	// The saved proposal is the durable result. Repair it without another model call.
	var id string
	if err := (Store{Dir: c.Learning.StateDir}).Transaction(func(r *Registry) error {
		id = r.active().ID
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	p, err := loadProposal(c.Learning.StateDir, id)
	if err != nil {
		t.Fatal(err)
	}
	p.Result = result
	if err = writeJSON(proposalPath(c.Learning.StateDir, id), p); err != nil {
		t.Fatal(err)
	}
	if err = ApplyProposal(c, id); err != nil {
		t.Fatal(err)
	}
	for _, change := range result.Changes {
		body, err := os.ReadFile(filepath.Join(c.Vault.Path, change.Path))
		if err != nil || string(body) != change.Content {
			t.Fatalf("guidance not published: %s %q %v", change.Path, body, err)
		}
	}
	assertReviewed(t, c, 1)
}

func TestManualGuidanceRejectsSymlinkedVault(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "vault")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if err := AddGuidance(link, "lesson", "# Lesson\n", false); err == nil {
		t.Fatal("accepted a symlinked vault")
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("wrote outside the vault: %v %v", entries, err)
	}
	if err := AddGuidance(root, "lesson", "# Lesson\n", false); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, "lesson.md"))
	if err != nil || string(body) != "# Lesson\n" {
		t.Fatalf("%q %v", body, err)
	}
}
