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

[sync.schedule]
interval = "5m"

# Optional named backup remotes. tmix remote also manages these.
# [remotes]
# origin = "git@example.com:you/memory.git"
```

Use absolute paths or `~/` paths. An executable can also be a command name on PATH.
Omitted source keys use the default directories. Empty strings disable sources.
CLI source overrides apply only to the current scan or begin command.
The registry records the last scan's sources for diagnostics, not configuration.
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

Each invocation captures all eligible pending turns through its start time, across all configured sessions.
Automatic mode drains that workload in sequential batches, with at most `max_turns` turns from one session per batch.
The configured `timeout` applies separately to each batch.
New sessions and turns wait for the next invocation.
A batch failure stops processing without discarding successful batches or marking unfinished turns reviewed.
Source and session errors prevent a full-success result.
Proposal mode still pauses for approval after one batch.
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

## Navigation maps

The worker maintains `navigation/index.md` and topic maps under `navigation/`.
Navigation uses the same publication policy as lessons.
It runs before learning and after each published lesson batch, even with no pending turns during initial construction.
A failure leaves maintenance pending and stops later batches; completed lesson progress remains intact.

Read the root with `tmix read navigation/index`, then follow relevant links or search.
The root is limited to 4 KiB; topic pages are limited to 8 KiB.
Maps describe the applicability of underlying notes rather than duplicating their rules.
They are agent-maintained, including pre-existing material in `navigation/`.

Use `tmix learning navigation build` for an explicit rebuild, including after manual note changes.
This operation can incur provider charges and does not scan or review sessions.
It reports a `navigation_id` and private `proposal_path` when a proposal is produced.
Accept with `tmix learning accept NAVIGATION_ID`.
To replace a rejected or conflicting proposal, run `tmix learning navigation discard NAVIGATION_ID`.
The next invocation regenerates it from current notes; published pages are not deleted.
Session completion and abort commands apply only to lesson proposals, not navigation.
See `tmix instructions` for maintenance, input limits, and retry behavior.

## Manual guidance

Save explicitly requested guidance without a model request or proposal file:

```sh
printf '# Retry policy\n\nRetry transient failures twice.\n' | tmix learning add procedures/retry
```

Use `tmix learning add --overwrite NOTE` with the complete Markdown to replace existing guidance.
Guidance requires a Markdown title and at most 64 KiB per note.
Manual and automatic guidance use the same publication checks.
Manual guidance does not create or complete a transcript review.

The separate manual evidence ledger and its record, status, begin, complete, and abort commands are removed.
Existing `.learning/ledger.json` files remain untouched. No automatic migration reads or converts them.

## Review and publication

With `publication = "proposals"`, the command reports a private `proposal_path` and `run_id`.
Inspect the file, then publish the proposal explicitly:

```sh
tmix learning accept RUN
```

A proposal awaiting approval pauses later worker passes.
For a session-learning proposal, reject its changes and finish reviewing the evidence:

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

Publication checks existing-note baselines and validates all wiki links in a staging vault once per batch.
A broken link anywhere in the vault prevents publication, including manual guidance.
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
Sync and learning use the same scheduler implementation but separate OS jobs and intervals.
Neither job guarantees ordering relative to the other.
Use `sync.schedule.interval` for sync and `learning.schedule.interval` for learning.
Re-enable the affected job after changing its interval.
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
Temporary vault and remote overrides cannot configure scheduled jobs.
Set `vault.path` and `sync.remote` in TOML instead.
Both jobs read the TOML file on each invocation.

Status includes installed files, OS job state, the latest invocation, and the retained failure report.
Invocation reports include targeted/completed sessions and turns, completed batches, remaining turns, and the current batch's ranges.
The registry directory stores bounded reports in `last_job.json` and `last_failure.json`, not raw harness output.
Later successful runs preserve the last failure report.
The updated agent skill checks this status at the start of each interaction and surfaces retained failures, including later successful invocations.
Refresh copied agent skills after upgrading. Existing agent sessions need the updated operating guide.
A forced termination can leave a stale `running` status without a final failure report.
Linux learning services have no total runtime limit because each batch has its own timeout.
Re-enable an existing Linux learning job after upgrading to install this service change.
On Linux, inspect service diagnostics with `journalctl --user -u trailmix-learning.service`.
On macOS, inspect `launchctl print gui/$(id -u)/com.trailmix.learning` for OS exit information.
Disabling stops the job and removes its definitions while preserving notes and review state.
