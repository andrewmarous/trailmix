package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLI(t *testing.T) {
	root := filepath.Join(t.TempDir(), "vault")
	invoke := func(input string, args ...string) (string, error) {
		t.Helper()
		var out, diagnostics bytes.Buffer
		err := run(append([]string{"--vault", root}, args...), strings.NewReader(input), &out, &diagnostics)
		return out.String(), err
	}
	if _, err := invoke("", "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := invoke("# Verification\nVPN database\n", "write", "preferences/verification"); err != nil {
		t.Fatal(err)
	}
	if _, err := invoke("extra\n", "append", "preferences/verification"); err != nil {
		t.Fatal(err)
	}
	out, err := invoke("", "read", "preferences/verification")
	if err != nil || out != "# Verification\nVPN database\nextra\n" {
		t.Fatal(out, err)
	}
	out, err = invoke("", "search", "--json", "VPN database")
	if err != nil || !strings.Contains(out, `"path":"preferences/verification.md"`) {
		t.Fatal(out, err)
	}
	if _, err := invoke("oops", "write", "preferences/verification"); err == nil {
		t.Fatal("overwrite accepted")
	}
	for _, args := range [][]string{{"unknown"}, {"read"}, {"init", "unexpected"}, {"search"}, {"write", "a", "b"}} {
		if _, err := invoke("", args...); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	out, err = invoke("", "--help")
	if err != nil || !strings.Contains(out, "Usage:") {
		t.Fatal(out, err)
	}
}
