---
title: Workspaces and tasks
description: Create workspaces and tasks, chat with agents, attach files, queue prompts and tidy up.
summary: "This page explains workspaces (registered git repositories) and tasks (one worktree and branch each) in sMind by Spacing Mind, plus chat, attachments, the prompt queue, spaces, pinning and archiving. It is for everyday users of the web app and CLI."
order: 4
---

# Workspaces and tasks

A **workspace** is a git repository you registered with smind. A **task** is
one unit of work inside it: smind gives each task its own git worktree and
branch, so agents never touch your main checkout. You talk to the agent in a
**chat** on the task.

## Workspaces

The repository must already exist and be a git repository.

In the web UI, click the **New workspace** (+) button in the sidebar header, enter the **Path** (or use **Browse…**), optionally a **Title**,
and click **Create workspace**. Without a title, smind uses the folder name.

From the command line:

```sh
smind workspace create ~/code/my-app my-app hard
smind workspace ls
```

`workspace create` takes the repository path, a name and a policy word. It
prints the new workspace id. You need the id for the other commands.

A workspace can also run setup and teardown scripts in each task worktree. See
`smind workspace setup show|set|clear` in the [CLI reference](cli-reference.md),
or **Worktree settings** in the workspace's row menu in the sidebar.

To see whether a repository is set up for agents, run `smind workspace readiness [workspaceId]`
(or open **Worktree settings** and read **Agent readiness**). It reads the repo's files
only, and never runs any of its scripts or touches the network. It lists what is
there (agent instructions, a test and a lint command, a setup script, a
`.gitignore`, CI, docs, lockfiles) and what is missing, with a one-line hint for each
gap. In a monorepo it also looks one level into package folders (`apps/*`,
`packages/*`, `workspaces` entries) for test scripts and lockfiles. Type checking
alone does not count as a lint or format command. Warnings are advice; they never
block a task.

To remove a workspace, use **Delete workspace** in the same menu. This only
removes it from smind. Files on disk stay, and uncommitted work in task
worktrees is saved to each task's branch first.

## Tasks

Create a task in the web UI with **Add task** in the workspace's row menu (or
**New task** in the command palette). Give it a **Title**. The dialog also asks
where to start from:

- **Workspace HEAD**: a new branch off the workspace's default base.
- **Branch…**: pick a branch, then **New branch from it** or **Check it out**.
- **Pull request…**: build from a GitHub pull request.
- **Issue…**: start from a GitHub issue. Its title and link are put in the
  first prompt for you to edit.

The pull request and issue pickers need the GitHub CLI (`gh`) on the daemon's
machine.

From the command line:

```sh
smind task new 1 "Fix the login bug"
smind task new 1 "Try the new API" --base origin/main
smind task new 1 "Review the fix" --branch fix/login
smind task new 1 "Pick up the PR" --pr 42
smind task new 1 "Sub-task" --parent 3
smind task new 1 "Cheap experiment" --budget 5
smind task ls 1
```

