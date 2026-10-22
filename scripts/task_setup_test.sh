#!/bin/sh
# Tests scripts/task-setup.sh with stub `bun` and `npm` on PATH (no network):
# both installs run from the right directories with lockfile-exact flags, a
# second run is harmless, a missing lockfile skips its directory, and a missing
# tool warns without failing.
#
#   scripts/task_setup_test.sh
set -eu

here="$(cd "$(dirname "$0")" && pwd)"
script="$here/task-setup.sh"

work="$(mktemp -d 2>/dev/null || mktemp -d -t smind-task-setup-test)"
trap 'rm -rf "$work"' EXIT INT TERM

stubs="$work/stubs"
mkdir -p "$stubs"
for tool in bun npm; do
	cat >"$stubs/$tool" <<'EOF2'
#!/bin/sh
echo "${0##*/} $* @ ${PWD##*/}" >>"$CALLS"
EOF2
	chmod +x "$stubs/$tool"
done

fail() {
	echo "FAIL: $1" >&2
	exit 1
}

# A tree with both lockfiles.
tree="$work/tree"
mkdir -p "$tree/web" "$tree/mobile"
: >"$tree/web/bun.lock"
: >"$tree/mobile/package-lock.json"
CALLS="$work/calls"
export CALLS

(cd "$tree" && PATH="$stubs:$PATH" sh "$script") >/dev/null 2>&1 || fail "run on a full tree failed"
grep -qx 'bun install --frozen-lockfile @ web' "$CALLS" || fail "bun install not run in web: $(cat "$CALLS")"
grep -qx 'npm ci @ mobile' "$CALLS" || fail "npm ci not run in mobile: $(cat "$CALLS")"

# Idempotent: a second run succeeds and runs the same commands again.
(cd "$tree" && PATH="$stubs:$PATH" sh "$script") >/dev/null 2>&1 || fail "second run failed"
[ "$(wc -l <"$CALLS" | tr -d ' ')" = 4 ] || fail "second run did not repeat both installs: $(cat "$CALLS")"

# A missing lockfile skips only that directory.
rm "$tree/mobile/package-lock.json"
: >"$CALLS"
(cd "$tree" && PATH="$stubs:$PATH" sh "$script") >/dev/null 2>&1 || fail "run without mobile lockfile failed"
[ "$(cat "$CALLS")" = 'bun install --frozen-lockfile @ web' ] || fail "mobile was not skipped: $(cat "$CALLS")"

# A missing tool warns and still exits 0. The script only needs shell
# builtins, so a PATH holding just the npm stub hides any real bun.
sh_bin="$(command -v sh)"
mkdir -p "$work/npm-only"
cp "$stubs/npm" "$work/npm-only/npm"
: >"$tree/mobile/package-lock.json"
: >"$CALLS"
out="$(cd "$tree" && PATH="$work/npm-only" "$sh_bin" "$script" 2>&1)" || fail "run without bun failed"
case "$out" in *"bun not on PATH"*) ;; *) fail "no warning for missing bun: $out" ;; esac
[ "$(cat "$CALLS")" = 'npm ci @ mobile' ] || fail "npm should still run: $(cat "$CALLS")"

echo "PASS: task-setup.sh"
