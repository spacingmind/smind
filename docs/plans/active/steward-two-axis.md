# Steward two-axis review: Standards and Spec in two fresh contexts (ADR-0054)

Status: active (2026-11-08), **spec only until the Human accepts ADR-0054's decisions D1–D10**. Binding text:
[`docs/decisions/0054-steward-two-axis-review.md`](../../decisions/0054-steward-two-axis-review.md) (Proposed; amends ADR-0047 §1, §3, §4, §8, §9).

Size: L (7) — scope 2 (daemon + web + mobile + CLI), PRs 2 (four, sequential, not stacked), risk 1 (verdict authority is unchanged; a second seat), decisions 1 (ten, each with a
recommendation), verification 1 (UI frames, fake-LLM seat run).

## Acceptance Criteria

Schema and combine (P1, daemon, flag-gated)
- AC1 With `steward.twoAxis` false or absent, a review is byte-for-byte today's: one run, no pass row, no `axes` on the wire (a golden test over today's review tests).
- AC2 `steward_review_passes` is a new table appended to `schema.sql` (no `migrate.go` entry), unique per `(review_id, axis)`.
- AC3 The stored verdict is `Clamp(min over the submitted axis verdicts, gates)`; neither axis can raise a verdict past a failing gate; the daemon never writes `ACCEPTED`; `Clamp` itself is unchanged.
- AC4 A pass that ends `blocked` (timeout, budget, crashed, cancelled) never yields `ACCEPTED`; the verdict is capped at `CHANGES` with the failed axis named; both failed = `BLOCKED`.
- AC5 `SeatVerdict` gains optional `axis` and `criteria[]`, `SeatFinding` gains optional `axis` and `rule`; parsing stays strict and every new field has a cap in `SchemaCaps`/`ArrayCaps` and the tool schema.
- AC6 The verdict's `axis` must equal the axis bound to the pass token; a Standards finding without a `rule` is rejected; a Spec `ACCEPTED` needs a `criteria` entry for every extracted criterion (`met`/`unverifiable` with evidence ≥ 10 non-space chars), the frame observations and gate acks; a Standards `ACCEPTED` needs none of the last two.

Digest, prompts, seats (P2, daemon)
- AC7 With the flag on, the digest gains `standards` (ordered files with path, bytes, sha256, truncated; configured rules; `changedInDiff`) and `spec` (sources found/missing/unavailable, criteria ≤ 60, `namedTestsInTree`), within the 64 KiB cap and the same shrink order.
- AC8 Standards files come from a fixed ordered list plus `steward.standards.files`; a symlink leaving the tree is rejected; they are read at the **merge-base** (never head, D11): the daemon copies the base versions into `.smind-standards/` in the pass's scratch tree (outside the index), the digest carries their base hashes and a capped `headDiff`, a PR editing `AGENTS.md` is judged against the base text, and an unresolvable base ref blocks the pass; contents are never inlined in the prompt.
- AC9 Spec resolution is deterministic in the daemon in the order contract → plan file → PR body (`Spec:`, `Closes|Fixes|Refs #N`, `ADR-NNNN`); an issue fetch failure is `unavailable`, never a pass failure; no spec at all skips the Spec pass (`skipped`, `no spec available`, cost 0).
- AC10 Each pass is a separate seat run: own scratch worktree at the same sha, own task, chat, run, single-use token hashed in the pass row, own `Watch` (budget, minutes); both start together after the gates are decided; an unknown gate starts neither.
- AC11 Per-pass budgets sum to at most `steward.budgetUsd`; a config that exceeds it is a load error; a pass over its own cap is `BLOCKED: budget`.
- AC12 The prompt of each axis is built from the digest's objective fields and fixed text only: nothing from `untrusted`, the PR, commits, plan or standards text, or any path reaches it; both seats have the unchanged tool catalogue (no shell, write, web, task, permission or message tool) and the seat floor.
- AC13 Rails count a review once (hourly, per task); auto-pause counts a review, not a pass; cancel stops both passes, blocked rows written before the runs are stopped.
- AC14 `steward-standards` is seeded idempotently by `smind profile seed roles`; an absent profile falls back to `steward`; the pass row records the model actually used.
- AC15 `RunScopedMethods` gains no new steward method; the existing two stay bound to the seat's task and token.

