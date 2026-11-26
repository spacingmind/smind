#!/bin/sh
# Tests scripts/verify-classify.sh against fixtures in testdata/verify/:
# one named case per row of the classification table in
# docs/plans/active/e9-tri-state-verify.md, plus the exit-0, flaky and
# usage-error rules. Each case checks RESULT, CAUSE and the process exit
# code (0/1/2 for PASS/FAIL/INCONCLUSIVE).
#
#	scripts/verify-classify_test.sh
set -eu

here="$(cd "$(dirname "$0")" && pwd)"
classifier="$here/verify-classify.sh"
data="$here/testdata/verify"

fail=0

case_() { # name kind exit file want_result want_cause
	name=$1
	kind=$2
	code=$3
	file=$4
	want_result=$5
	want_cause=$6

	status=0
	out=$("$classifier" "$kind" "$code" "$data/$file" 2>/dev/null) || status=$?
	result=$(printf '%s\n' "$out" | sed -n 's/^RESULT=//p')
	cause=$(printf '%s\n' "$out" | sed -n 's/^CAUSE=//p')
	case $want_result in
	PASS) want_status=0 ;;
	FAIL) want_status=1 ;;
	INCONCLUSIVE) want_status=2 ;;
	esac

	if [ "$result" != "$want_result" ] || [ "$cause" != "$want_cause" ]; then
		echo "FAIL: $name: got $result/$cause, want $want_result/$want_cause" >&2
		fail=1
	fi
	if [ "$status" != "$want_status" ]; then
		echo "FAIL: $name: got exit $status, want $want_status" >&2
		fail=1
	fi
}

case_ row1-optional-skip lint 0 row1-shellcheck-skip.txt PASS optional-skip
case_ row1-echoed-command-is-not-a-skip lint 0 row1-skip-text-echoed-only.txt PASS ok
case_ row2-no-signal test 0 row2-no-signal.txt INCONCLUSIVE no-signal
case_ row3-no-tests-matched test 0 row3-no-tests-run.txt INCONCLUSIVE no-tests-matched
case_ row4-pass test 0 row4-pass.txt PASS ok
case_ row5-test-failed test 1 row5-test-failed.txt FAIL test-failed
case_ row5-build-error build 1 row5-build-error.txt FAIL build-error
case_ row5-type-error lint 2 row5-type-error.txt FAIL type-error
case_ row5-vitest-failed test 1 row5-vitest-failed.txt FAIL test-failed
case_ row5-import-error build 1 row5-import-error.txt FAIL build-error
case_ row5-test-timeout test 2 row5-test-timeout.txt FAIL test-failed
case_ row6-timeout test 124 row6-timeout.txt INCONCLUSIVE timeout
case_ row6-killed test 137 row6-timeout.txt INCONCLUSIVE killed
case_ row6-terminated test 143 row6-timeout.txt INCONCLUSIVE killed
case_ row7-missing-tool test 127 row7-missing-tool.txt INCONCLUSIVE missing-tool
case_ row7-missing-tool-go build 1 row7-missing-tool-go.txt INCONCLUSIVE missing-tool
case_ row8-deps-missing test 1 row8-deps-missing.txt INCONCLUSIVE deps-not-installed
case_ row8-module-not-found test 1 row8-module-not-found.txt INCONCLUSIVE deps-not-installed
case_ row9-disk-full build 1 row9-disk-full.txt INCONCLUSIVE disk-full
case_ row10-network build 1 row10-network.txt INCONCLUSIVE network
case_ row11-unrecognised lint 1 row11-unrecognised.txt FAIL unrecognised
case_ flaky-fails-stay-fail test 1 flaky-fail-plus-network.txt FAIL test-failed
case_ exit0-with-network-text-passes test 0 exit0-network-text.txt PASS ok

# The network row must point at the offending line, not just name the cause.
status=0
out=$("$classifier" build 1 "$data/row10-network.txt" 2>/dev/null) || status=$?
detail=$(printf '%s\n' "$out" | sed -n 's/^DETAIL=//p')
if [ "$status" -ne 2 ] || ! printf '%s\n' "$detail" | grep -q 'dial tcp'; then
	echo "FAIL: row10-network: DETAIL lacks 'dial tcp': $detail" >&2
	fail=1
fi

# Usage errors: bad kind, non-numeric exit code, missing file.
usage_case() { # name kind code file
	status=0
	"$classifier" "$2" "$3" "$4" >/dev/null 2>&1 || status=$?
	if [ "$status" -ne 64 ]; then
		echo "FAIL: usage-errors/$1: got exit $status, want 64" >&2
		fail=1
	fi
}
usage_case bad-kind bogus 1 "$data/row11-unrecognised.txt"
usage_case non-numeric-exit test x "$data/row11-unrecognised.txt"
usage_case missing-file test 1 "$data/no-such-file.txt"

[ "$fail" -eq 0 ] && echo "ok: verify-classify"
exit "$fail"
