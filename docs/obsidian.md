# Browse notes in Obsidian

## Start

Create the default vault:

```sh
tmix init
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

On macOS or Linux with `apt` and `dpkg`, optionally install Obsidian:

```sh
tmix init --install-obsidian
```

On macOS, installation uses Homebrew. On Linux, installation downloads the latest official `.deb` package and installs it through `apt`.
Linux installation requires root access or `sudo`. Only architectures with an official `.deb` package are supported (amd64 or arm64).
Other Linux package formats are not supported.

Initialization detects Obsidian in standard application locations or on `PATH`. Detection does not cover every custom installation location.

The vault remains usable if installation fails. To view the vault in Obsidian, open the vault directory as an existing vault.

