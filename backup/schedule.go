package backup

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/andrewmarous/trailmix/schedule"
)

// Schedule installs or removes the current user's sync job.
func (g Git) Schedule(remote string, interval time.Duration, enable bool) error {
	if enable {
		remotes, err := g.load()
		if err != nil {
			return err
		}
		url, ok := remotes[remote]
		if !ok {
			return fmt.Errorf("remote %q is not configured; use tmix remote add", remote)
		}
		if err := validURL(url); err != nil {
			return err
		}
	}
	m, err := schedule.New(g.AppConfig, "sync")
	if err != nil {
		return err
	}
	if enable {
		return m.Enable(interval)
	}
	return m.Disable()
}

// Background writes bounded logs and reports persistent failures at most daily.
func (g Git) Background(root, remote string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, "Library", "Logs", "Trailmix")
	if runtime.GOOS == "linux" {
		state := os.Getenv("XDG_STATE_HOME")
		if state == "" {
			state = filepath.Join(home, ".local", "state")
		}
		dir = filepath.Join(state, "trailmix")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path := filepath.Join(dir, "sync.log")
	if info, err := os.Stat(path); err == nil && info.Size() > 5*1024*1024 {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	g.Out, g.Err = f, f
	if runtime.GOOS == "darwin" && os.Getenv("SSH_AUTH_SOCK") == "" {
		if sock, err := exec.Command("launchctl", "getenv", "SSH_AUTH_SOCK").Output(); err == nil {
			_ = os.Setenv("SSH_AUTH_SOCK", string(bytes.TrimSpace(sock)))
		}
	}
	err = g.Sync(root, remote)
	fmt.Fprintf(f, "%s sync: %v\n", time.Now().UTC().Format(time.RFC3339), err)
	if err == nil {
		_ = os.Remove(filepath.Join(root, ".tmx-sync-failing"))
		return nil
	}
	// Start the stale period on the first failure, including before the first successful sync.
	failing := filepath.Join(root, ".tmx-sync-failing")
	if _, e := os.Stat(failing); os.IsNotExist(e) {
		_ = os.WriteFile(failing, nil, 0600)
	}
	info, e := os.Stat(failing)
	notice := filepath.Join(root, ".tmx-sync-notified")
	previous, pe := os.Stat(notice)
	if e == nil && time.Since(info.ModTime()) > 24*time.Hour && (pe != nil || time.Since(previous.ModTime()) > 24*time.Hour) {
		if runtime.GOOS == "darwin" {
			_ = exec.Command("osascript", "-e", `display notification "Memory sync needs attention. Check tmix sync and ~/Library/Logs/Trailmix/sync.log." with title "Trailmix sync"`).Run()
		}
		_ = os.WriteFile(notice, nil, 0600)
	}
	return err
}
