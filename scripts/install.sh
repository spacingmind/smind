#!/bin/sh
# Installs the smind daemon/CLI from a GitHub Release.
#
#   curl -fsSL https://spacingmind.com/install.sh | sh
#   curl -fsSL https://spacingmind.com/install.sh | sh -s -- --version 0.11.1
#
# Options:
#   --version <v>      release to install, e.g. 0.11.1 (default: latest)
#   --desktop          macOS only: install the sMind desktop app instead of the
#                      CLI (see below)
#   --service          after installing, run `smind service install` (a
#                      systemd user unit on Linux, a LaunchAgent on macOS)
#   --system           with --service on Linux: a system unit that runs as the
#                      invoking user (run the script with sudo; never root)
#   -h, --help         print usage and exit
#
# Environment overrides:
#   SMIND_VERSION      same as --version; the flag wins if both are given
#   SMIND_INSTALL_DIR  target directory (default: /usr/local/bin if writable,
#                      otherwise ~/.local/bin)
#   SMIND_APP_DIR      with --desktop: where sMind.app goes (default:
#                      /Applications if writable, otherwise ~/Applications)
#
# Downloads smind_<version>_<os>_<arch>.tar.gz plus checksums.txt, verifies
# the SHA-256 (mandatory), and installs the `smind` binary. Re-running it
# installs the requested (or latest) release over the previous one.
# Linux and macOS only: on Windows use scripts/install.ps1 (PowerShell).
#
# --desktop (macOS) installs sMind.app from the release's
# smind-desktop-<version>-macos-<arm64|x86_64>.app.tar.gz, verified against the
# same checksums.txt. The app is ad-hoc signed and not notarized, so a copy
# downloaded in a browser is quarantined and Gatekeeper blocks it; curl does
# not set the quarantine flag, so installing it with this script opens without
# a prompt. It is swapped in atomically (the previous app stays until the new
# one is in place), and refuses while sMind is running -- it never kills it.
set -eu

REPO="spacingmind/smind"
WINDOWS_ONELINER="irm https://raw.githubusercontent.com/$REPO/master/scripts/install.ps1 | iex"

say() { printf '%s\n' "$*"; }
err() { printf 'smind-install: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || err "required command not found: $1"; }

usage() {
  cat <<'EOF'
Usage: install.sh [--version <version>] [--service [--system]] [--desktop] [--help]

Installs the smind daemon/CLI from a GitHub Release (Linux and macOS), or with
--desktop the sMind desktop app (macOS).

Options:
  --version <v>   release to install, e.g. 0.11.1 (default: latest)
  --service       after installing, run `smind service install` so the daemon
                  starts at login/boot (systemd user unit on Linux,
                  LaunchAgent on macOS)
  --system        with --service on Linux: install a system-wide unit that
                  runs as the invoking user (needs sudo; never runs as root):
                    curl -fsSL https://spacingmind.com/install.sh | sudo sh -s -- --service --system
  --desktop       macOS only: install the sMind desktop app (sMind.app) instead
                  of the CLI. The app is not notarized; downloading it with
                  this script avoids the Gatekeeper quarantine prompt. Refuses
                  while the sMind being replaced is running. Not combinable
                  with --service.
  -h, --help      print this help

Environment:
  SMIND_VERSION      same as --version (the flag wins if both are given)
  SMIND_INSTALL_DIR  target directory (default: /usr/local/bin if writable,
                     otherwise ~/.local/bin)
  SMIND_APP_DIR      with --desktop: where sMind.app goes (default:
                     /Applications if writable, otherwise ~/Applications)
EOF
}

# An explicit --version must carry a real value: not empty (that would
# silently fall back to latest) and not another option (`--version --help`).
set_version() {
  case "$1" in
    "" | -*)
      usage >&2
      printf "smind-install: --version needs a version, got: '%s'\n" "$1" >&2
      exit 2
      ;;
  esac
  version="$1"
}

version="${SMIND_VERSION:-}"
service=0
system=0
desktop=0
while [ $# -gt 0 ]; do
  case "$1" in
    -h | --help)
      usage
      exit 0
      ;;
    --version)
      if [ $# -lt 2 ]; then
        usage >&2
        printf 'smind-install: --version needs a value\n' >&2
        exit 2
      fi
      set_version "$2"
      shift 2
      ;;
    --version=*)
      set_version "${1#--version=}"
      shift
      ;;
    --service)
      service=1
      shift
      ;;
    --system)
      system=1
      shift
      ;;
    --desktop)
      desktop=1
      shift
      ;;
    *)
      usage >&2
      printf 'smind-install: unknown option: %s\n' "$1" >&2
      exit 2
      ;;
  esac
