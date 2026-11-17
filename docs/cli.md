# CLI reference

## Cancel a command

Press Ctrl-C once, or send SIGTERM, to cancel indexing, search, Git backup/sync, or commands that wait for stdin.
These operations share one CLI cancellation context. Canceled stdin commands do not save partial input.
Git operations stop their transport children and attempt bounded cleanup of temporary worktrees.
Cancellation does not undo completed commits, accepted remote pushes, or cached embedding batches.
The CLI exits with status 130 after cancellation cleanup. A second signal terminates immediately and can skip cleanup.

## Commands

```sh
printf '# Verification\n\nTest the combined capability.\n' |
  tmix write preferences/verification.md

tmix search verification
tmix search --limit 5 --json VPN database
tmix read preferences/verification.md

printf '\n## Example\n\nConnect to the database through the VPN.\n' |
  tmix append preferences/verification.md
```

`write` and `append` read UTF-8 content from stdin, with a 16 MiB limit. They preserve the input exactly.

`write` creates parent directories. Existing notes require `write --overwrite NOTE`. `append` requires an existing note and adds no separators.

Note paths are relative to the vault. The `.md` extension is optional. Hidden paths, traversal paths, and symlinks inside the vault are not supported.

Use an alternate vault for experiments:

```sh
tmix --vault /tmp/trailmix-demo init
tmix read --vault /tmp/trailmix-demo inbox/example.md
```

Flags must precede positional arguments. `--vault` works before or after the command. Errors go to stderr with exit status 1.

## Search

Search ranks passages from the local index, then returns the best passage per note.
Text mode uses BM25. Hybrid mode adds embedding similarity and reciprocal rank fusion.
Quoted query terms use OR matching in FTS5. User input is text, not FTS syntax.
Results need not contain every query term. Each result contains a path, line number, and original-Markdown snippet.

Search returns at most 10 notes by default. `--limit` accepts 1–100. Snippets contain at most 240 characters plus a truncation marker.

No matches produce empty text or an empty JSON array. Warnings go to stderr, not JSON output.
Search excludes changed, deleted, and unsafe indexed sources. Read errors fail the operation.
New notes remain undiscoverable until the next index build. Search never refreshes the index automatically.

## Link parsing

Obsidian is not required. Resolve outgoing wiki links and backlinks with:

```sh
tmix links procedures/retries.md
tmix links --json procedures/retries.md
```

The parser recognizes `[[path]]`, `[[note|alias]]`, `[[note#heading]]`, `[[note#^block]]`, and `![[note]]` embeds.
Local references such as `[[#heading]]` have an empty target.
JSON contains `outgoing` and `backlinks` arrays. Each reference includes `source`, resolved `path`, original `target`, and `line`.
Optional fields include `fragment`, `alias`, and `embed`.
Text output includes the direction, source path, source line, and resolved target with its fragment.

The parser preserves order and duplicates. It ignores fenced code, inline code, HTML comments, and escaped opening brackets.
Links cannot span lines. Standard Markdown links are not wiki links and are not included.
The operating guide defines resolution rules for written links. A broken link anywhere in the scan causes an error without partial output.
Fragments are preserved but not validated. Each call scans the vault without a persistent index.
Files can change during a scan. The results are not a consistent snapshot.

