package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setup(t *testing.T) *Vault {
	t.Helper()
	v, err := Open(filepath.Join(t.TempDir(), "vault"))
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Init(); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestLifecycle(t *testing.T) {
	v := setup(t)
	if err := v.Write("preferences/verification", []byte("# Verification\nVPN database"), false); err != nil {
		t.Fatal(err)
	}
	if err := v.Init(); err != nil {
		t.Fatal(err)
	}
	for _, dir := range Categories {
		if _, err := os.Stat(filepath.Join(v.Root, dir)); err != nil {
			t.Fatal(err)
		}
	}
	if err := v.Write("preferences/verification", []byte("lost"), false); err == nil {
		t.Fatal("overwrite allowed")
	}
	if err := v.Append("preferences/verification", []byte("\nTest connectivity.")); err != nil {
		t.Fatal(err)
	}
	data, err := v.Read("preferences/verification")
	if err != nil || !strings.HasSuffix(string(data), "\nTest connectivity.") {
		t.Fatalf("read: %q %v", data, err)
	}
	if err := v.Append("missing", []byte("x")); err == nil {
		t.Fatal("append created note")
	}
	if err := v.Write("preferences/verification", []byte("replacement"), true); err != nil {
		t.Fatal(err)
	}
	data, _ = v.Read("preferences/verification")
	if string(data) != "replacement" {
		t.Fatal(string(data))
	}
}

func TestSafety(t *testing.T) {
	v := setup(t)
	for _, path := range []string{"../escape.md", "/tmp/escape.md", ".obsidian/config.md", "x.txt", ""} {
		if err := v.Write(path, []byte("x"), true); err == nil {
			t.Errorf("accepted %q", path)
		}
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(v.Root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := v.Write("link/escape.md", []byte("x"), true); err == nil {
		t.Fatal("followed symlink")
	}
	if _, err := v.Read("link/escape.md"); err == nil {
		t.Fatal("read symlink")
	}
	if err := os.Symlink(outside, filepath.Join(v.Root, "inbox", "alias.md")); err != nil {
		t.Fatal(err)
	}
	if err := v.Write("inbox/alias.md", []byte("x"), true); err == nil {
		t.Fatal("replaced symlink")
	}
	if err := v.Write("inbox/invalid.md", []byte{0xff}, false); err == nil {
		t.Fatal("accepted invalid UTF-8")
	}
	rootLink := filepath.Join(t.TempDir(), "root")
	if err := os.Symlink(v.Root, rootLink); err != nil {
		t.Fatal(err)
	}
	linked, _ := Open(rootLink)
	if err := linked.Init(); err == nil {
		t.Fatal("accepted symlink root")
	}
}
