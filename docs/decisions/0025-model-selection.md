# 0025: Model selection on runs and profiles

## Status

Accepted (2026-10-11). The user approved D2 of
[`docs/plans/completed/orchestration-parity-paseo.md`](../plans/completed/orchestration-parity-paseo.md):
a `model` field on the run config and on profiles, sourced from ACP model
config options plus a static Claude catalog; `provider_list` lists models,
and live switching goes through `task_set_option`.

## Context

smind has no model concept. `internal/taskrunner` never passes a model to
any backend, profiles (ADR-0014) carry provider/permissionMode/thinking
only, and `provider.list` returns permission modes but no models. Paseo has
`list_models` and `update_agent settings.model`. Today the model a run uses
is whatever each backend defaults to (a Claude login's default, GLM's
default, Codex's default), so an orchestrator cannot pick a cheaper or
stronger model for a worker, and a human cannot save "Sonnet, low thinking"
as a profile.

What the backends offer:

- **Claude native:** `claudecode.WithModel(name)` sends `--model <name>` at
  spawn (`claude-agent-sdk-go` v0.3.2 `client.go:289`), and
  `Client.SetModel(ctx, *string)` switches mid-session (`nil` resets).
- **ACP (GLM, Kimi):** the agent advertises a config option with category
  `model` in `session/new`'s `config_options` (already captured, see
  `smind-control-parity`), settable with `session/set_config_option`.
- **Codex:** `thread/start` accepts `model`, and the app server has a
  `model/list` request (`refs/codex/.../common.rs:1038`). There is no
  mid-turn switch.

## Decision

### Wire and storage

- `model` (optional string) is added to `run.start`, `task.prompt`,
  agent profiles (`profile.create/update/get/list`), and the MCP `task_send`
  and `profile_list`. **Absent, empty, or NULL means "the provider's
  default"**: no flag is sent and behavior is exactly today's.
- Storage: additive nullable `model TEXT` columns on `runs` (the *requested*
  model, so history shows what was asked for) and `agent_profiles`; the
  queue's stored params (ADR-0021) carry `model` so a queued prompt keeps it.
  No backfill: existing rows are NULL. This is separate from
  `run_usage.model` (ADR-0020), which records what the agent *reported*
  using.
- Model is per run, not per chat: each run spawns/resumes the agent, so
  consecutive runs on one chat may use different models. The chat's
  provider binding (ADR-0016) is unchanged.
- A profile's `model` applies when that profile is selected; an explicit
  `model` on the same call overrides the profile's.
- Selecting a model is **not** a permission decision. An orchestrator may
  pick any model from the catalog. The cost consequence is visible through
  ADR-0020's per-run usage; profile `notes` are where a human steers
  orchestrators.

### Discovery (`provider.list`)

Each `ProviderInfo` gains `models: [{id, label, description?, default?}]`
and `modelsDiscovered: bool`, built per provider kind like modes already are
(ADR-0019 #4, `Runner.ProviderCatalog`, never blocking):

- **claude-native:** a static catalog in code (aliases `sonnet`, `opus`,
  `haiku` plus the current full ids). The catalog is a convenience, not an
  allowlist: any id passing the syntax check below is accepted, so a newly
  released Claude model works without a smind release. `modelsDiscovered`
  is false (static).
- **ACP providers:** the options of the `model`-category config option from
  the same lazy probe `session/new` that discovers modes, cached for the
  daemon's lifetime, with a static empty fallback. Until probed,
  `modelsDiscovered` is false and a supplied model is passed through; the
  agent's own rejection surfaces as the run's error. Once discovered, an
  unknown model is rejected up front with the valid ids.
- **codex-native:** `model/list` from a short-lived app-server session,
  cached with the same probe machinery; if the call is unavailable the
  provider reports no models and accepts a syntax-valid string.

Syntax check for any model string (all providers): 1-100 characters from
`[A-Za-z0-9._:/@\[\]-]`, not beginning with `-` (it reaches
`--model <name>` as argv and JSON fields, never a shell). Violations are
rejected before any spawn.

### Applying a model

- Claude: `WithModel` at spawn/resume.
- ACP: after `session/new`/`session/load` and before the first prompt,
  `session/set_config_option` for the `model`-category option, when a model
  was requested and differs from the current value. A failure fails the run
  start with the agent's error.
- Codex: `model` in the `thread/start`/`thread/resume` params.

### Live switch

Through the existing config-option surface (`run.listConfigOptions`,
`run.setConfigOption`, MCP `task_options`/`task_set_option`), so there is no
new tool:

- ACP: the agent's own `model` option, already listed and settable. PR-C's
  fail-closed allowlist (categories `thought_level` and `model`) covers it.
- Claude native: a **synthetic** option `{configId: "model", category:
  "model", options: <catalog>}` is added to the listing and routed to
  `Client.SetModel` (selecting the catalog's default sends `nil`). It takes
  effect for the running session's next turn.
- Codex: no synthetic option and `setConfigOption` rejects `model` with
  "applies from the next run: pass `model` to task_send", the same shape of
  refusal `run.setPermissionMode` gives Codex (ADR-0019).

### Migration

Additive `ALTER TABLE ... ADD COLUMN model TEXT` steps in the existing
migrations; old daemons ignore the column; old clients that omit `model`
get NULL = provider default. Nothing is remapped or discarded.

## Alternatives considered

- **A dedicated `run.setModel` RPC and `task_set_model` MCP tool.**
  Rejected: duplicates the config-option surface that already exists for
  ACP and that the user asked live switching to go through.
- **Model as a per-chat (bound) field like provider.** Rejected: ADR-0016
  binds provider because sessions are provider-specific; a model can change
  between turns and Paseo's `update_agent` changes it live.
- **A strict allowlist from `provider_list` for every provider.** Rejected
  for Claude (a static list goes stale on every model release) and for
  undiscovered ACP/Codex catalogs (a failed probe would block all runs).
  Syntax validation plus the backend's own error is the floor.
- **Proxy-side model routing (ADR-0020 §5).** Orthogonal: it routes
  external proxy callers, and smind's own runs bypass the proxy.

## Rationale

The choice of model is the cheapest lever an orchestrator or a profile has
over cost and quality, and every backend already exposes it. Reusing the
mode-catalog machinery and the config-option RPCs keeps the new surface to
one optional field, one catalog field, and no new tools, while NULL = default
keeps every existing client and stored row working unchanged.
