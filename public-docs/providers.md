---
title: Providers
description: Log in to Claude Code, Codex, GLM and Kimi, and choose each provider's config directory.
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

## Provider instances (coming)

> This section is a placeholder. Provider instances are not available yet.
