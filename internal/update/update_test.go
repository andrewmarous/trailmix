package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gofrs/flock"
)

type entry struct {
	name, body string
	kind       byte
}

func archiveFor(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		kind := e.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		size := int64(len(e.body))
		if kind != tar.TypeReg {
			size = 0
		}
		if err := tw.WriteHeader(&tar.Header{Name: e.name, Mode: 0755, Size: size, Typeflag: kind}); err != nil {
			t.Fatal(err)
		}
		if size > 0 {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestUpdate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("release updater supports Unix executables")
	}
	// Exercise replacement with a real release executable, not just a shell fixture.
	releasePath := filepath.Join(t.TempDir(), "release-tmix")
	build := exec.Command("go", "build", "-ldflags", "-X main.version=v0.2.0 -X main.commit=test -X main.date=today", "-o", releasePath, "../../cmd/tmix")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build release: %v\n%s", err, output)
	}
	releaseBinary, err := os.ReadFile(releasePath)
	if err != nil {
		t.Fatal(err)
	}
	good := archiveFor(t, entry{name: "tmix", body: string(releaseBinary)})
	cases := []struct {
		name, current string
		check         bool
		archive       []byte
		sums          string
		status        int
		want          string
		updated       bool
	}{
		{name: "install", current: "v0.1.0", archive: good, want: "Updated tmix v0.1.0 → v0.2.0.", updated: true},
		{name: "check", current: "v0.1.0", check: true, archive: good, want: "Installed: v0.1.0; latest stable: v0.2.0."},
		{name: "current", current: "v0.2.0", archive: good, want: "is up to date"},
		{name: "newer local", current: "v0.10.0", archive: good, want: "is up to date"},
		{name: "development", current: "dev", archive: good, want: "cannot self-update development/source build"},
		{name: "development check", current: "dev", check: true, archive: good, want: "Installed: dev; latest stable: v0.2.0."},
		{name: "corrupt", current: "v0.1.0", archive: good, sums: strings.Repeat("0", 64), want: "checksum mismatch"},
		{name: "bad version", current: "v0.1.0", archive: archiveFor(t, entry{name: "tmix", body: "#!/bin/sh\necho 'tmix v9.0.0 (commit x)'\n"}), want: "unexpected version"},
		{name: "bad executable", current: "v0.1.0", archive: archiveFor(t, entry{name: "tmix", body: "not an executable"}), want: "verify downloaded executable"},
		{name: "http error", current: "v0.1.0", status: 403, want: "HTTP 403"},
		{name: "unsafe archive", current: "v0.1.0", archive: archiveFor(t, entry{name: "../tmix", body: "bad"}), want: "unsafe archive path"},
		{name: "duplicate", current: "v0.1.0", archive: archiveFor(t, entry{name: "tmix", body: "one"}, entry{name: "tmix", body: "two"}), want: "duplicate tmix"},
		{name: "link", current: "v0.1.0", archive: archiveFor(t, entry{name: "tmix", kind: tar.TypeSymlink}), want: "unsupported archive entry"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "tmix")
			if err := os.WriteFile(target, []byte("original"), 0755); err != nil {
				t.Fatal(err)
			}
			name := "trailmix_0.2.0_linux_amd64.tar.gz"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.status != 0 {
					w.WriteHeader(tc.status)
					return
				}
				switch r.URL.Path {
				case "/releases/latest":
					fmt.Fprint(w, `{"tag_name":"v0.2.0"}`)
				case "/v0.2.0/checksums.txt":
					sum := tc.sums
					if sum == "" {
						sum = fmt.Sprintf("%x", sha256.Sum256(tc.archive))
					}
					fmt.Fprintf(w, "%s  ./%s\n", sum, name)
				case "/v0.2.0/" + name:
					w.Write(tc.archive)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			u := updater{client: server.Client(), api: server.URL, downloads: server.URL, executable: target, goos: "linux", arch: "amd64"}
			var out bytes.Buffer
			err := u.run(context.Background(), tc.current, tc.check, &out)
			result := out.String()
			if err != nil {
				result = err.Error()
			}
			if !strings.Contains(result, tc.want) {
				t.Fatalf("got %q, want %q", result, tc.want)
			}
			contents, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			expected := "original"
			if tc.updated {
				expected = string(releaseBinary)
			}
			if string(contents) != expected {
				t.Fatalf("installed executable differs from expected contents")
			}
			if tc.updated {
				output, err := exec.Command(target, "--version").CombinedOutput()
				if err != nil || string(output) != "tmix v0.2.0 (commit test, built today)\n" {
					t.Fatalf("installed version: %q, %v", output, err)
				}
			}
			staged, _ := filepath.Glob(filepath.Join(dir, ".tmix-update-*"))
			if len(staged) != 0 {
				t.Fatalf("left staged files: %v", staged)
			}
		})
	}
}

