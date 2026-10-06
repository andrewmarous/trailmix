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

func TestSearch(t *testing.T) {
	v := setup(t)
	notes := map[string]string{
		"preferences/verification.md": "# Verification\nVPN access to database",
		"experiences/deployment.md":   "VPN database verification",
		"concepts/unrelated.md":       "VPN only",
	}
	for name, body := range notes {
		if err := v.Write(name, []byte(body), false); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(v.Root, ".obsidian", "hidden.md"), []byte("VPN database verification"), 0600); err != nil {
		t.Fatal(err)
	}
	results, err := v.Search("VPN DATABASE verification", 10)
	if err != nil || len(results) != 2 {
		t.Fatalf("%+v %v", results, err)
	}
	if results[0].Path != "preferences/verification.md" {
		t.Fatalf("ranking: %+v", results)
	}
	results, err = v.Search("VPN", 1)
	if err != nil || len(results) != 1 {
		t.Fatalf("limit: %v %v", results, err)
	}
	for _, query := range []string{"", "  "} {
		if _, err := v.Search(query, 10); err == nil {
			t.Fatal("empty query accepted")
		}
	}
	if _, err := v.Search("VPN", 0); err == nil {
		t.Fatal("invalid limit accepted")
	}
	results, err = v.Search("nothing", 10)
	if err != nil || len(results) != 0 {
		t.Fatal(results, err)
	}
}
