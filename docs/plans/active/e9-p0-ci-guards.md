# E9 P0: test-determinism guard and public-docs gates

Size: S+ (5) — scope 1 (two Go hygiene tests plus data files), PRs 2, risk 1 (a guard that is too strict blocks every PR), decisions 2 (both answered below as recommendations), verify 1.

Status 2026-11-15: **spec only.** Code starts from a fresh `origin/develop` after the `wave/desktop` landing PR merges; until then this file is the only change. Branch for this spec: `smind/e9-p0-spec`.

## Why

- CI has failed a different `internal/wsapi` or `internal/server` timing test on unrelated PRs (each passes locally); the only defence today is "rerun it". Fixed sleeps are the usual cause, and nothing stops a new one from landing.
- Public text keeps drifting: a product name or a local path slips into a README, a public page or a committed ADR and is caught by a person at review time.

## What already exists on `origin/develop` (checked 2026-11-15, tip `91c0246aa`)

- `internal/hygiene/listeners_test.go`: the one repo-wide hygiene test (no wildcard listeners in tests and harnesses). The new guards live next to it.
- `cmd/smind/docs_drift_test.go`: for `public-docs/` and `README.md` it already checks relative links and anchors, front matter, that every `smind ...` command exists, the generated CLI block, and `forbidden` strings (`/Users/`, `sk-`, `ADR-`, `PR #`, `#NNN`) outside "Further reading". It does NOT check product names, does not cover `CONTRIBUTING.md`, `docs-site/`, `CHANGELOG.md` or committed ADRs for names/paths, and never checks that a backticked repo path exists.
- `task test` is plain `go test ./...`; CI has no repeat or race stress and no flaky list.
- Fixed sleeps in tests today: `internal/wsapi` 62 `time.Sleep` in 34 files (largest: `plugin_hooks_test.go` 7, `plugin_session_test.go` 6, `inbox_test.go` 4, `account_usage_test.go` 4); `internal/server` 8 in 4 files. No product-name gate and no backticked-path gate exist.
- Measured (script over README, CONTRIBUTING, AGENTS, SECURITY, `docs/*.md`, `public-docs`, `docs-site`, `docs/decisions`): 89 backticked repo paths that do not exist, 88 of them in `docs/` (mostly ADRs and plans that cite files since removed) and one in `AGENTS.md` (`docs/research/local/`, intentionally gitignored); local-path strings (`/Users/`, `/home/x`) appear in `docs/homelab.md` (5), `public-docs` (3), and 6 ADRs. These numbers fix the starting baselines; the PR re-measures them with the real test, not this script.

## Acceptance criteria

**Part A: test-determinism guard (PR 1)**

- A1. A hygiene test `TestNoNewFixedSleepsInTimingTests` counts `time.Sleep(` calls per `_test.go` file in `internal/wsapi` and `internal/server` and compares each file to `internal/hygiene/testdata/sleep-baseline.txt` (one `path count` line per file, sorted).
- A2. Ratchet, both directions: a file above its baseline (or a new file with any sleep) fails with the file, the count and "wait on a condition (poll with a deadline, a channel or the existing helper) instead"; a file BELOW its baseline also fails with "lower the baseline to N" so the baseline can only shrink and a fixed test cannot silently regrow.
- A3. A sleep that is the point of the test (asserting that something does NOT happen within a window) carries `// sleep-ok: <reason>` on its line and is not counted; an empty reason fails the test.
- A4. The failure message names the file to edit (the baseline) and never asks anyone to raise it without a reviewer; raising a number is a visible diff in a data file.
- A5. The guard needs no network, daemon or build tags and runs in `go test ./...` and so in `task test` and CI (verified by a CI run on the PR).
- A6. The PR converts at least the five wsapi files with the most sleeps from fixed sleeps to condition waits (the baseline shrinks by that amount, shown in the diff); the rest stay in the baseline for follow-ups, each one tracked as a line in the baseline file, not a TODO.

**Part B: public-docs gates (PR 2)**

