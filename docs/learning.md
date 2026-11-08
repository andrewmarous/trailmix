# Scheduled learning

Trailmix can review completed Pi and Codex turns through either harness.
The worker produces evidence-backed Markdown changes or records that no reusable lesson is supported.
Scheduling and automatic publication both require explicit configuration.

## Configure

1. Install a current Pi or Codex executable and authenticate through that harness.
2. Run `tmix init` to initialize the vault, configuration template, and machine-local learning cutoff.
3. Run `tmix config path` to locate the TOML file.
4. Edit the file to select the vault, harness, and publication policy.

The default file is `~/.config/trailmix/config.toml` on macOS and Linux.
`XDG_CONFIG_HOME` overrides its directory. Global `--config PATH` selects another file.
`tmix config init` creates only the template; it refuses to overwrite an existing file.
`tmix config show` prints it. Unknown keys cause errors.

For example:

```toml
[vault]
path = "~/.trailmix"

[learning]
harness = "pi"                   # Or "codex".
executable = "/absolute/path/pi"  # Empty uses the harness name on PATH.
model = ""                       # Empty uses the harness's configured default.
publication = "proposals"        # Or explicitly select "automatic".
state_dir = "~/.local/state/trailmix/learning"
timeout = "20m"
max_turns = 20
max_input_bytes = 2097152

[learning.sources]
pi = "~/.pi/agent/sessions"
codex = "~/.codex/sessions"
codex_archive = "~/.codex/archived_sessions"

[learning.schedule]
interval = "1h"

[sync]
remote = "origin"

# Optional named backup remotes. tmix remote also manages these.
# [remotes]
# origin = "git@example.com:you/memory.git"
```

Use absolute paths or `~/` paths. An executable can also be a command name on PATH.
Omit source keys to retain registry settings. Use empty strings to disable unavailable sources.
The cutoff, discovery inventory, review progress, and worker results remain local state, not TOML settings.
Initialization preserves the existing cutoff. Earlier turns remain excluded, not falsely marked reviewed.
New turns in resumed older sessions remain eligible.

With the default config path, legacy `remotes.gitconfig` remains readable until the next remote mutation migrates its entries into TOML.
An alternate `--config` file never inherits legacy remotes.
Remote mutations canonicalize TOML formatting and remove comments; other typed settings remain unchanged.
An explicitly empty `[remotes]` table disables legacy fallback.

## Check and run

```sh
tmix learning doctor
tmix learning doctor --probe
tmix learning run
```

The first command checks configuration, initialization, and required CLI options.
With an explicit Pi model, it also checks Pi credentials.
The probe makes a real model request and can incur provider charges.
Only a real invocation establishes readiness for the selected model/provider.

Credentials stay with the harness. Avoid storing keys in TOML or generated service files.
Shell-only API keys are not captured by scheduler installation.
Use harness credential storage or deliberately configure the user service environment.

Each pass reviews at most one session and `max_turns` pending turns.
Evidence and existing notes are sent to the configured harness/provider; inspect sensitive source logs before enabling this.
The worker includes existing notes to detect duplicates and corrections.
Oversized input fails rather than silently dropping evidence or notes.
Reduce `max_turns` or increase `max_input_bytes` to resolve an input-limit failure.
Limits bound input, output, turns, and model runtime, not provider expenditure.

Pi runs with tools, extensions, MCP, context files, skills, and persistent sessions disabled.
Codex runs ephemerally in a read-only sandbox with shell tools and hooks disabled.
Both run from a temporary directory. Neither resumes your interactive session.
Codex retains model/provider settings but disables MCP, app, browser, and computer tools for reviews.
Treat the harness executable and its credential configuration as trusted software.

## Review and publication

With `publication = "proposals"`, the command reports a private `proposal_path` and `run_id`.
Inspect the file, then publish the proposal explicitly:

```sh
tmix learning accept RUN
```

A proposal awaiting approval pauses later worker passes.
To reject its changes and finish reviewing the evidence:

```sh
printf 'Rejected proposed changes; no notes published.\n' | tmix learning sessions complete RUN
```

To leave the turns pending for another review:

```sh
tmix learning sessions abort RUN
```

With `publication = "automatic"`, the worker publishes without asking for approval.
A valid result with no supported lesson also advances the reviewed snapshot.
The worker requires a structured result and citations to pending turns; exit status zero alone is insufficient.
These checks do not prove that every model-generated claim is correct.

Publication checks existing-note baselines and validates wiki links in a staging vault.
Conflicting edits fail rather than overwriting newer notes.
A durable proposal supports idempotent retries after interrupted or partial publication.
Review progress commits only after successful publication.
Multiple note writes are not one filesystem transaction; coordinate external editors.

An exclusive worker lock prevents overlapping model jobs.
The registry lock is released while the model runs.
A worker interrupted before saving a result retries its unchanged pending turns later.
A worker with a saved result retries publication without regenerating it.
Manual active reviews remain untouched.

## Install the job

After a successful manual pass:

```sh
tmix learning schedule enable
tmix learning schedule status
tmix learning schedule disable
```

Enabling installs or updates one learning job for the current user. It requires no root access.
Initialization alone never installs a job.

- **macOS:** `~/Library/LaunchAgents/com.trailmix.learning.plist`, loaded into the GUI user session.
- **Linux:** `trailmix-learning.service` and `trailmix-learning.timer` in the systemd user configuration directory.

Linux requires a running systemd user manager. Staying active after logout requires separately enabled user lingering.
Other operating systems return an unsupported-platform error; there is no cron fallback.
macOS starts a pass when loading the job. Linux starts after approximately one minute.
Linux then waits the configured interval after each pass. macOS uses launchd's start interval.
Missed intervals are not replayed as separate jobs. The cutoff still determines pending work.

The job invokes the absolute Trailmix executable with the absolute TOML path.
PATH is captured at installation so harness launchers can locate runtimes such as Node.js.
Shell startup scripts and shell-only credentials are not captured.
Use a stable installed executable, not a temporary `go run` binary.
Re-enable after interval, PATH, or executable-location changes. Other TOML edits apply on the next invocation.
A temporary `--vault` override cannot configure the scheduled vault; set `vault.path` in TOML instead.

Status includes installed files, OS job state, and the last worker result.
The registry directory's `last_job.json` is overwritten with bounded status metadata, not raw harness output.
On Linux, inspect service diagnostics with `journalctl --user -u trailmix-learning.service`.
On macOS, inspect `launchctl print gui/$(id -u)/com.trailmix.learning` for OS exit information.
Disabling stops the job and removes its definitions while preserving notes and review state.
