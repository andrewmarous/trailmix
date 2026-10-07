package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownloadObsidianDeb(t *testing.T) {
	for _, tc := range []struct {
		name, arch                    string
		metadataStatus, packageStatus int
		missing                       bool
		wantError                     string
	}{
		{name: "amd64", arch: "amd64", metadataStatus: 200, packageStatus: 200},
		{name: "arm64", arch: "arm64", metadataStatus: 200, packageStatus: 200},
		{name: "unsupported", arch: "i386", wantError: "unsupported Debian architecture"},
		{name: "missing", arch: "amd64", metadataStatus: 200, missing: true, wantError: "no .deb package"},
		{name: "release failure", arch: "amd64", metadataStatus: 403, wantError: "HTTP 403"},
		{name: "download failure", arch: "amd64", metadataStatus: 200, packageStatus: 404, wantError: "HTTP 404"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/release" {
					w.WriteHeader(tc.metadataStatus)
					if tc.missing {
						fmt.Fprint(w, `{"assets":[]}`)
						return
					}
					fmt.Fprintf(w, `{"assets":[{"name":"Obsidian.AppImage","browser_download_url":"%s/wrong"},{"name":"obsidian_1.0_%s.deb","browser_download_url":"%s/package"}]}`, server.URL, tc.arch, server.URL)
					return
				}
				if r.URL.Path != "/package" {
					t.Errorf("unexpected download: %s", r.URL.Path)
				}
				w.WriteHeader(tc.packageStatus)
				fmt.Fprint(w, "package contents")
			}))
			defer server.Close()
			path := filepath.Join(t.TempDir(), "obsidian.deb")
			err := downloadObsidianDeb(server.Client(), server.URL+"/release", tc.arch, path)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("error = %v, want %q", err, tc.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != "package contents" {
				t.Fatalf("package = %q", data)
			}
		})
	}
}

func TestInstallObsidianUnsupportedPlatform(t *testing.T) {
	err := installObsidian("windows", strings.NewReader(""), &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "install manually") {
		t.Fatalf("error = %v", err)
	}
}
