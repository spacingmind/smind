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

## Progress

- 2026-10-11: Phase A written (ADR-0026 Proposed, this plan). No feature code.
- 2026-10-18: user accepted D1-D11 as recommended and added D12 (similarity
  check). ADR-0026 set to Accepted; D12 added to the ADR (gate list, inbox
  UX), ACs 33-34 and the named tests above. Phase B starts with B1.

## Validation

Empty until Phase B: each AC above gets its confirming test names and any
live evidence here, and the file moves to `docs/plans/completed/` only when
every AC is confirmed.
