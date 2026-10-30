# Steward seat: permission policy engine and review loop (ADR-0047)

Status: active (2026-10-31). Binding text: [`docs/decisions/0047-steward-seat.md`](../../decisions/0047-steward-seat.md) (Proposed; every decision D1–D18 is a Supervisor default
2026-10-31 under the Human's directive, Human may veto; a veto changes the PR named in the ADR's table).

Size: XL as one item (9) → split into five L/M PRs below, sequential where noted, non-stacked. Scope 2, PRs 2, risk 2 (a parser on the permission hot path; authority of a
verdict), decisions 1 (D1 amends ADR-0019, Human to read), verification 2 (UI screenshots, fake-LLM seat run).

## Acceptance Criteria

Policy engine (S1)
- AC1 With `steward.policy.enabled` false (default) or absent, the permission path is byte-for-byte today's: no audit row, no new resolution, no behaviour change (a golden test over the existing permission tests).
- AC2 Policy file v1 (`$SMIND_HOME/steward/policy.yaml`) is strict YAML (no anchors, tags, merge keys; duplicate keys and unknown fields are errors). The loader rejects with a field-path message: `*` in `seats`/`tool`/`argv0`/`subcommand`; an `allow` without `tool`; a mutating `allow` (Edit/Write/Bash that writes) without `paths.within`; an `allow` a floor rule would deny; `why` missing or > 160; > 200 rules; any regex field; duplicate rule ids.
- AC3 Evaluation order is by action: `deny` > `ask` > `allow`, independent of file order; no matching rule = `ask`; an `allow` needs a rule covering **every** simple command of a compound Bash request.
- AC4 The shell splitter handles quotes, `;`, `&&`, `||`, `|`, `&`, redirects, `$(…)`, backticks, subshells, here-docs and here-strings; heredoc bodies are never matched as commands and are elided (length only) in audit and card; anything unparsed (unbalanced quote, `eval`, `bash -c` non-literal, process substitution, unresolvable env command) makes the request `ask`.
- AC5 Paths are `realpath`-resolved (a missing path via its deepest existing parent; a resolution failure = `ask`); `paths.within` is compared after resolution, so a symlink or `..` escape is not within.
- AC6 The floor denies, with the floor id in the denial text, even against an allow rule and even when the policy file is invalid: `no-stash`, `no-force-push`, `no-merge`, `no-install-state`, `no-secrets`, `no-destructive-outside`, `no-curl-sh`, `no-real-home-tests`, `no-add-all` (ADR-0047 §2.1 table, each with positive and near-miss cases).
- AC7 `git push origin <taskBranch>` is allowed by the template rule only for the matching task branch; `git push origin <other>`, `git push`, `git push origin HEAD:develop`, `git push --force-with-lease`, `git push origin +x`, `git push origin :x` are not allowed (the last three are floor denies).
- AC8 An invalid, unreadable or oversize policy file evaluates as floor + `ask` (never allow-all) and `policy.get` reports `{valid:false, error}`; a valid edit is picked up without a restart.
- AC9 Every evaluation appends a `permission_policy_audit` row (decision, ruleId, policySha, redacted normalised target ≤ 512, resolved paths ≤ 8, latency); the human answer or the timeout appends a second row linked by `requestId` (`ask_human_allow|ask_human_deny|ask_timeout_deny`); no update/delete RPC; retention sweep at 90 days.
- AC10 `permission_resolved` carries `policy_allow` / `policy_deny` with `ruleId`; an unanswered escalated request is still auto-denied after 5 minutes (never allowed, any mode) and is audited.
- AC11 No RPC, MCP tool or run-scoped method can write the policy; reads (`policy.get|test|audit`) work over the relay; `policy.test` explains each segment's decision without side effects.
- AC12 CLI: `smind policy init` (commented template with **zero active allow rules**, refuses to overwrite), `show`, `test`, `audit` (`--json`).

