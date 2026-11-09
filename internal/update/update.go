// Package update installs standalone Trailmix releases without changing user data.
package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

const repository = "https://api.github.com/repos/andrewmarous/trailmix"
const downloads = "https://github.com/andrewmarous/trailmix/releases/download"
const maxArchive = 64 << 20
const maxBinary = 128 << 20

var stableVersion = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// Run checks or updates the running executable. Package-manager installations
// remain owned by their package manager.
func Run(ctx context.Context, current string, check bool, out io.Writer) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return fmt.Errorf("refusing non-HTTPS redirect")
		}
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	}}
	u := updater{client: client, api: repository, downloads: downloads,
		executable: executable, goos: runtime.GOOS, arch: runtime.GOARCH}
	return u.run(ctx, current, check, out)
}

type updater struct {
	client                                 *http.Client
	api, downloads, executable, goos, arch string
}

func (u updater) run(ctx context.Context, current string, check bool, out io.Writer) error {
	if (u.goos != "darwin" && u.goos != "linux") || (u.arch != "amd64" && u.arch != "arm64") {
		return fmt.Errorf("updates are not supported on %s/%s", u.goos, u.arch)
	}
	target, err := filepath.EvalSymlinks(u.executable)
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	before, err := os.Lstat(target)
	if err != nil {
		return err
	}
	if !before.Mode().IsRegular() {
		return fmt.Errorf("executable is not a regular file")
	}
	managed := managerInstruction(target)
	if !check && managed != "" {
		return fmt.Errorf("this installation is package-managed; %s", managed)
	}
	if !check && !stableVersion.MatchString(current) {
		return fmt.Errorf("cannot self-update development/source build %q; rebuild from source or install an official release", current)
	}
	data, err := u.fetch(ctx, u.api+"/releases/latest", 1<<20)
	if err != nil {
		return err
	}
	var release struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.Unmarshal(data, &release); err != nil {
		return fmt.Errorf("decode release: %w", err)
	}
	if release.Draft || release.Prerelease || !stableVersion.MatchString(release.Tag) {
		return fmt.Errorf("invalid stable release %q", release.Tag)
	}
	newer, err := isNewer(current, release.Tag)
	if err != nil && stableVersion.MatchString(current) {
		return err
	}
	if err == nil && !newer {
		_, err = fmt.Fprintf(out, "tmix %s is up to date (latest: %s).\n", current, release.Tag)
		return err
	}
	if check {
		_, err = fmt.Fprintf(out, "Installed: %s; latest stable: %s.\n", current, release.Tag)
		if err == nil && managed != "" {
			_, err = fmt.Fprintln(out, managed)
		}
		return err
	}
	lock := flock.New(filepath.Join(filepath.Dir(target), ".tmix-update.lock"))
	locked, err := lock.TryLock()
	if err != nil {
		return fmt.Errorf("lock update in %s (check directory permissions): %w", filepath.Dir(target), err)
	}
	if !locked {
		return fmt.Errorf("another tmix update is running")
	}
	defer lock.Unlock()
	name := fmt.Sprintf("trailmix_%s_%s_%s.tar.gz", strings.TrimPrefix(release.Tag, "v"), u.goos, u.arch)
	base := u.downloads + "/" + release.Tag + "/"
	checksums, err := u.fetch(ctx, base+"checksums.txt", 1<<20)
	if err != nil {
		return err
	}
	expected, err := checksum(checksums, name)
	if err != nil {
		return err
	}
	archive, err := u.fetch(ctx, base+name, maxArchive)
	if err != nil {
		return err
	}
	actual := sha256.Sum256(archive)
	if !bytes.Equal(expected, actual[:]) {
		return fmt.Errorf("checksum mismatch for %s", name)
	}
	binary, err := extract(archive)
	if err != nil {
		return err
	}
	staged, err := os.CreateTemp(filepath.Dir(target), ".tmix-update-*")
	if err != nil {
		return fmt.Errorf("stage update (check directory permissions): %w", err)
	}
	defer os.Remove(staged.Name())
	defer staged.Close()
	if _, err = staged.Write(binary); err != nil {
		return err
	}
	if err = staged.Chmod(0755); err != nil {
		return err
	}
	if err = staged.Sync(); err != nil {
		return err
	}
	if err = staged.Close(); err != nil {
		return err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// Bound child output as well as its runtime; a broken release must not exhaust memory.
	var probe limitedOutput
	cmd := exec.CommandContext(probeCtx, staged.Name(), "--version")
	cmd.Stdout, cmd.Stderr = &probe, &probe
	cmd.WaitDelay = time.Second
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("verify downloaded executable: %w (%s)", err, probe.String())
	}
	if !strings.HasPrefix(probe.String(), "tmix "+release.Tag+" (commit ") {
		return fmt.Errorf("downloaded executable reports unexpected version: %q", probe.String())
	}
	now, err := os.Lstat(target)
	if err != nil {
		return err
	}
	if !os.SameFile(before, now) || before.Size() != now.Size() || !before.ModTime().Equal(now.ModTime()) {
		return fmt.Errorf("installed executable changed during update; retry")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err = os.Rename(staged.Name(), target); err != nil {
		return fmt.Errorf("replace executable: %w", err)
	}
	_, err = fmt.Fprintf(out, "Updated tmix %s → %s. Skills and user data were not changed.\n", current, release.Tag)
	return err
}

func managerInstruction(path string) string {
	path = filepath.ToSlash(path)
	if strings.Contains(path, "/Cellar/") {
		return "run brew update && brew upgrade trailmix"
	}
	if strings.HasPrefix(path, "/nix/store/") {
		return "update Trailmix through Nix"
	}
	return ""
}

func isNewer(current, latest string) (bool, error) {
	a, b := stableVersion.FindStringSubmatch(current), stableVersion.FindStringSubmatch(latest)
	if a == nil || b == nil {
		return false, fmt.Errorf("cannot compare versions %q and %q", current, latest)
	}
	for i := 1; i <= 3; i++ {
		x, err := strconv.ParseUint(a[i], 10, 64)
		if err != nil {
			return false, err
		}
		y, err := strconv.ParseUint(b[i], 10, 64)
		if err != nil {
			return false, err
		}
		if x != y {
			return y > x, nil
		}
	}
	return false, nil
}

func (u updater) fetch(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "trailmix-updater")
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var details struct {
			Message string `json:"message"`
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = json.Unmarshal(body, &details)
		return nil, fmt.Errorf("download %s: HTTP %s (message %q, request ID %q)", url, resp.Status, details.Message, resp.Header.Get("X-GitHub-Request-Id"))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("download %s exceeds %d bytes", url, limit)
	}
	return data, nil
}

