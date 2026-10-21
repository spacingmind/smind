# Learning probe: probe before approve

Size: XL (10) split into L/M items — scope 2, PRs 2, risk 2, decisions 2 (ADR-0037, accepted), verification 2 (live `claude` CLI).
One Lead drives the sequence; each PR is non-stacked off `develop`. Security-sensitive: the sandbox/probe PRs get an independent
`reviewer` pass on the exact commit before the Human is asked to merge.

Spec for [ADR-0037](../../decisions/0037-learning-probe-before-approve.md) (read it first: arms, R1–R7, budget, verdicts,
retention). Amends ADR-0026 AC17. Study: `docs/research/local/harness2-study.md` (local-only).

## Scope and PRs

| PR | Content | Size | Live calls |
|---|---|---|---|
| 1 | **Docs**: ADR-0037 + this plan; ADR-0026 status cross-reference | S | none |
| 2a | **S0** prevalence scan: signature derivation, store query, `learning.proposals.prevalence`, one UI line | S | none |
| 2b | **D11** reflector one-shot retry with gate findings | S | none (fake LLM in tests) |
| 3 | **P0 spike**: prove R1–R3 and the tool surface against the real CLI; ADR-0037 addendum; gated live suite | M | yes: 24 calls, USD 0.068 |
| 4a | **Arm + sandbox**: `git archive` arm materialiser, sandbox settings/env/decider, attestation, caps, tripwires, probe projection builder | L | yes (gated suite) |
| 4b | **Probe service**: eligibility, estimate, A/A′/B orchestration, `purpose='probe'`, Probe bucket, tier-1 contrasts + verdict, ledger, store, wsapi, CLI, events, retention janitor, `probeCheck` | M | yes (one end-to-end) |
| 4c | **UI**: Probe card, states, estimate popover, disabled reasons, verdict chip, light+dark screenshots incl. transitions | M | none |

Order: 1 → (2a ‖ 2b, independent) → 3 → 4a → 4b → 4c. If P0 fails (R1–R3 not provable with the CLI command sandbox), the Lead
reports `BLOCKED` with evidence and proposes the container path; 4a–4c do not start.

Deferred, not in this plan: LLM judge and contrast report (after probe data), profile-notes proposals (D10), probation holdout
(D12), GLM/Kimi/Codex probes, Windows probes.

## Acceptance criteria

### PR 1 — docs

- AC1. ADR-0037 exists, status Accepted, lists D1–D14 as decided, states the AC17 amendment as a rule, lists R1–R7, the budget
  table, the verdict vocabulary and retention. ADR-0026's status block links to it.
- AC2. This plan has ACs, named tests, the PR split, the budget model and R1–R7 mapped to tests.

### S0 — prevalence scan (PR 2a)

- AC3. `learning/probe/signature` derives a deterministic signature from an evidence item: `failure_recovery` → tool name +
  normalised error prefix (lowercased, digit runs → `#`, hex ≥ 8 → `<h>`, absolute paths → `<path>`, whitespace collapsed, first
  120 chars); `permission_denied` → tool + request kind. `user_message` and `loaded_skill_wrong` have none.
- AC4. The scan counts, over the workspace's persisted `run_events` of runs with `purpose = 'user'` in the last
  `learning.probe.prevalenceDays` (default 30), the **distinct runs and distinct chats** whose events match the signature, plus
  the last-seen time. It never reads another workspace's events and never counts reflect/curate/probe runs.
- AC5. The scan is bounded (`learning.probe.prevalenceMaxEvents`, default 20000 newest events); a truncated scan returns
  `truncated: true` and the UI says "at least N".
- AC6. `learning.proposals.prevalence` (wsapi) returns `{signatures: [{signature, runs, chats, lastSeenAt, truncated}]}` for at
  most 3 signatures of the proposal; no model call, no write. Output carries only redacted text.
- AC7. The proposal detail shows "Seen in N runs across M chats (last 30 days)" under the evidence; nothing for evidence kinds
  without a signature; loading and error states are quiet. UI passes the light + dark screenshot check.

