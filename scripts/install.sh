#!/bin/sh
# Installs the smind daemon/CLI from a GitHub Release.
#
#   curl -fsSL https://spacingmind.com/install.sh | sh
#   curl -fsSL https://spacingmind.com/install.sh | sh -s -- --version 0.11.1
#
# Options:
#   --version <v>      release to install, e.g. 0.11.1 (default: latest)
#   -h, --help         print usage and exit
#
# Environment overrides:
#   SMIND_VERSION      same as --version; the flag wins if both are given
#   SMIND_INSTALL_DIR  target directory (default: /usr/local/bin if writable,
#                      otherwise ~/.local/bin)
#
# Downloads smind_<version>_<os>_<arch>.tar.gz plus checksums.txt, verifies
# the SHA-256 (mandatory), and installs the `smind` binary. Re-running it
# installs the requested (or latest) release over the previous one.
# Linux and macOS only: on Windows use scripts/install.ps1 (PowerShell).
#
# TODO(ADR-0031 P8): `--service` (install, then `smind service install`).
set -eu

REPO="spacingmind/smind"
WINDOWS_ONELINER="irm https://raw.githubusercontent.com/$REPO/master/scripts/install.ps1 | iex"

say() { printf '%s\n' "$*"; }
err() { printf 'smind-install: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || err "required command not found: $1"; }

usage() {
  cat <<'EOF'
Usage: install.sh [--version <version>] [--help]

Installs the smind daemon/CLI from a GitHub Release (Linux and macOS).

Options:
  --version <v>   release to install, e.g. 0.11.1 (default: latest)
  -h, --help      print this help

Environment:
  SMIND_VERSION      same as --version (the flag wins if both are given)
  SMIND_INSTALL_DIR  target directory (default: /usr/local/bin if writable,
                     otherwise ~/.local/bin)
EOF
}

version="${SMIND_VERSION:-}"
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
      version="$2"
      shift 2
      ;;
    --version=*)
      version="${1#--version=}"
      shift
      ;;
    *)
      usage >&2
      printf 'smind-install: unknown option: %s\n' "$1" >&2
      exit 2
      ;;
  esac
done

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  MINGW* | MSYS* | CYGWIN*)
    err "this script is for Linux and macOS; on Windows run in PowerShell: $WINDOWS_ONELINER"
    ;;
  *) err "unsupported OS: $(uname -s)" ;;
esac

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
  "" | *[!0-9A-Za-z.+-]*) err "could not determine a valid version (got '$version')" ;;
esac

name="smind_${version}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/v${version}"

tmp="$(mktemp -d 2>/dev/null || mktemp -d -t smind)"
trap 'rm -rf "$tmp"' EXIT INT TERM

say "Downloading smind v${version} (${os}/${arch})..."
curl -fsSL -o "$tmp/$name" "$base/$name" || err "download failed: $base/$name"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt" || err "download failed: $base/checksums.txt"

expected="$(awk -v f="$name" '$2 == f || $2 == "*" f { print $1 }' "$tmp/checksums.txt")"
[ -n "$expected" ] || err "no checksum for $name in checksums.txt"
actual="$(sha256 "$tmp/$name")"
[ "$expected" = "$actual" ] || err "checksum mismatch for $name"

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
cp "$tmp/smind" "$dir/.smind.tmp.$$"
chmod 755 "$dir/.smind.tmp.$$"
mv -f "$dir/.smind.tmp.$$" "$dir/smind"

say "Installed $("$dir/smind" --version 2>/dev/null || echo "smind v$version") to $dir/smind"

case ":${PATH}:" in
  *":$dir:"*) ;;
  *)
    say ""
    say "$dir is not on your PATH. Add it with:"
    say "  export PATH=\"$dir:\$PATH\""
    ;;
esac

say ""
say "Next:"
say "  smind serve    # then open http://localhost:4648"
