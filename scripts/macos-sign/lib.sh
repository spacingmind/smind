# shellcheck shell=bash
# Shared by sign-notarize.sh and verify.sh (docs/plans/completed/desktop-notarization-prep.md,
# ADR-0060 draft). Sourced, never run.
#
# The rules every script here keeps:
#  - nothing runs without the secrets: a missing variable is a refusal (exit 3) that names the
#    VARIABLE, never a value, before any tool is invoked;
#  - no secret is echoed, logged, written under a predictable name or passed through `set -x`
#    (the scripts never enable xtrace); secrets live in env vars and in 0600 files in a private
#    temp dir that is removed on exit, including the temporary keychain;
#  - the signing keychain is a temporary one, never the login keychain, and is deleted on exit;
#  - nothing is signed with `--deep` and nothing is sent anywhere except the notarization
#    submission itself.
# An inherited xtrace (`bash -x`, SHELLOPTS=xtrace, a verbose PS4, BASH_XTRACEFD) would print every
# command with its expanded secrets: switch it off before anything runs, and never switch it on.
set +o xtrace
set +o verbose
unset BASH_XTRACEFD PS4
set -euo pipefail
umask 077

# The Human's Apple credentials. The names are the contract with the workflow's secrets.
SMIND_SIGN_REQUIRED_VARS=(
  SMIND_MACOS_CERT_P12_BASE64   # the "Developer ID Application" certificate + private key, .p12, base64
  SMIND_MACOS_CERT_PASSWORD     # the .p12 export password
  SMIND_MACOS_SIGN_IDENTITY     # e.g. "Developer ID Application: Example Ltd (ABCDE12345)"
  SMIND_NOTARY_KEY_P8_BASE64    # an App Store Connect API key (.p8), base64
  SMIND_NOTARY_KEY_ID           # its 10-character key id
  SMIND_NOTARY_ISSUER           # the issuer id (a UUID)
)

die() { printf 'macos-sign: %s\n' "$*" >&2; exit 1; }
refuse() { printf 'macos-sign: refusing to run: %s\n' "$*" >&2; exit 3; }
say() { printf 'macos-sign: %s\n' "$*"; }

require_darwin() {
  [ "$(uname -s)" = "Darwin" ] || refuse "this needs macOS (codesign, notarytool, stapler)"
}

# Refuses, before any tool runs, unless every secret is set and has the right shape. Prints
# variable names only.
require_secrets() {
  local v missing=()
  for v in "${SMIND_SIGN_REQUIRED_VARS[@]}"; do
    # empty or only whitespace counts as missing
    [ -n "$(printf '%s' "${!v:-}" | tr -d '[:space:]')" ] || missing+=("$v")
  done
  if [ "${#missing[@]}" -gt 0 ]; then
    refuse "missing secret environment variable(s): ${missing[*]}. Nothing was run: signing and notarization need the Human's Apple Developer credentials (docs/desktop-macos-signing.md)"
  fi
  # the full certificate name: "Developer ID Application: <name> (<10-character team id>)"
  grep -Eq '^Developer ID Application: .+ \([A-Z0-9]{10}\)$' <<<"$SMIND_MACOS_SIGN_IDENTITY" \
    || refuse "SMIND_MACOS_SIGN_IDENTITY must be the full \"Developer ID Application: <name> (<TEAMID>)\" certificate name (a distribution outside the App Store is signed with that certificate type)"
  grep -Eq '^[A-Z0-9]{10}$' <<<"$SMIND_NOTARY_KEY_ID" || refuse "SMIND_NOTARY_KEY_ID is not a 10-character key id"
  grep -Eqi '^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$' <<<"$SMIND_NOTARY_ISSUER" || refuse "SMIND_NOTARY_ISSUER is not a UUID"
  # (`base64 -d` needs macOS 13 or later; older macOS spells it -D)
  [ -n "$(printf '%s' "$SMIND_MACOS_CERT_P12_BASE64" | base64 -d 2>/dev/null | head -c 1)" ] || refuse "SMIND_MACOS_CERT_P12_BASE64 is not valid, non-empty base64"
  [ -n "$(printf '%s' "$SMIND_NOTARY_KEY_P8_BASE64" | base64 -d 2>/dev/null | head -c 1)" ] || refuse "SMIND_NOTARY_KEY_P8_BASE64 is not valid, non-empty base64"
}