### D11 — reflector retry (PR 2b)

- AC8. When ≥ 1 intent is rejected **only** with repairable findings (trigger cue missing, body/description/line caps, name
  charset or `learned-` prefix, frontmatter keys, links), the reflector is called once more with the original prompt plus the
  gate's findings (reason + clipped detail) for those intents. Provenance, guard, secret, duplicate and similarity findings are
  never retried.
- AC9. At most one retry per job. The retry's output passes the **same gate with the same config**; nothing is relaxed. Intents
  accepted on the first pass are not re-emitted or double-created.
- AC10. The budget is re-checked before the retry; over budget skips the retry (the first-pass verdicts stand). Retry cost and
  tokens are added onto the same job row.
- AC11. A retry that fails to parse or errors leaves the first-pass verdicts and job status as they would have been without a
  retry.

### P0 — sandbox spike (PR 3)

All against a throwaway repo, an isolated `SMIND_HOME` and a non-4648 port; never `~/.smind`.

- AC12. **R1 write confinement**: a Bash command in an arm that writes outside the arm dir (a sibling dir, `$HOME`, `/tmp` outside
  the private temp) fails; a write inside succeeds. Evidence recorded with the files checked before/after.
- AC13. **R1 read confinement**: a Bash read of a decoy credential dir (e.g. a fake `~/.ssh` under a decoy HOME path allowed to the
  real CLI process) fails; a built-in Read outside the arm dir is auto-denied non-interactively (and by which mechanism).
- AC14. **R2 network**: a Bash `curl` to a non-model host fails; the model call itself succeeds; `WebFetch` and `WebSearch` are
  refused by the denylist; no MCP server or connector is loaded.
- AC15. **fail-if-unavailable**: with the sandbox forced unavailable, a Bash command does not run (the CLI errors or refuses), it
  does not fall back to unsandboxed.
- AC16. **Launch shape**: AC12–AC15 hold under the exact smind launch (SDK, `--setting-sources=`, `--settings` carrying the
  sandbox keys), and the env is the R3 allowlist with a private HOME. The one auth variable required is identified.
- AC17. A self-check that attests the sandbox without a model call is identified (or the spike reports that none exists and a
  one-call canary is needed). Call count and cost of the spike are reported; the ADR-0037 addendum records pass/fail per R with
  CLI version.
- AC18. A verdict: **PASS** (R1–R3 provable with the CLI command sandbox, the tier of D3(a)) or **BLOCKED** with evidence and the
  container proposal. No claim is made for anything not run.

### Arm + sandbox (PR 4a)

- AC19. `arm.Materialise(repo, baseSHA, dir)` exports `baseSHA` with `git archive`, `git init` + one commit, no remote, no
  credentials, no link to the real `.git`; refuses a dirty/unknown sha; the real repo's HEAD, index and refs are byte-unchanged.
