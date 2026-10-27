---
title: Writing a plugin
description: The manifest reference, what a plugin may call, hooks, RPCs, settings, how a plugin process runs, and a verified walkthrough of the hello-process example.
summary: "This page is the reference for writing a plugin for sMind by Spacing Mind: the manifest, what a plugin may call, hooks, RPCs, settings and how a plugin process runs, with a verified walkthrough. It is for plugin authors."
order: 16
---

# Writing a plugin

This page is for people who write plugins. To install and manage them, read
[Plugins](plugins.md). Everything here exists today; two things in the design
are not built and are marked **planned**: panels and sidebar items (declarative
views), and plugins that add coding-agent providers.

A plugin is a directory with a `smind-plugin.json`. It can be data only (MCP
servers, skills, profiles, prompt commands, themes: nothing runs) or it can also
declare a long-lived `process` that smind starts, which adds hooks, RPCs and
calls back into smind.

## What a plugin can and cannot do

A plugin is code you chose to run, as you. smind **mediates** what a plugin
asks of smind; it does not **sandbox** the plugin.

- A plugin process can read and write every file you can, open network
  connections, and read `~/.smind/token` and your provider credentials. smind
  removes its own secrets from the process's environment and nothing more.
- What smind does enforce: the process can call only the methods in
  [the capability table](#capability-table) for the capabilities its manifest
  declared and you confirmed, it never receives the daemon token, it cannot
  answer or change a permission request, and what it sends to you or to an agent
  is labeled with its id.
- Plugins are off until you set `plugins.enabled: true` in `config.yaml`.
  Installing, enabling, changing settings and removing are done by you on a
  loopback connection (or a network login if `plugins.allowRemoteManage` is
  true), never by an agent and never from a phone.
- smind does not stop an agent that runs as your user from editing
  `config.yaml` or reading the token. What an agent may read is decided by the
  agent's own sandbox and permission settings.
- There is no registry and no signing. A plugin's code is as trustworthy as where
  you got it; pin what you install (see [Install sources](#install-sources)).

## Quick start: hello-process

`plugin-examples/hello-process/` in the repository is a complete process plugin
in about 70 lines of Python 3 (standard library, no network). It has a process,
one RPC (`echoPrompt`), an RPC-backed slash command (`/hello`), a `run.finished`
hook that logs, and a typed setting (`greeting`).

Turn plugins on (`plugins.enabled: true` in `config.yaml`), then, from the
repository root:

```sh
smind plugin install plugin-examples/hello-process
```

```text
Installed hello-process 0.1.0 (disabled; nothing runs until you enable it).

Hello (process example) (hello-process) 0.1.0
  A tiny process plugin: one RPC, one RPC-backed slash command, a run.finished hook and a setting. Python 3, standard library only.
  Source:       .../plugin-examples/hello-process (dir)
  Content hash: c0ff700bfbff0dfca2d3f0640dc984e2ebd53e47c342baf5348d106a46709111
  Runs code:    YES, as you, with your files and credentials
  Process:      python3 server.py
  Hook:         run.finished (on error: open)
  May call:     nothing

To turn it on: smind plugin enable hello-process
```

`install` never starts anything. Enabling prints the same summary and asks you to
confirm (`--yes` skips the question):

```sh
smind plugin enable hello-process --yes
smind plugin show hello-process
smind plugin logs hello-process
```

```text
State: enabled (confirmed: true)
Process: running (pid 99167)
...
07:02:34.299 [smind] started python3 (pid 99167)
07:02:34.314 hello-process ready, greeting='Hello'
07:02:34.314 [smind] ready
```

The first log line is smind's, the second is the script writing to stderr. Now
open a task's message box and type `/hello`: the list shows the command with
"from hello-process", and picking it puts `Hello! Please introduce yourself and
summarize this task.` in the box. Nothing is sent; you edit and press send.

Under the hood, picking the command made smind call the plugin's RPC. The same
call as a wsapi request (a websocket to `/ws` with the daemon token) is
`plugin.call`:

```json
{"id": "1", "method": "plugin.call", "params": {"pluginId": "hello-process", "rpc": "echoPrompt", "params": {"command": "hello-process:hello", "args": ""}}}
```

```json
{"prompt":"Hello! Please introduce yourself and summarize this task."}
```

The `greeting` setting changes the answer. Settings are set with the
`plugin.settings.set` request, which restarts the process:

```json
{"id": "2", "method": "plugin.settings.set", "params": {"id": "hello-process", "values": {"greeting": "Ahoy"}}}
```

The next `echoPrompt` answers `{"prompt":"Ahoy! Please introduce yourself and summarize this task."}`, and the log
shows the restart (`started python3 (pid ...)`, `greeting='Ahoy'`). Asking for a name the manifest does not
declare never reaches the process:

```json
{"error":{"message":"plugin.call: plugin hello-process: it does not declare rpc \"nope\" in contributes.rpc","code":"unknown_rpc","data":{"plugin":"hello-process","rpc":"nope"}}}
```

When a run finishes, the plugin's `run.finished` hook receives
`{runId, taskId, chatId, ...}` and the script writes
`run finished: run=<id> task=<id> status=<status>` to its log.

Clean up with `smind plugin disable hello-process` and
`smind plugin remove hello-process`.

### How the script works

`server.py` reads `Content-Length`-framed JSON-RPC 2.0 messages from stdin and
writes them to stdout (the framing language servers use). It answers
`plugin.initialize` (the answer is the ready signal and must come within 10
seconds), handles `rpc.echoPrompt` and `hook.run.finished`, exits on
`plugin.shutdown`, and answers any other request with error `-32601`. Copy it as a
starting point; any language that can read and write stdio works.

## Manifest reference

`smind-plugin.json` at the plugin root, at most 256 KiB. Unknown top-level keys
are ignored; unknown keys inside `engines` are rejected, and so are unknown
capability names. Every error names the field and the fix, and `install`, `enable`,
`update` and `reload` check the whole file again.

| Field | Required | Rules |
|---|---|---|
| `id` | yes | 1-64 characters from `a-z`, `0-9`, `-`, starting with a letter; not a Windows device name (`con`, `nul`, ...). Names everything the plugin adds as `<id>:<name>`. |
| `name` | yes | 1-100 characters, display name. |
| `version` | yes | Semantic version, for example `1.2.0` or `1.2.0-rc.1`. |
| `api` | yes | Integer; the daemon supports `1`. See [Versioning](#versioning). |
| `engines.smind` | yes | A semver range the running smind must satisfy, for example `>=0.15.0` or `^0.15.0`. Nothing else is allowed under `engines`. A development build of smind skips the check. |
| `description` | no | Up to 500 characters. |
| `process.command` | no | An argument list (1-32 entries, no shell). Needed for hooks, RPCs and RPC commands. See [The process](#the-process). |
| `capabilities` | no | Names from [the capability table](#capability-table) (and `hooks.beforeRun`): `events.observe`, `files.read`, `hooks.beforeRun`, `mcp.read`, `profiles.read`, `tasks.read`, `tasks.write`, `ui.notify`. Anything else fails. |
| `settings[]` | no | `key` (letters, digits, `_`, starting with a letter, unique), `type` (`string`, `secret`, `boolean`, `number`, `enum`), `label`; optional `description`, `required`, `default`, `options` (enum), `maxLength` (string and secret, up to 8192 bytes). A `secret` cannot have a default. See [Settings and secrets](plugins.md#settings-and-secrets). |
| `contributes.mcpServers[]` | no | `name`, `transport` (`stdio`, `http`, `sse`), `command` (stdio) or `url` (http, sse), optional `env` (stdio) or `headers` (http, sse). `${setting.KEY}` is allowed in `env` and `headers` only, and must name a declared setting. |
| `contributes.skills[]` | no | Paths of directories that hold a `SKILL.md`. |
| `contributes.profiles[]` | no | Paths of JSON profile files. |
| `contributes.commands[]` | no | `name`, optional `description`, and exactly one of `prompt` (path of a UTF-8 text file, up to 16 KiB) or `rpc` (a name declared in `contributes.rpc`; needs `process`). |
| `contributes.rpc[]` | no | `name`, optional `description`, optional `timeoutMs` (1 to 30000). Needs `process`. Names are unique. |
| `contributes.themes[]` | no | `id`, optional `label` (up to 48 characters), `file` (a JSON color set). See [Themes](plugins.md#themes). |
| `contributes.hooks[]` | no | `event` (`run.beforeStart` or `run.finished`) and optional `onError` (`open` or `closed`). Needs `process`. |
| `contributes.providers[]`, `panels[]`, `sidebar[]` | no | Reserved. `providers` entries are checked but do nothing; `panels` and `sidebar` are accepted and ignored. **Planned**, not built. |

Names (`commands[].name`, `rpc[].name`, `themes[].id`, MCP server names) are 1-64
characters of letters, digits, `.`, `_` and `-`, starting with a letter or digit.
Paths are relative, forward-slashed, stay inside the plugin and may not be under
`data/`.

A complete manifest that uses most of it:

```json
{
  "id": "acme",
  "name": "Acme tools",
  "version": "1.0.0",
  "api": 1,
  "engines": { "smind": ">=0.15.0" },
  "process": { "command": ["python3", "server.py"] },
  "capabilities": ["tasks.read", "events.observe", "hooks.beforeRun"],
  "settings": [{ "key": "token", "type": "secret", "label": "API token", "required": true }],
  "contributes": {
    "rpc": [{ "name": "status", "timeoutMs": 5000 }],
    "commands": [
      { "name": "status", "description": "Insert the current status", "rpc": "status" },
      { "name": "review", "description": "Review the diff", "prompt": "commands/review.md" }
    ],
    "hooks": [{ "event": "run.finished" }, { "event": "run.beforeStart", "onError": "open" }]
  }
}
```

## The process

`process.command` is an argument list and never goes through a shell. A first
entry with a path separator (`./bin/server`) is a program inside the plugin (or an
absolute path); a bare name (`python3`) is looked up on a fixed `PATH`, not yours:
`/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin` on macOS and
Linux. Put an interpreter somewhere on that list or ship the program in the plugin.

- **Environment.** Built, not inherited: that `PATH`, `HOME` (and `USERPROFILE`)
  set to the plugin's `data/` directory, the temp directory inside it, and
  `LANG`, `LC_*` and `TZ`. No `SMIND_*` and no provider key.
- **Directories.** The working directory is the plugin directory (the linked
  directory for a link). `data/` is persistent per-plugin storage that survives
  updates and is deleted by `remove` unless `--keep-data`.
- **Framing.** `Content-Length` JSON-RPC 2.0 both ways. Headers end in CRLF, are
  ASCII and at most 1 KiB each; only `Content-Length` and `Content-Type` are
  accepted. A body is at most 1 MiB. Any violation (malformed header, a body that
  is not a JSON-RPC object, a batch) closes the connection and kills the process,
  which counts as a crash.
- **Handshake.** smind sends `plugin.initialize {api, smindVersion, dataDir,
  settings}` and the plugin has 10 seconds to answer. `settings` always exists:
  your values (secrets in the clear) plus defaults.
- **Host to plugin.** `plugin.initialize`, `rpc.<name>`, `hook.run.beforeStart`
  (request), `hook.run.finished` (notification), `event` and `event.dropped`
  notifications, and `plugin.shutdown` (notification). At most 32 requests are
  outstanding; a request times out after 30 seconds.
- **Plugin to host.** Requests are answered through the capability table; at most
  16 are handled at once. A request the plugin may not make gets `-32001`
  (forbidden) or `-32601` (method not found).
- **Lifecycle.** A crash restarts after 1 s, 2 s, 4 s ... up to 30 s; a run of 60 s
  resets the count. Five crashes in ten minutes mark the plugin **failed**: it stays
  enabled, is not restarted, and leaves that state by `smind plugin reload <id>`
  or disable and enable. Stop is `plugin.shutdown`, SIGTERM to the process group, 5
  seconds, then a kill of the group (a job object on Windows).
- **Logs.** `smind plugin logs <id> [--lines=N] [--json]`: stderr plus `[smind]`
  lines, in memory (1000 lines, 256 KiB, 2 KiB per line), control characters
  removed, secrets and token shapes redacted before storing. Write diagnostics to
  stderr, never to stdout (stdout is the protocol).

## Capability table

A plugin declares `capabilities` in its manifest; you confirm them when you
enable it. The host enforces them on every call. This table is checked against the
code by a test, so it is the whole plugin API:

<!-- BEGIN CAPABILITY TABLE -->
| Capability | Methods |
|---|---|
| `events.observe` | `events.subscribe`, `events.unsubscribe` |
| `mcp.read` | `mcp.get`, `mcp.list` |
| `profiles.read` | `profile.get`, `profile.list` |
| `tasks.read` | `chat.list`, `run.list`, `space.get`, `space.list`, `task.get`, `task.list`, `task.tree`, `workspace.list` |
| `tasks.write` | `task.send` |
<!-- END CAPABILITY TABLE -->

`hooks.beforeRun` grants no call: it lets the `run.beforeStart` hook run.
`files.read` and `ui.notify` can be declared and grant nothing yet. A listed
method whose capability was not declared fails `forbidden` naming it; an unlisted
method (including anything smind adds later) fails `method not found`. `task.send`
also needs `tasks.read`. The plugin, provider, account, relay, automation and
settings methods, writes to your tasks, MCP servers and profiles, and anything
that answers a permission request are never available. Details of the allowed
topics, `task.send` limits and results are in
[What a plugin may call](plugins.md#what-a-plugin-may-call).

The topics a plugin with `events.observe` may subscribe to:

<!-- BEGIN EVENT TOPICS -->
`chat.archived`, `chat.created`, `chat.updated`, `permission.pending`, `permission.resolved`, `plugin.updated`, `run.failover`, `run.status`, `space.created`, `space.deleted`, `task.archived`, `task.created`, `task.deleted`, `task.stalled`, `task.status`, `task.updated`, `workspace.created`, `workspace.deleted`
<!-- END EVENT TOPICS -->

## Hooks

Declare hooks in `contributes.hooks`; they need a `process`.

**`run.finished`** is observe-only. `hook.run.finished` is a notification with
`{runId, taskId, chatId, provider, model, origin, status, stopReason?,
failureKind?, toolCalls}`: ids and states, never the prompt or the output.
Delivery is best effort from a 64-event queue per plugin (the newest is dropped
when it is full) so a slow plugin never delays smind. System runs (learning,
probes) are not reported.

**`run.beforeStart`** needs the `hooks.beforeRun` capability. Just before an agent
starts, smind sends the request `hook.run.beforeStart {runId, taskId, chatId,
workspaceId, provider, model, permissionMode, origin}` and waits up to 10
seconds. The result may hold exactly these fields, and smind checks each:

| Field | Limit | Effect |
|---|---|---|
| `additionalContext` | string, 8 KiB | Appended to the agent's prompt in a `[plugin <id>]` ... `[end plugin <id>]` block. The stored run prompt stays the sender's text; the timeline notes the size, never the text. |
| `env` | up to 16 string entries, values 4 KiB | Added to the agent's environment. Names match `^[A-Z][A-Z0-9_]{0,63}$`. Names smind, a provider, the shell or a loader use are refused: `PATH`, `HOME`, `USER`, `SHELL`, `TMPDIR`, `LANG`, `TZ`, the proxy variables, `NODE_OPTIONS`; prefixes such as `SMIND_`, `ANTHROPIC_`, `CLAUDE`, `OPENAI_`, `CODEX_`, `GLM_`, `KIMI_`, `GOOGLE_`, `AWS_`, `GIT_`, `SSH_`, `GITHUB_`, `LD_`, `DYLD_`, `NODE_`, `NPM_`, `PYTHON`, `BASH_`; and anything ending `_KEY`, `_TOKEN`, `_SECRET`, `_PASSWORD`, `_CREDENTIALS`, `_PROXY` or `_AUTH`. A key already set is never overridden. Codex runs take no per-run environment. |
| `mcpServers` | up to 16 names | Servers this same plugin contributes (manifest name or `<id>__<name>`). Any other name fails `mcp_refused`. |

Any other field fails the hook with a typed error naming it: `forbidden_edit` for
`provider`, `cwd`, `model`, `profile`, `permissionMode`, `autoAccept`, a
permission decision or a rewritten input; `unknown_field` for anything else.
Nothing from a failed answer is applied. **There is no permission hook**: nothing
in the API can approve, deny or answer a permission request, and smind has no
auto-approval. A plugin may observe `permission.pending` (ids and option names,
not its summary) and nothing more.

`onError` is `open` (default: the run goes ahead and the failure shows as the
plugin's `lastError`, in its log and as a note on the run) or `closed` (the run
ends in error as `plugin <id> hook run.beforeStart: <code>: <reason>`). A hook
answers failure with a JSON-RPC error. A plugin that is not running, or lacks
`hooks.beforeRun`, is skipped and the skip is shown. Several plugins run in id
order, then manifest order; instructions append in that order, the first plugin to
set an env key keeps it, and nothing is merged deeper.

Names follow Claude Code's hook vocabulary where the meaning matches: the result
field `additionalContext` is the one `SessionStart` uses, and `run.finished`
corresponds to `Stop`. There are no exit codes and no per-tool hooks
(`PreToolUse`, `PostToolUse`); smind does not see every provider's tool calls.

## RPCs and slash commands

List the methods a client may call in `contributes.rpc`; the process answers
`rpc.<name>`. The wsapi request `plugin.call {pluginId, rpc, params}` runs one
for you (and for a paired phone; not for agents, and never from one plugin to
another). Only declared names are ever sent to the process. Params are at most 64
KiB, a result at most 256 KiB (a bigger one is dropped), JSON only, 4 calls at
once per plugin, 30 seconds per call; `timeoutMs` can lower that, never raise it.

A command with `rpc` is listed in the `/` list on web and phone like a prompt
command. Picking it calls the RPC with `{"command": "<id>:<name>", "args": ""}`
and the result must be `{"prompt": "<text>"}` (up to 16 KiB). The text goes into
the message box as plain characters and is never sent. See
[Plugin RPCs](plugins.md#plugin-rpcs) for the rest.

## Settings and secrets

Declare typed `settings` and read them from `plugin.initialize`. Secrets are
write-only: no request returns one, and they are redacted from logs. A change
restarts the process. See [Settings and secrets](plugins.md#settings-and-secrets)
for the limits and who may set values.

## Install sources

```sh
smind plugin install ./acme                       # a directory (copied)
smind plugin link ./acme                          # a directory, used in place, for development
smind plugin install git+https://example.com/acme.git@<40-hex commit>
smind plugin install ./acme.tar.gz#sha256=<64-hex hash>
```

A Git source must name a full 40-character commit, and an archive must carry its
sha256; a branch, tag or short sha is refused (`unpinned_ref`, `unpinned_archive`),
and a changed archive fails `hash_mismatch`. `ssh` Git URLs, `http` and URLs with
passwords are not accepted. Extraction refuses `..`, absolute paths, drive
letters, symlinks and hard links, duplicate names, more than 5000 entries, files
over 16 MiB and 64 MiB in total. Everything is staged and validated before it is
moved into place, so a failed install leaves nothing. A repository's contents
never install a plugin; only an explicit `install` or `link` by you does.

`smind plugin update <id>` stages a new version. One that changes what the
plugin runs or may call (its process, MCP and provider commands, hook events,
capabilities, flagged profiles) waits until you confirm with `--yes`, and the
old version keeps serving. A linked plugin that changes such a field on `reload`
is disabled until you enable it again. `smind plugin reload <id>` re-reads and
restarts the process; a failed reload keeps the last good version.

## Versioning

- `api` is the plugin API major. The daemon loads the majors it supports (today
  `1`) and refuses others naming the field. Within a major the API only grows:
  new capabilities, methods and manifest fields are added, existing ones keep
  their meaning. A breaking change bumps `api`.
- Because the capability table follows the daemon's own request methods, a method
  that is not in the table stays unavailable until it is added to it.
- `engines.smind` gates the daemon version, checked at install, enable, reload and
  daemon start. Use `>=` the first release you tested.
- Your own `version` is a semantic version; an update with the same capabilities
  and commands applies at once.

## Troubleshooting

Typed errors name the plugin where one is involved. Over a script they are the
`code` of the error; the CLI prints it inside the message, for example
`plugin install: plugin.install: unpinned_ref: a Git source must be pinned to a full 40-hex commit sha: ...`.

| Code | Meaning and fix |
|---|---|
| `plugins_disabled` | `plugins.enabled` is not true in `config.yaml`. Edit the file; no command sets it. |
| `bad_manifest` | A field is wrong; the message names it and the fix. |
| `unpinned_ref`, `unpinned_archive`, `hash_mismatch` | Pin the Git commit (40 hex) or the archive sha256. |
| `unsafe_entry`, `link_rejected`, `too_large`, `too_many_entries`, `duplicate_entry`, `bad_archive`, `bad_source`, `git_failed` | The source was refused or could not be read; the message says why. |
| `already_installed`, `not_found` | Use `update` for an installed plugin; check the id with `smind plugin list`. |
| `consent_required` | `enable` needs the confirmation of what runs (`--yes`). |
| `update_pending`, `no_pending_update` | An update that changes what runs waits for `update <id> --yes`. |
| `bad_setting` | A settings value does not fit its declaration; the message names the setting. |
| `forbidden` | Not available to this caller: management needs a local human connection; a capability was not declared. |
| `read_only_owned` | A row a plugin owns (MCP server, profile) cannot be edited. Disable the plugin instead. |
| `unknown_rpc` | The name is not in `contributes.rpc`. Declare it. |
| `plugin_unavailable` | The plugin is off, disabled, not running, failed, or died during the call. See `smind plugin show` and `logs`. |
| `call_timeout` | No answer within the timeout (30 s at most). |
| `call_too_large` | Params over 64 KiB or a result over 256 KiB (the result was dropped). |
| `plugin_busy` | 4 calls are already running for that plugin. Retry. |
| `plugin_error` | The plugin answered with an error; its text (cut, redacted) is in the message. |
| `bad_call` | `params` is not valid JSON. |
| `forbidden_edit`, `unknown_field`, `mcp_refused` | A `run.beforeStart` answer tried something not allowed; see [Hooks](#hooks). |
| `-32003` (to the plugin) | A `task.send` was refused by a limit; `data.reason` says which. |

If a plugin shows **failed**, read `smind plugin logs <id>` for the last crash,
fix it and run `smind plugin reload <id>`. If `/hello`-style commands do not
appear, check that the plugin is enabled and running (`smind plugin list`) and that
the global switch is on.

## Further reading

- [Plugins](plugins.md): installing and managing plugins, the Plugins page, themes and prompt commands.
- [Plugin system decision record](../docs/decisions/0042-plugin-system.md)
