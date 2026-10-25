---
title: Plugins
description: Add MCP servers, skills and agent profiles from a plugin directory, and see what a plugin does before you turn it on.
order: 12
---

# Plugins

Plugins are a preview. A plugin is a directory with a `smind-plugin.json`
file. Today a plugin can add three things: MCP servers, skills and agent
profiles. Nothing else a manifest declares (commands, themes, hooks,
providers, panels) has an effect yet.

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

Installing, enabling, disabling, removing and updating a plugin are human
actions on the machine that runs the daemon. Agents and paired phones cannot
do them. A phone can still see the lists.

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
    "profiles": ["profiles/reviewer.json"]
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

### Settings and secrets

A manifest may declare `settings` and use `${setting.NAME}` in an MCP server's
`env` or `headers`, and nowhere else. Storing setting values is not available
yet, so a server that uses a placeholder is listed but is not given to runs;
`smind plugin show <id> --json` says which setting has no value.

## Further reading

- [Plugin system decision record](../docs/decisions/0042-plugin-system.md)
