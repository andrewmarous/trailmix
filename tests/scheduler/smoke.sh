#!/usr/bin/env bash
# Run only in a disposable VM or CI runner: this installs a real user scheduler.
set -euo pipefail
: "${TMIX_TEST_BINARY:?Set TMIX_TEST_BINARY to a binary built for this guest}"
work=$(mktemp -d "${TMPDIR:-/tmp}/trailmix-scheduler.XXXXXX")
remote_name="scheduler-test-$$"
case "$(uname -s)" in
  Darwin)
    scheduler_file="$HOME/Library/LaunchAgents/com.trailmix.sync.plist"
    launchctl print "gui/$(id -u)" >/dev/null || { echo 'A GUI launchd user domain is required'; exit 1; }
    ;;
  Linux)
    export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}"
    export DBUS_SESSION_BUS_ADDRESS="${DBUS_SESSION_BUS_ADDRESS:-unix:path=$XDG_RUNTIME_DIR/bus}"
    scheduler_file="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/trailmix-sync.timer"
    systemctl --user show-environment >/dev/null
    ;;
  *) echo 'Unsupported test platform' >&2; exit 1 ;;
esac
if [ -e "$scheduler_file" ]; then echo "Refusing to replace $scheduler_file" >&2; exit 1; fi
mkdir -p "$work/install with spaces/bin" "$work/vault"
install -m 755 "$TMIX_TEST_BINARY" "$work/install with spaces/bin/tmix"
tmix=("$work/install with spaces/bin/tmix" --config "$work/config.toml")
printf '[vault]\npath = "%s"\n[sync]\nremote = "%s"\n' "$work/vault" "$remote_name" > "$work/config.toml"
enabled=0
cleanup() {
  result=$?
  trap - EXIT
  if [ "$enabled" = 1 ]; then "${tmix[@]}" sync --disable || result=1; fi
  if [ "$result" != 0 ]; then
    cat "$HOME/Library/Logs/Trailmix/sync.log" 2>/dev/null || true
    cat "${XDG_STATE_HOME:-$HOME/.local/state}/trailmix/sync.log" 2>/dev/null || true
  fi
  rm -rf "$work"
  exit "$result"
}
trap cleanup EXIT
"${tmix[@]}" --help >/dev/null
git init --bare "$work/remote.git"
git -C "$work/vault" init --initial-branch=main
git -C "$work/vault" config user.name 'Scheduler Test'
git -C "$work/vault" config user.email scheduler@example.invalid
git -C "$work/vault" config commit.gpgsign false
printf '# Scheduled memory\n' > "$work/vault/note.md"
"${tmix[@]}" remote add "$remote_name" "$work/remote.git"
# Set cleanup before enable, to clean up even a partially failed installation.
enabled=1
"${tmix[@]}" sync --enable
test -f "$scheduler_file"
case "$(uname -s)" in
  Darwin) plutil -lint "$scheduler_file"; launchctl print "gui/$(id -u)/com.trailmix.sync" >/dev/null ;;
  Linux) systemctl --user is-enabled trailmix-sync.timer; systemctl --user is-active trailmix-sync.timer ;;
esac
# No direct sync or manual scheduler trigger: RunAtLoad / the timer must do the work.
for attempt in $(seq 1 90); do
  if git --git-dir="$work/remote.git" show main:note.md > "$work/actual" 2>/dev/null &&
     cmp "$work/vault/note.md" "$work/actual" && test -f "$work/vault/.tmx-last-sync"; then break; fi
  sleep 2
done
git --git-dir="$work/remote.git" show main:note.md | cmp "$work/vault/note.md" -
test -f "$work/vault/.tmx-last-sync"
"${tmix[@]}" sync --disable
enabled=0
test ! -e "$scheduler_file"
case "$(uname -s)" in
  Darwin) if launchctl print "gui/$(id -u)/com.trailmix.sync" >/dev/null 2>&1; then exit 1; fi ;;
  Linux)
    test ! -e "$(dirname "$scheduler_file")/trailmix-sync.service"
    if systemctl --user is-active --quiet trailmix-sync.timer; then exit 1; fi
    ;;
esac
echo "PASS: scheduler installation, scheduled sync, and removal ($(uname -sm))"
