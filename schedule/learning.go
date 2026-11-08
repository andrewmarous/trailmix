// Package schedule installs the current user's learning job without root access.
package schedule

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

const label = "com.trailmix.learning"
const service = "trailmix-learning.service"
const timer = "trailmix-learning.timer"

type Manager struct {
	OS         string
	Home       string
	ConfigHome string
	Executable string
	ConfigPath string
	Path       string
	UID        int
	Command    func(string, ...string) ([]byte, error)
}
type Status struct {
	Platform  string   `json:"platform"`
	Installed bool     `json:"installed"`
	Loaded    bool     `json:"loaded"`
	NextRun   string   `json:"next_run,omitempty"`
	Files     []string `json:"files"`
	Error     string   `json:"error,omitempty"`
}

func New(configPath string) (Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Manager{}, err
	}
	exe, err := exec.LookPath(os.Args[0])
	if err != nil {
		exe, err = os.Executable()
	}
	if err != nil {
		return Manager{}, err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return Manager{}, err
	}
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}
	return Manager{OS: runtime.GOOS, Home: home, ConfigHome: configHome, Executable: exe, ConfigPath: configPath, Path: os.Getenv("PATH"), UID: os.Getuid()}, nil
}
func (m Manager) command(name string, args ...string) ([]byte, error) {
	if m.Command != nil {
		return m.Command(name, args...)
	}
	// No model output or credentials are included in scheduler errors.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return out, nil
}
func text(s string) string { var b bytes.Buffer; _ = xml.EscapeText(&b, []byte(s)); return b.String() }
func argument(s string) string {
	s = strings.ReplaceAll(s, "%", "%%")
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "$", "$$")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	return "\"" + s + "\""
}

