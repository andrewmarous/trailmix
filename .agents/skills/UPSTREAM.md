# Imported engineering skills

Installed into this repository for agents that develop Trailmix. These skills are not part of the distributed product skill.
Copied from `../oz/.agents/skills/`; upstream revisions below come from Oz's provenance record.
Upstream skill directories are copied without local edits, including their references and helpers.

| Source | Revision | Skills |
| --- | --- | --- |
| https://github.com/DietrichGebert/ponytail | `9b58c1ffb790c075ca32e70a89cf1d80588f4abf` | ponytail, ponytail-review |
| https://github.com/backnotprop/pstack | `3a604672c46cd8187d2b19980eae0a34f9f91138` | how, tdd, interrogate, show-me-your-work, unslop, principle-prove-it-works, principle-test-behavior-not-implementation, principle-encode-lessons-in-structure |

`unslop` and `principle-encode-lessons-in-structure` support the decision-trail skill.
`deslop` is copied from Oz's local skill. `verify-trailmix` is a local replacement for `verify-oz`.
Operational routing and Trailmix overrides live in root `AGENTS.md`.
`LICENSE.ponytail` and `LICENSE.pstack` retain the imported upstream licenses.

To update, review the selected upstream revision and its dependency references, replace only its imported skill directories, and record the revision here. Preserve local skills and review upstream changes against the Trailmix overrides. Full poteto-mode, factory orchestration, shipping automation, and lifecycle hooks are outside this installation.