Wire, inbox, UI, CLI (P3 web + CLI, P4 mobile)
- AC16 `steward.get|list` return `axes[]` (`axis, status, seatVerdict, summary, blockedReason, model, costUsd, runId, stewardTaskId, criteria?, specSources?`) and `findings[].axis|rule`, absent for a single-pass review; old fields keep today's meaning; a client without the new fields renders a coherent review.
- AC17 The inbox `review` item carries optional `axes:[{axis,verdict}]`; supersession, staleness and `mergeReady` are unchanged; summary ≤ 160 chars, redacted.
- AC18 Web card: stored verdict chip, two axis rows (chip with icon and text, summary, model, cost), `Skipped · no spec available`, findings grouped by axis then file with a text axis label, Spec criteria checklist, `standards.changedInDiff` chip, copy-findings carries the axis; single-axis reviews render as today.
- AC19 Mobile: the same, one column, 44 pt targets, criteria collapsible, two axis chips on the inbox row.
- AC20 CLI: `smind steward show` prints an `AXES` block and `[standards]`/`[spec]` tags (`--json` carries the fields); `smind steward review <task> --axis standards|spec` re-runs one axis; no approve or merge subcommand.

Pilot (P5)
- AC21 Fake-LLM end to end in an isolated `SMIND_HOME`: both ACCEPT → one `review` row ACCEPTED; Standards CHANGES + Spec ACCEPTED → CHANGES; CI failing + both ACCEPT → CHANGES; one pass killed → never ACCEPTED; no spec → Spec `skipped`.
- AC22 One real run with `claude-haiku-5-5` only, isolated home and port, on a throwaway repo; the per-axis cost is recorded. The Human's pilot on a real workspace is **not** done by an agent.

## Test Scenarios

P1 (`internal/steward`, `internal/store`): `TestCombine_MinOverAxes`, `TestCombine_AxisCannotRaiseAboveGates`, `TestCombine_FailedAxisNeverAccepted`, `TestCombine_BothFailedIsBlocked`,
`TestClamp_UnchangedForSinglePass`, `TestSeatVerdict_AxisMustMatchToken`, `TestSeatVerdict_StandardsFindingNeedsRule`, `TestSeatVerdict_SpecAcceptedNeedsEveryCriterion`,
`TestSeatVerdict_UnknownFieldsStillRejected`, `TestSeatVerdict_SchemaCapsCoverNewFields`, `TestStore_PassRowsUniquePerAxis`, `TestView_AxesAbsentWhenSinglePass`, `TestTwoAxisOff_ReviewIsByteIdentical` (golden).

