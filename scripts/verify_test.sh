#!/bin/sh
# Tests scripts/verify.sh with a fake task command: each step's output and
# exit code are scripted through $work/out-<step> files and FAKE_CODE_<step>
# env vars, so the cases cover the runner's own logic (stop after FAIL,
# continue past INCONCLUSIVE, overall exit mapping, the TMPDIR precheck)
# without running the real Taskfile. HOME and SMIND_HOME are pinned per run
# so an ambient TMPDIR under ~/.smind cannot trip the precheck in every case.
#
#	scripts/verify_test.sh
# shellcheck disable=SC2329  # the assertion helpers are invoked through ck "$@"
set -eu

here="$(cd "$(dirname "$0")" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

cat >"$work/faketask" <<'EOF'
#!/bin/sh
# $1 = step name (build|test|lint): prints $FAKE_HOME/out-<step> when present,
# exits with $FAKE_CODE_<step> (default 0), records the step in $FAKE_HOME/ran.
step=$1
printf '%s\n' "$step" >>"$FAKE_HOME/ran"
f="$FAKE_HOME/out-$step"
if [ -f "$f" ]; then
	cat "$f"
fi
case $step in
build) code=${FAKE_CODE_build:-0} ;;
test) code=${FAKE_CODE_test:-0} ;;
lint) code=${FAKE_CODE_lint:-0} ;;
esac
exit "$code"
EOF
chmod +x "$work/faketask"

fail=0
case_name=''

ck() { # ck <what> <command...>: record a failure instead of exiting
	what=$1
	shift
	if "$@"; then
		return 0
	fi
	echo "FAIL: $case_name: $what" >&2
	fail=1
}

eq() { [ "$1" = "$2" ]; }
has() { grep -qF -- "$2" "$1"; }
last_line() { [ "$(tail -n 1 "$1")" = "$2" ]; }
no_s() { [ ! -s "$1" ]; }
ran_eq() { # ran_eq <step>...: $work/ran lists exactly these steps, in order
	printf '%s\n' "$@" >"$work/ran-want"
	diff -q "$work/ran-want" "$work/ran" >/dev/null 2>&1
}

fake_build_code=0
fake_test_code=0
fake_lint_code=0

reset() {
	fake_build_code=0
	fake_test_code=0
	fake_lint_code=0
	rm -rf "${work:?}/ran" "${work:?}/out" "${work:?}/out-build" "${work:?}/out-test" \
		"${work:?}/out-lint" "${work:?}/home" "${work:?}/xhome" "${work:?}/stdout" \
		"${work:?}/stderr" "${work:?}/ran-want"
	mkdir -p "$work/tmp"
}

run() {
	code=0
	FAKE_HOME="$work" \
		FAKE_CODE_build="$fake_build_code" \
		FAKE_CODE_test="$fake_test_code" \
		FAKE_CODE_lint="$fake_lint_code" \
		VERIFY_TASK="$work/faketask" \
		VERIFY_OUT_DIR="$work/out" \
		TMPDIR="$work/tmp" \
		HOME="$work/xhome" \
		SMIND_HOME='' \
		sh "$here/verify.sh" >"$work/stdout" 2>"$work/stderr" || code=$?
}

case_all_pass() {
	case_name='all-pass'
	reset
	printf 'ok  \tpkg\t0.1s\n' >"$work/out-test"
	run
	ck "exit 0, got $code" test "$code" -eq 0
	ck "stdout has build: PASS" has "$work/stdout" "build: PASS"
	ck "stdout has test: PASS" has "$work/stdout" "test: PASS"
	ck "stdout has lint: PASS" has "$work/stdout" "lint: PASS"
	ck "last line is VERIFY: PASS" last_line "$work/stdout" "VERIFY: PASS"
	ck "ran build test lint" ran_eq build test lint
}

