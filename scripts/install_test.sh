#!/bin/sh
# Tests scripts/install.sh against a fake release server: `curl` and `uname`
# on PATH are stubs that serve local fixtures, so no network is used and the
# result is the same on Linux and macOS. The --desktop tests also stub pgrep,
# xattr, sysctl and the kill commands, and install into a scratch
# SMIND_APP_DIR -- never the real /Applications.
#
#   scripts/install_test.sh
set -eu

here="$(cd "$(dirname "$0")" && pwd)"
script="$here/install.sh"

work="$(mktemp -d 2>/dev/null || mktemp -d -t smind-install-test)"
trap 'rm -rf "$work"' EXIT INT TERM

stubs="$work/stubs"
releases="$work/releases"
mkdir -p "$stubs" "$releases"

# --- stubs -----------------------------------------------------------------

cat >"$stubs/uname" <<'EOF'
#!/bin/sh
case "$1" in
  -s) echo "${FAKE_UNAME_S:-Linux}" ;;
  -m) echo "${FAKE_UNAME_M:-x86_64}" ;;
  *) echo "${FAKE_UNAME_S:-Linux}" ;;
esac
EOF

# Serves $FAKE_RELEASES/v<version>/<file> for github.com release downloads and
# answers the /releases/latest redirect probe with v$FAKE_LATEST.
cat >"$stubs/curl" <<'EOF'
#!/bin/sh
out=
url=
while [ $# -gt 0 ]; do
  case "$1" in
    -o | -w) [ "$1" = -o ] && out="$2"; shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
echo "$url" >>"$FAKE_CURL_LOG"
case "$url" in
  */releases/latest)
    printf '%s' "https://github.com/spacingmind/smind/releases/tag/v${FAKE_LATEST:-9.9.9}"
    ;;
  */releases/download/*)
    f="$FAKE_RELEASES/${url#*/releases/download/}"
    [ -f "$f" ] || exit 22
    cp "$f" "$out"
    ;;
  *) exit 22 ;;
esac
EOF

# --desktop collaborators: sMind "running" is FAKE_SMIND_RUNNING=1; xattr and the
# kill commands only record their arguments (a kill is a test failure).
cat >"$stubs/pgrep" <<'EOF'
#!/bin/sh
echo "$@" >>"${FAKE_PGREP_LOG:-/dev/null}"
[ "${FAKE_SMIND_RUNNING:-0}" = 1 ]
EOF
cat >"$stubs/xattr" <<'EOF'
#!/bin/sh
echo "$@" >>"${FAKE_XATTR_LOG:-/dev/null}"
EOF
cat >"$stubs/sysctl" <<'EOF'
#!/bin/sh
echo "${FAKE_TRANSLATED:-0}"
EOF
for k in kill pkill killall; do
  cat >"$stubs/$k" <<'EOF'
#!/bin/sh
echo "$0 $*" >>"${FAKE_KILL_LOG:-/dev/null}"
EOF
done
chmod +x "$stubs/uname" "$stubs/curl" "$stubs/pgrep" "$stubs/xattr" "$stubs/sysctl" "$stubs/kill" "$stubs/pkill" "$stubs/killall"

if command -v sha256sum >/dev/null 2>&1; then
  sha256() { sha256sum "$1" | cut -d' ' -f1; }
else
  sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
fi

# make_release <version> [tamper]: a linux/amd64 archive whose `smind` prints
# its own version, plus a checksums.txt (wrong digest when tamper is set).
make_release() {
  v="$1"
  d="$releases/v$v"
  mkdir -p "$d" "$work/pkg"
  # `smind service ...` records its arguments (and can be made to fail), like
  # the real command would be invoked by --service; anything else prints a version.
  # shellcheck disable=SC2016 # the stub's own $1 / ${...} must stay literal
  printf '#!/bin/sh\nif [ "$1" = service ]; then echo "$@" >>"${FAKE_SMIND_LOG:-/dev/null}"; exit "${FAKE_SERVICE_EXIT:-0}"; fi\necho "smind version %s"\n' "$v" >"$work/pkg/smind"
  chmod +x "$work/pkg/smind"
  tar -czf "$d/smind_${v}_linux_amd64.tar.gz" -C "$work/pkg" smind
  sum="$(sha256 "$d/smind_${v}_linux_amd64.tar.gz")"
  [ "${2:-}" = tamper ] && sum="0000000000000000000000000000000000000000000000000000000000000000"
  {
    echo "1111111111111111111111111111111111111111111111111111111111111111  smind_${v}_darwin_arm64.tar.gz"
    echo "$sum  smind_${v}_linux_amd64.tar.gz"
  } >"$d/checksums.txt"
}

