package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/andrewmarous/trailmix/backup"
	"github.com/andrewmarous/trailmix/config"
	"github.com/andrewmarous/trailmix/guide"
	"github.com/andrewmarous/trailmix/learning"
	"github.com/andrewmarous/trailmix/vault"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

const usage = `Usage: tmix [--config PATH] [--vault PATH] COMMAND [options] [note/query]
Run tmix instructions for when to search, how to search, what to preserve,
and how to correct shared memory. Write only on explicit user request.
Commands:
  learning [--vault PATH] status | record SESSION | begin | complete RUN | abort RUN
                                Track evidence and learning review snapshots (JSON)
  learning sessions help        Discover Pi/Codex sessions and track reviewed turns
  learning run                  Run a bounded learning pass with Pi or Codex
  learning doctor [--probe]     Check readiness; --probe makes a real model request
  learning accept RUN           Publish a queued proposal and complete its review
  learning schedule enable|disable|status
                                Manage macOS launchd or Linux systemd learning jobs
  config init|path|show          Create or inspect shared TOML settings
  instructions                  Print the operating guide
  init    [--install-obsidian] [--learning-state-dir PATH]
                                Create the vault and initialize the learning cutoff
  search  [--limit N] [--json] QUERY
  links   [--json] NOTE          Resolve outgoing links and backlinks
  read    NOTE                   Print Markdown
  write   [--overwrite] NOTE      Create a note from stdin
  append  NOTE                   Append stdin exactly to an existing note
  remote  [-v | add NAME URL | remove NAME | rename OLD NEW | get-url NAME | set-url NAME URL]
  backup  [REMOTE]               Commit and push the vault (default: origin)
  sync    [--enable | --disable | --status] [REMOTE]
                                Sync now or manage automatic macOS/Linux sync
Flags must precede positional arguments. --vault also works after COMMAND.
`

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "tmix:", err)
		os.Exit(1)
	}
}

