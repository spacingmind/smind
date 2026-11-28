# P0 A1: Codex spend-limit failover, and the Add project path placeholder

Status: active. Size S. Daemon (one switch statement and tests) plus one copy string in the web UI. No wire, schema or UI-behaviour change.

## Context (verified in the code)

Codex rust-v0.172.0 reports an account's spend limit with three new `CodexErrorInfo` names: `apiKeySpendLimitExceeded`, `projectSpendLimitExceeded`, `organizationSpendLimitExceeded` (the project and organization limits used to arrive as `usageLimitExceeded`). `taskrunner.FailureHint` (`internal/taskrunner/failure_hint.go`) maps only `usageLimitExceeded` to `failure.CodeUsageLimit`, so a spend-limited API-key Codex run classifies as nothing: no cooldown, no fallback profile, no honest "limit" sentence.

The path from a hint to a failover, read in the code and pinned by a test below:
1. `Registry.finish` (`internal/runs/registry.go`) calls `taskrunner.FailureHint(err)` for an error run and passes the code to `failure.Classify`.
2. `Classify` maps `CodeUsageLimit` to `KindPlanWindowExhausted`; `failure.IsPlanLimit` is true for it.
3. `applyRateLimitPolicy` (`internal/runs/failover.go`) acts on a plan limit by the launching profile's `onRateLimit`: `fallback` starts a successor on the fallback profile (once per chain, user runs only), `wait` retries only with a known future reset (a spend limit carries none, so `wait` correctly does nothing: the daemon never guesses a time).
So mapping the three names to `CodeUsageLimit` is the whole change for them: they take exactly the `usageLimitExceeded` path.

## Decisions

- **D1** `apiKeySpendLimitExceeded`, `projectSpendLimitExceeded`, `organizationSpendLimitExceeded` -> `failure.CodeUsageLimit` (the same class and path as `usageLimitExceeded`).
- **D2** `inferenceHookDenied` and `bioPolicy` stay **unclassified on purpose** (hint `""`), now stated explicitly in the code and pinned by tests. Reason: no existing class fits honestly. `too_many_denials` means "many denied actions, review the permission policy" (not one hook veto, and not a provider safety refusal); the limit classes would make the daemon fail over or wait, which cannot fix a policy refusal; `auth_expired` / `model_not_in_plan` are false. An unclassified error run is never retried, never put on cooldown, never failed over (`applyRateLimitPolicy` acts only on typed kinds), and the run's own error text, the provider's own sentence, is what the person reads. A new kind would add one more value to the failure tables of the daemon, the CLI text, the web and the mobile client (about a dozen files) for a sentence the provider already supplies: **left for a decision** (recommendation: do not add one until the Human wants a dedicated action for it).
- **D3** The comments that list the names (`failure_hint.go`, `internal/codex/client.go` `codexErrorInfo` doc) name the new ones.
- **D4** Copy only: the Add project dialog's Path placeholder `/path/to/repo` becomes `/path/to/folder` (a folder does not need to be a git repo). No frames: a placeholder string.

## Acceptance criteria

- AC1 Each of the three spend-limit names maps to `failure.CodeUsageLimit` and `failure.Classify` turns it into `plan_window_exhausted`.
- AC2 A finished codex run with each of the three names, on a profile with `onRateLimit: fallback`, starts the fallback successor (end to end through `Registry.finish`), with the run's failure kind `plan_window_exhausted`; on a `wait` profile it enqueues nothing (no reset time is guessed).
- AC3 `inferenceHookDenied` and `bioPolicy` stay unclassified: no failure kind, no fallback successor, no retry item, no cooldown.
- AC4 A negative row: an unknown name (`someFutureError`) maps to `""`.
- AC5 The Add project Path placeholder reads `/path/to/folder`; no other occurrence of the old string remains in `web/`.
- AC6 `task test`, `task lint`, `go test ./internal/hygiene/`, `cd web && bunx tsc -b packages/ui`, `sh scripts/verify.sh` pass.

## Test scenarios (named)

- `internal/taskrunner/failure_hint_test.go` (the existing table): rows `apiKeySpendLimitExceeded`, `projectSpendLimitExceeded`, `organizationSpendLimitExceeded` (-> `CodeUsageLimit`), `inferenceHookDenied`, `bioPolicy` and `someFutureError` (-> `""`); plus `TestFailureHint_SpendLimitsClassifyAsPlanWindow` (each name through `failure.Classify` gives `KindPlanWindowExhausted`).
- `internal/runs/failure_policy_test.go`: `TestFinish_CodexSpendLimitsFailOverLikeUsageLimit` (the three names on a `fallback` profile: kind `plan_window_exhausted` and a successor on the fallback chat; on a `wait` profile no retry item) and `TestFinish_CodexPolicyRefusalsAreUnclassifiedAndNeverActed` (`inferenceHookDenied`, `bioPolicy`: no kind, no extra chat, no retry item, no cooldown).
- `web/packages/ui` : the test that opens the Add project dialog asserts the placeholder text `/path/to/folder` (`add_project_path_placeholder_is_a_folder`).

## Staffing

One GLM peer from a step-by-step brief, one commit per step. Lead: spec, review, mutation checks, checks, PR.

## Progress

- Spec first, then one GLM peer (three commits, one per step), reviewed and cherry-picked by the Lead.

## Validation

- Mutations by the Lead on committed code: removing `apiKeySpendLimitExceeded` from the switch fails `TestFailureHint`, `TestFailureHint_SpendLimitsClassifyAsPlanWindow` and `TestFinish_CodexSpendLimitsFailOverLikeUsageLimit/apiKeySpendLimitExceeded`; adding `bioPolicy` to the usage-limit case fails `TestFailureHint/bioPolicy`.
- `sh scripts/verify.sh` PASS, `go test ./internal/hygiene/` ok, `cd web/packages/ui && bunx tsc -b` clean, `crud-dialogs.test.tsx` 7 passed.
- No contract: S-size, a failure-class mapping and one placeholder string; no wsapi, store, permission, relay or UI-behaviour change (ADR-0045 asks a contract for those).
