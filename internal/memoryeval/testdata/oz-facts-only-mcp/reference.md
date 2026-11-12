# Reference: facts-only MCP

## Historical sequence

On October 18, the snapshot restored 30 facts. MCP connected, but shared report binding blocked facts reads.
The user resumed the same session on October 27. The agent replaced that binding with `FACT_SET_ID`.
Only report reads retained the determination requirement.

The recorded replay returned 30 facts and 50 source selections. It verified 651 guidance artifacts.
The replay reported unchanged database and archive contents, with zero provider calls.
The final response recorded 284 offline passes and 33 PostgreSQL passes, with explicit skips.

## Evidence map

- `oz-original-failure`: the original blocker, not a current requirement.
- `oz-binding-plan`: explicit selection, company authorization, and artifact checks.
- `oz-launcher-result`: structured replay output and its limits.
- `oz-completed-fix`: completed change and reported test results.

## Grading limits

The replay output also says `Launcher exit: 130`. It does not establish a clean process exit.
The positive tool results support successful reads, not a claim that every lifecycle check passed.
This case includes no current checkout, database, archive, or authenticated hosted service.
A good answer separates historical success from current operational readiness.
The expected next step is inspection and a local restore check, not paid recomputation.

## Memory outcome

Useful learning replaces or qualifies the old MCP limitation.
A second note that leaves contradictory current instructions unresolved is insufficient.
Grade the future answer separately from the appearance of the notes.