Steward seat and review loop (S2)
- AC13 `steward.review {taskId, commitSha?}` is accepted only from the Human (direct or relay) or the daemon's own trigger; refused for run-scoped agents, Peers and the `automation` origin; creates a detached scratch worktree at the exact sha (a 40-hex that is an ancestor of the branch head), a new chat and a run on profile `steward`; one review at a time per task.
- AC14 The daemon builds the digest (§3) deterministically with no agent: binaries, churn, files-vs-writeScope, commit subjects, stacking, PR title/body sections, CI state at head (forge status), validator verdict at head, frames evidence (path, capturing sha, matrix), Peer handoff blocks, secret-scan counts, tests-changed ratio, policy-audit counts. The digest is ≤ 64 KiB; free text from Lead/Peer/PR is length-capped inside an "untrusted" delimiter.
- AC15 The seat's MCP entry carries `steward_digest_get` and `steward_verdict_submit` (and `steward_annotate` only when `steward.annotate`) and nothing else; the token is in that entry's env only, hashed at rest, single use; a Lead, Peer, wrong run, reused token or ended run is refused; the MCP catalogue has no steward verdict tool for a Lead.
- AC16 The seat has no Bash and no write tool; its own permission requests resolve through the engine's seat floor (Read/Grep/Glob inside the scratch tree allowed, all else `deny`).
- AC17 `steward.verdict.submit` requires, for `ACCEPTED`, an acknowledgement of every gate and an `observation` per frame; `CHANGES` requires findings each with file, line, why and the check that shows it fixed; `BLOCKED` requires `blockedReason`. Incomplete = rejected naming the missing item.
- AC18 **Clamp:** the stored `verdict` is the minimum of the seat's verdict and the gates (failing gate → at most `CHANGES`; `not_stacked`/`no_secrets` failing → `BLOCKED`); CI `pending` holds the review `pending` and publishes nothing; the seat's value is kept as `seatVerdict`; the daemon never writes `ACCEPTED`.
- AC19 Daemon-written `BLOCKED` only for `timeout`, `budget`, `crashed`; wall clock excludes rate-limit waits; the Lead worktree `HEAD` and `git status --porcelain` digest are checked before/after (`scope_violation`).
- AC20 A review is stale when the head moves, the contract version changes or a newer review exists; `mergeReady` is computed on read; there is no merge RPC (handler-map walk).
- AC21 Triggers/rails: `steward.autoReview` default off; fires once for a root Lead `done` run with a branch; dedupe by `(headSha, contractSha, ciState)`; `maxReviewsPerHour` 6, `maxReviewsPerTask` 5, USD 1.5 / 20 min defaults (ceilings 5 / 60); auto-pause after 3 consecutive crashed/timeout reviews until the Human resumes; `steward.enabled` default false.
- AC22 Role pack seeds `steward` as the fourth profile (claude-native `claude-sonnet-5-5`, `default`, `childPolicy: none`, `onRateLimit: wait`); `smind profile seed roles` stays idempotent and never rewrites an existing profile; `smind-leader` and `docs/WORKSPACE_PROTOCOL.md` describe the review.
- AC23 CLI: `smind steward review|list|show|cancel` (`--json`); no approve/merge subcommand.
- AC24 (S2b, last) with `steward.annotate` on, `steward_annotate` on an escalated pending request stores `recommendation ∈ {approve,deny,unsure}` + reason ≤ 160; it never answers, snoozes or alters the timeout; the seat's input is the engine's normalised request only.

Inbox UI (S3)
- AC25 `inbox.list/count` derive kind `review` (key `review:<reviewId>`) from the latest non-stale review; it **replaces** that task's `verdict` row; stale ones drop out; summary ≤ 160 chars redacted; `ACCEPTED`/`CHANGES`/`BLOCKED` groups and attention per ADR-0047 §5; setters refused as in ADR-0045 AC16; `inbox.changed` coalesced on `steward.updated`.
- AC26 The `permission` card shows the matched rule id + `why` (or "no rule matched") and the normalised command with heredocs elided; no approve-all, no "always allow", no merge button; policy-allowed/denied requests are **not** inbox items.
- AC27 Web: review card (verdict chip icon+text, gates checklist failing-first, findings by file, evidence frames inline, "copy findings", "open PR", "resolve"); Settings → Permission policy read-only (validity banner, rules table, recent audit, the on-disk path); light+dark at 1280 and 390; every transition frame reviewed; polite live region.
- AC28 Mobile: the same rows and card, one column, 44 pt, no hover-only action; narrow-layout screenshots.

