# Provider layer v2 (ADR-0046)

Status: active (2026-10-30). Binding text: [`docs/decisions/0046-provider-spec-harness-overlay.md`](../../decisions/0046-provider-spec-harness-overlay.md) (Proposed; the ten
decisions are the Supervisor default 2026-10-30 under the Human's "làm tất", Human may veto). Research input: a local-only note (`docs/research/local/providers-epic-research.md`,
gitignored); it is cited here as the "local research note" and its section numbers are used.

Size: XL as one item (9+) → split into PR0..PR6 below. Scope 2, PRs 2, risk 2 (secrets, launch env, relay), decisions 0 (ADR written, all taken), verification 2 (live provider, UI
screenshots; the live part is capped by the probe budget).

## Ground rules

- **No real key anywhere.** Tests use sentinel strings and a fake Anthropic-compatible / OpenAI-compatible HTTP server on a free local port (`httptest`), so no real key and no real
  model call is needed. Any live call needs the Steward's OK first and goes in the ledger below.
- **Probe budget ≤ USD 2 TOTAL** for the whole epic: haiku-class or free calls only; free local Ollama/LM Studio calls only if already installed (never install them).
- Isolated daemons only: own `SMIND_HOME=$(mktemp -d)`, free port; never `~/.smind`, `:4648` or the launchd service. Tests that pick a home dir use `HOME=$(mktemp -d)`.
  Kill only PIDs you started. No global installs (`npx`/`bunx` into a temp dir with an isolated `HOME` is fine).
- No compat shims (no installed base): note each break in the ADR and the release notes. **No release**: the Human triggers it.
- UI: reuse shared stores/pickers and the icon registry (`lib/icons.ts` + drift test); every UI PR has light+dark frames at 1280 and 390, every transition frame looked at.
  Screenshot scripts never click Send against a live provider (pin a fake daemon).
- If any PR touches provider usage endpoints (it should not): rate-limited, budgeted, single-flight (ADR-0028).
- Each PR: one load-bearing-test mutation per AC group (the test must FAIL, a build failure proves nothing), listed in the PR body; `task test` and `task lint` before the PR.

## Sequence and parallelism

```
PR0 (this: ADR + plan + spike) ──► PR1 registry generalisation + Kimi fix
                                     ├──► PR2 secrets + typed env ──┐
                                     ├──► PR4 Codex endpoint         ├──► PR3 claude-code recipes ──► PR6 web/mobile UX
                                     └──► PR5 five ACP providers ────┘ (PR5 independent of PR3; PR6 also lists ACP icons)
```

PR1 first. PR2, PR4, PR5 run in parallel after PR1 (disjoint write scopes: config + secret store / `internal/codex` / ACP specs). PR3 after PR1 + PR2. PR6 after PR3 (and after
PR5 if the ACP icons are to ship with it). Never branch a PR off an unmerged PR; the Supervisor merges on green. Docs update travels with each PR.

## Data model (ADR-0046 section 1)

`Spec { ID, Label, Description, Harness, Extends, Launch{Command, Env}, Models+ModelsMode, ModelAliases, DisallowedTools, Endpoint{Kind, BaseURL, KeyRef, Timeout}, Thinking, Usage }`;
instance overlay = `{Label, Description, Launch.Env, Launch.Command, Models(+mode), ModelAliases, DisallowedTools, Endpoint, Thinking}`; `EnvValue = literal | secretRef | passthrough`.
Typed env: per-harness allowlist of names with value kinds (url, secret, plain, int, path); see the ADR table.

## Threat model (ADR-0046 section 7), summarized

Spike result (2026-10-30, `claude` 2.1.310, macOS, fake server, sentinel keys): Claude Code does **not** scrub `ANTHROPIC_AUTH_TOKEN`/`ANTHROPIC_API_KEY` from its Bash tool env by
default (nor with `CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=0`); with `=1` both are hidden along with many `*_KEY`/`*_TOKEN`/`*_SECRET`/`*_PASSWORD` names, while `GITHUB_TOKEN` and arbitrary
names survive; `=1` forces permission mode back to `default`. Mitigation is best effort and a defence, not a boundary. Recipe to re-run is in the ADR.

## The five native ACP providers (PR5)