# make_app_release <version> [tamper|nobundle]: appends smind-desktop-<v>-macos-
# <arch>.app.tar.gz (both arches) to <version>'s release and checksums.txt. The
# app's Contents/Resources/version records which release it came from. `tamper`
# publishes wrong digests; `nobundle` ships an archive with no .app in it.
make_app_release() {
  v="$1"
  d="$releases/v$v"
  for arch in arm64 x86_64; do
    rm -rf "${work:?}/apppkg"
    mkdir -p "$work/apppkg"
    if [ "${2:-}" = nobundle ]; then
      echo "not an app" >"$work/apppkg/README"
      tar -czf "$d/smind-desktop-${v}-macos-${arch}.app.tar.gz" -C "$work/apppkg" README
    else
      mkdir -p "$work/apppkg/sMind.app/Contents/MacOS" "$work/apppkg/sMind.app/Contents/Resources"
      printf '#!/bin/sh\n' >"$work/apppkg/sMind.app/Contents/MacOS/sMind"
      chmod +x "$work/apppkg/sMind.app/Contents/MacOS/sMind"
      echo "$v $arch" >"$work/apppkg/sMind.app/Contents/Resources/version"
      tar -czf "$d/smind-desktop-${v}-macos-${arch}.app.tar.gz" -C "$work/apppkg" sMind.app
    fi
    sum="$(sha256 "$d/smind-desktop-${v}-macos-${arch}.app.tar.gz")"
    [ "${2:-}" = tamper ] && sum="0000000000000000000000000000000000000000000000000000000000000000"
    echo "$sum  smind-desktop-${v}-macos-${arch}.app.tar.gz" >>"$d/checksums.txt"
  done
}

make_release 1.2.3
make_release 4.5.6
make_release 6.6.6 tamper
make_release 7.1.0
make_release 7.1.1
make_release 7.3.0
make_app_release 1.2.3
make_app_release 4.5.6
make_app_release 7.1.0 tamper
make_app_release 7.1.1 nobundle
# 7.3.0 has CLI archives only: no app asset, no checksum for it

# --- harness ---------------------------------------------------------------

fails=0
passes=0

# run_install [args...]: runs install.sh with stubs first on PATH; sets rc,
# and leaves stdout/stderr in $work/out and $work/err. SMIND_VERSION and
# FAKE_* are taken from the caller's environment.
run_install() {
  : >"$work/curl.log"
  : >"$work/smind.log"
  rm -rf "${work:?}/bin" "${work:?}/home"
  mkdir -p "$work/home"
  set +e
  env PATH="$stubs:$PATH" HOME="$work/home" \
    FAKE_RELEASES="$releases" FAKE_CURL_LOG="$work/curl.log" FAKE_SMIND_LOG="$work/smind.log" \
    SMIND_INSTALL_DIR="$work/bin" "$@" >"$work/out" 2>"$work/err"
  rc=$?
  set -e
}

check() { # check <name> <condition-exit-status>
  if [ "$2" -eq 0 ]; then
    passes=$((passes + 1))
    echo "ok   $1"
  else
    fails=$((fails + 1))
    echo "FAIL $1"
    echo "  rc=$rc"
    sed 's/^/  out: /' "$work/out"
    sed 's/^/  err: /' "$work/err"
  fi
}

has() { grep -q -- "$2" "$1"; } # has <file> <fixed-ish pattern>

# --- tests -----------------------------------------------------------------

test_help_prints_usage() {
  run_install sh "$script" --help
  [ "$rc" -eq 0 ] && has "$work/out" "Usage: install.sh" && has "$work/out" "--version"
}

test_unknown_flag_exits_2() {
  run_install sh "$script" --bogus
  [ "$rc" -eq 2 ] && has "$work/err" "unknown option: --bogus" && [ ! -e "$work/bin/smind" ]
}

test_version_without_value_exits_2() {
  run_install sh "$script" --version
  [ "$rc" -eq 2 ] && has "$work/err" "needs a value"
}

test_version_flag_installs_that_release() {
  run_install sh "$script" --version 1.2.3
  [ "$rc" -eq 0 ] && [ "$("$work/bin/smind")" = "smind version 1.2.3" ]
}

test_version_equals_form_and_v_prefix() {
  run_install sh "$script" --version=v1.2.3
  [ "$rc" -eq 0 ] && [ "$("$work/bin/smind")" = "smind version 1.2.3" ]
}