// Environment= does not expand $, unlike ExecStart=. Both expand % specifiers.
func environmentArgument(s string) string { return strings.ReplaceAll(argument(s), "$$", "$") }
func (m Manager) files() ([]string, error) {
	switch m.OS {
	case "darwin":
		return []string{filepath.Join(m.Home, "Library", "LaunchAgents", label+".plist")}, nil
	case "linux":
		dir := filepath.Join(m.ConfigHome, "systemd", "user")
		return []string{filepath.Join(dir, service), filepath.Join(dir, timer)}, nil
	default:
		return nil, fmt.Errorf("learning scheduling requires macOS launchd or Linux with a systemd user manager")
	}
}
func (m Manager) Render(interval time.Duration) (map[string][]byte, error) {
	paths, err := m.files()
	if err != nil {
		return nil, err
	}
	if interval < time.Minute || interval > 7*24*time.Hour || interval%time.Second != 0 {
		return nil, fmt.Errorf("schedule interval must be whole seconds between 1m and 168h")
	}
	if !filepath.IsAbs(m.Executable) || !filepath.IsAbs(m.ConfigPath) {
		return nil, fmt.Errorf("scheduler executable and config paths must be absolute")
	}
	files := map[string][]byte{}
	seconds := int64(interval / time.Second)
	if m.OS == "darwin" {
		files[paths[0]] = []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>%s</string>
<key>ProgramArguments</key><array><string>%s</string><string>--config</string><string>%s</string><string>learning</string><string>run</string></array>
<key>StartInterval</key><integer>%d</integer><key>RunAtLoad</key><true/>
<key>ProcessType</key><string>Background</string>
<key>EnvironmentVariables</key><dict><key>HOME</key><string>%s</string><key>PATH</key><string>%s</string></dict>
</dict></plist>
`, label, text(m.Executable), text(m.ConfigPath), seconds, text(m.Home), text(m.Path)))
	} else {
		files[paths[0]] = []byte("[Unit]\nDescription=Review Trailmix learning evidence\n\n[Service]\nType=oneshot\nExecStart=" + argument(m.Executable) + " --config " + argument(m.ConfigPath) + " learning run\nEnvironment=" + environmentArgument("PATH="+m.Path) + "\nUMask=0077\nTimeoutStartSec=3700\nKillMode=control-group\n")
		files[paths[1]] = []byte(fmt.Sprintf("[Unit]\nDescription=Schedule Trailmix learning\n\n[Timer]\nOnActiveSec=60s\nOnUnitInactiveSec=%ds\nUnit=%s\n\n[Install]\nWantedBy=timers.target\n", seconds, service))
	}
	return files, nil
}
func atomicFile(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return fmt.Errorf("scheduler file is not a regular file: %s", path)
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".tmix-schedule-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func (m Manager) domain() string { return "gui/" + strconv.Itoa(m.UID) }
func (m Manager) preflight() error {
	if m.OS == "linux" {
		if _, err := m.command("systemctl", "--user", "show-environment"); err != nil {
			return fmt.Errorf("a running systemd user manager is required; check your user session: %w", err)
		}
		return nil
	}
	if m.OS == "darwin" {
		_, err := m.command("launchctl", "print", m.domain())
		return err
	}
	_, err := m.files()
	return err
}
func (m Manager) lock() (func(), error) {
	if err := os.MkdirAll(m.ConfigHome, 0700); err != nil {
		return nil, err
	}
	lock := flock.New(filepath.Join(m.ConfigHome, ".trailmix-learning-schedule.lock"))
	if err := lock.Lock(); err != nil {
		return nil, err
	}
	return func() { _ = lock.Unlock() }, nil
}
func (m Manager) Enable(interval time.Duration) error {
	for _, part := range strings.Split(filepath.ToSlash(m.Executable), "/") {
		if strings.HasPrefix(part, "go-build") {
			return fmt.Errorf("install tmix at a stable path before scheduling; go run executables are temporary")
		}
	}
	files, err := m.Render(interval)
	if err != nil {
		return err
	}
	if err = m.preflight(); err != nil {
		return err
	}
	unlock, err := m.lock()
	if err != nil {
		return err
	}
	defer unlock()
	previous := m.Status()
	paths, _ := m.files()
	old := map[string][]byte{}
	for _, p := range paths {
		info, e := os.Lstat(p)
		if e == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
			return fmt.Errorf("unsafe scheduler file: %s", p)
		}
		b, e := os.ReadFile(p)
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		if e == nil {
			old[p] = b
		}
	}
	rollback := func() {
		if m.OS == "linux" {
			_, _ = m.command("systemctl", "--user", "disable", "--now", timer)
		}
		for _, p := range paths {
			if b, ok := old[p]; ok {
				_ = atomicFile(p, b)
			} else {
				_ = os.Remove(p)
			}
		}
		if m.OS == "linux" {
			_, _ = m.command("systemctl", "--user", "daemon-reload")
			if previous.Loaded {
				_, _ = m.command("systemctl", "--user", "enable", "--now", timer)
			}
		} else if len(old) > 0 && previous.Loaded {
			_, _ = m.command("launchctl", "bootstrap", m.domain(), paths[0])
		}
	}
	for _, p := range paths {
		if err = atomicFile(p, files[p]); err != nil {
			rollback()
			return err
		}
	}
	if m.OS == "linux" {
		if _, err = m.command("systemctl", "--user", "daemon-reload"); err == nil {
			_, err = m.command("systemctl", "--user", "enable", "--now", timer)
		}
		if err == nil {
			_, err = m.command("systemctl", "--user", "restart", timer)
		}
	} else {
		if _, e := m.command("launchctl", "print", m.domain()+"/"+label); e == nil {
			if _, err = m.command("launchctl", "bootout", m.domain()+"/"+label); err != nil {
				rollback()
				return err
			}
		}
		_, err = m.command("launchctl", "bootstrap", m.domain(), paths[0])
	}
	if err != nil {
		rollback()
		return fmt.Errorf("install learning scheduler: %w", err)
	}
	return nil
}
func (m Manager) Disable() error {
	paths, err := m.files()
	if err != nil {
		return err
	}
	if err = m.preflight(); err != nil {
		return err
	}
	unlock, err := m.lock()
	if err != nil {
		return err
	}
	defer unlock()
	if m.OS == "linux" {
		// Stop an in-flight worker too. A missing installation is an idempotent no-op.
		exists := false
		for _, p := range paths {
			if _, e := os.Stat(p); e == nil {
				exists = true
			}
		}
		if exists {
			if _, err = m.command("systemctl", "--user", "disable", "--now", timer); err != nil {
				return err
			}
			if _, err = m.command("systemctl", "--user", "stop", service); err != nil {
				return err
			}
		}
	} else if _, e := m.command("launchctl", "print", m.domain()+"/"+label); e == nil {
		if _, err = m.command("launchctl", "bootout", m.domain()+"/"+label); err != nil {
			return err
		}
	}
	for _, p := range paths {
		if err = os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if m.OS == "linux" {
		_, err = m.command("systemctl", "--user", "daemon-reload")
		return err
	}
	return nil
}
func (m Manager) Status() Status {
	s := Status{Platform: m.OS}
	paths, err := m.files()
	if err != nil {
		s.Error = err.Error()
		return s
	}
	s.Files = paths
	s.Installed = true
	for _, p := range paths {
		if _, err = os.Stat(p); err != nil {
			s.Installed = false
		}
	}
	if m.OS == "darwin" {
		_, err = m.command("launchctl", "print", m.domain()+"/"+label)
		s.Loaded = err == nil
	} else {
		_, err = m.command("systemctl", "--user", "is-active", timer)
		s.Loaded = err == nil
		if s.Loaded {
			out, _ := m.command("systemctl", "--user", "list-timers", "--all", "--no-legend", timer)
			s.NextRun = strings.TrimSpace(string(out))
		}
	}
	if err != nil && s.Installed {
		s.Error = err.Error()
	}
	return s
}