- AC20. **R1/R2/R3** (as measured by P0, ADR-0037 addendum): the sandbox package builds the CLI settings (sandbox on,
  `failIfUnavailable`, strict network allowlist, `allowWrite` = arm, read **allow-list** (`denyRead:["/"]`, `allowRead` = arm, projection and the minimal system paths; no deny-list mode), unsandboxed commands off, `permissions.deny` write tools on the projection), the `--tools`
  **allowlist**, `acceptEdits` + `--permission-prompts none`, the MCP-closed flags, git's `GIT_CONFIG_GLOBAL=/dev/null
  GIT_CONFIG_NOSYSTEM=1`, and the `env -i` **launcher** with the allowlist `PATH, LANG, TMPDIR, HOME, CLAUDE_CONFIG_DIR, USER`. A table test asserts no daemon env key leaks and
  that the argv/env contain no secret value in logs or events.
- AC21. (P0: the SDK decider is never consulted headless, so this is belt-and-braces; the audit signal is `result.permission_denials`
  and tool_result text.) The permission decider auto-allows Read/Edit/Write/Glob/Grep inside the arm dir and the read-only projection, allows Bash
  only inside the sandbox, and denies everything else, never prompting a human; denials are counted as events identically for all
  arms.
- AC22. `sandbox.Attestation` can only be produced by a passing **canary call** (one 2-turn haiku call, about USD 0.002, charged to the
  probe bucket: outside write absent, inside write present, init tools == allowlist; there is no model-free attestation); the probe projection builder does not accept a nil or
  zero attestation (compile-level and runtime). A failing or stale canary disables probes with a stated reason.
- AC23. **AC17 amendment**: production delivery still never includes a `proposed` skill; the probe projection includes the
  candidate only with an attestation. The projection hash is recorded on the arm's run.
- AC24. **R4**: arm timeout, `min(2 × source turns, 80)` turn cap, cumulative cost cap from usage events, output cap and
  process-group kill all end an arm with a distinct status (`timeout`, `turns`, `capped`, `output`, `killed`); after any end, no
  child process of the arm survives and the arm dir is removed. Orphan arm dirs are swept at daemon start.
- AC25. **R7**: after every arm the tripwires run (real checkout `git status`/HEAD, human skill dir hashes, sandbox violation
  log); a hit marks the probe `unsafe` and stops the remaining arms.

### Probe service (PR 4b)

- AC26. Eligibility reasons (each a stable code, each shown in the UI): `not_pending`, `provider_not_claude_native`,
  `no_prompt`, `no_base_sha`, `dirty_checkout`, `continuation_run`, `source_cost_unknown`, `sandbox_unavailable`, `budget`,
  `quota`, `busy`, `windows`, `platform_unsupported` (Linux: no measured sandbox, decision 2026-10-22), `learning_off`.
- AC27. **Estimate**: `est_arm = 1.2 × source run cost`, `est_probe = n_arms × est_arm`; `dry-run` returns it with the expected
  duration and writes nothing; disabled when `est_probe > perProbeBudgetUSD` or `est_probe + spent_24h > dailyBudgetUSD`.
- AC28. **Budget enforcement** (count-asserting): the probe bucket (`run_usage` with `purpose = 'probe'`) is separate from the
  learning bucket; learning spend never reduces probe headroom and probe spend never reduces the learning cap; a probe that hits
  the per-probe cap stops the running arm, starts no further arms and ends `Inconclusive`; an arm with unreported usage is charged
  its cap; the quota check reads only the cached account snapshot (a test asserts **zero** provider usage-endpoint calls).
- AC29. A probe runs exactly its planned arms (A, A′, B or A, B), with no duplicate run ids, and each arm's projection hash
  matches its role (A and A′ equal, B different), asserted at the end (the Harness² budget-identity idea).
- AC30. Probe runs are excluded from the collector, cadence counter, offered/loaded counters, every learning trigger, the task
  list and the recents; they run under a hidden `tasks.kind = 'system'` task and openable from the card.
- AC31. **Contrasts**: per arm the deterministic metrics of ADR-0037 §4 are computed from persisted events; `loaded` uses
  `loads.Match` unchanged; targeted-failure recurrence uses the S0 signature; delta(A, A′) is the noise floor.
- AC32. **Verdict** is a pure function of the contrasts and follows ADR-0037 §4 exactly; `Helped` needs a signature, B loaded, the
  failure in A and A′ but not B, nothing worse than noise, `probeCheck` not worse; `--quick` caps everything except
  `never_loaded`, `probe_failed` and `unsafe` at `inconclusive`. The verdict never changes a proposal's status.
- AC33. `probeCheck` is a workspace field set only by a human through Settings or the CLI (not MCP, not a tool call, not the
  reflector), runs after each arm inside the sandbox with no network and a timeout, and yields pass/fail plus a clipped
  redacted output.
- AC34. Arm diffs and final messages are stored redacted (caps 64 KB / 8 KB); a secret planted in an arm's output is not present in
  any stored row.
- AC35. **Retention**: a janitor purges a probe's rows, runs, tasks and events 14 days after the proposal decision; a pending
  proposal keeps its probe; **forget-workspace** hard-deletes every probe row, run, event and arm dir for the workspace.
- AC36. wsapi `learning.probe.estimate | start | cancel | get | list` and CLI `smind learn probe <proposal> [--quick]
  [--dry-run]` / `smind learn probe show <proposal>`; **no MCP tool** exposes any of it (a catalog test asserts it). Events
  `learnedSkill.probe.{started,arm,finished,failed}` exist. The ledger gains one `probed` row (verdict, cost, run ids, projection
  hash).
- AC37. One probe per workspace and one per daemon at a time; a second start returns `busy`; cancel stops the process group and
  removes the arm dir.

### UI (PR 4c)

- AC38. Idle, running, result, ineligible, error and cancelled states of the Probe card exist and use the shared TanStack Query
  hooks, zustand stores and shared primitives (ADR-0032), no local fetch/state copies.
- AC39. The button shows the estimate and expected duration; a disabled button shows the stable reason text.
- AC40. Running shows the three rows (Control / Control replicate / With this skill) with status, elapsed and cost so far, a link
  to each hidden run, and Cancel.
- AC41. Result shows the verdict chip, the contrast table with the noise-floor column, `loaded`, `targeted failure recurred`, the
  check result, the produced-diff viewer and the disclaimer line "behavioural contrast on one replay of the source episode, not a
  measurement" plus "fixes the observed failure here". The chip is never styled like an approval.
- AC42. Light and dark screenshots of every state **including the transition frames** are captured on the real app and reviewed.

## Probe wire contract (4b daemon ↔ 4c UI, frozen 2026-10-22)

4c builds against a typed fake client implementing exactly this; 4b implements it in `internal/wsapi` + `internal/learning/probe`.
JSON is camelCase, ids are numbers except run ids (strings), times are RFC 3339, money is USD as a number, durations are ms.
`id` in every `learning.probe.*` param is the **proposal id**. Platforms other than macOS get `eligible:false` with reason
`platform_unsupported` (Linux) / `windows`; there is no Probe button there, only the explanation.

RPCs (no MCP tool, no CLI-only fields):

| Method | Params | Result |
|---|---|---|
| `learning.probe.estimate` | `{id, quick?}` | `ProbeEstimate` (no write, no model call) |
| `learning.probe.start` | `{id, quick?}` | `ProbeView` (status `queued`/`running`); error `failed_precondition` (message = first reason) when ineligible or `busy` |
| `learning.probe.cancel` | `{id}` | `{}` (no-op when nothing runs) |
| `learning.probe.get` | `{id}` | `ProbeView \| null` (the proposal's latest probe, with diffs and final messages) |
| `learning.probe.list` | `{workspaceId, limit?}` | `ProbeView[]` (summaries: no diff/finalMessage) |

```ts
type ProbeReasonCode = "not_pending" | "provider_not_claude_native" | "no_prompt" | "no_base_sha" | "dirty_checkout" | "continuation_run"
  | "source_cost_unknown" | "sandbox_unavailable" | "budget" | "quota" | "busy" | "windows" | "platform_unsupported" | "learning_off";
