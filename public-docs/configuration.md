---
title: Configuration reference
description: The user-facing keys of config.yaml and their defaults.
summary: "This page documents the user-facing keys of config.yaml for sMind by Spacing Mind, where the file lives and the default of each key. It is for people who tune or operate the daemon."
order: 12
---

# Configuration reference

The daemon reads one file, `config.yaml`. Every key is optional. A missing key
keeps its default, and a missing file is fine.

## Where config.yaml lives

The file is `config.yaml` inside the smind home directory. The home directory
is `~/.smind` unless the `SMIND_HOME` environment variable names another one.
The daemon log, `daemon.log`, lives in the same directory.

```sh
SMIND_HOME=/srv/smind smind serve
```

The CLI and the daemon must see the same `SMIND_HOME`. `smind status` prints
the home directory and config file path the CLI uses.

## Change settings from the command line

`smind config` edits the file for you and keeps its comments.

```sh
smind config get                       # all settable keys
smind config get server.port           # one key
smind config set server.port 4650
smind config unset server.port         # back to the default
```

Only these paths can be set:

| Path | Value |
| --- | --- |
| `server.port` | A port from 1 to 65535. |
| `server.listen` | `host:port` to bind. |
| `server.hostnames` | Comma-separated host names. |
| `providers.<id>.defaultModel` | A model name. `<id>` is `claude-native`, `codex-native`, `glm` or `kimi`. |

`server.passwordHash` can only be removed with `smind config unset`. Set it with
`smind set-password`. Every other key below is edited by hand in `config.yaml`.

Changes apply when the daemon restarts. If the file does not parse or a value
fails the checks below, the daemon logs a `config:` error and carries on with
its default settings, so check the log after you edit by hand.

## Keys

`config get` shows values as stored in the file. It does not show defaults or
environment overrides.

### server

| Key | Default | Notes |
| --- | --- | --- |
| `server.port` | `4648` | Port of the default listen address. |
| `server.listen` | empty | `host:port`. Empty means `127.0.0.1` and `server.port`. If it also names a port, that port must match `server.port`. |
| `server.hostnames` | empty | Extra `Host` header names accepted besides `localhost` and IP addresses. A leading `.` matches the domain and its subdomains, such as `.example.com`. No ports, schemes or wildcards. |
| `server.passwordHash` | empty | A bcrypt hash written by `smind set-password`. |

A non-loopback `server.listen` (for example `0.0.0.0:4648`) needs a password.
The daemon refuses to start without one. The daemon always also listens on
`127.0.0.1` so local clients keep working.

### orchestration

| Key | Default | Notes |
| --- | --- | --- |
| `orchestration.maxDepth` | `2` | How many ancestors a task may have. A root task is depth 0. |
| `orchestration.maxConcurrentRunsPerTree` | `4` | Runs that may be active at once in one task tree. Must be 1 or more. |
| `orchestration.notifyParent` | `true` | When a child task goes quiet, queue one message on the parent chat. |
| `orchestration.notifyDebounceSeconds` | `20` | Hold a parent message so several finished children merge into one. `0` sends at once. |
| `orchestration.stallAlertMinutes` | `15` | Raise a "tree stalled" notification after this long with unfinished work and nothing running or queued. `0` turns it off. |

### worktrees

| Key | Default | Notes |
| --- | --- | --- |
| `worktrees.setupTimeoutSeconds` | `600` | Time limit for a task's setup script. |
| `worktrees.teardownTimeoutSeconds` | `120` | Time limit for a teardown script. |
| `worktrees.ports.range` | empty | `lo-hi` inside 1024-65535. Empty turns per-task port allocation off. |

### providers

```yaml
providers:
  claude-native:
    defaultModel: sonnet
    env:
      CLAUDE_CONFIG_DIR: ~/.claude-work
```

