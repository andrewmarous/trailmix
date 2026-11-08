package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigDefaultsAndStrictTOML(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "config.toml")
	c, err := Load(path, home)
	if err != nil {
		t.Fatal(err)
	}
	if c.Learning.Schedule.Interval != "1h" || c.Learning.Publication != "" {
		t.Fatal("unsafe defaults")
	}
	if err = Create(path); err != nil {
		t.Fatal(err)
	}
	if err = Create(path); err == nil {
		t.Fatal("config init overwrote existing file")
	}
	c, err = Load(path, home)
	if err != nil {
		t.Fatal(err)
	}
	if c.Vault.Path != filepath.Join(home, ".trailmix") {
		t.Fatal("tilde not expanded")
	}
	if err = c.ValidateLearning(); err == nil {
		t.Fatal("unconfigured learning accepted")
	}
	if err = os.WriteFile(path, []byte("[learning]\nharnes='pi'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Load(path, home); err == nil {
		t.Fatal("typo silently ignored")
	}
}
func TestRemoteMigrationPreservesSettingsAndEmptyMap(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, "config.toml")
	if err := os.WriteFile(path, []byte("[learning]\nharness='codex'\npublication='proposals'\n[learning.schedule]\ninterval='2h'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, remotes := range []map[string]string{{"origin": "git@example.com:notes.git"}, {}} {
		if err := SaveRemotes(path, remotes); err != nil {
			t.Fatal(err)
		}
		c, err := Load(path, home)
		if err != nil {
			t.Fatal(err)
		}
		if c.Remotes == nil || len(c.Remotes) != len(remotes) {
			b, _ := os.ReadFile(path)
			t.Fatalf("empty map will resurrect legacy remotes: %#v; TOML: %s", c.Remotes, b)
		}
		if c.Learning.Harness != "codex" || c.Learning.Schedule.Interval != "2h" {
			t.Fatal("remote edit lost learning settings")
		}
	}
}