func TestManagedAndLockedInstallations(t *testing.T) {
	for _, kind := range []string{"homebrew", "nix", "locked", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "tmix")
			if kind == "homebrew" {
				target = filepath.Join(dir, "Cellar", "trailmix", "0.1.0", "bin", "tmix")
				if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "nix" {
				if got := managerInstruction("/nix/store/hash-trailmix/bin/tmix"); got != "update Trailmix through Nix" {
					t.Fatal(got)
				}
				return
			}
			if err := os.WriteFile(target, []byte("original"), 0755); err != nil {
				t.Fatal(err)
			}
			executable := target
			if kind == "symlink" {
				executable = filepath.Join(dir, "link")
				if err := os.Symlink(target, executable); err != nil {
					t.Fatal(err)
				}
			}
			lock := flock.New(filepath.Join(filepath.Dir(target), ".tmix-update.lock"))
			if kind == "locked" || kind == "symlink" {
				if err := lock.Lock(); err != nil {
					t.Fatal(err)
				}
				defer lock.Unlock()
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"tag_name":"v0.2.0"}`) }))
			defer server.Close()
			u := updater{client: server.Client(), api: server.URL, executable: executable, goos: "linux", arch: "amd64"}
			err := u.run(context.Background(), "v0.1.0", false, &bytes.Buffer{})
			want := "another tmix update is running"
			if kind == "homebrew" {
				want = "brew update && brew upgrade trailmix"
			}
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("got %v, want %s", err, want)
			}
			contents, _ := os.ReadFile(target)
			if string(contents) != "original" {
				t.Fatalf("changed executable: %q", contents)
			}
		})
	}
}

func TestDownloadLimitAndCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "123456789") }))
	defer server.Close()
	u := updater{client: server.Client()}
	if _, err := u.fetch(context.Background(), server.URL, 4); err == nil || !strings.Contains(err.Error(), "exceeds 4 bytes") {
		t.Fatalf("limit: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := u.fetch(ctx, server.URL, 20); err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("cancel: %v", err)
	}
	data, err := u.fetch(context.Background(), server.URL, 9)
	if err != nil || string(data) != "123456789" {
		t.Fatalf("download: %q, %v", data, err)
	}
}

func TestChecksumsAndVersions(t *testing.T) {
	sum := strings.Repeat("ab", 32)
	for _, file := range []string{"tmix.tar.gz", "./tmix.tar.gz", "*tmix.tar.gz"} {
		got, err := checksum([]byte(sum+"  "+file+"\n"), "tmix.tar.gz")
		if err != nil || fmt.Sprintf("%x", got) != sum {
			t.Fatalf("checksum: %x, %v", got, err)
		}
	}
	for _, data := range []string{"", "bad tmix.tar.gz", sum + " tmix.tar.gz\n" + sum + " tmix.tar.gz"} {
		if _, err := checksum([]byte(data), "tmix.tar.gz"); err == nil {
			t.Fatalf("accepted %q", data)
		}
	}
	for _, tc := range []struct {
		a, b string
		want bool
	}{{"v0.9.0", "v0.10.0", true}, {"v1.0.0", "v0.99.0", false}, {"v0.1.9", "v0.2.0", true}, {"v1.0.0", "v1.0.0", false}} {
		got, err := isNewer(tc.a, tc.b)
		if err != nil || got != tc.want {
			t.Fatalf("compare %s %s: %v %v", tc.a, tc.b, got, err)
		}
	}
}