Pilot (S4)
- AC29 Fake-LLM end-to-end in an isolated `SMIND_HOME`: Lead finishes → review enqueued → seat submits `ACCEPTED` → inbox shows one `review` row → a new commit makes it stale; with CI failing the verdict clamps to `CHANGES`.
- AC30 One real run with `claude-haiku-5-5` only, isolated home and port, on a throwaway repo; the Human's pilot (policy enabled for one workspace) is **not** done by an agent.

## Test Scenarios

S1 (`internal/permpolicy`, `internal/runs`, `internal/store`, `internal/wsapi`, `cmd/smind`)
- Loader: `TestPolicyLoad_ValidV1`, `TestPolicyLoad_RejectsDuplicateKeyAnchorTagUnknownField`, `TestPolicyLoad_RejectsWildcardSeatToolArgv`, `TestPolicyLoad_RejectsMutatingAllowWithoutPathScope`, `TestPolicyLoad_RejectsAllowThatFloorDenies`, `TestPolicyLoad_RejectsRegexAndOverCaps`, `TestPolicyLoad_InvalidFileMeansFloorPlusAsk`, `TestPolicyReload_PicksUpEditWithoutRestart`.
- Engine: `TestDecide_DisabledIsNoop`, `TestDecide_PrecedenceDenyAskAllowIgnoresOrder`, `TestDecide_UnmatchedIsAsk`, `TestDecide_AllowNeedsEverySegmentCovered`, `TestDecide_CompoundWithAllowedThenDisallowedIsAsk`, `TestDecide_HeredocBodyNotMatchedAndElided`, `TestDecide_UnparseableIsAsk` (table: unbalanced quote, `eval`, `bash -c "$x"`, `<(…)`, `$(…)` hiding a push), `TestDecide_SymlinkAndDotDotEscapeNotWithin`, `TestDecide_MissingPathResolvedByParent`, `TestDecide_PathResolutionFailureIsAsk`, `TestDecide_SeatScopedRule`, `TestDecide_WorkspaceScopedRule`.
- Floor: `TestFloor_StashDenied`, `TestFloor_ForcePushVariantsDenied` (`-f`, `--force`, `--force-with-lease`, `+ref`, `:ref`, `--mirror`, `--delete`), `TestFloor_MergeReleaseTagPublishDenied`, `TestFloor_InstallStateDenied` (`~/.smind`, `$HOME/.smind`, `$SMIND_HOME`, `:4648`, `smind service stop`, `pkill`), `TestFloor_SecretsDenied`, `TestFloor_DestructiveOutsideWorktreeDenied`, `TestFloor_CurlPipeShellDenied`, `TestFloor_RealHomeTestsDenied`, `TestFloor_AddAllDenied`, `TestFloor_NearMissesNotDenied` (`git push origin task-branch`, `git add file.go`, `git stash` in a string literal arg of `echo`), `TestFloor_AppliesEvenWhenPolicyInvalid`, `TestFloor_BeatsAllowRule`.
- Push rule: `TestPushOwnBranch_AllowedOnlyForTaskBranch`, `TestPushOwnBranch_OtherRefsAsk`.
- Fuzz: `FuzzSplitCommand` (never panics; any input that contains a floor token inside a parsed segment is never `allow`), `TestSplitCommand_DifferentialAgainstBashN` (skipped when `bash` absent).
- Hook (`internal/runs`): `TestDecider_PolicyAllowResolvesWithoutHuman`, `TestDecider_PolicyDenyResolvesToDenyOption`, `TestDecider_AskFallsThroughToHumanPath`, `TestDecider_AskTimeoutDeniesAndAudits`, `TestDecider_PolicyOffMatchesGolden`, `TestDecider_BypassModeNeverAsksSoPolicyNotConsulted`, `TestPermissionResolved_PolicyResolutionsEncodeAndDecode`.
- Audit: `TestAudit_EveryEvaluationAppendsRow`, `TestAudit_HumanAndTimeoutOutcomeLinkedByRequestID`, `TestAudit_RedactsAndElides`, `TestAudit_AppendOnlyNoMutatingRPC` (handler walk), `TestAudit_RetentionSweep90Days`.
- RPC/CLI: `TestPolicyRPCs_ReadOnlyAndRelayCanRead`, `TestNoPolicyWriteRPCOrMCPTool` (handler map + MCP catalogue walk), `TestPolicyTest_ExplainsEachSegmentNoSideEffects`, `TestCLI_PolicyInitWritesTemplateWithZeroAllowRules`, `TestCLI_PolicyInitRefusesOverwrite`.