done

if [ "$system" = 1 ] && [ "$service" = 0 ]; then
  usage >&2
  printf 'smind-install: --system only applies together with --service\n' >&2
  exit 2
fi

if [ "$desktop" = 1 ] && { [ "$service" = 1 ] || [ "$system" = 1 ]; }; then
  usage >&2
  printf 'smind-install: --desktop installs the app only; --service/--system apply to the CLI install\n' >&2
  exit 2
fi

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  MINGW* | MSYS* | CYGWIN*)
    err "this script is for Linux and macOS; on Windows run in PowerShell: $WINDOWS_ONELINER"
    ;;
  *) err "unsupported OS: $(uname -s)" ;;
esac

if [ "$system" = 1 ] && [ "$os" != linux ]; then
  err "--system is only available on Linux (macOS uses a per-user LaunchAgent)"
fi

if [ "$desktop" = 1 ] && [ "$os" != darwin ]; then
  err "--desktop is only supported on macOS (there is no Linux desktop app; install the CLI without --desktop)"
fi

need curl
need tar

case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) err "unsupported architecture: $(uname -m)" ;;
esac

# Rosetta reports x86_64; prefer the native arm64 build on Apple silicon.
if [ "$os" = darwin ] && [ "$arch" = amd64 ] &&
  [ "$(sysctl -n sysctl.proc_translated 2>/dev/null || echo 0)" = 1 ]; then
  arch=arm64
fi

if command -v sha256sum >/dev/null 2>&1; then
  sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
  sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
  err "need sha256sum or shasum to verify the download"
fi

if [ -z "$version" ]; then
  # Resolve "latest" via the release redirect rather than the API, which is
  # rate-limited for anonymous callers.
  latest_url="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest")" ||
    err "could not resolve the latest release"
  version="${latest_url##*/}"
fi
version="${version#v}"
case "$version" in
  "" | [!0-9]* | *[!0-9A-Za-z.+-]*) err "could not determine a valid version (got '$version')" ;;
esac

base="https://github.com/$REPO/releases/download/v${version}"

tmp="$(mktemp -d 2>/dev/null || mktemp -d -t smind)"
stage=
stage_dir=
dest=
cleanup() {
  # Interrupted between the two renames of the app swap: put the previous app
  # back before the scratch directory (which still holds it) is removed.
  if [ -n "$stage_dir" ] && [ -d "$stage_dir/previous.app" ] && [ -n "$dest" ] && [ ! -e "$dest" ]; then
    mv "$stage_dir/previous.app" "$dest" || true
  fi
  rm -rf "$tmp"
  [ -z "$stage" ] || rm -f "$stage"
  [ -z "$stage_dir" ] || rm -rf "$stage_dir"
}
trap cleanup EXIT
trap "exit 130" INT
trap "exit 143" TERM

# download_verified <asset>: fetches the asset and checksums.txt into $tmp and
# checks the asset's SHA-256 (mandatory), for the CLI archive and the app alike.
download_verified() {
  asset="$1"
  curl -fsSL -o "$tmp/$asset" "$base/$asset" || err "download failed: $base/$asset"
  curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt" || err "download failed: $base/checksums.txt"
  expected="$(awk -v f="$asset" '$2 == f || $2 == "*" f { print $1 }' "$tmp/checksums.txt")"
  [ -n "$expected" ] || err "no checksum for $asset in checksums.txt"
  actual="$(sha256 "$tmp/$asset")"
  [ "$expected" = "$actual" ] || err "checksum mismatch for $asset"
}

# sMind refuses to be replaced while it runs (a running app would keep its old
# code, and the new bundle would be swapped under it). Never kills it. Only the
# app at $dest counts: an sMind running from /Applications does not block an
# install into another SMIND_APP_DIR, and a process that merely mentions the
# path (an editor, a `tail`) is not the app because the match is anchored at the
# start of the command line.
refuse_if_running() {
  need pgrep
  # pgrep -f takes an extended regex: escape the path's metacharacters.
  dest_re="$(printf '%s' "$1" | sed 's/[][\\.*^$+?(){}|]/\\&/g')"
  if pgrep -f "^${dest_re}/Contents/MacOS/" >/dev/null 2>&1; then
    err "sMind is running from $1; quit it (Cmd+Q) and run this again -- this script never closes it for you"
  fi
}

