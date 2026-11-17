# Contributing

## Guide maintenance

Edit `guide/operating-guide.md` to change the operating instructions. Then run `make docs`.
This command updates `AGENTS.md` and `skills/trailmix/SKILL.md` from that source.
`make docs-check` rejects stale copies. `make test` includes this check.
The generator preserves handwritten contributor instructions outside the marked block in `AGENTS.md`.
Repository-local engineering skills live in `.agents/skills/`, separate from the distributed product skill.

`tmix instructions` prints the same guide from the executable without reading files from the vault.
Rebuild the executable after changes to the guide.
CLI help points agents to this command. Install `skills/trailmix/` in your agent's skill directory to enable skill discovery.
Trailmix does not install the skill automatically.

## Build

Install Go 1.24 or newer. Then build the executable:

```sh
make build
```

The executable is `build/tmix`. Use `build/tmix` to run the executable before installation.

To build and install the executable in `/usr/local/bin`, run:

```sh
make install
```

This directory is normally on `PATH` on macOS and Linux. After installation, use `tmix` instead of `build/tmix`.

If directory permissions prevent installation, run `sudo make install`. For a different destination, use `make install BINDIR=/path/to/bin`.

`PREFIX` defaults to `/usr/local`. `DESTDIR` supports staged installations for packaging.

## Library

The `trailmix/vault` package provides `Open`, `Init`, `Read`, `Write`, `Append`, and `Search`. The CLI handles arguments, stdin, and Obsidian installation.

Writes use a temporary file and atomic publication. New-note publication cannot replace an existing note.

Writes and appends use persistent per-note `.tmx-lock-*` files with `github.com/gofrs/flock` on local macOS and Linux filesystems.
The lock covers the entire append operation. Different notes can change concurrently.
The OS releases locks when the owning descriptors close, including after a process crash.
Keep lock files in place while processes can use the vault. Removing a lock file can break mutual exclusion.
Overwrites still accept stale content. Coordinate read-edit-overwrite sequences and Obsidian saves, which do not use these locks.
Search, links, and backups do not provide a consistent snapshot across notes. Network filesystems are not supported for locking.

Path checks prevent ordinary symlink escapes. They do not protect against an adversary who changes filesystem paths during an operation.

## Scheduler installation tests

These tests install real user schedulers. Run them only in disposable VMs or CI runners.
They check installation, an automatic sync to a local Git remote, and scheduler removal.
They do not need GitHub credentials or alter your host scheduler.

For local VM tests, install Go and Lima. On Apple Silicon, also install Tart for macOS tests.
The tests find `tart` on PATH, in `~/tart.app`, or in `/Applications/tart.app`.
Set `TART_BIN` to the full executable path for another installation location.
Then run:

```sh
make test-scheduler-vms                  # Linux and macOS, where supported
make test-scheduler-vms PLATFORM=linux   # Lima Ubuntu VM
make test-scheduler-vms PLATFORM=macos   # Tart macOS VM
```

The tests download VM images and delete their test VMs afterward. Image downloads can require many gigabytes.
The Linux VM matches the host CPU architecture. Tart tests require an Apple Silicon Mac.
On other hosts, the macOS test prints a skip message. An Intel Mac also skips because Tart requires Apple Silicon.
Missing VM tools cause an error on supported hosts.

The default macOS image is `ghcr.io/cirruslabs/macos-tahoe-base:latest`. Tart reuses cached images.
Set `TART_IMAGE` to another trusted image or an existing local VM, such as `tahoe-base`.
Tests create a disposable clone and leave the source VM unchanged.
The macOS guest must have Git, Tart Guest Agent, and a logged-in GUI user session.
The tests use the guest agent, so SSH credentials and sshpass are not necessary.
The Linux guest must have internet access for package installation.

`.github/workflows/scheduler.yml` runs native scheduler tests on Ubuntu and macOS, each on AMD64 and ARM64.
CI also runs the unit tests and installs the CLI through `make install`.

## Tests

```sh
go test -race ./...
go vet ./...
```

Tests use temporary vaults. They do not install Obsidian or change the default vault.
