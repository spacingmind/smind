#!/usr/bin/env bash
# Developer ID code signing, notarization and stapling of the built macOS app, including the Go
# sidecar, with the hardened runtime and committed entitlements. GATED ON SECRETS: it refuses to
# run (exit 3, names the missing variables) unless the Human's Apple credentials are in the
# environment; nothing here has ever been run against Apple (docs/plans/active/
# desktop-notarization-prep.md, ADR-0060 draft).
#
# usage: sign-notarize.sh --app <sMind.app> [--out <dir>] [--dmg <out.dmg>] [--updater-tarball <out.app.tar.gz>]
#                         [--entitlements-app <file>] [--entitlements-sidecar <file>] [--force-installed] | --check
#   --check   only the gate: secrets present, certificate imports, identity found; signs nothing
#
# Order (inside out, never --deep): the frameworks in Contents/Frameworks and the other executables
# in Contents/MacOS (the sidecar), then the main executable's bundle; a bundle with any other kind of
# nested code (Helpers, XPCServices, PlugIns, Library/LaunchServices) is refused, not half-signed;
# verify; zip; notarize and wait; fail with the log unless Accepted; staple.
# With --dmg the stapled app goes into a dmg that is signed, notarized and stapled too. With
# --updater-tarball the stapled app is packed for the updater: the caller must then re-sign that
# tarball with the updater key (`tauri signer sign`), the old .sig no longer matches.
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/macos-sign/lib.sh
. "$here/lib.sh"

# an option value is a non-empty path that does not look like another option
need() {
  [ -n "${2:-}" ] || { printf 'macos-sign: %s needs a value\n' "$1" >&2; exit 2; }
  case "$2" in -*) printf 'macos-sign: %s needs a path, not an option (%s)\n' "$1" "$2" >&2; exit 2 ;; esac
}
app="" out="" dmg="" tarball="" check=0 FORCE_INSTALLED=0
ent_app="$here/../../desktop/src-tauri/entitlements/app.entitlements"
ent_sidecar="$here/../../desktop/src-tauri/entitlements/sidecar.entitlements"
while [ $# -gt 0 ]; do
  case "$1" in
    --app) need "$1" "${2:-}"; app="$2"; shift 2 ;;
    --out) need "$1" "${2:-}"; out="$2"; shift 2 ;;
    --dmg) need "$1" "${2:-}"; dmg="$2"; shift 2 ;;
    --updater-tarball) need "$1" "${2:-}"; tarball="$2"; shift 2 ;;
    --entitlements-app) need "$1" "${2:-}"; ent_app="$2"; shift 2 ;;
    --entitlements-sidecar) need "$1" "${2:-}"; ent_sidecar="$2"; shift 2 ;;
    --check) check=1; shift ;;
    --force-installed) FORCE_INSTALLED=1; shift ;;
    -h|--help) sed -n '2,/^set -/{/^set -/!p;}' "$0"; exit 0 ;;
    *) printf 'macos-sign: unknown argument %s\n' "$1" >&2; exit 2 ;;
  esac
done

# The gate comes first: before any path is looked at, before any tool runs.
require_secrets
require_darwin
init_work
setup_keychain
setup_notary_key
if [ "$check" -eq 1 ]; then
  say "check passed: secrets present, certificate imported, identity found (nothing signed)"
  exit 0
fi

[ -n "$app" ] || { printf 'macos-sign: --app is required\n' >&2; exit 2; }
refuse_installed "$app" "--app"
[ -z "$out" ] || refuse_installed "$out" "--out"
[ -z "$dmg" ] || refuse_installed "$dmg" "--dmg"
[ -z "$tarball" ] || refuse_installed "$tarball" "--updater-tarball"
if [ ! -d "$app" ] || [ -L "$app" ]; then die "--app is not a directory (or is a link): $app"; fi
case "$app" in *.app) ;; *) die "--app must be a .app bundle" ;; esac
[ -f "$ent_app" ] || die "entitlements file not found: $ent_app"
[ -f "$ent_sidecar" ] || die "entitlements file not found: $ent_sidecar"
[ -n "$out" ] || out="$(dirname "$app")"
mkdir -p -- "$out"

