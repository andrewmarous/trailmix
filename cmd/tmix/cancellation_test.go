package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/andrewmarous/trailmix/retrieval"
)

// Opt-in: exercises the real CLI, model, signals, and persistent retry state.
func TestLiveIndexCancellation(t *testing.T) {
	runtime, model := os.Getenv("TMIX_TEST_LLAMA"), os.Getenv("TMIX_TEST_MODEL")
	if runtime == "" || model == "" {
		t.Skip("set TMIX_TEST_LLAMA and TMIX_TEST_MODEL for local inference")
	}
	if _, err := exec.LookPath("pgrep"); err != nil {
		t.Fatal("live cancellation check requires pgrep")
	}
	dir := t.TempDir()
	binary, root, state := filepath.Join(dir, "tmix"), filepath.Join(dir, "vault"), filepath.Join(dir, "index")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.toml")
	config := fmt.Sprintf("[retrieval]\nmode = \"hybrid\"\nruntime = %q\nmodel = %q\nstate_dir = %q\n", runtime, model, state)
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	command := func() *exec.Cmd {
		cmd := exec.Command(binary, "--config", configPath, "--vault", root, "index")
		cmd.Env = append(os.Environ(), "HOME="+dir, "XDG_CONFIG_HOME="+filepath.Join(dir, "config"))
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		return cmd
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name+".md"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("baseline", "# Baseline\nPublication checks protect newer edits.\n")
	if out, err := command().CombinedOutput(); err != nil {
		t.Fatalf("baseline: %v\n%s", err, out)
	}
	db, err := sql.Open("sqlite3", filepath.Join(state, "index.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var original string
	if err = db.QueryRow("SELECT value FROM metadata WHERE key='snapshot'").Scan(&original); err != nil {
		t.Fatal(err)
	}
	count := func() int {
		t.Helper()
		var n int
		if err := db.QueryRow("SELECT count(*) FROM embeddings").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for i := 0; i < 16; i++ {
		write(fmt.Sprintf("pending-%02d", i), fmt.Sprintf("# Safety %d\n%s\n", i, strings.Repeat(" safety", 1500)))
	}
	for _, signal := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		t.Run(signal.String(), func(t *testing.T) {
			before := count()
			cmd := command()
			var output bytes.Buffer
			cmd.Stdout, cmd.Stderr = &output, &output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			child := 0
			waited := false
			defer func() {
				// Also clean up if startup or the batch-progress check fails.
				if !waited && child == 0 {
					if ids, err := exec.Command("pgrep", "-P", strconv.Itoa(cmd.Process.Pid)).Output(); err == nil {
						for _, id := range strings.Fields(string(ids)) {
							if pid, err := strconv.Atoi(id); err == nil {
								_ = syscall.Kill(pid, syscall.SIGKILL)
							}
						}
					}
				}
				_ = cmd.Process.Kill()
				if child != 0 {
					_ = syscall.Kill(child, syscall.SIGKILL)
				}
			}()
			deadline := time.NewTimer(2 * time.Minute)
			defer deadline.Stop()
			for count() < before+2 {
				select {
				case err := <-done:
					waited = true
					t.Fatalf("index exited before interruption: %v\n%s", err, output.String())
				case <-deadline.C:
					t.Fatal("index did not commit an inference batch")
				case <-time.After(10 * time.Millisecond):
				}
			}
			children, err := exec.Command("pgrep", "-P", strconv.Itoa(cmd.Process.Pid)).Output()
			if err != nil {
				t.Fatalf("find runtime: %v", err)
			}
			ids := strings.Fields(string(children))
			if len(ids) != 1 {
				t.Fatalf("expected one operation-owned runtime, got %q", children)
			}
			child, err = strconv.Atoi(ids[0])
			if err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			if signal == syscall.SIGINT {
				err = syscall.Kill(-cmd.Process.Pid, signal)
			} else {
				err = cmd.Process.Signal(signal)
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case err = <-done:
				waited = true
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 130 {
					t.Fatalf("cancellation exit: %v\n%s", err, output.String())
				}
			case <-time.After(12 * time.Second):
				t.Fatal("index did not finish bounded cleanup")
			}
			if !strings.Contains(output.String(), "operation canceled") {
				t.Fatalf("missing cancellation diagnostic: %s", output.String())
			}
			if err = syscall.Kill(child, 0); !errors.Is(err, syscall.ESRCH) {
				t.Fatalf("runtime %d still exists: %v", child, err)
			}
			child = 0 // Successfully reaped; do not signal a potentially reused PID.
			var snapshot string
			var active int
			if err = db.QueryRow("SELECT value FROM metadata WHERE key='snapshot'").Scan(&snapshot); err != nil {
				t.Fatal(err)
			}
			if err = db.QueryRow("SELECT count(*) FROM passages").Scan(&active); err != nil {
				t.Fatal(err)
			}
			if snapshot != original || active != 1 {
				t.Fatalf("cancellation replaced active generation: snapshot=%q passages=%d", snapshot, active)
			}
			t.Logf("%s: exit=130 shutdown=%s runtime_reaped=true active_passages=%d cached_vectors=%d", signal, time.Since(started), active, count())
		})
	}
	cached := count()
	out, err := command().CombinedOutput()
	if err != nil {
		t.Fatalf("resume: %v\n%s", err, out)
	}
	var report retrieval.IndexReport
	if err = json.Unmarshal(out, &report); err != nil {
		t.Fatalf("report: %v\n%s", err, out)
	}
	if report.Notes != 17 || report.Passages != 17 || report.Reused < cached || report.Embedded+report.Reused != 17 {
		t.Fatalf("resume did not reuse completed work: %+v, cached %d", report, cached)
	}
	t.Logf("resume: %+v", report)
}

func TestCanceledCommand(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, diagnostics bytes.Buffer
	err := runCommand(ctx, []string{"--version"}, strings.NewReader(""), &out, &diagnostics)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled command: %v", err)
	}
	if err = runCommand(context.Background(), []string{"--version"}, strings.NewReader(""), &out, &diagnostics); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "tmix "+version+" (commit ") {
		t.Fatalf("version output: %q", out.String())
	}
}
