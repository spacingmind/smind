# Validator seat and Human inbox (ADR-0045)

Status: active (2026-10-27). Binding text: [`docs/decisions/0045-validator-and-inbox.md`](../../decisions/0045-validator-and-inbox.md) (Accepted, all 12 answers as recommended).

Size: XL as one item (9) → split into five L/M PRs below, one Lead, sequential, non-stacked. Scope 2, PRs 2, risk 2 (authority: who may issue a verdict; permissions surface), decisions 0 (ADR accepted), verification 2 (UI screenshots, fake-LLM seat run).

## Acceptance Criteria

Contract (PR1)
- AC1 `validation.contract.submit` parses a YAML front matter (strict subset: no anchors, tags or merge keys; duplicate keys and unknown fields are errors), validates it against the JSON Schema and stores an immutable row; a task's versions are 1, 2, …; the current contract is the highest version.
- AC2 Submit refuses, with a field-path message: oversize (front matter > 64 KiB, body > 24 KiB), over item caps (40 ACs, 80 tests, 20 mutations, 24 frames, 20 commands), an AC with no covering test/mutation/frame, an unknown referenced id, a mutation `expectFail` naming a test not in `tests`, a path with `..` or absolute, `writeScope.paths` wider than a directory prefix, `limits.budgetUsd` above `validation.maxBudgetUsd` (default ceiling 5; per-contract default 1.5).
- AC3 `validation.contract.get` returns the stored copy; the markdown body is stored for humans but is **not** part of what `get` returns to a validator identity.

Verdict (PR1)
- AC4 `validation.verdict.submit` is accepted only with the validation's one-time token (stored hashed), from the run bound to the validation, once; a Lead, a Peer, a wrong run id, a reused or missing token, or a run that already ended are refused. Not in `RunScopedMethods`' generic allow; the MCP catalogue walk asserts no Lead-visible verdict tool.
- AC5 Completeness at submit: `pass` needs a passing item for every contract test, mutation, command, frame and AC; `fail` needs a failing item plus a finding or excerpt; `blocked` needs `blockedReason` and a summary. An incomplete `pass` is rejected naming the missing item. Evidence paths may not escape the evidence root; evidence files are copied under `SMIND_HOME/validation/<id>/` and paths rewritten. A frame item needs an observation; an AC resting only on frames is rejected unless the contract marks it `visual: true` with a reason.
- AC6 A verdict stores `commitSha` and the contract sha256. `merge-ready` is computed on read: latest verdict `pass`, `commitSha` equals the branch head, the contract is the task's highest version, no `scope_violation`. A push (head moves) or a newer contract version makes it stale and never merge-ready. There is no merge RPC.
- AC7 The daemon may itself write only `blocked` verdicts with `timeout`, `budget` or `crashed`; never `pass`/`fail`.
- AC8 CLI: `smind validation contract submit|show`, `validation list|show` (`--json`).

