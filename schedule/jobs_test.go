package schedule

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestIndependentJobs(t *testing.T) {
	for _, platform := range []string{"darwin", "linux"} {
		t.Run(platform, func(t *testing.T) {
			learning := manager(t, platform)
			learning.Command = func(string, ...string) ([]byte, error) { return nil, nil }
			sync := learning
			sync.Task = "sync"
			for _, job := range []struct {
				manager  Manager
				interval time.Duration
				command  string
				period   string
			}{
				{sync, 5 * time.Minute, `"sync" "--background"`, "300"},
				{learning, time.Hour, `"learning" "run"`, "3600"},
			} {
				if err := job.manager.Enable(job.interval); err != nil {
					t.Fatal(err)
				}
				paths, _ := job.manager.files()
				body, err := os.ReadFile(paths[0])
				if err != nil {
					t.Fatal(err)
				}
				if platform == "darwin" {
					command := "<string>learning</string><string>run</string>"
					if job.manager.Task == "sync" {
						command = "<string>sync</string><string>--background</string>"
					}
					if !strings.Contains(string(body), command) || !strings.Contains(string(body), "<integer>"+job.period+"</integer>") {
						t.Fatal(string(body))
					}
				} else {
					if !strings.Contains(string(body), job.command) {
						t.Fatal(string(body))
					}
					timer, err := os.ReadFile(paths[1])
					if err != nil || !strings.Contains(string(timer), "OnUnitInactiveSec="+job.period+"s") {
						t.Fatalf("%s %v", timer, err)
					}
				}
			}
			if err := sync.Disable(); err != nil {
				t.Fatal(err)
			}
			paths, _ := learning.files()
			for _, path := range paths {
				body, err := os.ReadFile(path)
				if err != nil || !strings.Contains(string(body), "learning") {
					t.Fatalf("disabling sync affected learning: %s %v", body, err)
				}
			}
		})
	}
}

func TestSystemdArgument(t *testing.T) {
	for input, want := range map[string]string{
		"/path with spaces/tmix": `"/path with spaces/tmix"`,
		"a%h$b":                  `"a%%h$$b"`,
		"a\"b\\c\nd":             `"a\"b\\c\nd"`,
	} {
		if got := argument(input); got != want {
			t.Errorf("%q: got %q, want %q", input, got, want)
		}
	}
}
