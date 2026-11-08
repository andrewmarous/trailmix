---
name: deslop
description: Reduce code introduced by a branch while preserving requested behavior. Use when asked to deslop a diff, remove overengineering, or simplify changes before merge. Review the complete merge-base diff and make two deletion passes.
---

# Deslop

Substantially reduce the code introduced by the change. Preserve the requested behavior and existing contracts.

Prefer these actions, in order:

1. Delete code.
2. Reuse existing code.
3. Simplify code.
4. Rewrite code only when necessary.

Do not add features, broaden scope, or improve unrelated code.
Do not trade clarity or correctness for fewer lines.

## First priority: Mike Acton approximator

Before proposing or implementing a solution, ask in order:

1. Can we not do this at all?
2. Can we do this only once?
3. Can we do this fewer times?
4. Can we approximate the results so no one notices?
5. Can we use a small lookup table?
6. Can we use a small FIFO?
7. Can we constrain the problem further?

Apply these questions in both deletion passes while preserving the requested behavior and existing contracts.

## 1. Establish the baseline

Read the applicable `AGENTS.md` files.
Identify the requested behavior from the task, contracts, and relevant tests.
If the behavior is unclear, ask before editing.

Inspect `git status --short` and the current branch.
Identify the target branch from the user request or local repository metadata.
Do not assume that the upstream branch is the merge target.
If the target is ambiguous, ask the user.
Do not fetch or change branches without permission.

Resolve and record a fixed merge-base SHA:

```sh
git merge-base HEAD <target-ref>
```

Review the complete tracked diff from that SHA to the current working tree:

```sh
git diff --stat <merge-base-sha>
git diff --find-renames <merge-base-sha> --
git ls-files --others --exclude-standard
```

This diff includes committed, staged, and unstaged changes.
Read relevant untracked files separately. Include them in the review and LOC totals.
If output is truncated, continue until you review every changed file.
Inspect binary changes without counting them as text LOC.

Record existing local changes before editing.
Preserve unrelated work, including unrelated hunks within changed files.
Do not reset, stash, stage, or discard user changes.
If ownership or scope is unclear, ask before editing those changes.

Record the initial LOC totals with `git diff --numstat <merge-base-sha> --`.
Count physical added and deleted text lines, not semantic statements.
Add relevant untracked text files to the added-line total without staging them.
Record the file set and exclusions for the final comparison.

## 2. Inspect before editing

Read the surrounding implementation, callers, tests, and package configuration.
Search the repository for existing implementations and conventions that the diff can reuse.
Check actual contracts before treating compatibility or defensive code as unnecessary.

Look for these deletion candidates:

- Abstractions introduced for one use
- Duplicate implementations of existing repository functionality
- Unnecessary helper functions
- Unnecessary new types or interfaces
- Speculative extensibility
- Compatibility code without an existing contract
- Defensive handling of impossible or unrequired states
- Excessive comments
- Redundant tests
- Tests of implementation details
- Unnecessary configuration or options
- Unnecessary error wrapping
- Needless indirection
- Code that can use an existing repository convention.

For each candidate, identify why deletion preserves the requested behavior.
If evidence is insufficient, keep the code or ask the user.

## 3. First deletion pass

Remove unnecessary code before introducing replacements.
Reuse existing functionality only when its behavior matches the requirement.
Inline single-use helpers when this makes the code simpler.
Keep required types, validation, and error translation at real contract boundaries.

Preserve security checks, tenant isolation, resource cleanup, and persistence contracts.
Do not label untrusted input states as impossible without evidence.
Preserve tests for required behavior, invalid input, and relevant failures.
Delete tests only when their behavioral coverage is redundant or unnecessary.
Do not weaken assertions, typing, or lint rules to make checks pass.
Keep comments that explain non-obvious constraints or required decisions.

## 4. Validate and delete again

Run the relevant tests, typechecker, and linter specified by the repository.
Use existing package environments and locked dependencies.
Do not run deployments, destructive operations, or paid integrations without explicit authorization.
Report unavailable checks and pre-existing failures separately.

Review the complete resulting diff against the same merge-base SHA.
Perform a second deletion pass with the same candidate list.
Inspect new helpers, types, options, and tests introduced during the first pass.
If no further deletion is safe, state that result.

After the second pass, rerun the checks affected by those edits.
Run `git diff --check`.
Review untracked files for whitespace errors separately.
Inspect the final diff for unrelated changes, secrets, generated files, and accidental lock-file updates.

## 5. Report

Recalculate LOC with the same merge-base SHA, file scope, and counting method.
Keep deleted files in the comparison. Include newly introduced files in both scope descriptions.
Separate production code from tests, documentation, and configuration when practical.
Do not claim code reduction from documentation deletion alone.

Report these results concisely:

- The target ref and fixed merge-base SHA
- The main deletions and reused repository functionality
- The result of the second deletion pass
- Added/deleted/net LOC before and after, where net equals added minus deleted
- The reduction in added LOC, with exclusions and untracked-file accounting
- The commands run, their results, and checks not run.

If safe simplification does not substantially reduce code, explain the remaining constraints.
Do not force deletions to meet a numeric target.
