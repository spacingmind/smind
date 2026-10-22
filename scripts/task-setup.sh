#!/bin/sh
# Sample sMind workspace setup script (ADR-0024) for this repository: a fresh
# task worktree has no node_modules, so install the web and mobile
# dependencies from their lockfiles. Install it with:
#
#   smind workspace setup set <workspaceId> --setup-file scripts/task-setup.sh
#
# sMind runs it with `sh -c` in the task worktree, so it only uses POSIX sh and
# relative paths. Re-running is safe: the installs are lockfile-exact and a
# directory whose lockfile is missing is skipped. A missing package manager is
# a warning, not a failure, so a Go-only task is not blocked by a PATH without
# bun or npm.
set -eu

install_in() {
	dir=$1
	lock=$2
	tool=$3
	shift 3
	if [ ! -f "$dir/$lock" ]; then
		echo "task-setup: $dir/$lock not found, skipping $dir" >&2
		return 0
	fi
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "task-setup: warning: $tool not on PATH, skipping $dir" >&2
		return 0
	fi
	echo "task-setup: $tool $* in $dir"
	(cd "$dir" && "$tool" "$@")
}

install_in web bun.lock bun install --frozen-lockfile
install_in mobile package-lock.json npm ci
