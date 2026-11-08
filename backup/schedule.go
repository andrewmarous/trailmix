package backup

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

func xmlText(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// Schedule installs or removes the current user's sync scheduler.
func (g Git) Schedule(root, remote string, enable bool) error {
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
	if runtime.GOOS == "linux" {
		return scheduleSystemd(root, remote, enable, g.AppConfig)
	}
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("automatic scheduling requires macOS or Linux with a systemd user session")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	label := "com.trailmix.sync"
	path := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
	domain := "gui/" + strconv.Itoa(os.Getuid())
	if !enable {
		_ = exec.Command("launchctl", "bootout", domain+"/"+label).Run()
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	logDir := filepath.Join(home, "Library", "Logs", "Trailmix")
	if err := os.MkdirAll(logDir, 0700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	configArgs := ""
	if g.AppConfig != "" {
		configArgs = "<string>--config</string><string>" + xmlText(g.AppConfig) + "</string>"
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>%s</string>
<key>ProgramArguments</key><array><string>%s</string>%s<string>--vault</string><string>%s</string><string>sync</string><string>--background</string><string>%s</string></array>
<key>StartInterval</key><integer>300</integer><key>RunAtLoad</key><true/>
<key>EnvironmentVariables</key><dict><key>PATH</key><string>/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin</string></dict>
</dict></plist>
`, label, xmlText(exe), configArgs, xmlText(root), xmlText(remote))
	if err := os.WriteFile(path, []byte(plist), 0600); err != nil {
		return err
	}
	_ = exec.Command("launchctl", "bootout", domain+"/"+label).Run()
	out, err := exec.Command("launchctl", "bootstrap", domain, path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("install sync agent: %s: %w", out, err)
	}
	return nil
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
