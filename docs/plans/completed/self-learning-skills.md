# Self-learning skills (v1)

Spec for [ADR-0026](../../decisions/0026-self-learning-skills.md)
(**Accepted** 2026-10-18; D1-D11 as recommended, D12 added by the user).
Phase A (design) is done; Phase B (code) is unblocked and ships as
non-stacked PRs off `develop`. If a decision changes, the affected criteria
change with it.

Reference: `refs/autoharness` (read-only clone, commit 9eec623; supervisor
note: PR #287 adds it to the AGENTS.md refs map). Pattern only; no code is
copied.

## Goal

smind learns reusable, workspace-scoped skills from its own runs, daemon-side
and provider-agnostic, with a human approving every skill, measured use, and
a lifecycle that keeps the library small, without ever touching a skill a
human or third party wrote.

## Scope

In (v1):

- collector, redactor, reflector job, gate, approval inbox, lifecycle
  counters, curator, ledger, versions and snapshots;
- delivery to **claude-native** (add-dir projection) and **GLM/Kimi** (inline
  preface);
- per-workspace opt-in, global kill switch, Settings -> Skills (learned),
  `smind learn` CLI, lifecycle events and notifications;
- `smind learn export` of an approved skill to repo / `~/.claude` / `~/.agents`
  / `~/.codex` through the existing #279 install machinery.

Out (explicitly):

- global/cross-workspace skills (D5), Codex delivery and measurement (D9);
- `skill_get` MCP tool for ACP load telemetry (Phase C, D7);
- skill subfiles (`scripts/`, `templates/`, `assets/`, `references/`);
- auto-approve, an LLM judge of adherence, any benchmark;
- exposing `learning.*` to agents (RPC or MCP).

## Acceptance criteria

Each is confirmed in Validation when done. Numbers are the ADR's defaults and
are config knobs.

**Trigger and input**

- AC1. A workspace in `learning_mode = off` (the default) never produces a
  window, a job, a projection or a counter change; `propose` does.
- AC2. A reflection is enqueued when a chat's pending tool-call count reaches
  50 at a run's terminal state, when "Learn from this chat" /
  `smind learn reflect` is invoked, or when a chat is archived/idle 30 min
  with >= 10 pending. Never more than one job per workspace at a time, and
  never more than one per 30 min except an explicit trigger.
- AC3. The window is built only from the chat's `run_events` after
  `learn_watermark`, and the watermark advances only after a job reaches a
  terminal state (`landed`, `empty`, `failed`, `skipped`), so a crash retries
  the same window without double counting.
- AC4. The rendered bundle tags each user turn with its origin (`human` /
  `orchestrator` / `agent`) and marks `correction` turns; tags tool failures
  that were later recovered; includes permission denials by a human; includes
  the list of learned skills loaded in the window.
- AC5. The bundle never contains: run env values, MCP `env`/`headers`/args/
  URLs, thinking events, results of reads of secret-shaped paths, file bodies
  beyond 200 bytes, base64/binary blobs. It stays within the caps (record
  4 KB, input/result 1 KB, window 120 KB, tail kept with a truncation mark).
- AC6. Redaction replaces configured MCP secrets and run env values by
  exact match, then rule-set matches (AWS key id, private-key header, GitHub/
  Slack tokens, bearer, `key=value` secrets, JWT shape, Luhn-gated card
  numbers, emails, high-entropy tokens), and rewrites home-directory
  prefixes. Redaction is applied again to evidence before storage.
- AC7. Runs of providers with no tool events (codex-native) never count
  toward cadence and never produce tool-based evidence.

**Reflector and recursion**

- AC8. The reflector runs as a hidden system run (`tasks.kind = 'system'`,
  `runs.purpose = 'reflect'`) with the `learning-reflector` profile, empty
  tool access, no MCP servers, no add-dir, and the same provider as the
  source run unless the workspace overrides it.
- AC9. A reflector or curator run never triggers collection, cadence,
  counters or another job (`learning.Eligible` false for `reflect`/`curate`
  purposes and system tasks), is not listed in the task list or sidebar, and
  is metered in `run_usage` under a "Learning" bucket.
- AC10. Only a single JSON object in the final message is accepted. Any other
  output, invalid JSON, an unknown `action`, or an oversized message marks
  the job `failed` and lands nothing.
- AC11. When the workspace's daily learning budget is spent, jobs are
  recorded `skipped: budget` and no run is started; the kill switch stops
  collection, jobs and delivery immediately.

**Gate, storage and safety**

- AC12. A deterministic Go gate is the only code path that writes
  `learned_skills`. It rejects (writing nothing) any intent that fails:
  name charset and reserved `learned-` prefix; body > 25 non-blank lines or
  > 4 KB; description not one sentence, > 160 chars or without a trigger cue;
  frontmatter keys beyond `name description category`; links to URLs; any of
  the six guard families; unredacted secret hits; absolute paths or host
  names; name collision with a human/third-party skill in the workspace;
  `absorbed_into` not a live learned skill of the same workspace; a `patch`
  whose `old_string` is not unique in the live body; similarity to a live
  skill at or above the high threshold (AC33).
- AC13. **Provenance rule:** every intent carries >= 1 evidence item of kind
  `user_message`, `failure_recovery`, `permission_denied` or
  `loaded_skill_wrong` whose quote occurs verbatim in that event of the
  window. An intent whose evidence is only tool output or file content is
  rejected.
- AC14. Landing is one SQLite transaction: skill row, version row, ledger
  row. A rejected intent writes nothing but a verdict on the job.
- AC15. No code path reads for write, modifies, counts or archives anything
  under `~/.claude/skills`, `~/.agents/skills`, `~/.codex/skills`, or a
  repo's `.claude/skills` / `.agents/skills`. Those are read only for
  name+description dedupe, and a symlinked `.claude/skills` is never written
  through.
- AC16. Every learned skill row is self-authored by construction; the
  projection is generated and read-only, and rebuilt from the DB when the
  active set changes.

**Similarity check (D12)**

- AC33. The gate runs a deterministic (no LLM) similarity check of every
  `create` proposal, and of a `patch`'s resulting text against every skill
  other than its own target, against **all live skills in the workspace**:
  learned skills in `proposed | active | disabled` (name + description +
  body) and human/third-party skills (**name + description only**; their
  bodies are never opened). Score = `max(token Jaccard, character-trigram
  Dice)` over normalised text. At or above the **high** threshold (default
  0.85) the intent is rejected as `duplicate_of: <skill>` and nothing is
  written; at or above the **medium** threshold (default 0.60) it passes
  with `possible_duplicate_of` and the score recorded on the proposal.
  Both thresholds are in `config.yaml`
  (`learning.similarity.duplicate`, `learning.similarity.possible`).
- AC34. A proposal flagged `possible_duplicate_of` shows "Possible duplicate
  of `<skill>`" in the inbox with both skills side by side and the score. For
  a learned match it offers **Merge into this skill**, which turns the
  proposal into a `patch` of the existing learned skill (editable merged
  body, evidence carried over, gate re-run, ledger entry, still
  approval-gated). For a human skill match it shows name + description only
  and offers no merge; human skills are never modified.

**Approval and UX**

- AC17. A `proposed` skill is never delivered to any agent. Only `active`
  skills are projected/prefaced.
- AC18. Settings -> Skills (learned) lists Proposed / Active / Archived per
  workspace with name, category, description, source, counters, badges
  (`probation`, `pinned`, `unmeasured`); detail shows body, provenance (runs
  and evidence quotes linking to the run log), version history with diffs and
  the ledger.
- AC19. Actions work through UI and CLI: approve, edit-then-approve, reject
  (with note), pin, disable/enable, archive/restore, undo to a version,
  delete, export, reflect, curate, forget-workspace.
- AC20. A proposal is announced by a `learnedSkill.*` lifecycle event, a
  sidebar/Settings pending badge and a toast; a run's completion is not
  blocked by it.
- AC21. UI passes the light + dark screenshot check
  (feedback: UI clean like ZCode/Paseo) and matches the existing Settings
  design tokens.

**Delivery**

- AC22. claude-native runs in a workspace with active skills get
  `WithAddDirs(<projection>)`; the projection is content-addressed,
  read-only, and contains only `learned-*` skills; with no active skills no
  option is added and behavior is unchanged.
- AC23. GLM/Kimi runs get a leading text block on the first prompt of an
  agent session with the index and bodies within the 6 KB budget (overflow:
  index-only, ordered by load rate); persisted `runs.prompt` and the UI
  transcript still show only the user's text; with no active skills the
  prompt is byte-identical to today.
- AC24. Codex runs are unchanged; `smind learn export --to codex` still
  works.

**Validation in use and lifecycle**

- AC25. `offered_runs` increments once per eligible, measured-provider run
  started while the skill was active; `loaded_runs`/`loads` count `Skill`
  tool calls naming the skill and `Read` calls under the projection path,
  tagged by `via`; a reflector/curator run never counts.
- AC26. Skills offered only on providers with no load signal are `measured =
  false` and are never archived for zero use.
- AC27. Lifecycle (pure function): `offered_runs < 25` is probation (never
  evicted, outside the cap); at >= 25, measured with `loaded_runs = 0` is
  archived; mature active skills over 30 evict the lowest load rate with a
  deterministic tie-break; pinned skills are exempt; archive keeps row,
  versions and ledger.
- AC28. Every landing writes a version row; `undo` restores a prior version
  as a new version and a ledger entry. A library snapshot (5 kept) is written
  before each curator run and restorable.

**Curator**

- AC29. A curator job is enqueued after >= 5 landed changes and >= 8 skills,
  at most weekly, or on demand; it sees only the workspace's learned skills
  with bodies; its merges land as one bundle (umbrella patch + absorbed
  deletes) through the same gate and the same approval, and the absorbed
  skills are archived with `absorbed_into` only when the bundle is approved.
- AC30. The ledger distinguishes a merge from a retirement (`absorbed_into`)
  and records actor `reflector | curator | human`.

**Privacy**

- AC31. The only network egress added is the reflector/curator model call to
  the profile's provider; same-recipient rule enforced by default; a
  cross-provider override requires an explicit per-workspace acknowledgement
  recorded in the ledger.
- AC32. "Forget everything" for a workspace hard-deletes its skills,
  versions, ledger, jobs and snapshots; turning learning off keeps data but
  stops delivery.

## Named test scenarios

Go (`internal/learning`, store, wsapi, taskrunner, runs):

- `TestCollector_OffByDefault`, `TestCollector_CadenceAt50ToolCalls`,
  `TestCollector_ExplicitTriggerBypassesCooldown`,
  `TestCollector_IdleTailFlush`, `TestCollector_OneJobPerWorkspace`,
  `TestCollector_WatermarkAdvancesOnlyOnTerminalJob`,
  `TestCollector_CrashRetriesSameWindow`,
  `TestCollector_CodexRunsDoNotCount`.
- `TestWindow_TagsUserOriginAndCorrection`,
  `TestWindow_PairsFailureWithRecovery`, `TestWindow_IncludesHumanPermissionDenial`,
  `TestWindow_ListsLoadedSkills`, `TestWindow_CapsRecordInputResultAndTotal`,
  `TestWindow_ExcludesEnvMcpThinkingAndSecretPathReads`.
- `TestRedact_ExactMcpSecretsAndRunEnv`, `TestRedact_RuleCorpus` (AWS, GitHub,
  Slack, bearer, JWT, key=value, PEM header, Luhn pass/fail, email, entropy,
  git SHA left alone), `TestRedact_HomePrefix`, `TestRedact_AppliedToEvidenceOnStore`.
- `TestEligible_ExcludesReflectCurateAndSystemTasks`,
  `TestReflect_DoesNotRetriggerLearning`, `TestSystemTask_HiddenFromTaskList`,
  `TestReflect_MeteredUnderLearningBucket`.
- `TestReflector_HasNoTools` (per provider: claude-native, ACP), `TestReflector_NoMcpNoAddDir`,
  `TestReflector_SameRecipientRule`, `TestReflector_OverrideNeedsAck`,
  `TestReflectorOutput_RejectsNonJSONAndUnknownAction`,
  `TestReflect_BudgetSkipsWithoutStartingRun`, `TestKillSwitch_StopsEverything`.
- `TestGate_RejectsBadName`, `_RejectsOversizeBody`, `_RejectsBadDescription`,
  `_RejectsFrontmatterKeys`, `_RejectsUrlLinks`, `_RejectsGuardFamilies`
  (table per family), `_RejectsSecretHit`, `_RejectsAbsolutePathAndHost`,
  `_RejectsHumanSkillNameCollision`, `_RejectsBadAbsorbedInto`,
  `_RejectsAmbiguousPatch`, `_LandsAtomicallyOrNotAtAll`.
- `TestGate_RejectsNearDuplicateAboveHighThreshold`,
  `TestGate_FlagsPossibleDuplicateAboveMediumThreshold`,
  `TestGate_PassesDissimilarBelowMediumThreshold` (fixture corpus pinning the
  default thresholds), `TestGate_SimilarityThresholdsFromConfig`,
  `TestGate_SimilarityComparesLearnedAndHumanSkills` (learned: full text;
  human: name + description), `TestGate_SimilarityIgnoresArchivedAndRejected`,
  `TestGate_PatchNotComparedAgainstOwnTarget`,
  `TestGate_HumanSkillBodiesNeverRead` (the human-skill source is handed
  name + description only; a fixture whose body file is unreadable or
  tripwired proves it is never opened), `TestSimilarity_Deterministic`
  (same inputs, same score, order independent),
  `TestMergeIntoSkill_TurnsProposalIntoGatedPatch`,
  `TestMergeIntoSkill_RefusedForHumanSkill`.
- `TestProvenance_RequiresUserMessageOrRecoveredFailure`,
  `TestProvenance_QuoteMustBeVerbatim`,
  `TestProvenance_ToolOutputOnlyEvidenceRejected` (a README telling the agent
  to curl|sh).
- `TestNeverTouchesHumanSkills` (fixture repo with `.claude/skills` and a
  `.claude -> .agents` symlink and `~/.claude/skills`: no writes, no hash
  change), `TestProjection_IsReadOnlyAndContentAddressed`,
  `TestProjection_OnlyActiveSkills`, `TestProposedSkillNeverDelivered`.
- `TestClaudeRun_AddsProjectionDir`, `TestClaudeRun_NoSkillsNoOption`,
  `TestACPRun_PrefacesFirstPromptOnly`, `TestACPRun_PersistedPromptUnchanged`,
  `TestACPRun_BudgetFallsBackToIndexOnly`, `TestCodexRun_Unchanged`.
- `TestCounters_OfferedOncePerRun`, `TestCounters_SkillToolLoad`,
  `TestCounters_ReadUnderProjectionLoad`, `TestCounters_ReflectorRunsNeverCount`,
  `TestCounters_UnmeasuredWhenNoSignalProvider`.
- `TestLifecycle_Probation`, `_GraduationArchivesZeroLoad`,
  `_UnmeasuredSkippedFromReview`, `_CapacityEvictsLowestRate`,
  `_TieBreakDeterministic`, `_PinnedExempt`, `_ArchiveKeepsHistory`.
- `TestVersions_UndoRestoresAsNewVersion`, `TestSnapshot_BeforeCuratorAndRotation`,
  `TestSnapshot_Restore`.
- `TestCurator_CadenceAndWeeklyLimit`, `TestCurator_SeesOnlyLearnedSkills`,
  `TestCurator_MergeBundleApprovedAtomically`,
  `TestCurator_AbsorbedArchivedOnlyOnApproval`, `TestLedger_MergeVsRetire`.
- `TestApproval_ApproveRejectEditPinDisableDeleteUndo`,
  `TestApproval_RejectNoteFedBackAsNegativeExample`,
  `TestForgetWorkspace_HardDeletesAll`, `TestLearningOff_KeepsDataStopsDelivery`.
- wsapi: `TestLearning_RPCRoundTrip`, `TestLearning_EventsPublished`,
  `TestLearning_NotExposedOnMcp`; CLI: `TestLearnCLI_*`;
  schema: `TestMigrate_AddsLearningTablesAndColumns`,
  `TestMigrate_PreExistingRowsGetPurposeUserAndKindTask`.

Web (`web/packages/ui`): `learned-skills-section.test.tsx` (list, tabs,
badges, empty state), `learned-skill-detail.test.tsx` (provenance links,
diffs, version undo), `proposal-actions.test.tsx` (approve/reject/edit),
`learning-optin.test.tsx` (per-workspace toggle, kill switch), settings
registry test for the new section; `possible-duplicate.test.tsx` (`possible
duplicate shows both skills and merge action`: flag, side-by-side, score,
merge action for a learned match, no merge action for a human match);
light + dark screenshots (incl. the possible-duplicate view and transition
frames).

Live (not mocked), run once in Validation: a claude-native run in an opted-in
repo emits a proposal after a seeded correction; approval makes the next
claude-native run load the skill (counter increments) and a GLM run receive
the preface; `git status` of the repo is clean throughout.

## Phases

- **Phase A (this PR, docs only):** ADR-0026 (Proposed) and this plan.
- **Phase B (after the user decides):** split into reviewable PRs, each with
  its own tests:
  - B0 spikes (ADR open questions 1-5): add-dir skill loading, no-tools run,
    ACP leading block vs usage, window cost, human-origin tagging.
  - B1 schema + `internal/learning` core: store tables, `Eligible`, gate
    (incl. the D12 similarity check), guard, redactor, lifecycle function,
    ledger/versions (no model calls). Security-sensitive: `reviewer` pass.
  - B2 collector + reflector job + system task/profile + budget + kill switch.
  - B3 projection + delivery (claude add-dir, ACP preface) + counters.
  - B4 wsapi/CLI/events + Settings -> Skills UI + notifications.
  - B5 curator + snapshots + export.
- **Phase C (later):** `skill_get` MCP for ACP, Codex delivery, plain-text
  `references/`, optional auto-approve for patches, index-preface A/B.

## Decisions

Accepted 2026-10-18, recorded in ADR-0026 "Decisions" (D1-D12): opt-in per
workspace (default off); approval always; daemon DB + read-only projection,
export explicit; same-recipient reflector, Haiku-class for Claude / GLM
default for GLM, USD 0.25 / 150k tokens per workspace per day, 1 job / 30
min, 120 KB window, all in config; no global layer; strict provenance;
GLM/Kimi inline-only in v1; Claude native listing only (with the preface
switch); no Codex in v1; human skill names + descriptions shown to the
reflector, bodies never; autoharness-derived numbers (50 tool calls /
probation 25 runs / capacity 30 / curator every 5 landings) as config knobs;
D12 deterministic similarity check in the gate (high threshold rejects,
medium threshold flags with a merge action; thresholds in config).

Implementation decisions made in Phase B (refinements, not architecture):

- **One proposal model.** Every proposal (create, patch, merge bundle) lives
  in `learned_skill_proposals` (`kind create|patch|bundle`, `status
  pending|approved|rejected`); `learned_skills` holds only approved skills
  (`active|disabled|archived`). Approval is one transaction (`LandSkill` /
  `PatchSkill` / `ApplyBundle` with a `ProposalID`). Rejected proposals are
  kept for the reflector's negative examples. This supersedes the ADR text
  that mentions `proposed`/`rejected` skill statuses; the gate's `LiveSkill`
  still takes pending proposals (status `proposed`) so D12 compares against
  them.
- **Gate contract.** `Mode` (reflect/curate) is derived from the job type
  server-side. The collector must honour the `Window` contract in
  `internal/learning/gate/types.go` (`user_message` = human-typed only;
  `failure_recovery` = agent action, not repo-controlled tool output). A
  window is built for one workspace only. The human prompt is not a run event
  (it lives in `runs.prompt`), so the window gives it the sentinel key
  `(runID, seq -1)`.
- **Landing must re-verify** (AC14 addendum, from the gate review): re-run
  `gate.Check` inside the landing transaction against a refreshed snapshot;
  match the patch's base version/body hash; apply batched intents
  sequentially with no mutual-absorb cycles; persist `Render(Verdict.Result)`
  not the raw intent; per-workspace skill quota.

**Decided (D13, user, 2026-10-18): reflector tool isolation.** The Phase B0
spike (read-only) found that the claude-native reflector can be run with no
tools (`--tools ""`, strict MCP, empty setting sources, empty temp cwd,
deny-all decider; confirm with one live run), but the GLM ACP agent owns its
tools: `read_file`, `list_files`, `web_search` and `web_reader` are never
permission-gated and the client cannot disable them (glm-acp-agent 1.14.0
`protocol/agent.js`, `tools/executor.js`); Kimi's tooling could not be
inspected. Decision: **v1 reflects claude-native runs only.** A window whose
source run is GLM/Kimi is recorded as a job `skipped:
provider_no_tool_isolation` and no reflector run starts; learned skills are
still delivered to GLM/Kimi. Consequences for the spec: AC8's "same provider
as the source run" applies to claude-native only; `TestReflector_HasNoTools`
covers claude-native; add `TestCollector_AcpSourcedWindowSkipped` (job
recorded `skipped: provider_no_tool_isolation`, no run started, watermark
advanced) and `TestReflector_SameRecipientRule` becomes claude-only. Options
not taken: GLM reflector with mitigations (web egress risk), a tool-less
direct API call (second LLM client), Claude reflecting GLM transcripts
(breaks D4).

## Progress
- 2026-10-11: Phase A written (ADR-0026 Proposed, this plan). No feature code.
- 2026-10-18: user accepted D1-D11 as recommended and added D12 (similarity
  check). ADR-0026 set to Accepted; D12 added to the ADR (gate list, inbox
  UX), ACs 33-34 and the named tests above. Phase B starts with B1.
- 2026-10-18: B1 split into three non-stacked PRs, each reviewed by an
  independent read-only reviewer and hardened before opening:
  #392 (store schema, ledger/versions/proposals/jobs, lifecycle function),
  #393 (redactor + `learning:` config + kill switch that fails closed),
  #394 (gate, D12 similarity, human-skill loader). B0 spike done (see OPEN
  decision above). Next: B2a window builder + `Eligible`, B2b run plumbing
  (`runs.purpose`, hidden system tasks, run-finished observer, Learning usage
  bucket), B2c isolated reflector runner (claude-native), B2d collector/job
  service; then B3 delivery, B4 wsapi/CLI/UI, B5 curator/export.
