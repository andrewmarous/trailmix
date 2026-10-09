package vault

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofrs/flock"
)

// Run the real vault operations in separate processes; goroutines alone cannot
// establish cross-process exclusion or kernel cleanup after a crash.
func TestLockProcess(t *testing.T) {
	mode := os.Getenv("TMIX_LOCK_TEST_MODE")
	if mode == "" {
		return
	}
	v, err := Open(os.Getenv("TMIX_LOCK_TEST_ROOT"))
	if err != nil {
		t.Fatal(err)
	}
	switch mode {
	case "hold":
		p, err := v.note("note")
		if err != nil {
			t.Fatal(err)
		}
		lock, err := v.lockNote(p)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Unlock()
		// Publish a replacement while retaining the sidecar lock.
		if err := publish(p, []byte("replacement\n"), true); err != nil {
			t.Fatal(err)
		}
		fmt.Println("locked")
		// Wait until the parent kills us, without a sleep-based readiness signal.
		_, _ = bufio.NewReader(os.Stdin).ReadByte()
	case "append":
		for i := 0; i < 20; i++ {
			if err := v.Append("note.md", []byte(fmt.Sprintf("%s-%d\n", os.Getenv("TMIX_LOCK_TEST_ID"), i))); err != nil {
				t.Fatal(err)
			}
		}
	default:
		t.Fatal("unknown helper mode")
	}
}

func lockProcess(t *testing.T, root, mode, id string) *exec.Cmd {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLockProcess$")
	cmd.Env = append(os.Environ(), "TMIX_LOCK_TEST_MODE="+mode, "TMIX_LOCK_TEST_ROOT="+root, "TMIX_LOCK_TEST_ID="+id)
	cmd.Stderr = os.Stderr
	return cmd
}

func TestConcurrentProcessAppends(t *testing.T) {
	v, _ := Open(t.TempDir())
	if err := v.Write("note", nil, false); err != nil {
		t.Fatal(err)
	}
	var cmds []*exec.Cmd
	for i := 0; i < 6; i++ {
		cmd := lockProcess(t, v.Root, "append", fmt.Sprint(i))
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cmds = append(cmds, cmd)
	}
	for _, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	data, err := v.Read("note")
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		counts[line]++
	}
	if len(counts) != 120 {
		t.Fatalf("got %d distinct appends, want 120", len(counts))
	}
	for line, count := range counts {
		if count != 1 {
			t.Fatalf("%s occurred %d times", line, count)
		}
	}
}

func TestLockSurvivesReplacementAndReleasesOnCrash(t *testing.T) {
	v, _ := Open(t.TempDir())
	if err := v.Write("note", nil, false); err != nil {
		t.Fatal(err)
	}
	holder := lockProcess(t, v.Root, "hold", "")
	stdout, err := holder.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := holder.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	defer holder.Process.Kill()
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "locked\n" {
		t.Fatalf("holder readiness: %q, %v", line, err)
	}
	// Same normalized identity for extensionless and explicit note names.
	p, err := v.note("note.md")
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(filepath.Dir(p), ".tmx-lock-"+filepath.Base(p))
	before, err := os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	contender := flock.New(lockPath)
	ok, err := contender.TryLock()
	if err != nil || ok {
		t.Fatalf("replacement lost exclusion: acquired=%v, err=%v", ok, err)
	}
	if err := holder.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = holder.Wait() // A killed holder must not exit successfully.
	ok, err = contender.TryLock()
	if err != nil || !ok {
		t.Fatalf("crash did not release lock: acquired=%v, err=%v", ok, err)
	}
	if err := contender.Unlock(); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(lockPath)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("sidecar identity changed: %v", err)
	}
}

func TestRejectSymlinkLock(t *testing.T) {
	v, _ := Open(t.TempDir())
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(v.Root, ".tmx-lock-note.md")); err != nil {
		t.Fatal(err)
	}
	if err := v.Write("note", []byte("new"), false); err == nil {
		t.Fatal("accepted symlink lock")
	}
}
