---
title: Permission policy
description: Answer routine agent permission requests from a file you write, with a fixed floor that no rule can lift.
summary: "This page explains the standing permission policy in sMind by Spacing Mind: the smind policy commands, the policy file and how a request is decided, the built-in floor of always-denied commands with its rule ids, and notes for Windows. It is for people who want routine permission prompts answered from rules they wrote."
order: 22
---

# Permission policy

Off by default. When you turn it on, smind answers the routine permission
requests of your agents from a file you write, so you read only the requests
that need a person. No model is involved: the daemon compares each request with
your rules. Policy commands are for people only.

## Turn it on

1. Write the template: `smind policy init` creates
   `~/.smind/steward/policy.yaml` (under your smind home) with `version: 1`,
   `rules: []` and every example commented out. It never overwrites an existing
   file. With no active rule, the engine approves nothing.
2. Edit the file by hand. Nothing in smind, no agent and no relay session can
   write it.
3. Set `steward.policy.enabled: true` in `config.yaml`. The daemon rereads the
   file whenever it changes, with no restart.

## The commands

```sh
smind policy init
smind policy show
smind policy test "git push origin my-branch"
smind policy audit
```

- `smind policy show`: the engine switch, the state of the policy file and the
  rules in force.
- `smind policy test [--tool T] [--seat S] [--task ID] "<command>"`: explains
  what the engine would decide, command by command. It has no side effects, so
  use it to try a rule before relying on it.
- `smind policy audit [--task ID] [--decision D] [--limit N]`: recent decisions,
  newest first.
- `show`, `test` and `audit` take `--json` for the raw result.

## The file

Each rule has an `id`, an `action` (`allow`, `ask` or `deny`), a `why` of at
most 160 characters and a `match`. A `match` can name the `tool` (for example
`Bash`), the `seats` it applies to (for example `[lead]`), a `command` (`argv0`,
an optional `subcommand`, and `args` with `allow_only` or `deny_flags`) and
`paths.within`, the roots a request's paths must stay inside. The placeholders
`{worktree}` and `{taskBranch}` stand for the task's worktree and branch.

```yaml
version: 1
rules:
  - id: go-test
    action: allow
    why: "Run Go tests inside the task worktree"
    match:
      tool: Bash
      command:
        argv0: go
        subcommand: test
        args: {deny_flags: ["-exec", "-toolexec", "-overlay", "-modfile", "-C"]}
      paths: {within: ["{worktree}"]}
```

An `allow` for Bash must match on `argv0`, and an `allow` for a tool that
changes files needs `paths.within`. Wildcards are not accepted: v1 matches exact
names. The file is checked as a whole; an invalid file is reported by
`smind policy show` with the field and reason.

## How a request is decided

- A request nothing allows is **asked**, exactly as without the policy. If nobody
  answers within 5 minutes it is denied.
- `deny` beats `ask`, and `ask` beats `allow`, whatever the order in the file.
- A command is split into its simple commands. An `allow` needs a rule for every
  one of them. Anything smind cannot parse with certainty (an unbalanced quote,
  `eval`, `bash -c "$x"`, process substitution) is asked.
- Paths are resolved through symlinks before they are compared with a rule's
  `paths.within`.
- If the file is invalid, smind applies only the floor and asks about everything
  else.

Every decision is recorded in an audit log that you can read with
`smind policy audit` and that smind keeps for 90 days. Commands are shown with
secrets masked and heredoc bodies left out.

## The floor

The floor is compiled in. No rule in your file can override it, and a floor hit
is a **deny** that names the rule id:

| Id | What it stops |
|---|---|
| `no-stash` | `git stash`, which hides work from the task branch |
| `no-force-push` | force pushes and ref deletions, which rewrite shared history |
| `no-merge` | merging, tagging and releasing, which belong to you |
| `no-install-state` | touching the install state, credentials or the daemon |
| `no-secrets` | secret and environment dumps reaching the transcript |
| `no-destructive-outside` | destructive commands outside the task worktree |
| `no-curl-sh` | running downloaded code unreviewed (`curl … \| sh`) |
| `no-real-home-tests` | tests that point `HOME` at your real home |
| `no-add-all` | `git add -A`: stage named files, never the whole tree |

The floor is checked on every part of a command, even when another part could
not be parsed, so a deny is never softened to an ask.

## On Windows

The engine works on Windows with the same rules. Paths may use drive letters (any
case), UNC names, and `\` or `/`. Comparison is case-insensitive, and links and
junctions are resolved before comparing. `%USERPROFILE%`, `~` and `$HOME` all
mean your home. The floor also covers `C:\Windows`, `Program Files`,
`ProgramData` and `AppData`, and the PowerShell and cmd equivalents of `rm`,
`copy` and `move`, `Get-Content` of secrets, `Get-ChildItem Env:`, and
`iwr … | iex`.

A command passed to `powershell -Command`, `pwsh -c` or `cmd /c` is read in
its own syntax and split like `sh -c`, and each inner command is checked like
any other:

- the floor and your `deny` and `ask` rules apply to the inner commands, so a
  destructive inner command is denied;
- the wrapper itself is never allowed, whatever your rules say about the inner
  commands, so the best outcome is an ask;
- what the engine cannot read with certainty is asked: `-EncodedCommand`,
  script blocks, escape characters, a command built from a variable, and the
  cmd keywords `if`, `for`, `call` and `start`.

Anything the engine cannot place on Windows (`C:foo`, `\\.\pipe`,
`file:stream`, trailing dots) is asked or denied, never allowed.

## What it does not change

A provider mode that already approves everything (`bypassPermissions`, or
`--auto-accept`) never sends requests to smind, so the policy cannot narrow it.
The policy applies to requests that reach smind's permission prompt. See
[Scripting and permissions](scripting-and-permissions.md) for the modes.

## Related

- [Scripting and permissions](scripting-and-permissions.md): permission modes and
  the short version of this page.
- [Inbox](inbox.md): the permission requests the policy asks about land here.
- [Configuration reference](configuration.md): `steward.policy.enabled`.