Seat (PR2)
- AC9 `validation.request` (the task's own run or the Human) creates a child task with a detached scratch worktree at the exact sha (a 40-hex that must be an ancestor of the branch head), a new chat, and a run whose profile is `validator`; one validation at a time per task; refused for a run-scoped caller when the tree is over budget (a Human request is never refused).
- AC10 The validator's first prompt is built by the daemon from the stored contract's structured fields, the diff pointer and fixed instructions only; the Lead supplies no free text; the contract body, Peer `notes` and the Lead's chat never reach it. Its MCP entry carries `validation_contract_get` and `validation_verdict_submit` and nothing else (no `task_new`/`task_send`/profile/permission tools); the token is only in that entry's env.
- AC11 Daemon post-check: the Lead worktree's `HEAD` and `git status --porcelain` digest are recorded before and after; a change marks the verdict `scope_violation` (blocked, not merge-ready). A mutation item must say `restored: true` and the scratch tree must equal `commitSha` at the end.
- AC12 Validator run times out or ends without a verdict → the daemon writes `blocked: timeout`/`crashed`; a rate-limit wait does not consume the wall clock; `limits.budgetUsd`/`maxMinutes` default 1.5/30, ceilings 5/120 from config `validation.*`.
- AC13 Flaky rule: fail-then-pass = `pass` with `attempts: 2` and a `flaky` flag; fail-twice = `fail`; a `quarantine` test failing yields `pass` with a finding.
- AC14 Role pack seeds `validator` (claude-native `claude-sonnet-5-5`, `auto`, `childPolicy: none`, `onRateLimit: wait`) instead of `reviewer`; `smind profile seed roles` stays idempotent and never rewrites an existing profile. The `smind-leader` text and `docs/WORKSPACE_PROTOCOL.md` describe the contract-then-request flow.

Inbox (PR3–5)
- AC15 `inbox.list/count` derive items on read: `permission` (live pending requests), `verdict` (latest validation per task: merge-ready pass, or fail/blocked; stale ones drop out), `stall` (the persisted stall marker). Table `inbox_state` holds only Human actions and is garbage-collected when a key no longer derives. Item summaries are redacted (≤ 160 chars, no command text for a permission beyond tool name and target path).
- AC16 `inbox.seen/snooze/resolve`: allowed for direct and relay Human sessions; refused for run-scoped agents and for the `automation` origin; `snooze` and `resolve` refused for `perm:` keys; snooze ≤ 24 h. A pending permission is always counted in the badge.
- AC17 `inbox.changed {}` is published, coalesced at 250 ms, when a permission appears/resolves, a verdict lands, the stall marker changes, or a state setter runs; no new push kind.
- AC18 Answering a permission from the inbox calls the existing `run.respondPermission` unchanged; the inbox has no approve-all, no merge button, no "always allow".
- AC19 Web: `/inbox` page + sidebar entry with badge; permission card is the task page's component (full detail, path and diff); keyboard j/k/Enter/s/e (never `e` on a permission); polite live region for count changes; light+dark at 1280 and 390; every transition frame reviewed.
- AC20 Mobile: Inbox entry with badge, one column, 44 pt rows, the permission card full-screen, same actions; narrow-layout screenshots.

## Test Scenarios

PR1 (`internal/validation`, `internal/store`, `internal/wsapi`, `cmd/smind`)
- `TestContractSubmit_ValidStoresVersion1`, `TestContractSubmit_SecondSubmitIsVersion2`, `TestContractSubmit_RejectsDuplicateKeyAnchorTagUnknownField`, `TestContractSubmit_RejectsACWithNoCheck`, `TestContractSubmit_RejectsPathEscapeAndWideWriteScope`, `TestContractSubmit_RejectsOverCaps`, `TestContractSubmit_RejectsBudgetAboveCeiling`, `TestContractGet_OmitsBodyForValidator`.
- `TestVerdictSubmit_RequiresToken`, `TestVerdictSubmit_TokenOnceOnly`, `TestVerdictSubmit_LeadCannotWriteVerdict`, `TestVerdictSubmit_WrongRunRefused`, `TestVerdictSubmit_AfterRunEndRefused`, `TestVerdictSubmit_PassWithMissingItemRejected`, `TestVerdictSubmit_FrameWithoutObservationRejected`, `TestVerdictSubmit_EvidencePathEscapeRejected`, `TestVerdictSubmit_FailNeedsFailingItem`, `TestVerdictSubmit_BlockedNeedsReason`, `TestVerdictSubmit_FrameOnlyACRejectedUnlessVisual`.
- `TestMergeReady_PassAtHead`, `TestMergeReady_StaleShaNotReady`, `TestMergeReady_SupersededContractNotReady`, `TestMergeReady_ScopeViolationNotReady`, `TestNoMergeRPC` (handler map walk), `TestValidationRPCs_MCPCatalogueHasNoVerdictToolForLead`.
- `TestValidationList_RelayCanRead`, `TestContractSubmit_RunScopedOnlyOwnTask`.

PR2 (`internal/validation`, `internal/runs`, `internal/profiles`, `cmd/smind`, fake-LLM harness)
- `TestRequest_CreatesScratchWorktreeAtSha`, `TestRequest_RefusesNonAncestorSha`, `TestRequest_SingleFlightPerTask`, `TestRequest_OverBudgetRefusedForAgentNotHuman`, `TestValidatorPrompt_ContainsOnlyStructuredFields`, `TestValidatorPrompt_NeverContainsBodyOrLeadNotes`, `TestValidatorMCPEntry_OnlyTwoTools`, `TestValidatorMCPEntry_TokenOnlyInValidatorEnv`, `TestPostCheck_LeadWorktreeChangedIsScopeViolation`, `TestTimeout_WritesBlockedTimeout`, `TestRateLimitWaitDoesNotConsumeWallClock`, `TestFlaky_FailThenPassFlagged`, `TestFlaky_FailTwiceFails`, `TestSeedRoles_ValidatorReplacesReviewer`, `TestSeedRoles_ExistingReviewerKept`.
- Fake-LLM end-to-end: `TestValidatorSeat_E2E_FakeLLM` (Lead requests, fake validator submits a pass with evidence, parent is woken with the json block, `validation.get` says merge-ready, then a new commit makes it stale).

PR3 (`internal/inbox`, `internal/store`, `internal/wsapi`)
- `TestInboxList_DerivesPermissionVerdictStall`, `TestInboxList_StaleVerdictDropsOut`, `TestInboxList_RedactsPermissionSummary`, `TestInboxState_GCWhenKeyGone`, `TestInboxSnooze_Max24h`, `TestInboxSnooze_RefusedForPermission`, `TestInboxResolve_RefusedForPermission`, `TestInboxSetters_RefusedForRunScoped`, `TestInboxSetters_RefusedForAutomationOrigin`, `TestInboxSetters_AllowedOverRelay`, `TestInboxCount_PermissionAlwaysCounted`, `TestInboxChanged_CoalescedOnPermissionVerdictStall`, `TestInboxReads_OpenOverRelay`, `TestNoInboxApproveAllMethod` (handler walk).

PR4 web (`web/packages/ui`): `inbox page renders kinds in order`, `permission row uses the task page permission card`, `no resolve/snooze on a permission row`, `badge count follows inbox.changed`, `keyboard j/k/Enter/s/e`, `live region is polite`, query-key + drift tests; light/dark × 1280/390 frames and transition contact sheets (≤ 12 images each) at `/tmp/validator-inbox-shots/`.

PR5 mobile (`mobile/src`): `inbox list model`, `badge from inbox.count`, `permission row not snoozable`, narrow-layout screenshots.

Mutations (one per load-bearing test, listed in each PR body): token check removed → `TestVerdictSubmit_LeadCannotWriteVerdict`; `sha != head` flipped → `TestMergeReady_StaleShaNotReady`; completeness check removed → `TestVerdictSubmit_PassWithMissingItemRejected`; run-scoped refusal removed from `inbox.resolve` → `TestInboxSetters_RefusedForRunScoped`; permission guard removed from `inbox.snooze` → `TestInboxSnooze_RefusedForPermission`; contract body leaked into the prompt → `TestValidatorPrompt_NeverContainsBodyOrLeadNotes`; post-check removed → `TestPostCheck_LeadWorktreeChangedIsScopeViolation`.

## PR split and write scopes

Non-stacked: each PR branches from `origin/develop`; a PR that needs an earlier one waits for its merge.

| PR | Scope | Files (provisional) | Needs |
|---|---|---|---|
| P0 | this plan | `docs/plans/active/validator-inbox.md` | — |
| P1 contract + verdict + RPC/CLI | model, storage, RPCs, CLI | new `internal/validation/`; new `internal/store/validation.go`; **shared:** append tables to `internal/store/schema.sql`; **shared:** one line each in `internal/wsapi/handlers.go` (handler map) and `internal/wsapi/session.go` (`RunScopedMethods`); new `internal/wsapi/validation.go`; `internal/config` (`validation.*`); new `cmd/smind/validation*.go`; events topic `validation.updated` in `internal/wsapi/events.go` | — |
| P2 seat | request, scratch worktree, prompt, MCP entry, post-check, timeouts, seed, fake-LLM test, skill/protocol text | `internal/validation/*` (seat), `internal/runs`, `internal/profiles/rolepack.go`, `cmd/smind/mcp_*validation*.go`, skill `smind-leader`, `docs/WORKSPACE_PROTOCOL.md` | P1 merged |
| P3 inbox daemon | read model + RPCs + event | new `internal/inbox/`; new `internal/store/inbox.go`; **shared:** append `inbox_state` to `schema.sql`; **shared:** handler map line; `internal/wsapi/events.go` (`inbox.changed`) | P1 merged (verdict kind); independent of P2 |
| P4 web | `/inbox`, sidebar entry, badge | `web/packages/ui/src/...` inbox files, sidebar, query-keys | P3 merged |
| P5 mobile | Inbox entry | `mobile/src/...` | P3 merged |

**Shared files and sequencing.** The automations lane edits `internal/store/schema.sql`, `internal/store/migrate.go`, `internal/wsapi/events.go` and `server.go`; plugins PB.2 edits store schema and `server.go`. This work adds **new tables only** (`CREATE TABLE IF NOT EXISTS`, appended at the end of `schema.sql`), so **no `migrate.go` entry** and no change to an existing table: the `purpose = validation` marker is the presence of a `validations` row, not a column on `tasks`. Handler-map, `RunScopedMethods`, `events.go` topic and `knownTopics` edits are one-line additions; I rebase on `origin/develop` before each PR and resolve any conflict in those files there. If a lane merges a schema or events change while one of my PRs is open, I rebase (never stack) and re-run the touched packages.

## Decisions

- ADR-0045 binds everything; no architecture is decided here. `validations` identifies validator tasks (no new `tasks` column) to keep the schema change additive and conflict-free with the other lanes.
- Verdict token: 32 random bytes, stored as sha256, delivered via the validator MCP entry env only, single use.
- Peers: none planned for P1/P3 (solo). A `peer-sonnet` may run P4/P5 screenshot loops in parallel once P3 is merged; decided then.
- Live LLM calls only with `--model claude-haiku-5-5`, and only in an isolated `SMIND_HOME` and port; all seat tests use the fake-LLM harness.

## Progress

- P0 plan: merged (#656).
- P1 contract + verdict + RPC/CLI: merged (#659). AC1-AC8.
- P2 seat: this PR. AC9-AC14 (`validation.request`, scratch worktree at the exact sha on a new task branch rather than a detached HEAD, daemon-built prompt, validator-only MCP entry with an env token, post-check, timeout/budget/crash verdicts, `validator` seed replacing `reviewer`, skill and protocol text, fake-LLM test). Deviation: the ADR check that a mutation `file` exists at `base` is not built (needs git at an arbitrary base ref); P1's schema checks the path shape only.
- P3-P5 inbox: not started.

## Validation

Filled per PR: each AC → the test that confirms it, the mutation run and its failing test, commands (`task test`, `task lint`, `go test -race` on touched packages, `cd web && bunx tsc -b packages/ui`), screenshots for UI.

### P2 (seat)

- AC9 `TestRequest_OnlyOwnTaskOrHuman`, `TestRequest_SingleFlightPerTask`, `TestRequest_RefusesNonAncestorSha`, `TestRequest_OverBudgetRefusedForAgentNotHuman`, `TestValidatorSeat_E2E_FakeLLM` (worktree at the exact sha, parent = the Lead).
- AC10 `TestValidatorPrompt_NeverContainsBodyOrLeadNotes`, `TestValidatorPrompt_E2E_HasNoLeadReasoning`, `TestValidatorMCPEntry_OnlyTwoTools`, `TestValidatorMCPEntry_TokenAndIdsComeFromEnvNotArguments`, `TestMCPServe_ValidatorEnvServesOnlyValidatorTools` (real stdio process), `TestValidatorEnv_OnlyForTheValidatorTask`.
- AC11 `TestPostCheck_LeadWorktreeChangedIsScopeViolation`, `TestPostCheck_ValidatorWritingLeadWorktreeIsScopeViolation`. A Lead that edits its own worktree while a validation runs also trips this: the Lead must wait.
- AC12 `TestTimeout_WritesBlockedTimeout`, `TestBudget_WritesBlockedBudget`, `TestCrash_RunEndedWithoutVerdictIsBlockedCrashed`, `TestRunEndWithoutVerdictIsBlockedCrashed_E2E`, `TestRateLimitWaitDoesNotConsumeWallClock`.
- AC13 not built in the daemon: the flaky retry rule (re-run a failing test once, record `attempts`/`flaky`) is delivered as validator instructions and prompt text; the completeness check already accepts `attempts: 2`. A mechanical flaky check needs the validator's own tool output, which the daemon does not see.
- AC14 `TestSeedRoles_CreatesThreeSeatsInOrder`, `TestSeedRoles_ExistingReviewerKept`, `TestSeedRoles_ValidatorNeverOverwritten`; skill and `docs/WORKSPACE_PROTOCOL.md` text.

