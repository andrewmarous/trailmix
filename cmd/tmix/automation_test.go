package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andrewmarous/trailmix/backup"
	"github.com/andrewmarous/trailmix/config"
	"github.com/pelletier/go-toml/v2"
)

func TestSharedConfigAndLearningCLI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	path := filepath.Join(home, "config.toml")
	c := config.Defaults(home)
	c.Vault.Path = filepath.Join(home, "configured-vault")
	c.Learning.StateDir = filepath.Join(home, "configured-state")
	c.Learning.Harness = "pi"
	c.Learning.Publication = "proposals"
	c.Learning.Sources = config.Sources{}
	c.Learning.Executable = filepath.Join(home, "fake-pi")
	if err := os.WriteFile(c.Learning.Executable, []byte("#!/bin/sh\ncat >/dev/null\nprintf '%s' '{\"summary\":\"Create discovery map.\",\"changes\":[{\"path\":\"navigation/index.md\",\"content\":\"# Memory map\\n\\nUse search for topics omitted here.\\n\",\"evidence\":[]}]}'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	b, _ := toml.Marshal(c)
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	invoke := func(args ...string) (string, error) {
		var out bytes.Buffer
		err := run(append([]string{"--config", path}, args...), strings.NewReader(""), &out, &out)
		return out.String(), err
	}
	if _, err := invoke("init"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(c.Vault.Path, ".obsidian")); err != nil {
		t.Fatal("configured vault not used:", err)
	}
	if _, err := os.Stat(filepath.Join(c.Learning.StateDir, "registry.json")); err != nil {
		t.Fatal("configured state not initialized:", err)
	}
	if _, err := invoke("learning", "sessions", "--pi-root", "", "--codex-root", "", "--codex-archive-root", "", "scan"); err != nil {
		t.Fatal(err)
	}
	out, err := invoke("learning", "run")
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err = json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if result["status"] != "awaiting_approval" {
		t.Fatal(out)
	}
	id, ok := result["navigation_id"].(string)
	if !ok {
		t.Fatal(out)
	}
	if _, err = invoke("learning", "accept", id); err != nil {
		t.Fatal(err)
	}
	out, err = invoke("learning", "run")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if result["status"] != "no_work" {
		t.Fatal(out)
	}
	if body, err := os.ReadFile(filepath.Join(c.Vault.Path, "navigation/index.md")); err != nil || string(body) != "# Memory map\n\nUse search for topics omitted here.\n" {
		t.Fatalf("%s %v", body, err)
	}
	out, err = invoke("learning", "navigation", "build")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	id, ok = result["navigation_id"].(string)
	if !ok {
		t.Fatal(out)
	}
	if _, err = invoke("learning", "navigation", "discard", id); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.Learning.StateDir, "last_failure.json"), []byte(`{"status":"failed","error":"synthetic batch failure","turns_remaining":2}`), 0600); err != nil {
		t.Fatal(err)
	}
	out, err = invoke("learning", "schedule", "status")
	if err != nil {
		t.Fatal(err)
	}
	var health struct {
		LastJob struct {
			Status string `json:"status"`
		} `json:"last_job"`
		LastFailure struct {
			Error          string `json:"error"`
			TurnsRemaining int    `json:"turns_remaining"`
		} `json:"last_failure"`
	}
	if err := json.Unmarshal([]byte(out), &health); err != nil {
		t.Fatal(err)
	}
	if health.LastJob.Status != "no_work" || health.LastFailure.Error != "synthetic batch failure" || health.LastFailure.TurnsRemaining != 2 {
		t.Fatal(out)
	}
	if _, err = invoke("remote", "add", "origin", "git@example.com:notes.git"); err != nil {
		t.Fatal(err)
	}
	c, err = config.Load(path, home)
	if err != nil {
		t.Fatal(err)
	}
	if c.Learning.Publication != "proposals" || c.Remotes["origin"] != "git@example.com:notes.git" {
		t.Fatal("remote edit lost shared settings")
	}
}
func TestLegacyRemotesMigrateWithoutResurrection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	path := config.Path(home)
	legacyDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	g := backup.Git{Config: filepath.Join(legacyDir, "trailmix", "remotes.gitconfig"), Out: &out, Err: &out}
	if err = g.Remote([]string{"add", "origin", "git@example.com:legacy.git"}); err != nil {
		t.Fatal(err)
	}
	invoke := func(args ...string) string {
		t.Helper()
		out.Reset()
		if err := run(args, strings.NewReader(""), &out, &out); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	isolated := filepath.Join(home, "isolated.toml")
	if err = config.Create(isolated); err != nil {
		t.Fatal(err)
	}
	if text := invoke("--config", isolated, "remote"); text != "" {
		t.Fatal("alternate config inherited production remotes:", text)
	}
	if text := invoke("remote", "get-url", "origin"); !strings.Contains(text, "legacy.git") {
		t.Fatal(text)
	}
	invoke("remote", "set-url", "origin", "git@example.com:new.git")
	c, err := config.Load(path, home)
	if err != nil {
		t.Fatal(err)
	}
	if c.Remotes["origin"] != "git@example.com:new.git" {
		t.Fatal("migration failed")
	}
	invoke("remote", "remove", "origin")
	if text := invoke("remote"); text != "" {
		t.Fatal("legacy remote resurrected:", text)
	}
}
