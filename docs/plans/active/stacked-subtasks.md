# Child tasks branch from the parent (G18)

Size: M (5) — scope 2, PRs 1, risk 1, decisions 0, verify 1 (+ a test-only follow-up commit in `internal/stall`)

A **human-created** sub-task starts from its parent's branch (stacked work); its PR base defaults to the parent's branch while that branch
is pushed. Agent-created children are unchanged. Binding inputs: ADR-0039 (stored `base_ref`/`base_sha`, G1), ADR-0041 (orchestration).
Research (local-only, not committed): `git-github-ux.md` G18.

## Findings (investigation, 2026-10-25)

- **Role pack does not expect stacked branches; it expects the opposite.** `docs/WORKSPACE_PROTOCOL.md:68`: "PRs branch from
  `origin/develop`. **Do not stack PRs.** Squash merges make the PR on top conflict." The Lead role text (`internal/profiles/rolepack.go:59`):
  "When you accept a Peer's candidate and its commits are on the branch you will push, archive the Peer's task" — the Lead integrates Peer
  commits into its own branch; Peers do not build on the Lead's branch. `cmd/smind/mcp_instructions.go:42`: "task_new also takes baseRef
  (e.g. origin/develop)". ADR-0041 line 130: "Each Peer already has its own worktree and branch ... overlap shows up at merge." No mismatch with
  keeping agent-created children on today's base; **no agent behaviour changes** and no Human decision is needed for that.
- **There is no "New sub-task" UI today.** Web `CreateTaskDialog` (`crud-dialogs.tsx`) never sends `parentTaskId`; mobile has no parent
  param; the only human path to a sub-task is `smind task new --parent`. This item adds the web entry point ("New sub-task…" in the task
  menu) reusing `CreateTaskDialog`.
- Today `task.create` without `baseRef` forks off the main checkout's HEAD and stores the workspace default base (`computeTaskBase`).
  With `baseRef` = a local branch, `baseBranchOf` stores that branch name as `base_ref` — so a stacked task's `base_ref` *is* the parent's
  branch name, and G5's ahead/behind (`gitstate.Compute` → `compareRef(in.BaseRef)`) and the diff base already use the stored base.
- Create PR dialog already defaults its base to `git.base.ref` (the stored base) and `prTarget` falls back `baseBranch → t.BaseRef →
  workspace default`; so today a stacked task would target the parent branch even when it is unpushed or gone. That is what changes.
- `Base.Tip == ""` with `Base.Ref != ""` already means "the base branch resolves nowhere" (gone).

## Decisions (Supervisor defaults; the Human may veto)

1. Human callers only (non-run-scoped `task.create`): with `parentTaskId` and none of baseRef/branch/prNumber, the **daemon** defaults
   `baseRef` to the parent's branch when the parent has a branch that exists as a local branch; else today's behaviour (no baseRef).
   One place, so web/CLI/MCP-human agree. Run-scoped (agent) callers never get this default.
2. Web sends the choice explicitly as `baseRef`: "Parent branch" sends the parent's branch, "Default base" sends the workspace default base
   (`origin/<name>` when only the remote has it). No new RPC param.
3. CLI: `smind task new <ws> <title> --parent <id>` gets the daemon default; `--base` overrides; the base actually used (`task.BaseRef`) is
   printed to stderr.
4. Mobile: no sub-task creation exists; not touched.
5. PR base: the daemon computes `TaskGit.base.prDefault`: the stored base when it is not stacked; when `base_ref` equals the parent's
   branch, the parent's branch only if `origin/<parent>` exists, else the workspace default base. `prTarget` uses the same rule when
   `baseBranch` is empty. The dialog shows the default and a one-line "Stacked on <parent>" hint; switching to the default base is the
   existing base field.
