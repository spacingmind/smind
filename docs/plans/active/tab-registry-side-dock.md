# Tab registry + per-task tabs + sidebar attention badges

Implements the prerequisite recommended by `docs/research/dual-mode-ui.md`,
under the scoping decided in `docs/decisions/0004-per-task-editor-tabs.md`.
Frontend-only; no daemon changes.

## Acceptance Criteria

- `App.tsx`'s hardcoded tab strip becomes a tab registry: tabs are
  `{kind, key, taskId, title}` entries (kind e.g. `task`, `file`,
  `diff`, `terminal`) rendered from a list, not hardcoded JSX per tab.
  Adding a new tab kind later must not require editing the strip's
  layout logic.
- Per ADR 0004: tab sets are scoped per **task**. Switching the selected
  task in the sidebar switches the visible tab set; each task remembers
  its own open tabs while the app is open (component state is enough —
  no persistence required this pass).
- File/diff/terminal tabs opened from a task's surfaces carry that
  task's id in their key (two tasks can have the same file path open
  without colliding).
- The workspace/space/task sidebar shows attention badges: a task with a
  run in `error`, a finished-but-unseen run, or a pending permission
  request gets a visible marker. Driven by data the app already receives
  (run status via the existing run list/attach flows) — do NOT build new
  push/subscribe plumbing for this (that's a separate task,
  `wsapi-event-subscription`); a refresh/reconnect-driven update is
  acceptable this pass. Badge state clears when the task is selected.
- The existing features keep working unchanged: task detail timeline,
  file explorer + editor + preview, diff viewer, terminal, permission
  prompts.

## Test Scenarios

- Frontend component tests (jsdom + Testing Library, existing
  FakeWsClient patterns): opening a file in task A and the same path in
  task B yields two distinct tabs, both preserved when switching tasks
  back and forth; closing a tab removes only that tab; a task with an
  errored/pending-permission run shows the badge, selecting the task
  clears it; existing App-level tests (App.test.tsx) still pass or are
  updated deliberately with justification in Decisions.
- `bunx tsc -b` clean, `bun run test` passes, `task build` succeeds;
  restore `internal/server/dist/.gitkeep` if the build wipes it.

## Decisions

(To be filled by the implementer: registry data shape, where per-task
 tab state lives, badge-state derivation, any App.test.tsx changes.)

## Progress

- [ ] Tab registry refactor of App.tsx tab strip
- [ ] Per-task tab scoping (ADR 0004)
- [ ] Sidebar attention badges
- [ ] Verification (typecheck/tests/build)

## Validation

(Filled in as each Acceptance Criterion is confirmed.)
