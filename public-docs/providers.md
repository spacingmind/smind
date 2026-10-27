---
title: Providers
description: Log in to Claude Code, Codex, GLM and Kimi, and choose each provider's config directory.
summary: "This page explains how to connect the four coding-agent providers sMind by Spacing Mind runs (Claude Code, Codex, GLM and Kimi), including login and each provider's config directory. It is for anyone setting up accounts."
order: 3
---

# Providers

smind runs four providers. Each one is a coding agent that smind starts for a
task.

| Provider id | Name in the UI | What smind starts |
| --- | --- | --- |
| `claude-native` | Claude Code | the `claude` command |
| `codex-native` | Codex | `codex app-server` |
| `glm` | GLM | `npx -y glm-acp-agent` (needs Node.js) |
| `kimi` | Kimi | `kimi acp` |

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
smind account ls
```

`account test` prints `ok` and a detail line when the provider can start. For
`claude-native`, `codex-native` and `glm` it checks that the command is on the
daemon's `PATH`. For `kimi` it checks for a stored, unexpired credential. It
never starts a run and never calls the network.

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

`kimi acp` expects an installed, already logged-in Kimi CLI. Install it with
`pip install kimi-cli`, then run `kimi` once and log in with `/login`.
smind does not do that login.

You can also register a Kimi API key as an account. smind has no browser login
for Kimi, so the credential comes in on stdin:

```sh
smind account add kimi work < kimi-key.txt
```

`--base-url <url>` sets another API address.

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
over the daemon's own environment. These two variables are the only launch
environment you can set, and only as directories.

The same setting lives in `config.yaml`:

```yaml
providers:
  claude-native:
    env:
      CLAUDE_CONFIG_DIR: ~/.claude-work
```

GLM and Kimi have no launch environment field.

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
  `glm`, `kimi`) and must be unique.
- `extends` is `claude-native` or `codex-native`.
- `env` takes only the extended provider's own variable, `CLAUDE_CONFIG_DIR` or
  `CODEX_HOME`, as an absolute path or one starting with `~/`.
- An instance may not set `command`, `args`, `models` or `modes`.

A bad entry is rejected with the field named, for example
`providers.custom[2].command: required`. smind reads `providers.custom` at
startup. After you edit the file by hand, choose **Reload providers** in the
detail of any custom provider in **Settings** → **Providers** to apply it
without a restart. To remove an instance, delete its entry and reload. The
Settings screen has no remove button.

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

`command` is required. `env` is free-form, and smind never prints its values in
an error. `models` and `modes` are optional lists of `id`, `label`, `description`
and, for a mode, `autoApproves`. A custom ACP provider shows the generic provider
icon and the label you gave it.
