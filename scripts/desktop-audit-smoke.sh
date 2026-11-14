#!/usr/bin/env bash
# Smoke-test a built audit app: it must REFUSE to start without a scratch daemon. Two launches, each
# with a scratch HOME and an otherwise empty environment, each required to exit with status 2, the
# refusal message naming the missing variable, and nothing created in HOME:
#
#   1. SMIND_HOME unset, SMIND_DAEMON_URL set to the discard port (http://127.0.0.1:9). The URL is always
#      set here so that a build WITHOUT the guard (exactly what this smoke exists to catch) cannot attach
#      to the real daemon on :4648: it would only ever try the discard port.
#   2. Only after (1) passed, which proves the guard exists: SMIND_HOME set to a scratch directory and
#      SMIND_DAEMON_URL unset. A guard-less build would try the default :4648 here, so this launch is
#      never made for an app that failed (1), and it has a short timeout.
#
# An app that keeps running is killed after the timeout and fails. See
# docs/plans/active/desktop-audit-guard-wiring.md.
#
# Usage: scripts/desktop-audit-smoke.sh <path to smind-desktop>
# Env:   SMOKE_TIMEOUT        seconds to wait for the first refusal (default 15)
#        SMOKE_TIMEOUT_SHORT  seconds for the second launch (default 5)
# Exit:  0 the guard refused as it should, or there is no build to check (skipped, said so);
#        1 the app did not refuse (it carries no guard, or it misbehaved)
set -euo pipefail

bin=${1:-}
if [ -z "$bin" ] || [ ! -x "$bin" ]; then
  echo "desktop-audit-smoke: skipped, no built app at '${bin}'"
  exit 0
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
discard_url=http://127.0.0.1:9

fail() {
  echo "desktop-audit-smoke: FAIL: $*" >&2
  exit 1
}

# launch <label> <variable the refusal must name> <timeout seconds> [VAR=value ...]
launch() {
  local label=$1 names=$2 timeout=$3
  shift 3
  local home="$work/home-$label"
  rm -rf "$home"
  mkdir "$home"
  env -i HOME="$home" PATH=/usr/bin:/bin "$@" "$bin" >"$work/stdout-$label" 2>"$work/stderr-$label" &
  local pid=$! waited=0
  while kill -0 "$pid" 2>/dev/null; do
    if [ "$waited" -ge $((timeout * 10)) ]; then
      kill "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
      fail "$label: the app was still running after ${timeout}s: it carries no audit guard"
    fi
    sleep 0.1
    waited=$((waited + 1))
  done
  local code=0
  wait "$pid" || code=$?
  [ "$code" -eq 2 ] || fail "$label: exit status $code, want 2"
  grep -q "smind audit build:" "$work/stderr-$label" || fail "$label: no refusal message on stderr: $(cat "$work/stderr-$label")"
  grep -q "$names" "$work/stderr-$label" || fail "$label: the refusal does not name $names: $(cat "$work/stderr-$label")"
  [ -z "$(ls -A "$home")" ] || fail "$label: the app created files in its HOME before refusing"
}

# 1: no SMIND_HOME. The discard URL keeps a guard-less build away from the real daemon.
launch no-home SMIND_HOME "${SMOKE_TIMEOUT:-15}" SMIND_DAEMON_URL="$discard_url"

# 2: a scratch SMIND_HOME and no URL. Reached only because (1) passed, so the guard exists.
mkdir "$work/scratch-smind"
launch no-url SMIND_DAEMON_URL "${SMOKE_TIMEOUT_SHORT:-5}" SMIND_HOME="$work/scratch-smind"
[ -z "$(ls -A "$work/scratch-smind")" ] || fail "no-url: the app wrote into the scratch SMIND_HOME before refusing"

echo "desktop-audit-smoke: ok, the built app refuses to start without a scratch daemon"