test_env_version_still_works() {
  run_install env SMIND_VERSION=4.5.6 sh "$script"
  [ "$rc" -eq 0 ] && [ "$("$work/bin/smind")" = "smind version 4.5.6" ]
}

test_version_flag_wins_over_env() {
  run_install env SMIND_VERSION=4.5.6 sh "$script" --version 1.2.3
  [ "$rc" -eq 0 ] && [ "$("$work/bin/smind")" = "smind version 1.2.3" ] &&
    ! grep -q "v4.5.6" "$work/curl.log"
}

test_latest_resolves_via_release_redirect() {
  run_install env FAKE_LATEST=4.5.6 sh "$script"
  [ "$rc" -eq 0 ] && [ "$("$work/bin/smind")" = "smind version 4.5.6" ]
}

test_missing_release_fails_and_installs_nothing() {
  run_install sh "$script" --version 7.7.7
  [ "$rc" -ne 0 ] && has "$work/err" "download failed" && [ ! -e "$work/bin/smind" ]
}

test_version_flag_rejects_empty_and_option_values() {
  run_install env SMIND_VERSION=1.2.3 sh "$script" --version ""
  [ "$rc" -eq 2 ] && has "$work/err" "needs a version" && [ ! -s "$work/curl.log" ] || return 1
  run_install sh "$script" --version=
  [ "$rc" -eq 2 ] && [ ! -s "$work/curl.log" ] || return 1
  run_install sh "$script" --version --help
  [ "$rc" -eq 2 ] && [ ! -s "$work/curl.log" ] && [ ! -e "$work/bin/smind" ]
}

test_version_must_start_with_digit() {
  run_install sh "$script" --version .hidden
  [ "$rc" -ne 0 ] && has "$work/err" "valid version" && [ ! -s "$work/curl.log" ]
}

test_stdin_form_sh_dash_s() {
  # curl ... | sh -s -- --version X
  : >"$work/curl.log"
  rm -rf "${work:?}/bin" "${work:?}/home"
  mkdir -p "$work/home"
  set +e
  env PATH="$stubs:$PATH" HOME="$work/home" FAKE_RELEASES="$releases" \
    FAKE_CURL_LOG="$work/curl.log" SMIND_INSTALL_DIR="$work/bin" \
    sh -s -- --version 4.5.6 <"$script" >"$work/out" 2>"$work/err"
  rc=$?
  set -e
  [ "$rc" -eq 0 ] && [ "$("$work/bin/smind")" = "smind version 4.5.6" ]
}

test_no_service_flag_never_runs_service_install() {
  run_install sh "$script" --version 1.2.3
  [ "$rc" -eq 0 ] && [ ! -s "$work/smind.log" ] && has "$work/out" "smind service install"
}

test_install_sh_service_flag_runs_service_install() {
  run_install sh "$script" --version 1.2.3 --service
  [ "$rc" -eq 0 ] && [ "$(cat "$work/smind.log")" = "service install" ] &&
    has "$work/out" "smind service status"
}

test_service_system_flag_passes_through() {
  run_install sh "$script" --version 1.2.3 --service --system
  [ "$rc" -eq 0 ] && [ "$(cat "$work/smind.log")" = "service install --system" ]
}

test_system_without_service_exits_2() {
  run_install sh "$script" --version 1.2.3 --system
  [ "$rc" -eq 2 ] && has "$work/err" "together with --service" && [ ! -s "$work/curl.log" ]
}

test_service_install_failure_fails_the_script_but_keeps_the_binary() {
  run_install env FAKE_SERVICE_EXIT=1 sh "$script" --version 1.2.3 --service
  [ "$rc" -ne 0 ] && has "$work/err" "service install' failed" && [ -x "$work/bin/smind" ]
}

test_service_flag_on_msys_still_points_at_powershell() {
  run_install env FAKE_UNAME_S=MINGW64_NT-10.0 sh "$script" --service
  [ "$rc" -ne 0 ] && has "$work/err" "install.ps1" && [ ! -s "$work/smind.log" ]
}

test_invalid_version_rejected() {
  run_install sh "$script" --version '1.0;rm -rf'
  [ "$rc" -ne 0 ] && has "$work/err" "valid version" && [ ! -s "$work/curl.log" ]
}

test_msys_prints_powershell_oneliner() {
  run_install env FAKE_UNAME_S=MINGW64_NT-10.0 sh "$script"
  [ "$rc" -ne 0 ] && has "$work/err" "install.ps1" && has "$work/err" "irm " &&
    ! has "$work/err" "no native Windows" && [ ! -s "$work/curl.log" ]
}