P2 (`internal/steward`, `internal/wsapi`, `internal/config`, `internal/profiles`, `cmd/smind`; the names as implemented):
- Digest and standards: `TestDigest_StandardsFilesBaseHashesAndChangedFlag`, `TestDigest_StandardsHeadDiffCappedAndUntrusted`, `TestDigest_StandardsRulesCapped`, `TestDigest_TwoAxisOffIsUnchanged`, `TestDigest_TwoAxisSectionsShrinkBeforeExistingOnes`, `TestStandards_ReadAtMergeBaseNotHead`, `TestStandards_SymlinkAndDirSymlinkRefused`, `TestStandards_OrderCapsAndUnsafePaths`, `TestStandards_BaseRefUnresolvedIsAnError`, `TestStandards_CopyRefusesAChangedFile`.
- Spec: `TestSpec_SourcesResolveInOrder`, `TestSpec_ContractBeatsPlanOnSameID`, `TestSpec_PlanAcceptanceCriteriaParsed`, `TestSpec_IssueRefIsUnavailable`, `TestSpec_ADRMentionFoundOrMissing`, `TestSpec_NoSpecHasNoSources`, `TestSpec_BadIDsAndTestNamesDropped`.
- Prompts: `TestPrompt_UntrustedInjectionNeverReachesPromptBody` (table, both axes), `TestPrompt_AxisBriefsAreDistinctAndNameTheirAxisOnly`.
- Seats and rails (`steward_twoaxis_e2e_test.go`, fake LLM, isolated home): `TestTwoAxisE2E_BothAcceptedStoresAccepted` (two seats, own tasks/runs/tokens/scratch, base-text copy), `TestTwoAxisE2E_StandardsChangesSpecAcceptedIsChanges`, `TestTwoAxisE2E_CIFailureClampsBothAccepted`, `TestTwoAxisE2E_OnePassKilledNeverAccepted` (also: one review counted once, no auto-pause strike), `TestTwoAxisE2E_BothPassesKilledBlocksAndCountsOneFailure`, `TestTwoAxisE2E_NoSpecSkipsTheSpecPass`, `TestTwoAxisE2E_UnresolvedBaseBlocksStandardsNeverJudgesTheHead`, `TestTwoAxisE2E_StandardsProfileFallsBackAndRecordsTheModel`, `TestTwoAxisE2E_CancelWritesBothRowsBeforeStoppingAndIsNotACrash`, `TestTwoAxisE2E_TokensAndSeatsAreBoundToTheirPass`, `TestTwoAxisE2E_GateUnknownStartsNeitherPassUntilCIDecides`, `TestTwoAxisSeats_FloorAndCatalogueAreTheSingleSeatsForBothAxes`, `TestTwoAxisOff_ReviewHasNoPassesAndOneSeat`; watches: `TestWatch_PassBudgetBlocksOnlyThatPass`, `TestWatch_PassTimeoutAndCrash`, `TestWatch_PassStopsWhenTheReviewWasDecidedWithoutIt`, `TestWatch_PassScopeViolationBlocksTheWholeReview`, `TestWatch_PassAfterVerdictWaitsForTheRunThenPostChecks`.
- Collector: `TestStewardCollect_TwoAxisInputsReadStandardsAtTheMergeBase`, `TestStewardCollect_TwoAxisOffByDefault`, `TestStewardCollect_TwoAxisSpecInputs`, `TestStewardCollect_TwoAxisPlanPathCannotEscape`.
- Config, role pack, tool schema: `TestStewardConfig_PassBudgetsOverTotalRejectedOnlyWhenTwoAxis` and the other `TestStewardConfig_*` pass and standards tests, `TestRolePack_SeedsStewardStandardsIdempotently`, `TestRolePack_StewardSeatUnchanged`, `TestStewardVerdictSchema_FlagOffIsByteIdentical`, `TestStewardMCPEntry_BothAxesKeepTheSameTwoTools`, `TestStewardAxisVerdictSchema_CarriesTheAxisFieldsAndCaps`; the existing handler-map walks prove no new `RunScopedMethods` entry.

P3 (`internal/inbox`, `web/packages/ui`, `cmd/smind`): `TestInbox_ReviewItemCarriesAxesAdditively`, `TestInbox_ReviewSupersessionUnchanged`, `steward-review-card.test.tsx` (two axis rows, skipped, disagreement, criteria, old single-axis payload,
findings by axis then file, copy-findings axis), `steward-types.test.ts` (decoders tolerate missing/unknown axis fields), `TestCLI_StewardShowPrintsAxes`, `TestCLI_StewardReviewAxisFlag`.

P4 (`mobile`): review screen unit tests for the axis rows and the criteria section; smoke stage `85-steward-review` extended with a two-axis fixture.