case_test_fails() {
	case_name='test-fails'
	reset
	printf '%s\n' '--- FAIL: TestX (0.01s)' >"$work/out-test"
	fake_test_code=1
	run
	ck "exit 1, got $code" test "$code" -eq 1
	ck "stdout has test: FAIL" has "$work/stdout" "test: FAIL"
	ck "stdout has lint: not run (test FAIL)" has "$work/stdout" "lint: not run (test FAIL)"
	ck "last line is VERIFY: FAIL" last_line "$work/stdout" "VERIFY: FAIL"
	ck "ran build test" ran_eq build test
}

case_test_inconclusive() {
	case_name='test-inconclusive'
	reset
	printf '%s\n' 'sh: bun: command not found' >"$work/out-test"
	fake_test_code=127
	run
	ck "exit 2, got $code" test "$code" -eq 2
	ck "stdout has test: INCONCLUSIVE (missing-tool" has "$work/stdout" "test: INCONCLUSIVE (missing-tool"
	ck "lint still ran" ran_eq build test lint
	ck "last line is VERIFY: INCONCLUSIVE" last_line "$work/stdout" "VERIFY: INCONCLUSIVE"
}

case_fail_beats_inconclusive() {
	case_name='fail-beats-inconclusive'
	reset
	printf '%s\n' 'sh: bun: command not found' >"$work/out-test"
	fake_test_code=127
	printf '%s\n' 'foo.go:3.1: bad' >"$work/out-lint"
	fake_lint_code=1
	run
	ck "exit 1, got $code" test "$code" -eq 1
	ck "last line is VERIFY: FAIL" last_line "$work/stdout" "VERIFY: FAIL"
}

case_timeout() {
	case_name='timeout'
	reset
	fake_test_code=124
	run
	ck "exit 2, got $code" test "$code" -eq 2
	ck "stdout has test: INCONCLUSIVE (timeout" has "$work/stdout" "test: INCONCLUSIVE (timeout"
}

case_tmpdir_in_daemon_home() {
	case_name='tmpdir-in-daemon-home'
	reset
	mkdir -p "$work/home/.smind/tmp"
	code=0
	FAKE_HOME="$work" \
		VERIFY_TASK="$work/faketask" \
		VERIFY_OUT_DIR="$work/out" \
		HOME="$work/home" \
		TMPDIR="$work/home/.smind/tmp" \
		SMIND_HOME='' \
		sh "$here/verify.sh" >"$work/stdout" 2>"$work/stderr" || code=$?
	ck "exit 2, got $code" test "$code" -eq 2
	ck "stdout has tmpdir-in-daemon-home" has "$work/stdout" "tmpdir-in-daemon-home"
	ck "no step ran" no_s "$work/ran"
}

case_shellcheck_skip() {
	case_name='shellcheck-skip'
	reset
	printf 'ok  \tpkg\t0.1s\n' >"$work/out-test"
	printf '%s\n' 'shellcheck not installed; skipping install script lint' >"$work/out-lint"
	run
	ck "exit 0, got $code" test "$code" -eq 0
	ck "stdout has lint: PASS (optional-skip" has "$work/stdout" "lint: PASS (optional-skip"
}

case_unrecognised_nonzero() {
	case_name='unrecognised-nonzero'
	reset
	printf '%s\n' 'something odd' >"$work/out-build"
	fake_build_code=1
	run
	ck "exit 1, got $code" test "$code" -eq 1
	ck "stdout has build: FAIL (unrecognised" has "$work/stdout" "build: FAIL (unrecognised"
	ck "ran build only" ran_eq build
}

case_tail() {
	case_name='tail'
	reset
	printf '%s\n' '--- FAIL: TestX (0.01s)' >"$work/out-test"
	fake_test_code=1
	run
	ck "stdout has --- last 40 lines of test ---" has "$work/stdout" "--- last 40 lines of test ---"
	ck "stdout has --- end ---" has "$work/stdout" "--- end ---"
}

case_all_pass
case_test_fails
case_test_inconclusive
case_fail_beats_inconclusive
case_timeout
case_tmpdir_in_daemon_home
case_shellcheck_skip
case_unrecognised_nonzero
case_tail

[ "$fail" -eq 0 ] && echo "ok: verify"
exit "$fail"