S2 (`internal/steward`, `internal/store`, `internal/wsapi`, `internal/profiles`, `cmd/smind`, fake-LLM harness)
- Digest: `TestDigest_BinaryDetectedFromNumstat`, `TestDigest_ChurnGenSchemasLockfile`, `TestDigest_FilesOutsideWriteScope`, `TestDigest_StackedBranchDetected`, `TestDigest_PRBaseNotDevelop`, `TestDigest_ConventionalCommitsTitle`, `TestDigest_PRBodyMissingNotDone`, `TestDigest_CIStatesMapped`, `TestDigest_ValidatorStaleShaNotPass`, `TestDigest_FramesCapturedAtOtherShaFlagged`, `TestDigest_FramesMatrixMissingCell`, `TestDigest_PeerHandoffNotVerifiedMustBeRepeated`, `TestDigest_SecretScanCountsOnly`, `TestDigest_CappedAt64KiB`, `TestDigest_UntrustedTextDelimitedAndCapped`.
- Request/seat: `TestReview_OnlyHumanOrDaemon`, `TestReview_RefusedForRunScopedAndAutomationOrigin`, `TestReview_CreatesScratchWorktreeAtSha`, `TestReview_RefusesNonAncestorSha`, `TestReview_SingleFlightPerTask`, `TestStewardPrompt_ContainsOnlyDigestAndFixedText`, `TestStewardMCPEntry_OnlyTwoToolsByDefault`, `TestStewardMCPEntry_TokenOnlyInSeatEnv`, `TestStewardSeat_NoBashNoWriteTool`, `TestStewardSeatFloor_ReadInScratchAllowedElseDeny`.
- Verdict: `TestVerdictSubmit_RequiresToken`, `TestVerdictSubmit_TokenOnceOnly`, `TestVerdictSubmit_LeadCannotWrite`, `TestVerdictSubmit_AcceptedNeedsAllGatesAndFrameObservations`, `TestVerdictSubmit_ChangesNeedsFindingsWithCheck`, `TestVerdictSubmit_BlockedNeedsReason`, `TestClamp_FailingGateCapsAtChanges`, `TestClamp_StackedAndSecretCapAtBlocked`, `TestClamp_SeatCannotRaiseAboveGates`, `TestClamp_CIPendingHoldsReviewPending`, `TestDaemonNeverWritesAccepted`, `TestTimeoutBudgetCrashWriteBlocked`, `TestRateLimitWaitDoesNotConsumeWallClock`, `TestPostCheck_LeadWorktreeChangedIsScopeViolation`.
- Staleness/merge: `TestReviewStale_HeadMoves`, `TestReviewStale_NewContractVersion`, `TestReviewStale_NewerReview`, `TestNoMergeRPC` (handler walk, extended).
- Triggers/rails: `TestAutoReview_OffByDefault`, `TestAutoReview_FiresOnceForRootLeadDone`, `TestAutoReview_NotForErrorStoppedOrPeer`, `TestRails_DedupeSameHeadContractCI`, `TestRails_MaxReviewsPerHour`, `TestRails_MaxReviewsPerTask`, `TestRails_BudgetCeilingRefused`, `TestRails_AutoPauseAfterThreeCrashes`, `TestSwitches_EnabledFalseAndInvalidConfigOff`.
- Seed/CLI: `TestSeedRoles_StewardFourthAndIdempotent`, `TestSeedRoles_ExistingStewardKept`, `TestCLI_StewardHasNoApproveOrMerge`.
- S2b: `TestAnnotate_OffByDefault`, `TestAnnotate_StoresEnumAndCappedReason`, `TestAnnotate_CannotAnswerOrExtendTimeout`, `TestAnnotate_InputIsNormalisedRequestOnly`.

