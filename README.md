# Trailmix

`tmix` is a small CLI for a shared Markdown knowledge base. Obsidian is optional. No database, daemon, or runtime dependency is necessary.

## Build

Install Go 1.24 or newer. Then build the executable:

```sh
make build
```

The executable is `build/tmix`. For the `./tmix` examples below, run `ln -s build/tmix tmix` after the build.

To build and install the executable in `/usr/local/bin`, run:

```sh
make install
```

This directory is normally on `PATH` on macOS and Linux. After installation, use `tmix` instead of `./tmix` or `build/tmix`.

If directory permissions prevent installation, run `sudo make install`. For a different destination, use `make install BINDIR=/path/to/bin`.

`PREFIX` defaults to `/usr/local`. `DESTDIR` supports staged installations for packaging.

## Start

Create the default vault:

```sh
./tmix init
```

The vault path is `~/.trailmix`. Initialization creates these directories without replacing existing content:

```text
.obsidian/
concepts/
projects/
decisions/
experiences/
preferences/
procedures/
inbox/
```

These directories are conventions, not enforced note types. Initialization adds no notes, templates, or required metadata.

On macOS, optionally install Obsidian through Homebrew:

```sh
./tmix init --install-obsidian
```

Initialization detects Obsidian in standard application locations or on `PATH`. Detection does not cover every custom installation location.

The vault remains usable if installation fails. To view the vault in Obsidian, open the vault directory as an existing vault.

## Commands

```sh
printf '# Verification\n\nTest the combined capability.\n' |
  ./tmix write preferences/verification.md

./tmix search verification
./tmix search --limit 5 --json VPN database
./tmix read preferences/verification.md

printf '\n## Example\n\nConnect to the database through the VPN.\n' |
  ./tmix append preferences/verification.md
```

`write` and `append` read UTF-8 content from stdin, with a 16 MiB limit. They preserve the input exactly.

`write` creates parent directories. Existing notes require `write --overwrite NOTE`. `append` requires an existing note and adds no separators.

Note paths are relative to the vault. The `.md` extension is optional. Hidden paths, traversal paths, and symlinks inside the vault are not supported.

Use an alternate vault for experiments:

```sh
./tmix --vault /tmp/trailmix-demo init
./tmix read --vault /tmp/trailmix-demo inbox/example.md
```

Flags must precede positional arguments. `--vault` works before or after the command. Errors go to stderr with exit status 1.

## Search

Search matches literal, case-insensitive terms across each note's path and content. Every whitespace-separated query term must match somewhere in the note.

Path and heading matches rank before body-only matches. Ties use alphabetical path order. Each result contains a path, line number, and snippet.

Search returns at most 10 notes by default. `--limit` accepts 1–100. Snippets contain at most 240 characters plus a truncation marker.

For path-only matches, the line number is 0. No matches produce empty text or an empty JSON array.

Search excludes hidden files, hidden directories, symlinks, and non-Markdown files. It scans the vault on each call without an index.

## Git backups

CAUTION: Use a private repository. Backups include vault content and attachments, including any secrets in those files.

Configure a remote globally, independently of the selected vault:

```sh
build/tmix remote add origin git@github.com:YOUR-USER/YOUR-PRIVATE-REPO.git
build/tmix remote
build/tmix remote -v
build/tmix remote get-url origin
build/tmix remote set-url origin git@github.com:YOUR-USER/OTHER-REPO.git
build/tmix remote rename origin archive
build/tmix remote remove archive
```

Remote configuration uses `trailmix/remotes.gitconfig` under the operating system's user configuration directory. On macOS, this directory is `~/Library/Application Support`.

Configure `origin` again if you removed it. Then create and push a snapshot:

```sh
build/tmix backup
build/tmix backup archive
build/tmix --vault /path/to/vault backup origin
```

Git must be installed. Backups use your existing Git identity and SSH or HTTPS credentials. No credentials are stored by Trailmix.

Backup creates a Git repository inside the vault if necessary. It commits changes and deletions, then pushes the `main` branch.

New snapshots exclude `.obsidian/` and temporary `.tmx-*` files. Git ignore rules also apply. Previously committed files remain in repository history.

Backup requires the local `main` branch. It does not pull, merge, force-push, create a hosted repository, or configure authentication.

If a push fails, the local commit remains available for another attempt. If the remote contains conflicting history, Git rejects the push.

Use an empty remote repository for the first backup. Different vaults must use different remotes to avoid conflicting histories.

## Library

The `trailmix/vault` package provides `Open`, `Init`, `Read`, `Write`, `Append`, and `Search`. The CLI handles arguments, stdin, and Obsidian installation.

Writes use a temporary file and atomic publication. New-note publication cannot replace an existing note.

Concurrent edits to the same note can lose updates. Serialize edits until the tool has concurrency controls.

Path checks prevent ordinary symlink escapes. They do not protect against an adversary who changes filesystem paths during an operation.

## Tests

```sh
go test -race ./...
go vet ./...
```

Tests use temporary vaults. They do not install Obsidian or change the default vault.