| Flag | What it does |
|---|---|
| `--space <spaceId>` | Put the task in a [space](#spaces). |
| `--parent <parentTaskId>` | Make it a sub-task of another task. By default a sub-task branches from the parent's branch (see [Git and pull requests](git-and-prs.md#stacked-sub-tasks)). |
| `--base <ref>` | Fork a new branch at this ref. Use `origin/<name>` to start from the remote branch; the daemon fetches it first. |
| `--branch <existingBranch>` | Check out an existing branch with no new branch. |
| `--pr <number>` | Build from a GitHub pull request. |
| `--budget <usd>` | Cap the reported cost of the task and its sub-tasks. Root tasks only. |

Use at most one of `--base`, `--branch` and `--pr`. `smind task new` prints the
task id, its branch and its worktree path. `smind task ls` lists live tasks;
add `--archived` or `--all` for archived ones, or `--json` for scripts.

To see the hierarchy with run state and cost, run `smind task tree <workspaceId>`.

## Chat

Each task starts with one chat. In the web UI, type in the composer at the
bottom of the task and press Enter to send. The composer lets you pick the
provider, model, thinking level and permission mode for the run. **Esc**
stops the running turn.

A task can have more than one chat. Use **New chat** in the tab bar to start
another, so side questions do not clutter the main thread. Archiving a chat
keeps its history on the task.

From the command line:

```sh
smind task send 1 claude-native "Add a unit test for the parser"
smind task send 1 claude-native "Now fix the failures" --chat <chatId>
smind task runs 1
smind task logs <runId> --tail 50
```

`task send` starts a run and streams its output. Press Ctrl+C to detach; the
run keeps going. Add `--detach` to start the run and return right away. Other
useful flags are `--mode <permissionMode>`, `--auto-accept`, `--model <model>`
and `--thinking off|standard|extended`. The provider names are the ones listed
under Settings → Providers. See [Providers](providers.md).

## Attachments

The composer takes images, files and references.

- **Attach** button (paperclip), then **Image or file…**: choose files from
  disk. You can also drag files onto the task pane ("Drop to attach") or paste
  a screenshot.
- **Attach** → **Mention a file**, or type `@` in the composer: pick a file in
  the task's worktree. The agent gets the path, not a copy.
- **Attach** → **GitHub pull request…** and **GitHub issue…**: search, or
  paste a number or URL. These entries need `gh`; without it they show as
  disabled with the reason.

Each attachment shows as a pill above the text box. The composer marks a pill
that breaks a limit, and Send stays disabled until you remove it. The daemon's
default limits:

- Up to 10 attachments per message.
- Images: PNG, JPEG, GIF or WebP, up to 5 MB each.
- Files: up to 10 MB each.
- 10 MB in total per message.

When you re-run a message, its earlier attachments go along.

## Queue and when-busy

A chat runs one turn at a time. When you send a prompt while a run is active,
the **when-busy** mode decides what happens:

| Mode | Behavior |
|---|---|
| `reject` | The send fails. This is the default for `smind task send`. |
| `queue` | The prompt waits and runs when the current run finishes, in order. |
| `interrupt` | The prompt goes first and stops the running turn. |

```sh
smind task send 1 claude-native "Also update the docs" --when-busy queue
smind task queue ls --chat <chatId>
smind task queue cancel <itemId>
```

In the web UI, the send button becomes **Queue** while a run is active, and
Enter queues the prompt. **Send now** (⌘/Ctrl+Enter) interrupts the running
turn and sends. Queued prompts are listed above the composer; cancel one from
its row.

Right after you create a task, its workspace setup script may still be running.
`smind task send` then queues the prompt for you and starts it when setup
finishes.

## Continue a failed run

A run can stop before it is done: at the output limit, at the tool-turn limit,
at the budget limit, or when the daemon restarted under it. The run footer
says so ("Stopped at the output limit", "Hit the tool-turn limit", "Stopped at
the budget limit", "Interrupted when the daemon restarted") and shows a
**Continue** button on the chat's latest run.

**Continue** sends the prompt "Continue" with the run's own provider, model
and thinking level. It is queued if the chat is busy.

## Spaces

A space groups tasks inside a workspace. It is not a separate repository.

```sh
smind space create 1 "Release 2"
smind space ls 1
smind task new 1 "Write changelog" --space 1
```

In the web UI, use **Add space** in the workspace's row menu. Move a task with
**Move to space** in its menu; **Ungrouped** takes it out of a space. **Delete
space** keeps its tasks and makes them top-level.

## Pin and archive

**Pin** puts a task in the **Pinned** section at the top of the sidebar. Use
**Pin** or **Unpin** in the task's menu. Pins are stored in your browser, so
they do not follow you to another device. A pin is dropped when the task is
archived.

**Archive** removes the task's worktree and hides the task. In the web UI use
**Archive task** in the task's menu. From the command line:

```sh
smind task archive 3
smind task archive 3 --delete-branch
smind task archive 3 --delete-branch --force
```

Before it archives, smind lists what is at risk (the CLI prints the same
lines as the web dialog):

- Uncommitted changes. They are saved to the branch first.
- Commits that are not pushed, or only on this machine because the branch was
  never pushed.
- A branch that is not merged anywhere.
- Files git ignores (such as `node_modules` and `.env`). They are not kept.
- The workspace's teardown script, if there is one, runs first.

Archiving does not delete the branch unless you ask. `--delete-branch` (or the
**Delete branch** checkbox) removes the local branch when it is merged, clean
and checked out nowhere else. `--force` (the **Delete the branch anyway**
option) also deletes a branch that is not merged or has uncommitted changes. A
branch that smind did not create is never deleted, and branches on the remote
are never touched.

If the task has sub-tasks, the web dialog offers **Archive parent only** and
lists the children. To bring an archived task back, see
[Archive and restore](git-and-prs.md#archive-and-restore).

### Scratch directories

Every agent run gets a private scratch directory,
`$SMIND_HOME/scratch/<task id>/`, and smind points `TMPDIR` (and `TMP` and
`TEMP` on Windows) at it. Anything the agent creates with `mktemp -d`, such as
a temporary `HOME` for a hermetic test run, then lands there and belongs to
that task. If you pin `TMPDIR` in a provider's env, your value wins.

Archiving or deleting a task removes its scratch directory. smind never
follows a symlink out of it, and a task with a running run is left alone. At
startup, and every ten minutes after, smind also removes scratch directories
of tasks that are archived, deleted or unknown.

```sh
smind scratch usage     # total size, and how much belongs to closed tasks
smind scratch clean     # remove the scratch of archived tasks now
```

Settings -> General shows the same numbers with a **Clean archived scratch**
button. See `agents.sharedCaches` in [Configuration](configuration.md) for the
shared build caches.
