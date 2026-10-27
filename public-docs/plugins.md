---
title: Plugins
description: Add MCP servers, skills, agent profiles, prompt commands and themes from a plugin directory, and see what a plugin does before you turn it on.
order: 12
---

# Plugins

Plugins are a preview. A plugin is a directory with a `smind-plugin.json`
file. Today a plugin can add five things: MCP servers, skills, agent profiles,
prompt commands and themes, and it can declare a long-lived process that smind
starts (see [Plugin processes](#plugin-processes)). Nothing else a manifest
declares (providers, panels) has an effect yet.

A plugin is trusted code you chose to run. An MCP server it adds starts as
you, with your files and credentials, exactly like an MCP server you add by
hand. smind shows what a plugin would run before you turn it on, but it does
not sandbox anything.

## Turn plugins on

Plugins are off until you set this in `config.yaml`:

```yaml
plugins:
  enabled: true
```

The setting is read on every request, so a change applies without a restart.
With it off, no plugin adds anything, even one that is enabled.

Installing, enabling, disabling, removing, updating and reloading a plugin are
human actions. They need a loopback connection to the daemon, which includes a
reverse proxy on the same machine. To manage plugins over a network login, set
`plugins.allowRemoteManage: true`; it is off by default. Agents and paired
phones cannot do any of this whatever the setting. A phone can still see the
lists, offer plugin prompt commands and apply plugin themes; see
[Plugins on the phone](mobile-and-relay.md#plugins-on-the-phone).

smind does not stop an agent that runs as your user from reading the daemon's
token or `config.yaml`. What an agent may read is decided by the agent's own
sandbox and permission settings, so keep those strict if you run agents you
do not trust.

## Install and enable

```sh
smind plugin install ./acme
smind plugin enable acme
```

`install` copies the directory, records the plugin as disabled and prints what
it would do. `enable` prints the same summary and asks you to confirm;
`--yes` confirms without asking.

The summary lists each MCP server's command or URL and environment, each skill
and each profile. A profile is flagged when it raises the permission mode, skips
permission prompts, turns on orchestration tools or may launch child agents.
An update that adds such a profile, or changes what a plugin runs, waits for
you to confirm it again.

To work on a plugin without copying it, use `smind plugin link <dir>`. A
linked plugin is read from its directory as it is, so the summary carries the
warning "live directory: changes after consent are not re-reviewed", and
`smind plugin enable` prints it.

`smind plugin list`, `show`, `disable`, `remove`, `update` and `reload` do what
their names say. Disabling or removing a plugin takes its servers, skills and
profiles away at once.

## The Plugins page

Settings → Plugins shows what is installed and lets you manage it in the web
app and the desktop app. The page cannot turn plugins on: with `plugins.enabled`
off it shows **Plugins are off**, how to set it in `config.yaml`, and every
action stays disabled until you edit the file.

Each plugin is a row with its name, version, where it came from (`dir`, `link`,
`git` or `archive`) and its status: **Enabled** or **Disabled**, plus **Pending
update** and **Error** when they apply. An error row shows the last error the
daemon recorded. A linked plugin carries the line "Live directory: changes after
consent are not re-reviewed". With no plugins the page says **No plugins
installed** and shows `smind plugin install <path>`.

- **Details** (the arrow on the row) show the same summary `smind plugin enable`
  prints: whether anything runs, the capabilities, each command, the hook
  events, the skills and the agents, with an agent that raises the permission
  mode, skips prompts, gets orchestration tools or may launch children marked
  **Escalates**. They also list what the plugin contributes, anything that was
  not applied, the last error, and when it was installed and updated.
- **Enable** opens that summary in a confirmation (with the live-directory
  warning for a linked plugin). Nothing is enabled until you confirm.
  **Disable** needs no confirmation.
- The row's menu has **Reload**, **Check for update** (or **Review update** when
  one is pending) and **Remove**. Remove asks first and has a **Keep plugin data**
  box, off by default; a linked directory is never deleted.
- An update that changes what a plugin runs shows **Update available** with each
  change; **Review and apply** asks you to confirm the new summary.
- **Install plugin** takes a plugin directory, a Git URL pinned to a full commit
  or an archive pinned with `#sha256=`. The plugin is installed disabled, and
  its details open so you can review it before enabling.

These actions are refused on a relay connection, and on a remote login unless
`plugins.allowRemoteManage` is set. The page shows the daemon's message and
becomes read-only. The page does not show process logs yet (it says "Process
logs arrive with plugin processes."); use `smind plugin logs <id>`.

Anything a plugin added is labelled "from acme" (its id) in Settings → MCP
servers and Settings → Agents, in the agent picker of the composer, and next to
the plugin's skills when you choose skills for an agent. These rows cannot be
edited, disabled or deleted there; the badge opens the plugin in Settings →
Plugins.

## The manifest

```json
{
  "id": "acme",
  "name": "Acme review tools",
  "version": "1.0.0",
  "api": 1,
  "engines": { "smind": ">=0.1.0" },
  "description": "A reviewer seat, a skill and a notes server",
  "contributes": {
    "mcpServers": [
      { "name": "notes", "transport": "stdio", "command": ["npx", "-y", "notes-mcp"], "env": { "MODE": "readonly" } }
    ],
    "skills": ["skills/review"],
    "profiles": ["profiles/reviewer.json"],
    "commands": [
      { "name": "review", "description": "Review the staged diff", "prompt": "commands/review.md" }
    ],
    "themes": [{ "id": "dusk", "label": "Dusk", "file": "themes/dusk.json" }]
  }
}
```

- `id`: lowercase letters, digits and `-`, starting with a letter.
- `api` must be `1`. `engines.smind` is a version range the running smind must
  satisfy.
- `mcpServers`: `name`, `transport` (`stdio`, `http` or `sse`), `command` as a
  list for `stdio` or `url` for `http` and `sse`, and optional `env` (`stdio`)
  or `headers` (`http`, `sse`).
- `skills`: each entry is a directory holding a `SKILL.md`. The directory name
  is the skill name.
- `profiles`: each entry is a JSON file with `name`, `provider` and, optionally,
  `permissionMode`, `autoAccept`, `thinkingLevel`, `model`, `notes`,
  `onRateLimit` (`none` or `wait`), `orchestrationTools`, `instructions`,
  `skills`, `childPolicy` and `childProfiles` (names of the plugin's other
  profiles).

```json
{ "name": "reviewer", "provider": "glm", "instructions": "You review changes.", "skills": ["acme:review"] }
```

- `commands`: each entry has a `name`, an optional `description` and a `prompt`,
  the path of a UTF-8 text file (up to 16 KiB). See [Prompt commands](#prompt-commands).
- `themes`: each entry has an `id`, an optional `label` (up to 48 characters)
  and a `file`, a JSON color set. See [Themes](#themes).

smind rejects a manifest at install time and names the field that is wrong.

## What an enabled plugin adds

Everything a plugin adds is shown with its owner and cannot be edited or
deleted while the plugin is enabled.

- **MCP servers** appear in `smind mcp ls` as `acme:notes`, in the OWNER column
  as `plugin:acme`. New runs receive them like any other MCP server; the agent
  sees the server named `acme__notes`.
- **Profiles** appear in `smind profile ls` as `acme:reviewer` and work
  anywhere a profile does, for example
  `smind task send <taskId> "check the diff" --profile <profileId>`. You cannot
  create a profile with the same name as a plugin's.
- **Skills** are named `acme:review`. A profile may list `acme:review` in its
  `skills`. Every enabled plugin skill is also offered to new runs, read-only:
  smind never writes a plugin skill into `~/.claude/skills`, a repository's
  `.claude/skills` or `.agents/skills`. Codex runs get no plugin skills.

A run keeps what it was started with. Disabling a plugin, removing it or turning
the global setting off changes what the next run gets.

If you already have a profile with the name a plugin would use, yours wins and
`smind plugin show <id> --json` lists the plugin's profile under `conflicts`.

### Prompt commands

A prompt command puts text in the message box. Type `/` as the first character
of the box and the list shows the commands of every enabled plugin, each with a
quiet "from acme". The arrow keys move, Enter or Tab picks, Esc closes. Picking
a command replaces the `/word` you typed with the command's file, exactly as it
is written, and puts the caret after it. It never sends anything: you read the
text, edit it and press send yourself. The command palette lists the
same commands as "Insert command: /review", which insert into the message box
you last used.

```json
{
  "id": "acme-demo",
  "name": "Acme demo",
  "version": "1.0.0",
  "api": 1,
  "engines": { "smind": ">=0.1.0" },
  "contributes": {
    "commands": [
      { "name": "review", "description": "Review the staged diff", "prompt": "commands/review.md" },
      { "name": "plan", "description": "Plan before coding", "prompt": "commands/plan.md" }
    ]
  }
}
```

with `commands/review.md` holding `Review the staged diff. List bugs first, then
risks, then style nits.` Every command can be typed as `/acme-demo:review`. The
short form `/review` goes to a built-in command with that name if there is one,
otherwise to the first plugin, by id, that has a command called `review`; the
others keep only the long form, and `smind plugin show <id> --json` lists the
short name under `conflicts`. With no plugin commands the message box behaves
as it always did. A command file is only text: it starts nothing and gets no
access to anything. A command that asks a plugin process for its text is
described under [Plugin RPCs](#plugin-rpcs).

### Themes

A theme file is JSON with a `light` and/or a `dark` object. Each object must
set **every** color token of the web UI, 115 in all, and nothing else:

```json
{
  "dark": {
    "--background": "rgb(26 22 37)",
    "--foreground": "rgb(217 212 232)",
    "--card": "rgb(39 32 54)"
  }
}
```

(That is the shape, not a working theme: all the tokens are required.) You do
not need the list up front. Install a plugin whose scheme is `{}`, such as
`{ "dark": {} }`, and `smind plugin install` rejects it and lists every token
that scheme is missing; install and update reject any incomplete scheme the same
way, so a plugin never leaves part of the interface unstyled. A value is a hex
color (`#` and 3, 4, 6 or 8 digits), `rgb()`, `rgba()`, `hsl()`, `hsla()` or `oklch()` with plain numbers, a
plain length such as `12px`, or `transparent`. Anything else (`url()`,
`var()`, `calc()`, names, quotes, semicolons, braces) rejects the plugin.

An enabled plugin's themes appear in Settings, Appearance, under "Plugin
theme", next to the plugin's name, on the web interface. A theme applies in the
schemes it declares and leaves the other alone: pick a dark-only theme and the
light theme stays the stock one. Choosing "Default" goes back, and so does
disabling or removing the plugin or turning the global setting off: the choice
is dropped and the stock theme returns.

### Settings and secrets

A manifest may declare typed `settings`:

```json
"settings": [
  { "key": "token", "type": "secret", "label": "API token", "required": true },
  { "key": "region", "type": "enum", "label": "Region", "options": ["eu", "us"], "default": "eu" },
  { "key": "retries", "type": "number", "label": "Retries", "default": 3 }
]
```

The types are `string`, `secret`, `boolean`, `number` and `enum` (with
`options`). `string` and `secret` are at most 1024 bytes unless `maxLength` says
up to 8192. A `secret` cannot have a default: a manifest is not a place for one.

You set values with the `plugin.settings.set` request (`{"id": "acme", "values":
{"token": "..."}}`; a `null` clears a key back to its default); `plugin.settings.get`
lists the settings and which are set. There is no settings command or page yet.
Setting values is a human action with the same limits as `enable`: loopback only,
or a network login with `plugins.allowRemoteManage`; never an agent or a phone.
A phone may read: it sees each secret as `set: true` and nothing else, and so does
every other reader, you included. A value is validated against its declared type;
an error names the setting, never the value, and a patch with one bad key changes
nothing.

Values are stored in smind's database the way provider credentials and MCP
environment values are, not encrypted separately. A value goes to exactly two
places: the plugin's own `plugin.initialize` (see below) and `${setting.NAME}`
in the plugin's MCP server `env` or `headers`, and nowhere else. It never
appears in `plugin list`, `plugin show`, events, errors or logs; a plugin's
stderr is scrubbed of the secret values of the plugins you have set (values
shorter than 8 characters cannot be scrubbed by match, so use long tokens).
Removing a plugin deletes its values, even with `--keep-data`.

Changing a setting restarts the plugin's process, so it starts with the new
values. A server that uses a placeholder whose setting has no value is listed
but not given to runs; `smind plugin show <id> --json` says which setting is
missing.

## Plugin processes

A manifest may declare `"process": { "command": ["node", "server.js"] }`. While
the plugin is enabled and `plugins.enabled` is on, smind starts that command as
you, in the plugin's own directory, and keeps it running. It is a program you
chose to run: the process can read your files and open network connections, and
smind shows its command before you enable the plugin.

- **No shell.** The command is an argument list. A first entry with a slash
  (`./bin/server`) is a program inside the plugin; a bare name (`node`) is looked
  up on a fixed system `PATH` (`/usr/local/bin`, `/opt/homebrew/bin`,
  `/usr/bin`, `/bin`, `/usr/sbin`, `/sbin` on macOS and Linux), not on yours.
- **A clean environment.** The process does not inherit smind's environment: no
  `SMIND_*` value, no provider key, nothing from your shell. It gets that `PATH`,
  `HOME` set to the plugin's `data/` directory (and the temp directory inside
  it), and your locale (`LANG`, `LC_*`) and `TZ`. This hides smind's secrets from
  the environment only: a same-user process can still read files such as
  `~/.smind/token`.
- **Handshake.** smind sends `plugin.initialize` with `api`, `smindVersion`,
  `dataDir` and `settings` (your values with secrets in the clear, the defaults for
  what is unset) on the process's stdin, in `Content-Length`-framed JSON-RPC 2.0
  (the framing language servers use), and the plugin has 10 seconds to answer
  it. Hooks are described under [Hooks](#hooks).
- **Limits.** A frame body is at most 1 MiB and a header line 1 KiB. A malformed
  or oversize frame closes the connection and kills the process; it counts as a
  crash.
- **Crashes.** A process that exits is restarted after 1 s, 2 s, 4 s, ... up to
  30 s. Five crashes within ten minutes mark the plugin **failed**: it stays
  enabled but is not restarted, `smind plugin show` and the Plugins page show
  why, and `smind plugin reload <id>` (or disable and enable) tries again.
- **Stopping.** Disable, remove, reload, an applied update, switching
  `plugins.enabled` off and stopping the daemon send `plugin.shutdown`, then
  SIGTERM to the process's whole group, wait 5 seconds, then kill the group
  (a job object on Windows), so nothing the plugin started is left behind. If
  the daemon itself is killed (SIGKILL, power loss) a Unix plugin process is not
  stopped by smind; on Windows the job object ends it.
- **Logs.** `smind plugin logs <id>` prints what the process wrote to stderr
  plus smind's own `[smind]` lines (started, ready, crashed, restarting). The log
  is kept in memory, at most 1000 lines or 256 KiB, each line cut at 2 KiB;
  control characters are removed and secrets are redacted before a line is
  stored. It is gone when the daemon restarts or the plugin is removed.

### What a plugin may call

A plugin process can send requests back to smind. There is one table of what it
may call, grouped by the `capabilities` its manifest declares and you confirmed:

| Capability | Methods |
|---|---|
| `tasks.read` | `task.list`, `task.get`, `task.tree`, `run.list`, `chat.list`, `workspace.list`, `space.list`, `space.get` |
| `mcp.read` | `mcp.list`, `mcp.get` |
| `profiles.read` | `profile.list`, `profile.get` |
| `events.observe` | `events.subscribe`, `events.unsubscribe` |
| `tasks.write` | `task.send` (also needs `tasks.read`) |

Everything else is refused. A method in the table whose capability the plugin did
not declare fails with `forbidden` (code -32001) naming the capability; any other
method, including one smind adds later, fails with `method not found`. Whatever
the plugin declares, it can never call the plugin, provider, account, relay,
automation or settings methods, anything that writes your tasks, runs, MCP
servers or profiles, or anything that answers or changes a permission request.
Results are redacted copies: token shapes and home paths are removed and a task's
setup output is left out. `files.read` and `ui.notify` can be declared but grant
nothing yet; `hooks.beforeRun` grants no call, it lets the `run.beforeStart` hook
run (see [Hooks](#hooks)).

This is mediation, not isolation: a process running as you can still read your
files, including smind's token.

**Events.** After `events.subscribe {"topics": [...]}` a plugin with
`events.observe` receives `event` notifications `{topic, seq, payload}`. Only
these topics are allowed: `task.status`, `task.created`, `task.updated`,
`task.archived`, `task.deleted`, `task.stalled`, `run.status`, `run.failover`,
`permission.pending`, `permission.resolved`, `workspace.created`,
`workspace.deleted`, `space.created`, `space.deleted`, `chat.created`,
`chat.updated`, `chat.archived` and `plugin.updated`. Delivery is live and best
effort: nothing is replayed, at most 256 events wait per plugin, and a plugin
that falls behind gets `event.dropped {count}`; one that stops reading never
slows smind or other subscribers (after 10 seconds without reading, the
connection is closed and the process counts as crashed). A plugin may observe
that a permission request is pending, with its ids and option names, but not its
`summary`, and it has no way to answer it.

**`task.send`.** With `tasks.write` (and `tasks.read`: a plugin may only message a
task it could read) a plugin can queue one prompt: `task.send {taskId, chatId?,
prompt, whenBusy?, model?}`. `whenBusy` is `queue` (the default) or `reject`; the
chat must already have a provider. The message is stored with source
`plugin:<id>` and delivered to the agent with a `[message from plugin <id>]`
header, so the agent and you can tell it from your own text. The send cannot set
the provider, the permission mode, auto-accept or a profile (those params are
ignored), cannot create a task (so `orchestration.maxDepth` cannot be exceeded),
and the run it starts is under the per-tree concurrency guard and the tree budget.
It is limited to 6 sends a minute and 60 an hour per plugin and 20 an hour per
task tree, and a plugin whose own messages started the last 3 runs of a chat may
not send into it again until something else has run there: that is what stops a
`run.finished` hook that answers every run with a send. A refusal is an error to
the plugin (code -32003, saying which limit and when it frees up) and shows as the
plugin's error in `smind plugin show` and the Plugins page.

### Hooks

A plugin with a process can subscribe to two events in `contributes.hooks`; smind
tells it with a request or notification on the same stdio connection, and no hook
can approve, deny or answer a permission request (smind has no auto-approval), or
rewrite a tool call.

- **`run.finished`** (observe only). `hook.run.finished` is a notification with
  `{runId, taskId, chatId, provider, model, origin, status, stopReason?,
  failureKind?, toolCalls}`: ids and states, never the prompt or the output, with
  secret shapes redacted. Delivery is best effort from a per-plugin queue (64
  events, the newest is dropped when it is full), so a slow plugin never delays
  smind. System runs (learning, probes) are not reported.
- **`run.beforeStart`** (needs the `hooks.beforeRun` capability). Just before an
  agent starts, smind sends `hook.run.beforeStart {runId, taskId, chatId,
  workspaceId, provider, model, permissionMode, origin}` and waits at most 10
  seconds. The answer may hold exactly three fields and smind checks every one:
  - `additionalContext` (string, at most 8 KiB): appended to the prompt the agent
    gets, in a block labeled `[plugin <id>]` ... `[end plugin <id>]`. The run's
    stored prompt stays the sender's text, and the run's timeline says how many
    bytes were added, never the text.
  - `env` (object of strings, at most 16 entries, values at most 4 KiB): added to
    the agent's environment. Names are upper case letters, digits and `_`; names
    smind, a provider or the loader use are refused (`SMIND_*`, `ANTHROPIC_*`,
    `CLAUDE*`, `OPENAI_*`, `GIT_*`, `LD_*`, `DYLD_*`, `PATH`, `HOME`, proxies and
    anything ending `_KEY`, `_TOKEN`, `_SECRET`, `_PASSWORD`), and a key that is
    already set is never overridden. Codex runs take no per-run environment.
  - `mcpServers` (list of names): servers this same plugin contributes, by manifest
    name or `<id>__<name>`. Any other name, another plugin's included, is refused.
  Any other field (`provider`, `cwd`, `model`, `profile`, `permissionMode`,
  `autoAccept`, a permission decision, a rewritten input, anything unknown) fails
  the hook with a typed error naming the field; nothing from a failed answer is
  applied. With several plugins, hooks run in plugin id order and then in the order
  of the manifest; instructions are appended in that order, the first plugin to set
  an env key keeps it, and nothing is merged deeper than that.

Each hook has `onError`: `open` (the default) runs the task anyway and shows the
failure on the plugin (`lastError`, the log and a note on the run), `closed` fails
the start naming the plugin and the reason. A plugin that is not running (crashed,
restarting, failed) or lacks the capability is skipped and the skip is shown; smind
never waits for it. A hook that answers with a JSON-RPC error is a failure, which is
how a `closed` hook stops a start. Names follow Claude Code's hook vocabulary where
they mean the same (`additionalContext`; `run.finished` is its `Stop`); there are no
exit codes, since a plugin answers over JSON-RPC.

## Plugin RPCs

A plugin process may declare methods a client can ask it to run, in
`contributes.rpc`:

```json
"contributes": {
  "rpc": [{ "name": "echoPrompt", "description": "Build a prompt", "timeoutMs": 5000 }],
  "commands": [{ "name": "hello", "description": "Insert a greeting", "rpc": "echoPrompt" }]
}
```

The wsapi method `plugin.call {pluginId, rpc, params}` sends `rpc.<name>` to the
process and returns its JSON result. Only names declared in `contributes.rpc`
are ever sent; anything else fails `unknown_rpc` without reaching the process.

- **Who can call.** You, in the web or desktop app or over a remote login, and a
  paired phone. Agents cannot, and one plugin cannot call another (a plugin's own
  calls to smind never include `plugin.*`). `plugins.allowRemoteManage` does not
  apply: it governs installing and enabling, not asking an enabled plugin to do
  what it declared.
- **Limits.** Params at most 64 KiB, a result at most 256 KiB (a bigger result is
  dropped and the call fails `call_too_large`), JSON only, 4 calls in flight per
  plugin (the fifth fails `plugin_busy` at once), 30 s per call. A manifest may
  lower the timeout with `timeoutMs` and can never raise it.
- **Errors** are typed and name the plugin: `unknown_rpc`, `plugin_unavailable`
  (off, disabled, not running, failed or gone during the call), `call_timeout`,
  `call_too_large`, `plugin_busy`, `bad_call`, and `plugin_error` (the process
  answered with an error; its text is cut to 300 characters and scrubbed of
  secrets). Each call is logged in `smind plugin logs <id>` with a scrubbed preview.
- **RPC-backed slash commands.** A command with `"rpc"` appears in the `/` list like
  a prompt command (web and phone), "from <plugin>". Picking it calls the RPC with
  `{"command": "<plugin>:<name>", "args": ""}`; the result must be
  `{"prompt": "<text>"}` (at most 16 KiB). The text is put in the message box as
  plain characters, in place of the `/word`, and is never sent; a failure shows
  under the list and leaves the box alone. The same name and collision rules as
  prompt commands apply. The command palette does not list RPC commands yet.

A small working plugin is in `plugin-examples/hello-process/` in the repository;
[Writing a plugin](plugins-authoring.md) walks through it.

## Further reading

- [Writing a plugin](plugins-authoring.md): the manifest reference, hooks, RPCs and a worked example.
- [Plugin system decision record](../docs/decisions/0042-plugin-system.md)
