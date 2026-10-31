# 0046: Provider = spec + harness + overlay; compatible endpoints as data; provider secrets

## Status

**Proposed — 2026-10-30.** The Human said "do ALL of it" for the providers epic ("Provider layer v2"). Every decision in [section 10](#10-the-ten-decisions) is the Supervisor default
taken on 2026-10-30 under the Human's "làm tất"; the Human may veto any of them, and a veto changes the PR that depends on it (listed per decision). Spec, PR split and acceptance
criteria: `docs/plans/active/providers-epic.md`. This ADR is docs only: no code ships with it.

Inputs: ADR-0042 (plugin system; Wave 0 introduced the provider registry and provider *instances*, `providers.custom`), ADR-0019 (provider-native permission modes),
ADR-0028 (account usage windows, rate-limited usage endpoints), ADR-0033 (provider login, account identity), ADR-0037 (secretdeny, the CLI env scrub is named "not covered" there),
ADR-0041/0045 (seats, validator), `internal/providers/providers.go` (the registry), `internal/config/providers_custom.go` + `provider_env.go` (the config surface), and a
local research note (`docs/research/local/providers-epic-research.md`, gitignored, cited here as the "local research note"; its section numbers are used below).

## Context

smind has four providers: `claude-native`, `codex-native`, `glm` (ACP, third-party `glm-acp-agent`) and `kimi` (ACP, `kimi acp`). ADR-0042 Wave 0 made them registry
specs and added *instances*: a `providers.custom` entry with `extends: claude-native|codex-native` that runs the same CLI with its own launch env. That env is today limited to
two directory keys (`CLAUDE_CONFIG_DIR`, `CODEX_HOME`), "no tokens, PATH or API keys" (`internal/config/provider_env.go`).

Paseo (the reference app, `refs/paseo`) shows what the generalisation buys: one `extends` + `env` + `models` + `disallowedTools` + `command` overlay turns Claude Code into
a client for Z.AI, Qwen/DashScope, DeepSeek, OpenRouter, Ollama and so on, and a generic ACP provider turns any ACP agent into a provider by data (local research note, section 1).
The gaps in smind (local research note, section 2), in order of weight:

1. An instance cannot carry endpoint env (`ANTHROPIC_BASE_URL`, token, model aliases, timeouts). "Claude Code on Z.AI" is impossible without Go edits; GLM works only through a
   third-party ACP wrapper with 69 GitHub stars.
2. An instance cannot carry `models`, `disallowedTools` or a `command`; the Claude catalog is one static list shared by every claude-runner entry; the default model of an instance
   falls back to its **base's**, wrong for a foreign-model endpoint.
3. A Codex instance cannot point at an endpoint: `thread/start` and `thread/resume` have no `config`/`modelProvider` although Codex accepts both.
4. There is no secret slot for provider keys. Any inline env value lands in plaintext `config.yaml` and in the child process env.
5. The ACP built-ins (GLM, Kimi) are hard-coded Go plus a GLM-only `agents.glm.maxTurns`.
6. The Kimi provider targets an archived tool: `MoonshotAI/kimi-cli` says "Legacy Python Kimi CLI, no longer maintained, use Kimi Code CLI: github.com/MoonshotAI/kimi-code"
   (GitHub API, 2026-10-30). smind's docs and `internal/acp/kimi.go` still say `pip install kimi-cli`.
7. The rate-limit classifier switches on provider ids (GLM wording, claude/codex regexes); a Z.AI endpoint driven by Claude Code needs a vendor profile, not an id.
8. smind lacks the ACP agents users run daily (OpenCode, Copilot CLI, Cline, Goose, Cursor CLI), although the ACP runner makes them near-free.

The most important constraint is gap 4: it is also where a design mistake is hardest to take back. The threat model is in [section 7](#7-threat-model-and-the-security-spike).

## Decision

### 1. A provider is a registry spec, a harness kind and an overlay

`internal/providers` stays the single source (it already is). A provider is:

```
Spec {
  ID, Label, Description
  Harness   claude-code | codex-app-server | acp         // today's Runner claude-native|codex-native|acp; wire values of "runner" are kept
  Extends   <id of a harness-bearing built-in or custom spec>      // instances
  Launch    { Command []string; Env map[string]EnvValue }          // EnvValue = literal | secretRef | passthrough
  Models    []Model + ModelsMode replace|add                       // Paseo's models / additionalModels
  ModelAliases map[string]string                                   // claude-code: opus/sonnet/haiku/subagent slot -> endpoint model id
  DisallowedTools []string
  Endpoint  *Endpoint { Kind anthropic|openai-responses; BaseURL; KeyRef; Timeout }   // typed sugar that expands to Env (+ the Codex config)
  Thinking  supported | hidden
  Usage     none | account:<provider> | source:<id>                // only `none` and `account:` exist in this epic
  ... existing capability flags
}
```

An *instance* is a spec with `Extends` set: it copies the base, then applies only the overlay `{Label, Description, Launch.Env, Launch.Command, Models(+mode), ModelAliases,
DisallowedTools, Endpoint, Thinking}`. `Extends` may name `claude-native`, `codex-native` **or any ACP spec** (built-in or custom), not only the first two as today.
`providers.custom[]` gains `models`, `additionalModels`, `disallowedTools`, `env`, `endpoint`, `command` on instances, mirroring Paseo's table minus `options`, `paseoTools`, `order`.

Precedence for the model list: `models` (replace) > discovery (ACP config options, Codex model list) merged with `additionalModels`. A claude-code instance has no runtime discovery
over an Anthropic-compatible endpoint: its list is the recipe's list plus the user's ids; any syntax-valid id is accepted (already true of `ValidateModelSyntax`). The default model
of an instance is resolved from the instance first, and from the base only when the instance sets no models **and** has no endpoint. The run shows the raw model id for ids smind does
not know (Paseo's `resolveObservedClaudeModelId` behaviour), never a blank.

Claude launch passes `WithDisallowedTools(spec.DisallowedTools...)` (the SDK has it; smind never used it). Merge rule as Paseo's: base + instance, concatenated.

### 2. Harness kinds

| Harness | Launch | Used by |
|---|---|---|
| `claude-code` | `claude-agent-sdk-go` → the `claude` binary; env = task env + launch spec + provider env last | `claude-native`, every Anthropic-compatible recipe, GLM default path |
| `codex-app-server` | `codex app-server` JSON-RPC; endpoint through thread `config`/`modelProvider` | `codex-native`, the generic Responses-URL recipe |
| `acp` | ACP over stdio (`internal/acp`) | `kimi`, `glm` (legacy), the five new providers, custom ACP entries |

No fourth harness. Pi (`--mode rpc`) and OpenCode's `serve` HTTP path would each need a new runner and are not in this epic.

### 3. Typed env: names per harness, values by kind

`ProviderEnvKeys` keeps its role but changes meaning: a per-**harness** allowlist of **names**, each with a **value kind**.

| Harness | Allowed names | Kinds |
|---|---|---|
| `claude-code` | `ANTHROPIC_BASE_URL` (url), `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_API_KEY` (secret or empty), `ANTHROPIC_MODEL`, `ANTHROPIC_DEFAULT_{OPUS,SONNET,HAIKU,FABLE}_MODEL`, `ANTHROPIC_SMALL_FAST_MODEL`, `CLAUDE_CODE_SUBAGENT_MODEL` (plain), `API_TIMEOUT_MS`, `CLAUDE_CODE_AUTO_COMPACT_WINDOW` (int), `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC`, `CLAUDE_CODE_EFFORT_LEVEL`, `CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY`, `ENABLE_TOOL_SEARCH` (plain), `CLAUDE_CONFIG_DIR` (path) | url, secret, plain, int, path |
| `codex-app-server` | `OPENAI_BASE_URL` (url), `OPENAI_API_KEY` (secret), `CODEX_HOME` (path) | url, secret, path |
| `acp` | any name matching `envNameRe`, as today (values never echoed) | plain, secret, path |

A name outside the list is rejected for claude-code and codex instances with the field named (`providers.custom[1].env.PATH`). Deliberately absent: `PATH`, `LD_PRELOAD`,
`ANTHROPIC_CUSTOM_HEADERS` (a header can carry a credential and is not needed by any recipe), `HTTPS_PROXY`. Adding a name later is a reviewed code change, not config.

`EnvValue` is one of: `literal` (non-secret, may be in `config.yaml`), `secret: <name>` (a reference to a stored secret, [section 4](#4-provider-secrets)), `passthrough` (copy the
variable from the daemon's own env at launch; for users who export keys outside smind). A `secret`-kind name with a `literal` value is rejected: **a key never goes inline in
`config.yaml`, ever** (breaking change, [section 9](#9-breaking-changes-no-installed-base)).

### 4. Provider secrets

- Values live in `smind.db` in a `provider_secrets` table, same store and protection as account credentials (`internal/accounts`), keyed by `(instanceId, name)`.
- `provider.setSecret {instanceId, name, value}` and `provider.clearSecret {instanceId, name}` are **human-only** (same guard as `provider.setEnv`/`provider.addInstance`: refused for
  run-scoped callers and the automation origin) and **write-only**: no method returns a value. `provider.list` reports `{set: bool, fingerprint: <last 4 of a SHA-256>}` per secret name.
  Relay: the call is allowed from a Human relay session as a request; its response carries no value, so no secret ever crosses the E2EE relay in a response.
- Resolution happens once, at launch, into the child env map. A secret is **never** in argv (the Claude MCP config already moves its secrets to a 0600 file for this reason),
  never in a log line, an event, `provider.list`, an error string, a task transcript or a diagnostic copy. Validation errors name the field and never echo a value.
- UI: password field, never re-displayed; "Check key" is a separate explicit click that may call the vendor's models endpoint; `provider.test` stays free of network calls.
- The OS keychain was considered and rejected as the default (Decision 1): the daemon is headless or remote and `smind.db` already holds credentials.

### 5. Recipes: compatible endpoints as data

A **recipe** is an embedded file (`internal/providers/recipes.yaml` via `go:embed`), a template for one `providers.custom` entry: harness, endpoint kind, base URL, env names and
default values, default models + aliases, disallowed tools, thinking mode, caveat notes, docs link, and two provenance fields **`source`** (`paseo-docs | vendor-docs | inferred`)
and **`verifiedAt`** (a date or empty). The catalog is embedded only (Decision 7): offline, reviewable, updated with releases. `provider.recipes` lists it; "Add from recipe" asks
for a key (and a region where the recipe has one) and writes the instance plus the secret. Users can still write entries by hand. The rows of the catalog are in the plan.

Defaults of every claude-code recipe: `Endpoint.Kind: anthropic`, `disallowedTools: [WebSearch]` (third-party endpoints lack Anthropic's server-side tools), a **private
`CLAUDE_CONFIG_DIR`** (`<SMIND_HOME>/providers/<id>/claude`) with an "inherit my `~/.claude`" toggle (Decision 6), `ANTHROPIC_API_KEY=""` where the vendor needs the empty value, and
`CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=1` where the PR2 validation allows it ([section 7](#7-threat-model-and-the-security-spike)).

### 6. Codex endpoints

`thread/start` and `thread/resume` gain `config` and `modelProvider`. `Endpoint{openai-responses}` expands to `model_providers.<id> = {name, base_url (+/v1 once, trailing slashes
stripped), wire_api: "responses", env_key: "OPENAI_API_KEY" only when a key is set, requires_openai_auth: false}`, as Paseo does. **Constraint:** Codex's config schema accepts only
`wire_api: "responses"`, so chat-completions-only vendors cannot go through Codex; they use an Anthropic-compatible route into Claude Code, a Responses gateway (LiteLLM), or OpenCode.
A chat-only endpoint gets a clear "unsupported by the Codex harness" message, not a silent 404 at run time. Models are not discovered for Codex custom endpoints: `models` is required.

### 7. Threat model and the security spike

**Assets.** A vendor API key (Z.AI, DeepSeek, OpenRouter, and so on); Anthropic/OpenAI account credentials already in `smind.db`; the relay and daemon tokens.
**Actors.** (a) The agent itself, which may be prompt-injected by repo content or a web page and has shell tools; (b) a repo's own scripts the agent runs; (c) another local user;
(d) a log/diagnostic reader; (e) a remote peer over the relay.
**Paths a key can leak on**, and the control for each:

| Path | Control |
|---|---|
| `config.yaml` read by anyone with file access, or by a codex/ACP agent (secretdeny covers only Claude CLI tools) | no inline keys, ever: only references; `secretdeny` already denies `config.yaml` to the Claude CLI |
| argv (`ps`, `/proc/*/cmdline`, crash reports) | env only; test that the sentinel is absent from argv |
| logs, events, `provider.list`, error text, diagnostics copy | write-only RPC, redaction test that greps a whole fake run for a sentinel |
| the agent's shell tool reads its own env (`printenv`, `echo $NAME`, `/proc/self/environ`) | **see the spike below**: partial, best effort |
| the agent reads the secret store (`smind.db`) | already in secretdeny's deny list for the Claude CLI's tools; codex/ACP agents are not covered by secretdeny (residual) |
| an agent asks the daemon for a secret over RPC | no read method exists; set/clear are human-only (run-scoped callers refused, tested) |
| the secret crossing the E2EE relay in a response | responses never carry a value (tested) |
| the endpoint itself sees the key | by design: the user chose that vendor; a recipe's base URL is fixed data, an instance's `ANTHROPIC_BASE_URL` is shown in the UI before the key is saved |

**The agent can read its own environment.** Provider env is placed in the child process env, and the Claude CLI's Bash tool runs commands as a child of the CLI. The question the
research note left open (section 3.5): does Claude Code scrub `ANTHROPIC_AUTH_TOKEN` from the Bash tool's environment? Answered by a spike, 2026-10-30, no real key, no network but localhost:

**Spike method.** `/tmp/steward4/spike-sec/` (not in the repo): `fake.js` is a ~40-line Node server on a free `127.0.0.1` port that speaks the Anthropic Messages streaming protocol. It answers
the first request that lists the Bash tool with a `tool_use` of Bash running `printenv | grep -i -E 'ANTHROPIC|sentinel'`, logs the `tool_result` the CLI sends back, and then ends the
turn. `run.sh <label> [VAR=val ...]` starts it and runs, in an empty temp dir:

```
env -i PATH=$PATH HOME=$(mktemp -d)/home ANTHROPIC_BASE_URL=http://127.0.0.1:$PORT \
  ANTHROPIC_AUTH_TOKEN=sentinel-NOT-A-REAL-KEY-12345 ANTHROPIC_API_KEY=sentinel-APIKEY-NOT-REAL-67890 \
  SENTINEL_PLAIN=sentinel-plain-env CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1 [CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=...] \
  claude -p "run the command" --model fake --dangerously-skip-permissions --allowedTools Bash --output-format json
```

Environment: `claude` 2.1.310 (`~/.local/bin/claude`), macOS (Darwin 27), Node 22.14. The scrub variable is documented in the Claude Code changelog ("`CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=1`
strips Anthropic and cloud provider credentials from subprocess environments (Bash tool, hooks, MCP stdio servers)", `refs/claude-code/CHANGELOG.md`) and present in the binary.

**Results** (the `tool_result` content the fake server received):

| Run | `ANTHROPIC_AUTH_TOKEN` | `ANTHROPIC_API_KEY` | `ANTHROPIC_BASE_URL` | `SENTINEL_PLAIN` |
|---|---|---|---|---|
| no scrub env (default) | **visible** | **visible** | visible | visible |
| `CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=0` | **visible** | **visible** | visible | visible |
| `CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=1` | hidden | hidden | visible | visible |

With `=1` the model request itself still authenticated (the fake server saw an `authorization` header), so the CLI keeps the token for its own API calls. A second round with `=1` and
extra variables: `OPENAI_API_KEY`, `MOONSHOT_API_KEY`, `ZAI_API_KEY`, `MYVENDOR_TOKEN`, `VENDOR_SECRET`, `MY_PASSWORD`, `ZAI_KEY`, `AWS_SECRET_ACCESS_KEY` were all **hidden**;
`GITHUB_TOKEN` and `CUSTOMAUTH` were **visible** (so the scrub is a name heuristic with an exception list, not "everything that looks secret"). The binary also states that with the
scrub set, a permission mode other than `default` (including `--dangerously-skip-permissions`) is **forced back to `default`** ("allowed_non_write_users hardening: declare
allowedTools explicitly"); the changelog adds a PID-namespace sandbox on Linux when it is set.

**Facts, plainly.** (1) **By default Claude Code does NOT scrub the token from its Bash tool**: an agent running `printenv`, `env`, `set`, `echo $ANTHROPIC_AUTH_TOKEN` or
`cat /proc/self/environ` reads the key (the same exposure exists in Paseo). A file-deny rule such as secretdeny's `Read` rules does not touch this path. (2)
`CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=1` works for the Anthropic names and many `*_KEY`/`*_TOKEN`/`*_SECRET`/`*_PASSWORD` names; it is a name heuristic and leaves `GITHUB_TOKEN` and
`CUSTOMAUTH`-style names alone. (3) The scrub is a Claude-Code-only mechanism: Codex and ACP agents have no equivalent; their shell tools inherit the env (residual risk, stated in
the docs). (4) Tested on one version and one OS (macOS, 2.1.310); Linux behaviour (the PID-namespace sandbox) and Windows were not measured. (5) Not measured: whether the scrub also
removes smind's own orchestration MCP token from the MCP stdio server entry (the changelog says MCP stdio servers are scrubbed; the entry's explicit `env` may be exempt), and
whether smind's permission modes still behave when the scrub forces `default`. Both are PR2 acceptance criteria and block enabling the scrub by default.

**Mitigation, best effort, and a defence, not a boundary** (the same wording the docs already use for secretdeny):

1. Set `CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=1` in the launch env of every claude-code instance that holds a secret, *if* the PR2 validation shows permission modes and the MCP token
   still work; if not, leave it off and say so in the docs. It is added to the claude-code env allowlist for this purpose.
2. Extend secretdeny with `Bash` deny rules for `env`, `printenv`, `set`, `export -p`, `declare -x`, `echo $NAME` / `${NAME}` for each secret env name of the instance, and
   `Read` denies for `/proc/*/environ` and `<SMIND_HOME>/providers/*/**`. These are pattern matches on a command string and are trivially bypassable (`python -c 'import os'`,
   a script file, `$(printenv)` assembled in pieces): they raise the cost of a lazy prompt injection, they do not stop a determined one.
3. Prefer **passthrough-free, narrowly scoped keys**: recipes tell the user to create a key with the lowest scope and spend limit the vendor offers (a leaked key costs money, not an
   account). The UI says so next to the key field.
4. The residual stays documented: Codex/ACP instances with a secret expose it to their own agent's shell tools. For those the docs recommend a local gateway (LiteLLM) so the agent
   only ever holds a throwaway token.
5. Re-run the spike when `claude` changes major behaviour: the recipe above is deterministic and costs nothing.

**Measurement for PR2 (2026-10-30), decides AC2.6:** runs against `claude` 2.1.310 with the fake server and sentinel keys
(`/tmp/steward4/pr2-spike`, not in the repo):

| Configuration | Result |
|---|---|
| `bypassPermissions`, no scrub | Bash `echo ran-ok > ran.txt; echo rc=$?` ran (file created) |
| `bypassPermissions` + `SCRUB=1` | refused ("A variable in this command can't be checked before it runs"), no file |
| `acceptEdits` + `SCRUB=1` | refused the same way |
| MCP stdio entry with explicit env `{SMIND_RUN_ID, SMIND_VERDICT_TOKEN}` | both delivered with and without the scrub (only `ANTHROPIC_AUTH_TOKEN` was removed from the MCP server's inherited env) |

Conclusion: (a) the permission-mode conflict is real → the scrub is **not** enabled by default; mitigation 1 above becomes "left off; documented;
per-instance opt-in"; (b) no conflict — smind's MCP stdio env is unaffected.

### 8. Rate-limit vendor profile

`ratelimit.Classify` keeps taking the provider id but resolves a **vendor profile** from the spec (`zai`, `anthropic`, `openai`, `generic`) instead of switching on ids. The generic
patterns always apply, so an unknown vendor still classifies. The cooldown stays per instance id: a GLM-worded 429 on the Z.AI instance blocks that instance until its (UTC+8) reset and
leaves `claude-native` running. A foreign endpoint driven by Claude Code produces Claude-shaped errors from the CLI; PR3 tests this with the fake endpoint.

### 9. Breaking changes (no installed base)

No compat shims (memory: no installed base, 2026-10). Breaks to note in release notes: `providers.custom` instance `env` values of secret kind must move to the secret store; the env
allowlist semantics change (per harness); `agents.glm.maxTurns` is deleted when GLM becomes data (GLM default path = Claude Code + Z.AI; `glm-acp-agent` stays as a legacy
entry for one release, then goes); `internal/acp/kimi.go` and the docs point to `kimi-code`. **No release is cut by this epic**: the Human triggers it.

### 10. The ten decisions

Each is "**Supervisor default 2026-10-30 under the Human's 'làm tất', Human may veto**". The right column says which PR a veto changes.

| # | Decision | Default taken | A veto changes |
|---|---|---|---|
| 1 | Where provider secrets live | `smind.db` via a write-only human-only RPC + `passthrough` from the daemon env; no inline keys in `config.yaml`, never logged or returned; secretdeny extended to the new env names. OS keychain rejected as default | PR2 (and PR3 UI) |
| 2 | Breaking changes to `ProviderEnvKeys`, `providers.custom` shape, `agents.glm.maxTurns`, the hard-coded GLM spec | Accept (no installed base); `agents.glm.maxTurns` deleted with GLM-as-data; noted in release notes | PR1, PR3 |
| 3 | Which providers to add natively | OpenCode, Copilot CLI, Cline, Goose, Cursor CLI over ACP (the Human's list supersedes the research note's OpenCode + Gemini). **Gemini CLI is out of scope.** OpenCode gets a spike first; the paid/closed ones (Copilot, Cursor) are best-effort and their health check names the login they need | PR5 |
| 4 | Live-probe budget | ≤ USD 2 **total** for the whole epic, haiku-class or free calls only, plus a free local Ollama/LM Studio **only if already installed (never installed by an agent)**; every live call needs the Steward's OK; ledger in the plan | all PRs |
| 5 | Thinking/effort control for foreign endpoints | Per recipe `thinking: supported \| hidden`; hidden for MiniMax and unknown endpoints | PR3, PR6 |
| 6 | `CLAUDE_CONFIG_DIR` for a compatible-endpoint instance | Private per instance by default (`<SMIND_HOME>/providers/<id>/claude`), "inherit my `~/.claude`" toggle per instance | PR3, PR6 |
| 7 | Recipe catalog hosting | Embedded in the binary only; updates ship with releases; no remote fetch | PR3 |
| 8 | Usage windows for compatible endpoints | Out of scope: no PR7. Run-level usage and per-instance cooldown suffice | none |
| 9 | GLM: Claude Code vs `glm-acp-agent` | Claude Code + Z.AI becomes the default GLM provider; `glm-acp-agent` kept one release as legacy | PR3 |
| 10 | Release | **None.** One minor later (version number is the Human's call) after PR3 and PR5 land; the Human triggers it | n/a |

## Alternatives considered

- **Keychain for secrets.** Rejected as the default: headless/remote daemons, and `smind.db` already holds the stronger credentials.
- **Env passthrough only.** Cheap, but a UI "paste your key" flow is impossible and the daemon's env must carry every vendor key; kept as one of the three value kinds.
- **A Go file per vendor.** What GLM/Kimi are today; each new vendor costs a PR and a release. Recipes cost a data row.
- **Remote recipe catalog.** Fresher, but an unreviewed network input that writes env and base URLs; rejected for v1.
- **Plugin-contributed providers (ADR-0042 Wave D).** Compatible; this epic does not touch ids of the form `<plugin>/<provider>`.
- **A Gemini CLI entry.** Not requested by the Human; the data model makes it a one-row follow-up.

## What we are NOT doing

Usage meters per vendor (no PR7); Pi or OpenCode-`serve` runners; Paseo's `options` bag and `paseoTools`; a remote catalog; real-key tests (every test uses sentinel strings and a fake
Anthropic-/OpenAI-compatible server on a local port); installing Ollama, LM Studio or any global tool; touching `~/.smind`, the launchd service or `:4648`; a release.

## Consequences

- One data path for "a new compatible endpoint" and for "a new ACP agent": a row, an icon, a health check; no new runner.
- Secrets get a store, a write-only RPC and a redaction test: the first time smind keeps a key that is not an account credential. The spike shows the agent's shell can still read a
  key placed in its env unless the CLI's scrub is on, and the scrub is Claude-Code-only; the residual is documented instead of hidden.
- Paid or closed ACP agents (Copilot, Cursor) are best-effort: they may not run without their own subscription and login, and no live run of them is promised in this epic.
- `provider.list` gains additive fields (`endpoint.kind`, `notes[]`, `usage.kind`, `recipe`, per-secret `{set, fingerprint}`); clients that ignore them keep working.

## Questions for the Human

All ten decisions above are taken as defaults; the three where a veto would cost the most, with the recommendation:

1. **Secrets in `smind.db`, not the keychain** (Decision 1). Recommendation: keep. The spike shows the stronger risk is the agent's own shell, not where the key sits at rest.
2. **Scrub on by default for claude-code instances that hold a key** (section 7). It forces permission mode `default` per the CLI's own text, which may fight smind's auto modes;
   PR2 measures it. Recommendation: enable only if PR2's validation passes; otherwise ship without it and document the exposure.
3. **Private `CLAUDE_CONFIG_DIR` by default** (Decision 6). Costs the user's `~/.claude` skills/CLAUDE.md/MCP in compatible instances until they flip the toggle.

## Amendment 2026-10-31: provider.updateInstance

Human-approved 2026-10-31 (the follow-up PR6 left open). An existing `providers.custom` instance, whether made from a recipe, from Settings or by hand, can now have its
**models**, **additionalModels**, **disallowedTools** and **inheritClaudeConfig** changed without editing `config.yaml` by hand.

- **RPC** `provider.updateInstance {id, models?, additionalModels?, disallowedTools?, inheritClaudeConfig?}`. A present field is applied, an empty list or `false` removes the
  key, an absent field is untouched; nothing present is an error. The resulting entry is validated with the config-load rules (`ValidateCustomProviders`), so the errors name the
  field (`providers.custom[1].additionalModels: ...`) and `models` + `additionalModels` stay exclusive (a client switching mode sends the new list and `[]` for the other).
  Applied live like `provider.reload` (atomic: an invalid file changes nothing), then `provider.updated` is published. Keys stay on `provider.setSecret`/`clearSecret`; `env`,
  `command`, `endpoint` and `label` are not editable here.
- **Authority**: human-only, like `provider.addInstance`: refused for run-scoped (agent) callers, on the plugin never-list, unreachable from automation. It writes config and
  changes what runs launch with (a tool list that gates what an agent may do), so an agent must never edit it.
- **Minimal config edit**: `mutate` (used by `addInstance`) decodes and re-encodes the whole file, which keeps comments but drops blank lines and re-indents. For this RPC the
  writer instead patches the bytes of the changed `key: value` blocks of the one entry; every other byte is identical. The patched file is parsed again and must decode to the
  expected entry and still load before the atomic write. Entries it cannot locate exactly (flow style `- {id: x}`, a key on the `- ` line, a block scalar in a changed field) are
  refused with a message to edit by hand: we do not guess.
- **`provider.list`** serves `instance: {models, additionalModels, disallowedTools, inheritClaudeConfig}` for an instance (its own overlay as configured, not the merged catalog),
  additive.
- **Break**: none (additive RPC and wire field). No installed base to migrate.