# Refuses a path under an Applications folder (the real installed app is /Applications/sMind.app):
# signing replaces the bundle's signature in place, and the scripts must not be pointed at what the
# user is running. `--force-installed` is the explicit override, set by the caller as FORCE_INSTALLED=1.
refuse_installed() { # <path> <what>
  local p="$1" dir
  if dir="$(cd "$(dirname "$p")" 2>/dev/null && pwd -P)"; then :; else dir="$(dirname "$p")"; fi
  case "$dir/$(basename "$p")" in
    /Applications/*|/System/Applications/*|"${HOME:-/nonexistent}"/Applications/*|/Users/*/Applications/*)
      [ "${FORCE_INSTALLED:-0}" = "1" ] || refuse "$2 is under an Applications folder (an installed app): pass --force-installed to sign it in place on purpose" ;;
  esac
}

WORK=""
KEYCHAIN=""

cleanup() {
  local rc=$?
  if [ -n "$KEYCHAIN" ]; then
    security delete-keychain "$KEYCHAIN" >/dev/null 2>&1 || true
  fi
  if [ -n "$WORK" ] && [ -d "$WORK" ]; then
    rm -rf "$WORK"
  fi
  return "$rc"
}

# Creates the private work dir (0700) and the cleanup trap.
init_work() {
  WORK="$(mktemp -d "${TMPDIR:-/tmp}/smind-macos-sign.XXXXXX")"
  chmod 700 "$WORK"
  trap cleanup EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
  trap 'exit 129' HUP
  trap 'exit 131' QUIT
}

# Imports the signing certificate into a temporary keychain and checks the identity is there.
# Sets KEYCHAIN. The keychain password is random and never printed. The .p12 password is passed
# to `security import` on its command line (the tool offers no other way), and the random keychain
# password to create-keychain, unlock-keychain and set-key-partition-list: both are visible to other
# local users in `ps` for that moment, so this is meant for a single-user CI runner or the Human's
# own machine.
setup_keychain() {
  local kcpw p12 identities
  KEYCHAIN="$WORK/signing.keychain-db"
  kcpw="$(head -c 24 /dev/urandom | base64)"
  p12="$WORK/cert.p12"
  printf '%s' "$SMIND_MACOS_CERT_P12_BASE64" | base64 -d > "$p12"
  security create-keychain -p "$kcpw" "$KEYCHAIN"
  security set-keychain-settings -lut 3600 "$KEYCHAIN"
  security unlock-keychain -p "$kcpw" "$KEYCHAIN"
  # only codesign may use the imported key (not the `security` tool itself)
  security import "$p12" -P "$SMIND_MACOS_CERT_PASSWORD" -f pkcs12 -t cert -k "$KEYCHAIN" \
    -T /usr/bin/codesign >/dev/null
  rm -f "$p12"
  security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k "$kcpw" "$KEYCHAIN" >/dev/null
  # captured first: `grep -q` at the end of a pipeline under pipefail fails on a large listing
  identities="$(security find-identity -v -p codesigning "$KEYCHAIN")"
  if ! grep -qF -- "$SMIND_MACOS_SIGN_IDENTITY" <<<"$identities"; then
    die "the signing identity is not among the valid identities of the imported certificate: check SMIND_MACOS_SIGN_IDENTITY against the .p12, that it has not expired, and that the .p12 carries the Developer ID certificate chain (the check lists VALID identities only)"
  fi
  say "signing identity found in a temporary keychain"
}

# Writes the notarization API key to a 0600 file and sets NOTARY_KEY_FILE.
setup_notary_key() {
  NOTARY_KEY_FILE="$WORK/AuthKey.p8"
  printf '%s' "$SMIND_NOTARY_KEY_P8_BASE64" | base64 -d > "$NOTARY_KEY_FILE"
  chmod 600 "$NOTARY_KEY_FILE"
}

# notarytool with the API key (the key goes by file path, the ids by argument: neither is a secret
# by itself).
notary() {
  xcrun notarytool "$@" --key "$NOTARY_KEY_FILE" --key-id "$SMIND_NOTARY_KEY_ID" --issuer "$SMIND_NOTARY_ISSUER"
}
