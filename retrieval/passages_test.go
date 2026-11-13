package retrieval

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func chunkNote(t *testing.T, body string, budget int) []passage {
	t.Helper()
	n := sourceNote{Path: "safety.md", Body: body, Hash: hash([]byte(body))}
	parts, err := splitNote(context.Background(), n, func(_ context.Context, s string) (bool, error) { return utf8.RuneCountInString(s) <= budget, nil })
	if err != nil {
		t.Fatal(err)
	}
	return parts
}

func TestMarkdownSectionsOwnOnlyTheirContent(t *testing.T) {
	body := "Preamble.\n\n# Safety\n\nIntro.\n\n## Empty\n\n### Checks\n\n- Keep the baseline.\n- Preserve evidence.\n\n## Examples\n\n```md\n# Not a heading\n```\n\n## Last\n"
	parts := chunkNote(t, body, 2000)
	var texts []string
	for _, p := range parts {
		texts = append(texts, p.Text)
		if p.Text != body[p.Start:p.End] || p.SourceHash != hash([]byte(body)) {
			t.Fatal("invalid source pointer")
		}
	}
	want := []string{"Preamble.\n\n", "# Safety\n\nIntro.\n\n", "### Checks\n\n- Keep the baseline.\n- Preserve evidence.\n\n", "## Examples\n\n```md\n# Not a heading\n```\n\n"}
	if !reflect.DeepEqual(texts, want) {
		t.Fatalf("sections: %#v", texts)
	}
	if parts[2].Input != "Note: Safety\nPath: safety.md\nSection: Safety > Empty > Checks\n\n"+want[2] {
		t.Fatalf("context: %q", parts[2].Input)
	}
	if parts[2].Line != 9 || parts[2].EndLine != 13 {
		t.Fatalf("source lines %d-%d", parts[2].Line, parts[2].EndLine)
	}
}

func TestUnheadedAndSetextNotes(t *testing.T) {
	for _, body := range []string{"A rule.\n\nIts evidence.\n", "Safety\n======\n\nA rule.\n\nChecks\n------\n\nIts evidence.\n"} {
		parts := chunkNote(t, body, 2000)
		var restored strings.Builder
		for _, p := range parts {
			restored.WriteString(p.Text)
		}
		if restored.String() != body {
			t.Fatalf("lost source: %q", restored.String())
		}
	}
	parts := chunkNote(t, "Safety\n======\n\nA rule.\n\nChecks\n------\n\nIts evidence.\n", 2000)
	if len(parts) != 2 || !strings.Contains(parts[1].Input, "Section: Safety > Checks\n") {
		t.Fatalf("setext sections: %+v", parts)
	}
}

func TestTokenFallbackPartitionsUnicodeAndKeepsBlocksWhenTheyFit(t *testing.T) {
	body := "# Safety\n\nFirst paragraph.\n\n```md\n# Not a section\nThis example stays whole.\n```\n\n| Rule | Evidence |\n| --- | --- |\n| Keep | Session 3 |\n\n" + strings.Repeat("安全。", 100) + "\n"
	parts := chunkNote(t, body, 150)
	var restored strings.Builder
	codeWhole, tableWhole := false, false
	for i, p := range parts {
		if !utf8.ValidString(p.Input) || utf8.RuneCountInString(p.Input) > 150 {
			t.Fatalf("invalid passage input: %q", p.Input)
		}
		if i > 0 && parts[i-1].End != p.Start {
			t.Fatal("gap or overlap")
		}
		codeWhole = codeWhole || strings.Contains(p.Text, "```md\n# Not a section\nThis example stays whole.\n```")
		tableWhole = tableWhole || strings.Contains(p.Text, "| Rule | Evidence |\n| --- | --- |\n| Keep | Session 3 |")
		restored.WriteString(p.Text)
	}
	if restored.String() != body || !codeWhole || !tableWhole {
		t.Fatalf("source preservation: code=%v table=%v", codeWhole, tableWhole)
	}
}

func TestOversizedParagraphKeepsItsHeadingWithContent(t *testing.T) {
	parts := chunkNote(t, "# Safety\n\n"+strings.Repeat("x", 200)+"\n", 100)
	for _, p := range parts {
		if !strings.Contains(p.Text, "x") {
			t.Fatalf("heading-only passage: %q", p.Text)
		}
	}
}

func TestSectionEditDoesNotChangeUnrelatedEmbeddingInput(t *testing.T) {
	before := chunkNote(t, "# Safety\n\n## First\n\nOld text.\n\n## Second\n\nKeep this.\n", 2000)
	after := chunkNote(t, "# Safety\n\n## First\n\nNew text with an extra line.\nMore text.\n\n## Second\n\nKeep this.\n", 2000)
	if before[1].Hash != after[1].Hash || before[1].Input != after[1].Input || before[1].Start == after[1].Start {
		t.Fatal("unrelated section did not retain its embedding identity and updated source location")
	}
}
