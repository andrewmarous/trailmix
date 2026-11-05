package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSystemdArg(t *testing.T) {
	cases := map[string]string{
		"/path with spaces/tmix": `"/path with spaces/tmix"`,
		"a%h$b":                  `"a%%h$$b"`,
		"a\"b\\c\nd":             `"a\"b\\c\nd"`,
	}
	for input, want := range cases {
		if got := systemdArg(input); got != want {
			t.Errorf("%q: got %q, want %q", input, got, want)
		}
	}
}

func TestSystemdInstallation(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	calls := filepath.Join(dir, "calls")
	t.Setenv("TEST_SYSTEMCTL_CALLS", calls)
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$TEST_SYSTEMCTL_CALLS\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := scheduleSystemd("/vault with spaces/%h", "origin", true); err != nil {
		t.Fatal(err)
	}
	config, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	units := filepath.Join(config, "systemd", "user")
	service, err := os.ReadFile(filepath.Join(units, "trailmix-sync.service"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(service), `--vault "/vault with spaces/%%h" sync --background "origin"`) {
		t.Fatal(string(service))
	}
	timer, err := os.ReadFile(filepath.Join(units, "trailmix-sync.timer"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(timer), "OnUnitInactiveSec=5min") {
		t.Fatal(string(timer))
	}
	if err := scheduleSystemd("/vault", "origin", false); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"trailmix-sync.service", "trailmix-sync.timer"} {
		if _, err := os.Stat(filepath.Join(units, name)); !os.IsNotExist(err) {
			t.Fatal("unit not removed", err)
		}
	}
	data, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--user daemon-reload", "--user enable --now trailmix-sync.timer", "--user disable --now trailmix-sync.timer", "--user stop trailmix-sync.service"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("missing %s in %s", want, data)
		}
	}
}
