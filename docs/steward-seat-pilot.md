# Steward seat pilot (S4), 2026-11-01

Evidence for ADR-0047 and `docs/plans/active/steward-seat.md` (AC29, AC30). One real review
by the steward seat on a throwaway repo, plus the fake-LLM end-to-end tests that cover the
same path without a model.

## What was run

- Isolated daemon: `SMIND_HOME=~/.smind/dogfood/steward-s4/home`, port 4963, `smind` built
  from this branch. `HOME` stayed the real one on purpose: the `claude` CLI login lives in
  the login keychain and a temporary `HOME` cannot open it (`claude auth status` reports
  `loggedIn: false` there). Only `SMIND_HOME` is isolated (and guarded).
- Guard (`lib/guard.sh`, sourced by every script): refuses unless `SMIND_HOME` resolves
  under the dogfood root, the port is not 4648/4870, the steward profile model is exactly
  `claude-haiku-5-5`, and the config holds the caps `budgetUsd: 0.30`, `maxMinutes: 8`.
- Throwaway repo: a tiny Go package (`Sum` plus its test). The Lead task's one commit,
  `feat: add stats.Average`, plants two flaws: `Average` divides by `len(xs)-1`
  (P0, `stats.go:15`) and `TestAverage` discards the result and asserts nothing
  (`stats_test.go:11`).
- One call: `smind steward review 1` with the `steward` profile re-pinned to
  `claude-haiku-5-5`. Run logs: `~/.smind/dogfood/steward-s4/pilot/` (copied to
  `/tmp/steward-seat-pilot/`: `run.log`, `review.json`, `show.{txt,json}`, `digest.json`,
  `seat-run.log`, `usage.txt`).

## What the seat found

Verdict `BLOCKED` (seat `BLOCKED`, daemon kept it), 20 s wall clock, one seat run.

Findings, all four correct and each with file, line, why and a check that shows it fixed:

1. P0 `stats.go:15`: divides by `len(xs)-1`; `{2,4,6}` returns 6, not 4. The planted flaw.
2. P1 `stats_test.go:11`: `_ = Average(...)` asserts nothing, so the bug passes. The second planted flaw.
3. P2 `stats.go:15`: empty input gives `-0`, no defined contract.
4. P2 `stats.go:1`: the task title said `--json` (a typo in the pilot's own `task new` call, not planted) but the diff has no JSON output. The seat caught it from the digest.

Did the verdict match the planted flaw? The findings did; the verdict class did not, by
design of the throwaway repo: it has no `origin/develop`, so the daemon's `not_stacked` gate
failed (base not reachable from `origin/develop`) and `pr_hygiene` failed (no PR), and the
seat followed the prompt rule "stacked means BLOCKED". The planted flaw alone is a `CHANGES`
case. The pilot therefore showed the gate-dominates-judgement rule working, not a clean
CHANGES verdict. A second run with a faked `origin/develop` would have isolated it; the plan
allowed one live call, so it was not done.

## Cost and size

- Haiku 5.5 usage (`smind usage --by model`): 1 run, 1,953 input, 4,274 output, 215,742
  cache-read, 40,629 cache-write tokens, USD 0.0126.
- Digest: 1,511 bytes (the 64 KiB cap is far away on a one-commit item).
- Review row `costUsd` stayed `0` although the run cost USD 0.0126. The review row does not
  pick up the seat run's usage, so the per-review budget (`budgetUsd`) cannot trip on cost
  today (it still has the wall-clock bound). Finding for a fix (see below).

## Prompt and tool problems

The seat needed four `steward_verdict_submit` calls; the first three were rejected by
validation before the token was consumed (so nothing was stored, as designed):

1. It passed `verdict: "BLOCKED"` (a string); the tool's `verdict` parameter is the whole
   verdict object, whose own `verdict` field is the string. The name `verdict` inside
   `verdict` is the trap. It recovered with a `ToolSearch` to reload the schema.
2. `blockedReason` was 231 bytes, the cap is 200. The cap is in the error, not in the tool
   description.
3. Fourth call accepted.

Also: `steward_digest_get` and `steward_verdict_submit` reached the seat floor and were
allowed by the engine (`policy_allow`), no human was asked. The seat read only the two
changed files (`Read` x2), no `Grep`/`Glob`. It put the gate failure first and the code
findings after, as the digest ordered them.

## Recommendations

- S2b (`steward_annotate`): not needed to judge a review; the seat is cheap enough (cents)
  and quick enough (20 s) that annotating an escalated permission request is affordable,
  but this pilot did not exercise it. Build it only after the Human's own pilot with policy
  enabled shows escalations worth annotating; keep it off by default as in AC24.
- Tool description: rename the outer parameter (for example `submission`) or state in the
  description that `verdict` is an object with a `verdict` field, and print the 200-byte cap
  for `blockedReason` and the finding fields in the schema (`maxLength`).
- Review cost: add the seat run's usage to `steward_reviews.cost_usd` so the budget rail is
  real, not only the wall clock.
- Default rails: `budgetUsd 1.5` / `maxMinutes 20` are very loose for a haiku or Sonnet seat
  on a small digest (USD 0.013 and 20 s here). Keep the ceilings; consider USD 0.50 and
  10 minutes as defaults once the cost is wired, and `maxReviewsPerHour 6` / `maxReviewsPerTask 5`
  are fine.
- A real forge item (with `origin/develop` and a PR body) is still needed to see a
  `CHANGES` verdict from a clean gate set; that belongs to the Human's pilot.

## Fake-LLM end-to-end tests (no model)

See the plan's Validation section: `TestStewardE2E_FakeLLM_AcceptedThenStale`,
`TestStewardE2E_FakeLLM_CIFailureClampsToChanges`, `TestStewardE2E_SafetyRails`
(`internal/wsapi/steward_e2e_test.go`) and `TestPolicyE2E_PushOwnBranchAllowedStashDenied`
(`internal/runs/policy_e2e_test.go`).