interface ProbeReason { code: ProbeReasonCode; message: string }            // message is user-facing, one sentence
interface ProbeEstimate {
  eligible: boolean; reasons: ProbeReason[];                                // empty iff eligible
  quick: boolean; arms: 2 | 3; estUSD: number; estArmUSD: number; expectedMinutes: number;
  perProbeCapUSD: number; dailyCapUSD: number; spentTodayUSD: number; quotaPercent: number | null;
}
type ProbeStatus = "queued" | "running" | "done" | "failed" | "cancelled" | "unsafe";
type ProbeVerdict = "helped" | "no_effect" | "never_loaded" | "hurt" | "inconclusive" | "probe_failed" | "unsafe";
type ProbeArmRole = "A" | "A2" | "B";                                      // Control / Control replicate / With this skill
type ProbeArmStatus = "pending" | "running" | "done" | "capped" | "timeout" | "turns" | "output" | "killed" | "error";
interface ProbeArmView {
  role: ProbeArmRole; status: ProbeArmStatus; runId?: string;              // hidden run; link target
  startedAt?: string; elapsedMs: number; costUSD: number;
  turns: number; toolCalls: number; failedToolCalls: number; permissionDenials: number;
  loadedSkill?: boolean;                                                    // role B only
  failureRecurred?: boolean | null;                                         // null = evidence has no signature
  producedDiff: boolean; diffBytes: number;
  diff?: string; finalMessage?: string;                                     // get only; redacted; diff <= 64 KB, finalMessage <= 8 KB
  check?: { passed: boolean; output?: string };                             // present only when probeCheck is configured
}
interface ProbeMetricRow { key: "toolCalls" | "failedToolCalls" | "permissionDenials" | "turns" | "costUSD" | "durationMs" | "diffBytes";
  a: number; a2: number | null; b: number; deltaAB: number; noise: number | null; worse: boolean }   // worse = B worse than A by more than the noise floor
