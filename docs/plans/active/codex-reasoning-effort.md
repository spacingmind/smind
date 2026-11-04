# Codex reasoning effort (per-run, discovered per model)

Status: spec (docs-only PR), 2026-11-04. Owner: Lead of task `codex-reasoning-effort`.

## Problem

`internal/taskrunner/thinking.go` claims Codex has "no reachable per-turn
reasoning-effort knob", and `runCodexNative` ignores `thinkingLevel`. That is
outdated: the app-server exposes the knob and each model advertises its own
levels. smind should offer them on par with Claude's thinking level and the
GLM/Kimi ACP config option.

## Protocol facts (confirmed, not assumed)

Read in `refs/codex` @ `2c4a957` (`codex-rs/app-server-protocol/src/protocol/v2/`)
and against the **installed codex-cli 0.166.0**
(`codex app-server generate-json-schema --out <dir>`):

| Fact | Evidence |
|---|---|
| `model/list` entries carry `supportedReasoningEfforts: [{reasoningEffort, description}]` and `defaultReasoningEffort` | `model.rs:103-104`, `ReasoningEffortOption` at `model.rs:~132`; 0.166.0 `ModelListResponse.json` has both |
| `ReasoningEffort` on the wire is a **non-empty string** the model advertises (`none/minimal/low/medium/high/xhigh/max/ultra/persistent` or a model-defined custom id) | `protocol/src/openai_models.rs:50-63`; 0.166.0 schema: `{"type":"string","minLength":1}` |
| **`turn/start` takes `effort`** ("Override the reasoning effort for this turn and subsequent turns") | `turn.rs:226-228`; 0.166.0 `TurnStartParams.json` property `effort` |
| `thread/start` has **no** `effort` field in 0.166.0 (only `config`, which could carry `model_reasoning_effort`) | 0.166.0 `ThreadStartParams.json` properties |
| `thread/settings/update` (`thread.rs:~259`) and `turn/settings/update` (`turn.rs:53`, experimental, mid-turn) also exist | refs only |

**Right per-run knob: `turn/start.effort`.** It is stable (non-experimental),
present on the pinned and the installed version, scoped to the turn (and
following turns of the same thread), and smind already builds `turn/start`
in `internal/codex/client.go` `Prompt` (smind opens one thread per run, so
"subsequent turns" never leaks into another run). Rejected alternatives:
`thread/start config.model_reasoning_effort` (free-form config key, no
validation by the server, only at thread creation, and also sticks on resume);
`turn/settings/update` (experimental, mid-turn only).

Omitting `effort` leaves Codex's own default (`defaultReasoningEffort`, e.g.
`medium` for gpt-6.1-sol): smind must send nothing when unspecified.

## Decisions