P5: AC21, AC22 above.

Mutations (committed work, one at a time, restored with `git checkout -- <file>`; each named test must then fail): standards read at head instead of the merge-base → `TestStandards_ReadAtMergeBaseNotHead`; the combine uses `max` → `TestCombine_MinOverAxes`; a failed axis counts as ACCEPTED →
`TestCombine_FailedAxisNeverAccepted`; the axis is not checked against the token → `TestTokenAxis_StandardsSeatCannotSubmitSpec`; the budget sum check removed → `TestConfig_PassBudgetsOverTotalRejected`;
plan text is copied into the prompt → `TestPrompt_UntrustedInjectionNeverReachesPromptBody`; the flag off still writes pass rows → `TestTwoAxisOff_ReviewIsByteIdentical`.

## PR split and write scopes

Sequential, each from `origin/develop`, never stacked, each waits for the Human's review. P1 and P2 are daemon-only, behind `steward.twoAxis` (default false), so nothing changes for anyone until the Human opts in.

| PR | Scope | Write scope |
|---|---|---|
| P1 | pass table, `SeatVerdict`/`SeatFinding` fields, per-axis checks, combine; no seat start yet | `internal/store`, `internal/steward` |
| P2 | digest sections, per-axis prompts, two seats, watches, config, role-pack profile, rails | `internal/steward`, `internal/wsapi/steward*`, `internal/config`, `internal/profiles`, `internal/runs` (seat source only) |
| P3 | `View.axes`, inbox `axes`, web card, CLI | `internal/wsapi`, `internal/inbox`, `web/packages/ui`, `cmd/smind` |
| P4 | mobile review screen and inbox chips | `mobile/` |
| P5 | fake-LLM end-to-end, haiku-only live run, measured cost recorded in the ADR | `internal/wsapi` tests, `docs/` |

UI PRs (P3, P4) need light and dark frames at 1280 and 390, every transition frame, from the final commit, and a second read of the card by someone who did not write it.

## Decisions

Eleven decisions D1–D11 with recommendations are in the ADR ("Decisions for the Human"); this section records the accepted answers once the Human gives them.

## Progress

