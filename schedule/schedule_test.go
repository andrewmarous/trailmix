package schedule

import (
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func manager(t *testing.T, platform string) Manager {
	t.Helper()
	home := t.TempDir()
	return Manager{Task: "learning", OS: platform, Home: home, ConfigHome: filepath.Join(home, "config"), Executable: "/bin/tmix with spaces/%h", ConfigPath: "/config/trailmix & more/config.toml", Path: "/opt/homebrew/bin:/usr/bin", UID: 501}
}
func TestRenderBothSchedulers(t *testing.T) {
	for _, platform := range []string{"darwin", "linux"} {
		t.Run(platform, func(t *testing.T) {
			m := manager(t, platform)
			files, err := m.Render(time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			for _, b := range files {
				body := string(b)
				if strings.HasSuffix(body, "</plist>\n") {
					decoder := xml.NewDecoder(strings.NewReader(body))
					for {
						_, err := decoder.Token()
						if err == io.EOF {
							break
						}
						if err != nil {
							t.Fatal(err)
						}
					}
					if !strings.Contains(body, "<integer>3600</integer>") || !strings.Contains(body, "&amp;") {
						t.Fatal(body)
					}
				} else if strings.Contains(body, "ExecStart=") {
					if !strings.Contains(body, `"/bin/tmix with spaces/%%h" "--config" "/config/trailmix & more/config.toml" "learning" "run"`) {
						t.Fatal(body)
					}
				} else if !strings.Contains(body, "OnUnitInactiveSec=3600s") {
					t.Fatal(body)
				}
			}
		})
	}
}
func TestLinuxLearningCanDrainBeyondOneHour(t *testing.T) {
	for _, task := range []string{"learning", "sync"} {
		m := manager(t, "linux")
		m.Task = task
		files, err := m.Render(time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		body := string(files[filepath.Join(m.ConfigHome, "systemd", "user", "trailmix-"+task+".service")])
		want := "TimeoutStartSec=infinity\n"
		if task == "sync" {
			want = "TimeoutStartSec=3700\n"
		}
		if !strings.Contains(body, want) {
			t.Fatalf("%s service has wrong deadline: %s", task, body)
		}
	}
}

func TestInstallUpdateDisableAndRollback(t *testing.T) {
	for _, platform := range []string{"darwin", "linux"} {
		t.Run(platform, func(t *testing.T) {
			m := manager(t, platform)
			label, _, _ := m.names()
			loaded := false
			fail := false
			m.Command = func(name string, args ...string) ([]byte, error) {
				joined := strings.Join(args, " ")
				if strings.Contains(joined, "is-active") || ((args[0] == "print" || args[0] == "bootout") && strings.Contains(joined, "/"+label)) {
					if strings.Contains(joined, "bootout") {
						loaded = false
						return nil, nil
					}
					if !loaded {
						return nil, errors.New("not loaded")
					}
				}
				if strings.Contains(joined, "bootstrap") || strings.Contains(joined, "enable --now") {
					if fail {
						return nil, errors.New("injected install failure")
					}
					loaded = true
				}
				if strings.Contains(joined, "disable --now") {
					loaded = false
				}
				return nil, nil
			}
			if err := m.Enable(time.Hour); err != nil {
				t.Fatal(err)
			}
			if s := m.Status(); !s.Installed || !s.Loaded {
				t.Fatal(s)
			}
			if err := m.Enable(2 * time.Hour); err != nil {
				t.Fatal(err)
			}
			paths, _ := m.files()
			old, _ := os.ReadFile(paths[len(paths)-1])
			fail = true
			if err := m.Enable(3 * time.Hour); err == nil {
				t.Fatal("ignored installation failure")
			}
			restored, _ := os.ReadFile(paths[len(paths)-1])
			if string(old) != string(restored) {
				t.Fatal("failed installation did not restore old files")
			}
			fail = false
			if err := m.Disable(); err != nil {
				t.Fatal(err)
			}
			if err := m.Disable(); err != nil {
				t.Fatal("disable is not idempotent:", err)
			}
			if s := m.Status(); s.Installed {
				t.Fatal(s)
			}
		})
	}
}
func TestUnsupportedAndUnavailableManager(t *testing.T) {
	m := manager(t, "windows")
	if err := m.Enable(time.Hour); err == nil {
		t.Fatal("Windows scheduler installed")
	}
	m = manager(t, "linux")
	m.Command = func(string, ...string) ([]byte, error) { return nil, errors.New("no user session") }
	if err := m.Enable(time.Hour); err == nil {
		t.Fatal("missing systemd ignored")
	}
	paths, _ := m.files()
	if _, err := os.Stat(paths[0]); !os.IsNotExist(err) {
		t.Fatal("files written before preflight")
	}
}