1. **Wire shape: reuse the existing `thinkingLevel` string** (task.prompt,
   run.start, profiles, queue/failover/automation rows, CLI `--thinking`, MCP
   `task_send`) instead of adding a parallel `effort` field. Its value is
   provider-scoped: Claude keeps `off|standard|extended`; for a codex-native
   run it is the model's effort id verbatim (`low|medium|high|xhigh|...`).
   Why: `thinkingLevel` is already threaded through ~10 layers (store queue
   rows, failover re-dispatch, profile persistence, CLI, MCP, mobile/web
   clients) and a second field would duplicate every one of them with the
   same lifecycle. Cost: `ThinkingLevel.IsValid` (enum-only today) becomes a
   syntax check (lowercase token, `^[a-z][a-z0-9_.-]{0,31}$`) and the
   *membership* check moves to the provider-specific layer
   (`runClaudeNative` rejects anything but off/standard/extended; the codex
   path checks the model's list). Breaking (no users): a codex run with
   `thinkingLevel:"off"` used to be silently ignored; now `off` is rejected
   unless the model lists it (Codex's id for it is `none`).
   **DECISION FOR THE HUMAN** (wire shape): recommend the reuse above; the
   alternative is a new `effort` field beside `thinkingLevel`.
2. **Discovery**: extend `codex.ModelInfo` / `modelListResponse` with
   `Efforts []EffortOption{ID, Description}` and `DefaultEffort`; carry them on
   `taskrunner.ModelInfo` as additive `efforts` / `defaultEffort` wire fields
   so `provider.list` exposes them with the models. No new probe: efforts ride
   the existing `probeCodexModels` (single in-flight per provider,
   `acpModeProbeRetry` back-off, lazy from `provider.list`, cached in
   `acpModes[provider].models`). Hot paths (`provider.list`, run start) read
   the cache only. Hidden models stay skipped.
3. **Validation (run path)**: in `runCodexNative`, resolve the run's model
   (explicit, else catalog default, else none) and its effort list from the
   cache. A non-empty level not in that list → run fails before spawn-turn
   with `taskrunner: invalid reasoning effort "x" for model "m" (supported:
   low, medium, high)`. Never dropped. If the list is unknown (probe not
   finished or old codex) the level passes through and Codex judges it (same
   rule as ADR-0025's undiscovered models). Unspecified → `Prompt` omits
   `effort` entirely.
4. **Profiles**: `profile.thinkingLevel` already persists; its validation
   becomes syntax-only and, when the profile pins a model on a codex runner and
   the catalogue is discovered, membership is checked on set (best effort,
   cache-only). `smind profile` gets the field with no new flag.
5. **UI**: the thinking control is generalised to "Reasoning": for
   claude-native it is the existing off/standard/extended list; for a
   codex-native provider it is the selected model's `efforts` (labels from the
   id, help from the server description, default marked, none shown when the
   model lists no efforts). Web: `run-config-toolbar` / `provider-model-picker`
   / profiles section via `lib/thinking-levels.ts`. Mobile: new-task form and
   composer get a Reasoning row driven by the same `efforts`. CLI `task send`
   / `task new` already take `--thinking`; help text and validation updated.

## Acceptance criteria

- AC1 `model/list` efforts and default are parsed per model (incl. a model
  with fewer levels, a model with none, hidden skipped).
- AC2 `provider.list` model entries carry `efforts` + `defaultEffort` for
  codex-native; Claude/ACP entries are unchanged on the wire.
- AC3 No extra probe: repeated `provider.list` calls spawn one codex
  app-server per provider within the retry window (count-asserting test).
- AC4 A supported effort reaches the wire as `turn/start.effort`; an
  unspecified effort sends no `effort` key.
- AC5 An unsupported effort is rejected with an error naming the supported
  levels; nothing is sent to Codex; Claude/ACP behaviour unchanged.
- AC6 A profile pinning an effort applies it on a run started from the
  profile; `smind profile` shows/sets it.
- AC7 Web and mobile pickers show the real levels per model with the default
  marked, a disabled/empty state when none, light+dark parity.
- AC8 One guarded live call on `gpt-6.1-sol` accepts an effort (evidence
  logged under `~/.smind/dogfood/codex-effort/`).
- AC9 The stale "no reachable knob" comment in `thinking.go` is gone.

## Named tests

Go:
- `internal/codex`: `TestListModelsParsesEfforts`, `TestPromptSendsEffortOnTurnStart`,
  `TestPromptOmitsEffortWhenUnspecified` (fake agent records `turn/start` params).
- `internal/taskrunner`: `TestProbeCodexModelsRecordsEfforts`,
  `TestRunCodexRejectsUnsupportedEffort`, `TestRunCodexAppliesSupportedEffort`,
  `TestRunCodexUnspecifiedEffortSendsNothing`, `TestRunClaudeRejectsCodexEffortLevel`,
  `TestThinkingLevelSyntax`, `TestCodexEffortProbeIsCached` (counts spawns).
- `internal/wsapi`: `TestProviderListCodexEfforts`, `TestTaskPromptCodexEffortRejected`.
- `internal/profiles`: `TestProfilePinsCodexEffort`.
- `cmd/smind`: `TestTaskSendThinkingPassesCodexEffort`.

Web (`vitest`): `thinking-levels.test.ts` (codex levels from model, default
mark, empty), `run-config-toolbar.test.tsx` (codex effort row, claude
unchanged, disabled state), `profiles-section.test.tsx` (pin).
Mobile (`vitest`): effort row selection + empty state.
Live: `~/.smind/dogfood/codex-effort/live.sh` (guarded).

## Level scope and failover (Steward review, required)

Because `thinkingLevel` is reused (decision 1), a level is **scoped to the
provider and model it was validated for**. One function owns this:
`taskrunner.ResolveLevel(catalog ProviderInfo, runner, model, level, explicit) (level, note, err)`
(cache-only: it reads the discovered catalog, never probes).

- **Explicit request** (a human/agent passes a level on task.prompt, run.start,
  CLI `--thinking`, MCP `task_send.thinkingLevel`, or a queue item the caller
  created with an explicit level): validated at **send time** in
  `Registry.StartWhenBusy` (where `thinkingLevel.IsValid` is called today)
  against the provider's runner vocabulary (Claude: off/standard/extended;
  Codex: the model's list when discovered, else pass-through; ACP: none) and
  the run's model. A mismatch is an error naming the supported levels.
- **Inherited level** (failover fallback profile, rate-limit/transient retry,
  queue re-dispatch from a stored `chatQueueRunConfig`, automation re-fire, a
  profile's pinned level, a level stored on an old row): never an error.
  If it is not valid for the provider+model the run will actually use, it is
  **dropped to that provider's own default** (nothing sent / no Option) and a
  visible run note is emitted ("reasoning level `xhigh` is not supported by
  claude-native; using the provider default"). A successor profile's own
  pinned level is applied by the same function (its own pin, validated
  against its own provider+model); the failed run's level is never copied
  across providers (`enqueueFallback` already builds the config from the
  successor profile, `enqueueRetry` re-sends the same provider, so the
  function is the safety net and the single rule, not a new copy path).
- **Model change between sends** (task's model changed, queued item still
  holds the old level): explicit request for the new model rejected at send;
  a queued/inherited level that the new model does not list is dropped with a
  note at dispatch time (`deliverOne`/`RunPrompt`, which re-resolves against
  the model the run uses).
- **Old rows**: `standard`/`extended` (or any level) stored on a codex task,
  queue row, profile or automation can never brick a run: at run time they are
  inherited-class and dropped with a note. Stale `off` on codex likewise.
  No store migration (no users, nothing to rewrite).

Every layer that validates `ThinkingLevel` today, and what changes:

| Layer | Today | After |
|---|---|---|
| `wsapi/handlers.go` task.prompt (1451), run.start (1554) | `IsValid()` enum | syntax-only `IsValid()` (cheap early reject of garbage); membership is the registry's send-time check |
| `runs/registry.go` Start (1097) | `IsValid()` | syntax + `ResolveLevel(explicit=true)` for direct starts; queue/failover/retry paths call it with `explicit=false` |
| `profiles/registry.go` (251) | `IsValid()` | syntax-only; membership best-effort on set (cache-only), never blocks save when the catalogue is undiscovered |
| store rows (`chat_queue` run config JSON, runs, automation rows) | opaque string | unchanged storage; re-resolved as inherited at dispatch |
| `taskrunner.runClaudeNative` switch | unknown falls to default | the resolver strips non-Claude levels first; the switch keeps rejecting if it ever sees one |
| MCP `task_send` / CLI `task send --thinking`, `task new`, `profile` | accept any | pass through; server error surfaces; help text lists per-provider vocab |

## Additional acceptance criteria

- AC10 Failover to a different provider never carries the failed run's level;
  the successor runs with its own profile pin or provider default, with a run
  note when a pin is invalid for its provider+model.
- AC11 An explicit level invalid for the run's provider/model is rejected at
  send time; an inherited/queued one is dropped with a note, never an error.
- AC12 Old `standard`/`extended`/`off` on a codex task or queue row cannot
  fail a run.

## Additional named tests

- `internal/runs`: `TestFailoverCodexToClaudeDropsEffort`,
  `TestFailoverClaudeToCodexDropsThinking`,
  `TestFailoverSuccessorProfileUsesOwnPinnedLevel`,
  `TestQueuedEffortDroppedWhenModelChanges`,
  `TestExplicitEffortRejectedWhenModelChanges`,
  `TestOldStandardLevelOnCodexRowDroppedWithNote`.
- `internal/taskrunner`: `TestResolveLevel` (table: explicit vs inherited x
  claude/codex/acp x supported/unsupported/undiscovered).

## Progress

- [ ] spec PR
- [ ] Go: codex client + discovery
- [ ] Go: run path + validation + profiles + CLI
- [ ] Web UI
- [ ] Mobile UI
- [ ] Frames, live check, mutations

## Validation

(filled in as ACs are satisfied)
