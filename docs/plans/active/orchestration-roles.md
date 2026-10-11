# Orchestration roles: Supervisor → Lead → Peer natively in smind

Size: L (7) — scope 2, PRs 1, risk 1, decisions 2, verify 1.

**Status: proposed, docs-only (2026-10-11).** Nothing here is built. Per the
user's decision, the extensions are implemented **later, and only if real
use shows that profile `notes` plus
[`docs/WORKSPACE_PROTOCOL.md`](../../WORKSPACE_PROTOCOL.md) are not
enough**. Design: [ADR-0027](../../decisions/0027-profile-as-role-contract.md)
(Proposed). The acceptance criteria and test names below are the spec to
pick up when that day comes; they are deliberately light.

**Goal (user, 2026-10-11):** make the Supervisor → Lead → Peer workflow work
better natively in smind than it does today on Paseo.

**Related work, not duplicated here:**

- `task-hierarchy-tiers.md` (PR #286, another Lead, branch
  `feat/task-tree-tiers`) owns the tree UI, rollups, `task_tree`,
  `task.tree` and the tree concurrency guard (#289). This plan only adds an
  optional contract input to its `TierOf` after its PR-T lands, and edits
  none of its files.
- `orchestration-parity-paseo.md` owns D12 and D13 (answered below).
- `self-learning-skills.md` (ADR-0026, PR #288) owns learning; the role
  interaction is a note for it (section 5).

## 1. Gap table

| # | Capability | Paseo stopgap (today) | smind today | Target (ADR-0027) |
|---|---|---|---|---|
| 1 | Role instructions | Brief says "read `WORKSPACE_PROTOCOL.md`"; profile has only `notes` | Same; `systemPrompt` excluded (ADR-0014) | Profile `instructions` + bundled `skills`, delivered per spawn (Claude append-system-prompt, ACP first-prompt preface, Codex `developerInstructions`) |
| 2 | Tool policy | `paseoTools.enabled=false` on two Peer providers | `smind mcp serve` added by hand; no policy | `orchestrationTools` bool; daemon builds the MCP config; strict MCP on Claude |
| 3 | Allowed children | Lead's note says Peers only | Any `profileId` or raw provider | `childPolicy` none / leaf / profiles, enforced on `task_send`/`task_new` |
| 4 | Caller identity | Paseo parent label | Self-reported `source`/`fromTaskId` | Run-scoped token; forced parent and source; method allowlist; own-subtree scope |
| 5 | Turn limit | "20-turn limit" in a note | `agents.glm.maxTurns`, daemon-wide | Per-profile `maxTurns` (Claude, GLM); "not enforced" where a provider can't |
| 6 | Budget | none | Usage measured (ADR-0020), no cap | Per-run and per-subtree caps from `run_usage` |
| 7 | Concurrency | none | Per-tree guard (#289) | `maxConcurrentRuns` on the same guard, rooted at the caller |
| 8 | Tier label | Flat list | Structural `TierOf` (#286, not built yet) | Contract tier, structural fallback |
| 9 | Brief | Prose (7-point list in the protocol) | Prose | Optional structured `brief`; write-scope overlap warning |
| 10 | Handoff and signals | Prose; words in a message | `done` + `stopReason` | `handoff_submit`, `run_handoffs`, derived `disposition` |
| 11 | Turn-limit stop hidden as `done` | Lead reads the logs | Same (D-G5/D13) | `needs_continue` disposition, whatever the transcript claims |
| 12 | Accept/reject record | In the plan's Progress | none | `handoff_review` per run |
| 13 | Sizing | Supervisor scores in prose | none | Optional `tasks.size`; suggests profiles and a budget |
| 14 | Worktree setup | `paseo.json` (repo file) | ADR-0024, daemon-side, human-authored (built) | none: done |
| 15 | Supervisor heartbeat | Paseo heartbeat | none; schedules deferred (parity D5) | stays on Paseo or the host's `/loop` |
| 16 | Read-only reviewer | `reviewer` profile, plan mode | `plan` mode (Claude); ACP modes are agent-defined | A profile; nothing new |

## 2. When to build (the trigger)

Build the first phase below only after real use of the stopgap shows at
least one of these. Record the occurrence in Progress with the run or PR.

- A Peer spawned or messaged another agent despite its note.
- A Lead accepted work that had stopped at a turn limit or produced no edits.
- A Lead spawned a non-Peer profile, or a raw provider, for a Peer's work.
- A run or a tree overran a budget the human had agreed, undetected until
  the usage report.
- Two Peers edited the same write scope because no one saw the overlap.
- The Supervisor could not tell, from the tree, who owned a handoff.

Until then, improve the notes and `WORKSPACE_PROTOCOL.md` first.

## 3. Phases (each its own PR off `develop`; sequenced, never stacked)

Every phase is additive and a profile without a contract behaves as today.
Each runs `task test`, `task lint` and `cd web && bunx tsc -b packages/ui`;
web phases add light and dark screenshots.

**R1. Disposition (D13, no contract needed).** Derive `endReason` /
`disposition` from `StopReason` and run state; expose it on `run.list`,
`task_status`, `task_wait` and the CLI. The first PR to build, because it
needs no contract. Tests: `TestDisposition_TurnLimitIsNeedsContinue`
(ACP `max_turn_requests`, Claude max-turns result),
`TestDisposition_DoneWithEndTurnIsDelivered`,
`TestDisposition_BudgetExceeded`,
`TestMCPTaskWait_ReturnsDispositionOnTurnLimit`,
`TestDisposition_StatusEnumUnchanged` (pins that the run status values and
the queue's finished trigger do not change).

**R2. Contract data and CLI.** Nullable columns, validation in
`internal/profiles`, `profile.*`, `smind profile add|edit` flags,
`profile_list` fields. No runtime effect. Tests:
`TestMigrate_ProfileContractColumns`, `TestProfile_ContractRoundTrip`,
`TestProfile_PeerForcesChildPolicyNone`,
`TestProfile_InstructionsOverCapRejected`,
`TestProfile_UnknownSkillRejected`, `TestProfile_LegacyProfileUnchanged`.

**R3. Instruction delivery and daemon-side profile.** `run.start`/`task.prompt`
accept `profileId`; `runs.profile_id`; per-provider delivery. Tests:
`TestRunClaude_AppendsInstructions`, `TestRunACP_PrefaceOnNewSession`,
`TestRunACP_PrefaceResentWhenHashChanged`,
`TestRunCodex_DeveloperInstructionsOnThreadStartAndResume`,
`TestRun_PromptAndTranscriptStayUserText`,
`TestRunStart_ContractNotOverridableByCaller`.

**R4. Tool policy, caller identity and child policy.** Run token, method
allowlist, built-in `smind` MCP server for `orchestrationTools=true`,
reserved name, strict MCP on Claude, `childPolicy`, forced parent and
source, own-subtree scope, `profile_list` filtered. Tests:
`TestRunClaude_PeerHasStrictMCPAndNoSmindServer`,
`TestRunACP_PeerMcpServersOmitSmind`,
`TestTaskSend_PeerCallerRejected`,
`TestTaskSend_LeadMayOnlySpawnLeafProfiles`,
`TestTaskSend_LeadRawProviderRejected`,
`TestTaskSend_LeadCannotTouchSiblingSubtree`,
`TestRunToken_ForgedSourceIgnored`,
`TestRunToken_MethodAllowlistFailsClosed` (`profile.create`, `mcp.add`,
`workspace.setScripts`, `account.add` denied),
`TestRunToken_DiesWithRun`, `TestProfileList_LeadSeesOnlyAllowedChildren`,
`TestMCPServer_NoApprovalTools` (extended). Depends on the guard (#289)
being on `develop`.

**R5. Limits.** Per-profile `maxTurns`, run budget (Claude flag plus a daemon
stop at the next usage event), `subtreeBudget*`, `maxConcurrentRuns`.
Tests: `TestRun_MaxTurnsPassedToClaudeAndGLM`,
`TestRun_BudgetStopsRunAtUsageEvent`,
`TestRun_UnreportedUsageShownAsNotEnforced` (nothing is estimated),
`TestSubtreeBudget_RejectsNewRun`, `TestSubtreeBudget_CountsArchivedDescendants`,
`TestConcurrency_CallerSubtreeLimitAtomic` (`-race`, 32 goroutines, exact
admission, same shape as `TestTreeGuard_ConcurrentStartsAdmitExactlyLimit`).

**R6. Handoff and review.** `handoff_submit`, `run_handoffs`, `handoff_review`,
the `unreported` disposition, fields on `task_wait`/`task_status`. Tests:
`TestHandoff_RequiredFieldsPerOutcome`, `TestHandoff_CommitMustExistInWorktree`,
`TestHandoff_ChangedPathsCheckedAgainstGit`,
`TestHandoff_SignalNeedsEvidenceConsequenceDecision`,
`TestDisposition_ContractRunWithoutHandoffIsUnreported`,
`TestDisposition_TurnLimitOverridesDeliveredClaim`,
`TestHandoffReview_RecordedAndVisible`,
`TestMCPCatalog_HandoffToolsAreNotApprovalTools`.

**R7. Tier label and profile editor (web).** Contract tier in the tree and
header (after #286's PR-T/U1 land), contract fields in the profile editor,
disposition chip. Tests: `tier label comes from the contract when the task
has a profile`, `structural tier is the fallback`, `profile editor saves
contract fields`, `disposition chip shows needs continue`. Light and dark
screenshots.

**R8. Sizing, brief, write-scope overlap (optional).** `tasks.size`,
`profile_list {size}`, `brief` rendering, overlap warning. Tests:
`TestSize_ClassFromPoints`, `TestProfileList_RanksBySize`,
`TestBrief_RenderedIntoPrompt`, `TestBrief_OverlappingWriteScopeWarns`.

**R9. Migration of the stopgap.** A documented role pack and an idempotent
`smind profile seed roles` (human command), then one L item run end to end
through smind with the Paseo stopgap as the control (section 4).

**Learning (ADR-0026)** is not a phase here: the tier-scoped skill filter is
specified in section 5 and built in the self-learning plan.

## 4. Migrating the Paseo stopgap

| Stopgap piece | smind |
|---|---|
| Profiles `supervisor`, `lead`, `peer-glm`, `peer-sonnet`, `reviewer` | Seeded as contract profiles. Supervisor is in no `childPolicy`, so only a human launches it (the Paseo "never spawned by agents" note, enforced). Lead: `childPolicy=leaf`. Peers and reviewer: `orchestrationTools=false` |
| `glm-peer` / `claude-s-peer` (Paseo tools off) | `orchestrationTools=false` |
| `WORKSPACE_PROTOCOL.md` | Stays the repo-owned contract. Profile `instructions` stay short and point at it; they do not copy it |
| `paseo.json` worktree setup | ADR-0024, already built: daemon-side, human-authored |
| Supervisor heartbeat | Stays on Paseo or `/loop` until schedules (parity D5) |
| "20-turn limit", "never Opus" in notes | `maxTurns`; fixed `model` in the profile; a Lead cannot override it |

Cutover is gradual: smind runs one L item while Paseo stays the control; the
stopgap is retired only after the trigger list in section 2 stops recurring
under smind. Never both writing the same scope.

## 5. Self-learning interaction (ADR-0026)

Role instructions and attached skills are the human base layer: always
delivered, outside probation and capacity, never touched by the reflector
or curator. Learned skills sit below them. A learned skill carries a tier
scope, defaulting to the tier of the run it was learned from; the projector
filters by the receiving run's tier. The gate refuses a peer-scoped skill
that names orchestration tools. Reflector and curator runs use their own
`orchestrationTools=false` profile and are outside every tree's limits.

## 6. Decisions for the user

Recommendation first. None is made until you answer, and all of them wait on
the trigger in section 2.

1. **Whether and when to build.** Recommend: not now; build R1 first when
   the trigger fires, since it needs no contract.
2. **Profile data model.** Recommend additive nullable columns on
   `agent_profiles` (not a JSON blob), no stored `role`, tier derived.
   Amends ADR-0014 on `systemPrompt` and on client-side apply.
3. **Enforcement points.** Recommend a daemon-side profile, a run-scoped
   token and a fail-closed method allowlist, so identity is not
   self-reported. State plainly that it is not a sandbox against a Peer with
   a shell. The alternative, not injecting the MCP server, cannot cover
   Codex.
4. **Budget defaults.** Recommend no cap by default. Seed `maxTurns` values
   from the stopgap's observed ones (Peer 20; Lead, reviewer to be set) and
   leave dollar and token caps unset until two weeks of `usage.summary
   groupBy task` data exist. Any number written now would be a guess.
5. **Structured handoff.** Recommend `handoff_submit` plus `handoff_review`
   and a derived disposition. The Codex fenced-block fallback is later.
6. **D12 (live `autoAccept` CLI).** Recommend yes, a human-only `smind task
   mode` (small, no ADR); for roles, prefer a human-authored Peer profile
   over flipping a run live.
7. **D13 (`needs-continue`).** Recommend a derived `disposition` and not a
   new run status (rationale in ADR-0027 item 4), reversing the earlier
   "distinct status" lean in `orchestration-parity-paseo.md`.
8. **Reversing D8** (auto-inject `smind mcp serve`): recommend yes, but
   only for profiles that say `orchestrationTools=true`, now that the depth
   and concurrency guards exist.
9. **Migrating the stopgap.** Recommend the gradual cutover in section 4.
10. **Learned skill tier scope.** Recommend yes, default to the source run's
    tier.

## Progress

- 2026-10-11: ADR-0027 and this plan written as Proposed, docs-only. Not
  built, per the user. PR #290 (protocol) is on `develop`.

## Validation

Docs only. Checked: ADR numbering (0026 is Proposed on PR #288, so 0027 is
next); every ADR and PR referenced exists; cited code facts were read on
`develop` (`profileId` resolved in `cmd/smind/mcp_task_send.go`, `source`
asserted by the client in `internal/wsapi/handlers.go`, `WithAppendSystemPrompt`,
`WithMaxTurns`, `WithMaxBudgetUSD`, `WithStrictMCPConfig` in
`claude-agent-sdk-go` v0.3.2, `developerInstructions` in Codex
`ThreadStartParams`/`ThreadResumeParams`). Not verified: that ACP agents
accept a leading preface block without changing usage semantics (shared open
question with ADR-0026), and Claude's exact stop value on a max-turns result.