S3 (`internal/inbox`, `internal/wsapi`, then `web/packages/ui`, then `mobile/src`)
- Daemon: `TestInboxList_DerivesReviewKind`, `TestInboxList_ReviewReplacesVerdictRow`, `TestInboxList_StaleReviewDropsOut`, `TestInboxList_ReviewSummaryRedacted`, `TestInboxList_PolicyAllowedDeniedNotListed`, `TestInboxChanged_CoalescedOnStewardUpdated`, `TestNoInboxApproveAllMethod` (still green).
- Web: `inbox review row renders verdict chip with icon and text`, `review card lists failing gates first`, `review card shows frames and copy findings`, `permission card shows matched rule and peek`, `permission card has no always-allow or approve-all`, `policy settings page is read-only`, `unknown permission resolution renders as text`, query-key + drift tests; frames at `/tmp/steward-seat-shots/` (light/dark × 1280/390, transition contact sheets ≤ 12 images).
- Mobile: `review row model`, `permission card rule line`, `policy audit read-only`, narrow-layout screenshots.

S4: `TestStewardE2E_FakeLLM_AcceptedThenStale`, `TestStewardE2E_FakeLLM_CIFailureClampsToChanges`, `TestPolicyE2E_PushOwnBranchAllowedStashDenied`, one live haiku run log at `/tmp/steward-seat-pilot/`.

### Mutations (one per load-bearing test, run on committed work, listed in each PR body)

| PR | Mutation (file, change) | Test that must then fail |
|---|---|---|
| S1 | `permpolicy/decide.go`: treat `allow` as winning over `deny` | `TestDecide_PrecedenceDenyAskAllowIgnoresOrder` |
| S1 | `permpolicy/floor.go`: drop `--force-with-lease` from the force-push set | `TestFloor_ForcePushVariantsDenied` |
| S1 | `permpolicy/split.go`: on a parse error return `allow` instead of `ask` | `TestDecide_UnparseableIsAsk` |
| S1 | `permpolicy/paths.go`: compare `within` before `realpath` | `TestDecide_SymlinkAndDotDotEscapeNotWithin` |
| S1 | `runs/decider.go`: resolve timeout to the first option instead of deny | `TestDecider_AskTimeoutDeniesAndAudits` |
| S1 | `permpolicy/load.go`: accept `allow` with no `paths.within` | `TestPolicyLoad_RejectsMutatingAllowWithoutPathScope` |
| S2 | `steward/clamp.go`: return `seatVerdict` unclamped | `TestClamp_SeatCannotRaiseAboveGates` |
| S2 | `wsapi/steward.go`: remove the token check | `TestVerdictSubmit_LeadCannotWrite` |
| S2 | `steward/digest.go`: compare frame sha with the base instead of head | `TestDigest_FramesCapturedAtOtherShaFlagged` |
| S2 | `steward/seat.go`: add `Bash` to the seat's tools | `TestStewardSeat_NoBashNoWriteTool` |
| S2 | `steward/rails.go`: remove the dedupe key | `TestRails_DedupeSameHeadContractCI` |
| S3 | `inbox/derive.go`: keep the `verdict` row when a review exists | `TestInboxList_ReviewReplacesVerdictRow` |
| S3 | web card: render an "Always allow" button | `permission card has no always-allow or approve-all` |