func run(args []string, in io.Reader, out, diagnostics io.Writer) error {
	if len(args) == 1 && (args[0] == "--version" || args[0] == "version") {
		_, err := fmt.Fprintf(out, "tmix %s (commit %s, built %s)\n", version, commit, date)
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	global := flag.NewFlagSet("tmix", flag.ContinueOnError)
	global.SetOutput(diagnostics)
	root := global.String("vault", filepath.Join(home, ".trailmix"), "vault directory")
	configPath := global.String("config", config.Path(home), "shared TOML config file")
	if err := global.Parse(args); err != nil {
		if err == flag.ErrHelp {
			fmt.Fprint(out, usage)
			return nil
		}
		return err
	}
	args = global.Args()
	if len(args) == 0 || args[0] == "help" {
		fmt.Fprint(out, usage)
		return nil
	}
	command := args[0]
	*configPath, err = filepath.Abs(config.Expand(*configPath, home))
	if err != nil {
		return err
	}
	if command == "config" {
		return runConfig(args[1:], *configPath, out)
	}
	cfg, err := config.Load(*configPath, home)
	if err != nil {
		return err
	}
	vaultOverride := false
	global.Visit(func(f *flag.Flag) {
		if f.Name == "vault" {
			vaultOverride = true
		}
	})
	if !vaultOverride {
		*root = cfg.Vault.Path
	}
	if command == "instructions" {
		if len(args) != 1 {
			return fmt.Errorf("instructions takes no arguments")
		}
		_, err := io.WriteString(out, guide.Text)
		return err
	}
	if command == "learning" {
		return runLearning(args[1:], *root, cfg, *configPath, in, out, diagnostics)
	}
	switch command {
	case "init", "search", "links", "read", "write", "append", "remote", "backup", "sync":
	default:
		return fmt.Errorf("unknown command %q", command)
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	flags.StringVar(root, "vault", *root, "vault directory")
	var overwrite, install, jsonOutput bool
	learningStateDir := cfg.Learning.StateDir
	var enableSync, disableSync, backgroundSync, syncStatus bool
	limit := 10
	switch command {
	case "sync":
		flags.BoolVar(&enableSync, "enable", false, "enable automatic sync every five minutes (macOS launchd or Linux systemd)")
		flags.BoolVar(&disableSync, "disable", false, "disable automatic sync")
		flags.BoolVar(&backgroundSync, "background", false, "run scheduled sync with logs")
		flags.BoolVar(&syncStatus, "status", false, "show last successful sync")
	case "init":
		flags.StringVar(&learningStateDir, "learning-state-dir", learningStateDir, "machine-local learning registry directory")
		flags.BoolVar(&install, "install-obsidian", false, "install Obsidian with Homebrew (macOS) or apt (Linux .deb)")
	case "write":
		flags.BoolVar(&overwrite, "overwrite", false, "replace an existing note")
	case "links":
		flags.BoolVar(&jsonOutput, "json", false, "emit JSON")
	case "search":
		flags.IntVar(&limit, "limit", 10, "maximum results (1–100)")
		flags.BoolVar(&jsonOutput, "json", false, "emit JSON")
	}
	parseArgs := args[1:]
	if command == "remote" {
		parseArgs = nil
	}
	if err := flags.Parse(parseArgs); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	positional := flags.Args()
	if command == "remote" {
		// Preserve git remote's -v flag instead of parsing it as a CLI flag.
		positional = args[1:]
	}
	if command == "remote" || command == "backup" || command == "sync" {
		if command != "remote" && len(positional) > 1 {
			return fmt.Errorf("backup accepts at most one remote name")
		}
	} else if command == "init" {
		if len(positional) != 0 {
			return fmt.Errorf("init takes no positional arguments")
		}
	} else if command == "search" {
		if len(positional) == 0 {
			return fmt.Errorf("search requires a query")
		}
	} else if len(positional) != 1 {
		return fmt.Errorf("%s requires exactly one note path", command)
	}
	if *root == "~" {
		*root = home
	} else if strings.HasPrefix(*root, "~/") {
		*root = filepath.Join(home, (*root)[2:])
	}
	v, err := vault.Open(*root)
	if err != nil {
		return err
	}
	if command == "write" || command == "append" || command == "backup" || command == "sync" {
		unlock, err := backup.Lock(v.Root)
		if err != nil {
			return err
		}
		defer unlock()
	}
	switch command {
	case "remote", "backup", "sync":
		configDir, err := os.UserConfigDir()
		if err != nil {
			return err
		}
		if cfg.Remotes == nil && *configPath != filepath.Clean(config.Path(home)) {
			cfg.Remotes = map[string]string{}
		}
		g := backup.Git{Config: filepath.Join(configDir, "trailmix", "remotes.gitconfig"), Remotes: cfg.Remotes, SaveRemotes: func(remotes map[string]string) error { return config.SaveRemotes(*configPath, remotes) }, AppConfig: *configPath, Out: out, Err: diagnostics}
		if command == "remote" {
			unlock, err := config.Lock(*configPath)
			if err != nil {
				return err
			}
			defer unlock()
			latest, err := config.Load(*configPath, home)
			if err != nil {
				return err
			}
			g.Remotes = latest.Remotes
			if g.Remotes == nil && *configPath != filepath.Clean(config.Path(home)) {
				g.Remotes = map[string]string{}
			}
			g.SaveRemotes = func(remotes map[string]string) error { return config.SaveRemotes(*configPath, remotes, true) }
			return g.Remote(positional)
		}
		remote := cfg.Sync.Remote
		if len(positional) == 1 {
			remote = positional[0]
		}
		if command == "sync" {
			count := 0
			for _, b := range []bool{enableSync, disableSync, backgroundSync, syncStatus} {
				if b {
					count++
				}
			}
			if count > 1 {
				return fmt.Errorf("choose only one sync option")
			}
			if enableSync || disableSync {
				if err := g.Schedule(v.Root, remote, enableSync); err != nil {
					return err
				}
				fmt.Fprintln(out, "Automatic sync enabled:", enableSync)
				return nil
			}
			if syncStatus {
				if _, err := os.Stat(filepath.Join(v.Root, ".tmx-sync-failing")); err == nil {
					fmt.Fprintln(out, "Automatic sync is failing. Run tmix sync for details.")
				}
				data, err := os.ReadFile(filepath.Join(v.Root, ".tmx-last-sync"))
				if os.IsNotExist(err) {
					fmt.Fprintln(out, "No successful sync yet")
					return nil
				}
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "Last successful sync: %s", data)
				return nil
			}
			if backgroundSync {
				return g.Background(v.Root, remote)
			}
			return g.Sync(v.Root, remote)
		}
		return g.Backup(v.Root, remote)
	case "init":
		if _, err := os.Stat(*configPath); os.IsNotExist(err) {
			if err = config.Create(*configPath); err != nil {
				return err
			}
		}
		if err := v.Init(); err != nil {
			return err
		}
		start, err := (learning.Store{Dir: learningStateDir}).Initialize()
		if err != nil {
			return fmt.Errorf("initialize learning state: %w (vault remains ready)", err)
		}
		fmt.Fprintln(out, "Vault ready:", v.Root)
		fmt.Fprintln(out, "Learning starts at:", start)
		if obsidianInstalled(home) {
			fmt.Fprintln(out, "Obsidian detected. Open this folder as a vault.")
			return nil
		}
		if !install {
			fmt.Fprintln(out, "Obsidian not detected. The CLI is ready; install Obsidian separately or use init --install-obsidian.")
			return nil
		}
		if err := installObsidian(runtime.GOOS, in, diagnostics); err != nil {
			return fmt.Errorf("install Obsidian: %w (vault remains ready)", err)
		}
		fmt.Fprintln(out, "Obsidian installed. Open this folder as a vault.")
	case "links":
		links, err := v.Navigate(positional[0])
		if err != nil {
			return err
		}
		if jsonOutput {
			return json.NewEncoder(out).Encode(links)
		}
		for _, group := range []struct {
			name string
			refs []vault.Reference
		}{{"outgoing", links.Outgoing}, {"backlink", links.Backlinks}} {
			for _, ref := range group.refs {
				target := ref.Path
				if ref.Fragment != "" {
					target += "#" + ref.Fragment
				}
				if _, err := fmt.Fprintf(out, "%s %s:%d: %s\n", group.name, ref.Source, ref.Line, target); err != nil {
					return err
				}
			}
		}
		return nil
	case "read":
		data, err := v.Read(positional[0])
		if err != nil {
			return err
		}
		_, err = out.Write(data)
		return err
	case "write", "append":
		data, err := io.ReadAll(io.LimitReader(in, 16*1024*1024+1))
		if err != nil {
			return err
		}
		if len(data) > 16*1024*1024 {
			return fmt.Errorf("stdin exceeds 16 MiB")
		}
		if command == "write" {
			err = v.Write(positional[0], data, overwrite)
		} else {
			err = v.Append(positional[0], data)
		}
		return err
	case "search":
		results, err := v.Search(strings.Join(positional, " "), limit)
		if err != nil {
			return err
		}
		if jsonOutput {
			return json.NewEncoder(out).Encode(results)
		}
		for _, r := range results {
			if _, err := fmt.Fprintf(out, "%s:%d: %s\n", r.Path, r.Line, r.Snippet); err != nil {
				return err
			}
		}
	}
	return nil
}

func obsidianInstalled(home string) bool {
	if _, err := exec.LookPath("obsidian"); err == nil {
		return true
	}
	if runtime.GOOS == "darwin" {
		for _, p := range []string{"/Applications/Obsidian.app", filepath.Join(home, "Applications", "Obsidian.app")} {
			if info, err := os.Stat(p); err == nil && info.IsDir() {
				return true
			}
		}
	}
	return false
}
