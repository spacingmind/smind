#!/usr/bin/env bash
# Guards a develop -> master promotion PR against accidentally clobbering
# release-please's own version/changelog history. The promotion works by
# making master's tree match develop's (see CONTRIBUTING.md's branching
# model -- master's ruleset only allows rebase-merge, so it can't take a
# real merge commit) -- which silently reverts a previous release's
# manifest/changelog entries if develop never absorbed that release
# commit first. This already happened for real once (v0.6.0 -> v0.7.0:
# PR #135 clobbered it, release-please then proposed re-releasing
# everything in PR #136, fixed by PR #137).
#
# Only meaningful for a PR targeting master. Compares this checkout's
# HEAD against origin/master: fails if the manifest version would go
# backwards, or if any version heading already in master's CHANGELOG.md
# is missing from HEAD's.
set -euo pipefail

git fetch origin master --quiet

head_version="$(jq -r '.["."]' .release-please-manifest.json)"
master_version="$(git show origin/master:.release-please-manifest.json | jq -r '.["."]')"

version_lt() {
  # true if $1 < $2, comparing dotted numeric versions
  [ "$1" != "$2" ] && [ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | head -n1)" = "$1" ]
}

status=0

if version_lt "$head_version" "$master_version"; then
  echo "::error::.release-please-manifest.json version ($head_version) is behind master's ($master_version) -- this would regress the tracked release version."
  status=1
fi

while IFS= read -r heading; do
  if ! grep -qF "$heading" CHANGELOG.md; then
    echo "::error::CHANGELOG.md is missing an entry already on master: $heading"
    status=1
  fi
done < <(git show origin/master:CHANGELOG.md | grep -E '^## \[')

if [ "$status" -eq 0 ]; then
  echo "release metadata check passed (manifest $head_version >= master's $master_version, no changelog entries dropped)"
fi

exit "$status"
