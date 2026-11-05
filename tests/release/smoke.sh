#!/usr/bin/env bash
set -euo pipefail
binary="${1:?binary path required}"
expected="${2:?version required}"
"$binary" --version | grep -F "tmix $expected (commit "
vault="$(mktemp -d)"
trap 'rm -rf "$vault"' EXIT
"$binary" --vault "$vault" init
printf '# Release check\n\nFind this release-check-marker.\n' | "$binary" --vault "$vault" write inbox/check
"$binary" --vault "$vault" search release-check-marker | grep -F 'inbox/check.md'
"$binary" --vault "$vault" read inbox/check | grep -F 'release-check-marker'
printf '\n[[inbox/check]]\n' | "$binary" --vault "$vault" write inbox/link
"$binary" --vault "$vault" links inbox/link | grep -F 'inbox/check.md'
"$binary" instructions | grep -F 'Trailmix operating guide'
