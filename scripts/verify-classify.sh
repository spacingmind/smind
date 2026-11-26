#!/bin/sh
# Classifies one verification step's combined output into PASS / FAIL /
# INCONCLUSIVE per docs/plans/active/e9-tri-state-verify.md: the first
# matching row of the classification table wins (rows 5 before 6 before
# 7..10), and an unrecognised non-zero exit is FAIL (the classifier fails
# closed).
#
#	scripts/verify-classify.sh <kind> <exit-code> <output-file>
#
# kind is build, test or lint. Prints RESULT=, CAUSE=, DETAIL= (the first
# line of the output matching the row's pattern, at most 160 chars, empty
# when the row has no pattern) and exits 0 (PASS), 1 (FAIL), 2
# (INCONCLUSIVE) or 64 (usage error).
set -eu

usage() {
	echo "usage: scripts/verify-classify.sh <build|test|lint> <exit-code> <output-file>" >&2
}

[ "$#" -eq 3 ] || { usage; exit 64; }
kind=$1
code=$2
file=$3

case $kind in
build | test | lint) ;;
*) usage; exit 64 ;;
esac

case $code in
'' | *[!0-9]*) usage; exit 64 ;;
esac

if [ ! -f "$file" ] || [ ! -r "$file" ]; then
	usage
	exit 64
fi

match() { grep -Eq -- "$1" "$file"; }

emit() { # RESULT CAUSE PATTERN (empty pattern -> empty DETAIL)
	detail=''
	if [ -n "$3" ]; then
		detail=$(grep -E -m1 -- "$3" "$file" | cut -c1-160)
	fi
	echo "RESULT=$1"
	echo "CAUSE=$2"
	echo "DETAIL=$detail"
	case $1 in
	PASS) exit 0 ;;
	FAIL) exit 1 ;;
	INCONCLUSIVE) exit 2 ;;
	esac
}

row1='shellcheck not installed; skipping'
row2='^ok|Tests .* passed|PASS'
row3='testing: warning: no tests to run'
row5='^--- FAIL: |^FAIL[[:space:]]+[^[:space:]]+[[:space:]]+[0-9.]+s|\[build failed\]|^panic: |\.go:[0-9]+:[0-9]+:|error TS[0-9]+|^[[:space:]]*FAIL[[:space:]]|Test Files[[:space:]]+[0-9]+ failed|Tests[[:space:]]+[0-9]+ failed|no required module provides package|is not in std'
row5_build='\[build failed\]|no required module provides package|is not in std'
row5_golc='\.go:[0-9]+:[0-9]+:'
row5_ts='error TS[0-9]+'
row5_deffail='^--- FAIL: '
# The backslash escapes $ in the ERE on purpose: the output says "$PATH".
# shellcheck disable=SC2016
row7='command not found|executable file not found in \$PATH'
row8="Cannot find module|ERR_MODULE_NOT_FOUND|error: Cannot find package|Cannot find package '"
row9='no space left on device|ENOSPC'
row10='dial tcp|no such host|Temporary failure in name resolution|i/o timeout|connection refused|TLS handshake timeout|ENOTFOUND|ETIMEDOUT|ECONNRESET|getaddrinfo'

# Row 1: exit 0 and an optional tool the Taskfile itself skips.
if [ "$code" -eq 0 ] && match "$row1"; then
	emit PASS optional-skip "$row1"
fi

# Rows 2 and 3 apply only to a test step that exited 0.
if [ "$code" -eq 0 ] && [ "$kind" = test ]; then
	if ! match "$row2"; then
		emit INCONCLUSIVE no-signal ''
	fi
	ok_lines=$(grep -Ec '^ok' "$file" || true)
	ok_marked=$(grep -Ec '^ok.*\[no tests to run\]' "$file" || true)
	if { [ "$ok_lines" -gt 0 ] && [ "$ok_lines" -eq "$ok_marked" ]; } || match "$row3"; then
		emit INCONCLUSIVE no-tests-matched "$row3"
	fi
fi

# Row 4: exit 0 otherwise.
if [ "$code" -eq 0 ]; then
	emit PASS ok ''
fi

# Row 5: a FAIL marker beats every environment pattern below.
if match "$row5"; then
	if match "$row5_build"; then
		emit FAIL build-error "$row5_build"
	elif match "$row5_golc" && ! match "$row5_deffail"; then
		emit FAIL build-error "$row5_golc"
	elif match "$row5_ts"; then
		emit FAIL type-error "$row5_ts"
	else
		emit FAIL test-failed "$row5"
	fi
fi

# Row 6: the runner's own wall-clock limit or a kill signal.
case $code in
124) emit INCONCLUSIVE timeout '' ;;
137 | 143) emit INCONCLUSIVE killed '' ;;
esac

# Rows 7-10: the step could not run (environment), recognised positively.
if match "$row7"; then
	emit INCONCLUSIVE missing-tool "$row7"
fi
if match "$row8"; then
	emit INCONCLUSIVE deps-not-installed "$row8"
fi
if match "$row9"; then
	emit INCONCLUSIVE disk-full "$row9"
fi
if match "$row10"; then
	emit INCONCLUSIVE network "$row10"
fi

# Row 11: nothing above matched; fail closed.
emit FAIL unrecognised ''
