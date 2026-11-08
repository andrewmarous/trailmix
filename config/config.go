// Package config holds machine-local, user-editable Trailmix settings.
package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Remotes  map[string]string `toml:"remotes"`
	Sync     Sync              `toml:"sync"`
	Vault    Vault             `toml:"vault"`
	Learning Learning          `toml:"learning"`
}
type Sync struct {
	Remote string `toml:"remote"`
}
type Vault struct {
	Path string `toml:"path"`
}
type Sources struct {
	Pi           string `toml:"pi"`
	Codex        string `toml:"codex"`
	CodexArchive string `toml:"codex_archive"`
}
type Learning struct {
	Sources       Sources  `toml:"sources"`
	Harness       string   `toml:"harness"`
	Executable    string   `toml:"executable"`
	Model         string   `toml:"model"`
	Publication   string   `toml:"publication"`
	StateDir      string   `toml:"state_dir"`
	Timeout       string   `toml:"timeout"`
	MaxTurns      int      `toml:"max_turns"`
	MaxInputBytes int      `toml:"max_input_bytes"`
	Schedule      Schedule `toml:"schedule"`
}
type Schedule struct {
	Interval string `toml:"interval"`
}

func Path(home string) string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "trailmix", "config.toml")
}
func Defaults(home string) Config {
	var c Config
	c.Sync.Remote = "origin"
	c.Vault.Path = filepath.Join(home, ".trailmix")
	c.Learning.StateDir = filepath.Join(home, ".local", "state", "trailmix", "learning")
	c.Learning.Timeout = "20m"
	c.Learning.MaxTurns = 20
	c.Learning.MaxInputBytes = 2 * 1024 * 1024
	c.Learning.Schedule.Interval = "1h"
	c.Learning.Sources = Sources{
		Pi:           filepath.Join(home, ".pi", "agent", "sessions"),
		Codex:        filepath.Join(home, ".codex", "sessions"),
		CodexArchive: filepath.Join(home, ".codex", "archived_sessions"),
	}
	return c
}
func Load(path, home string) (Config, error) {
	c := Defaults(home)
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err = toml.NewDecoder(bytes.NewReader(b)).DisallowUnknownFields().Decode(&c); err != nil {
		return c, fmt.Errorf("config %s: %w", path, err)
	}
	// The decoder leaves empty tables nil. Preserve an explicitly empty
	// remotes table so removed legacy remotes do not reappear.
	var sections map[string]any
	if err = toml.Unmarshal(b, &sections); err != nil {
		return c, err
	}
	if _, present := sections["remotes"]; present && c.Remotes == nil {
		c.Remotes = map[string]string{}
	}
	c.Vault.Path = filepath.Clean(Expand(c.Vault.Path, home))
	c.Learning.StateDir = filepath.Clean(Expand(c.Learning.StateDir, home))
	for _, root := range []*string{&c.Learning.Sources.Pi, &c.Learning.Sources.Codex, &c.Learning.Sources.CodexArchive} {
		if *root != "" {
			*root = Expand(*root, home)
			if !filepath.IsAbs(*root) {
				return c, fmt.Errorf("learning source roots must be absolute paths or empty to disable")
			}
			*root = filepath.Clean(*root)
		}
	}
	if strings.Contains(c.Learning.Executable, "/") {
		c.Learning.Executable = Expand(c.Learning.Executable, home)
	}
	if strings.Contains(c.Learning.Executable, "/") && !filepath.IsAbs(c.Learning.Executable) {
		return c, fmt.Errorf("learning.executable must be an absolute path or a command name")
	}
	if !filepath.IsAbs(c.Vault.Path) || !filepath.IsAbs(c.Learning.StateDir) {
		return c, fmt.Errorf("vault.path and learning.state_dir must be absolute paths")
	}
	return c, nil
}
func Expand(s, home string) string {
	if s == "~" {
		return home
	}
	if strings.HasPrefix(s, "~/") {
		return filepath.Join(home, s[2:])
	}
	return s
}
func (c Config) ValidateLearning() error {
	l := c.Learning
	if l.Harness != "pi" && l.Harness != "codex" {
		return fmt.Errorf("set learning.harness to pi or codex in config.toml")
	}
	if l.Publication != "automatic" && l.Publication != "proposals" {
		return fmt.Errorf("explicitly set learning.publication to automatic or proposals")
	}
	if l.MaxTurns < 1 || l.MaxTurns > 100 {
		return fmt.Errorf("learning.max_turns must be between 1 and 100")
	}
	if l.MaxInputBytes < 1024 || l.MaxInputBytes > 16*1024*1024 {
		return fmt.Errorf("learning.max_input_bytes must be between 1024 and 16777216")
	}
	d, err := time.ParseDuration(l.Timeout)
	if err != nil || d < time.Second || d > time.Hour {
		return fmt.Errorf("learning.timeout must be between 1s and 1h")
	}
	d, err = time.ParseDuration(l.Schedule.Interval)
	if err != nil || d < time.Minute || d > 7*24*time.Hour || d%time.Second != 0 {
		return fmt.Errorf("learning.schedule.interval must be whole seconds between 1m and 168h")
	}
	return nil
}

const Template = `# Machine-local settings. Authenticate through Pi or Codex, not this file.
# Named remotes can be managed with tmix remote; legacy remotes migrate on edit.
[sync]
remote = "origin"

[vault]
path = "~/.trailmix"

[learning]
# Select pi or codex and explicitly select automatic or proposals.
harness = ""
executable = ""
model = "" # Empty uses the harness default.
publication = ""
state_dir = "~/.local/state/trailmix/learning"
timeout = "20m"
max_turns = 20
max_input_bytes = 2097152

[learning.sources]
# Omitted roots use defaults. Empty strings disable a source.
# pi = "~/.pi/agent/sessions"
# codex = "~/.codex/sessions"
# codex_archive = "~/.codex/archived_sessions"

[learning.schedule]
interval = "1h"
# Enable/disable through tmix learning schedule; edits require re-enabling.
`

func Create(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.WriteString(Template)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
