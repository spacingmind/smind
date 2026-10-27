---
title: Git and pull requests
description: Branches, publishing, the Create PR dialog, the PR panel and stacked sub-tasks.
summary: "This page describes how sMind by Spacing Mind handles git for each task: branches, publishing, the Create PR dialog, the PR panel and stacked sub-tasks. It is for developers reviewing and shipping agent work."
order: 5
---

# Git and pull requests

Every task works on its own branch in its own git worktree. The Diff tab of a
task is where you review changes, commit, publish the branch and open a pull
request. Push, update, Create PR and the PR panel are in the web UI. Pull
request actions need the GitHub CLI (`gh`) on the machine that runs the
daemon, signed in. Open **Settings → GitHub** to check.

## Base ref

The **base** is the branch a task is measured against. The diff, the "ahead"
and "behind" counts and the pull request target all use it.

**Workspace default base.** smind detects it from the repository: the branch
`origin/HEAD` points to, else `develop`, `main` or `master` (whichever exists),
else the branch currently checked out. To pin one, open **Worktree settings**
in the workspace's row menu and fill **Default base branch**. Clear the field to
go back to auto-detect.

**Per-task base.** You choose it when you create the task, and it stays fixed:

- Web: the **Start from** control in the New task dialog. **Workspace HEAD**
  branches from the default base. **Branch…** lets you pick another branch
  (**New branch from it** or **Check it out**). **Pull request…** and **Issue…**
  start from GitHub.
- CLI: `smind task new <workspaceId> <title> --base <ref>` forks a new branch
  at `<ref>`. Use `origin/<name>` to start from the remote branch; the daemon
  fetches it first. `--branch <name>` checks out an existing branch, and
  `--pr <number>` builds from a pull request. Use only one of the three.

```sh
smind task new 1 "Add export" --base origin/develop
```

## Publish, push and update from base

Two buttons sit in the Diff tab header (below a narrow width they move into a
"…" menu, where each entry ends in "…"):

- **Publish branch** while the branch has never been pushed, then **Push**.
  The dialog shows the branch, the remote, how many commits are ahead and
  behind, and the next step. The push is a plain `git push`; smind never forces
  it. The remote is the branch's push remote, else `remote.pushDefault`, else
  `origin`. A task started from a pull request on a fork is read-only: it cannot
  be pushed.
- **Update from `<base>`** brings the base into the task branch. It fetches
  `origin/<base>` and merges it. Tick **Rebase instead of merge** to rebase
  instead. A rebased branch that is already published needs a manual
  `git push --force-with-lease`, because smind does not force-push.

If an update hits conflicts, the worktree is left in the middle of the merge
or rebase and the dialog lists the conflicting files. **Ask the agent to
resolve** sends them to the task's chat with instructions to finish the merge or
rebase and run the tests. smind never aborts a half-done merge for you.

A button that cannot run is disabled, and its tooltip says why.

## Create a pull request

When the task has no open pull request, the Diff tab header shows **Create PR…**.
It opens the **Create pull request** dialog:

- The branch line shows the task branch and the size of the diff. Below it, the
  base branch field is editable and offers your branches. It starts at the
  task's base. For a stacked sub-task it reads "Stacked on `<parent branch>`".
- **Title** starts as the task's title and **Description** as a summary of the
  commit subjects. Edit them freely.
- **Draft** is on by default. The choice is remembered per workspace, in your
  browser.
- The summary line shows the commit count. Uncommitted files are listed as "not
  included": commit first.

Click **Create PR**. smind pushes the branch and opens the pull request on
GitHub. If the task branch carries unrelated unpushed history from the
workspace checkout, smind pushes a clean branch with only the task's own
commits instead.

If something blocks the action (no `origin` remote, no commits beyond the base,
`gh` missing or not signed in), the dialog shows the reason, with a link to
the GitHub settings when that is the fix.

Once a pull request is open, the header shows **Update PR**, which pushes new
commits to the pull request's branch, and **View PR**, which opens it on
GitHub. A chip with the pull request number appears in the task header.

## The PR panel

The **Pull request** tab shows the task's pull request. It opens by itself when
the task gets one. It has:

- A header with the state (**Open**, **Draft**, **Merged**, **Closed**), a
  **Refresh** button and **Open on GitHub**.
- **Checks**: the status of each check, with a link to it on GitHub.
- **Reviews**: the review decision (**Approved**, **Changes requested** or
  **Review required**) and each review.
- **Threads**: review threads, marked **Resolved** or **Outdated**.

**Send to chat** on a failing check or a thread puts it in the task's chat, so
the agent can fix it. Until the task has a pull request, the tab says "No pull
request yet".

## Archive and restore

When a pull request is merged, the header offers **Archive…**. The dialog is
the same one as **Archive task** in the task menu: it lists what is at risk and
lets you delete the branch. See [Pin and archive](workspaces-and-tasks.md#pin-and-archive)
for the rules.

Archived tasks sit in the **Archived** row under their workspace in the
sidebar. Click **Restore** to bring one back. smind recreates the worktree from
the branch it kept (the local branch, else `origin/<branch>`) and runs the
workspace setup script again. If the branch is gone, **Restore** is disabled
with "The branch no longer exists, so there is nothing to restore."

## Stacked sub-tasks

A sub-task you create from a task branches from the parent's branch, so its
changes build on the parent's. This works for sub-tasks you create yourself,
in the web UI (**New sub-task…**) or with `--parent` on the command line:

```sh
smind task new 1 "Add the API" --parent 3
```

The New sub-task dialog has a **Base** control: **Parent branch (`<branch>`)**
or **Default base (`<base>`)** to branch from the workspace default instead. The
sub-task starts at the parent's last commit. Uncommitted work in the parent is
not included. If the parent has no branch yet, or it no longer exists, the
sub-task branches from the default base.

Sub-tasks that agents create while orchestrating start from the workspace
default base, not the parent's branch.

When you open a pull request for a stacked sub-task, the Create PR dialog
targets the parent's branch. If that branch has not been pushed yet, it falls
back to the default base and says "Parent branch not pushed yet".

## Draft a commit message with the agent

The Diff tab lists the task's changed files with a checkbox to stage each one.
Under the list is the commit bar: a **Commit message…** box and a **Commit (N
staged)** button.

**Draft with agent** asks the task's chat for a Conventional Commits message for
the staged changes. The reply appears in the chat as a normal turn, in a fenced
block. It does not fill the box and it does not commit; copy the message into
the box yourself. The button is disabled with a tooltip when nothing is staged
("Nothing staged") or the task has no chat to ask ("No agent chat to ask").

Like any other prompt, the request is queued when the chat is busy.
