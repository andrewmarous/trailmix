package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestConcurrentReplacementIsNotOverwritten(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix executables")
	}
	target := filepath.Join(t.TempDir(), "tmix")
	if err := os.WriteFile(target, []byte("original"), 0755); err != nil {
		t.Fatal(err)
	}
	archive := archiveFor(t, entry{name: "tmix", body: "#!/bin/sh\necho 'tmix v0.2.0 (commit test)'\n"})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/latest":
			// Simulate a newer updater winning while this process discovers its release.
			replacement := target + ".new"
			if err := os.WriteFile(replacement, []byte("newer installed release"), 0755); err != nil {
				t.Error(err)
				return
			}
			if err := os.Rename(replacement, target); err != nil {
				t.Error(err)
				return
			}
			fmt.Fprint(w, `{"tag_name":"v0.2.0"}`)
		case "/v0.2.0/checksums.txt":
			fmt.Fprintf(w, "%x  ./trailmix_0.2.0_linux_amd64.tar.gz\n", sha256.Sum256(archive))
		default:
			w.Write(archive)
		}
	}))
	defer server.Close()
	u := updater{client: server.Client(), api: server.URL, downloads: server.URL, executable: target, goos: "linux", arch: "amd64"}
	err := u.run(context.Background(), "v0.1.0", false, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "changed during update") {
		t.Fatalf("got %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "newer installed release" {
		t.Fatalf("installed: %q, %v", got, err)
	}
}

func TestArchiveValidation(t *testing.T) {
	good := archiveFor(t, entry{name: "tmix", body: "executable"}, entry{name: "README.md", body: "documentation"})
	got, err := extract(good)
	if err != nil || string(got) != "executable" {
		t.Fatalf("extract: %q %v", got, err)
	}
	corrupt := append([]byte(nil), good...)
	corrupt[len(corrupt)-8] ^= 1
	for _, data := range [][]byte{corrupt, good[:len(good)/2], archiveFor(t, entry{name: "README.md", body: "no executable"}), archiveFor(t, entry{name: "/tmix", body: "absolute"})} {
		if _, err := extract(data); err == nil {
			t.Fatal("accepted invalid archive")
		}
	}
}
