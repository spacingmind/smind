# 0004: Editor tabs are scoped per task

## Status

Accepted

## Decision

The web UI's editor tabs (open files, diff views, terminals) are scoped
per **task**, not per workspace. Selecting a different task in the
sidebar switches the tab set to that task's; each task keeps its own
open-tab state. (Decided by the maintainer when resolving open question
1 of `docs/research/dual-mode-ui.md`.)

## Alternatives considered

- **Per-workspace tabs (Paseo's model).** Paseo keys open tabs on the
  workspace because its worktrees exist inside the workspace session
  lifecycle. Passed over: smind's model is different by definition —
  each *task* is its own git worktree with its own agent runs, its own
  base ref, and its own diff (`task.diff` is base→worktree). A file
  "open" in one task's worktree is not the same file as the same path
  in another task's worktree; sharing tab state across them would show
  stale contents or force cross-worktree resolution for no benefit.
- **Global tabs across workspaces.** Even further from smind's task
  isolation; same objection, stronger.

## Rationale

- Task = worktree is smind's unit of parallel agent work. Per-task tabs
  make the tab set mirror exactly what that task's agent sees and
  changes — the review surface (files + diff) belongs to the task whose
  work is being reviewed.
- The maintainer also finds Paseo's code-review-and-commit UX poor;
  task-scoped tabs are the foundation for a review surface that is
  deliberately *not* a copy of it (review loop design is tracked in
  `docs/research/dual-mode-ui.md` and follow-up plans).
- Matches the research recommendation's prerequisite (a `{kind, key}`
  tab registry): the key includes the task, so per-task scoping falls
  out of the registry naturally.
