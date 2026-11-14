#!/bin/sh
# Tests scripts/desktop-audit-smoke.sh with fake "apps" (no build, no network). The fakes record the
# environment of every launch, so the test pins what the smoke gives the app: launch 1 has the discard
# URL and no SMIND_HOME (a guard-less build can then never reach the real daemon), launch 2 has a scratch
# SMIND_HOME and no URL and is made only after launch 1 passed.
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

# fake <name> <body>: an executable stand-in for the app; every launch appends its environment to <name>.log.
fake() {
	# shellcheck disable=SC2016 # $0 and the body expand when the fake runs, not here
	printf '#!/bin/sh\n{ echo "--- launch"; env; } >>"$0.log"\n%s\n' "$2" >"$work/$1"
	chmod +x "$work/$1"
}

calls() { grep -c '^--- launch$' "$work/$1.log" 2>/dev/null || echo 0; }

# launch <name> <n>: the environment recorded for the n-th launch.
launch_env() { awk -v n="$2" '/^--- launch$/ {i++} i==n' "$work/$1.log"; }

# a faithful guard: SMIND_HOME first, then SMIND_DAEMON_URL, then it would run on
# shellcheck disable=SC2016 # the fake's body is script text, expanded when the fake runs
fake good 'if [ -z "${SMIND_HOME:-}" ]; then echo "smind audit build: SMIND_HOME is not set" >&2; exit 2; fi
if [ -z "${SMIND_DAEMON_URL:-}" ]; then echo "smind audit build: SMIND_DAEMON_URL is not set" >&2; exit 2; fi
exec sleep 30'

# passes-when-the-guard-refuses, and the smoke gives the app exactly the environment it promises
SMIND_HOME=/tmp/callers-home SMIND_DAEMON_URL=http://127.0.0.1:4648 bash "$smoke" "$work/good" >"$work/out" 2>&1 || fail "passes-when-the-guard-refuses: $(cat "$work/out")"
grep -q "ok, the built app refuses" "$work/out" || fail "no ok line: $(cat "$work/out")"
[ "$(calls good)" = 2 ] || fail "want 2 launches, got $(calls good)"
first="$(launch_env good 1)"
second="$(launch_env good 2)"
echo "$first" | grep -qx 'SMIND_DAEMON_URL=http://127.0.0.1:9' || fail "launch 1 must carry the discard URL: $first"
if echo "$first" | grep -q '^SMIND_HOME='; then fail "launch 1 must have no SMIND_HOME (the caller's leaked): $first"; fi
echo "$second" | grep -q '^SMIND_HOME=.*scratch-smind' || fail "launch 2 must carry a scratch SMIND_HOME: $second"
if echo "$second" | grep -q '^SMIND_DAEMON_URL='; then fail "launch 2 must have no SMIND_DAEMON_URL (the caller's leaked): $second"; fi
for e in "$first" "$second"; do
	echo "$e" | grep -q '^HOME=.*/home-' || fail "each launch needs a scratch HOME: $e"
	if echo "$e" | grep -v '^\(HOME\|PATH\|SMIND_HOME\|SMIND_DAEMON_URL\|PWD\|SHLVL\|_\|OLDPWD\|--- launch\)=' | grep -q '^[A-Za-z_]*='; then fail "unexpected variables leaked into the app: $e"; fi
done
real_home="$(echo "$first$second" | grep -c '/tmp/callers-home' || true)"
[ "$real_home" = 0 ] || fail "the caller's SMIND_HOME reached the app"

