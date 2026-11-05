# Release procedure

## Prerequisites

The release maintainer needs write access to `andrewmarous/trailmix` and permission to push tags.
GitHub Actions publishes archives with the repository's `GITHUB_TOKEN`. No personal token is necessary.
The public Homebrew tap checks releases daily and through manual workflow dispatch.
The tap uses its own repository token to update the formula. No cross-repository write token is necessary.

## Publish

1. Update `CHANGELOG.md` for the new release.
2. Update the source-install version in `README.md`.
3. Run `make test vet`.
4. Commit the release changes and push `main`.
5. Wait for the scheduler CI matrix to pass on all four platforms.
6. Create the version tag with `git tag vX.Y.Z`.
7. Push the tag with `git push origin vX.Y.Z`.
8. Wait for the Release workflow to finish.
9. Run the tap's Update formula workflow for an immediate Homebrew update.
10. Install the release with Homebrew and run `tmix --version`.

The release workflow builds four archives and `checksums.txt`. It smoke-tests the Linux AMD64 archive before publication.
Each archive contains the executable, license, README, and agent skill.
The version, commit, and commit date are embedded in each release executable.

## Local artifact checks

Use an empty `dist` directory before each build:

```sh
bash scripts/release.sh v0.1.0
mkdir -p /tmp/trailmix-release-check
tar -xzf dist/trailmix_0.1.0_darwin_arm64.tar.gz -C /tmp/trailmix-release-check
bash tests/release/smoke.sh /tmp/trailmix-release-check/tmix v0.1.0
```

Select the archive that matches your host platform and architecture.
Local builds do not publish assets or create tags.

## Failed releases

If publication fails before a release exists, correct the failure and rerun the workflow for that tag.
Do not move a published tag or replace published archives. Publish a new patch version instead.
If the CLI release succeeds but the tap update fails, rerun the tap workflow after correction.
Existing releases remain available while the tap update waits.
