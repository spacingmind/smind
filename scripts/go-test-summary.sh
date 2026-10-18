#!/usr/bin/env bash
# go-test-summary.sh <go-test-output.log>
#
# Turns plain `go test ./...` output into a short Markdown report (failing
# packages, failing tests, per-package timeouts) for $GITHUB_STEP_SUMMARY.
# Used by .github/workflows/windows-full-suite.yml; runs anywhere bash +
# grep/sed are available (Git Bash on windows-latest).
set -euo pipefail

log="${1:?usage: go-test-summary.sh <go-test-output.log> [go-test.json]}"
json="${2:-}"

ok=$(grep -cE '^ok[[:space:]]' "$log" || true)
failed=$(grep -cE '^FAIL[[:space:]]' "$log" || true)
# Test-level failures, de-duplicated (subtests are indented; keep them).
tests=$(grep -E '^[[:space:]]*--- FAIL: ' "$log" | sed -E 's/^[[:space:]]*--- FAIL: //; s/ \([0-9.]+s\)$//' | sort -u || true)
ntests=$(printf '%s' "$tests" | grep -c . || true)

echo "## Go test suite on $(uname -s 2>/dev/null || echo unknown)"
echo
echo "**${ok} packages ok, ${failed} failed, ${ntests} failing tests.**"

if [ "$failed" -gt 0 ]; then
  echo
  echo "### Failing packages"
  echo
  # "FAIL<TAB>pkg<TAB>304.335s" -> "- `pkg` (304.335s)"
  grep -E '^FAIL[[:space:]]+[^[:space:]]+' "$log" \
    | sed -E 's/^FAIL[[:space:]]+([^[:space:]]+)[[:space:]]*(.*)$/- `\1` \2/' || true
fi

# A per-package timeout prints "panic: test timed out after 5m0s", then
# "running tests:" and one tab-indented "TestName (4s)" line per test.
timeouts=$(grep -E '^panic: test timed out' -A8 "$log" | grep -E '^[[:space:]]+Test' | sed -E 's/^[[:space:]]+//' | sort -u || true)
if [ -n "$timeouts" ]; then
  echo
  echo "### Tests killed by the per-package timeout"
  echo
  printf '%s\n' "$timeouts" | sed 's/^/- `/; s/$/`/'
fi

if [ "$ntests" -gt 0 ]; then
  echo
  echo "### Failing tests (first 200)"
  echo
  printf '%s\n' "$tests" | head -200 | sed 's/^/- `/; s/$/`/'
fi

# With the `go test -json` stream (and jq): the slowest tests and packages, so a
# suite that is merely slow shows where the time goes before it times out.
if [ -n "$json" ] && [ -s "$json" ] && command -v jq >/dev/null 2>&1; then
  echo
  echo "### Slowest packages"
  echo
  jq -r 'select((.Action=="pass" or .Action=="fail") and (.Test == null) and .Elapsed != null) | "\(.Elapsed)\t\(.Package)"' "$json" 2>/dev/null \
    | sort -rn | head -12 | awk -F'\t' '{printf "- `%s` %.1fs\n", $2, $1}' || true
  echo
  echo "### Slowest tests"
  echo
  jq -r 'select((.Action=="pass" or .Action=="fail") and .Test != null and (.Test | contains("/") | not) and .Elapsed != null) | "\(.Elapsed)\t\(.Package)\t\(.Test)"' "$json" 2>/dev/null \
    | sort -rn | head -25 | awk -F'\t' '{n=split($2,p,"/"); printf "- `%s` (%s) %.1fs\n", $3, p[n], $1}' || true
fi