| Provider (id) | Command | Install | Login / health message | Discovery | Best-effort |
|---|---|---|---|---|---|
| OpenCode (`opencode`) | `opencode acp` | `npm i -g opencode-ai`, `brew install sst/tap/opencode` or the vendor script | none for local models or your own provider key in `opencode.json`; hosted models: `opencode auth login` **[inferred, verify in the spike]** | ACP config options (models, modes) after `session/new` | no, but **spike first** (does ACP mode expose models/modes/permissions the way smind expects? Paseo chose `serve` HTTP for OpenCode, reason unknown) |
| Copilot CLI (`copilot`) | `copilot --acp` (Paseo's `copilot-acp-agent.ts` default) | `npm i -g @github/copilot` | needs a paid GitHub Copilot plan; run `copilot` once and `/login` **[inferred]**; ACP is a public preview | ACP config options | **yes**: closed source, paid, may not run on the Human's machine |
| Cline (`cline`) | `cline --acp` (Paseo pins `npx -y cline@3.0.73 --acp`) | `npm i -g cline` | needs a provider key configured in Cline (`cline auth`) **[inferred]** | ACP config options | no (open source), but a key is needed for a real run |
| Goose (`goose`) | `goose acp` | binary from block.github.io/goose, or `brew install block-goose-cli` **[inferred]** | `goose configure` to pick a provider/key **[inferred]** | ACP config options | no (open source), a provider key is needed for a real run |
| Cursor CLI (`cursor`) | `cursor-agent acp` | `curl https://cursor.com/install -fsS \| bash` **[inferred]** | needs a paid Cursor account; `cursor-agent login` **[inferred]** | ACP config options; Paseo needs special model-catalog handling (`cursor-acp-catalog`), check in PR5 | **yes**: closed, paid |

Each is one registry row (command, harness `acp`, health command, login message, learning-preface delivery as the other ACP specs), one icon in `lib/icons.ts`, one health check that
names what is missing (binary not on PATH, not logged in), and model discovery through ACP config options (lazy probe, `ModelsDiscovered`). **Gemini CLI is explicitly out** (not
requested; one data row later if wanted). Health-check texts marked [inferred] are verified against the real CLI's `--help` output, never a live model call; a real-login run of the
paid ones is best-effort and goes through the budget ledger or is recorded as "not verified".

## Recipe catalog (PR3): embedded data, rows 1-12

Embedded file `internal/providers/recipes.yaml`. `source`: `paseo-docs` (Paseo's docs), `vendor-docs` (the vendor's own page fetched 2026-10-30), `inferred` (from memory or by analogy:
must be live-probed or flagged in the UI before it ships). `verifiedAt` empty = not verified against a live endpoint. Common to every claude-code recipe: `harness: claude-code`,
`endpoint.kind: anthropic`, `disallowedTools: [WebSearch]`, private `CLAUDE_CONFIG_DIR`, thinking hidden unless noted, `ANTHROPIC_API_KEY=""` where the vendor needs the empty value.

| # | Recipe id | Harness | Endpoint / env (names; secrets are references) | Models | Caveats | source | verifiedAt |
|---|---|---|---|---|---|---|---|
| 1 | `zai-glm` | claude-code | `ANTHROPIC_BASE_URL=https://api.z.ai/api/anthropic`, `ANTHROPIC_AUTH_TOKEN`=secret, `API_TIMEOUT_MS=3000000`, `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`; optional `ANTHROPIC_DEFAULT_{OPUS,SONNET}_MODEL=glm-5.3[1m]`, `…HAIKU…=glm-5.3-flash[1m]`, `CLAUDE_CODE_AUTO_COMPACT_WINDOW=1000000` | `glm-5.3`, `glm-5.3-flash` (Paseo's docs list `glm-4.5-air`, `glm-5-turbo`, `glm-5.1`, `glm-4.7`) | GLM-worded 429 ("usage limit reached for N hour", UTC+8 reset); AUTH_TOKEN not API_KEY; CN endpoint `open.bigmodel.cn` inferred | endpoint/token/timeout/WebSearch: `paseo-docs`; model ids, compact window: `vendor-docs` (docs.z.ai/devpack/tool/claude); CN: `inferred` | empty |
| 2 | `qwen-dashscope` | claude-code | `ANTHROPIC_BASE_URL=https://coding-intl.dashscope.aliyuncs.com/apps/anthropic`, `ANTHROPIC_AUTH_TOKEN`=secret (`sk-sp-…`); PAYG variant `https://dashscope-intl.aliyuncs.com/apps/anthropic` with `ANTHROPIC_API_KEY`=secret | `qwen3.5-plus` (default), `qwen3-coder-next`, `kimi-k2.5`, `glm-5`, `MiniMax-M3`; PAYG adds `qwen3-max`, `qwen3.5-flash`, `qwen3-coder-plus`, `qwen3-coder-flash` | key made in the Singapore region; coding plan is personal interactive use only | `paseo-docs`; CN region `inferred` | empty |
| 3 | `deepseek` | claude-code | `ANTHROPIC_BASE_URL=https://api.deepseek.com/anthropic`, `ANTHROPIC_AUTH_TOKEN`=secret, `ANTHROPIC_MODEL`/`…OPUS…`/`…SONNET…=deepseek-flash[1m]`, `…HAIKU…=deepseek-flash`, `CLAUDE_CODE_SUBAGENT_MODEL=deepseek-flash`, `CLAUDE_CODE_EFFORT_LEVEL=max`, `CLAUDE_CODE_AUTO_COMPACT_WINDOW=786432` | `deepseek-flash`, `deepseek-v4-pro` | ignores `cache_control`, `mcp_servers`, `budget_tokens`; unknown `claude-opus*` bills as Pro; unknown non-`claude` names 400 | `vendor-docs` (api-docs.deepseek.com, both pages) | empty |
| 4 | `kimi-moonshot` | claude-code | `ANTHROPIC_BASE_URL=https://api.moonshot.ai/anthropic` (Kimi Code membership: `https://api.kimi.com/coding/`), `ANTHROPIC_AUTH_TOKEN`=secret | `kimi-k3.1-pro`, `kimi-k3.1-pro-highspeed` (older `kimi-k2.5` in row 2) | base URL for Claude Code **inferred**; ships only after a live probe or flagged "unverified" in the UI | models `vendor-docs`; base URL `inferred` | empty |
| 5 | `minimax` | claude-code | `ANTHROPIC_BASE_URL=https://api.minimax.io/anthropic`, `ANTHROPIC_AUTH_TOKEN`=secret, `ANTHROPIC_MODEL`/`…SONNET…`/`…OPUS…`/`…HAIKU…=MiniMax-M3.1-Flash[1m]`, `CLAUDE_CODE_AUTO_COMPACT_WINDOW=524288`, `CLAUDE_CODE_EFFORT_LEVEL=max` | `MiniMax-M3.1-Flash` (row 2 lists `MiniMax-M3`) | thinking cannot be turned off: **thinking hidden**; CN base `api.minimaxi.com` inferred | global `vendor-docs` (platform.minimax.io); CN `inferred` | empty |
| 6 | `openrouter` | claude-code | `ANTHROPIC_BASE_URL=https://openrouter.ai/api`, `ANTHROPIC_AUTH_TOKEN`=secret, `ANTHROPIC_API_KEY=""` (must be set and empty), optional `CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1`, role overrides `ANTHROPIC_DEFAULT_*_MODEL`, `CLAUDE_CODE_SUBAGENT_MODEL` | any OpenRouter id, e.g. `~anthropic/claude-sonnet-latest[1m]`, `qwen/qwen3-coder` | `[1m]` suffix needed for 1M context; only first-party Anthropic routing guaranteed; spend is the user's | `vendor-docs` (openrouter.ai Claude Code guide) | empty |
| 7 | `ollama` | claude-code | `ANTHROPIC_BASE_URL=http://localhost:11434`, `ANTHROPIC_AUTH_TOKEN=ollama` (literal, not a secret), `ANTHROPIC_API_KEY=""` | any tools-tagged model, e.g. `qwen3.5`, `kimi-k2.7-code:cloud` | needs 64k+ context; hosted WebSearch not fully supported; no key to store | `vendor-docs` (docs.ollama.com) | empty |
| 8 | `lm-studio` | claude-code | `ANTHROPIC_BASE_URL=http://localhost:1234`, `ANTHROPIC_AUTH_TOKEN=lmstudio` (literal unless "Require Authentication" is on) | e.g. `openai/gpt-oss-20b` | only `POST /v1/messages` documented | `vendor-docs` (lmstudio.ai) | empty |
| 9 | `litellm` | claude-code (codex variant flagged) | `ANTHROPIC_BASE_URL=http://<host>:4000` (or `/anthropic`), `ANTHROPIC_AUTH_TOKEN`=secret; codex variant `OPENAI_BASE_URL`+`OPENAI_API_KEY` | names must match the LiteLLM `config.yaml` exactly, no `[1m]` inside config names | MCP tool search off on non-first-party hosts unless `ENABLE_TOOL_SEARCH=true`; Bedrock needs `CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS=1`; codex route unconfirmed | claude route `vendor-docs` (docs.litellm.ai); codex route `paseo-docs` + `inferred` | empty |
| 10 | `anthropic-compatible` | claude-code | user supplies base URL + token + model ids | user | the Paseo "my-proxy" pattern with `disallowedTools:["WebSearch"]` | `paseo-docs` | empty |
| 11 | `openai-responses-compatible` | codex-app-server | `OPENAI_BASE_URL`, `OPENAI_API_KEY`=secret; models required | user | **Responses API only** (Codex schema has a single `wire_api` value) | `paseo-docs` + codex config schema | empty |
| 12 | `chat-completions-via-opencode` | acp (OpenCode) | OpenCode's own provider config (`opencode.json`), env for keys | user | not Codex-compatible; depends on the OpenCode spike (PR5); flagged unverified until then | `inferred` | empty |

Rule: a recipe with `source: inferred` and no `verifiedAt` is shown in the UI with an "unverified" note (PR6) and the public docs table repeats the flag.

## Acceptance criteria and named tests per PR

### PR0: ADR-0046 + this plan + the security spike (docs only)

- AC0.1 ADR-0046 exists, numbered after 0045, records the data model (section 1), the threat model with the spike result (section 7) and the ten decisions each as "Supervisor default 2026-10-30 under the Human's 'làm tất', Human may veto".
- AC0.2 The spike answers "does Claude Code scrub `ANTHROPIC_AUTH_TOKEN` from its Bash tool env?" with the version, exact result with and without `CLAUDE_CODE_SUBPROCESS_ENV_SCRUB`, and a re-runnable recipe; no real key and no network but localhost.
- AC0.3 This plan carries PR1..PR6 acceptance criteria, named tests, wire/schema changes, docs to update, the sequence, the recipe table, the ACP table and the budget ledger.
- Tests: none (docs). Validation: markdown sanity (headings, table column counts, links resolve), `git diff --stat`, no binaries.

### PR1: registry generalisation + Kimi fix (daemon, no new vendors)

Scope: `Spec` gains `Models`+`ModelsMode`, `ModelAliases`, `DisallowedTools`; `Harness` replaces `Runner` internally (wire `runner` values kept); `Extends` may name any harness-bearing spec; the instance overlay accepts `command`, `models`, `additionalModels`, `disallowedTools`; `validateInstance` relaxed accordingly (still the two directory env keys: typed env is PR2); `staticModels` honours instance models; default-model resolution stops falling back to the base when the instance has models; the claude launch passes `WithDisallowedTools`; Kimi re-pointed to kimi-code and the docs fixed; a model id unknown to smind is shown raw.
- AC1.1 An instance of `claude-native` with `models` lists exactly those models in `provider.list` and rejects none that are syntax-valid.
- AC1.2 `disallowedTools` reaches the CLI argv (`--disallowedTools`).
- AC1.3 An instance without models still lists the Claude catalog; with `additionalModels` the list is catalog + those ids merged by id.
- AC1.4 Config validation names the field (`providers.custom[1].disallowedTools[0]`); an instance of an ACP spec with a `command` override validates.
- AC1.5 Existing instances and ACP entries behave unchanged (no wire break in `provider.list`).
- AC1.6 The default model of an instance with models is not the base's default; an instance with neither models nor endpoint still inherits it.
- AC1.7 The Kimi spec launches `kimi acp` and its install/health text and the public docs name Kimi Code (`MoonshotAI/kimi-code`), not `kimi-cli`.
- AC1.8 A model id the registry does not know is displayed as the raw id.
- Tests: `TestInstance_ModelsReplaceClaudeCatalog`, `TestInstance_AdditionalModelsMergeByID`, `TestInstance_DefaultModelNotInheritedWhenModelsSet`, `TestInstance_DefaultModelInheritedWhenNoModelsNoEndpoint`, `TestClaudeLaunch_DisallowedToolsInArgv` (fake CLI records argv), `TestCustomProvider_InstanceOverlayValidation_FieldNamed`, `TestCustomProvider_ACPInstanceCommandOverride`, `TestKimiCommand_KimiCode`, `TestProviderList_AdditiveFields`, `TestModelDisplay_UnknownIDShownRaw`.
- Mutations: drop the `WithDisallowedTools` call (argv test must fail); make `staticModels` ignore the instance (replace test must fail); restore the base-default fallback (default-model test must fail).
- Wire/schema changes: `provider.list` additive fields (`harness`, `extends`, `models[].source`), config `providers.custom[].{models,additionalModels,disallowedTools,command}` for instances; no store migration.
- Docs to update: `public-docs/providers.md` (Kimi Code, instance overlay keys), `docs/homelab.md` config table, `docs/decisions/0042-plugin-system.md` pointer to ADR-0046.

### PR2: typed env + provider secret store (daemon; parallel with PR4, PR5)

Scope: per-harness env allowlist with value kinds; `secret:` references and `passthrough`; `provider_secrets` table and migration; `provider.setSecret`/`provider.clearSecret` (human-only, write-only); `provider.list` reports `{set, fingerprint}`; launch resolves refs into the child env; secretdeny extension; redaction; the scrub variable on claude-code instances that hold a secret, if the measurement below passes.
- AC2.1 A secret never appears in `config.yaml`, `provider.list`, events, logs, argv or error text (sentinel grep over a full fake run).
- AC2.2 A name outside the allowlist is rejected for claude-code and codex instances, field named; a `secret`-kind name with a literal value is rejected.
- AC2.3 `setSecret`/`clearSecret` are refused for run-scoped callers and the automation origin (like `provider.setEnv`), and no method returns a value; over the relay the response carries none.
- AC2.4 The launch env contains the resolved value, last-wins over the daemon env; `passthrough` copies from the daemon env.
- AC2.5 secretdeny emits the Bash deny rules (`env`, `printenv`, `set`, `export -p`, `declare -x`, `echo $NAME`) and the `/proc/*/environ` and provider-dir `Read` denies for each secret env name; docs call them best effort.
- AC2.6 Measured against the real `claude` with the fake Anthropic server and sentinel keys (not in CI; recorded in Validation): with `CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=1` smind's permission modes still work and smind's orchestration MCP stdio token still reaches its server. Pass → the scrub var is set on instances holding a secret; fail → it is not, and the docs say the exposure remains. A hermetic test asserts which of the two the code does.
- AC2.7 Deleting an instance (or a secret) removes its secret rows.
- Tests: `TestProviderSecret_NeverInConfigYaml`, `TestProviderSecret_RedactedInEventsAndLogs`, `TestProviderSecret_RefusesAgentCaller`, `TestProviderSecret_RelayResponseCarriesNoValue`, `TestProviderSecret_ListShowsFingerprintOnly`, `TestProviderEnv_AllowlistPerHarness`, `TestProviderEnv_SecretKindRejectsLiteral`, `TestProviderEnv_PassthroughCopiesDaemonEnv`, `TestProviderEnv_NotInArgv`, `TestSecretdeny_ProviderEnvNames`, `TestProviderSecret_DeletedWithInstance`, `TestScrubEnv_SetOnlyWhenPolicySaysSo`, `TestMigration_ProviderSecretsTable`.
- Mutations: put the resolved value into argv (argv test fails); log the env map (redaction test fails); drop the allowlist check (allowlist test fails); let a run-scoped caller through (refusal test fails).
- Wire/schema changes: new RPCs `provider.setSecret`, `provider.clearSecret`; `provider.list` per-secret `{name, set, fingerprint}`; `providers.custom[].env` value shape `{literal|secret|passthrough}`; **store migration** adding `provider_secrets`; breaking: inline secret-kind values rejected.
- Docs to update: `public-docs/providers.md` (secrets, the exposure and its residual), `docs/decisions/0037` pointer (scrub now measured), `SECURITY.md` if it lists secret locations.

### PR3: claude-code recipe catalog (data) + vendor rate-limit profiles (after PR1 + PR2)

Scope: embedded `recipes.yaml` (rows 1-10, the claude-code rows; row 11 belongs to PR4, row 12 to PR5), `provider.recipes` and `provider.addFromRecipe` RPCs, private config dir with the inherit toggle, thinking hidden per recipe, GLM default path = Claude Code + Z.AI (legacy `glm-acp-agent` kept one release, `agents.glm.maxTurns` deleted), the rate-limit vendor profile (ADR section 8).
- AC3.1 Adding Z.AI from its recipe needs only a key.
- AC3.2 A run against a fake Anthropic-compatible server shows the expected `ANTHROPIC_BASE_URL`, `--disallowedTools WebSearch`, `--model` and private `CLAUDE_CONFIG_DIR`; the sentinel key is the bearer token and nowhere in argv.
- AC3.3 A 429 with GLM wording classifies as rate limited with the UTC+8 reset on the **instance id only**; `claude-native` keeps running.
- AC3.4 A 429 from an unknown vendor classifies through the generic patterns.
- AC3.5 Every recipe has `source` and `verifiedAt`; rows with `inferred` and no `verifiedAt` are flagged in the doc table and in `provider.recipes`.
- AC3.6 No literal secret exists in the catalog; `thinking: hidden` recipes expose no thinking option in the run config.
- AC3.7 Inherit toggle on: no private dir is set and `~/.claude` is used.
- Tests: `TestRecipes_AllParseAndValidate`, `TestRecipe_NoSecretLiteralsInCatalog`, `TestRecipe_ZAI_RunAgainstFakeAnthropicServer`, `TestRecipe_InheritToggleUsesDefaultConfigDir`, `TestRecipe_ThinkingHiddenHidesOption`, `TestRecipe_InferredRowsFlagged`, `TestRateLimit_VendorProfile_GLMOnInstance`, `TestRateLimit_GenericUnknownVendor`, `TestRateLimit_InstanceCooldownLeavesBaseRunning`, `TestGLMDefault_IsClaudeCodeZAI`, `TestAddFromRecipe_WritesInstanceAndSecret`.
- Mutations: put a literal key in the catalog (no-secret test fails); classify on the base id (instance-only test fails); drop the private dir (run test fails).
- Wire/schema changes: new RPCs `provider.recipes`, `provider.addFromRecipe`; `provider.list` additive `recipe`, `endpoint.kind`, `notes[]`, `thinking`; config key `agents.glm.maxTurns` removed (breaking); `providers.custom[].inheritClaudeConfig`.
- Docs to update: `public-docs/providers.md` (recipe table with the unverified flags, GLM default path), `README.md` provider list, release-notes note of the breaks.

### PR4: Codex endpoint instances (parallel with PR2, PR5; after PR1)

Scope: `thread/start` and `thread/resume` gain `config` and `modelProvider`; `Endpoint{openai-responses}` expands to `model_providers.<id>`; recipe row 11 (generic Responses URL) and the LiteLLM/OpenRouter variants flagged unverified.
- AC4.1 The base URL gets `/v1` exactly once and trailing slashes are stripped.
- AC4.2 `env_key` and `requires_openai_auth:false` are set only when a key is configured.
- AC4.3 A chat-completions-only endpoint is documented as unsupported with a clear error, not a late 404.
- AC4.4 `CODEX_HOME` private dir default for endpoint instances.
- AC4.5 `models` is required for a Codex endpoint instance (no discovery); validation names the field.
- Tests: `TestCodexThreadStart_ModelProviderConfig` (fake app-server asserts the JSON), `TestCodexThreadResume_ModelProviderConfig`, `TestCodexEndpoint_BaseURLNormalization`, `TestCodexEndpoint_NoKeyNoEnvKey`, `TestCodexEndpoint_ModelsRequired`, `TestCodexEndpoint_ChatOnlyUnsupportedMessage`.
- Mutations: drop the `/v1` append (normalization test fails); always set `env_key` (no-key test fails).
- Wire/schema changes: Codex app-server request params (additive); `providers.custom[].endpoint`; no store migration.
- Docs to update: `public-docs/providers.md` (Responses-only constraint), recipe table.

### PR5: the five ACP providers (parallel with PR2, PR4; after PR1; may split OpenCode / Copilot+Cline / Goose+Cursor)

Scope: five registry rows (table above), icons in `lib/icons.ts`, health checks naming the missing binary/login, model discovery through ACP config options, optional ACP `clientCapabilities` and a `supportsMcpServers:false` opt-out, probe env `NO_BROWSER=true`; the OpenCode spike first (its result decides the OpenCode row and recipe row 12); the paid/closed ones best-effort.
- AC5.1 Each provider lists models and modes through `provider.list` after the lazy probe (fake-ACP agent in tests; real CLI only for the free `--help`/`--version` health checks).
- AC5.2 A fake-ACP conformance run (existing `fakeagent`) passes permission, resume and cancel for a spec registered from a row alone (no per-vendor Go switch).
- AC5.3 Health check says what is missing (binary not on PATH, login needed) and which login each needs; Copilot and Cursor say "needs a paid account".
- AC5.4 The OpenCode spike is recorded in Validation: does `opencode acp` expose models/modes/permissions through ACP config options and stream tool events? Pass → ship; fail → swap Copilot into the primary slot and mark OpenCode "partial".
- AC5.5 `supportsMcpServers:false` suppresses `mcpServers` in `session/new`.
- AC5.6 Any real run of a paid provider is best-effort: recorded as run (ledger) or "not verified".
- Tests: `TestACPProvider_OpenCode_SpecRegistersWithoutSwitchEdits`, `TestACPProvider_AllFive_RowsRegister`, `TestACPProvider_ProbeUsesNoBrowser`, `TestACPClient_SupportsMcpServersOptOut`, `TestProviderHealth_MissingBinaryNamed`, `TestProviderHealth_LoginMessagePerProvider`, `TestACPDiscovery_ConfigOptionsModelsAndModes`, `TestIcons_NewACPProvidersRegistered` (drift test).
- Mutations: hard-code a provider id in the ACP launch path (the "rows only" test fails); drop `NO_BROWSER` from the probe env (probe test fails).
- Wire/schema changes: registry rows only; `provider.list` rows for five new ids; optional `providers.custom[].acp.{supportsMcpServers,clientCapabilities}`; no store migration.
- Docs to update: `public-docs/providers.md` (five providers: install, login, best-effort flags), `docs/ARCHITECTURE.md` provider list if present.

### PR6: web + mobile provider UX (after PR3)

Scope: web Settings → Providers shows recipe cards, "Add from recipe" (pick, paste key, create), secret fields, model list editor, disallowed-tools chips, inherit-`~/.claude` toggle, unverified badge; mobile read-only list of instances/recipes; offline fallback unchanged; icons via the registry.
- AC6.1 A stored secret is never shown (field empty, "set · ••••<last 4>").
- AC6.2 Light and dark frames at 1280 and 390, including transitions, reviewed by eye; empty, loading, error, set and unverified states.
- AC6.3 An instance wears its base's mark; a recipe may ship its own vendor mark through the registry.
- AC6.4 Shared stores and pickers are reused; no new ad-hoc fetch layer.
- Tests: `providers-view.test.ts` extensions, `provider-recipes.test.ts`, `add-from-recipe.test.tsx` (key field is a password input, cleared after submit), icon drift test, mobile `providerCatalog` test, Playwright screenshot script pinned to a fake daemon (no live sends).
- Mutations: render the secret value in the field (never-shown test fails); drop an icon id from the registry (drift test fails).
- Wire/schema changes: none (consumes PR3 RPCs).
- Docs to update: `public-docs/providers.md` screenshots/steps, `docs-site` provider page if it mirrors it.

## Budget and probe ledger (≤ USD 2 total)

Rule: every live call needs the Steward's OK first, haiku-class or free calls only, no real key in tests, a ledger line per call; a PR may not exceed the remaining budget. Free local Ollama/LM Studio calls count USD 0 but only if already installed.

| Date | PR | Call | Model | Approved by | Cost USD | Result |
|---|---|---|---|---|---|---|
| 2026-10-30 | PR0 | env-scrub spike: real `claude` CLI against a **fake** localhost server, sentinel keys | none (no model call) | n/a (no network, no cost) | 0.00 | token visible by default, hidden with `SCRUB=1` (ADR-0046 section 7) |
| | | | | | | |

Running total: **USD 0.00** of 2.00.

## Decisions

- 2026-10-30 (Supervisor default under the Human's "làm tất", Human may veto): the ten decisions of ADR-0046 section 10. The Human's explicit scope: refactor, five ACP providers (no Gemini), recipes with "Add from recipe", no PR7, no release.
- 2026-10-30 (Lead): PR2 owns the decision to set `CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=1` by default, gated on AC2.6 (permission modes and the MCP token); recommendation in the ADR "Questions for the Human".

## Progress

- [x] Security spike (env scrub), recipe kept under `/tmp/steward4/spike-sec/` and described in the ADR
- [x] PR0: ADR-0046 + this plan (docs only)
- [x] PR1 registry generalisation + Kimi fix (code merged-ready; see Validation)
- [ ] PR2 typed env + provider secrets
- [ ] PR3 claude-code recipes + vendor rate-limit profiles
- [ ] PR4 Codex endpoint instances
- [ ] PR5 five ACP providers
- [ ] PR6 web + mobile UX

## Validation

PR0 (docs only): the spike results are in ADR-0046 section 7 (`claude` 2.1.310, macOS, fake Anthropic server, sentinel keys, no network but localhost, cost 0). Markdown sanity is checked in the PR body. Later PRs append their evidence here per AC.

PR1 (registry generalisation + Kimi fix), 2026-10-30, branch `smind/task-lead-providers-epic-pr1-registry-dmi9f7tyb0uw`:

| AC | Evidence |
|---|---|
| AC1.1 | `TestInstance_ModelsReplaceClaudeCatalog` (exact list, `source: config`, syntax-valid ids accepted, base untouched) |
| AC1.2 | `TestClaudeLaunch_DisallowedToolsInArgv` (fakeclaude argv has `--disallowedTools WebSearch,Bash(rm:*)`; absent with none) |
| AC1.3 | `TestInstance_AdditionalModelsMergeByID` (no models = the Claude catalog; additional = catalog + ids merged by id) |
| AC1.4 | `TestCustomProvider_InstanceOverlayValidation_FieldNamed` (`providers.custom[1].disallowedTools[0]: required` and nine more), `TestCustomProvider_ACPInstanceCommandOverride`, `TestInstance_ACPCommandOverrideRuns` (the override command is what runs) |
| AC1.5 | `TestProviderList_AdditiveFields` (kimi keeps its wire fields), full `task test` green |
| AC1.6 | `TestInstance_DefaultModelNotInheritedWhenModelsSet`, `TestInstance_DefaultModelInheritedWhenNoModelsNoEndpoint` |
| AC1.7 | `TestKimiCommand_KimiCode` (`kimi acp`; docs and `internal/acp/kimi.go` name `MoonshotAI/kimi-code`, vendor script verified from the vendor README) |
| AC1.8 | `TestModelDisplay_UnknownIDShownRaw` (vitest) |

Mutations (each made the named test FAIL, a test failure not a build failure; file restored with `git checkout -- <file>`): drop `WithDisallowedTools` (argv test);
`staticModels` ignores the instance (replace + merge tests); `configuredDefaultModel` skips the instance models (default test); skip `validateToolName` (field-named test);
ignore the instance `Command` in `resolve` (override tests).

Decisions taken in PR1: `Runner` stays the internal type and `Harness` is its ADR name (`Runner.Harness()`, wire `harness` additive, wire `runner` values unchanged) instead of a
rename across the codebase; `models` and `additionalModels` are mutually exclusive on one instance; `disallowedTools` is claude-native only; a claude-native or codex-native
instance `command` is the executable only (no `args`); an instance of an ACP provider keeps free-form env until PR2; an instance may not extend an instance; with `models` replace
mode the instance does not use ACP/Codex discovery (models > discovery). `ModelAliases` is on `Spec` and inert until PR2. The `docs/decisions/0042` pointer to ADR-0046 is added; `docs/homelab.md` needs no rows (it has no
`providers.custom` rows to extend).
