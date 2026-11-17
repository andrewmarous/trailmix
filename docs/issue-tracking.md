# Engineering issue tracking

Trailmix contributors use Beads Rust (`br`) for issues, dependencies, and verification records.
BR is a contributor tool, not a dependency of the Trailmix product.
Trailmix remains the memory store.

## Install from the pinned fork

Source: [andrewmarous/beads_rust](https://github.com/andrewmarous/beads_rust).
Pinned commit: `ad4212f562ca59864946b8620c1361f0933ee4a8` (BR v0.7.6).
The fork contains the same commit used for the local workflow checks.
The pinned source requires Rust nightly `2026-08-31`.

Run these commands with Git and rustup available. Stop if any command fails.

```sh
set -eu
source_dir="$(mktemp -d "${TMPDIR:-/tmp}/trailmix-br-source.XXXXXX")"
git clone https://github.com/andrewmarous/beads_rust.git "$source_dir/beads_rust"
git -C "$source_dir/beads_rust" checkout --detach ad4212f562ca59864946b8620c1361f0933ee4a8
test "$(git -C "$source_dir/beads_rust" rev-parse HEAD)" = ad4212f562ca59864946b8620c1361f0933ee4a8

rustup toolchain install nightly-2026-08-31 --profile minimal --no-self-update
(
    cd "$source_dir/beads_rust" || exit 1
    cargo +nightly-2026-08-31 build --release --locked
)
"$source_dir/beads_rust/target/release/br" --version
```

Build as your normal user, not with sudo.

Install BR beside `tmix`. The default `sudo make install` destination is `/usr/local/bin`.
If your tmix installation uses a `PREFIX` or `BINDIR` override, use the same destination here.

```sh
BINDIR=/usr/local/bin
sudo install -m 755 "$source_dir/beads_rust/target/release/br" "$BINDIR/br"
"$BINDIR/br" --version
command -v br
```

The version must report `br 0.7.6`. Verify that `command -v br` selects the intended executable.
Keep the source checkout until installation succeeds.

## Repository state

The repository tracks these files:

- `.beads/issues.jsonl`: issue records shared through Git.
- `.beads/config.yaml`: project configuration.
- `.beads/metadata.json`: portable database and export names.
- `.beads/.gitignore`: exclusions for local database files and sync state.

SQLite stores the local working state in `.beads/beads.db`.
The generated ignore rules exclude the database, sidecars, locks, recovery files, and local merge state.
BR exports JSONL after successful mutations by default.
Normal issue commands import changed JSONL before they run.
BR does not commit, fetch, push, pull, install Git hooks, or schedule background jobs for this workflow.

### First initialization

Trailmix already contains an initialized workspace.
For a repository without `.beads`, initialize it once from the repository root:

```sh
br init --prefix tmix
```

Review the generated files before staging them.
Keep the contributor guidance in the handwritten section of `AGENTS.md`.
The operating-guide generator does not own that section.

### Fresh clones and daily use

From a fresh clone, run:

```sh
br ready --json
```

BR reconstructs the local database from the committed JSONL.
A fresh clone does not require `br init`.
After an authorized Git pull, normal BR commands import the updated JSONL automatically.

For approved work, inspect the issue and claim it:

```sh
br show <id> --json
br update <id> --claim
```

Claims protect the local database only.
Separate clones can both claim an issue before they exchange changes.
Coordinate assignments before parallel work across clones.
An issue assignment does not replace the existing approval or verification requirements.

Record verification and remaining work before closing an issue.
Keep rollout tasks open until their own acceptance criteria pass.
Before an authorized commit, run:

```sh
br sync --flush-only
git diff -- .beads/issues.jsonl
git status --short -- .beads
```

Stage the intended portable files through the normal Git workflow.
BR needs no separate remote, server, or sync job.

## Conflicts and recovery

If Git or BR reports a conflict, stop normal issue mutations.
Preserve the conflicting JSONL and local database state before recovery.
BR rejects unresolved Git conflict markers.
Resolve the JSONL deliberately, with evidence from both versions.

Divergent SQLite and JSONL changes can require `br sync --merge`.
That command refuses to guess when a merge ancestor is unavailable.
Obtain approval before choosing `--force-db`, `--force-jsonl`, or another destructive recovery policy.
After recovery, inspect the affected issues and the JSONL diff before committing.

## Upgrade the pin

Treat BR upgrades as contributor-tool changes, separate from Trailmix releases.
Choose a commit in the fork and build it with its pinned toolchain and lockfile.
Repeat the Git round trip in temporary repositories before changing this document.
Cover initialization, fresh-clone import, dependencies, claims, export, ignored files, and conflicting edits.
Record the tested commit and version in the same change as the updated setup instructions.
