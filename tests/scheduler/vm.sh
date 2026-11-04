#!/usr/bin/env bash
# Creates disposable VMs. Explicit invocation can download multi-gigabyte images.
set -euo pipefail
cd "$(dirname "$0")/../.."
platform=${1:-all}
case "$platform" in linux|macos|all) ;; *) echo 'Usage: vm.sh [linux|macos|all]' >&2; exit 1 ;; esac
command -v go >/dev/null
work=$(mktemp -d)
name="trailmix-test-$$"
lima_created=0
tart_created=0
tart_pid=''
tart_bin=''
cleanup() {
 result=$?
 trap - EXIT
 if [ "$lima_created" = 1 ]; then limactl delete --force "$name" || true; fi
 if [ "$tart_created" = 1 ]; then
  "$tart_bin" stop "$name" || true
  if [ -n "$tart_pid" ]; then wait "$tart_pid" || true; fi
  "$tart_bin" delete "$name" || true
 fi
 rm -rf "$work"
 exit "$result"
}
trap cleanup EXIT
case "$(uname -m)" in arm64|aarch64) arch=arm64 ;; x86_64) arch=amd64 ;; *) echo 'Unsupported host architecture'; exit 1 ;; esac
if [ "$platform" = linux ] || [ "$platform" = all ]; then
 command -v limactl >/dev/null || { echo 'Install Lima first' >&2; exit 1; }
 CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -o "$work/tmix" ./cmd/tmix
 cp tests/scheduler/smoke.sh "$work/smoke.sh"
 lima_created=1
 limactl start --name="$name" --tty=false --mount-none --cpus=2 --memory=2 --disk=12 template:ubuntu-24.04
 limactl shell "$name" sudo apt-get update
 limactl shell "$name" sudo apt-get install -y git dbus-user-session
 limactl shell "$name" bash -c 'sudo loginctl enable-linger "$(id -un)"; sudo systemctl start "user@$(id -u).service"'
 tar -C "$work" -cf - tmix smoke.sh | limactl shell "$name" bash -c 'mkdir -p /tmp/trailmix-test; tar -C /tmp/trailmix-test -xf -'
 limactl shell "$name" bash -c 'TMIX_TEST_BINARY=/tmp/trailmix-test/tmix bash /tmp/trailmix-test/smoke.sh'
 limactl delete --force "$name"
 lima_created=0
fi
if [ "$platform" = macos ] || [ "$platform" = all ]; then
 # Tart requires Apple Silicon and macOS. Intel Macs cannot run this backend.
 if [ "$(uname -s)" != Darwin ] || [ "$arch" != arm64 ]; then
  echo 'SKIP: local macOS VM tests require an Apple Silicon Mac (Tart)'
  exit 0
 fi
 tart_bin=${TART_BIN:-}
 if [ -z "$tart_bin" ]; then
  tart_bin=$(command -v tart || true)
  if [ -z "$tart_bin" ]; then
   for candidate in "$HOME/tart.app/Contents/MacOS/tart" /Applications/tart.app/Contents/MacOS/tart; do
    if [ -x "$candidate" ]; then tart_bin=$candidate; break; fi
   done
  fi
 fi
 if [ ! -x "$tart_bin" ]; then
  echo 'Tart not found. Set TART_BIN to its executable or install tart.app in your home directory.' >&2
  exit 1
 fi
 CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o "$work/tmix" ./cmd/tmix
 cp tests/scheduler/smoke.sh "$work/smoke.sh"
 image=${TART_IMAGE:-ghcr.io/cirruslabs/macos-tahoe-base:latest}
 "$tart_bin" clone "$image" "$name"
 tart_created=1
 "$tart_bin" run --no-graphics "$name" >"$work/tart.log" 2>&1 &
 tart_pid=$!
 ready=0
 for attempt in $(seq 1 90); do
  if ! kill -0 "$tart_pid" 2>/dev/null; then break; fi
  if "$tart_bin" exec "$name" /usr/bin/true >"$work/guest-agent.log" 2>&1; then ready=1; break; fi
  sleep 2
 done
 if [ "$ready" != 1 ]; then
  cat "$work/tart.log" "$work/guest-agent.log" 2>/dev/null || true
  echo 'Tart Guest Agent did not become ready; use a non-vanilla Cirrus image with the agent installed.' >&2
  exit 1
 fi
 tar -C "$work" -cf - tmix smoke.sh | "$tart_bin" exec -i "$name" /bin/bash -c 'mkdir -p /tmp/trailmix-test; tar -C /tmp/trailmix-test -xf -'
 "$tart_bin" exec "$name" /bin/bash -c 'TMIX_TEST_BINARY=/tmp/trailmix-test/tmix bash /tmp/trailmix-test/smoke.sh'
fi