interface ProbeView {
  id: number; proposalId: number; workspaceId: number; quick: boolean; status: ProbeStatus;
  verdict: ProbeVerdict | null; verdictReason: string;                      // one sentence, advisory
  estUSD: number; costUSD: number; startedAt: string; finishedAt?: string; elapsedMs: number;
  arms: ProbeArmView[];                                                     // planned arms in order, pending ones included
  contrast?: { loaded: boolean; noiseFloor: boolean; metrics: ProbeMetricRow[] };   // done probes only
}
```

Events (published on the existing bus; payload is a hint, the UI refetches `learning.probe.get`/`list`):
`learnedSkill.probe.started | arm | finished | failed` with `{probeId, proposalId, workspaceId, status, role?, armStatus?}`.
UI query keys: `["learning.probe.estimate",{id,quick}]`, `["learning.probe.get",{id}]`, `["learning.probe.list",{workspaceId,limit}]`;
the event bridge invalidates get/list (and estimate) on any `learnedSkill.probe.*` event.

## Test scenarios

Names are the contract; Peers use these exact names.

**S0** (`internal/learning/probe/signature`, `internal/store`, `internal/wsapi`): `TestSignature_FailureRecoveryNormalises`,
`TestSignature_PermissionDeniedUsesToolAndKind`, `TestSignature_NoneForUserMessageAndLoadedSkillWrong`,
`TestSignature_StableAcrossPathsAndNumbers`, `TestPrevalence_CountsDistinctRunsAndChats`, `TestPrevalence_ExcludesNonUserPurpose`,
`TestPrevalence_WorkspaceScoped`, `TestPrevalence_WindowBoundary`, `TestPrevalence_TruncatedFlag`,
`TestPrevalenceRPC_NoModelCall`, `TestPrevalenceRPC_OutputRedacted`. Web: `proposal-prevalence.test.tsx` (counts, none for
unsigned evidence, truncated "at least", error quiet).

**D11** (`internal/learning/service`, fake LLM harness): `TestReflectRetry_RepairableFindingRetriedOnce`,
`TestReflectRetry_ProvenanceFindingNeverRetried`, `TestReflectRetry_GuardSecretDuplicateNeverRetried`,
`TestReflectRetry_SecondPassUsesSameGate`, `TestReflectRetry_AcceptedIntentsNotDuplicated`,
`TestReflectRetry_OverBudgetSkipsRetry`, `TestReflectRetry_CostAddedToSameJob`, `TestReflectRetry_ParseFailureKeepsFirstPass`,
`TestReflectRetry_AtMostOnce`, `TestReflectRetry_FindingsClippedInPrompt`.

**P0** (`internal/learning/probe/spike`, build tag `liveprobe`, plus `docs` addendum): `TestLive_WriteOutsideArmDenied`,
`TestLive_WriteInsideArmAllowed`, `TestLive_ReadDecoyCredentialDenied`, `TestLive_BuiltinReadOutsideArmDenied`,
`TestLive_NetworkToNonModelHostDenied`, `TestLive_ModelCallSucceeds`, `TestLive_WebToolsDenied`, `TestLive_NoMCPLoaded`,
`TestLive_FailIfUnavailableRefusesToRun`, `TestLive_UnderSMINDLaunchShape`, `TestLive_EnvIsAllowlist`. Unit-level (no CLI):
`TestSpike_SettingsJSONShape`.

**Arm + sandbox** (`internal/learning/probe/arm`, `.../sandbox`, `internal/learning/projection`, `delivery`):
`TestMaterialise_ExportsBaseSHA`, `TestMaterialise_NoRemoteNoCredentialsNoRealGit`, `TestMaterialise_RefusesDirtyOrUnknownSHA`,
`TestMaterialise_RealRepoUnchanged`, `TestSandboxSettings_Table`, `TestSandboxEnv_AllowlistOnly`,
`TestSandboxEnv_NoDaemonEnvLeak`, `TestSandboxLogs_NoSecrets`, `TestDecider_AllowsInsideArmOnly`,
`TestDecider_BashOnlyInSandbox`, `TestDecider_NeverPromptsHuman`, `TestAttestation_OnlyFromPassingSelfCheck`,
`TestAttestation_StaleDisablesProbe`, `TestProbeProjection_RequiresAttestation`,
`TestProposedSkillNeverDelivered` (kept, production paths), `TestProposedSkillOnlyDeliveredToProbeSandbox`,
`TestProbeProjection_HashRecordedOnRun`, `TestArmCaps_TimeoutTurnsCostOutputEachEndArm`, `TestArmEnd_NoSurvivingChildProcess`,
`TestArmEnd_ArmDirRemoved`, `TestStartupSweep_RemovesOrphanArmDirs`, `TestTripwire_RealCheckoutChangedMarksUnsafe`,
`TestTripwire_HumanSkillDirHashChangedMarksUnsafe`, `TestTripwire_ViolationLogMarksUnsafe`,
`TestTripwire_StopsRemainingArms`.

**Probe service** (`internal/learning/probe`, `internal/store`, `internal/wsapi`, `internal/cli`):
`TestEligibility_EachReasonCode` (table over AC26), `TestEstimate_SourceCostTimesHeadroom`, `TestEstimate_DryRunWritesNothing`,
`TestEstimate_DisabledOverPerProbeCap`, `TestEstimate_DisabledOverDailyCap`, `TestBudget_ProbeBucketSeparateFromLearning`,
`TestBudget_CapStopsRunningArmAndSkipsRest` (asserts arms started == arms planned until cap, exact counts),
`TestBudget_UnreportedUsageChargedAtCap`, `TestQuota_ReadsCacheOnlyZeroEndpointCalls`, `TestProbe_RunsExactlyPlannedArms`,
`TestProbe_NoDuplicateRunIDs`, `TestProbe_ProjectionHashMatchesRole`, `TestProbeRuns_ExcludedFromCollectorAndCounters`,
`TestProbeRuns_HiddenFromTaskList`, `TestContrast_LoadedUsesLoadsMatch`, `TestContrast_FailureRecurrenceUsesSignature`,
`TestContrast_NoiseFloor`, `TestVerdict_Table` (every chip, incl. `Helped` preconditions), `TestVerdict_QuickCappedInconclusive`,
`TestVerdict_NeverChangesProposalStatus`, `TestProbeCheck_HumanOnly`, `TestProbeCheck_RunsInSandboxNoNetwork`,
`TestStoredArtifacts_Redacted`, `TestRetention_PurgesFourteenDaysAfterDecision`, `TestRetention_PendingKeepsProbe`,
`TestForgetWorkspace_DeletesProbeArtifacts`, `TestProbeRPC_Surface`, `TestMCPCatalog_HasNoProbeTool`,
`TestProbe_SecondStartIsBusy`, `TestProbe_CancelStopsAndCleans`, `TestLedger_ProbedRow`, `TestEvents_ProbeLifecycle`.
Live (gated, one end-to-end, count + cost reported): `TestLiveProbe_EndToEnd`.

**UI** (`web/packages/ui`): `ProbeCard.idle.test.tsx`, `ProbeCard.disabledReasons.test.tsx`, `ProbeCard.running.test.tsx`,
`ProbeCard.result.test.tsx` (chip vocabulary, disclaimer, never approval-styled), `ProbeCard.uses-shared-stores.test.tsx`
(imports the shared query hooks and primitives; no local fetch), `ProbeCard.cancel.test.tsx`. Screenshots: light + dark of idle,
disabled, running (each row state), each verdict chip, cancelled, error, plus the idle→running and running→result transition
frames.

## Budget model

All numbers are defaults in `learning.probe.*` and are guesses until the first week of probe `run_usage` calibrates them.

| Quantity | Value |
|---|---|
| Per-probe hard cap | USD 1.50 across all arms |
| Daily cap | USD 3.00 per workspace, trailing 24 h, `purpose='probe'` |
| `est_arm` | 1.2 × source run cost (source cost unknown → ineligible) |
| `est_probe` | 3 × `est_arm` (2 × with `--quick`) |
| Worked example, source USD 0.40 | `est_arm` 0.48, `est_probe` 1.44 (quick 0.96); fits the per-probe cap; two of them fit the daily cap |
| Source USD 0.60 | `est_probe` 2.16 > 1.50 → disabled, reason `budget` |
| Arm cap | `min(2 × est_arm, 1.50 − spent so far)`; hit → status `capped`, remaining arms not started, verdict `Inconclusive` |
| Unreported usage on a probe run | charged at that arm's cap |
| Quota | skip at or above `quotaSkipPercent` (80) of the cached account window; **cache only**, never the provider endpoint |
| Learning bucket | untouched; its USD 0.25/day cap neither funds nor is reduced by probes |

## R1–R7 → tests

| R | Static/unit test (PR 4a) | Live proof (P0, then gated suite) |
|---|---|---|
| R1 filesystem | `TestSandboxSettings_Table`, `TestDecider_AllowsInsideArmOnly`, `TestMaterialise_NoRemoteNoCredentialsNoRealGit` | `TestLive_WriteOutsideArmDenied`, `TestLive_ReadDecoyCredentialDenied`, `TestLive_BuiltinReadOutsideArmDenied` |
| R2 network | `TestSandboxSettings_Table` (allowlist, denylist, MCP-closed flags) | `TestLive_NetworkToNonModelHostDenied`, `TestLive_WebToolsDenied`, `TestLive_NoMCPLoaded`, `TestLive_FailIfUnavailableRefusesToRun` |
| R3 credentials | `TestSandboxEnv_AllowlistOnly`, `TestSandboxEnv_NoDaemonEnvLeak`, `TestSandboxLogs_NoSecrets` | `TestLive_EnvIsAllowlist` |
| R4 resources | `TestArmCaps_TimeoutTurnsCostOutputEachEndArm`, `TestArmEnd_NoSurvivingChildProcess`, `TestStartupSweep_RemovesOrphanArmDirs` | — |
| R5 side effects | follows R1–R3; `TestMaterialise_NoRemoteNoCredentialsNoRealGit` | `TestLive_WriteOutsideArmDenied` |
| R6 data | `TestStoredArtifacts_Redacted`, `TestRetention_*`, `TestForgetWorkspace_DeletesProbeArtifacts` (PR 4b) | — |
| R7 tripwires | `TestTripwire_*` | `TestLiveProbe_EndToEnd` (tripwires run clean) |

## Decisions

- ADR-0037 holds the design, Accepted 2026-10-20: D1–D14 as recommended, including the live P0 spike (D14).
- **Sandbox/isolation code is `peer-sonnet`**, not `peer-glm`: it is security-sensitive, the failure modes are subtle (env
  inheritance, settings merge order, tripwire completeness) and GLM runs low-thinking. GLM does the mechanical work (S0, D11,
  store/wsapi/CLI plumbing, UI) from step-by-step briefs per `docs/WORKSPACE_PROTOCOL.md`.
- S0 and D11 ship as **two small PRs** rather than one: disjoint write scopes (store/wsapi/UI vs `service/reflect.go`), independent
  review, no stacking.
- The P0 spike is a **real PR** (findings + gated live suite), not a throwaway: the live tests become the regression suite for the
  sandbox in 4a.
- **Probe-lite on macOS only** (Human, 2026-10-22): Linux/Windows have no Probe button and a stated reason; read allow-list and the per-probe canary are mandatory; the keychain test was approved and run (ADR-0037 addendum).
- **PR sequencing:** 4a (arm + sandbox) is off `develop`; 4b stacks on 4a's branch because it needs the arm (retarget to `develop` after 4a merges); 4c is built against the frozen wire contract above in parallel and merges after 4b.
- No users yet (2026-10): no compatibility shims or migration code for the new tables/config; the break is noted in the PR.

## Progress

- [x] PR 1: ADR-0037 + this plan (docs).
- [ ] PR 2a: S0 prevalence scan.
- [ ] PR 2b: D11 reflector retry.
- [x] PR 3: P0 spike and ADR addendum: conditional PASS on macOS (24 live calls, USD 0.068); keychain, Linux, Windows untested.
- [ ] PR 4a: arm + sandbox, reviewed (macOS only; keychain test done 2026-10-22: unreachable under the allow-list, ADR-0037 addendum).
- [ ] PR 4b: probe service.
- [ ] PR 4c: UI.

## Validation

(Filled in as each criterion is confirmed; every AC gets a line before this file moves to `docs/plans/completed/`.)

### PR 3 — P0 spike (2026-10-20, claude 2.1.303, macOS 27 arm64, SDK v0.3.2; 24 live calls, USD 0.068)

Staged evidence (a single live run of the final code was not possible within the cap); an independent read-only review found the first
round's PASS overstated, and the hardening round re-measured or downgraded each flagged claim (ADR-0037 addendum has the table).

- AC12 R1 write: `TestLive_WriteOutsideArmDenied`, `TestLive_WriteInsideArmAllowed` — confirmed; documented exception: the CLI's per-session `$TMPDIR` is writable.
- AC13 R1 read: `TestLive_ReadDecoyCredentialDenied`, `TestLive_BuiltinReadOutsideArmDenied` — confirmed for decoys with the **allow-list** read policy (including an unlisted third-location path); the deny-list is partial by construction. Real credential files were deliberately not read; keychain reachability is **untested (open risk, needs the Human's approval)**.
- AC14 R2 network: `TestLive_NetworkToNonModelHostDenied`, `TestLive_ModelCallSucceeds`, `TestLive_WebToolsDenied`, `TestLive_NoMCPLoaded` — confirmed for TCP/curl and DNS answers; DNS-query exfiltration, UDP, ICMP, unix sockets untested.
- AC15 fail-if-unavailable: **partial**. `TestLive_FailIfUnavailableRefusesToRun` proves fail-closed at exec; the flag itself could not be isolated on macOS.
- AC16 launch shape + R3: `TestLive_UnderSMINDLaunchShape`, `TestLive_EnvIsAllowlist`, `TestSpike_LauncherScrubsEnv` — confirmed for the process env; the login needs `USER`.
- AC17 attestation: no model-free method exists; one canary call per probe (~USD 0.002) — recorded in ADR-0037 §3.
- AC18 verdict: **conditional PASS on macOS** (R1 write, R2, R3-env, read allow-list, built-ins as measured); PARTIAL items: keychain, `failIfUnavailable`, toolchains beyond sh/git/curl, Linux/Windows. The probe arm PR (4a) must not claim more than the addendum.
