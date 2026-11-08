package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManualGuidance(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	root := filepath.Join(home, "vault")
	call := func(content string, args ...string) error {
		t.Helper()
		var out bytes.Buffer
		return run(append([]string{"--vault", root, "learning", "add"}, args...), strings.NewReader(content), &out, &out)
	}
	first := "# Retry\n\nRetry only transient failures.\n"
	if err := call(first, "procedures/retry"); err != nil {
		t.Fatal(err)
	}
	if err := call("# Replacement\n", "procedures/retry"); err == nil {
		t.Fatal("replaced guidance without approval")
	}
	if err := call("# Broken\n\n[[missing]]\n", "--overwrite", "procedures/retry"); err == nil {
		t.Fatal("published a broken link")
	}
	path := filepath.Join(root, "procedures", "retry.md")
	body, err := os.ReadFile(path)
	if err != nil || string(body) != first {
		t.Fatalf("failed publication changed the note: %q %v", body, err)
	}
	second := "# Retry\n\nRetry at most twice.\n"
	if err := call(second, "--overwrite", "procedures/retry"); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(path)
	if err != nil || string(body) != second {
		t.Fatalf("guidance not saved: %q %v", body, err)
	}
	if err := call("# Related\n\n[[procedures/retry]]\n", "related"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(root, ".learning"), filepath.Join(home, ".local", "state", "trailmix", "learning")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("manual guidance created review state: %s %v", path, err)
		}
	}
	for _, input := range []struct{ path, body string }{
		{"../outside", "# Unsafe\n"},
		{".hidden", "# Unsafe\n"},
		{"plain", "No title"},
		{"large", "# Large\n" + strings.Repeat("x", 64*1024)},
	} {
		if err := call(input.body, input.path); err == nil {
			t.Fatalf("accepted invalid guidance: %s", input.path)
		}
	}
}
