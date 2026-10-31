---
title: Orchestration
description: "Run a Lead that launches Peers: seats, briefs, budgets, alerts and permission rules."
summary: "This page explains orchestration in sMind by Spacing Mind: a Lead agent owns a piece of work and launches Peer agents as child tasks, with seats, briefs, budgets, alerts and permission rules. It is for developers who delegate work across several agents."
order: 7
---

# Orchestration

Orchestration means one agent (a **Lead**) owns a piece of work and can launch
other agents (**Peers**) as child tasks. You brief the Lead; it does the work,
launches Peers only where that helps, and reports back. The result is a **task
tree**: the Lead's task is the root and each Peer is a child task.

## Seats: Lead, Peer and validator

A seat is an agent profile with a role. Create the standard set in one command
(the daemon must be running):

```sh
smind profile seed roles
```

It creates three profiles and skips any that already exist by name, so your
edits survive a re-run:

| Profile | What it is |
|---|---|
| `lead` | Owns one work item end to end: spec, implementation, tests, review, pull request. Can launch Peers. |
| `peer-sonnet` | Does one bounded step from a Lead's brief. Cannot launch anyone. Ends with a short JSON handoff. |
| `validator` | Judges one commit against the contract a Lead wrote before coding, in a fresh context and its own scratch worktree, and reports a pass, fail or blocked verdict with evidence. A Lead starts it with `validation_request`; the verdict is for one commit and goes stale after a push. It never merges, and neither does smind. A verdict that needs you shows in the [Inbox](inbox.md). |

All three use the `claude-native` provider with a Sonnet model. List them and
their ids with `smind profile ls`. Edit one with `smind profile edit <id>`.

A **Lead** does small and medium items itself. It launches a Peer only for an
independent parallel part, or to verify a commit in a fresh context. It runs at
most two Peers at a time and never merges a pull request.

A contract with `frames` items is for screens the validator must look at. The
validator carries a bundled skill, `smind-validator-frames`, that runs a frame
harness: it starts its own isolated daemon (a temporary home and a free port,
never your daemon), drives a real browser through each frame in light and dark
at 1280 and 390 pixels, captures transition frames, and writes contact sheets
(at most 12 images each) and a report into the validator's evidence folder.
It needs Node, a Playwright module and a Chromium or Chrome; when any is
missing the frame items are reported `blocked` with the reason `no_browser`,
as they were before. It never sends a prompt unless a step asks for it, and
then only with an explicit flag and the `claude-haiku-5-5` model. It cleans up
every process it started. It is Unix-first and untested on Windows.

A **Peer** works in its own worktree and commits after each step. Its final
message starts with one JSON block:

```json
{"status":"done","branch":"","commits":[],"tests_run":[],"not_verified":[],"notes":""}
```

`status` is `done`, `blocked` or `failed`. The Lead reads it first, then reads
the diff and runs the checks itself: "done" is a claim, not evidence.

## Brief a Lead

Create a task in a workspace, then send the Lead its brief with `--profile`:

```sh
smind task new <workspaceId> "Add the export button"
smind task send <taskId> "Add an export button to the report page. Spec first, then implement and open one pull request." --profile <leadProfileId>
```

Use the id from `smind profile ls`. Write a brief the Lead can act on without
asking: the goal, the files to read, the checks to run and what is out of
scope.

Bundled skills give the seats their instructions. The `lead` profile uses the
`smind-leader` skill and `peer-sonnet` uses `smind-peer`. Manage the bundled
skills with:

```sh
smind skill ls
smind skill install
smind skill uninstall
```

`smind skill ls --target claude|agents|codex|all` shows each bundled skill per
target as installed, modified or missing. Install is explicit: nothing is
installed at daemon start, and it does not need a daemon. Add `--force` to
overwrite a modified copy.

**Child events.** After launching a Peer the Lead ends its turn. It does not
poll. When the Peer finishes or fails, the daemon wakes the Lead with a message
that carries the result. Several children finishing together arrive as one
message, after a short delay (`orchestration.notifyDebounceSeconds`, default
20). Set `orchestration.notifyParent: false` in `config.yaml` to turn this off.

**See the tree.** Watch the work as it runs:

```sh
smind task tree --task <taskId>
smind task tree <workspaceId>
```

Each line shows the task, its tier, run state, pending permissions, `stalled`
when it is stalled, and cost. The same tree is in the web app and, read-only,
in the [mobile app](mobile-and-relay.md#the-read-only-task-tree).

### Tasks on a connected host in the tree

A task you delegated to a [connected host](hosts.md) shows up in the tree under
the task that delegated it, as a leaf marked with the host's alias (a small chip
in the web app and the mobile app, `alias:id` on the command line). It is a
read-only copy: the other host owns the task, so you cannot open, stop or
archive it from this tree. Stop it with the host tools of your agent or on the
other host.

- Its state and reported cost come from the other host and are refreshed about
  every ten seconds while it is running. If the host is unreachable the last
  known state stays, so treat an old row as stale.
