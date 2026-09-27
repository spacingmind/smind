# Contributing

Thanks for your interest in `smind`. This is a small, early-stage project
(1-2 maintainers, no formal governance) — the process below is
intentionally lightweight.

## Building and testing locally

```sh
task build      # build the web UI, then the smind binary (bin/smind)
task test       # go test ./... (plus the web UI's test suite)
task lint       # go vet + gofmt check
```

Run `task build && task test && task lint` — the same sequence CI runs —
before opening a PR.

## Branching model

- **`develop` is the single integration branch.** Base your feature/fix
  branch on `develop` and open your PR against `develop`.
- **Releases happen on `develop`.** release-please opens
  `chore(develop): release X.Y.Z` PRs against `develop` (version bump +
  changelog from Conventional Commits); merging one creates the `vX.Y.Z`
  tag + GitHub Release, builds the binaries, and attaches them.
- **`master` is a read-only pointer to the latest release** — the
  release workflow fast-forwards it to each release tag. Never open PRs
  against it and never push to it directly.
- Don't push tags by hand; the release PR is the only path to a tag.

### History (why this shape)

The earlier model — a release-only `master` promoted from `develop`,
synced back by a workflow — broke twice for real: the v0.6.0→v0.7.0
metadata clobber (PR #135/#136/#137), and then a structural stall:
sync-develop's own merge commits made `develop` impossible to
rebase-merge onto master (whose ruleset only allowed rebase), forcing a
125-commit squash promotion that left release-please blind ("No user
facing commits") with 187 commits stuck unreleased. The guards that
existed only patched the first failure. Single-branch releases remove
the failure mode instead of guarding it: the release commit lands on
`develop` and can't be clobbered by a promotion that no longer exists.
Full analysis: `docs/release-model-review.md`.

## Conventional Commits

PRs are squash-merged, so **the PR title becomes the commit message that
lands in the repo's history** — and eventually, when `develop` is released
into `master`, the changelog entry `release-please` generates. PR titles
must follow [Conventional Commits](https://www.conventionalcommits.org/):

- `feat: ...` — a new feature
- `fix: ...` — a bug fix
- `feat!: ...` or a `BREAKING CHANGE:` footer — a breaking change
- `chore: ...`, `docs: ...`, `ci: ...`, `test: ...` — maintenance work
  release-please excludes from the changelog

Individual commits on your feature branch don't need to follow this
strictly, but the PR title does — it feeds the release PR's version
bump and the changelog.

## Plan docs for nontrivial changes

This repo practices spec-driven development (see `AGENTS.md`, rule (c)).
For any change that spans more than a quick, obviously-bounded edit, write
a plan first: create `docs/plans/active/<slug>.md` with concrete
acceptance criteria and named test scenarios before starting
implementation, keep it updated as work proceeds, and move it to
`docs/plans/completed/` once every acceptance criterion is validated.
Small, self-contained fixes don't need one — use your judgment, and see
`AGENTS.md` for the full rule.

If a change materially affects the data model, routing behavior, the
`/ws` wire protocol, or public API shape, please open an issue or discuss
before investing in a large PR — see `AGENTS.md` rule (d).

## Opening a PR

- Target `develop`, not `master`.
- Give the PR a Conventional Commits-style title.
- Fill out the PR template checklist.
