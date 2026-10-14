#!/usr/bin/env bash
# Signs <dir>/checksums.txt with the desktop-updater minisign key (ADR-0023,
# ADR-0031 decision 1) and writes <dir>/checksums.txt.minisig, the plain
# minisign signature file `smind update` and `minisign -V` verify.
#
# usage: scripts/sign-checksums.sh <dir> <version>
#
# environment (read by the Tauri CLI; the key is never put on a command line,
# echoed or written to disk by this script):
#   TAURI_SIGNING_PRIVATE_KEY           the key, literal (required)
#   TAURI_SIGNING_PRIVATE_KEY_PASSWORD  its password (empty = no password)
#   SMIND_SIGN_PUBKEY                   the matching public key in Tauri's form
#                                       (base64 of the .pub file); the new
#                                       signature is verified against it with
#                                       `minisign -V` before the script succeeds
#   TAURI_CLI                           path to the tauri CLI
#                                       (default: desktop/node_modules/.bin/tauri)
#
# The version is bound into the signed trusted comment (`--app-version`), so a
# signature cannot be replayed as another release's; smind update checks it.
set -euo pipefail

dir="${1:?usage: sign-checksums.sh <dir> <version>}"
version="${2:?usage: sign-checksums.sh <dir> <version>}"
tauri="${TAURI_CLI:-desktop/node_modules/.bin/tauri}"

[ -s "$dir/checksums.txt" ] || { echo "sign-checksums: $dir/checksums.txt is missing or empty" >&2; exit 1; }
[ -n "${TAURI_SIGNING_PRIVATE_KEY:-}" ] || { echo "sign-checksums: TAURI_SIGNING_PRIVATE_KEY is not set" >&2; exit 1; }
[ -n "${SMIND_SIGN_PUBKEY:-}" ] || { echo "sign-checksums: SMIND_SIGN_PUBKEY is not set" >&2; exit 1; }
[ -x "$tauri" ] || { echo "sign-checksums: tauri CLI not found at $tauri (npm ci --prefix desktop)" >&2; exit 1; }
command -v minisign >/dev/null || { echo "sign-checksums: minisign is required to verify the signature" >&2; exit 1; }

# An unset password means "no password" (ADR-0023); never let the CLI prompt.
export TAURI_SIGNING_PRIVATE_KEY_PASSWORD="${TAURI_SIGNING_PRIVATE_KEY_PASSWORD-}"

rm -f "$dir/checksums.txt.sig" "$dir/checksums.txt.minisig"
# stdout is the base64 signature (public); stderr may carry key-handling
# diagnostics, so it is shown only on failure.
if ! err="$("$tauri" signer sign --app-version "$version" "$dir/checksums.txt" 2>&1 >/dev/null)"; then
  echo "sign-checksums: signing failed: $err" >&2
  exit 1
fi
[ -s "$dir/checksums.txt.sig" ] || { echo "sign-checksums: the tauri CLI wrote no signature" >&2; exit 1; }

# Tauri stores the signature file base64-encoded; publish the plain minisign file.
base64 -d < "$dir/checksums.txt.sig" > "$dir/checksums.txt.minisig"
rm -f "$dir/checksums.txt.sig"

# Independent check with the reference implementation: the key line of the
# public key file is the second line of the base64-decoded Tauri form.
pubkey_line="$(printf '%s' "$SMIND_SIGN_PUBKEY" | base64 -d | sed -n 2p)"
minisign -V -m "$dir/checksums.txt" -x "$dir/checksums.txt.minisig" -P "$pubkey_line"
echo "sign-checksums: $dir/checksums.txt.minisig verifies against the public key (version $version)"
