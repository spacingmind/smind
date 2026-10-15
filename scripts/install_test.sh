#!/bin/sh
# Tests scripts/install.sh against a fake release server: `curl` and `uname`
# on PATH are stubs that serve local fixtures, so no network is used and the
# result is the same on Linux and macOS.
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
chmod +x "$stubs/uname" "$stubs/curl"

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

make_release 1.2.3
make_release 4.5.6
make_release 6.6.6 tamper

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

tests="$(sed -n 's/^\(test_[a-z0-9_]*\)() {$/\1/p' "$0")"
for t in $tests; do
  if "$t"; then check "$t" 0; else check "$t" 1; fi
done

echo
echo "$passes passed, $fails failed"
[ "$fails" -eq 0 ]
