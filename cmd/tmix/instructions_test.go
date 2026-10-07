package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"trailmix/guide"
)

func TestInstructions(t *testing.T) {
	var out, diagnostics bytes.Buffer
	// Instructions must work without an initialized vault.
	err := run([]string{"--vault", filepath.Join(t.TempDir(), "missing"), "instructions"}, strings.NewReader(""), &out, &diagnostics)
	if err != nil || out.String() != guide.Text {
		t.Fatalf("instructions: %v; output %q", err, out.String())
	}
	if diagnostics.Len() != 0 {
		t.Fatalf("unexpected diagnostics: %s", &diagnostics)
	}
	if err := run([]string{"instructions", "extra"}, strings.NewReader(""), &out, &diagnostics); err == nil {
		t.Fatal("accepted extra argument")
	}
}