test_checksum_mismatch_installs_nothing() {
  run_install sh "$script" --version 6.6.6
  [ "$rc" -ne 0 ] && has "$work/err" "checksum mismatch" && [ ! -e "$work/bin/smind" ]
}

test_checksum_mismatch_leaves_existing_install() {
  # run_install wipes $work/bin, so seed the previous install via a wrapper.
  : >"$work/curl.log"
  rm -rf "${work:?}/bin" "${work:?}/home"
  mkdir -p "$work/bin" "$work/home"
  printf '#!/bin/sh\necho previous\n' >"$work/bin/smind"
  chmod +x "$work/bin/smind"
  set +e
  env PATH="$stubs:$PATH" HOME="$work/home" FAKE_RELEASES="$releases" \
    FAKE_CURL_LOG="$work/curl.log" SMIND_INSTALL_DIR="$work/bin" \
    sh "$script" --version 6.6.6 >"$work/out" 2>"$work/err"
  rc=$?
  set -e
  [ "$rc" -ne 0 ] && [ "$("$work/bin/smind")" = previous ]
}

test_rerun_updates_in_place() {
  run_install sh "$script" --version 1.2.3
  env PATH="$stubs:$PATH" HOME="$work/home" FAKE_RELEASES="$releases" \
    FAKE_CURL_LOG="$work/curl.log" SMIND_INSTALL_DIR="$work/bin" \
    sh "$script" --version 4.5.6 >"$work/out" 2>"$work/err" || return 1
  [ "$("$work/bin/smind")" = "smind version 4.5.6" ]
}

# --- --desktop ---------------------------------------------------------------

# reset_desktop: clean scratch dirs and logs; run_desktop <cmd...>: like
# run_install but as macOS with every collaborator stubbed, installing into
# $work/apps (SMIND_APP_DIR) -- the real /Applications is never involved.
reset_desktop() {
  : >"$work/curl.log"
  : >"$work/xattr.log"
  : >"$work/kill.log"
  : >"$work/pgrep.log"
  rm -rf "${work:?}/apps" "${work:?}/bin" "${work:?}/home"
  mkdir -p "$work/apps" "$work/home"
}

run_desktop() {
  set +e
  env PATH="$stubs:$PATH" HOME="$work/home" FAKE_UNAME_S=Darwin \
    FAKE_RELEASES="$releases" FAKE_CURL_LOG="$work/curl.log" FAKE_XATTR_LOG="$work/xattr.log" \
    FAKE_KILL_LOG="$work/kill.log" FAKE_PGREP_LOG="$work/pgrep.log" \
    SMIND_APP_DIR="$work/apps" SMIND_INSTALL_DIR="$work/bin" "$@" >"$work/out" 2>"$work/err"
  rc=$?
  set -e
}

# seed_old_app: a previously installed sMind.app, with a marker to tell it apart.
seed_old_app() {
  mkdir -p "$work/apps/sMind.app/Contents/MacOS"
  echo "previous" >"$work/apps/sMind.app/Contents/Resources-marker"
}

app_version() { cat "$work/apps/sMind.app/Contents/Resources/version" 2>/dev/null; }

no_scratch_left() { [ -z "$(find "$work/apps" -maxdepth 1 -name '.sMind-install.*' -print)" ]; }

test_desktop_installs_the_app_and_not_the_cli() {
  reset_desktop
  run_desktop sh "$script" --desktop --version 1.2.3
  [ "$rc" -eq 0 ] && [ "$(app_version)" = "1.2.3 x86_64" ] && [ ! -e "$work/bin/smind" ] &&
    has "$work/curl.log" "smind-desktop-1.2.3-macos-x86_64.app.tar.gz" &&
    ! has "$work/curl.log" "smind_1.2.3" && has "$work/out" "Installed sMind v1.2.3" && no_scratch_left
}

test_desktop_picks_the_arm64_app_on_apple_silicon() {
  reset_desktop
  run_desktop env FAKE_UNAME_M=arm64 sh "$script" --desktop --version 1.2.3
  [ "$rc" -eq 0 ] && [ "$(app_version)" = "1.2.3 arm64" ]
}

test_desktop_prefers_native_arm64_under_rosetta() {
  reset_desktop
  run_desktop env FAKE_UNAME_M=x86_64 FAKE_TRANSLATED=1 sh "$script" --desktop --version 1.2.3
  [ "$rc" -eq 0 ] && [ "$(app_version)" = "1.2.3 arm64" ]
}

