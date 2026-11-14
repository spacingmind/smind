#!/bin/sh
# Tests scripts/desktop-audit-build.sh in DRY_RUN mode (no build, no network, temp HOME): the generated
# override carries the derived identity and leaves the committed config alone, a bad AUDIT_ID or an
# out-dir on the real app-data, WebKit, log, /Applications or ~/.smind path is refused with nothing
# written, and DRY_RUN stops before any build.
#
#   scripts/desktop-audit-build_test.sh
set -eu

here="$(cd "$(dirname "$0")" && pwd)"
script="$here/desktop-audit-build.sh"
repo="$(cd "$here/.." && pwd)"
real="$(jq -r .identifier "$repo/desktop/src-tauri/tauri.conf.json")"

work="$(mktemp -d 2>/dev/null || mktemp -d -t smind-audit-build-test)"
trap 'rm -rf "$work"' EXIT INT TERM
home="$work/home"
mkdir -p "$home"
before="$(git -C "$repo" status --porcelain -- desktop/src-tauri/tauri.conf.json)"

fail() {
	echo "FAIL: $1" >&2
	exit 1
}

# run <out-dir> [VAR=value ...]: runs the script in dry-run mode under the temp HOME.
run() {
	out="$1"
	shift
	(cd "$repo" && env HOME="$home" DRY_RUN=1 "$@" bash "$script" "$out") >"$work/stdout" 2>"$work/stderr"
}

# refused <name> <out-dir> [VAR=value ...]: must exit non-zero and leave the out-dir absent.
refused() {
	name="$1"
	out="$2"
	shift 2
	if run "$out" "$@"; then
		fail "$name: the script accepted it"
	fi
	[ ! -e "$out/tauri.audit.conf.json" ] || fail "$name: wrote an override despite refusing"
	grep -q "desktop-audit-build:" "$work/stderr" || fail "$name: no refusal message: $(cat "$work/stderr")"
}

# override-has-derived-identity
out="$work/out"
run "$out" || fail "override-has-derived-identity: dry run failed: $(cat "$work/stderr")"
conf="$out/tauri.audit.conf.json"
[ -f "$conf" ] || fail "override-has-derived-identity: no override written"
[ "$(jq -r .identifier "$conf")" = "$real.audit" ] || fail "identifier is $(jq -r .identifier "$conf"), want $real.audit"
[ "$(jq -r .identifier "$conf")" != "$real" ] || fail "derived identifier equals the real one"
[ "$(jq -r .productName "$conf")" = "sMind Audit" ] || fail "product name"
[ "$(jq -r '.plugins["deep-link"].desktop.schemes[0]' "$conf")" = "smind-audit" ] || fail "deep-link scheme"
jq -r '.plugins.updater.endpoints[0]' "$conf" | grep -q '^https://127.0.0.1:9/' || fail "updater endpoint is not the dead one"
grep -q "DRY_RUN: stopping before any build" "$work/stdout" || fail "dry run did not stop before the build"

# a second id gives a second, different identity
run "$out" AUDIT_ID=second || fail "AUDIT_ID=second refused"
[ "$(jq -r .identifier "$out/tauri.second.conf.json")" = "$real.second" ] || fail "second identifier"

# committed-config-untouched
[ "$(git -C "$repo" status --porcelain -- desktop/src-tauri/tauri.conf.json)" = "$before" ] || fail "committed tauri.conf.json changed"

# refuses-bad-audit-id
long="abcdefghijklmnopqrstuvwxyz"
for bad in "" Audit a.b ../x a/b 1abc "a b" "$long"; do
	refused "refuses-bad-audit-id ($bad)" "$work/bad" AUDIT_ID="$bad"
done

# real app-data, WebKit, cache, log, /Applications and ~/.smind
refused "refuses-real-app-data-out-dir" "$home/Library/Application Support/$real"
refused "refuses-under-real-app-data" "$home/Library/Application Support/$real/managed-daemon/x"
refused "refuses-real-webkit" "$home/Library/WebKit/$real"
refused "refuses-real-logs" "$home/Library/Logs/$real/audit"
refused "refuses-real-caches" "$home/Library/Caches/$real"
# the applications directory is overridden so a broken guard can never write into the real /Applications
mkdir -p "$work/Applications"
refused "refuses-applications" "$work/Applications/audit" AUDIT_APPLICATIONS_DIR="$work/Applications"
refused "refuses-real-smind-home" "$home/.smind"
refused "refuses-under-real-smind-home" "$home/.smind/worktrees/x"

# a path that merely resembles them is fine, and so is the default scratch place under ~/.smind/dogfood
run "$home/Library/Application Support/${real}.audit" || fail "an app-data sibling with another identifier was refused"
run "$home/.smind/dogfood/desktop-audit" || fail "the default scratch out-dir was refused: $(cat "$work/stderr")"

# a symlink onto the real app-data directory is refused too
mkdir -p "$home/Library/Application Support/$real"
ln -s "$home/Library/Application Support/$real" "$work/link"
refused "refuses-symlink-to-real-app-data" "$work/link"

# build-args-carry-the-guard-feature: the dry run prints the exact tauri command line the real build runs
run "$work/args" || fail "build-args-carry-the-guard-feature: dry run failed"
args="$(grep '^tauri args:' "$work/stdout" || true)"
[ -n "$args" ] || fail "build-args-carry-the-guard-feature: the dry run printed no tauri args"
case "$args" in *" --features audit-guard "*) ;; *) fail "build-args-carry-the-guard-feature: no --features audit-guard in: $args" ;; esac
case "$args" in *" --bundles app "*) ;; *) fail "tauri args lack --bundles app: $args" ;; esac
case "$args" in *" --config "*"/args/tauri.audit.conf.json") ;; *) fail "tauri args lack the override --config: $args" ;; esac
# shellcheck disable=SC2016 # the pattern is the literal source text, not an expansion
grep -q 'npx tauri "${tauri_args\[@\]}"' "$script" || fail "the real build does not run the printed tauri args"
grep -q 'desktop-audit-smoke.sh' "$script" || fail "the build does not run the smoke check on the built app"

# the guard stays out of every release path: not a default feature, and nothing but the audit script passes it
grep -q '^default = \[\]$' "$repo/desktop/src-tauri/Cargo.toml" || fail "audit-guard must not be a default feature: Cargo.toml lacks default = []"
if grep -rn 'audit-guard' "$repo/.github" "$repo/Taskfile.yml" "$repo/desktop/src-tauri/tauri.conf.json" "$repo/desktop/src-tauri/tauri.macos.conf.json" "$repo/desktop/package.json" >/dev/null 2>&1; then
	fail "a workflow, Taskfile target or tauri config mentions audit-guard: the release build must not enable it"
fi

echo "desktop-audit-build_test: ok"