- 2026-10-18: #390-#395 merged. User decided D13 (claude-native-only
  reflection in v1, GLM/Kimi windows skipped, delivery to GLM/Kimi kept);
  recorded in ADR-0026 and above. B2 starts with B2a (window builder).
- 2026-10-18/19: Phase B landed as non-stacked PRs, each independently
  reviewed (read-only reviewer on the redactor, gate, isolated runner and
  reflector; findings fixed before merge): #392 store + lifecycle, #393
  redact + config, #394 gate + similarity + human-skill loader, #395/#398
  plan + D13, #400 window builder, #401 run plumbing, #402 isolated runner,
  #403 reflector prompts + parser, #405 approval service, #406 projection +
  preface + load matcher, #408 run origin + executor, #410 export, #411
  wsapi + CLI + events, #412 UI inbox + settings + D12 duplicate view, #413
  collector + reflect pipeline, #414 delivery + counters + lifecycle, #417
  curator + snapshots, #418 integration + budget + export RPC; #420 (open)
  export dialog + budget row.
- 2026-10-19: live pass against a real daemon (isolated home, seeded DB, real
  RPCs, real UI via Playwright): gate rejection with findings, merge into a
  learned skill, plain approve, curator bundle approve, stale patch. It found
  and fixed two real bugs (UI sent `mergeInto` as an id; the dev seed lacked
  the `learned-` prefix). No real model or provider CLI has been run.
