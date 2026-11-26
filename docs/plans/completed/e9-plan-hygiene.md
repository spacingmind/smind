# E9: plan hygiene (move finished plans to completed)

Size S, docs only. Governing: AGENTS.md rule (c) ("when finished, every acceptance criterion confirmed in Validation, move the file to `docs/plans/completed/`").

## Rubric

A plan moves to `docs/plans/completed/` only when ALL hold:

1. Its Validation (or Progress) section confirms every acceptance criterion, or the plan has no unchecked items and says it shipped.
2. Its work is on develop (named PRs merged, or the named files and tests exist in the tree).
3. Nothing in it is a live tracker for work that is still to come.

Otherwise it stays, with a one-line `Open: ...` note directly under its title. A `*.contract.md` file follows its parent plan (moves with it; stays with it). `mobile-relay-interop-ci-gap.md` stays ACTIVE until its implementation (the next item) ships. `single-loop/` is a directory of notes: judged as one unit.

## Acceptance criteria

- AC1 Every plan in `docs/plans/active/` has a recorded verdict (move or keep) with evidence, in a table reviewed by a fresh reviewer; at least 25 verdicts are spot-checked against the code or the Validation section by the Lead.
- AC2 Every moved plan is a `git mv` (history kept); every kept plan that was judged has an `Open:` line when it was not obviously active.
- AC3 No link to a moved file is left pointing at the old path (docs/, README, AGENTS.md, public-docs, docs-site, code comments, test data); checked by grep for each moved basename.
- AC4 `go test ./internal/hygiene/` and the docs drift tests pass; where a moved plan fixes baseline lines the baseline shrinks (the ratchet fails otherwise).
- AC5 This plan moves to `completed/` in the same PR once validated (it is the last move).

## Named checks

- `go test ./internal/hygiene/ ./internal/providers/` (docs gates, baseline ratchet, drift tests)
- `grep -rn "plans/active/<basename>"` over the repo for every moved file returns nothing
- `ls docs/plans/active | wc -l` plus `ls docs/plans/completed | wc -l` equals the counts before (135 + 123) plus nothing lost

## Method

GLM peer 1 writes the classification table to a scratch file; the Lead reviews it (25+ spot checks) and a fresh reviewer checks it; GLM peer 2 applies the moves with a script (every step asserts) and fixes links; the Lead runs the checks.

## Progress

- [x] spec (this file)
- [x] classification table (103 plans, two GLM peers), reviewed (26 spot checks by the Lead, one fresh reviewer)
- [x] moves, Closed/Open notes and link fixes applied (third GLM peer)
- [x] hygiene tests, link grep, counts

## Validation

- AC1: 103 parent plans classified (43 moved on the peers' verdicts, 14 more moved by the Lead as "shipped but the in-file boxes were never ticked", each with a Closed line naming the merged PRs, 45 kept with an Open line, the single-loop folder among them, and this plan moved last); 9 of the 33 contract files moved with their parents, 24 stayed; contracts followed their `planRef:` parent. The Lead spot-checked 26 verdicts (named tests and files exist on develop: TestSliceACPTurn, TestFloor_SelfApproveCLIDenied, TestBrowserReachDriftEveryProviderHasARow, TestFleetPeerByNameIsDeniedByAddress, TestClaudeModeMismatchNote, TestDispositionOf, TestNeverList_, TestClassify_Fixtures, reorderTabInLayout, requireSignedVersion and others; the tab e2e specs). One fresh reviewer checked 15 moves, all 14 Closed lines and 10 Open lines: it found one move needing a Closed line (pr-panel), one overclaim (browser-for-agents-pr3), one stale Open line (run-skeleton-storage), one garbled Open line (permission-heredoc-reveal) and one stale status header (qa-frames-skill); all fixed in this PR.
- AC2: 67 `git mv` (66 plus this file), all renames; active 136 -> 69, completed 123 -> 190.
- AC3: `grep -rnF "plans/active/<name>"` over the repo for each of the 66 moved names returns nothing; links in docs/ (ROADMAP, ADRs), plans, YAML `planRef` fields and a few code comments were rewritten, nothing else.
- AC4: `go test ./internal/hygiene/ ./internal/providers/` and `go test ./cmd/smind -run 'Doc|Drift'` pass; no baseline line was fixed by the moves, so the baseline is unchanged.
- AC5: this file moved to `completed/` in the last commit.
- Not verified: the PR-merge claims of the 14 Closed lines rest on `git log origin/develop --grep '(#N)'` plus the plans' own text, not on re-running their tests.
