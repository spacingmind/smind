#!/usr/bin/env bash
# Read-only check of a signed, notarized macOS app (and optionally its dmg): the gate that runs
# after signing and that the Human can run on any Mac. Needs no secrets and changes nothing.
# docs/plans/active/desktop-notarization-prep.md, ADR-0060 draft.
#
# usage: verify.sh --app <sMind.app> [--dmg <file.dmg>] [--entitlements-app <file>] [--entitlements-sidecar <file>]
#
# Checks: a strict signature (no --deep), a Developer ID signature (not ad hoc), the hardened
# runtime flag on the app and on every nested executable, the same Team ID everywhere, the
# entitlements equal the committed files, Gatekeeper assessment (spctl), a valid stapled ticket;
# for a dmg: signature, spctl, stapled ticket.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
fail=0
ok() { printf 'ok   %s\n' "$*"; }
bad() { printf 'FAIL %s\n' "$*"; fail=1; }

[ "$(uname -s)" = "Darwin" ] || { echo "macos-sign: refusing to run: this needs macOS" >&2; exit 3; }

need() { [ -n "${2:-}" ] || { echo "macos-sign: $1 needs a value" >&2; exit 2; }; }
app="" dmg=""
ent_app="$here/../../desktop/src-tauri/entitlements/app.entitlements"
ent_sidecar="$here/../../desktop/src-tauri/entitlements/sidecar.entitlements"
while [ $# -gt 0 ]; do
  case "$1" in
    --app) need "$1" "${2:-}"; app="$2"; shift 2 ;;
    --dmg) need "$1" "${2:-}"; dmg="$2"; shift 2 ;;
    --entitlements-app) need "$1" "${2:-}"; ent_app="$2"; shift 2 ;;
    --entitlements-sidecar) need "$1" "${2:-}"; ent_sidecar="$2"; shift 2 ;;
    -h|--help) sed -n '2,/^set -/{/^set -/!p;}' "$0"; exit 0 ;;
    *) echo "macos-sign: unknown argument $1" >&2; exit 2 ;;
  esac
done
[ -n "$app" ] || { echo "macos-sign: --app is required" >&2; exit 2; }
[ -d "$app" ] || { echo "macos-sign: not a directory: $app" >&2; exit 2; }

# Entitlements compared as plists with whitespace removed: the committed files carry a comment
# that the signed copy does not, so `plutil -p` (which drops comments) normalises both; a binary
# signed with no entitlements prints nothing, which equals an empty dict.
ents_signed() {
  local out
  out="$(codesign -d --entitlements :- "$1" 2>/dev/null | plutil -p - 2>/dev/null | tr -d '[:space:]' || true)"
  [ -n "$out" ] || out="{}"
  printf '%s' "$out"
}
ents_file() { plutil -p "$1" | tr -d '[:space:]'; }

details() { codesign -dv --verbose=4 "$1" 2>&1 || true; }
# `grep -q` in a pipeline under `set -o pipefail` is a trap: grep exits at the first match, the
# writer gets SIGPIPE, and the whole pipeline then "fails" on a large output (found by running this
# script against real signed apps). So the output is captured first and searched as a here-string.
has() { local out; out="$(details "$1")"; grep -Eq -- "$2" <<<"$out"; }
team_of() { local out; out="$(details "$1")"; sed -n 's/^TeamIdentifier=//p' <<<"$out" | head -1 || true; }

exe_name="$(plutil -extract CFBundleExecutable raw -o - "$app/Contents/Info.plist")"
if codesign --verify --strict --verbose=2 "$app" >/dev/null 2>&1; then ok "app: strict signature verifies"; else bad "app: strict signature does not verify"; fi
if has "$app" '^Signature=adhoc'; then bad "app: ad-hoc signature (not Developer ID)"; else ok "app: not ad hoc"; fi
if has "$app" '^Authority=Developer ID Application:'; then ok "app: Developer ID Application authority"; else bad "app: no Developer ID Application authority"; fi
app_team="$(team_of "$app")"
if [ -n "$app_team" ] && [ "$app_team" != "not set" ]; then ok "app: team $app_team"; else bad "app: no Team ID"; fi

check_runtime() { # <label> <path>
  if has "$2" 'flags=0x[0-9a-f]+\(.*runtime.*\)'; then ok "$1: hardened runtime"; else bad "$1: hardened runtime flag missing"; fi
}
check_runtime app "$app"
for f in "$app"/Contents/Frameworks/*; do
  [ -e "$f" ] || continue
  name="framework $(basename "$f")"
  if codesign --verify --strict "$f" >/dev/null 2>&1; then ok "$name: signature verifies"; else bad "$name: signature does not verify"; fi
  check_runtime "$name" "$f"
  t="$(team_of "$f")"
  if [ "$t" = "$app_team" ]; then ok "$name: same team"; else bad "$name: team '$t' differs from the app's '$app_team'"; fi
done
for f in "$app"/Contents/MacOS/*; do
  [ -f "$f" ] || continue
  [ "$(basename "$f")" = "$exe_name" ] && continue
  name="sidecar $(basename "$f")"
  if codesign --verify --strict "$f" >/dev/null 2>&1; then ok "$name: signature verifies"; else bad "$name: signature does not verify"; fi
  check_runtime "$name" "$f"
  t="$(team_of "$f")"
  if [ "$t" = "$app_team" ]; then ok "$name: same team"; else bad "$name: team '$t' differs from the app's '$app_team'"; fi
  if [ "$(ents_signed "$f")" = "$(ents_file "$ent_sidecar")" ]; then ok "$name: entitlements equal the committed file"; else bad "$name: entitlements differ from $ent_sidecar"; fi
done
if [ "$(ents_signed "$app")" = "$(ents_file "$ent_app")" ]; then ok "app: entitlements equal the committed file"; else bad "app: entitlements differ from $ent_app"; fi
if spctl -a -t exec -vv "$app" >/dev/null 2>&1; then ok "app: Gatekeeper accepts it"; else bad "app: Gatekeeper (spctl) rejects it"; fi
if xcrun stapler validate "$app" >/dev/null 2>&1; then ok "app: notarization ticket stapled"; else bad "app: no valid stapled ticket"; fi

if [ -n "$dmg" ]; then
  [ -f "$dmg" ] || { echo "macos-sign: not a file: $dmg" >&2; exit 2; }
  if codesign --verify --strict "$dmg" >/dev/null 2>&1; then ok "dmg: signature verifies"; else bad "dmg: signature does not verify"; fi
  if spctl -a -t open --context context:primary-signature -vv "$dmg" >/dev/null 2>&1; then ok "dmg: Gatekeeper accepts it"; else bad "dmg: Gatekeeper (spctl) rejects it"; fi
  if xcrun stapler validate "$dmg" >/dev/null 2>&1; then ok "dmg: notarization ticket stapled"; else bad "dmg: no valid stapled ticket"; fi
fi
exit "$fail"