- B1. No other product names. `TestPublicTextHasNoForeignProductNames` scans the public text set (A: `README.md`, `CONTRIBUTING.md`, `CHANGELOG.md`, `SECURITY.md`, `public-docs/`, `docs-site/` pages that are not generated, the landing source, and committed ADRs under `docs/decisions/`) for the denylist and fails with file:line and the matched word. Integrated provider names (the providers smind itself drives) are not on the list.
- B2. No local paths. The existing `forbidden` check is extended to the same set (B1's set, ADRs included) for `/Users/<x>`, `/home/<x>` outside a clearly marked example (`/home/user`, `/home/you` stay allowed), `C:\Users\`, and `/Volumes/`; an inline `<!-- docs-gate: allow -->` on the previous line permits one reviewed exception.
- B3. Backticked repo paths exist. `TestBacktickedRepoPathsExist` takes inline code spans that start with a repo top-level directory (`cmd/`, `internal/`, `web/`, `docs/`, `desktop/`, `mobile/`, `scripts/`, `public-docs/`, `docs-site/`, `.github/`, `plugin-examples/`, `docker/`) with no spaces or glob characters, strips a trailing `:line` or `:line-line`, and requires the path to exist (relative to the repo root or to the file). `docs/research/local/` and the other gitignored directories are on a fixed allowlist inside the test.
- B4. Scope of B3: `README.md`, `CONTRIBUTING.md`, `AGENTS.md`, `SECURITY.md`, `docs/*.md`, `public-docs/`, `docs-site/` pages. Committed ADRs and plans are history and are NOT in B3's hard scope; they go into a shrinking baseline (`internal/hygiene/testdata/docs-path-baseline.txt`, `path -> missing`), with the same ratchet as A2 (a fixed link must be removed from the baseline).
- B5. The same ratchet applies to B1 and B2 findings that already exist on `develop` (6 ADRs, `docs/homelab.md`, three `public-docs` lines): each is either fixed in the PR or listed in a baseline; none is silently allowed.
- B6. The denylist is NOT committed in plain text (it would itself name the products). `internal/hygiene/testdata/foreign-products.sha256` holds the SHA-256 of each lowercased word token, one per line; the test tokenises the text, hashes each token and compares. A failure prints the word found, never anything about the list. Adding a name is `printf '%s' name | tr A-Z a-z | shasum -a 256`.
- B7. Every test in A and B has a negative test in the same package proving it fires on a seeded bad input (a temp tree with a new sleep; a file with a path and a product name; a missing path) and stays quiet on a clean one.

## Named tests

`TestNoNewFixedSleepsInTimingTests`, `TestSleepBaselineCanOnlyShrink`, `TestSleepOKNeedsAReason`, `TestFixedSleepGuardFiresOnSeededTree`, `TestPublicTextHasNoForeignProductNames`, `TestForeignProductListIsHashedOnly`, `TestPublicTextHasNoLocalPaths`, `TestBacktickedRepoPathsExist`, `TestDocsPathBaselineCanOnlyShrink`, `TestDocsGatesFireOnSeededInput`.

## Mutation checks (each must make the named test fail)

- Add a `time.Sleep(50 * time.Millisecond)` to any `internal/wsapi/*_test.go` → `TestNoNewFixedSleepsInTimingTests`.
- Delete one sleep from a baselined file without lowering the baseline → `TestSleepBaselineCanOnlyShrink`.
- Add a product name's hash to the list and put the word in `README.md` → the names test; remove the hash → it passes (proves it reads the list, not a constant).
- Add `` `internal/nosuchdir/x.go` `` to `README.md` → `TestBacktickedRepoPathsExist`.
- Add `/Users/someone/x` to `docs/decisions/0001-*.md` → the local-paths test.

## Decisions

- D1 (answered, recommended): the sleep guard is a static count with a two-way ratchet, not a runtime repeat-run job. Reason: it is deterministic, free, runs in every `go test ./...`, and a repeat-run job would itself be a flaky-time sink. A repeat-run stress job (`go test -count=N -race` on the two packages, non-blocking, weekly) is a possible follow-up, not part of this item.
- D2 (answered, recommended): product names are hashed, the list is built once from local material (the local research notes and reference clones), and public text is never allowed to name them; integrated provider names stay allowed because they are not on the list. No decision of the Human is open.
- Not decided here, out of scope: tri-state verify (the epics plan's P1 line), and any change to the docs-drift generated CLI block.

## Out of scope

- Fixing every flaky test (only the five worst files in A6).
- Changing `forbidden` semantics for `public-docs` beyond adding to the set.
- Any code before the landing PR merges.

## Progress

- 2026-11-15: spec written, develop checked. No code.
- 2026-11-15: PR 2 (docs gates). Differences from the spec text: (1) plans (`docs/plans/`) are not scanned by B3 at all, not baselined, because they cite files that were later moved by nature and churn when a plan moves to `completed/`; committed ADRs are baselined (30 findings, file + path, no line number so edits above do not churn it); (2) `internal/pkg.Symbol` and `file.go:12-20,30` / `file.go:Symbol` forms are understood (the package directory or file must exist); (3) the local-path gate ignores repo paths that merely contain `home/` and treats `/home/smind` (the homelab service account) and `/home/user`, `/Users/you` style names as placeholders; (4) there is no separate landing source in the repo, so B1's set is README, CONTRIBUTING, CHANGELOG, SECURITY, CODE_OF_CONDUCT, `public-docs/`, `docs-site/` pages and committed ADRs. Fixed on the way: one real account name in ADR-0018 (now `/home/<user>/...`) and a stale package name in the roadmap. Product list: 7 digests; none of the repo's public text contained any of them (the gate is armed for the next one).
- State at wind-down (2026-11-15, one-Lead directive): PR 1 (sleep guard) is open as #952, accepted and frozen. PR 2 (docs gates) is committed locally on `smind/e9-docs-gates` and NOT pushed; its PR text is `~/.smind/dogfood/e9/pr2-body.md`. Next step, only when told: plain-merge fresh `origin/develop` into the branch (it was one commit behind), re-run `task test` plus the hygiene mutations, push, open the PR against `develop` with that body. Worktrees: `~/.smind/worktrees/4/e9-pr1` (PR 1, frozen) and `~/.smind/worktrees/4/e9-pr2` (PR 2). Scratch lives under `~/.smind/dogfood/e9/` and `/private/tmp/e9tmp` (TMPDIR for tests); nothing is half-done.

## Validation

Filled in when the PRs land: the baseline sizes before and after, the CI run that proves the guard runs in `task test`, and the mutation results.
