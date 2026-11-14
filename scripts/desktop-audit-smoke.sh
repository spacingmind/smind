#!/usr/bin/env bash
# Smoke-test a built audit app: launched WITHOUT the scratch SMIND_HOME and SMIND_DAEMON_URL it must
# exit with status 2 and the refusal message, and must create nothing in its HOME. A copy built
# without the audit guard would instead start (and be killed here after SMOKE_TIMEOUT seconds), so
# this fails for it. See docs/plans/active/desktop-audit-guard-wiring.md.
#
# Usage: scripts/desktop-audit-smoke.sh <path to smind-desktop>
# Env:   SMOKE_TIMEOUT  seconds to wait for the app to refuse (default 15)
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
mkdir "$work/home"
timeout=${SMOKE_TIMEOUT:-15}

env -i HOME="$work/home" PATH=/usr/bin:/bin "$bin" >"$work/stdout" 2>"$work/stderr" &
pid=$!
waited=0
while kill -0 "$pid" 2>/dev/null; do
  if [ "$waited" -ge $((timeout * 10)) ]; then
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
    echo "desktop-audit-smoke: FAIL: the app was still running after ${timeout}s: it carries no audit guard" >&2
    exit 1
  fi
  sleep 0.1
  waited=$((waited + 1))
done
code=0
wait "$pid" || code=$?

if [ "$code" -ne 2 ]; then
  echo "desktop-audit-smoke: FAIL: exit status $code, want 2" >&2
  exit 1
fi
if ! grep -q "smind audit build:" "$work/stderr"; then
  echo "desktop-audit-smoke: FAIL: no refusal message on stderr: $(cat "$work/stderr")" >&2
  exit 1
fi
if [ -n "$(ls -A "$work/home")" ]; then
  echo "desktop-audit-smoke: FAIL: the app created files in its HOME before refusing" >&2
  exit 1
fi
echo "desktop-audit-smoke: ok, the built app refuses to start without a scratch daemon"
