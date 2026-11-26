#!/bin/sh
# Runs the repo's verification steps (task build, task test, task lint) and
# reports PASS / FAIL / INCONCLUSIVE per docs/plans/active/e9-tri-state-verify.md:
# each step's combined output is classified by scripts/verify-classify.sh, a
# FAIL stops the later steps, an INCONCLUSIVE step does not, and the overall
# exit is 0 (PASS), 1 (FAIL) or 2 (INCONCLUSIVE), FAIL beating INCONCLUSIVE.
#
#	scripts/verify.sh
#
# Overridable: VERIFY_TASK (the task command), VERIFY_TIMEOUT (seconds per
# step, default 1800), VERIFY_OUT_DIR (output directory, default a fresh one
# under $TMPDIR).
set -u

here="$(cd "$(dirname "$0")" && pwd)"
classifier="$here/verify-classify.sh"
task_cmd="${VERIFY_TASK:-task}"
timeout_s="${VERIFY_TIMEOUT:-1800}"

resolve() { # print the physical path of $1, or $1 itself when it cannot be entered
	r=$(cd "$1" 2>/dev/null && pwd -P)
	if [ -n "$r" ]; then
		printf '%s\n' "$r"
	else
		printf '%s\n' "$1"
	fi
}

inside() { # 0 when $1 is $2 or somewhere under it
	case $1 in
	"$2") return 0 ;;
	"$2"/*) return 0 ;;
	esac
	return 1
}

# Precheck: a TMPDIR inside the daemon home collides with the daemon's files.
tmp="${TMPDIR:-/tmp}"
tmp_res=$(resolve "$tmp")
if [ -n "${HOME:-}" ] && [ -d "$HOME/.smind" ] && inside "$tmp_res" "$(resolve "$HOME/.smind")"; then
	echo "TMPDIR is inside the daemon home; set TMPDIR to a directory outside ~/.smind and re-run"
	echo "VERIFY: INCONCLUSIVE (tmpdir-in-daemon-home)"
	exit 2
fi
if [ -n "${SMIND_HOME:-}" ] && [ -d "$SMIND_HOME" ] && inside "$tmp_res" "$(resolve "$SMIND_HOME")"; then
	echo "TMPDIR is inside the daemon home; set TMPDIR to a directory outside ~/.smind and re-run"
	echo "VERIFY: INCONCLUSIVE (tmpdir-in-daemon-home)"
	exit 2
fi

out="${VERIFY_OUT_DIR:-}"
if [ -z "$out" ]; then
	out=$(mktemp -d "${TMPDIR:-/tmp}/verify.XXXXXX") || {
		echo "VERIFY: INCONCLUSIVE (cannot create output directory)"
		exit 2
	}
fi
mkdir -p "$out" || {
	echo "VERIFY: INCONCLUSIVE (cannot create output directory $out)"
	exit 2
}
echo "output: $out"

tw=''
if command -v timeout >/dev/null 2>&1; then
	tw="timeout $timeout_s"
elif command -v gtimeout >/dev/null 2>&1; then
	tw="gtimeout $timeout_s"
else
	echo "note: no timeout(1); steps run without a time limit"
fi

fail=0
inconc=0
failed_step=''

for step in build test lint; do
	if [ "$fail" -eq 1 ]; then
		echo "$step: not run ($failed_step FAIL)"
		continue
	fi

	# shellcheck disable=SC2086
	$tw "$task_cmd" "$step" >"$out/$step.log" 2>&1
	code=$?

	cls=$(sh "$classifier" "$step" "$code" "$out/$step.log")
	cls_code=$?
	if [ "$cls_code" -eq 64 ]; then
		echo "VERIFY: FAIL (classifier usage error)"
		exit 1
	fi
	result=$(printf '%s\n' "$cls" | sed -n 's/^RESULT=//p')
	cause=$(printf '%s\n' "$cls" | sed -n 's/^CAUSE=//p')
	detail=$(printf '%s\n' "$cls" | sed -n 's/^DETAIL=//p')

	case $result in
	PASS)
		if [ "$cause" = optional-skip ]; then
			echo "$step: PASS ($cause: $detail)"
		else
			echo "$step: PASS"
		fi
		;;
	INCONCLUSIVE)
		echo "$step: INCONCLUSIVE ($cause: $detail)"
		inconc=1
		;;
	*)
		echo "$step: FAIL ($cause: $detail)"
		fail=1
		failed_step=$step
		;;
	esac
	if [ "$result" = FAIL ] || [ "$result" = INCONCLUSIVE ]; then
		echo "--- last 40 lines of $step ---"
		tail -n 40 "$out/$step.log"
		echo "--- end ---"
	fi
done

if [ "$fail" -eq 1 ]; then
	echo "VERIFY: FAIL"
	exit 1
fi
if [ "$inconc" -eq 1 ]; then
	echo "VERIFY: INCONCLUSIVE"
	exit 2
fi
echo "VERIFY: PASS"
exit 0
