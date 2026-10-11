#!/bin/sh
# Decides whether a Tauri build should produce updater artifacts
# (ADR-0023.4): sign only when the private key secret is present, fail
# loudly when a real release requires it and it is missing. Never prints
# the key itself.
set -eu

key="${TAURI_SIGNING_PRIVATE_KEY:-}"

if [ -n "$key" ]; then
  echo "sign=true"
  exit 0
fi

if [ "${REQUIRE_UPDATER_SIGNING:-}" = "true" ]; then
  echo "require-updater-signing is set but the TAURI_SIGNING_PRIVATE_KEY secret is empty; set it with gh secret set TAURI_SIGNING_PRIVATE_KEY" >&2
  exit 1
fi

echo "sign=false"
