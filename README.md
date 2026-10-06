# Trailmix

`tmx` is a small CLI for a shared Markdown knowledge base. Obsidian is optional. No database, daemon, or runtime dependency is necessary.

## Build

Install Go 1.24 or newer. Then build the executable:

```sh
go build -o tmx ./cmd/tmx
```

To install the executable in your Go binary directory, run:

```sh
go install ./cmd/tmx
```

## Start

Create the default vault:

```sh
./tmx init
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
./tmx init --install-obsidian
```

Initialization detects Obsidian in standard application locations or on `PATH`. Detection does not cover every custom installation location.

The vault remains usable if installation fails. To view the vault in Obsidian, open the vault directory as an existing vault.

## Commands

```sh
printf '# Verification\n\nTest the combined capability.\n' |
  ./tmx write preferences/verification.md

./tmx search verification
./tmx search --limit 5 --json VPN database
./tmx read preferences/verification.md

printf '\n## Example\n\nConnect to the database through the VPN.\n' |
  ./tmx append preferences/verification.md
```

`write` and `append` read UTF-8 content from stdin, with a 16 MiB limit. They preserve the input exactly.

`write` creates parent directories. Existing notes require `write --overwrite NOTE`. `append` requires an existing note and adds no separators.

Note paths are relative to the vault. The `.md` extension is optional. Hidden paths, traversal paths, and symlinks inside the vault are not supported.

Use an alternate vault for experiments:

```sh
./tmx --vault /tmp/trailmix-demo init
./tmx read --vault /tmp/trailmix-demo inbox/example.md
```

Flags must precede positional arguments. `--vault` works before or after the command. Errors go to stderr with exit status 1.

## Search

Search matches literal, case-insensitive terms across each note's path and content. Every whitespace-separated query term must match somewhere in the note.

Path and heading matches rank before body-only matches. Ties use alphabetical path order. Each result contains a path, line number, and snippet.

Search returns at most 10 notes by default. `--limit` accepts 1–100. Snippets contain at most 240 characters plus a truncation marker.

For path-only matches, the line number is 0. No matches produce empty text or an empty JSON array.

Search excludes hidden files, hidden directories, symlinks, and non-Markdown files. It scans the vault on each call without an index.

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