## PR split and write scopes

Non-stacked: each PR branches from `origin/develop`; a PR that needs an earlier one waits for its merge (the `plan` + implementation of the same PR may be one branch, never plan
commits inside a later impl branch).

| PR | Scope | Files (provisional) | Needs |
|---|---|---|---|
| S0 | this docs PR | `docs/decisions/0047-steward-seat.md`, `docs/plans/active/steward-seat.md` | — |
| **S1** policy engine + audit | pure engine, hook in the decider, audit, RPC/CLI, config | new `internal/permpolicy/` (`load.go`, `decide.go`, `floor.go`, `split.go`, `paths.go`, tests, fuzz); `internal/runs/` decider hook (one function, plus `registry.go` audit calls); `internal/taskrunner/event.go` (2 enum values + optional `PermissionDetail.Policy`); new `internal/store/policyaudit.go`; **shared:** append `permission_policy_audit` to `schema.sql`; **shared:** handler-map lines in `internal/wsapi/handlers.go`; new `internal/wsapi/policy.go`; `internal/config` (`steward.policy.*`); new `cmd/smind/policy*.go` | S0 merged |
| **S2** seat + review loop | collector, seat, verdict, clamp, rails, trigger, seed | new `internal/steward/`; new `internal/store/steward.go`; **shared:** append `steward_reviews` to `schema.sql`; handler map; new `internal/wsapi/steward.go`; `internal/profiles/rolepack.go`; new `cmd/smind/mcp_*steward*.go`, `cmd/smind/steward*.go`; skill `smind-leader`; `docs/WORKSPACE_PROTOCOL.md`; `internal/config` (`steward.*`) | S1 merged (seat floor uses the engine) |
| **S3a** inbox daemon + web | derive `review`, `permission` card fields, web inbox + settings page | `internal/inbox/`, `internal/wsapi/inbox.go`, `internal/wsapi/events.go` (one line), `web/packages/ui/src/...` (inbox, permission card, settings, query keys) | S2 merged |
| **S3b** mobile | Inbox rows/card | `mobile/src/...` only | S3a merged (decoders), may run in parallel with S4 |
| **S4** pilot | fake-LLM e2e, live haiku run, pilot doc | `internal/steward/e2e_test.go`, `docs/plans/active/steward-seat.md` Validation, `docs/` pilot note | S2 + S3a merged |
| S2b annotate | `steward_annotate`, card advisory line | `internal/steward/annotate.go`, wsapi, web card | S4 evidence in hand |

**Shared files and sequencing.** `schema.sql`, `handlers.go`, `events.go`, `rolepack.go` get appended lines only; **new tables only, no `migrate.go` entry, no `tasks` column**. Rebase on
`origin/develop` before each PR (never stack) and re-run the touched packages; S1 and the providers epic both touch `internal/config`: keep the `steward.*` block in its own file
(`internal/config/steward.go`) to avoid conflicts.

### Delegation to peer-glm (Human GLM policy: the Lead plans, GLM implements, the Lead reviews every line)

