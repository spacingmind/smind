#!/bin/sh
# Smoke-tests `smind service` against the REAL service manager of this machine
# (a systemd user unit on Linux, a LaunchAgent on macOS): install, health,
# idempotent re-install, logs, graceful stop stays stopped, a crash restarts,
# restart, uninstall. It installs and removes a service named smind for the
# current user and listens on SMOKE_PORT (default 4999), so run it only where
# that is fine: CI runners, or a disposable VM. Used by
# .github/workflows/service-unix.yml.
#
#   scripts/service_smoke.sh /path/to/smind
set -eu

bin="${1:?usage: service_smoke.sh /path/to/smind}"
port="${SMOKE_PORT:-4999}"
SMIND_HOME="${SMIND_HOME:-$(mktemp -d)}"
export SMIND_HOME
mkdir -p "$SMIND_HOME"
printf 'server:\n  port: %s\n' "$port" >"$SMIND_HOME/config.yaml"

case "$(uname -s)" in
  Linux) supervisor=systemd ;;
  Darwin) supervisor=launchd ;;
  *) echo "unsupported OS" >&2; exit 1 ;;
esac

step() { printf '\n== %s\n' "$*"; }
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
health() { curl -fsS --max-time 2 "http://127.0.0.1:$port/healthz"; }
wait_up() {
  i=0
  while [ "$i" -lt "${1:-60}" ]; do
    health >/dev/null 2>&1 && return 0
    i=$((i + 1))
    sleep 1
  done
  fail "daemon did not answer on port $port"
}
wait_down() {
  i=0
  while [ "$i" -lt "${1:-30}" ]; do
    health >/dev/null 2>&1 || return 0
    i=$((i + 1))
    sleep 1
  done
  fail "daemon still answering on port $port"
}
service_pid() {
  "$bin" service status 2>/dev/null | sed -n 's/.*pid \([0-9][0-9]*\).*/\1/p' | head -1
}
status_code() {
  set +e
  "$bin" service status >/dev/null 2>&1
  code=$?
  set -e
  echo "$code"
}
cleanup() {
  "$bin" service uninstall >/dev/null 2>&1 || true
}
trap cleanup EXIT

step "install"
"$bin" service install
wait_up 30
health | grep -q "\"managedBy\":\"$supervisor\"" || fail "healthz has no managedBy=$supervisor: $(health)"
[ "$(status_code)" = 0 ] || fail "status after install should exit 0"
"$bin" service status

step "install again is a no-op for the running daemon"
out="$("$bin" service install)"
printf '%s\n' "$out"
printf '%s\n' "$out" | grep -q "already running" || fail "second install should report 'already running'"

step "logs"
"$bin" service logs -n 20 | grep -q "smind listening" || fail "daemon.log has no startup line"
[ -f "$SMIND_HOME/daemon.log" ] || fail "no daemon.log in SMIND_HOME"

step "a graceful stop stays stopped (no restart after smind stop)"
"$bin" stop
wait_down 30
sleep 12 # longer than systemd RestartSec=5 and launchd's 10 s throttle
health >/dev/null 2>&1 && fail "the daemon came back after a graceful stop"
[ "$(status_code)" = 3 ] || fail "status after a graceful stop should exit 3"

step "start"
"$bin" service start
wait_up 30

step "a crash (kill -9) restarts the daemon"
pid="$(service_pid)"
[ -n "$pid" ] || fail "no pid in status output"
kill -9 "$pid"
# launchd can respawn faster than we can poll for the gap, so wait for a NEW pid
# that answers rather than for the daemon to be down.
i=0
while [ "$i" -lt 60 ]; do
  new="$(service_pid)"
  if [ -n "$new" ] && [ "$new" != "$pid" ] && health >/dev/null 2>&1; then
    break
  fi
  i=$((i + 1))
  sleep 1
done
[ "$i" -lt 60 ] || fail "the daemon was not restarted after kill -9 (pid was $pid)"

step "restart"
"$bin" service restart
wait_up 30

step "uninstall"
"$bin" service uninstall
wait_down 30
[ "$(status_code)" = 4 ] || fail "status after uninstall should exit 4"
[ -f "$SMIND_HOME/config.yaml" ] || fail "uninstall must not touch SMIND_HOME"
trap - EXIT
printf '\nservice smoke test passed (%s)\n' "$supervisor"