sign() { # sign <entitlements|""> <path>
  local ent="$1" path="$2"
  if [ -n "$ent" ]; then
    codesign --force --options runtime --timestamp --keychain "$KEYCHAIN" \
      --sign "$SMIND_MACOS_SIGN_IDENTITY" --entitlements "$ent" "$path"
  else
    codesign --force --options runtime --timestamp --keychain "$KEYCHAIN" \
      --sign "$SMIND_MACOS_SIGN_IDENTITY" "$path"
  fi
}

# Nested code this script does not know how to order is refused rather than left unsigned (an
# unsigned nested binary is exactly what makes Apple reject the notarization, after a long wait).
for d in Helpers XPCServices PlugIns Library/LaunchServices Library/LoginItems; do
  [ ! -e "$app/Contents/$d" ] || die "the bundle has Contents/$d: nested code this script does not sign (extend it and its tests first)"
done

exe_name="$(plutil -extract CFBundleExecutable raw -o - "$app/Contents/Info.plist")"
[ -n "$exe_name" ] || die "the app has no CFBundleExecutable"

say "signing nested code"
if [ -d "$app/Contents/Frameworks" ]; then
  for f in "$app"/Contents/Frameworks/*; do
    [ -e "$f" ] || continue
    sign "" "$f"
  done
fi
for f in "$app"/Contents/MacOS/*; do
  [ -f "$f" ] || continue
  [ "$(basename "$f")" = "$exe_name" ] && continue
  say "  sidecar: $(basename "$f")"
  sign "$ent_sidecar" "$f"
done
say "signing the app bundle"
sign "$ent_app" "$app"
codesign --verify --strict --verbose=2 "$app"

notarize() { # notarize <path-to-zip-or-dmg> <label>
  local file="$1" label="$2" result id status
  result="$WORK/notary-$label.json"
  say "submitting $label to Apple (waiting; up to 30 minutes)"
  local raw="$WORK/notary-$label.out" rc=0
  notary submit "$file" --wait --timeout 30m --output-format json > "$raw" 2>"$WORK/notary-$label.err" || rc=$?
  # notarytool may print progress text before the JSON object: keep from the first line that is `{`
  sed -n '/^{/,$p' "$raw" > "$result"
  id="$(plutil -extract id raw -o - "$result" 2>/dev/null || true)"
  status="$(plutil -extract status raw -o - "$result" 2>/dev/null || true)"
  if [ "$status" != "Accepted" ]; then
    if [ -n "$id" ]; then
      notary log "$id" "$out/notarization-log-$label.json" >/dev/null 2>&1 || true
    fi
    die "notarization of $label ended as '${status:-unknown}' (notarytool exit status $rc, submission ${id:-none}); the log, if Apple returned one, is $out/notarization-log-$label.json"
  fi
  say "$label notarized (submission $id)"
}

zip_path="$WORK/app.zip"
ditto -c -k --keepParent "$app" "$zip_path"
notarize "$zip_path" app
xcrun stapler staple "$app"
xcrun stapler validate "$app"

if [ -n "$tarball" ]; then
  say "packing the updater tarball (re-sign it with the updater key afterwards)"
  COPYFILE_DISABLE=1 tar -C "$(dirname "$app")" -czf "$tarball" "$(basename "$app")"
fi

if [ -n "$dmg" ]; then
  stage="$WORK/dmg-stage"
  mkdir -p "$stage"
  ditto "$app" "$stage/$(basename "$app")"
  ln -s /Applications "$stage/Applications"
  hdiutil create -volname "$(basename "$app" .app)" -srcfolder "$stage" -ov -format UDZO "$dmg" >/dev/null
  codesign --force --timestamp --keychain "$KEYCHAIN" --sign "$SMIND_MACOS_SIGN_IDENTITY" "$dmg"
  notarize "$dmg" dmg
  xcrun stapler staple "$dmg"
  xcrun stapler validate "$dmg"
fi
say "done"
