#!/bin/sh
# Tests the changed-file filter of .github/workflows/mobile-check.yml: it reads the
# regular expression out of the workflow itself (so the test cannot drift from it) and
# feeds it sample changed-file lists. A path that reaches the mobile client over the
# wire must run the live interop suites; unrelated paths must not (the required check
# then reports success at once).
#
#   scripts/mobile-check-filter_test.sh
set -eu

here="$(cd "$(dirname "$0")" && pwd)"
wf="$here/../.github/workflows/mobile-check.yml"

# The single grep -qE line of the "Detect" step; its quoted argument is the filter.
re="$(sed -n "s/^[[:space:]]*grep -qE '\(.*\)'; then[[:space:]]*$/\1/p" "$wf")"
if [ -z "$re" ] || [ "$(printf '%s\n' "$re" | wc -l)" -ne 1 ]; then
	echo "FAIL: could not read exactly one filter from $wf" >&2
	exit 1
fi

fail=0
check() { # expected(yes|no) path
	if printf '%s\n' "$2" | grep -qE "$re"; then got=yes; else got=no; fi
	if [ "$got" != "$1" ]; then
		echo "FAIL: $2 -> $got, want $1" >&2
		fail=1
	fi
}

for p in mobile/src/relay/RelayConnection.ts mobile/package.json internal/relay/bridge/hub.go \
	internal/wsapi/rpc.go internal/push/seal.go web/packages/relay-client/src/e2ee.ts web/packages/relay-client/package.json web/packages/relay-web/src/controller.ts web/packages/relay-web/package.json .github/workflows/mobile-check.yml; do
	check yes "$p"
done
for p in docs/plans/active/a.md web/packages/ui/src/a.tsx web/packages/relay-clientx/a.ts web/packages/relay-webx/a.ts web/package.json internal/store/runs.go internal/inbox/inbox.go \
	internal/wsapiextra/a.go internal/pushnotes/a.go mobilex/a.ts .github/workflows/ci.yml README.md; do
	check no "$p"
done

[ "$fail" -eq 0 ] && echo "ok: mobile-check filter"
exit "$fail"
