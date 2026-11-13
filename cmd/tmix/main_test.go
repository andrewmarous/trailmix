package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func run(args []string, in io.Reader, out, diagnostics io.Writer) error {
	return runCommand(context.Background(), args, in, out, diagnostics)
}

func TestCLI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	configPath := filepath.Join(configHome, "trailmix", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(fmt.Sprintf("[retrieval]\nruntime = \"/missing/llama-server\"\nmodel = \"/missing/model.gguf\"\nstate_dir = %q\n", t.TempDir())), 0600); err != nil {
		t.Fatal(err)
	}
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
	if _, err = invoke("", "index"); err != nil {
		t.Fatal(err)
	}
	out, err = invoke("", "search", "--json", "VPN database")
	if err != nil || out != "[{\"path\":\"preferences/verification.md\",\"line\":2,\"snippet\":\"VPN database\"}]\n" {
		t.Fatal(out, err)
	}
	if _, err = invoke("Changed source.\n", "append", "preferences/verification"); err != nil {
		t.Fatal(err)
	}
	var staleOut, staleDiagnostics bytes.Buffer
	err = run([]string{"--vault", root, "search", "--json", "VPN"}, strings.NewReader(""), &staleOut, &staleDiagnostics)
	if err != nil || staleOut.String() != "[]\n" || !strings.Contains(staleDiagnostics.String(), "excluded changed note") || !strings.Contains(staleDiagnostics.String(), "no valid indexed matches remain") {
		t.Fatalf("stale stdout=%q stderr=%q: %v", staleOut.String(), staleDiagnostics.String(), err)
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