6. Hover card: `GitBranchLine` already renders `branch → base`; add `(gone)` when the base is set but its tip is empty, and a "stacked"
   marker only through the label (`→ <parent branch>` is already the base text). Wire: `base.parentBranch` (set when base ref equals the
   parent task's branch), `base.gone` (bool), `base.prDefault`; all `omitempty`.
7. A child's `base_ref` is a plain string: archiving the parent or deleting its branch never rewrites it.

## Acceptance criteria

- AC1 A human sub-task defaults to the parent's branch (daemon; CLI; web dialog).
- AC2 An explicit base (`develop`, `--base`) wins over the parent default.
- AC3 A parent with no branch / unresolvable branch falls back to today's behaviour; the dialog says so.
- AC4 An agent-created child keeps the workspace default base (asserted).
- AC5 The child worktree starts at the parent branch tip: `git merge-base child parent` == parent tip at creation.
- AC6 A grandchild stacks on the child.
- AC7 Parent branch gone/archived: the child keeps `base_ref`; hover shows `→ <name> (gone)`; PR default falls back to the default base.
- AC8 PR default base = parent branch only when `base_ref` equals the parent's branch AND it is pushed; else the default base.
- AC9 The hover card (sidebar + header) shows `→ <parent branch>` for a stacked task.
- AC10 Follow-up: live descendants of an archived root never raise a stalled-tree alert.

## Test scenarios (named)

Go (`internal/workspace`, `internal/wsapi`, `internal/gitstate`, `cmd/smind`):
- `TestCreateTask_HumanSubtaskDefaultsToParentBranch` (wsapi, real git: base_ref == parent branch, merge-base == parent tip)  [AC1, AC5]
- `TestCreateTask_ExplicitBaseBeatsParentDefault` [AC2]
- `TestCreateTask_ParentWithoutBranchFallsBack` and `TestCreateTask_ParentBranchGoneFallsBack` [AC3]
- `TestCreateTask_AgentChildKeepsDefaultBase` (run-scoped caller; base_ref == workspace default, merge-base != parent tip) [AC4]
- `TestCreateTask_GrandchildStacksOnChild` [AC6]
- `TestTaskGit_StackedBaseGoneAndParentBranch` (gitstate: `parentBranch`, `gone`) [AC7, AC9]
- `TestPRBase_StackedParentPushedUsesParent`, `TestPRBase_StackedParentNotPushedFallsBack`, `TestPRBase_ParentBranchDeletedFallsBack`,
  `TestPRBase_NotStackedUnchanged` [AC7, AC8]
- `TestTaskNew_ParentDefaultPrintsBase` and `TestTaskNew_BaseOverridesParentDefault` (cmd/smind) [AC1, AC2]
- `TestLiveDescendantsOfArchivedRootNeverAlert` (internal/stall) [AC10]

Web (vitest): `crud-dialogs` / `new-sub-task-dialog.test.tsx`:
- `defaults to the parent branch and sends it as baseRef` [AC1]
- `switching to the default base sends the default base` [AC2]
- `parent without a branch shows the fallback helper and sends no baseRef` [AC3]
- `sidebar task menu offers New sub-task and opens the dialog with the parent`
- `git-branch-line.test.tsx`: `shows (gone) when the base is gone`; `hover card shows the parent branch for a stacked task` [AC7, AC9]
- `create-pr-dialog.test.tsx`: `defaults to prDefault and shows "Stacked on <parent>"`, `falls back to the default base when the parent is not pushed` [AC8]

## Mutations (each must fail its named test; listed in the PR body)

1. Default ignores the parent → `TestCreateTask_HumanSubtaskDefaultsToParentBranch` fails.
2. Agent-created path also defaults to the parent → `TestCreateTask_AgentChildKeepsDefaultBase` fails.
3. PR base always the parent even when not pushed → `TestPRBase_StackedParentNotPushedFallsBack` fails.
4. Descendant skip removed in `internal/stall` → `TestLiveDescendantsOfArchivedRootNeverAlert` fails.

## Frames (real app, isolated daemon, throwaway repo; light + dark; stable path `/tmp/stacked-task-shots/`)

New sub-task dialog (parent default, then switched to default base), hover card of a stacked task, Create PR dialog with "Stacked on",
plus transition frames for the base option switch (no layout shift).

## Progress

- [ ] spec committed
- [ ] daemon: default + PR default + gitstate fields (Peer A)
- [ ] web: New sub-task entry + dialog + hover + PR dialog (Peer B)
- [ ] stall follow-up
- [ ] frames, mutations, checks, PR

## Validation

(filled in as each acceptance criterion is confirmed)
