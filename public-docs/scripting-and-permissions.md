---
title: Scripting and permissions
description: Drive agents from the command line, choose permission modes, and connect other tools over MCP.
summary: "This page explains how to script sMind by Spacing Mind from the command line, choose permission modes for agents, and use the built-in MCP server. It is for developers automating agent work."
order: 15
---

# Scripting and permissions

Everything in the web UI also works from the `smind` command line. This page
covers scripts, permission modes, and the MCP server.

## Run an agent from a script

`smind task send` streams the run and waits for it to end. To start a run and
come back later, detach:

```sh
RUN=$(smind task send <taskId> glm "<prompt>" --auto-accept --detach)
smind task logs "$RUN" -f
smind task runs <taskId>
```

`--detach` prints just the run ID. `task logs -f` follows the run, and
`task attach` streams it again. Detaching never stops a run. Raw agent events
are hidden by default; add `--verbose` to `send`, `attach` or `logs` to see
them. A prompt that needs a human still prints in full.

## Choose where a task starts

By default a task branches from the workspace checkout's current `HEAD`, so a
stale local branch gives a stale base. Pick the start point yourself. Use at most one of these:

```sh
smind task new <workspaceId> "title" --base origin/develop
smind task new <workspaceId> "title" --branch some-branch
smind task new <workspaceId> "title" --pr 123
```

`--base` fetches the remote branch first, so your main checkout is never
touched. `--branch` checks out an existing branch. `--pr` builds the task from a
GitHub pull request.

## Permission modes

Each provider has its own modes:

| Provider | Modes |
| --- | --- |
| Claude Code | `acceptEdits` (default), `default`, `plan`, `auto`, `bypassPermissions` |
| Codex | `auto` (default), `full-access` |
| GLM and Kimi | the modes the agent offers, plus `--auto-accept` to approve every prompt |

```sh
smind task send <taskId> claude-native "<prompt>" --mode plan
smind task send <taskId> glm "<prompt>" --auto-accept
```

Anything the mode still asks about waits for a person. The web UI shows it, or
use the command line:

```sh
smind task permissions <runId>
smind task approve <runId>
smind task deny <runId>
```

To run Claude Code without anyone approving each command, pick `auto` and pin
the model, because an unpinned run uses the CLI's own default:

```sh
smind task send <taskId> claude-native "<prompt>" --model claude-sonnet-5-5 --mode auto --detach
```

`auto` lets Claude Code's own classifier approve prompts. It is not a sandbox:
an explicit instruction to do something destructive will probably be approved.
Keep briefs narrow, and let each agent work in its own task worktree.
`bypassPermissions` skips prompts with no classifier at all.

You can change a live run's mode instead of approving calls one by one:

```sh
smind task mode <runId> --auto-accept on
smind task mode <runId> acceptEdits
smind task mode <runId> --auto-accept off
```

Turning on auto-approval needs a person at a terminal: the command asks you
to type `yes`. This is a speed bump, not a lock. Every client of the daemon
holds the same token, so the real boundary against an agent that can run
shell commands as you is the provider's own permission mode or sandbox.

## A standing permission policy (optional)

> **Windows is not supported yet.** The policy engine's path rules assume POSIX paths. On Windows, `steward.policy.enabled: true` is ignored: the daemon logs one warning and keeps the engine off, so permission requests behave exactly as without a policy, and `smind policy` commands exit with "not supported on Windows yet". Use macOS or Linux for the policy engine; Windows support is planned.

Off by default. If you turn it on, smind can answer the routine permission
requests of your agents from a file you write, so you only read the requests
that need a person. No model is involved: the daemon compares each request with
your rules.

1. Create the file: `smind policy init` writes `~/.smind/steward/policy.yaml`
   with every example commented out and no active allow rule. It refuses to
   overwrite an existing file.
2. Add the rules you want. Edit the file by hand; nothing in smind, no agent and
   no relay session can write it.
3. Turn the engine on with `steward.policy.enabled: true` in `config.yaml`. The
   daemon reads the file again whenever it changes, with no restart.

How a request is decided:

- A request nothing allows is **asked**, exactly as without the policy. If
  nobody answers within 5 minutes it is denied.
- `deny` beats `ask`, and `ask` beats `allow`, whatever the order in the file.
- A command is split into its simple commands. An `allow` needs a rule for every
  one of them; anything smind cannot parse with certainty (an unbalanced quote,
  `eval`, `bash -c "$x"`, process substitution) is asked.
- Paths are resolved through symlinks before they are compared with a rule's
  `paths.within`.
- A short list of commands is always denied, whatever your file says: `git
  stash`, force pushes, merging or releasing, anything that touches your smind
  home or the daemon's port, killing processes, secret dumps, writes outside the
  task worktree, `curl | sh`, pointing a test's `HOME` at your real home, and
  `git add -A`. The denial names the rule.
- If the file is invalid, smind applies only that fixed list and asks about
  everything else.

Every decision is recorded in an audit log that you can read and that smind
keeps for 90 days. Commands are shown with secrets masked and heredoc bodies
left out.

```sh
smind policy show            # validity and the rules in force
smind policy test "git push origin my-branch"   # explain a decision, no side effects
smind policy audit           # recent decisions (--json for scripts)
```

### On Windows

The engine works on Windows with the same rules. Paths may use drive letters
(any case), UNC names, and `\` or `/`; comparison is case-insensitive, and links
and junctions are resolved before comparing. `%USERPROFILE%`, `~` and `$HOME`
are all your home. The always-denied list also covers `C:\Windows`, `Program
Files`, `ProgramData` and `AppData`, and the PowerShell and cmd equivalents of
`rm`, `copy` and `move`, `Get-Content` of secrets, `Get-ChildItem Env:`, and
`iwr … | iex`. A command string passed to `powershell -Command` or `cmd /c` is
never looked into, so it is always asked. Anything the engine cannot place on
Windows (`C:foo`, `\\.\pipe`, `file:stream`, trailing dots) is asked or denied,
never allowed.

This does not narrow a provider mode that already approves everything
(`bypassPermissions`, `--auto-accept`): those requests never reach smind. It is
a convenience for your own routine commands, not a sandbox.

## Connect other tools over MCP

`smind mcp serve` runs an MCP server on stdio, so an agent (Claude Code,
Cursor and others) can drive smind tasks without shelling out:

```json
{ "mcpServers": { "smind": { "command": "smind", "args": ["mcp", "serve"] } } }
```

The daemon must already be running. `task_send` returns a run ID at once, and
`task_wait` blocks until the run ends, a permission is pending, or its timeout
passes.

The server deliberately has no approval tools. An agent can show you a pending
permission, but only you approve it, in the web UI or with `smind task approve`.
For the same reason an agent cannot turn on auto-approval itself.

`smind mcp add`, `ls`, `rm`, `enable` and `disable` manage the MCP servers that
your agents use. See the [CLI reference](cli-reference.md).

## Set up worktree dependencies

Each task gets a fresh git worktree with no `node_modules`. A workspace setup
script runs once, in the background, when a task is created:

```sh
smind workspace setup set <workspaceId> --setup-file setup.sh
smind workspace setup show <workspaceId>
```

The script runs with `sh -c` and the daemon's `PATH`. While it runs the task
counts as busy, so a `smind task send` right after `task new` needs
`--when-busy queue`. The prompt is delivered when setup finishes.

## Further reading

- [Permission modes decision record](../docs/decisions/0019-provider-native-permission-modes.md)
- [MCP server decision record](../docs/decisions/0017-mcp-server.md)
