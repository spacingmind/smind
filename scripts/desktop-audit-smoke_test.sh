#!/bin/sh
# Tests scripts/desktop-audit-smoke.sh with fake "apps" (no build, no network): an app that refuses with
# status 2 and the message passes; one that runs on (no guard), exits 0, exits 2 without the message, or
# writes into its HOME fails; a missing binary is skipped cleanly; and a runaway fake is killed.
#
#   scripts/desktop-audit-smoke_test.sh
set -eu

here="$(cd "$(dirname "$0")" && pwd)"
smoke="$here/desktop-audit-smoke.sh"

work="$(mktemp -d 2>/dev/null || mktemp -d -t smind-audit-smoke-test)"
trap 'rm -rf "$work"' EXIT INT TERM

fail() {
	echo "FAIL: $1" >&2
	exit 1
}

# fake <name> <body>: an executable stand-in for the app.
fake() {
	printf '#!/bin/sh\n%s\n' "$2" >"$work/$1"
	chmod +x "$work/$1"
}

# passes-when-the-guard-refuses
fake good 'echo "smind audit build: SMIND_HOME is not set" >&2; exit 2'
bash "$smoke" "$work/good" >"$work/out" 2>&1 || fail "passes-when-the-guard-refuses: $(cat "$work/out")"
grep -q "ok, the built app refuses" "$work/out" || fail "no ok line: $(cat "$work/out")"

# fails-when-the-app-has-no-guard (it starts and keeps running; the smoke kills it)
# shellcheck disable=SC2016 # the fake's body is script text, expanded when the fake runs
fake runaway 'echo $$ >"$0.pid"; exec sleep 30'
started="$(date +%s)"
if SMOKE_TIMEOUT=1 bash "$smoke" "$work/runaway" >"$work/out" 2>&1; then fail "fails-when-the-app-has-no-guard: accepted a running app"; fi
[ "$(($(date +%s) - started))" -lt 10 ] || fail "the smoke waited for the runaway instead of killing it after its timeout"
grep -q "carries no audit guard" "$work/out" || fail "wrong failure for a running app: $(cat "$work/out")"
sleep 1
if kill -0 "$(cat "$work/runaway.pid")" 2>/dev/null; then fail "the runaway fake was not killed"; fi

# fails-on-exit-zero
fake exits0 'echo "smind audit build: x" >&2; exit 0'
if bash "$smoke" "$work/exits0" >"$work/out" 2>&1; then fail "fails-on-exit-zero: accepted exit 0"; fi

# fails-on-exit-two-without-the-message
fake nomsg 'echo "something else" >&2; exit 2'
if bash "$smoke" "$work/nomsg" >"$work/out" 2>&1; then fail "fails-on-exit-two-without-the-message: accepted"; fi
grep -q "no refusal message" "$work/out" || fail "wrong failure without the message: $(cat "$work/out")"

# fails-when-the-app-writes-before-refusing
# shellcheck disable=SC2016 # the fake's body is script text, expanded when the fake runs
fake writes 'touch "$HOME/created"; echo "smind audit build: x" >&2; exit 2'
if bash "$smoke" "$work/writes" >"$work/out" 2>&1; then fail "fails-when-the-app-writes-before-refusing: accepted"; fi
grep -q "created files" "$work/out" || fail "wrong failure for a writing app: $(cat "$work/out")"

# skips-cleanly-without-a-build
bash "$smoke" "$work/does-not-exist" >"$work/out" 2>&1 || fail "skips-cleanly-without-a-build: exit non-zero"
grep -q "skipped" "$work/out" || fail "no skip message: $(cat "$work/out")"
bash "$smoke" >"$work/out" 2>&1 || fail "skips-cleanly-without-a-build: no argument failed"

# the app is launched without the scratch variables even if the caller has them
# shellcheck disable=SC2016 # the fake's body is script text, expanded when the fake runs
fake env-probe 'if [ -n "${SMIND_HOME:-}${SMIND_DAEMON_URL:-}" ]; then echo "leaked" >&2; exit 0; fi; echo "smind audit build: x" >&2; exit 2'
SMIND_HOME=/tmp/x SMIND_DAEMON_URL=http://127.0.0.1:1 bash "$smoke" "$work/env-probe" >"$work/out" 2>&1 || fail "the smoke leaks the caller's SMIND_* into the app: $(cat "$work/out")"

echo "desktop-audit-smoke_test: ok"
