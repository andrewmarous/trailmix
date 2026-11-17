# Installation and updates

## Install

Supported platforms: macOS and Linux, on Intel/AMD64 and ARM64. Windows is not supported in v0.1.0.

With Homebrew:

```sh
brew install andrewmarous/tap/trailmix
tmix --version
```

Without Homebrew, download your platform's archive from [GitHub Releases](https://github.com/andrewmarous/trailmix/releases/latest).
Download `checksums.txt` into the same directory. Verify the archive with `sha256sum --check --ignore-missing checksums.txt` on Linux.
On macOS, use `shasum -a 256 -c checksums.txt` and check that your archive reports `OK`. Missing archives are expected.
Extract the archive, then install `tmix` in a directory on your `PATH`, such as `~/.local/bin`.
Archives also contain the license, README, documentation, and agent skill.

For a source installation with Go 1.24 or newer:

```sh
go install github.com/andrewmarous/trailmix/cmd/tmix@v0.1.0
```

Go installs the executable in `GOBIN`, or `~/go/bin` by default. This directory must be on `PATH`.
Source installations report `dev` unless the build sets release metadata.

## Upgrade and uninstall

For Homebrew, run `brew update` and then `brew upgrade trailmix`.
For a standalone release installation, run `tmix update` to install the latest stable release.
Run `tmix update --check` to check for a release without changes.
Older versions without this command require one manual executable replacement.
Development builds require a rebuild or an official release installation.

The updater supports macOS and Linux on AMD64 and ARM64. It leaves Homebrew and Nix installations under package-manager control.
It downloads official GitHub assets with timeouts and size limits, verifies SHA-256 checksums, and checks the staged executable's version.
It then replaces the executable atomically at its resolved path. A lock prevents concurrent replacements.
The destination directory must be writable. The updater does not invoke `sudo`.

Checksums detect corruption but do not authenticate a release independently of GitHub and HTTPS.
Updates preserve vaults, configuration, learning progress, and scheduler settings. Existing jobs finish with the old executable.
Agent skills remain unchanged. Refresh copied skills separately with the installation instructions.
After an upgrade, run `tmix --version` and `tmix sync --status` if you use automatic sync.
Before using session learning, rerun `tmix init` to initialize any missing machine-local cutoff.

Before uninstalling, run `tmix learning schedule disable` if you enabled scheduled learning.
Run `tmix sync --disable` if you enabled automatic sync.
Then run `brew uninstall trailmix`, or remove your manually installed executable and agent skill.
Uninstallation does not remove `~/.trailmix`, remote backups, or user configuration.

