#!/usr/bin/env bash
# Build an isolated, unsigned macOS copy of the desktop app for a real-window audit.
#
# The committed tauri.conf.json is not edited. A generated override gives the copy its
# own bundle identifier (so its app-data directory, single-instance lock and settings are
# separate from an installed sMind), its own product name and deep-link scheme (so it never
# claims `smind://`), and an updater endpoint that cannot be reached (so it never talks to
# the release feed). Point it at a scratch daemon when you launch it:
#
#   SMIND_HOME=<scratch home> SMIND_DAEMON_URL=http://127.0.0.1:<port> \
#     "<out>/sMind Audit.app/Contents/MacOS/smind-desktop"
#
# Usage: scripts/desktop-audit-build.sh [out-dir]   (default: $HOME/.smind/dogfood/desktop-audit)
# Env:   AUDIT_ID  short lowercase word for the identifier suffix, scheme and product name (default: audit)
#        CARGO_TARGET_DIR  share a Rust build cache
#        DRY_RUN=1 write the override, print the derived identifier and stop before any build
#
# It refuses an AUDIT_ID that could collide with the real identifier or escape a path, and an
# out-dir that is, or lies under, the real app-data, WebKit, cache or log directory, /Applications
# or the real ~/.smind. See docs/plans/completed/desktop-audit-build.md.
set -euo pipefail

die() { echo "desktop-audit-build: $*" >&2; exit 1; }

root=$(git rev-parse --show-toplevel)
cd "$root"
id=${AUDIT_ID-audit}
out=${1:-$HOME/.smind/dogfood/desktop-audit}
real=$(jq -r .identifier desktop/src-tauri/tauri.conf.json)
if [ -z "$real" ] || [ "$real" = null ]; then
  die "cannot read the real bundle identifier from tauri.conf.json"
fi

printf '%s' "$id" | LC_ALL=C grep -Eq '^[a-z][a-z0-9-]{0,23}$' ||
  die "AUDIT_ID must be 1 to 24 characters, a lowercase letter then a-z, 0-9 or '-', got '$id'"
identifier="$real.$id"
[ "$identifier" != "$real" ] || die "derived identifier equals the real one"

# canon prints an absolute path with symlinks resolved, for a path that may not exist yet.
canon() {
  local p=$1 tail=""
  while [ ! -d "$p" ]; do
    tail="/$(basename "$p")$tail"
    p=$(dirname "$p")
  done
  echo "$(cd "$p" && pwd -P)$tail"
}
out=$(canon "$out")
home=$(canon "$HOME")
apps=$(canon "${AUDIT_APPLICATIONS_DIR:-/Applications}") # overridable only so the test never has to aim at the real one
for bad in \
  "$home/Library/Application Support/$real" "$home/Library/WebKit/$real" \
  "$home/Library/Caches/$real" "$home/Library/Logs/$real" \
  "$home/.smind" "$apps"; do
  case "$out/" in
    "$bad"/*)
      # the default out-dir lives under ~/.smind/dogfood, which is scratch, not the daemon home
      case "$out/" in "$home/.smind/dogfood/"*) continue ;; esac
      die "refusing out-dir '$out': it is, or is under, '$bad'" ;;
  esac
done

mkdir -p "$out"
conf="$out/tauri.$id.conf.json"
cat >"$conf" <<JSON
{
  "productName": "sMind Audit",
  "identifier": "$identifier",
  "plugins": {
    "updater": { "endpoints": ["https://127.0.0.1:9/latest.json"] },
    "deep-link": { "desktop": { "schemes": ["smind-$id"] } }
  }
}
JSON
echo "override: $conf"
echo "identifier: $identifier"

# The build target and the one tauri command line, fixed before the dry-run exit so a test can assert
# on exactly what the real build runs. `audit-guard` compiles the launch guard in (see
# docs/plans/completed/desktop-audit-app-guard.md); only this script ever passes it.
case $(uname -m) in
  arm64 | aarch64) target=aarch64-apple-darwin goarch=arm64 ;;
  x86_64) target=x86_64-apple-darwin goarch=amd64 ;;
  *) die "unsupported architecture $(uname -m)" ;;
esac
tauri_args=(build --target "$target" --bundles app --features audit-guard --config "$conf")
echo "tauri args: ${tauri_args[*]}"

if [ "${DRY_RUN:-}" = 1 ]; then
  echo "DRY_RUN: stopping before any build"
  exit 0
fi

[ "$(uname -s)" = Darwin ] || die "the app build needs macOS (use DRY_RUN=1 to check the override only)"

version=$(jq -r .version desktop/src-tauri/tauri.conf.json)
short=$(git rev-parse --short HEAD)

task build:web
mkdir -p desktop/src-tauri/binaries
echo "building the bundled daemon $version ($short) for $target"
CGO_ENABLED=0 GOOS=darwin GOARCH=$goarch go build -buildvcs=false \
  -ldflags "-X github.com/spacingmind/smind/internal/version.Version=$version -X github.com/spacingmind/smind/internal/version.Commit=$short" \
  -o "desktop/src-tauri/binaries/smind-$target" ./cmd/smind

(cd desktop && npm ci && npx tauri "${tauri_args[@]}")

# CARGO_TARGET_DIR (if set) is honoured by cargo and by the copy below, so a cache can be shared.
app="${CARGO_TARGET_DIR:-desktop/src-tauri/target}/$target/release/bundle/macos/sMind Audit.app"
rm -rf "$out/sMind Audit.app"
ditto "$app" "$out/sMind Audit.app"
echo "built: $out/sMind Audit.app"

# The built app must really carry the guard: launched without the scratch environment it has to refuse.
"$(dirname "$0")/desktop-audit-smoke.sh" "$out/sMind Audit.app/Contents/MacOS/smind-desktop"
