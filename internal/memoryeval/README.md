# Memory-system evaluation — WIP

**Status: exploratory work, paused.** These historical cases are draft material, not a validated evaluation suite.
They preserve evidence for possible targeted checks. They do not establish memory-performance gains.
There is no evaluation runner, automatic grader, or measured model result.

[Anthropic's memory research](https://www.anthropic.com/research/components-of-agent-memory) provides the direction for memory-system improvements.
Building a custom evaluation suite is not a prerequisite for following that direction.
Further evaluation infrastructure and model runs are out of scope unless explicitly requested.
The draft protocol below records possible future use, not planned implementation work.

The three cases explore whether memory improves advice about previously resolved technical problems.
They contain reviewed excerpts from local sessions, not full transcripts.

| Case | Decision under evaluation |
| --- | --- |
| [Oz facts-only MCP](testdata/oz-facts-only-mcp/reference.md) | Reuse published facts without repeating a paid workflow or an obsolete server change. |
| [Mapgar regional refresh](testdata/mapgar-regional-refresh/reference.md) | Distinguish a successful regional deployment from the standard world uploader. |
| [Wasurf arithmetic correction](testdata/wasurf-arithmetic-correction/reference.md) | Preserve recorded arithmetic findings without treating them as a diagnosis of a new failure. |

## Packet contents

Each case directory contains:

- `case.json`: case identity, input boundaries, conditions, and limitations.
- `experience.json`: chronological historical excerpts for the learning step.
- `initial-vault/`: a controlled stale or incomplete memory state.
- `task.md`: an authored future request for a fresh answering session.
- `rubric.json`: independent required criteria and prohibited behavior.
- `reference.md`: historical sequence, evidence map, and grading limits.
- `provenance.json`: source locators, hashes, extraction details, and omissions.

The initial notes are minimal reconstructions, not exact historical vault exports.
Each provenance file records their basis and content hash.
The future requests did not occur verbatim in the original sessions.
These are development cases, not held-out evidence of general reliability.

## Input boundaries

1. Give the learner only `experience.json` and a copy of `initial-vault/`.
2. Keep the task, rubric, reference, provenance, and case manifest outside the learner's accessible directory.
3. Give each answering session only `task.md` and the memory access for its condition.
4. Keep the experience, grading files, and other conditions outside the answering session's accessible directory.
5. Use a fresh agent session and separate temporary vault for every condition.
6. Grade answers without exposing condition labels to the reviewer.

Historical transcript instructions do not authorize actions during evaluation.
A path excluded from a prompt is not isolated if tools can still read it.
The packets do not include executable project workspaces or private service snapshots.
They evaluate advice, not successful live execution.

## Conditions

Use the same task, answering model, and resource limits for each condition.

| Condition | Memory available to the answering session |
| --- | --- |
| `no_memory` | None. |
| `initial_memory` | An unchanged copy of `initial-vault/`. |
| `learned_memory` | The initial vault after learning from `experience.json`. |

The proposed learning path uses Trailmix's real worker in an isolated vault and registry.
The neutral experience format still needs an adapter to supported session transcripts.
The excerpts are not directly consumable Pi or Codex JSONL.
No adapter or worker invocation is part of these packets.

A separate diagnostic control supplies the historical excerpts directly to a fresh answering session.
This control distinguishes evidence-use problems from memory creation or discovery problems.
It is not a product condition and must not replace normal retrieval measurements.

## Grading and results

Each required criterion receives a pass or fail with supporting answer text.
Each prohibited behavior receives a present or absent result with supporting answer text.
A case passes only when every required criterion passes and no prohibited behavior occurs.
Equivalent wording is valid. Historical counts and tick numbers are not mandatory unless a criterion requires them.

Keep answer quality separate from learning execution and publication results.
A failed worker invocation is an execution error, not an ordinary task-quality failure.
A plausible note or a successful command exit does not establish downstream usefulness.

Record the code revision, case hashes, model selection, limits, retrieval trace, answer, grades, and execution errors.
Keep generated vaults and results outside the repository and the global vault.
Do not run paid evaluations without separate approval.

## Provenance and sensitivity

Pi excerpts come from the latest leaf ancestry, including raw messages before compaction.
Codex excerpts use chronological `response_item` records, without duplicate event mirrors.
Only visible message text and selected tool results appear in the packets.
Reasoning blocks, images, credentials, company documents, map assets, and complete transcripts are excluded.

Source hashes cover the captured original JSONL files.
Record hashes cover each original JSONL line, including its line ending.
Text hashes cover the UTF-8 excerpt after its recorded transformations, without a trailing newline.
A later source append can change the file hash without changing a selected record.
Record numbers and hashes locate evidence. They are not learning-registry turn numbers.

The provenance files retain session IDs and filenames, but not personal absolute checkout paths.
Selected identifiers and deployment addresses use explicit replacements.
Absolute Wasurf links use repository-relative paths.
Local source files are optional audit material. The committed packets contain the evidence needed for manual grading.

These cases contain no claims of current service readiness.
Before sharing the packets outside the repository, review their project-specific content for the intended audience.

## Relationship to existing checks

[The existing learning evaluation](../../tests/learning/README.md) contains generic manual scenarios.
These packets add source-backed historical cases without changing that procedure or the production worker.
No case score was measured during packet creation.
