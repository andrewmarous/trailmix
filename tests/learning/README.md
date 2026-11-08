# Learning evaluation

This is a manual agent evaluation, not an automated model benchmark.
The CLI tests cover ledger transitions. These cases evaluate agent behavior.

## Procedure

1. Build a local executable with `go build -o /tmp/tmix ./cmd/tmix`.
2. Create a separate temporary vault for each case and condition.
3. Run `/tmp/tmix --vault PATH init --learning-state-dir STATE` with a separate temporary registry directory.
4. Give session A the case evidence from `cases.json`.
5. Request a learning review for the learning condition.
6. Inspect proposed changes before approval.
7. Start session B with fresh context and only the follow-up task.
8. Give session B the same vault and the Trailmix skill.
9. Record the result with the rubric below.

Compare three conditions: no memory, explicit-save memory, and learning review.
For explicit-save memory, ask session A to save the supported lesson without a consolidation review.
Use the same model and task inputs across conditions.
Keep evaluation outputs outside the production vault.
For transcript-based evaluations, pass `--state-dir STATE` to each `learning sessions` command.
Set an earlier cutoff explicitly when evaluating historical sessions.
The manual evidence-ledger workflow does not use the transcript cutoff.

## Rubric

Record these fields for each case and condition:

- Model and version.
- Task success: pass or fail, with observed evidence.
- Repeated mistake: yes or no.
- Retrieval: note paths read and search calls made.
- Unsupported published claims: count and examples.
- Published note count and total bytes.
- Review result: new note, correction, merge, or no change.
- Approval: requested before publication, yes or no.
- Ledger: only snapshot sessions processed, yes or no.

A plausible-looking note is not evidence of task success.
Synthetic fixture evidence evaluates reasoning about supplied observations, not real command execution.
Add real development-session cases before making reliability claims.
