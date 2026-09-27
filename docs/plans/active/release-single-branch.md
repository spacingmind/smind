# Release: single-branch develop (drop release-only master)

From `docs/release-model-review.md` (the analysis; this is the
implementation plan). The old two-branch model is dead in practice:
release stalled 2026-09-17, 187 commits stuck, because sync-develop's
merge commits make develop un-rebaseable onto master (rebase-only
ruleset), forcing squash promotions that blind release-please.

## Changes

1. `release-please.yml`: trigger on push to `develop`; config gains
   `target-branch: develop`; new `fast-forward-master` job moves
   `master` to the tag after a real release's assets attach (refuses
   if master ever diverges).
2. Delete `sync-develop.yml` and `ci.yml`'s release-metadata guard +
   `scripts/check-release-metadata.sh` — the failure they patched
   (promotion clobbering the release commit) can't occur without
   promotions.
3. CONTRIBUTING.md: single-branch model, release-PR flow, history
   section explaining both real incidents.

## Manual follow-ups (after merge)

- Rulesets: delete the `master` ruleset (id 21716577) or reduce it to
  read-only; keep `develop`'s. GITHUB_TOKEN pushes from
  fast-forward-master must not be blocked.
- Default branch: `develop` (if not already).

## Validation

- YAML parses; release PR opened by release-please on the next develop
  push (expected: release v0.8.0 covering the 187 stuck commits, with
  binaries attached for the first time) is the end-to-end proof.
