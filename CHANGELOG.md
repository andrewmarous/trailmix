# Trailmix v0.1.0

First public release.

- Store shared agent memory as Markdown files.
- Search notes and resolve Obsidian wiki links and backlinks.
- Read, write, and append notes from the command line.
- Print the agent operating guide with `tmix instructions`.
- Back up notes to Git and synchronize through a configured remote.
- Schedule sync with macOS launchd or Linux systemd user timers.
- Optionally install Obsidian on supported systems.
- Install prebuilt binaries for macOS and Linux on AMD64 and ARM64.

## Limits

Windows and network filesystems are not supported in this release.
Notes are not encrypted. Use private repositories for backups.
External editors do not use Trailmix locks. Avoid external edits during sync.
Search, link scans, and backups do not provide consistent snapshots across notes.
Homebrew installs the CLI and skill files. Agent skill discovery requires manual setup.
