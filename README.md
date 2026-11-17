# Trailmix

**Give your AI agents a memory they can share.**

Save a decision once. Find it in your next session, even with a different agent.

Trailmix (`tmix`) stores knowledge as Markdown files you own. Search from the terminal, let your agent look things up, or browse in Obsidian.

- **Shared knowledge:** decisions, preferences, and procedures across agent sessions.
- **Ordinary files:** readable Markdown, optional Git backup, no required account or daemon.
- **Optional learning:** review Pi and Codex sessions for reusable technical lessons.

## Get started

On macOS or Linux, install with Homebrew:

```sh
brew install andrewmarous/tap/trailmix
tmix --version
```

Intel/AMD64 and ARM64 are supported. Windows is not supported.
For release downloads and source installation, see the [installation guide](docs/installation.md).

Create your memory store (a "vault") and save your first decision:

```sh
tmix init
printf '# Retry policy\n\nRetry transient failures up to three times.\n' | tmix write decisions/retries
tmix index
tmix search retry
tmix read decisions/retries
```

The search finds `decisions/retries.md`. The last command prints the complete note.

Your notes live in `~/.trailmix` by default. Obsidian is optional.
Run `tmix index` after changes to make new or edited notes searchable.

`tmix init` also creates the configuration file and initializes the learning cutoff.
It preserves existing notes and configuration. It does not enable background jobs.

## Connect your agent

Install the included Trailmix skill so your agent knows when to search and what to save.
The `tmix` executable must be on the agent's `PATH`.

For a Homebrew installation, choose your agent:

### Pi

```sh
mkdir -p ~/.pi/agent/skills
cp -R "$(brew --prefix trailmix)/share/trailmix/skills/trailmix" ~/.pi/agent/skills/
```

### Claude Code

```sh
mkdir -p ~/.claude/skills
cp -R "$(brew --prefix trailmix)/share/trailmix/skills/trailmix" ~/.claude/skills/
```

For a release archive or repository checkout, use `skills/trailmix` as the source path instead.
Run the copy command from the extracted archive or repository directory.

Restart your agent session, then ask:

> Search Trailmix for our retry policy and tell me which note you used. Do not save anything.

The agent must find `decisions/retries.md` from the quickstart.
The skill tells agents to save or correct notes only when you ask.
Installing the skill does **not** enable automatic session learning.

For the complete agent policy, run `tmix instructions` or read the [operating guide](https://github.com/andrewmarous/trailmix/blob/main/guide/operating-guide.md).

## Make it yours

| Want to… | Start here |
|---|---|
| Learn from Pi or Codex sessions | [Scheduled learning](docs/learning.md) |
| Search with local embeddings | [Local retrieval](docs/retrieval.md) |
| Back up notes or sync between machines | [Git backup and sync](docs/sync.md) |
| Browse notes visually | [Obsidian setup](docs/obsidian.md) |
| Explore commands, search, and wiki links | [CLI reference](docs/cli.md) |
| Upgrade or uninstall | [Installation and updates](docs/installation.md#upgrade-and-uninstall) |

Text search runs locally without a model. Hybrid search requires explicit local model and runtime configuration.

Optional session learning uses your configured Pi or Codex harness.
You choose whether to review proposed notes or authorize automatic publication.
Model requests can incur provider charges.

**Privacy:** Notes are not encrypted. Keep secrets out of your vault and use a private repository for remote backups.
Optional learning sends selected session evidence and notes to your configured harness/provider.
Inspect sensitive session logs before you enable it.

## Contribute

See [Contributing](https://github.com/andrewmarous/trailmix/blob/main/CONTRIBUTING.md) for builds, tests, library details, and guide maintenance.
