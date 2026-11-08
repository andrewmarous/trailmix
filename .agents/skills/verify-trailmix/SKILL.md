---
name: verify-trailmix
description: Verify changed Trailmix behavior with existing Go tests, isolated CLI checks, and relevant scheduler or release checks. Use before completing code changes.
---

# Verify Trailmix

1. Read the nearest `AGENTS.md` and `../principle-prove-it-works/SKILL.md`. Identify the observable result that proves the requested behavior.
2. Read the affected implementation, callers, and existing checks. Run a focused regression check first, then `make test vet`.
3. Select additional checks for the affected boundary:

   | Boundary | Existing checks and limits |
   | --- | --- |
   | Vault paths, writes, and locking | Tests in `vault/`, including subprocess locking tests. Preserve traversal and symlink rejection. |
   | Learning discovery, review progress, and publication | Tests in `learning/`, `config/`, and `cmd/tmix/`. Preserve cutoff, evidence, baseline, and approval contracts. |
   | Generated operating guide | Run `make docs` after source changes, then `make docs-check`. Verify contributor instructions survive generation. |
   | CLI packaging | Read `scripts/release.sh`, `tests/release/smoke.sh`, and `RELEASING.md` before local artifact checks. Do not publish a release without approval. |
   | Scheduler installation | Read `tests/scheduler/vm.sh` and `tests/scheduler/smoke.sh`. Use existing CI or an authorized disposable VM. |
   | Learning usefulness | Use `tests/learning/README.md` for an explicitly requested evaluation. Report output quality separately from worker execution. |

4. For manual CLI checks, isolate the vault, registry, configuration, and HOME when the command uses it. Use synthetic sessions and fake harnesses.
   Inspect scripts before execution. The scheduler smoke test changes a real user scheduler and remote configuration.
   Never run it on the developer's host as a routine check. Live model calls require separate authorization.
5. Inspect the actual output, persisted state, or artifact that demonstrates the requested result. A successful build alone does not prove behavior.
   Reuse existing tests rather than introduce a verification framework. Run `gofmt` on changed Go files and `bash -n` on changed Bash scripts.
6. Run `git diff --check`. Inspect the final diff and relevant untracked files for unrelated edits, secrets, and generated-file drift.
   Report commands, results, observed behavior, unavailable checks, and remaining uncertainty. Clean up only resources created by the checks.

These instructions route existing checks. They do not establish that the checks pass or that model-produced lessons are correct.
