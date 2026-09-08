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
  back and forth; closing a tab removes only that tab; a task with a
  errored/pending-permission run shows the badge, selecting the task
  clears it; existing App-level tests (App.test.tsx) still pass or are
  updated deliberately with justification in Decisions.
- `bunx tsc -b` clean, `bun run test` passes, `task build` succeeds;
  restore `internal/server/dist/.gitkeep` if the build wipes it.

## Decisions

- **Registry shape** (`components/tab-registry.tsx`): `TabEntry {kind,
  key, taskId, title, closable?}`; `TAB_KINDS` descriptor map holds the
  per-kind `{closable, defaultTitle}` defaults; `defaultTabsForTask`
  seeds the four base tabs (Chat/Files/Diff/Terminal, `closable:false`);
  `fileTab` builds a closable file tab titled by path basename. Keys are
  globally unique and task-scoped: `${taskId}:${kind}` for base tabs,
  `${taskId}:file:${path}` for file tabs — per-task scoping falls out of
  the key structure (ADR 0004) and the App strips the path back out of
  the key for the file renderer (`filePathFromKey`).
- **Where per-task tab state lives**: `hooks/use-task-tabs.ts` — a
  `Map<number, {tabs, activeKey}>` in App.tsx component state, no
  persistence. All setters are Map-updaters returning `prev` on no-ops.
  Closing the active tab activates the right neighbor, else the left;
  closing the last tab leaves `activeKey: null`.
- **Tab strip mounting**: `key={selectedTask.ID}` on the Tabs root gives
  each task a fresh mount, preserving Radix's intentional
  unmount-inactive-content behavior (detach-not-stop on tab/task
  switch). The strip renders from the task's tab list with a
  kind→renderer lookup (`TabContent`); closable tabs get a close
  `<span role="button">` with stopPropagation + preventDefault — no
  nested `<button>` inside the trigger's `<button>`.
- **Tree/editor split**: `file-explorer-pane.tsx` is tree-only; clicking
  a file calls optional `onOpenFile(path)` (App opens/activates the
  file tab). The editor half moved to `file-editor-pane.tsx`
  (`FileEditorPane({client, task, path})` — file.read on mount/path
  change, dirty/save state, file.write, CodeMirrorEditor Mod-s), with
  its state inline in the component rather than a shared hook — only
  one file is ever edited per pane instance now, so the explorer's
  multi-concern hook machinery (selectedPath sessions, cross-file reset)
  buys nothing there.
- **Attention semantics** (`hooks/use-task-attention.ts`): on every
  client (re)connect, `run.list` (all runs) + `run.logs` per running run
  to find a `permission_request` with no later `permission_resolved`
  (same requestId) → reason `permission`. Terminal runs not in the
  selected task's seen-snapshot (taken on selectedTaskId change) give
  `error` (status error) or `finished`. The selected task's badge is
  implicitly suppressed the same way: its terminal runs are snapshotted
  as seen at selection. Map is prop-drilled App→AppSidebar→TaskRows; no
  new push/subscribe plumbing, per the plan's constraint.
- **Test moves/updates**: editor-behavior tests moved from
  `file-explorer-pane.test.tsx` to `file-editor-pane.test.tsx`
  (deliberate — the editor is now a separate component; tree tests stay
  in place, plus new onOpenFile/no-inline-editor coverage).
  `App.test.tsx` updates: (a) the sidebar helper now also answers
  `run.list` since useTaskAttention fires one on connect; (b) task-row
  clicks use a helper disambiguating the sidebar row from
  TaskDetailPane's same-titled h2; (c) tab clicks `.focus()` before
  `fireEvent.click` because Radix tab triggers need DOM focus to
  activate under jsdom (real browser clicks focus first). Four new
  tests: same-path-two-tasks, close-only-that-tab, errored-run badge +
  clear-on-select, unresolved-permission badge.

## Progress

- [x] Tab registry refactor of App.tsx tab strip
- [x] Per-task tab scoping (ADR 0004)
- [x] Sidebar attention badges
- [x] Verification (typecheck/tests/build)

## Validation

- Registry-driven strip: App.tsx maps `taskState.tabs` into
  triggers/contents with a kind→renderer lookup; no per-tab hardcoded
  JSX remains — confirmed by the two-distinct-tabs and close-only-that-
  tab tests exercising tabs created purely from registry entries.
- Per-task scoping: "same file path opened in task A and task B yields
  two distinct tabs, both preserved across task switches" passes —
  switching to B shows no leaked A tabs; A's README.md tab (and its
  content) survives the round trip. Keys carry the task id by
  construction (`${taskId}:file:${path}`).
- Attention badges: "errored run shows dot, selecting clears it" and
  "unresolved permission shows dot" both pass, driven only by run.list/
  run.logs over the existing client (re)connect flow — no new
  subscriptions. Dot renders as `<span data-testid="task-attention">`.
- Existing features: existing suites pass unchanged except the justified
  App.test.tsx updates above (run.list answering, row-click
  disambiguation, focus-before-click) — all four pre-existing App tests
  still pass verbatim in intent.
- `bunx tsc -b` clean; `bun run test`: 9 files, 73/73 passed;
  `task build` succeeded (dist/.gitkeep restored after Vite's
  --emptyOutDir wiped it).