# a guard-less app: starts and keeps running. Launch 1 times out, launch 2 is never made, and the runaway is killed.
# shellcheck disable=SC2016 # the fake's body is script text, expanded when the fake runs
fake runaway 'echo $$ >"$0.pid"; exec sleep 30'
started="$(date +%s)"
if SMOKE_TIMEOUT=1 bash "$smoke" "$work/runaway" >"$work/out" 2>&1; then fail "fails-when-the-app-has-no-guard: accepted a running app"; fi
grep -q "carries no audit guard" "$work/out" || fail "wrong failure for a running app: $(cat "$work/out")"
[ "$(($(date +%s) - started))" -lt 10 ] || fail "the smoke waited for the runaway instead of killing it after its timeout"
[ "$(calls runaway)" = 1 ] || fail "second-launch-only-after-the-first-passed: a guard-less app was launched $(calls runaway) times"
if launch_env runaway 1 | grep -q '^SMIND_HOME='; then fail "the guard-less launch had a SMIND_HOME"; fi
launch_env runaway 1 | grep -qx 'SMIND_DAEMON_URL=http://127.0.0.1:9' || fail "the guard-less launch must only be offered the discard URL"
sleep 1
if kill -0 "$(cat "$work/runaway.pid")" 2>/dev/null; then fail "the runaway fake was not killed"; fi

# fails-when-the-guard-skips-the-url-check: launch 1 refuses, launch 2 runs on
# shellcheck disable=SC2016 # the fake's body is script text, expanded when the fake runs
fake nourl 'if [ -z "${SMIND_HOME:-}" ]; then echo "smind audit build: SMIND_HOME is not set" >&2; exit 2; fi
exec sleep 30'
if SMOKE_TIMEOUT_SHORT=1 bash "$smoke" "$work/nourl" >"$work/out" 2>&1; then fail "fails-when-the-guard-skips-the-url-check: accepted"; fi
grep -q "no-url: the app was still running" "$work/out" || fail "wrong failure for a missing URL check: $(cat "$work/out")"
[ "$(calls nourl)" = 2 ] || fail "nourl should have been launched twice"

# fails-on-exit-zero
fake exits0 'echo "smind audit build: SMIND_HOME SMIND_DAEMON_URL" >&2; exit 0'
if bash "$smoke" "$work/exits0" >"$work/out" 2>&1; then fail "fails-on-exit-zero: accepted exit 0"; fi
grep -q "exit status 0, want 2" "$work/out" || fail "wrong failure for exit 0: $(cat "$work/out")"

# fails-on-exit-two-without-the-message
fake nomsg 'echo "something else" >&2; exit 2'
if bash "$smoke" "$work/nomsg" >"$work/out" 2>&1; then fail "fails-on-exit-two-without-the-message: accepted"; fi
grep -q "no refusal message" "$work/out" || fail "wrong failure without the message: $(cat "$work/out")"

# fails-when-the-refusal-names-another-variable
fake wrongname 'echo "smind audit build: SOMETHING_ELSE is not set" >&2; exit 2'
if bash "$smoke" "$work/wrongname" >"$work/out" 2>&1; then fail "fails-when-the-refusal-names-another-variable: accepted"; fi
grep -q "does not name SMIND_HOME" "$work/out" || fail "wrong failure for a message naming another variable: $(cat "$work/out")"

# fails-when-the-app-writes-before-refusing
# shellcheck disable=SC2016 # the fake's body is script text, expanded when the fake runs
fake writes 'touch "$HOME/created"; echo "smind audit build: SMIND_HOME" >&2; exit 2'
if bash "$smoke" "$work/writes" >"$work/out" 2>&1; then fail "fails-when-the-app-writes-before-refusing: accepted"; fi
grep -q "created files" "$work/out" || fail "wrong failure for a writing app: $(cat "$work/out")"

# skips-cleanly-without-a-build
bash "$smoke" "$work/does-not-exist" >"$work/out" 2>&1 || fail "skips-cleanly-without-a-build: exit non-zero"
grep -q "skipped" "$work/out" || fail "no skip message: $(cat "$work/out")"
bash "$smoke" >"$work/out" 2>&1 || fail "skips-cleanly-without-a-build: no argument failed"

echo "desktop-audit-smoke_test: ok"
