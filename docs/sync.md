# Git backup and sync

## Git backups

CAUTION: Use a private repository. Backups include vault content and attachments, including any secrets in those files.

Configure a remote globally, independently of the selected vault:

```sh
tmix remote add origin git@github.com:YOUR-USER/YOUR-PRIVATE-REPO.git
tmix remote
tmix remote -v
tmix remote get-url origin
tmix remote set-url origin git@github.com:YOUR-USER/OTHER-REPO.git
tmix remote rename origin archive
tmix remote remove archive
```

Named remotes live under `[remotes]` in the shared TOML configuration.
Legacy `trailmix/remotes.gitconfig` remains readable until the next remote mutation migrates its entries into TOML.
Remote mutations canonicalize formatting and remove TOML comments while retaining other settings.

Configure `origin` again if you removed it. Then create and push a snapshot:

```sh
tmix backup
tmix backup archive
tmix --vault /path/to/vault backup origin
```

Git must be installed. Backups use your existing Git identity and SSH or HTTPS credentials. No credentials are stored by Trailmix.

Backup creates a Git repository inside the vault if necessary. It commits changes and deletions, then pushes the `main` branch.

New snapshots exclude `.obsidian/`, temporary `.tmx-*` files, and persistent `.tmx-lock-*` files. Git ignore rules also apply. Previously committed files remain in repository history.

Backup requires the local `main` branch. It does not pull, merge, force-push, create a hosted repository, or configure authentication.

If a push fails, the local commit remains available for another attempt. If the remote contains conflicting history, Git rejects the push.

Use an empty remote repository for the first backup. Different vaults must use different remotes to avoid conflicting histories.

## Automatic Git sync

Use a private repository. Sync uploads notes and attachments, including secrets that these files contain.

Configure the remote, then enable automatic sync:

```sh
tmix remote add origin git@github.com:YOUR-USER/YOUR-PRIVATE-REPO.git
tmix sync --enable
```

On macOS, the launchd agent runs at login and every five minutes.
On Linux, a systemd user timer runs after installation and every five minutes after each sync completes.
Linux requires an active systemd user session. For sync after logout, enable user lingering with `loginctl enable-linger`.
The executable must remain at its installed path.
Only one automatic sync scheduler is supported per user. Enabling another vault replaces the previous scheduler.
Git must have a configured identity and unattended access to the remote.

Local reads and writes do not require network access. Each sync commits local changes, fetches, merges, and pushes.
A failed push leaves local commits available for the next run. Sync never force-pushes.
The exclusions from Git backups also apply to sync.

On a new machine, configure the same remote and run `tmix sync`. An empty vault adopts the remote history.
Existing unrelated histories do not merge automatically. Untracked file collisions stop sync without overwriting those files.

Use these commands when needed:

```sh
tmix sync                   # Sync now
tmix sync --status          # Show the last successful sync and background failure state
tmix sync --disable         # Remove the scheduler
tmix sync archive           # Sync with a different configured remote
```

On macOS, background logs are in `~/Library/Logs/Trailmix/sync.log`.
On Linux, logs are in `$XDG_STATE_HOME/trailmix/sync.log`, or `~/.local/state/trailmix/sync.log` by default.
Logs rotate at approximately 5 MiB.
After 24 hours of continuous failures, the worker sends a macOS notification, at most once per day.
Notifications require the worker to run. They cannot detect an unloaded or broken launchd agent.

Merges run in a temporary Git worktree. A conflict stops sync without adding conflict markers to the live vault.
Run `tmix sync` to see the error. Resolve conflicting history with Git before retrying sync.
The original local and remote commits remain available. The temporary worktree is removed after each attempt.

CLI writes, backups, and sync operations share a vault lock. Obsidian and other external editors do not use this lock.
Avoid external edits during sync. The worker checks tracked changes before publication, but this check cannot eliminate editor races.

On other operating systems, schedule `tmix sync` with the system scheduler.
The existing `tmix backup` command remains push-only.