test_desktop_honours_version_env_and_flag_wins() {
  reset_desktop
  run_desktop env SMIND_VERSION=4.5.6 sh "$script" --desktop
  [ "$rc" -eq 0 ] && [ "$(app_version)" = "4.5.6 x86_64" ] || return 1
  reset_desktop
  run_desktop env SMIND_VERSION=4.5.6 sh "$script" --desktop --version=v1.2.3
  [ "$rc" -eq 0 ] && [ "$(app_version)" = "1.2.3 x86_64" ] && ! has "$work/curl.log" "v4.5.6"
}

test_desktop_latest_resolves_via_release_redirect() {
  reset_desktop
  run_desktop env FAKE_LATEST=4.5.6 sh "$script" --desktop
  [ "$rc" -eq 0 ] && [ "$(app_version)" = "4.5.6 x86_64" ]
}

test_desktop_replaces_an_existing_app() {
  reset_desktop
  seed_old_app
  run_desktop sh "$script" --desktop --version 4.5.6
  [ "$rc" -eq 0 ] && [ "$(app_version)" = "4.5.6 x86_64" ] &&
    [ ! -e "$work/apps/sMind.app/Contents/Resources-marker" ] && no_scratch_left &&
    [ "$(find "$work/apps" -maxdepth 1 -name '*.app' | wc -l | tr -d ' ')" = 1 ]
}

test_desktop_checksum_mismatch_installs_nothing() {
  reset_desktop
  run_desktop sh "$script" --desktop --version 7.1.0
  [ "$rc" -ne 0 ] && has "$work/err" "checksum mismatch" && [ ! -e "$work/apps/sMind.app" ] && no_scratch_left
}

test_desktop_checksum_mismatch_leaves_existing_app_untouched() {
  reset_desktop
  seed_old_app
  run_desktop sh "$script" --desktop --version 7.1.0
  [ "$rc" -ne 0 ] && has "$work/err" "checksum mismatch" &&
    [ "$(cat "$work/apps/sMind.app/Contents/Resources-marker")" = previous ] && [ ! -s "$work/xattr.log" ] && no_scratch_left
}

test_desktop_missing_app_asset_installs_nothing() {
  reset_desktop
  seed_old_app
  run_desktop sh "$script" --desktop --version 7.3.0
  [ "$rc" -ne 0 ] && has "$work/err" "download failed" && [ -f "$work/apps/sMind.app/Contents/Resources-marker" ] && no_scratch_left
}

test_desktop_rejects_an_archive_without_an_app_bundle() {
  reset_desktop
  seed_old_app
  run_desktop sh "$script" --desktop --version 7.1.1
  [ "$rc" -ne 0 ] && has "$work/err" "does not contain an app bundle" &&
    [ "$(cat "$work/apps/sMind.app/Contents/Resources-marker")" = previous ] && no_scratch_left
}

test_desktop_refuses_while_running_and_never_kills() {
  reset_desktop
  seed_old_app
  run_desktop env FAKE_SMIND_RUNNING=1 sh "$script" --desktop --version 1.2.3
  [ "$rc" -ne 0 ] && has "$work/err" "sMind is running" && has "$work/pgrep.log" "sMind" &&
    [ ! -s "$work/kill.log" ] && [ ! -s "$work/curl.log" ] &&
    [ "$(cat "$work/apps/sMind.app/Contents/Resources-marker")" = previous ]
}

test_desktop_clears_quarantine_on_the_installed_app_only() {
  reset_desktop
  run_desktop sh "$script" --desktop --version 1.2.3
  [ "$rc" -eq 0 ] && [ "$(cat "$work/xattr.log")" = "-dr com.apple.quarantine $work/apps/sMind.app" ]
}

test_desktop_on_linux_is_not_supported() {
  reset_desktop
  run_desktop env FAKE_UNAME_S=Linux sh "$script" --desktop --version 1.2.3
  [ "$rc" -ne 0 ] && has "$work/err" "only supported on macOS" && [ ! -s "$work/curl.log" ] && [ -z "$(ls -A "$work/apps")" ]
}

test_desktop_with_service_exits_2() {
  reset_desktop
  run_desktop sh "$script" --desktop --service
  [ "$rc" -eq 2 ] && has "$work/err" "--desktop installs the app only" && [ ! -s "$work/curl.log" ]
}

test_desktop_help_mentions_the_flag() {
  run_install sh "$script" --help
  [ "$rc" -eq 0 ] && has "$work/out" "--desktop" && has "$work/out" "SMIND_APP_DIR"
}

tests="$(sed -n 's/^\(test_[a-z0-9_]*\)() {$/\1/p' "$0")"
for t in $tests; do
  if "$t"; then check "$t" 0; else check "$t" 1; fi
done

echo
echo "$passes passed, $fails failed"
[ "$fails" -eq 0 ]
