package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const obsidianReleaseURL = "https://api.github.com/repos/obsidianmd/obsidian-releases/releases/latest"

func installObsidian(platform string, in io.Reader, diagnostics io.Writer) error {
	var cmd *exec.Cmd
	switch platform {
	case "darwin":
		brew, err := exec.LookPath("brew")
		if err != nil {
			return fmt.Errorf("Homebrew is required; install Obsidian manually from https://obsidian.md")
		}
		cmd = exec.Command(brew, "install", "--cask", "obsidian")
	case "linux":
		apt, err := exec.LookPath("apt")
		if err != nil {
			return fmt.Errorf("automatic Linux installation requires apt and dpkg (.deb support); install Obsidian manually")
		}
		dpkg, err := exec.LookPath("dpkg")
		if err != nil {
			return fmt.Errorf("automatic Linux installation requires dpkg (.deb support); install Obsidian manually")
		}
		var sudo string
		if os.Geteuid() != 0 {
			sudo, err = exec.LookPath("sudo")
			if err != nil {
				return fmt.Errorf("sudo is required to install Obsidian; run as root or install manually")
			}
		}
		arch, err := exec.Command(dpkg, "--print-architecture").Output()
		if err != nil {
			return fmt.Errorf("detect Debian architecture: %w", err)
		}
		dir, err := os.MkdirTemp("", "tmix-obsidian-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		// apt's sandbox user must be able to read the downloaded package.
		if err := os.Chmod(dir, 0755); err != nil {
			return err
		}
		path := filepath.Join(dir, "obsidian.deb")
		client := &http.Client{Timeout: 5 * time.Minute}
		if err := downloadObsidianDeb(client, obsidianReleaseURL, strings.TrimSpace(string(arch)), path); err != nil {
			return err
		}
		cmd = exec.Command(apt, "install", path)
		if sudo != "" {
			cmd = exec.Command(sudo, apt, "install", path)
		}
	default:
		return fmt.Errorf("automatic Obsidian installation supports macOS and Linux with apt and dpkg only; install manually")
	}
	cmd.Stdin = in
	cmd.Stdout = diagnostics
	cmd.Stderr = diagnostics
	return cmd.Run()
}

func downloadObsidianDeb(client *http.Client, releaseURL, arch, path string) error {
	if arch != "amd64" && arch != "arm64" {
		return fmt.Errorf("unsupported Debian architecture %q; install Obsidian manually", arch)
	}
	response, err := client.Get(releaseURL)
	if err != nil {
		return fmt.Errorf("find Obsidian release: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("find Obsidian release: HTTP %d", response.StatusCode)
	}
	var release struct {
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1024*1024)).Decode(&release); err != nil {
		return fmt.Errorf("read Obsidian release: %w", err)
	}
	var url string
	for _, asset := range release.Assets {
		if strings.HasSuffix(asset.Name, "_"+arch+".deb") {
			url = asset.URL
			break
		}
	}
	if url == "" {
		return fmt.Errorf("the latest Obsidian release has no .deb package for %s; install manually", arch)
	}
	response, err = client.Get(url)
	if err != nil {
		return fmt.Errorf("download Obsidian: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download Obsidian: HTTP %d", response.StatusCode)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, response.Body)
	closeErr := file.Close()
	if copyErr != nil {
		return fmt.Errorf("download Obsidian: %w", copyErr)
	}
	return closeErr
}