Per PR the Lead splits into briefs of one coherent step each (≤ 15 min, one commit each, exact files, named tests, "do not push, do not `gh pr create`, no `git stash`, install
nothing, touch no file not named"). Suggested S1 steps, at most two Peers in parallel on disjoint files:

1. (GLM-A) `internal/permpolicy/split.go` + `split_test.go` + `FuzzSplitCommand`: the shell splitter and heredoc elision (tests: `TestSplitCommand_*`, `TestDecide_HeredocBodyNotMatchedAndElided`).
2. (GLM-B, parallel) `internal/permpolicy/load.go` + `load_test.go`: schema structs, strict YAML, loader checks (`TestPolicyLoad_*`).
3. (GLM-A) `floor.go` + `floor_test.go` (`TestFloor_*`, table-driven from ADR §2.1; near-misses mandatory).
4. (GLM-B) `paths.go` + tests (`TestDecide_Symlink…`, `…MissingPath…`).
5. (Lead reviews 1-4 line by line, then GLM-A) `decide.go` wiring the above + `TestDecide_*`.
6. (GLM-B) `internal/store/policyaudit.go` + schema + `TestAudit_*`.
7. (GLM-A) `internal/runs` hook + `TestDecider_*` (the Lead reads the decider diff itself: it is the hot path).
8. (GLM-B) wsapi + CLI + `TestNoPolicyWriteRPCOrMCPTool`, `TestCLI_Policy*`.

A Peer that fails twice on a review finding is fixed by the Lead. S2 splits the same way (digest, rails, seat/MCP entry, verdict+clamp, seed/CLI); S3 UI frame loops go to
`peer-sonnet` only for screenshots.

## S1 engine design (binding for the S1 implementation; Lead 2026-10-31)

Package `internal/permpolicy`. Files and their exported surface (one owner per file):

- `split.go`: `SplitCommand(cmd string) Parsed`. `Parsed{Segments []Segment; Normalized string; HeredocBytes int; Unparsed []string}`; `Segment{Argv []string; Env []string; Redirects []Redirect; Dynamic bool; Raw string}`; `Redirect{Op, Target string}`. Never panics, never returns an error: doubt goes in `Unparsed` (non-empty means the caller must ask). `$(…)`/backticks/subshells/`bash -c '<literal>'` contents are parsed recursively and **appended** as extra segments (so the floor sees them) and the outer segment is `Dynamic`; a word containing `$`, a backtick, `*`, `?` or `[` makes its segment `Dynamic`. `eval`, `bash|sh|zsh -c` with a non-literal, `<(`/`>(`, unbalanced quotes, an argv0 beginning with `$` → `Unparsed`. Heredoc bodies are not segments; `Normalized` replaces each body with `<<TAG [heredoc N bytes]`.
- `request.go`: `Action` (`allow|deny|ask`), `Request`, `Decision`, `SegmentDecision` (types only).
- `paths.go`: `ResolvePath(p, cwd string) (string, error)` (abs; `~`/`$HOME`/`${HOME}` expanded from `os.UserHomeDir`; `internal/realpath.Resolve` on the deepest existing parent, then the missing tail re-joined; error = unresolvable → ask) and `Within(resolved string, roots []string) bool` (roots resolved the same way; path-separator aware, never a bare string prefix); `SegmentPaths(seg Segment, cwd string) ([]string, error)` (every non-flag word after argv0, the value of `--flag=value`, redirect targets).
- `floor.go`: `CheckFloor(req Request, p Parsed) (FloorHit, bool)`; `FloorHit{ID, Reason string}`; ids exactly the ADR §2.1 ids. Wrapper unwrapping (`env`, `command`, `nohup`, `time`, `exec`, `xargs`, `builtin`, `nice`) before matching; `sudo` is a floor hit. Also covers writes outside the worktree: an `Edit`/`Write` path, a redirect target or the target of `cp|mv|rm|tee|touch|mkdir|ln|chmod|chown|dd|install|truncate` that resolves outside the worktree **and** outside the OS temp dir → `no-destructive-outside`.
- `load.go`: `Load(data []byte) (*Policy, error)`; `Policy{Version int; Rules []Rule; Sha string}`; `Rule{ID, Action, Why string; Match Match}`; strict YAML (anchors, aliases, tags, merge keys, duplicate keys, unknown fields = error with a field path), caps, `Policy.Sha` = hex sha256 of the raw bytes. Every Bash `allow` counts as mutating and needs `paths.within`. The floor check on an `allow` (a probe segment built from `argv0 subcommand allow_only…`) is added in the decide step.
- `decide.go`: `Decide(req Request, pol *Policy) Decision`. `pol == nil` = floor + ask. Order: `Truncated` → ask; floor on the parsed segments (also when `Unparsed`, a floor hit wins) → deny; `Unparsed`/unresolvable path → ask; deny rules (any segment matches) → deny; ask rules → ask; allow only when **every** segment is covered by an allow rule, no segment is `Dynamic`, there is no redirect, every segment path and the cwd is `Within` the rule's `paths.within` (placeholders `{worktree}`, `{taskBranch}`), and `Worktree != ""`; otherwise ask. Command match: `argv0` exact, `subcommand` = the first N non-flag words (`"pr create"` is two), a flag **before** the subcommand means no match (ask), `args.allow_only` constrains every arg after the subcommand to exact literals (placeholders expanded), `args.deny_flags` exact flags that stop a match. Non-Bash tools match on `tool` + `paths`.
- `engine.go`: `Engine` (policy file path, mtime+size reload cache, `Enabled func() bool`, `AuditSink`), `Evaluate(Request) (Decision, bool)` (false = engine off, nothing audited), `RecordOutcome(requestID, outcome)`, `Status()`, `Explain(Request)` (dry run, no audit), `Rules()`. Audit text goes through `internal/learning/redact` and is capped (target 512, 8 paths).
- `template.go`: the commented template of `smind policy init` (zero active allow rules, loads valid).

Other files: `internal/store/policyaudit.go` + `schema.sql` (`permission_policy_audit`, new table, no migrate entry; `AppendPolicyAudit`, `ListPolicyAudit`, `SweepPolicyAudit`); `internal/config/steward.go` (`Config.Steward.Policy.Enabled`, default false, read per decision like `plugins.enabled`); `internal/taskrunner` (`PermissionResolvedByPolicyAllow|PolicyDeny`, `PermissionDetail.Policy`, and an optional `DenyReasonDecider` so the claude adapter's deny message carries the floor id; ACP/codex have no message channel); `internal/runs/decider_policy.go` (the hook, called after the system-run and AutoAccept branches and before the pending channel); `internal/wsapi/policy.go`; `cmd/smind/policy*.go`.

Stated limits: a provider adapter that sends no detail (Codex, an ACP non-execute/edit call, a Claude non-Bash tool) reaches the engine with an unknown tool and is `ask` unless a deny/ask rule names the tool; a command the provider truncated (`detail.Truncated`) is `ask`; OS-temp writes are not floor-denied.

## Decisions

- ADR-0047 binds everything; D1–D18 are Supervisor defaults 2026-10-31, vetoable.
- The engine package is `internal/permpolicy`, the seat package `internal/steward`; the validator's scratch-worktree/token/post-check helpers are reused if exported, else the S2 Lead
  extracts a small shared package in its own first commit (decided in S2, recorded here).
- The Claude `permissions.ask`/`deny` provider rules (#596, #632) stay; the engine does not write provider settings.
- Live LLM calls only with `claude-haiku-5-5` in an isolated `SMIND_HOME` and port; tests that choose a home use `HOME=$(mktemp -d)`.

## Progress

- S0 plan + ADR: this PR (docs only, Proposed).
- S1: in progress (Lead 2026-10-31): contract `steward-seat-s1.contract.md`, engine design above, GLM briefs in steps.
- S2–S4: not started.

## Validation

Filled per PR: each AC → the test that confirms it, the mutation run and its failing test, commands (`task test`, `task lint`, `go test -race` on touched packages, `cd web && bunx
tsc -b packages/ui`), screenshots for UI. For S1 the parser PR also needs a Windows dispatch (`windows-full-suite.yml --ref <branch> -f packages=internal/permpolicy,internal/runs`):
path and quoting code is exactly what the nightly has caught before.

### S0 (this PR)

Docs only: no tests apply. Checked: ADR number 0047 is the next free; every ADR reference (0017, 0019, 0037, 0040, 0041, 0042, 0044, 0045) and file path cited exists; each AC has a
named test scenario; the PR split has disjoint write scopes; the Human-veto defaults are listed in ADR §11.
