#!/usr/bin/env bash
# Fails if any of the four desktop version fields drifts from
# .release-please-manifest.json. release-please-config.json's extra-files
# bumps all four in lockstep with the manifest on every release PR; this
# guards against a manual edit (or a config regression) desyncing them.
set -euo pipefail

manifest_version="$(jq -r '.["."]' .release-please-manifest.json)"
status=0

check() {
  local label="$1" actual="$2"
  if [ "$actual" != "$manifest_version" ]; then
    echo "::error::$label version ($actual) does not match .release-please-manifest.json ($manifest_version)" >&2
    status=1
  fi
}

check "desktop/package.json" "$(jq -r '.version' desktop/package.json)"
check "desktop/src-tauri/tauri.conf.json" "$(jq -r '.version' desktop/src-tauri/tauri.conf.json)"
check "desktop/src-tauri/Cargo.toml" "$(grep -m1 '^version = ' desktop/src-tauri/Cargo.toml | sed -E 's/^version = "([^"]*)".*/\1/')"
check "desktop/daemon-client/Cargo.toml" "$(grep -m1 '^version = ' desktop/daemon-client/Cargo.toml | sed -E 's/^version = "([^"]*)".*/\1/')"

if [ "$status" -eq 0 ]; then
  echo "version sync check passed (all desktop fields match manifest $manifest_version)"
fi

exit "$status"
