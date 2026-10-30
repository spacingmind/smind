---
title: Providers
description: Log in to the coding-agent providers smind runs, and choose each provider's config directory.
summary: "This page explains the coding-agent providers sMind by Spacing Mind runs (Claude Code, Codex, GLM, Kimi, OpenCode, GitHub Copilot, Cline, Goose and Cursor), including login and each provider's config directory. It is for anyone setting up accounts."
order: 3
---

# Providers

smind runs the coding-agent providers listed below. Each one is a coding agent
that smind starts for a task. Five of them are new: OpenCode, GitHub Copilot,
Cline, Goose and Cursor. See [Native ACP providers](#native-acp-providers).

| Provider id | Name in the UI | What smind starts |
| --- | --- | --- |
| `claude-native` | Claude Code | the `claude` command |
| `codex-native` | Codex | `codex app-server` |
| `glm` | GLM | `npx -y glm-acp-agent` (needs Node.js) |
| `kimi` | Kimi | `kimi acp` |
| `opencode` | OpenCode | `opencode acp` |
| `copilot` | GitHub Copilot | `copilot --acp` |
| `cline` | Cline | `cline --acp` |
| `goose` | Goose | `goose acp` |
| `cursor` | Cursor | `cursor-agent acp` |

Install the agent's own command first. smind does not install it for you,
except that GLM downloads through `npx`.

Manage providers and accounts in the web UI under **Settings** → **Providers**,
or with `smind account`.

## How to check a provider is ready

```sh
smind account test claude-native
smind account test codex-native
smind account test glm
smind account test kimi
smind account test opencode
smind account test copilot
smind account test cline
smind account test goose
smind account test cursor
smind account ls
```

`account test` prints `ok` and a detail line when the provider can start. For
`claude-native`, `codex-native` and `glm` it checks that the command is on the
daemon's `PATH`, and the five native ACP providers are checked the same way.
For `kimi` it checks for a stored, unexpired credential. It never starts a run
and never calls the network.

In the UI, use the **Test** button on a provider's page or on an account row.

## Claude Code (`claude-native`)

Agent runs use the `claude` command's own login. Log in with Claude Code first,
the way its documentation says. Then run `smind account test claude-native`.

smind can also hold a Claude account for itself. It uses the account for usage
windows and for the daemon's `/v1` proxy. Register one with a browser login:

```sh
smind account login anthropic work
```

The command prints a URL and tries to open it in a browser. Sign in, and the
account is stored. The label (`work`) is yours to choose. The provider name for
the CLI is `anthropic`.

To add an account from a credential you already have, pipe it in on stdin:

```sh
smind account add anthropic work < credential.json
```

The credential is read from stdin so it never appears in your shell history.

In the UI, choose **Settings** → **Providers** → **Claude Code** and use
**Connect Claude Code**.

## Codex (`codex-native`)

Agent runs use the `codex` command's own login. Log in with Codex first, then
run `smind account test codex-native`.

For smind's own OpenAI account, the CLI provider name is `openai`:

```sh
smind account login openai work
```

This opens a browser login, like the Claude one. On a machine without a
browser, such as a server you reach over SSH, use a device code instead:

```sh
smind account login --device openai work
```

It prints a verification URL and a code. Open the URL on any device and enter
the code. `--device` is only for `openai`.

In the UI, choose **Settings** → **Providers** → **Codex** and use
**Connect Codex**.

## GLM

GLM manages its own login through its own CLI. smind keeps no credential for
it, and accounts added in the UI are not read by it. The provider page says
**Managed externally via CLI**.

GLM needs `npx`, so install Node.js. Check it with `smind account test glm`.
To limit how many turns one prompt may use, set `agents.glm.maxTurns` in
`config.yaml`. See [Configuration](configuration.md).

## Kimi

`kimi acp` expects an installed, already logged-in Kimi Code CLI
([`MoonshotAI/kimi-code`](https://github.com/MoonshotAI/kimi-code)). Install it
with the vendor's script, `curl -fsSL https://code.kimi.com/kimi-code/install.sh | bash`
(Windows: `irm https://code.kimi.com/kimi-code/install.ps1 | iex`), then run
`kimi` once and log in with `/login`. smind does not do that login. The older
Python `kimi-cli` is archived and no longer supported by smind.

You can also register a Kimi API key as an account. smind has no browser login
for Kimi, so the credential comes in on stdin:

```sh
smind account add kimi work < kimi-key.txt
```

`--base-url <url>` sets another API address.

## Native ACP providers

These five providers speak the Agent Client Protocol. smind starts each one
with its own command and does not install it.

| Provider id | Install | Login | Verified |
| --- | --- | --- | --- |
| `opencode` | `npm i -g opencode-ai` or `brew install sst/tap/opencode` | none for the built-in free models; `opencode providers login` or its config for your own provider key | yes, the real CLI (v1.18.33) against a local fake server |
| `copilot` | `npm i -g @github/copilot` | paid GitHub Copilot plan; run `copilot` once and use `/login`. ACP support is a public preview | not verified against a real login |
| `cline` | `npm i -g cline` | provider key from `cline auth` or the `CLINE_API_KEY` variable | not verified against a real login |
| `goose` | `brew install block-goose-cli` or the binary from block.github.io/goose | `goose configure` to pick a provider and key | not verified against a real login |
| `cursor` | `curl https://cursor.com/install -fsS \| bash` | paid Cursor account: `cursor-agent login` or `CURSOR_API_KEY` | not verified against a real login |

For OpenCode, smind sets `OPENCODE_PERMISSION` so it asks before bash and edit.
Without that, OpenCode would allow everything without asking. smind also sets
`OPENCODE_DISABLE_AUTOUPDATE=1`. Keep the same `HOME` and XDG data dir between
daemon restarts, or OpenCode sessions cannot resume.

Cursor needs a 2026 build. The 2025.09.18 build has no `acp` command, and the
check says it is too old.

`smind account test <id>` and the **Test** button check that the command is on
the daemon's `PATH`, run `<command> --version` once, and say what is missing:
the install command when it is not found, the login it needs when it is found,
whether the version is too old (Cursor), and "not verified against a real
login" for the four unverified ones. No model call is made.

Models and modes come from the agent itself, through its ACP config options,
the first time smind probes the provider. The probe sets `NO_BROWSER=true`, so
it never opens a browser.

Breaking: the ids `opencode`, `copilot`, `cline`, `goose` and `cursor` are now
reserved built-in ids. A `providers.custom` entry that used one of them must be
renamed.

## Provider launch environment

Claude Code and Codex keep their settings and login in a config directory.
Set that directory per provider in **Settings** → **Providers**. This is how
you run another login, for example a second Claude config for work.

| Provider | Field label | Variable smind sets | Default directory |
| --- | --- | --- | --- |
| Claude Code | Claude config directory | `CLAUDE_CONFIG_DIR` | `~/.claude` |
| Codex | Codex home | `CODEX_HOME` | `~/.codex` |

Type an absolute path, or one that starts with `~/`, and choose **Save**.
**Clear** removes your setting. Under the field, smind shows which directory it
uses and where that value came from: `set in smind`, `from the daemon's
environment` or `CLI default`. It warns when the directory does not exist yet.

The value applies to the next run. A run that already started keeps its old
setting. smind adds the variable to every launch of that provider, and it wins
over the daemon's own environment. For the base providers, these two variables
are the only launch environment you can set, and only as directories. An
instance can set more: see [Launch env and secrets](#launch-env-and-secrets).

The same setting lives in `config.yaml`:

```yaml
providers:
  claude-native:
    env:
      CLAUDE_CONFIG_DIR: ~/.claude-work
```

GLM and Kimi have no launch environment field.

## Recipes: compatible endpoints

A recipe is a built-in template for one provider instance that points Claude
Code (or Codex) at another vendor's API. smind ships the catalog inside the
daemon: there is no remote catalog, and it changes only with a release.
`provider.recipes` lists it (read-only). `provider.addFromRecipe {recipe, id?,
label?, baseUrl?, models?, secrets}` creates the instance from one row. It is
for Humans only: agents and plugins are refused. For Z.AI you only paste a key:
`secrets: {"api-key": "<key>"}`. The key goes into the secret store and is never
written to `config.yaml`, returned, logged or put on a command line. Adding the
same recipe with the same id again changes nothing in the config; an id used by
another entry is refused.

Every Claude Code recipe disallows `WebSearch` (third-party endpoints have no
hosted search tool) and runs with its own `CLAUDE_CONFIG_DIR` under
`<SMIND_HOME>/providers/<id>/claude`, so its files never mix with `~/.claude`.
Set `inheritClaudeConfig: true` on the entry to use `~/.claude` instead. A recipe
whose endpoint cannot turn thinking off is marked `thinking: hidden`: the run
ignores a requested thinking level and `provider.list` reports `thinking:
hidden` so a client hides the control. A model id smind does not know is shown
as the raw id.

Rate limits follow the vendor: an instance has a vendor profile (`zai`,
`anthropic`, `openai` or `generic`). A GLM-worded 429 on the Z.AI instance
(`vendor: zai`) puts that instance on cooldown until its UTC+8 reset and leaves
`claude-native` running. Any other vendor's 429 is classified by the generic
patterns, without a reset time.

| Recipe id | Harness | Needs | Source | Verified |
| --- | --- | --- | --- | --- |
| `zai-glm` | Claude Code | key | vendor docs | no |
| `qwen-dashscope` | Claude Code | key | Paseo docs | no |
| `deepseek` | Claude Code | key | vendor docs | no |
| `kimi-moonshot` | Claude Code | key | **inferred** | **no: the Claude Code base URL is a guess** |
| `minimax` | Claude Code | key | vendor docs | no |
| `openrouter` | Claude Code | key | vendor docs | no |
| `ollama` | Claude Code | a model id (local, no key) | vendor docs | no |
| `lm-studio` | Claude Code | a model id (local, no key) | vendor docs | no |
| `litellm` | Claude Code | URL, key, model ids | vendor docs | no |
| `anthropic-compatible` | Claude Code | URL, key, model ids | Paseo docs | no |
| `openai-responses-compatible` | Codex | URL, key, model ids | Paseo docs | no |
| `chat-completions-via-opencode` | OpenCode | not addable yet | **inferred** | **no** |

"Verified: no" means no row has been run against a live endpoint yet; the
catalog records `source` and `verifiedAt` per row, and `provider.recipes` returns
`unverified: true` for each. Treat the base URLs and model ids as the vendor's
documentation said on 2026-10-30. The Kimi and OpenCode rows are inferred and
should be tried with care. The `openai-responses-compatible` row speaks only the
OpenAI Responses API (Codex has no chat-completions mode).

For GLM, the default way to add a new login is now the `zai-glm` recipe (Claude
Code against the Z.AI endpoint). The built-in `glm` provider (the
`glm-acp-agent` wrapper) stays for one more release.

## Provider instances

A provider instance is a second login of Claude Code or Codex that has its own
config directory. It runs next to the base provider: both can have runs at the
same time, each with its own login, sessions, usage and cooldowns.

### Add an instance from Settings

1. Open **Settings** → **Providers** and choose **Add instance**.
2. Under **Found in your home folder**, smind lists the `~/.claude*` and
   `~/.codex*` directories it found, for example `~/.claude-work → Claude work`.
   Choose one and the instance is created at once and appears in the provider
   list. The chip stays in place with a check mark. smind only reads the names of
   the entries in your home folder, never the files inside them. Directories
   that a built-in provider or an existing instance already uses are not
   suggested.
3. For any other directory, open **Another directory**, pick **Claude Code** or
   **Codex**, fill in **Id**, **Label** and **Config directory**, and choose
   **Add instance**.

If smind refuses the entry, for example because the id is already used, the
message appears under the control you used and nothing is added.

Log in once inside the new directory with the provider's own CLI (for example
run `claude` with `CLAUDE_CONFIG_DIR` set to that directory). Open the instance
in **Providers** and choose **Test** to check it.

### The same thing in `config.yaml`

Instances are entries under `providers.custom`:

```yaml
providers:
  custom:
    - id: claude-work
      label: Claude work
      extends: claude-native
      env:
        CLAUDE_CONFIG_DIR: ~/.claude-work
    - id: codex-alt
      label: Codex alt
      extends: codex-native
      env:
        CODEX_HOME: ~/.codex-alt
```

- `id` is 1 to 64 characters of `a-z`, `0-9`, `.`, `_` and `-`, starting with a
  letter or digit. It may not be a built-in id (`claude-native`, `codex-native`,
  `glm`, `kimi`, `opencode`, `copilot`, `cline`, `goose`, `cursor`) and must be
  unique.
- `extends` is a built-in provider id (`claude-native`, `codex-native`, `glm`,
  `kimi`) or the id of an earlier ACP entry in the list. An instance cannot
  extend another instance.
- `env` takes typed values, and only the names the provider's harness allows;
  see [Launch env and secrets](#launch-env-and-secrets).
- `command` replaces the executable the instance runs (for an ACP provider,
  with `args`; for `claude-native` and `codex-native`, the executable only).
- `models` replaces the model list of the provider; `additionalModels` adds to
  it (merged by id). Set one or the other. Each entry has an `id` and an
  optional `label`, `description` and `default: true`. Without either, an
  instance of `claude-native` lists the Claude catalog. The first model (or
  the one marked `default`) is the instance's default, never the base's.
  smind accepts any model id of a valid shape and shows an id it does not know
  as it is.
- `disallowedTools` lists Claude Code tools the instance may not use (for
  example `WebSearch`); it is passed to the CLI as `--disallowedTools` and
  applies to instances of `claude-native` only.
- `endpoint` points an instance of `codex-native` at an OpenAI Responses-compatible
  API instead of the Codex login; see [Codex endpoint instances](#codex-endpoint-instances).
- An instance may not set `modes`.

A bad entry is rejected with the field named, for example
`providers.custom[2].command: required`. smind reads `providers.custom` at
startup. After you edit the file by hand, choose **Reload providers** in the
detail of any custom provider in **Settings** → **Providers** to apply it
without a restart. To remove an instance, delete its entry and reload. The
Settings screen has no remove button.

### Codex endpoint instances

An instance of `codex-native` can talk to any API that implements the OpenAI
**Responses** API (a gateway such as LiteLLM, OpenRouter's Responses route, or a
vendor's own `/v1/responses`):

```yaml
providers:
  custom:
    - id: gateway
      label: Team gateway
      extends: codex-native
      endpoint:
        kind: openai-responses
        baseUrl: https://llm.example.com
      models:
        - id: gpt-5.1
          default: true
```

- `baseUrl` gets `/v1` appended exactly once and trailing slashes are dropped:
  `https://h`, `https://h/` and `https://h/v1/` all become `https://h/v1`.
  It must be an `http(s)` URL with no userinfo, query or fragment. Put the key in
  the instance environment (`OPENAI_API_KEY`), never in the URL.
- `models` is required: smind does not discover the models of a custom endpoint.
  `additionalModels` is not allowed together with `endpoint`.
- smind tells Codex about the endpoint on every thread start and resume
  (`model_providers.<id>` with `wire_api = "responses"`). `env_key` and
  `requires_openai_auth = false` are sent only when a key is configured, so a
  keyless local endpoint does not demand one.
- The instance gets a private `CODEX_HOME` under `$SMIND_HOME/providers/<id>/codex`
  (mode 0700) unless you set `CODEX_HOME` yourself, so it never shares sessions or
  login state with your own `~/.codex`.
- **Responses only.** Codex does not speak the Chat Completions API. An endpoint
  of kind `openai-chat` (or `chat-completions`) is rejected at load time with
  `endpoint.kind: ... unsupported by the Codex harness`, not with a 404 in the
  middle of a run. For a chat-completions-only vendor use an Anthropic-compatible
  route into Claude Code, a Responses gateway such as LiteLLM in front of it, or
  OpenCode.
- Not yet verified against a live endpoint: tested with a fake app-server only.

### Where an instance shows up

An instance appears wherever a provider does: the model picker in the composer,
the provider choice when you create or edit an agent profile, the provider list
in **Settings** → **Providers** (with its config directory and where it came
from), the readiness **Test**, and the **Provider** grouping in **Usage**. It
wears the base provider's mark. An instance has no smind account row, so
**Connect account** does not apply to it.

### Custom ACP providers

A `providers.custom` entry without `extends` adds an agent that speaks the Agent
Client Protocol:

```yaml
providers:
  custom:
    - id: my-agent
      label: My agent
      command: /usr/local/bin/my-agent
      args: ["--acp"]
      env:
        MY_AGENT_HOME: ~/.my-agent
      models:
        - id: my-model
          label: My model
          default: true
      modes:
        - id: default
          label: Default
```

`command` is required. `env` accepts any valid variable name, and smind never
prints its values in an error; secret-looking names must be references, see
[Launch env and secrets](#launch-env-and-secrets). `models` and `modes` are
optional lists of `id`, `label`, `description` and, for a mode, `autoApproves`.
A custom ACP provider shows the generic provider icon and the label you gave it.

For an agent that cannot take MCP servers, set `acp.supportsMcpServers: false`.
smind then sends it none. The same setting works on an instance of an ACP
provider:

```yaml
providers:
  custom:
    - id: my-agent
      label: My agent
      command: /usr/local/bin/my-agent
      args: ["--acp"]
      acp:
        supportsMcpServers: false
```

## Launch env and secrets

An instance's `env` does more than point at a config directory. Every value is
typed:

- a plain string is a **literal**: a non-secret value, kept as written in
  `config.yaml`;
- `{secret: <name>}` is a **reference** to a key smind stores for you. The key
  itself never goes in `config.yaml`.
- `{passthrough: true}` copies the variable from the daemon's own environment
  at launch, for keys you export outside smind.

A Claude Code instance pointed at Z.AI:

```yaml
providers:
  custom:
    - id: zai
      extends: claude-native
      env:
        ANTHROPIC_BASE_URL: https://api.z.ai/api/anthropic
        ANTHROPIC_AUTH_TOKEN: {secret: zai-key}
        API_TIMEOUT_MS: 3000000
```

Each harness allows only certain names:

| Harness | Allowed names |
| --- | --- |
| `claude-code` | `ANTHROPIC_BASE_URL` (url); `ANTHROPIC_AUTH_TOKEN` and `ANTHROPIC_API_KEY` (secret); `ANTHROPIC_MODEL`, `ANTHROPIC_DEFAULT_OPUS_MODEL`, `ANTHROPIC_DEFAULT_SONNET_MODEL`, `ANTHROPIC_DEFAULT_HAIKU_MODEL`, `ANTHROPIC_DEFAULT_FABLE_MODEL`, `ANTHROPIC_SMALL_FAST_MODEL`, `CLAUDE_CODE_SUBAGENT_MODEL`, `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC`, `CLAUDE_CODE_EFFORT_LEVEL`, `CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY`, `ENABLE_TOOL_SEARCH`, `CLAUDE_CODE_SUBPROCESS_ENV_SCRUB` (plain); `API_TIMEOUT_MS` and `CLAUDE_CODE_AUTO_COMPACT_WINDOW` (int); `CLAUDE_CONFIG_DIR` (path) |
| `codex-app-server` | `OPENAI_BASE_URL` (url), `OPENAI_API_KEY` (secret), `CODEX_HOME` (path) |
| `acp` | any valid variable name; names ending `_KEY`, `_TOKEN`, `_SECRET` or `_PASSWORD` are secret-kind |

A name outside the list is rejected with the field named, for example
`providers.custom[1].env.PATH`. A secret-kind name with its value written
inline is rejected too: a key is never written in `config.yaml`. The one
exception is `ANTHROPIC_API_KEY`, which accepts an empty value where a vendor
needs the variable set but empty.

This is a breaking change from the earlier rule that an instance's `env` could
only carry `CLAUDE_CONFIG_DIR` or `CODEX_HOME` as a path. An entry that put a
key inline in `env` must move it to the secret store.

### Setting a key

Keys are stored by smind and referenced by name from `env`. Two write-only
RPCs manage them: `provider.setSecret {instanceId, name, value}` stores a
value, and `provider.clearSecret {instanceId, name}` removes one. Both are
for Humans: calls from agents and plugins are refused. No method ever returns
a stored value. `provider.list` shows, per secret, only `{name, env, set,
fingerprint}`, where the fingerprint is the last 4 characters of a SHA-256 —
enough to see that a key changed, not enough to use it.

Values live in `smind.db`, with the same protection as account credentials. A
secret is resolved only when a run launches, into the child process
environment. Deleting an instance from `config.yaml` and reloading providers
(or restarting smind) removes its stored secrets.

### What is and is not protected

**Protected.** A stored key never appears in `config.yaml`, in the launch
argv, in logs, in events, in `provider.list`, in error text or in a relay
response. The secret store is on secretdeny's deny list for the Claude CLI's
tools.

**Not protected (residual).** The agent's own shell can read its environment.
By default Claude Code does **not** scrub the token from its Bash tool
(measured 2026-10-30, `claude` 2.1.310, macOS: `printenv` shows
`ANTHROPIC_AUTH_TOKEN`). For claude-code instances that hold a secret, smind
adds best-effort deny rules: Bash `env`, `printenv`, `set`, `export -p`,
`declare -x` and `echo $NAME`, plus Read of `/proc/*/environ` and
`<SMIND_HOME>/providers/**`. These are pattern matches and are trivially
bypassed (`python -c`, a script file, a command assembled in pieces). They
raise the cost of a lazy prompt injection; they do not stop a determined one.
Codex and ACP agents have no equivalent: their shell tools inherit the env.

The recommendation that follows from this: use a narrowly scoped key with a
spend limit, and for Codex/ACP vendors put a local gateway (for example
LiteLLM) in front, so the agent holds only a throwaway token.

### CLAUDE_CODE_SUBPROCESS_ENV_SCRUB

smind does not set this variable: it is off by default, on purpose. Measured (2026-10-30, `claude` 2.1.310): with
it on, Claude Code forces the permission mode back to `default` — a run in
`bypassPermissions` then refuses a Bash command that uses a shell variable —
which conflicts with smind's permission modes. smind's own MCP stdio env (run
id, validator token) was unaffected. The variable does hide
`ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_API_KEY` and most `*_KEY`/`*_TOKEN` names
from the Bash tool. If you accept `default` permissions, you may opt in per
instance with `CLAUDE_CODE_SUBPROCESS_ENV_SCRUB: "1"` in the instance `env`.