- 2026-11-08 Phase 1: ADR-0054 (Proposed) and this spec, docs only. Implementation does not start until the decisions are answered.
- 2026-11-08 Decisions D1-D11 accepted. P1 implemented (schema + combine, daemon, flag off). Deviations from the spec text, decided while building:
  - AC5 is met with parallel types, not by editing the single-pass ones: `AxisVerdict`/`AxisFinding`/`CriterionResult` and `AxisSchemaCaps`/`AxisArrayCaps`. The single-pass seat's tool schema is built from `SeatVerdict`, `SchemaCaps` and `ArrayCaps`, so adding fields there would change what a flag-off seat sees; `TestSeatVerdict_SchemaCapsCoverNewFields` pins that they stay unchanged. P2 builds the two-axis seat's tool schema from the new types.
  - ADR §4's "`CheckSeatVerdict` gains an axis argument" is implemented as `CheckAxisVerdict` over the parallel types, on purpose (same reason as AC5).
  - No pass produced a verdict (both failed, or one failed and the other skipped) is `BLOCKED` with the first failed pass's reason, `crashed` as the fallback when none carries one.
  - A pass blocked with `scope_violation` blocks the whole review at once (reason `scope_violation`, the review's scope-violation flag set, no wait for the other pass); `TestCombine_ScopeViolationBlocksWholeReview`, `TestPassVerdict_ScopeViolationBlocksWholeReview`, `TestPassVerdict_ScopeViolationDoesNotWaitForTheOtherPass`.
  - The axis and pass-status strings live once, in `internal/store`; `internal/steward` aliases them and has a named `Axis` type.
  - `View.findings[]` is now `[]AxisFinding` (the seat's finding plus optional `axis`, `rule`), JSON-identical for a single-pass review (golden).
  - `Digest.Spec` carries only `criteria[]` in P1 (the verdict path needs the ids); P2 fills the rest of the section. `steward.twoAxis` exists in config; nothing reads it yet because nothing starts a pass.
  - A Spec `ACCEPTED` needs at least one criterion to judge, `WritePassSkipped` is spec-only and refused while criteria exist, and `SubmitVerdict` (single-pass) is refused on a review that has pass rows (found by the independent review). P2 relaxes the "at least one criterion" rule only if it adds a `spec.sources` field that says the spec exists but has no parseable criteria.

## Validation

Phase 1 is docs only: no tests apply. Checked: ADR number 0054 is the next free on `origin/develop` and in open PRs; every code reference in the ADR's Context section exists on `origin/develop`
(`internal/steward/{clamp,verdict,prompt,watch,rails,digest}.go`, `internal/wsapi/steward_seat.go`, `internal/profiles/rolepack.go`, `internal/store/schema.sql`, the web card, the mobile screen,
`cmd/smind/steward.go`); every ADR cited (0037, 0039, 0041, 0045, 0047) exists; each acceptance criterion has a named test or a stated manual check.

- 2026-11-08 P2 implemented (digest, prompts, two seats, config, role pack, collector), still behind `steward.twoAxis` (default false). Deviations from the spec text, decided while building:
  - Spec resolution lists `Closes|Fixes|Refs #N` as an `issue` source with status `unavailable`: smind's forge client has no issue API yet, so the issue body is not fetched. Everything else in the order is resolved (contract, plan, PR body `Spec:` line, ADR files).
  - Standards files are read through git's object database at the merge-base (`ls-tree` mode check, `cat-file blob`), never the working tree, which subsumes `realpath`: a symlink, a path through a symlinked directory, or a path outside the repository cannot be read. The configured extra paths are additionally validated at load.
  - A Spec ACCEPTED needs a result for every criterion, or, when the daemon resolved a spec source but no parseable criterion, an empty `criteria` list (the P1 rule "at least one criterion" is relaxed exactly that far via `SpecSection.HasSpec`). A pass can only be skipped when `HasSpec` is false.
  - The per-pass prompt carries counts, ids and gate states only; criterion ids, file names, standards paths and every text field stay out of it (`TestPrompt_UntrustedInjectionNeverReachesPromptBody`).
  - `steward.review {axes}` and the CLI `--axis` flag are P3, not here.
  - Found by the independent review and fixed: the seat's scratch tree is the PR's head checkout, so a change could commit its own `.smind-standards` (a symlink out of the tree, or extra files posing as standards): `WriteStandardsCopies` now removes whatever is there and writes into a fresh directory; the Standards pass also removes the head copies of the standards files (it can read only the base copies) and both passes remove `.claude` and `.mcp.json` from the head checkout, so a change cannot plant settings, hooks or tools a seat's CLI would load by itself. A `Spec:` line in the PR body is listed as one fixed, unresolved source (no author text outside the untrusted block, never counted as a spec). The pass watch also stops a run that overspends after its verdict, so the pass caps bound the sum.
  - Known and not changed here: the single seat (flag off) still runs in an unpruned head checkout, so a PR-planted `.claude` reaches it as it does today; this is a separate hardening item.

### P1 (2026-11-08)

Tests: every P1 test in the list above, plus `TestCombine_SkippedAndOpenPasses`, `TestPassVerdict_*` (both accepted, axes disagree, gate clamps, failed axis never accepted, both failed blocked, skipped spec, incomplete and held verdicts consume nothing), `TestPassSkipped_OnlySpecAndOnlyWithoutCriteria`, `TestSeatVerdict_SpecAcceptedNeedsCriteriaToJudge`, `TestSubmitVerdict_RefusedOnTwoAxisReview`, `TestStewardConfig_TwoAxisDefaultsOffAndReadsYAML`. `TestTwoAxisOff_ReviewIsByteIdentical` is a golden generated on the code before the change (its own first commit).
Mutations (committed work, one at a time, restored with `git checkout -- <file>`), each killed by the named test: combine uses max, failed axis counted ACCEPTED, axis not bound to the token, Clamp lets `not_stacked` cap only at CHANGES, Clamp ignores a failing gate, single-pass submit allowed on a two-axis review, flag-off view carries `axes`, standards finding needs no rule, spec accepts a missing criterion, standards pass skippable, spec ACCEPTED with no criteria.

### P2 (2026-11-08)

Tests: every P2 test in the list above, plus `TestStandards_*` (merge-base read, symlinks refused, order and caps, unresolved base, changed-file refusal), `TestWatch_Pass*`, `TestTwoAxisE2E_*` (both ACCEPT, Standards CHANGES + Spec ACCEPTED, red CI clamps, one pass killed, both killed, no spec, unresolved base, profile fallback, cancel, token and seat binding), `TestTwoAxisSeats_FloorAndCatalogueAreTheSingleSeatsForBothAxes`, `TestStewardCollect_TwoAxis*`, `TestStewardVerdictSchema_FlagOffIsByteIdentical` (golden generated on `origin/develop`), `TestStewardMCPEntry_BothAxesKeepTheSameTwoTools`, and the existing handler-map walks (no new `RunScopedMethods` entry).
Mutations (committed work, one at a time, restored): standards copied from the head, collector reads standards at the head, prompt carries the PR title, handler takes the axis from the wrong pass, cancel leaves pass rows open, pass budgets not checked against the total, collector always carries the two-axis inputs, a spec is always found, the seat env drops the axis, an unresolved base falls back to judging, any task owns a review, the single seat's tool schema takes the axis document, the pass watch ignores a decided review, pass cost never rolls in; each killed by its named test.
Review round: `TestStandards_HeadSmindStandardsDirIsReplacedNotFollowed`, `TestStandards_PruneScratchRemovesHeadStandardsAndAutoloads`, `TestWatch_PassBudgetAlsoBoundsSpendAfterTheVerdict`, `TestSpec_PRBodySpecLineIsFixedAndUnresolved`, `TestSafeRepoPath`; mutations: a planted `.smind-standards` not replaced, head standards not pruned, no budget stop after the verdict, a `Spec:` line counted as found, `SafeRepoPath` allowing `..`, each killed.

### Open items after the P2 review (not built)

- Per-axis digest filtering: one digest serves both passes, so the Standards seat can read plan and issue text and the Spec seat can read the standards diff. Filter `steward_digest_get` by the caller's axis.
- The single seat (flag off) still runs in an unpruned head checkout, as before: a PR-planted `.claude`, `.mcp.json` or instruction file reaches it. A separate hardening item that needs its own flag-off golden.
- A pass whose run never starts has an elapsed time of zero, so `maxMinutes` never fires (the same weakness as the single watch).
- An unresolved merge-base is written as `crashed` on the Standards pass and so counts toward the auto-pause when the other pass fails too. Arguable; `DaemonBlockedReasons` has no reason for "refused to run".

Round 2 (Steward's read): `PruneScratch` walks each path component with `Lstat` and removes the first symlink itself (it previously used `os.RemoveAll`, which follows an intermediate symlink: a committed `docs` symlink made it delete a file outside the tree); both passes prune CLAUDE.md, CLAUDE.local.md, `.claude`, `.mcp.json`, the other providers' autoload files and directories and AGENTS.md at any depth, case-insensitively, without descending into symlinked directories, and the Standards pass also prunes the default and configured standards paths. An unreadable config at start blocks the review, a failed Lead snapshot blocks both passes, a pass left open under a closed review is closed at the next start, and every intentionally ignored error on the pass paths is logged. The e2e tests wait with `poll` and `waitWatches` instead of sleeping.