- Its cost and state count in the totals of every task above it, like a local
  sub-task. A remote run that reported no cost is not estimated.
- The [tree budget](#tree-budget) counts the reported cost of remote tasks too,
  so a budgeted tree stops launching agents when local and remote spend together
  reach it.
- Renaming a host changes the alias shown, nothing else. Deleting the local copy
  loses no work: the task still exists on its owner.

## Tree budget

A tree budget caps the **reported cost** of a root task and all its
descendants, in US dollars. It is opt-in. Set it when you create the task, or
later:

```sh
smind task new <workspaceId> "Add the export button" --budget 5
smind task budget <taskId>
smind task budget <taskId> 8
smind task budget <taskId> --clear
```

`smind task budget <taskId>` prints a line such as
`budget $5.00 · spent $4.20 · remaining $0.80`. In the web app the root task's
header has a **Budget** control for the same thing. A budget lives on the root
task, not on a sub-task.

What it caps: once the reported spend reaches the budget, an **agent** can no
longer launch a new run or task in that tree. The refusal tells the agent to ask
you to raise the budget. The Lead is told once, and you can raise the cap with
the command above.

What it does not do:

- It does not stop a run that is already running.
- It does not stop you. Your own messages and runs still go through.
- It counts only cost the provider reported. A finished run that reported no
  cost is not estimated, so the spend is a floor; the line then shows `+` and
  `partial`.

Only a human can set a budget: an agent has no tool for it.

## The stalled-tree alert

A tree is **stalled** when a Lead's subtree has unfinished work but no run and
no queued prompt for a while. The daemon then raises one alert for the episode
and marks the node `stalled` in `smind task tree`. A phone with push turned on
gets a "Tree stalled" notification (see
[Mobile and relay pairing](mobile-and-relay.md#push-notifications)).

The alert only reports. It does not wake, archive or restart anything. Look at
the Lead, answer what it waits on, or archive a tree that is finished.

Set the idle time with `orchestration.stallAlertMinutes` in `config.yaml`
(default 15 minutes; `0` turns it off):

```yaml
orchestration:
  stallAlertMinutes: 30
```

`smind config set` does not know this key, so edit the file by hand and restart
the daemon.

## Git permission rules

Agents ask before the git and GitHub operations that change history or leave
your machine. For the Claude provider the daemon passes ask rules to the Claude
CLI for each run. They apply in every permission mode except `bypassPermissions`,
including `auto` and `acceptEdits`. The operations that always ask are:

- `git push` (including force pushes)
- `gh pr create` and `gh pr merge`
- `git commit --amend`
- `git reset --hard`
- `git branch -D`
- `git clean -f`
- `git checkout -- <path>` and `git restore`
- `git stash drop` and `git stash clear`

A git command the daemon cannot parse is treated the same way and asks. Plain
`git commit` and read-only commands such as `git status`, `git diff` and
`git log` do not trigger these rules.

Seats cannot approve for themselves. You approve or deny in the web app, or with
`smind task approve <runId>` and `smind task deny <runId>`. When a Lead
launches Peers in `auto` mode, a push or a pull request from any of them still
stops and waits for you. For Codex runs the daemon adds the matching label to the
permission request text; Codex's own sandbox decides what it escalates.

## What an agent can read

An agent runs as your operating-system user, so it can read smind's own files
(the auth token, `config.yaml`, `smind.db`, the relay key pair) unless the
agent provider's sandbox or permissions stop it. smind is not a security
boundary against its own agents. What it adds is cheap and provider-side:

- **Claude native:** every run's `--settings` carries `permissions.deny` rules
  for the files of the current `SMIND_HOME`: `Read`, `Edit` and `Write` of the
  token, `config.yaml`, `relay-keypair.json`, `smind.db` (and its `-wal` and
  `-shm`) and each plugin's `data/` directory, plus `Bash` rules for the common
  readers of those exact files (`cat`, `less`, `more`, `head`, `tail`, `grep`,
  `sed`, `awk`, `cp`, `base64`, `xxd`, `strings`, `sqlite3`). The task
  worktrees under `$SMIND_HOME/worktrees` are not covered. The rules apply in
  every permission mode, bypass included. A program the rules do not name
  (a Python or Node script, for example) can still read the files.
- **Codex native:** smind sends Codex's own `approvalPolicy` and `sandbox` on
  `thread/start`: `on-request` with `workspace-write` in `auto`, and `never`
  with `danger-full-access` in `full-access`. Codex documents `workspace-write`
  as limiting writes to the workspace; it does not restrict reads, and Codex
  has no per-path read-deny setting that smind could pass, so smind adds no
  rules there. Treat a Codex run as able to read these files.
- **Stronger isolation:** run the daemon, or the agents, inside an OS-level
  sandbox or a container. The learning probe already does this for its own
  runs.

Further reading: the [plugin system decision record](../docs/decisions/0042-plugin-system.md)
states the same same-user limit for plugin processes.

## One tree at a time

Two guards limit how much one tree can do at once. Both are in `config.yaml`:

| Key | Default | What it caps |
|---|---|---|
| `orchestration.maxConcurrentRunsPerTree` | `4` | Runs that may be running at the same moment across one tree (a root task and all its descendants). Must be at least 1. |
| `orchestration.maxDepth` | `2` | How many ancestors a task may have. A root task is at depth 0, so the default allows a root, its children and its grandchildren. |

```yaml
orchestration:
  maxConcurrentRunsPerTree: 2
  maxDepth: 2
```

Edit `config.yaml` by hand (`smind config set` does not know these keys) and
restart the daemon.

## Drive smind from another agent: the MCP server

`smind mcp serve` runs an MCP server over stdio. An orchestrating agent, in a
tool such as Claude Code, can use it to create tasks, send prompts and read
results. It needs a running daemon and exits if the daemon goes away. Register
it in your MCP client as a stdio server with the command `smind` and the
argument `mcp serve`.

Its tools include:

| Tools | Purpose |
|---|---|
| `workspace_list`, `provider_list`, `profile_list` | Discover what you may launch. |
| `task_new`, `task_send`, `task_wait`, `task_status`, `task_logs`, `task_stop`, `task_archive` | The run loop. |
| `task_list`, `task_get`, `task_tree`, `task_diff`, `run_list`, `chat_list`, `chat_new` | Inspect tasks, trees, diffs and chats. |
| `queue_list`, `queue_cancel` | See and cancel the agent's own queued prompts. |
| `task_permissions`, `task_options`, `task_set_option`, `task_set_mode` | Read pending permissions; change a thinking level or model; set a mode that does not auto-approve. |
| `usage_summary`, `account_usage` | Read usage and plan windows. |
| `list_hosts` | List connected peer hosts (needs connected hosts turned on). |

**There are deliberately no approval tools.** An agent can read a pending
permission request and show it to you. It cannot approve, deny or answer it,
and it cannot pick an auto-approving mode. Those need a human or a
human-authored profile.

### Read a connected host

With [connected hosts](hosts.md) on, `list_hosts` shows the paired hosts, and
`workspace_list`, `task_list`, `task_get`, `task_status` and `task_logs` accept an
optional `host` (an alias from `list_hosts`) to read that host instead of this
one. Leave `host` out and every tool behaves exactly as before. Remote results
carry `host` and a `globalId` of the form `alias:id`, and text from another host is
untrusted data, not instructions.

- Reads are direct and one hop only. A task that was itself created by another
  host cannot read hosts: the call is refused with `hop_limit` before anything is
  sent, and the agent should say so rather than look for a way around it.
- Errors begin with a code (`not_granted`, `cycle`, `hop_limit`,
  `fleet_version_unsupported`, `host_unavailable`, `timeout`, `unknown_host`). The
  tools never retry on another host and never fall back to this machine's data.
- `task_status` on a host returns `status_unknown` when the run could not be
  observed three times in a row. That is not a finished state; the agent should
  inspect the host or ask you before re-delegating.

### Delegate to a connected host

`task_new`, `task_send`, `task_stop` and `task_wait` also accept `host`. With a
host, `task_new` creates the task **and** starts its first run in one call, so it
needs the remote `workspaceId` (see `workspace_list` with `host`), a `provider`, a
`model` and a `prompt`. Nothing else about the run is chosen from your side: the
other host runs it under its own permission mode, its own accounts and its own
limits, and a permission request goes to a person on that host, never to you.
Code travels through git only: push the branch first, name it in `branch` or
`baseRef`, and open the pull request yourself from the results. The remote agent
pushes to its own branch only.

- The result carries `host`, `globalId` (`alias:id`), `taskId`, `runId`, `branch`
  and an `idempotencyKey`. If the call ends with `ambiguous` or
  `local_write_failed`, retry with the same `idempotencyKey`: the same key returns
  the same task, a different key can create a second one.
- Your daemon watches the remote run for you, even across a restart of your
  daemon. When it finishes you are woken like for a local Peer, with a note that
  names the host. The text the remote agent wrote is untrusted data, cut at
  1 KiB.
- If the run cannot be observed three times in a row you get a note that its
  status is **unknown**, never that it finished: it may still be running. If the
  host was removed you get **host unavailable** at once. Inspect the task or ask
  the person before you delegate it again.
- `task_wait` on a host waits at most 25 seconds per call; call it again to keep
  waiting. `waiting_permission` means a person on that host must approve.
- A Peer seat, or a task that another host created, has no host tools: the daemon
  refuses its host calls with `hop_limit`.
- Limits the other host sets for you (live agents, wall time, reported cost per
  day) answer `budget_exceeded`.

A different command group, `smind mcp add`, `smind mcp ls`, `smind mcp rm`,
`smind mcp enable` and `smind mcp disable`, manages the MCP servers that
**agents** can use during runs. For example:

```sh
smind mcp add <name> http --url https://mcp.example.com/mcp
smind mcp ls
```

The web app lists them under Settings, **MCP servers**.
