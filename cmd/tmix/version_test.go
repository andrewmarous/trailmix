package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	for _, arg := range []string{"--version", "version"} {
		var out, diagnostics bytes.Buffer
		if err := run([]string{arg}, strings.NewReader(""), &out, &diagnostics); err != nil {
			t.Fatal(err)
		}
		if want := "tmix " + version + " (commit " + commit + ", built " + date + ")\n"; out.String() != want {
			t.Fatalf("got %q, want %q", out.String(), want)
		}
	}
}
