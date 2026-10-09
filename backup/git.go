// Package backup stores global remote configuration and publishes vault snapshots.
package backup

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Git struct {
	Config   string
	Out, Err io.Writer
}

func (g Git) command(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// Do not allow inherited Git overrides to redirect vault operations.
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GIT_") {
			cmd.Env = append(cmd.Env, e)
		}
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(stderr.String()))
	}
	return data, nil
}

var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

func validName(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("remote name must contain letters, digits, hyphens, or underscores")
	}
	return nil
}
func validURL(url string) error {
	if strings.TrimSpace(url) == "" || strings.HasPrefix(url, "-") || strings.ContainsAny(url, "\r\n") {
		return fmt.Errorf("invalid remote URL")
	}
	return nil
}
func (g Git) load() (map[string]string, error) {
	result := map[string]string{}
	data, err := os.ReadFile(g.Config)
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	// Git itself parses the configuration; empty files are valid.
	if len(data) == 0 {
		return result, nil
	}
	out, err := g.command("", "config", "--file", g.Config, "--list", "-z")
	if err != nil {
		return nil, err
	}
	for _, entry := range strings.Split(string(out), "\x00") {
		key, value, ok := strings.Cut(entry, "\n")
		if ok && strings.HasPrefix(key, "remote.") && strings.HasSuffix(key, ".url") {
			result[strings.TrimSuffix(strings.TrimPrefix(key, "remote."), ".url")] = value
		}
	}
	return result, nil
}

// Remote implements a small subset of git remote over global configuration.
func (g Git) Remote(args []string) error {
	remotes, err := g.load()
	if err != nil {
		return err
	}
	if len(args) == 0 || (len(args) == 1 && (args[0] == "-v" || args[0] == "--verbose")) {
		// Git prints remote names in alphabetical order.
		names := make([]string, 0, len(remotes))
		for name := range remotes {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if len(args) == 0 {
				fmt.Fprintln(g.Out, name)
			} else {
				fmt.Fprintf(g.Out, "%s\t%s (fetch)\n%s\t%s (push)\n", name, remotes[name], name, remotes[name])
			}
		}
		return nil
	}
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		_, err := fmt.Fprintln(g.Out, "Usage: tmix remote [-v | add NAME URL | remove NAME | rename OLD NEW | get-url NAME | set-url NAME URL]")
		return err
	}
	op := args[0]
	count := 3
	switch op {
	case "remove", "rm", "get-url":
		count = 2
	case "add", "rename", "set-url":
	default:
		return fmt.Errorf("unsupported remote operation %q", op)
	}
	if len(args) != count {
		return fmt.Errorf("usage: tmix remote %s NAME%s", op, map[bool]string{true: " VALUE", false: ""}[count == 3])
	}
	name := args[1]
	if err := validName(name); err != nil {
		return err
	}
	_, exists := remotes[name]
	if op == "add" && exists {
		return fmt.Errorf("remote %q already exists", name)
	}
	if op != "add" && !exists {
		return fmt.Errorf("remote %q does not exist", name)
	}
	if op == "get-url" {
		_, err := fmt.Fprintln(g.Out, remotes[name])
		return err
	}
	if err := os.MkdirAll(filepath.Dir(g.Config), 0700); err != nil {
		return err
	}
	if _, err := os.Stat(g.Config); os.IsNotExist(err) {
		if err := os.WriteFile(g.Config, nil, 0600); err != nil {
			return err
		}
	}
	switch op {
	case "add", "set-url":
		if err := validURL(args[2]); err != nil {
			return err
		}
		_, err = g.command("", "config", "--file", g.Config, "--replace-all", "remote."+name+".url", args[2])
	case "remove", "rm":
		_, err = g.command("", "config", "--file", g.Config, "--remove-section", "remote."+name)
	case "rename":
		if err := validName(args[2]); err != nil {
			return err
		}
		if _, exists := remotes[args[2]]; exists {
			return fmt.Errorf("remote %q already exists", args[2])
		}
		_, err = g.command("", "config", "--file", g.Config, "--rename-section", "remote."+name, "remote."+args[2])
	}
	return err
}

func (g Git) Backup(root, remote string) error {
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
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("vault must be a directory, not a symlink")
	}
	gitDir := filepath.Join(root, ".git")
	if info, err := os.Lstat(gitDir); os.IsNotExist(err) {
		if _, err := g.command(root, "init", "--initial-branch=main"); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("vault .git must be a directory, not a worktree pointer or symlink")
	}
	// Keep each snapshot on one explicit branch, and avoid detached HEAD.
	branch, err := g.command(root, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(branch)) != "main" {
		return fmt.Errorf("backup requires the vault's main branch")
	}
	// Excluded files already in the index must not enter this snapshot.
	staged, err := g.command(root, "diff", "--cached", "--name-only", "--", ".obsidian", ":(glob)**/.tmx-*")
	if err != nil {
		return err
	}
	if len(staged) > 0 {
		return fmt.Errorf("excluded files have staged changes; commit or unstage them before backup")
	}
	// Snapshot the whole vault except local UI state, temporary writes, and locks.
	if _, err := g.command(root, "add", "-A", "--", ".", ":(exclude).obsidian", ":(exclude,glob)**/.tmx-*"); err != nil {
		return err
	}
	changed, err := g.command(root, "diff", "--cached", "--name-only")
	if err != nil {
		return err
	}
	if len(changed) > 0 {
		if _, err := g.command(root, "commit", "-m", "Back up Trailmix vault"); err != nil {
			return err
		}
	}
	if _, err := g.command(root, "rev-parse", "--verify", "HEAD"); err != nil {
		return fmt.Errorf("vault has no committed content to back up")
	}
	fmt.Fprintln(g.Err, "Pushing vault backup...")
	if _, err := g.command(root, "push", url, "HEAD:refs/heads/main"); err != nil {
		return fmt.Errorf("%w; local snapshot remains committed; no force-push attempted", err)
	}
	_, err = fmt.Fprintln(g.Out, "Vault backed up to", remote)
	return err
}
