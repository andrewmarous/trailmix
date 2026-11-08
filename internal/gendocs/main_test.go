package main

import "testing"

func TestReplaceGuide(t *testing.T) {
	original := "# Contributor instructions\n\nKeep this prefix.\n\n" + start + "old guide\n" + end + "\n\nKeep this suffix.\n"
	block := start + "new guide\n" + end
	want := "# Contributor instructions\n\nKeep this prefix.\n\n" + block + "\n\nKeep this suffix.\n"
	got, err := replaceGuide(original, block)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("replacement = %q, want %q", got, want)
	}
	again, err := replaceGuide(got, block)
	if err != nil || again != want {
		t.Fatalf("second replacement = %q, %v; want unchanged output", again, err)
	}
}

func TestReplaceGuideRejectsInvalidMarkers(t *testing.T) {
	for _, text := range []string{"", start, end, end + start, start + start + end, start + end + end} {
		if _, err := replaceGuide(text, start+"new\n"+end); err == nil {
			t.Errorf("accepted invalid markers in %q", text)
		}
	}
}
