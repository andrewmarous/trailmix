package backup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// systemdArg quotes a single ExecStart argument, including systemd specifiers.
func systemdArg(s string) string {
	s = strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "%", "%%", "$", "$$", "\n", "\\n", "\r", "\\r", "\t", "\\t").Replace(s)
	return "\"" + s + "\""
}

func systemctl(args ...string) error {
	out, err := exec.Command("systemctl", append([]string{"--user"}, args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %s: %w", strings.Join(args, " "), out, err)
	}
	return nil
}

func scheduleSystemd(root, remote string, enable bool, appConfig ...string) error {
	config, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(config, "systemd", "user")
	service := filepath.Join(dir, "trailmix-sync.service")
	timer := filepath.Join(dir, "trailmix-sync.timer")
	if err := systemctl("show-environment"); err != nil {
		return fmt.Errorf("a running systemd user session is required: %w", err)
	}
	if !enable {
		if err := systemctl("disable", "--now", "trailmix-sync.timer"); err != nil {
			return err
		}
		if err := systemctl("stop", "trailmix-sync.service"); err != nil {
			return err
		}
		for _, path := range []string{service, timer} {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		return systemctl("daemon-reload")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	configArg := ""
	if len(appConfig) > 0 && appConfig[0] != "" {
		configArg = " --config " + systemdArg(appConfig[0])
	}
	content := "[Unit]\nDescription=Sync Trailmix memory\n\n[Service]\nType=oneshot\nExecStart=" + systemdArg(exe) + configArg + " --vault " + systemdArg(root) + " sync --background " + systemdArg(remote) + "\nTimeoutStartSec=10min\n"
	if err := os.WriteFile(service, []byte(content), 0600); err != nil {
		return err
	}
	// OnActiveSec also covers installations after boot. Subsequent attempts run every five minutes.
	content = "[Unit]\nDescription=Sync Trailmix memory every five minutes\n\n[Timer]\nOnActiveSec=1s\nOnUnitInactiveSec=5min\nAccuracySec=1s\nUnit=trailmix-sync.service\n\n[Install]\nWantedBy=timers.target\n"
	if err := os.WriteFile(timer, []byte(content), 0600); err != nil {
		return err
	}
	if err := systemctl("daemon-reload"); err != nil {
		return err
	}
	return systemctl("enable", "--now", "trailmix-sync.timer")
}
