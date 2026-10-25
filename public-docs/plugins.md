---
title: Plugins
description: Add MCP servers, skills, agent profiles, prompt commands and themes from a plugin directory, and see what a plugin does before you turn it on.
order: 12
---

# Plugins

Plugins are a preview. A plugin is a directory with a `smind-plugin.json`
file. Today a plugin can add five things: MCP servers, skills, agent profiles,
prompt commands and themes. Nothing else a manifest declares (hooks, providers,
panels, commands that call a plugin process) has an effect yet.

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
becomes read-only. The page has no process logs yet: it says "Process logs
arrive with plugin processes."

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
access to anything. Commands that call a plugin process are not available yet.

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

A manifest may declare `settings` and use `${setting.NAME}` in an MCP server's
`env` or `headers`, and nowhere else. Storing setting values is not available
yet, so a server that uses a placeholder is listed but is not given to runs;
`smind plugin show <id> --json` says which setting has no value.

## Further reading

- [Plugin system decision record](../docs/decisions/0042-plugin-system.md)
