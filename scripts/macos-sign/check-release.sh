#!/usr/bin/env bash
# Refuses to let a release be signed unless it is a real, published, stable release whose tag commit
# is on develop. Runs BEFORE any secret is in the environment (no secret is read here), in the signing
# workflow's own download step. docs/plans/active/desktop-notarization-prep.md, ADR-0060.
#
# usage: check-release.sh --tag vX.Y.Z --repo owner/name        (run inside a clone that has the tags and
#        origin/develop; needs `gh` with a read token in GH_TOKEN, `git`, `jq`)
#
# Refuses (exit 1, one reason line) for: a tag that is not a stable vX.Y.Z; a draft or a prerelease; a
# release whose tag name differs from the asked one; a tag that does not exist locally; a tag commit that
# is not an ancestor of origin/develop (a writer's branch or a stray tag). The asset itself is proven by
# provenance.mjs (a signed checksums.txt), not here.
set -euo pipefail
tag="" repo=""
while [ $# -gt 0 ]; do
  case "$1" in
    --tag) tag="${2:-}"; shift 2 ;;
    --repo) repo="${2:-}"; shift 2 ;;
    *) echo "check-release: unknown argument $1" >&2; exit 2 ;;
  esac
done
refuse() { echo "check-release: REFUSED: $*" >&2; exit 1; }
if [ -z "$tag" ] || [ -z "$repo" ]; then echo "check-release: --tag and --repo are required" >&2; exit 2; fi
grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$' <<<"$tag" || refuse "the tag is not a stable vX.Y.Z"
info="$(gh release view "$tag" --repo "$repo" --json isDraft,isPrerelease,tagName)" || refuse "no such release"
jq -e '.isDraft == false' >/dev/null <<<"$info" || refuse "the release is a draft"
jq -e '.isPrerelease == false' >/dev/null <<<"$info" || refuse "the release is a prerelease"
jq -e --arg t "$tag" '.tagName == $t' >/dev/null <<<"$info" || refuse "the release's tag name is not $tag"
commit="$(git rev-list -n 1 "refs/tags/$tag" 2>/dev/null)" || refuse "the tag does not exist in this clone (fetch the tags)"
git rev-parse --verify --quiet refs/remotes/origin/develop >/dev/null || refuse "origin/develop is not in this clone"
git merge-base --is-ancestor "$commit" refs/remotes/origin/develop || refuse "the tag's commit is not on develop"
echo "check-release: $tag is a published stable release on develop"
