# Recipe instance detail page (providers epic PR6b)

Size: S (3) — scope 1, PRs 1, risk 0, decisions 0, verify 1 (web only, no daemon change)

## Problem
The Providers detail of a recipe instance (provider.addFromRecipe: Z.AI, Qwen, DeepSeek, ...) reads like the
Claude CLI login: "Ready (CLI login)" and "Managed externally via CLI ... CLI login kept in its own config
directory". Such an instance authenticates with the key stored in smind and runs with a private config dir
`SMIND_HOME/providers/<id>/claude` (unless inheritClaudeConfig). `provider.test` only checks the binary/health.

## Facts (checked in code)
- `provider.list` does NOT expose the private config dir, inheritClaudeConfig, or the endpoint URL/host
  (only `endpointKind`, `recipe`, `secrets[]`). So the UI shows "private (managed by smind)"; exposing the dir,
  the inherit flag and the host is a daemon follow-up (tiny additive fields), NOT done here.
- ProviderEnvField (generic Config directory block, daemon's own CLAUDE_CONFIG_DIR) is already skipped for
  `info.custom` since PR6; a test pins that it stays hidden for a recipe instance.

## Acceptance criteria
- AC1 Recipe instance (`info.recipe` set): readiness shows "Key set" / "No key set" from `secrets[]` (never
  "Ready (CLI login)"); when the instance has no secret rows (keyless local) it shows "Ready"; a failed check still shows "Not ready — <detail>".
- AC2 Recipe instance: the external block says it is managed by smind with the stored key, shows
  "Config directory: private (managed by smind)", and describes Test as a binary/health check only (no network, key unused).
- AC3 Recipe instance never renders the generic config-dir field or any daemon-env path.
- AC4 Non-recipe Claude instance / claude-native / codex / ACP: text and behaviour unchanged.

## Test scenarios (provider-detail.test.tsx, "recipe instances")
- recipe instance with key set -> readiness "Key set", no "CLI login" text anywhere.
- recipe instance with key not set -> "No key set".
- recipe instance: external block has "private (managed by smind)" and no `provider-env-*` testid.
- plain instance (no recipe) still "Ready (CLI login)" and old external text.
- claude-native existing test (Ready (CLI login)) stays green.

## Decisions
- Recipe detection is `info.recipe !== undefined` (the only wire marker of a recipe-made instance).

## Progress
- [ ] implement  - [ ] tests  - [ ] mutations  - [ ] frames  - [ ] PR

## Validation
(filled at the end)
