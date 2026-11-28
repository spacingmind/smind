# 0063: Workspaces without a git worktree

## Status

**Proposed. Not accepted. No code.** Written 2026-11-28 for the Human to decide.

The direction is **already decided** by the Human (2026-11-28): **a folder does not need to be a git repository.** A folder without git is a valid project. This record does not argue that again. It lays out the choices that are still open, with options, trade-offs and a recommendation for each. The last section lists what the Human has to answer.

Builds on [ADR-0039](0039-git-pr-state.md) (git and PR state per task), [ADR-0044](0044-automations.md), [ADR-0056](0056-human-only-rpc-principal.md) and [ADR-0061](0061-browser-for-agents.md) (the browser switch per workspace). It supersedes one earlier answer of the create-project plan (see [choice 7](#7-create-project-and-git-init)).

## Context

**Today the daemon refuses a folder without git.** Every claim below was read in the code on `develop` at this date.

- `workspace.create` checks for a `.git` entry and refuses the folder with "is not a git repository … tasks are created as git worktrees branched off it; run git init there first" (`internal/workspace/workspace.go:244-254`).
- Every task is a git worktree on its own branch. `CreateTask` runs `git worktree add` (`internal/workspace/task.go:160-177`, `internal/workspace/git.go:44-56`), under smind's own directory, not inside your folder (`internal/workspace/task.go:65-77`). The agent's working directory is that worktree (`internal/taskrunner/runner.go:481`).
- What a person sees about a task is built from git: the changes list and the diff (`taskDiff`, `taskChangedFiles`, `internal/workspace/git.go:261-330`, which snapshot the worktree into a throwaway index), the commit action (`gitTaskCommit`, `git.go:389`), the PR preview and push (`PRPreview`, `PushForPR`, `internal/wsapi/pr_handlers.go:221,389`), the branch list (`internal/workspace/branches.go`), the git state of a task (`internal/gitstate`, ADR-0039), and the per-run record of the head and dirty state (`internal/workspace/provenance.go:14,25`, `internal/runs/provenance.go`). Archiving a task commits a checkpoint, removes the worktree and deletes the branch (`internal/workspace/delete.go:24-43`, `internal/workspace/archive_branch.go:303`).
- Saved permission grants are keyed by the workspace id and carry the folder they were given for (`internal/store/permission_grants.go:16,84,117`: `WorkspaceID`, `Folder`).
- The agent browser's switch is keyed by the workspace id, not by the repo (`internal/store/browser_settings.go:10-17`). No row means off.
- `fs.listDir` already tells the dialog which sub-folders are git repositories (`isGitRepo`, `internal/wsapi/hostfs.go:17`, `internal/hostfs/hostfs.go:113`). The Add project dialog shows the inline hint "Folders without git are not supported yet." for a folder that is not (`web/packages/ui/src/components/add-project/open-folder-step.tsx:92-94`). That hint states today's daemon limit; it does not say sMind needs git.
- **A precedent already exists for a task outside git.** System runs (learning probe, reflect, curate) work in an empty scratch folder with no git, and the run record just leaves the git fields empty (`internal/runs/provenance.go`, ADR-0026).

**What the change is.** Your workspace can be any folder. A git folder keeps working as it does now. A folder without git needs answers to seven questions, which are the seven choices below.

## What is decided and what is open

| | State |
|---|---|
| A folder does not need to be a git repo | **Decided** (Human, 2026-11-28) |
| A git folder keeps one worktree per task, as today | **Decided for now**: nothing here changes it. Whether worktrees become optional for git folders is **open** (choice 6). |
| How a task runs in a folder without git (1), parallel tasks (2), what replaces diff / branch / PR (3), permissions (4), the browser and other per-workspace features (5), create-project (7) | **Open**: each has a recommendation below. |

## The choices

### 1. How a task runs in a folder without git

- **A. In place.** The agent works directly in your folder. No copy, no wait, the files are where you expect them. Cost: changes are live, so a mistake is real unless choice 3 gives you an undo.
- **B. In an isolated copy.** Each task gets a copy under smind's directory. Safe, but a big folder (dependencies, build output, media) is slow and large to copy, there are no ignore rules to skip the junk (git's ignore file is what makes today's worktrees small), and you need a story for getting the changes back (copy back, or merge) that has none of git's help. Two tasks that touch the same file give a conflict nobody can resolve for you.
- **C. Copy only below a size limit, in place above it.** Two code paths to build and explain.

**Recommendation: A, in place.** The copy-back problem is the hard one and it has no good answer without git. Safety comes from choice 3 (a baseline and an undo), not from a copy.

### 2. Parallel tasks on one folder

In place, two agents writing the same folder can overwrite each other.

- **A. One writer at a time.** A task holds the folder while a run is active. A second task that wants to run waits, and says why: "Waiting for *task name*, which is using this folder." The person can open the other task or stop it.
- **B. Allow parallel runs and warn.** Fast, and it loses work quietly.
- **C. Copies for the second and later tasks.** Brings back all of choice 1B.

**Recommendation: A**, per run (not per task), so an idle task does not block the folder. A deliberate "run anyway" override can come later. A git folder is unaffected: it keeps one worktree per task, so tasks never share files.

### 3. What replaces branch, diff, PR and the turn record

With no git there are no branches, no commits, no pull requests and no head to record. Three options:

- **A. sMind keeps its own baseline.** Before the first run of a task, sMind snapshots the folder into its own directory (names and hashes for every file, contents for files under a size limit). The Changes tab then compares the folder with that baseline: added, changed and deleted files, a text diff for text files. "Undo this task's changes" restores the baseline after a confirm. Cost: disk space and time at the start of a task, so there must be limits (see decision 9). Files over the limit show as "changed" with no text diff and cannot be restored.
- **B. No tracking.** The Changes tab and undo do not exist for such a folder. Simplest, and the least safe in place.
- **C. Names only.** A manifest (names, sizes, hashes) with no contents: shows what changed, cannot show how, cannot undo.

**What hides for a folder without git, in every option:** branches, commit, push, pull request, ahead and behind, the fork guard, the git state of a task, "base branch". The run record's head and dirty fields stay empty, as for system runs today.

**Recommendation: A, in two steps.** The first slice ships the manifest (C) so the person sees what changed. A later slice adds the stored contents, the text diff and the undo. Until the undo exists, say plainly in the UI that changes in this folder cannot be undone.

### 4. Permissions and saved grants for a folder with no repo identity

Saved grants belong to the workspace id and name the folder they were given for. A folder without git has no repo to identify it, only its path.

- **Key by workspace id, as today.** Nothing changes in the grant table. The folder stays in each grant.
- **If the folder moves or is renamed:** the workspace shows "Folder not found". The person can **relink** it to the new path. Because each grant names the old folder, **grants do not follow a relink**: they stop matching and the person grants again. This is the cautious answer and it needs no new code beyond the relink action.
- **No escape from the folder.** The agent's working directory is the folder. The same permission policy applies. In place, a path that leaves the folder (a symlink to another place, `..`) is judged on its resolved real path. The folder itself is stored as a resolved real path at create time and checked again at the start of each run.
- **Folders sMind refuses to open without git:** the filesystem root, the person's home directory itself, and sMind's own data directory. An agent in place there can reach everything, and the baseline of choice 3 would snapshot a whole disk. A sub-folder of the home directory is fine.

**Recommendation:** all of the above.

### 5. The browser's per-workspace switch and other per-workspace features

What each feature is keyed on is read in the code (second column). The last column is the proposal:

| Feature | Keyed on | Folder without git |
|---|---|---|
| Agent browser switch and allowlist (ADR-0061) | workspace id (`store/browser_settings.go:10`) | works unchanged |
| Saved permission grants | workspace id + folder | works unchanged (choice 4) |
| Learning mode, routing policy | workspace id | works unchanged |
| Setup and teardown scripts, task ports | the task's worktree | run in the folder once per task start; no per-task port reservation is needed in place |
| Readiness check of tracked `.env` files | `git ls-files` (`workspace.TrackedEnvFiles`, `internal/wsapi/readiness_handler.go:40`) | the check is skipped (no tracked set exists) |
| Branches, PR, fork guard, git state, ahead and behind | the repo | hidden |
| Search and file listing | `git ls-files` (`internal/workspace/search.go`) | falls back to a plain directory walk that honours a size and count limit |

**Recommendation:** the workspace gets one stored field, `vcs` (`git` or `none`), set at create time and re-checked at the start of each run (an agent may run `git init` itself). The UI reads that field and hides what needs git. There is no per-feature switch to maintain.

### 6. A git folder keeps today's worktree behaviour

**Decided:** nothing in this record changes a git folder. It still gets one worktree and one branch per task.

**Open:** should a git folder be allowed to run tasks in place too (on its current branch, no worktree)? That is useful for a small repo and for people who do not want branches. It would be a per-workspace setting, `isolation: worktree | in-place`, default `worktree`. In-place on a git folder inherits the one-writer rule of choice 2 and the safety rules below.

**Recommendation:** not in the first slices. Ship non-git first, keep the default, and add the setting as its own slice if the Human wants it.

### 7. Create-project and `git init`

The create-project plan recorded an earlier answer for the **New project** method (C2): "mkdir + `git init` + an empty initial commit, ticked and locked with the reason; an existing non-git folder only gets an explicit set-it-up click". **That answer is superseded by this record.** `git init` becomes a choice, not a requirement.

- **New project:** a tick box "Track changes with git" for a new empty folder. **Recommendation: ticked by default, not locked.** Git gives a new project branches, diffs, undo and pull requests at no cost, so it is the better default for code. A person who unticks it gets a folder without git and the behaviour of choices 1 to 5.
- **An existing folder without git:** never `git init` automatically. A later "Set up git here" action in the project's settings, explicit and with a confirm.
- **Until the implementation lands:** the daemon still refuses a folder without `.git`. The Add project dialog says so with its inline hint, "Folders without git are not supported yet.", and offers no `git init` button. That sentence is the current limit, not a statement that sMind needs git. It is removed in slice 1.

## Security and safety

The daemon runs in a folder an agent can edit. Git hardening is in `internal/gitsafe` (PR #996): every daemon-side git call goes through it so a repo's own config cannot make git run a program. This change adds new things to guard:

- **Never delete your folder.** Today archiving a task removes its worktree (`internal/workspace/delete.go`, `git.go:97`). For a task that runs in place the "worktree" **is your folder**. A task must carry an `in_place` mark, and archive, delete and every cleanup path must refuse to remove it. This is the highest-risk rule of the whole change and it gets a test per cleanup path.
- **A folder inside a git repo.** A sub-folder of a repository has no `.git` of its own, so it counts as "no git". But a git command run there finds the parent repository and reports its state. A folder with `vcs: none` must run **no git at all**, and the daemon sets `GIT_CEILING_DIRECTORIES` for anything that still shells out. The result must not be a diff of the parent repo shown as the folder's changes.
- **The baseline store.** It holds copies of your files under sMind's directory: created with owner-only permissions, never followed through a symlink out of the folder, never storing special files, capped (decision 9). Files such as `.env` end up in it: it is as sensitive as the folder itself and is removed with the task.
- **Undo and restore** never follow a symlink out of the folder and never write outside it.
- **No new wire method for a caller class that had none.** The new fields and actions are workspace and task actions of the same human-only, local-only kind as the existing ones; an agent's connection gets nothing new.
- **A folder where the agent runs `git init` later** becomes a git folder at the next run (the re-check in choice 5). Its earlier baseline is kept until the task is archived.

## Migration and compatibility

There is no installed base, so **breaking changes are fine** and no compatibility shim is needed. The store gets one new workspace column (`vcs`, default `git`) and one new task mark (`in_place`). An existing workspace is a git workspace and behaves as before. No data is rewritten.

## Phased implementation (for the Human to approve)

Each slice is its own pull request to `develop`. Sizes are my estimate.

| Slice | What | Size | Risk |
|---|---|---|---|
| **1. Open a folder without git** | `vcs` field, `workspace.create` stops requiring `.git` (and refuses root, home, the data directory), tasks run in place, the one-writer lock with the visible reason, `in_place` mark and the never-delete rule, no git runs for `vcs: none`, the UI hides what needs git, the dialog hint is removed, `fs.listDir` and the dialog stay as they are | L | **High** (daemon, deletion safety): validation contract and a validator |
| **2. See what changed** | the manifest baseline (choice 3, first step): the Changes tab shows added, changed and deleted files | M | Medium |
| **3. Text diff and undo** | stored contents under the size limit, text diff, "Undo this task's changes" | M | High (it restores files): contract |
| **4. New project and "Set up git here"** | the optional `git init` tick box and the explicit set-up action; the create-project plan's C2 is amended | S | Low |
| **5. Relink a moved folder** | "Folder not found" and relink, grants stop matching | S | Medium (permissions) |
| **6. Optional in-place for git folders** | only if the Human wants choice 6's setting | M | Medium |

## Decisions needed from the Human

Each has my recommended answer.

1. **In place or in a copy?** Recommended: **in place** (choice 1A).
2. **Parallel tasks on one folder?** Recommended: **one writer at a time**, others wait and say why (choice 2A).
3. **How much change tracking for a folder without git?** Recommended: **a baseline of your own**, shipped in two steps: names first, then contents and undo; say in the UI that there is no undo until the second step (choice 3A).
4. **Folders sMind refuses without git?** Recommended: **the filesystem root, your home folder itself and sMind's own data folder.** Anything inside them is fine (choice 4).
5. **If a folder moves, do saved grants follow it?** Recommended: **no.** A relink keeps your tasks and asks you to grant again (choice 4).
6. **Should a git folder be allowed to run tasks in place?** Recommended: **not now.** Ship non-git first; add the per-workspace setting later if you want it (choice 6).
7. **Does New project run `git init`?** Recommended: **a tick box, ticked by default, never locked.** An existing folder is never turned into a git repo automatically (choice 7).
8. **A sub-folder of a git repo counts as "no git"?** Recommended: **yes**, and sMind runs no git in it, so it never shows the parent repository's changes (security section).
9. **The default size limits of the baseline** (per-file text limit, total size, file count). Recommended defaults: **1 MiB per text file for stored contents, 500 MiB and 50 000 files in total**, all configurable; above the total the baseline falls back to names and hashes only.
10. **Slice order.** Recommended: **1, 2, 4, 3, 5**, then 6 only if wanted.
