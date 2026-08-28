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

- **`master` is release-only.** It only receives merges via PR from
  `develop`, and those merges trigger `release-please` (version bump +
  changelog from Conventional Commits) and a GitHub Release. Don't open
  PRs directly against `master`.
- **`develop` is the integration branch.** Base your feature/fix branch on
  `develop` and open your PR against `develop`.

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
strictly, but the PR title does.

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