install_desktop() {
  case "$arch" in
    amd64) app_arch=x86_64 ;;
    *) app_arch=$arch ;;
  esac
  app_asset="smind-desktop-${version}-macos-${app_arch}.app.tar.gz"

  appdir="${SMIND_APP_DIR:-}"
  if [ -z "$appdir" ]; then
    if [ -w /Applications ]; then
      appdir=/Applications
    else
      appdir="$HOME/Applications"
    fi
  fi
  mkdir -p "$appdir" || err "could not create $appdir"
  [ -w "$appdir" ] || err "$appdir is not writable; set SMIND_APP_DIR"
  dest="$appdir/sMind.app"

  refuse_if_running "$dest"

  say "Downloading sMind v${version} (macos/${app_arch})..."
  download_verified "$app_asset"

  # Extract next to the destination (same filesystem, so the swap below is a
  # rename) into a scratch dir that the exit trap removes.
  stage_dir="$(mktemp -d "$appdir/.sMind-install.XXXXXX")" || err "could not create a scratch directory in $appdir"
  tar -xzf "$tmp/$app_asset" -C "$stage_dir" || err "could not extract $app_asset"
  new_app=
  for candidate in "$stage_dir"/*.app; do
    [ -d "$candidate" ] || continue
    [ -z "$new_app" ] || err "$app_asset contains more than one .app"
    new_app="$candidate"
  done
  [ -n "$new_app" ] || err "$app_asset does not contain an app bundle"
  [ -d "$new_app/Contents/MacOS" ] || err "$app_asset is not a macOS app bundle"

  # Re-check right before the swap: the download took a while.
  refuse_if_running "$dest"

  # The previous app stays in place until the new one is: move it aside (a
  # rename), move the new one in, and only then delete the old. If the second
  # rename fails the old app is put back.
  if [ -e "$dest" ]; then
    mv "$dest" "$stage_dir/previous.app" || err "could not move the existing $dest aside; it was left untouched"
    if ! mv "$new_app" "$dest"; then
      mv "$stage_dir/previous.app" "$dest" || true
      err "could not install $dest; the previous app was restored"
    fi
  else
    mv "$new_app" "$dest" || err "could not install $dest"
  fi

  # Only the app just installed: curl downloads carry no quarantine flag, so
  # this is belt and braces (e.g. an archive that kept one).
  xattr -dr com.apple.quarantine "$dest" 2>/dev/null || say "note: could not clear the quarantine flag on $dest"

  say "Installed sMind v${version} to $dest"
  if [ "$appdir" != /Applications ] && [ -e /Applications/sMind.app ]; then
    say "note: /Applications/sMind.app also exists; this install went to $appdir"
  fi
  say ""
  say "Next: open sMind from $appdir (or: open \"$dest\")"
}

if [ "$desktop" = 1 ]; then
  install_desktop
  exit 0
fi

name="smind_${version}_${os}_${arch}.tar.gz"
say "Downloading smind v${version} (${os}/${arch})..."
download_verified "$name"

tar -xzf "$tmp/$name" -C "$tmp" smind || err "could not extract smind from $name"

dir="${SMIND_INSTALL_DIR:-}"
if [ -z "$dir" ]; then
  if [ -w /usr/local/bin ]; then
    dir=/usr/local/bin
  else
    dir="$HOME/.local/bin"
  fi
fi
mkdir -p "$dir" || err "could not create $dir"
[ -w "$dir" ] || err "$dir is not writable; set SMIND_INSTALL_DIR or re-run with sudo"

# Install via a temp name + rename so a running smind is never half-written.
stage="$dir/.smind.tmp.$$"
cp "$tmp/smind" "$stage"
chmod 755 "$stage"
mv -f "$stage" "$dir/smind"

say "Installed $("$dir/smind" --version 2>/dev/null || echo "smind v$version") to $dir/smind"

case ":${PATH}:" in
  *":$dir:"*) ;;
  *)
    say ""
    say "$dir is not on your PATH. Add it with:"
    say "  export PATH=\"$dir:\$PATH\""
    ;;
esac

if [ "$service" = 1 ]; then
  say ""
  say "Installing the smind service..."
  if [ "$system" = 1 ]; then
    "$dir/smind" service install --system ||
      err "smind is installed at $dir/smind, but 'smind service install --system' failed (see above)"
  else
    "$dir/smind" service install ||
      err "smind is installed at $dir/smind, but 'smind service install' failed (see above)"
  fi
  say ""
  say "Next:"
  say "  smind service status    # then open http://localhost:4648"
  say "  smind service logs -f"
else
  say ""
  say "Next:"
  say "  smind serve    # then open http://localhost:4648"
  say "  (or run it as a service: smind service install)"
fi