- 2026-10-19: live verification with the real `claude` CLI (4 calls, USD
  0.2033, see Validation). Found and fixed the init `plugins` abort (#426);
  noted the `haiku` alias cost finding. Plan moved to `completed/`.

## Validation

Status: **complete (2026-10-19).** Every acceptance criterion is confirmed by
an automated test or reviewed evidence below, and the live-only checks ran
once against the real `claude` CLI (see "Live verification"). The live pass
found one real bug (fixed in PR #426) and one cost finding (model alias),
both recorded there.

| AC | Status | Confirming tests / evidence |
|---|---|---|
| AC1 | confirmed | `TestCollector_OffByDefault`, `TestDelivery_OffWhenModeOffOrDisabled`, `TestCollector_CadenceAt50ToolCalls` |
| AC2 | confirmed | `TestCollector_CadenceAt50ToolCalls`, `TestCollector_ExplicitTriggerBypassesCooldown`, `TestCollector_IdleTailFlush`, `TestCollector_OneJobPerWorkspace` |
| AC3 | confirmed | `TestCollector_WatermarkAdvancesOnlyOnTerminalJob`, `TestCollector_CrashRetriesSameWindow` |
| AC4 | confirmed | `TestWindow_TagsUserOriginAndCorrection`, `TestWindow_PairsFailureWithRecovery`, `TestWindow_IncludesHumanPermissionDenial`, `TestWindow_ListsLoadedSkills` |
| AC5 | confirmed | `TestWindow_ExcludesEnvMcpThinkingAndSecretPathReads`, `TestWindow_CapsRecordInputResultAndTotal` |
| AC6 | confirmed | `TestRedact_ExactMcpSecretsAndRunEnv`, `TestRedact_RuleCorpus`, `TestRedact_HomePrefix`, `TestWindow_RedactsBeforeClipping`, `TestWindow_RealRedactorScrubsEverySurface`, `TestReflect_EvidenceRedactedBeforeStore`; the "applied again to evidence before storage" clause is `TestReflect_EvidenceRedactedBeforeStore` (the planned name `TestRedact_AppliedToEvidenceOnStore` was not used) |
| AC7 | confirmed | `TestCollector_CodexRunsDoNotCount`, `TestEmitsToolEvents` |
| AC8 | confirmed (fake CLI + live flag/init check, see Live verification) | `TestReflector_HasNoTools`, `TestReflector_NoMcpNoAddDir`, `TestRegistry_IsolatedRunUsesIsolatedRunner`, `TestExecutor_RunsHiddenIsolatedSystemRun`, `TestCanReflect_ClaudeNativeOnly`, `TestReflect_ProviderNoToolIsolationSkipped`; same-recipient is claude-native only by D13; there is no cross-provider override in v1, so `TestReflector_SameRecipientRule` / `TestReflector_OverrideNeedsAck` were not written (covered by the ACP refusal tests) |
| AC9 | confirmed | `TestEligible_ExcludesReflectCurateAndSystemTasks`, `TestReflect_DoesNotRetriggerLearning`, `TestSystemTask_HiddenFromTaskList`, `TestReflect_MeteredUnderLearningBucket`, `TestUsage_LearningBucketSplit` |
| AC10 | confirmed | `TestReflectorOutput_RejectsNonJSONAndUnknownAction`, `TestReflect_ParseFailureMarksJobFailed` |
| AC11 | confirmed | `TestReflect_BudgetSkipsWithoutStartingRun`, `TestKillSwitch_StopsEverything`, `TestCurator_BudgetSkipsWithoutStartingRun`, `TestKillSwitch_StopsCurator` |
| AC12 | confirmed | `TestGate_RejectsBadName`, `TestGate_RejectsOversizeBody`, `TestGate_RejectsBadDescription`, `TestGate_RejectsFrontmatterKeys`, `TestGate_RejectsUrlLinks`, `TestGate_RejectsGuardFamilies`, `TestGate_RejectsSecretHit`, `TestGate_RejectsAbsolutePathAndHost`, `TestGate_RejectsHumanSkillNameCollision`, `TestGate_RejectsBadAbsorbedInto`, `TestGate_RejectsAmbiguousPatch`, `TestGate_ScansCanonicalText`; atomic landing = AC14 |
| AC13 | confirmed | `TestProvenance_RequiresUserMessageOrRecoveredFailure`, `TestProvenance_QuoteMustBeVerbatim`, `TestProvenance_ToolOutputOnlyEvidenceRejected`, `TestProvenance_CodeSpansMustBeInCitedEvidence` |
| AC14 | confirmed | `TestLandSkill_AtomicAllOrNothing`, `TestApplyBundle_AtomicAllOrNothing`, `TestApproval_StaleSnapshotRejected`; the planned `TestGate_LandsAtomicallyOrNotAtAll` lives at the store/approval layer |
| AC15 | confirmed | `TestNeverTouchesHumanSkills`, `TestExport_NeverTouchesHumanSkills`, `TestHumanSkills_SymlinkedSkillsDirIsReadNotWritten`, `TestInstall_RepoRefusesSymlinkedSkillsDir`, `TestExport_RefusesSymlinkedRepoSkillsDir` |
| AC16 | confirmed | `TestProjection_IsReadOnlyAndContentAddressed`, `TestProjection_OnlyGivenSkills`, `TestProjection_RepairsTamperedDir` |
| AC17 | confirmed | `TestProposedSkillNeverDelivered`, `TestProjection_OnlyActiveSkills` |
| AC18 | confirmed (screenshots: Lead-reviewed light+dark) | web "possible duplicate shows both skills and merge action"; `TestLearning_RPCRoundTrip`; web files `learned-skills-section.test.tsx`, `learned-skill-detail.test.tsx` |
| AC19 | confirmed | `TestApproval_ApproveRejectEditPinDisableDeleteUndo`, `TestLearning_SetModeAndForget`, `TestLearnCLI_*`, `TestLearning_ExportRoundTrip`, `TestCurator_ExplicitBypassesCadenceNotBudget`, `TestForgetWorkspace_HardDeletesAll`; web `proposal-actions.test.tsx` |
| AC20 | confirmed | `TestLearning_EventsPublished`, `TestLearningWiring_SharedApprovalAndNotifier`; web `use-learning.test.tsx` (event -> toast + invalidation), the nav badge registration and pending count in `learned-skills-section.test.tsx` |
| AC21 | confirmed | light + dark screenshots of every tab, detail, patch diff, possible-duplicate (learned + human), merge editor, bundle, edit, reject note, empty states, narrow 390 px, tab-switch / pane-open / dialog-open transition frames, budget and export dialog states (Lead-reviewed; two live-pass bugs fixed) |
| AC22 | confirmed (fake CLI + live add-dir load from the 0555 projection) | `TestClaudeRun_AddsProjectionDir`, `TestClaudeRun_NoSkillsNoOption` |
| AC23 | confirmed | `TestACPRun_PrefacesFirstPromptOnly`, `TestACPRun_PersistedPromptUnchanged`, `TestACPRun_NoDeliveryNoLeadingBlock`, `TestPreface_OverBudgetFallsBackToIndexOnly`, `TestDelivery_PrefaceBudgetAndLoadRateOrder`; the planned `TestACPRun_BudgetFallsBackToIndexOnly` is covered by the two preface tests |
| AC24 | confirmed | `TestCodexRun_Unchanged`, `TestExport_WritesRenderedSkill` |
| AC25 | confirmed (fake events + live `Skill` input shape `{"skill": "<name>"}`, counter 1/3) | `TestCounters_OfferedOncePerRunE2E`, `TestCounters_SkillToolLoadE2E`, `TestCounters_ReadUnderProjectionLoadE2E`, `TestCounters_ReflectorRunsNeverCount`, `TestMatch_SkillToolCall` |
| AC26 | confirmed | `TestCounters_UnmeasuredWhenNoSignalProvider`, `TestLifecycle_UnmeasuredNeverArchivedE2E`, `TestCounters_UnmeasuredGlmOffer` |
| AC27 | confirmed | `TestLifecycle_Probation`, `TestLifecycle_GraduationArchivesZeroLoad`, `TestLifecycle_CapacityEvictsLowestRate`, `TestLifecycle_TieBreakDeterministic`, `TestLifecycle_PinnedExempt`, `TestLifecycle_GraduationArchivesZeroLoadE2E`, `TestPinDisableArchive_LedgerActions`; archive is a status change only (row, versions and ledger remain): `TestPinDisableArchive_LedgerActions`, `TestLearnedSkillsPartialUniqueIndexAllowsArchivedDuplicates` |
| AC28 | confirmed | `TestVersions_UndoRestoresAsNewVersion`, `TestSnapshot_BeforeCuratorAndRotation`, `TestSnapshot_Restore`, `TestCurator_SnapshotTakenBeforeRunAndJobStopsIfSnapshotFails` |
| AC29 | confirmed | `TestCurator_CadenceAndWeeklyLimit`, `TestCurator_SeesOnlyLearnedSkills`, `TestCurator_MergeBundleApprovedAtomically`, `TestCurator_AbsorbedArchivedOnlyOnApproval` |
| AC30 | confirmed | `TestLedger_MergeVsRetire` |
| AC31 | confirmed by construction | `TestRegistry_IsolatedStartRefusesAcpProviders`, `TestRunPromptIsolated_RefusesAcpProviders`; the only egress added is the isolated claude-native reflector/curator call; v1 offers no provider override, so the acknowledgement clause has nothing to guard |
| AC32 | confirmed | `TestForgetWorkspace_HardDeletesAll`, `TestLearningOff_KeepsDataStopsDelivery`, `TestSnapshot_ForgetDeletesSnapshots` |
| AC33 | confirmed | `TestGate_RejectsNearDuplicateAboveHighThreshold`, `TestGate_FlagsPossibleDuplicateAboveMediumThreshold`, `TestGate_PassesDissimilarBelowMediumThreshold`, `TestGate_SimilarityThresholdsFromConfig`, `TestGate_SimilarityComparesLearnedAndHumanSkills`, `TestGate_HumanSkillBodiesNeverRead`, `TestGate_PatchNotComparedAgainstOwnTarget` |
| AC34 | confirmed | `TestMergeIntoSkill_TurnsProposalIntoGatedPatch`, `TestMergeIntoSkill_RefusedForHumanSkill`; web "possible duplicate shows both skills and merge action"; live: real merge approved, new version + `merge` ledger row |


### Live verification (2026-10-19)

One manual pass, user-approved, with the real `claude` 2.1.293 CLI. Setup: a
`smind` built from `develop` + PR #426, an isolated `SMIND_HOME`
(`/tmp/learning-live/home`) and port (`SMIND_LISTEN=127.0.0.1:4711`), a
throwaway git repo as the workspace (`learning_mode = propose`, default
budget), a seeded chat (one agent run with a recovered `go test` failure, one
human correction run), and a tee wrapper named `claude` first on the daemon's
`PATH` that logged argv and the raw stream-json of every invocation. The
daemon's environment had the `CLAUDE_CODE_*` session variables removed and
the fake GLM agent was `npx` -> the taskrunner `fakeagent` (no real GLM
endpoint was called). `~/.smind`, port 4648 and `~/.claude` were not touched;
the daemon was stopped afterwards and no orphan processes remained.

**Real model calls: 4 (reflector #1, reflector #2, one 4-token probe, one
claude-native run). Total cost USD 0.2033** (0.0339 + 0.0154 + 0.0005 +
0.1535).

1. **Isolated flags (AC8).** argv of the reflector call: `--output-format
   stream-json --verbose --system-prompt <...> --tools "" --max-turns 1
   --model haiku --permission-mode plan --strict-mcp-config
   --setting-sources= --disable-slash-commands --input-format stream-json`,
   with `ENABLE_CLAUDEAI_MCP_SERVERS=false` in the env; the CLI accepted all
   of them (`--max-turns` included although `--help` omits it) and ended
   `result success, num_turns 1`. Init message: `"tools":[]`,
   `"mcp_servers":[]`, `"slash_commands":[]`, `"skills":[]`,
   `"permissionMode":"plan"`, and `"plugins"` = four compiled-in entries
   `{"name":"cc-plugin-sec-default","path":"builtin","source":"cc-plugin-sec-default@builtin"}`
   (+ `-agents-md`, `-telemetry`, `-plugin-authoring`). **Bug found:** the
   runner treated those as a violation, so reflector #1 failed with
   `isolated run attempted a tool call: init reported plugins` even though the
   model had answered. Fixed in PR #426 (exempt entries whose `path` is
   `builtin` and `source` is `<name>@builtin`; anything else still aborts;
   tests `TestInitViolation_RealCLIShape`,
   `TestInitViolation_PluginExemptionIsNarrow`,
   `TestRunPromptIsolated_InitBuiltinPluginsProceed`). With the fix,
   reflector #2 ran clean. Not run: a control call without
   `ENABLE_CLAUDEAI_MCP_SERVERS=false` to show the connectors it removes (USD
   ~0.0005 to do); the isolated init lists no MCP server at all.
2. **Add-dir skill loading and `Skill` input (AC22, AC25).** The claude-native
   run's argv carried `--add-dir /tmp/learning-live/home/learning/projection/1/0f2301d8cbf20ad1`
   (directories and `SKILL.md` all mode `dr-xr-xr-x` / `-r--r--r--`). Its init
   `skills` listed `learned-go-tests-no-sleep-run-race` first, from that
   read-only dir; the model then called `Skill` with input
   `{"skill":"learned-go-tests-no-sleep-run-race"}` (field `skill`, matches
   `loads.Match`). `learned_skill_loads` got `(skill 1, run, via=skill)` and
   `smind learn ls` showed `LOADED/OFFERED 1/3`. The generated frontmatter's
   extra `category:` key did not stop the CLI loading the skill.
3. **End to end.** The seeded human correction (`seed-run-2`, Interrupt)
   produced one proposal (`learned-go-tests-no-sleep-run-race`) that passed
   the gate, job `landed`, verdict `accepted`; `smind learn approve 1` made it
   active. A fresh fake-GLM session received the preface as a leading text
   block (`<<<LEARNED_SKILLS ... LEARNED_SKILLS>>>`) followed by the user's
   text block, while `runs.prompt` stayed the user's text only (AC23); both
   GLM runs counted as offered. The next claude-native run loaded the skill
   (item 2). `git status` of the workspace repo was empty before, between and
   after every step.
4. **Prompt adherence and cost.** Haiku-class prompt adherence (both reflector
   replies): the final message was a single JSON object with no prose or
   fences, the user quote was verbatim (`seq -1`), the `failure_recovery`
   quote matched the `[RECOVERY #4]` record, and every code span in the body
   came from a cited event; the gate accepted it with no findings. Measured
   (window about 6 KB, 4.1k non-cache tokens): reflector #2 cost USD 0.0154,
   `run_usage` 3649 in / 4812 cached / 2284 cache-write / 510 out, i.e. 6.1 %
   of the USD 0.25 cap and 2.8 % of the 150k token cap; the daemon's budget
   readout agreed (`usedUsd 0.0154`, `usedTokens 4159`). Not measured: a
   120 KB window (no extra call was made). Extrapolated from the observed
   per-token cost it is roughly USD 0.1 on the model that actually answered
   (below) and well under 0.01 on real Haiku, i.e. inside the USD cap, and
   about 30k tokens against the 150k cap.

**Finding (cost, not fixed): `--model haiku` is answered by Sonnet.** In all
three calls that passed the alias (`--model haiku`, as the daemon sends for
`learning.reflectorModel: haiku`), the init message said `claude-haiku-5-5`
but the assistant turn and the bulk of the cost were `claude-sonnet-5-5`
(`modelUsage`: Sonnet USD 0.0333 / 0.0150, Haiku only a side call of 3.6k
tokens, USD 0.0004-0.0005). The same prompt flags with the full id
`--model claude-haiku-5-5` were answered by Haiku (USD 0.0005 for the 4-token
probe). The cause was not isolated (alias resolution in this CLI/account vs
something else); the practical effect is a reflection costing about 30x what
Haiku would. The daemon did not force anything else, so users can pin the id
in `learning.reflectorModel` / `learning.curatorModel` today. Follow-up: decide
whether to default to a pinned id or verify the answering model from
`modelUsage` and surface it in the job row.

Other observations (recorded in Known limits): a failed isolated run's cost
is not metered (reflector #1 spent USD 0.034 and `unreportedRuns` rose
instead), and the token cap counts input and output tokens only, not cache
read/write tokens.

### Known limits and follow-ups (not blockers)

- Resolved after the live check (PR #428): the reflector/curator default
  model is the full id `claude-haiku-5-5` (the `haiku` alias was answered by
  Sonnet), a failed isolated run's reported usage is metered, and the token
  cap counts cache read/write tokens.
- `store.PatchSkill` persists description and body only: a category-only
  patch passes the gate but the new category is not stored.
- Merge ledger rows name the merged proposal but do not copy its evidence.
- A GLM/Kimi skill load cannot be observed in v1 (D7): those skills are
  `unmeasured` and never auto-archived for zero use.
- The cadence landing count reads the newest 500 ledger rows per workspace
  per tick.
- UI: restoring a library snapshot has no UI yet (store-level only).
- Phase C (not started, by decision): `skill_get` MCP tool for ACP load
  telemetry, Codex delivery, plain-text `references/`, optional auto-approve
  for patches, index-preface A/B (D8).