func checksum(data []byte, name string) ([]byte, error) {
	var sum []byte
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != "./"+name && strings.TrimPrefix(fields[1], "*") != name {
			continue
		}
		if sum != nil {
			return nil, fmt.Errorf("duplicate checksum for %s", name)
		}
		decoded, err := hex.DecodeString(fields[0])
		if err != nil || len(decoded) != sha256.Size {
			return nil, fmt.Errorf("invalid checksum for %s", name)
		}
		sum = decoded
	}
	if sum == nil {
		return nil, fmt.Errorf("missing checksum for %s", name)
	}
	return sum, nil
}

func extract(data []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open release archive: %w", err)
	}
	defer gz.Close()
	// Bound total decompressed input, including ignored documentation entries.
	expanded := &io.LimitedReader{R: gz, N: (256 << 20) + 1}
	tr := tar.NewReader(expanded)
	var binary []byte
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read release archive: %w", err)
		}
		if strings.HasPrefix(h.Name, "/") || strings.Contains(h.Name, "\\") {
			return nil, fmt.Errorf("unsafe archive path %q", h.Name)
		}
		for _, part := range strings.Split(h.Name, "/") {
			if part == ".." {
				return nil, fmt.Errorf("unsafe archive path %q", h.Name)
			}
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeDir {
			return nil, fmt.Errorf("unsupported archive entry %q", h.Name)
		}
		if h.Name != "tmix" {
			continue
		}
		if binary != nil || h.Typeflag != tar.TypeReg || h.Size <= 0 || h.Size > maxBinary {
			return nil, fmt.Errorf("invalid or duplicate tmix archive entry")
		}
		binary, err = io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
	}
	if _, err := io.Copy(io.Discard, expanded); err != nil {
		return nil, fmt.Errorf("read compressed archive: %w", err)
	}
	if expanded.N == 0 {
		return nil, fmt.Errorf("expanded release archive exceeds size limit")
	}
	if binary == nil {
		return nil, fmt.Errorf("release archive has no tmix executable")
	}
	return binary, nil
}

type limitedOutput struct{ bytes.Buffer }

func (b *limitedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len() < 4096 {
		keep := 4096 - b.Len()
		if keep > n {
			keep = n
		}
		_, _ = b.Buffer.Write(p[:keep])
	}
	return n, nil
}
