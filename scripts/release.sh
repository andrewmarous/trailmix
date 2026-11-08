#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
tag="${1:?Usage: bash scripts/release.sh v0.1.0}"
[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo 'Expected a stable vX.Y.Z tag' >&2; exit 1; }
commit="$(git rev-parse HEAD)"
date="$(git show -s --format=%cI HEAD)"
mkdir -p dist
if find dist -mindepth 1 -print -quit | grep -q .; then
  echo 'dist must be empty before a release build' >&2
  exit 1
fi
for os in darwin linux; do
  for arch in amd64 arm64; do
    name="trailmix_${tag#v}_${os}_${arch}"
    stage="$(mktemp -d)"
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath \
      -ldflags "-s -w -X main.version=$tag -X main.commit=$commit -X main.date=$date" \
      -o "$stage/tmix" ./cmd/tmix
    cp LICENSE README.md "$stage/"
    cp -R skills docs "$stage/"
    tar -czf "dist/$name.tar.gz" -C "$stage" tmix LICENSE README.md skills docs
    rm -rf "$stage"
  done
done
(cd dist && shasum -a 256 ./*.tar.gz > checksums.txt)
