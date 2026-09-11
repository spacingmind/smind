# CRUD UI: create workspace/space/task, archive task, account management

Closes the "dead end" gap (uiux-audit finding #3): the UI can consume
tasks but cannot create anything — every CRUD action is CLI-only today.
All backend RPCs already exist; this is frontend + a few small backend
additions (workspace.update, task.rename — see Decisions).

## Acceptance Criteria

### Workspace

- Empty state (no workspaces) renders a first-run guide: brief text +
  a "New workspace" button (not just "No workspaces yet.").
- Sidebar: a "+" button in the Workspaces header opens a small dialog:
  path (required, must be an existing local git repo — same validation
  as CLI; show the daemon's error inline if not), title (optional,
  defaults to repo dir name). Submit → workspace.create → tree
  refreshes → new workspace auto-expanded.
- Workspace row context menu (or button on hover): "Add task", "Add
  space", "Remove workspace" — remove is OUT OF SCOPE this pass
  (no workspace.delete RPC exists; note as follow-up).

### Space

- "Add space" (dialog: title) → space.create → tree refresh; spaces
  already render nested per current sidebar model.

### Task

- "Add task" at workspace or space level (dialog: title; optional
  space select when started from workspace level) → task.create → tree
  refresh → task selected (so the user lands in the chat tab ready
  to prompt).
- Task row context/hover action: "Archive task" with a confirm step
  (task.archive checkpoints work first — per PR #52; confirm copy
  mentions this: "Uncommitted work is checkpointed to the task branch
  before the worktree is removed.").

### Accounts (minimal settings)

- A small settings entry (gear icon in sidebar header) opening an
  accounts dialog: account.list shows provider/label/credential type/
  created; "Add account" form: provider select (from provider.list),
  label, credential JSON pasted into a textarea (stdin semantics in
  the CLI; UI takes the same blob) → account.add → list refreshes.
  No edit/remove this pass.

### Live behavior

- New task/space/workspace appears without manual refresh via the
  existing task.status/statusOverrides? — NO: there are no
  workspace/space/task-created events for OTHER connections; simplest
  correct behavior this pass: refresh the tree locally after the
  creating action succeeds (the acting client knows). Record in
  Decisions.

## Test Scenarios

- Web component tests (FakeWsClient): empty state renders guide +
  button; create-workspace dialog validates path presence, submits
  workspace.create with right params, surfaces daemon error inline,
  refreshes tree on success; add-space/add-task dialogs ditto; archive
  confirm calls task.archive; accounts dialog lists and adds via
  account.add.
- Manual: run through the full first-run flow in a real browser
  (empty daemon → create workspace pointing at a real repo → add task
  → send a prompt). Record honestly in Validation.
- Existing suites stay green; `bunx tsc -b`, `bun run test`,
  `task build` (restore .gitkeep); Go side untouched (verify chain
  anyway if any backend file changes).

## Decisions

- Dialog components: shadcn Dialog primitives, feature-local wrappers
  in crud-dialogs.tsx.
- Tree refresh: local refresh by the acting client after each
  successful create/archive (no cross-connection events).
- Context menu vs hover buttons: "⋯" dropdown (RowMenu) on hover for
  workspace/space rows; task rows get their own per-task trigger.
- workspace.update/task.rename RPCs: NOT added this pass.
- Follow-ups found during the Playwright pass (both fixed):
  - Backend `CreateWorkspace` now defaults an empty title to the repo
    dir name (`internal/workspace/workspace.go`) — the spec's
    "defaults to repo dir name" was previously only true in the UI's
    eyes; the stored title was empty.
  - Backend `Manager.ListTasks` now filters out archived tasks
    (`internal/workspace/task.go`) — task.list previously returned
    archived tasks forever, so an archived task never left the sidebar
    (component tests missed it because their mock refresh returns []).
- See also docs/research/ui-test-ids.md: data-testid coverage for
  Playwright selectors (gaps found during this pass).

## Progress

- [x] First-run empty state + workspace create
- [x] Space create + task create + archive action
- [x] Accounts settings dialog
- [x] Tests
- [x] Manual first-run pass in a real browser — done 2026-09-11 as a
      Playwright pass against a real daemon + real browser (see
      Validation), plus the two follow-up fixes it surfaced.
- [x] Verification

## Validation

- `task build`, `task test`, `task lint` all pass (Go side untouched by
  this change, verified anyway per the plan).
- Web: `bunx tsc -b` clean, `bun run test` 124/124 passing across 12
  files, covering every automated scenario listed above (empty state,
  workspace/space/task create + refresh, daemon error surfacing,
  archive confirm + task.archive, accounts list/add/error).
- **Manual real-browser walkthrough — DONE (Playwright, 2026-09-11).**
  Driven headless Chromium against a real daemon (fresh
  SMIND_HOME, embedded dist UI) with a real local git repo:
  - First-run empty state renders guide + "New workspace" (PASS);
  - Create workspace via dialog (path only, no title) → appears in
    sidebar, auto-expanded (PASS) — and, after the fix, the stored
    title defaults to the repo dir name ("repo" in `smind workspace
    ls`, previously empty);
  - "Add task" via the workspace row's ⋯ menu → dialog → task appears
    and is selected (PASS);
  - Prompt sent from the chat form ("Say exactly: smind-e2e-ok") →
    run created, executed end-to-end against a real Anthropic account,
    assistant text exactly `smind-e2e-ok`, `stopReason: end_turn`
    (verified in run_events) (PASS);
  - Archive task via task row ⋯ menu → confirm dialog shows
    checkpoint copy → confirm → task archived; after the ListTasks
    fix the archived task leaves the sidebar on refresh (PASS);
  - Accounts dialog lists the imported account: label `claude-main`,
    badge "Anthropic (Claude) · oauth-shape credential" (PASS).
  Scripts and raw results live outside the repo in /tmp/smind-e2e/
  (e2e.mjs, e2e2.mjs, E2E-RESULT.md).