| Key | Default | Notes |
| --- | --- | --- |
| `providers.<id>.defaultModel` | empty | Model a run uses when nothing else names one. Empty keeps the provider's own default. Up to 100 characters from letters, digits and `. _ : / @ [ ] -`; it may not start with `-`. |
| `providers.<id>.env.CLAUDE_CONFIG_DIR` | empty | `claude-native` only. An absolute path, or one starting with `~/`. |
| `providers.<id>.env.CODEX_HOME` | empty | `codex-native` only. Same rules. |
| `providers.custom` | empty | A list of extra providers: instances of Claude Code or Codex with their own config directory (`extends`), and custom ACP agents (`command`). See [Provider instances](providers.md#provider-instances). |

An unknown provider id under `providers.<id>` is an error. See
[Providers](providers.md) for the launch environment.

### agents

| Key | Default | Notes |
| --- | --- | --- |
| `agents.glm.maxTurns` | unset | Turn limit passed to the GLM agent. From 1 to 10000. Unset keeps the agent's own default. |
| `agents.sharedCaches` | `true` | Agent runs reuse your normal Go, npm and bun caches (`GOMODCACHE`, `GOCACHE`, `npm_config_cache`, `BUN_INSTALL_CACHE_DIR`), so a temporary `HOME` does not re-download or rebuild. A value the agent's own env already sets is never overridden. |

### accountUsage

| Key | Default | Notes |
| --- | --- | --- |
| `accountUsage.enabled` | `true` | Turn off to stop every outbound usage request. |
| `accountUsage.ttlSeconds` | `300` | How long a usage snapshot stays fresh. It can only be raised; the minimum is 300. |
| `accountUsage.routing` | `true` | Route around accounts whose usage window is exhausted. |
| `accountUsage.exhaustThresholdPct` | `100` | Usage percent at which an account counts as exhausted. From 1 to 100. |
| `accountUsage.passive` | `true` | Watch runs passively and cool down accounts that hit a limit. |

### relay

| Key | Default | Notes |
| --- | --- | --- |
| `relay.maxDevices` | `16` | How many paired devices may be connected at once. Must be 1 or more. |
| `relay.legacyDevices` | `false` | Admits older apps that cannot prove a device identity, without approval. Leave it off. |

### push

| Key | Default | Notes |
| --- | --- | --- |
| `push.enabled` | `true` | Send mobile push notifications. A daemon with no registered phone sends nothing. |
| `push.expoAccessToken` | empty | Only needed if your push project requires it. Prefer the `SMIND_EXPO_ACCESS_TOKEN` variable over writing it here. |

### log

| Key | Default | Notes |
| --- | --- | --- |
| `log.maxSizeMB` | `10` | Size at which `daemon.log` rotates. Must be 1 or more. |
| `log.maxFiles` | `5` | Log files kept, including the active one. Must be 1 or more. |

### fleet

| Key | Default | Meaning |
|---|---|---|
| `fleet.enabled` | `false` | Turns on [connected hosts](hosts.md). Only a hand edit of this file turns it on, and it needs a restart. With it off no key is created and nothing dials out. |
| `fleet.maxHosts` | `16` | The most hosts one daemon may be paired with. |
| `fleet.handshakeSeconds` | `10` | How long a peer handshake may take. |
| `fleet.replayWindowMinutes` | `10` | How long a request id is remembered to refuse a replay. |
| `fleet.auditRetentionDays` | `30` | How long the fleet audit trail is kept. |

### automations

| Key | Default | Meaning |
|---|---|---|
| `automations.enabled` | `false` | Runs the automation engine. With it off nothing polls and nothing fires. Read on every tick. |
| `automations.paused` | `false` | Records every signal as paused and fires nothing, keeping all state. |
| `automations.maxBudgetUsd` | `10` | The most a single automation run's budget may be. |
| `automations.allowRemoteManage` | `false` | Lets a direct non-loopback connection create, enable, resume or fire automations. Relay sessions and agents are refused either way. |
| `automations.allowUnattendedModes` | `false` | Lets an automation use `autoAccept` or a full-access permission mode. |

### plugins

| Key | Default | Notes |
| --- | --- | --- |
| `plugins.enabled` | `false` | Turns plugins on. With it off, no plugin adds anything. Read on every request, no restart needed. |
| `plugins.allowRemoteManage` | `false` | Lets a direct connection that is not a loopback connection install, enable, disable, remove, update or reload plugins. Read on every request. |

With `plugins.allowRemoteManage` off, those actions need a loopback connection.
A same-host reverse proxy that terminates TLS counts as loopback. Paired
phones (the relay) and agents cannot manage plugins whatever this is set to.
See [Plugins](plugins.md).

### steward

| Key | Default | Meaning |
| --- | --- | --- |
| `steward.policy.enabled` | `false` | Turn on the standing permission policy in `~/.smind/steward/policy.yaml` (see [Scripting and permissions](scripting-and-permissions.md#a-standing-permission-policy-optional)). Read on every decision. |

### learning

Learning is on by default. Set `learning.enabled: false` to stop collection and
delivery everywhere. Defaults for the other keys are tuned already; change
them only if you know you need to.

| Key | Default |
| --- | --- |
| `learning.enabled` | `true` |
| `learning.cadenceToolCalls` | `50` |
| `learning.tailFlushPending` | `10` |
| `learning.tailIdleMinutes` | `30` |
| `learning.cooldownMinutes` | `30` |
| `learning.windowKB` | `120` |
| `learning.recordKB` | `4` |
| `learning.inputKB` | `1` |
| `learning.resultKB` | `1` |
| `learning.failureResultKB` | `2` |
| `learning.dailyBudgetUSD` | `0.25` |
| `learning.dailyBudgetTokens` | `150000` |
| `learning.probationRuns` | `25` |
| `learning.capacity` | `30` |
| `learning.curatorEveryLandings` | `5` |
| `learning.curatorMinSkills` | `8` |
| `learning.curatorMinIntervalDays` | `7` |
| `learning.prefaceBudgetBytes` | `6144` |
| `learning.claudeIndexPreface` | `false` |
| `learning.similarity.duplicate` | `0.85` |
| `learning.similarity.possible` | `0.6` |
| `learning.reflectorModel` | `claude-haiku-5-5` |
| `learning.curatorModel` | `claude-haiku-5-5` |
| `learning.probe.prevalenceDays` | `30` |
| `learning.probe.prevalenceMaxEvents` | `20000` |
| `learning.probe.perProbeBudgetUSD` | `1.5` |
| `learning.probe.dailyBudgetUSD` | `3` |
| `learning.probe.quotaSkipPercent` | `80` |
| `learning.probe.armTimeoutMinutes` | `15` |
| `learning.probe.maxTurnsCap` | `80` |
| `learning.probe.retentionDays` | `14` |
| `learning.probe.outputCapKB` | `256` |

Every number must be 1 or more. Budgets must be above 0, and
`learning.probe.perProbeBudgetUSD` may not exceed `learning.probe.dailyBudgetUSD`.
`learning.similarity` needs `0 < possible < duplicate <= 1`.

## Environment variables

| Variable | Effect |
| --- | --- |
| `SMIND_HOME` | Home directory instead of `~/.smind`. |
| `SMIND_LISTEN` | Overrides `server.listen`. |
| `SMIND_HOSTNAMES` | Overrides `server.hostnames`. Separate names with commas or spaces. |
| `SMIND_PASSWORD` | Password for remote clients, hashed in memory at start. It is never read from or written to `config.yaml`. Handy for containers. |
| `SMIND_PUSH` | `0`, `false`, `off`, `no`, `n`, `f` or `disabled` turns push notifications off. |
| `SMIND_EXPO_ACCESS_TOKEN` | Overrides `push.expoAccessToken`. |
| `SMIND_LEARNING` | The same off values force `learning.enabled` off. |
| `SMIND_VERSION`, `SMIND_INSTALL_DIR`, `SMIND_APP_DIR` | Read by the install scripts. See [Getting started](getting-started.md). |

Environment variables win over the file.
