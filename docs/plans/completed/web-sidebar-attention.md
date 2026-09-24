# Web UI: actionable notifications + sidebar triage

## Context

smind runs agents in the background, so attention signals matter. See
`docs/research/local/paseo-uiux-2026-09.md` (local-only, gitignored), gaps #7 and #8.

What smind does today:

- `web/packages/ui/src/hooks/use-attention-notifications.ts` fires
  edge-triggered browser `new Notification(title, { body })` (around
  line 83) while the tab is hidden, but has no `onclick`. Clicking a
  notification is a dead end.
- No unread count in the tab title or favicon, no sound, and no
  Notifications settings section.
- The sidebar (`components/app-sidebar.tsx`, about 1249 lines;
  `lib/sidebar-signal.ts`, `hooks/use-task-attention.ts`) has a status
  dot, branch, changed count, attention by reason, search, archive and a
  CRUD context menu.
- It has no pin, no mark-unread, no hover card and no group-by-status
  view.

**Port the behavior from `refs/paseo/packages/app/src` (updated
2026-09-24):**

- Notifications:
  - `desktop/components/desktop-notifications-section.tsx` (permission
    status, play sound, test/sent feedback);
  - `push-notifications/index.web.ts`;
  - badge count (`desktop/host.ts` around line 113).
- Sidebar:
  - the unread handling (#3603) in `components/sidebar-workspace-list.tsx`;
  - `components/sidebar/{pinned-section-header.tsx,sidebar-workspace-row.tsx,sidebar-workspace-menu.tsx,sidebar-status-list.tsx}`;
  - `components/workspace-hover-card.tsx`;
  - group by status or project in `command-center/root-contributions.ts`
    (around line 34);
  - `components/status-ring/`.

## Decisions

- **All client-side state.** Pins, the unread set and the group-by
  choice persist in localStorage, the same way other web UI preferences
  already do (see `lib/storage.ts` and similar). No daemon or wire change.
- **Unread** = a task had new attention-worthy activity (a finished run,
  an error, a pending permission — reuse the existing attention reasons)
  since the user last viewed that task.
  - Opening a task marks it read.
  - "Mark unread" is a manual override in the task's context menu.
- **The tab title shows the unread count**, e.g. `(3) smind`. A favicon
  badge is optional — only if it's cheap.
- **Notification click:** `onclick` focuses the window and opens that
  task (reuse the existing route/`openTask` path).
- **Sound:** optional, off by default, set in the new Notifications
  settings section. Use a short bundled sound or the Web Audio API, not
  a network asset.
- **Notifications settings section:**
  - an enable toggle that moves the existing notifications setting from
    General if one lives there — check `settings-registry.ts` and don't
    duplicate it;
  - browser permission status with a request button;
  - a "send test notification" button;
  - the sound toggle.
- **Sidebar:**
  - A "Pinned" section at the top; pin/unpin from the task context menu.
  - An unread marker on rows.
  - A hover card on the task row: branch, diff stat, PR state if known,
    last activity — only data the web UI already has; don't invent data.
  - A view toggle: the grouped tree (default) or group-by-status, where
    status groups are needs attention / running / idle / done / error,
    derived from existing status and attention data.
- `Cmd+digit` task jump is **not** in this plan: `web-keyboard-tabs` owns
  it. Avoid editing `keyboard/shortcuts.ts`.

## Acceptance Criteria

- **AC1** Clicking a browser notification focuses the smind window and
  opens that task.
- **AC2** The tab title shows the unread count and updates live; opening
  a task marks it read; "Mark unread" works from the context menu.
- **AC3** A Notifications settings section with an enable toggle,
  permission status/request, a test notification and an optional sound.
  An existing notifications setting is moved, not duplicated.
- **AC4** Pinned tasks appear in a Pinned section at the top; pin/unpin
  persists across reloads.
- **AC5** A hover card on the task row shows branch, diff stat and last
  activity from existing data.
- **AC6** A group-by-status view toggle, persisted; groups are derived
  from existing status and attention data.
- **AC7** No hardcoded colors (the existing test passes), and all
  existing tests pass. `typecheck`, the web tests and `task lint` are
  green.

## Test Scenarios

- Unit:
  - unread model — attention event → unread; view → read; manual mark
    unread; persistence;
  - the title-count formatter;
  - the status-grouping function;
  - pin persistence.
- Component:
  - notification onclick handler (mock `Notification`) routes to the
    task;
  - settings section (permission states, test button, sound toggle);
  - the Pinned section;
  - the hover card content;
  - the group-by-status toggle.
- Manual: background the tab, trigger a run finishing, click the
  notification, and confirm it lands on the task. Check both themes.

## Progress

- [x] AC1 notification click → task
- [x] AC2 unread + title count + mark unread
- [x] AC3 Notifications settings section
- [x] AC4 pinned section
- [x] AC5 hover card
- [x] AC6 group-by-status view

## Validation

- **AC1**: `hooks/use-attention-notifications.ts` sets `notification.onclick`
  to `window.focus()` + the caller's `onOpenTask`; `AppSidebar` wires it to
  find the task in the already-fetched tree and call `onSelectTask`.
  Covered by `use-attention-notifications.test.ts`'s "clicking a fired
  notification focuses the window and opens its task" case.
- **AC2**: `hooks/use-unread-tasks.ts` (edge-triggered off `TaskAttention`,
  same `${taskId}:${reason}` bookkeeping as the notification hook; clears on
  selection; `markUnread` is the manual override) persisted via
  `lib/sidebar-preferences.ts`; `lib/tab-title.ts` formats the document
  title. Covered by `use-unread-tasks.test.ts` (gain → unread, open → read,
  manual mark-unread, persistence across remount, no re-add after read),
  `tab-title.test.ts`, and `app-sidebar.test.tsx`'s "unread marker (AC2)"
  describe block (marker presence/slot-width stability, the task menu's
  Mark unread action). Wired into the tab title in `App.tsx`.
  **Post-review fixes:**
  - *Ghost unread count*: `useUnreadTasks`/`usePinnedTasks` now take a
    `liveTaskIds: ReadonlySet<number> | null` param and prune stale entries
    against it -- `null` means "tree not loaded yet" and is never pruned
    against (an empty *initial* task list must not wipe a persisted set
    before the real fetch lands); `App.tsx`/`app-sidebar.tsx` derive it
    from `treeLoaded`/`workspaces !== null` respectively, so pruning only
    starts after the first successful load. The title count is therefore
    `unread ∩ live tasks` for free -- `formatTabTitle` reads the same
    already-pruned `unread` state. Covered by both hooks' "pruning against
    the live task list" describe blocks (no prune before load, archived
    task drops out, a still-live id survives).
  - *Hidden-window gap*: the currently-selected task is now exempt from a
    new attention gain only while `document.hidden` is false; a gain while
    the tab/window is hidden marks it unread same as any other task. A new
    `visibilitychange` listener clears the still-selected task's unread
    flag when the document becomes visible again. Covered by
    `use-unread-tasks.test.ts`'s "hidden-window gap" describe block (marks
    unread while hidden, clears on visibility return for the selected
    task, does not clear an unrelated task's unread flag).
- **AC3**: `components/settings/notifications-section.tsx`, a new
  registered settings section (permission status/request -- moved from
  `general-section.tsx`, not duplicated; a sound toggle backed by
  `hooks/use-notification-sound-preference.ts`; a test-notification button
  with sent/error feedback, behavior ported from refs/paseo's
  `DesktopNotificationsSection`). Sound plays via
  `lib/notification-sound.ts`'s Web Audio chime, wired into
  `useAttentionNotifications`'s new `playSound` param. Covered by
  `settings-screen.test.tsx`'s "SettingsScreen Notifications section
  (AC3)" describe block (moved-not-duplicated, permission toggle,
  disabled-until-granted test button, success/error feedback, sound
  toggle), `use-notification-sound-preference.test.ts`,
  `notification-sound.test.ts`, and the two new
  `use-attention-notifications.test.ts` cases for `playSound`.
- **AC4**: `hooks/use-pinned-tasks.ts` persists the pinned-task id set via
  `lib/sidebar-preferences.ts`; `app-sidebar.tsx`'s new `PinnedSection`
  renders every pinned task (flattened across workspaces, same `TaskRows`
  the tree uses) above the workspace list, and the task context menu gets
  a Pin/Unpin toggle. A pinned task still appears in its normal
  workspace/space location too (Paseo removes it from the pool entirely;
  simpler to duplicate here given smind's tree is client-derived, not
  server-bucketed). Covered by `use-pinned-tasks.test.ts` (toggle,
  persistence) and `app-sidebar.test.tsx`'s "pinned section (AC4)" describe
  block (pin shows the section, unpin removes it and flips the menu label,
  persistence across remount, collapse hides rows without unpinning).
- **AC5**: `components/ui/hover-card.tsx` (new shadcn-style wrapper over
  `radix-ui`'s HoverCard, already a project dependency) wraps each task
  row in `TaskRows`; `TaskHoverCardBody` shows title, branch, diff stat
  (from the same `useTaskStats` data `TaskMetaRow` already renders),
  `lib/relative-time.ts`'s "last activity" from `task.UpdatedAt`, and the
  coarse status -- no PR state, since `lib/types.ts`'s `Task` carries no PR
  field yet (plan's "don't invent data"). Covered by
  `relative-time.test.ts` and `app-sidebar.test.tsx`'s "task hover card
  (AC5)" describe block (pointer-enter + fake-timer advance past the open
  delay, asserting branch/diff-stat/last-activity content).
- **AC6**: `lib/sidebar-status-groups.ts`'s `statusGroupForTask`/
  `groupTasksByStatus` derive the five buckets (needs attention / error /
  running / done / idle) from the same `TaskAttention`/`TaskRunStatus`
  data the tree view's dots use -- `error` keys off the run's own status
  (survives being "seen"), `permission`/`finished` route through
  needs-attention ahead of running/done. `hooks/use-sidebar-group-mode.ts`
  persists the tree/status choice; `app-sidebar.tsx`'s new
  `StatusGroupedList`/`StatusGroupItem` render it (replacing the workspace
  tree while active; Pinned stays above either view) via a header toggle
  button. Covered by `sidebar-status-groups.test.ts`,
  `use-sidebar-group-mode.test.ts`, and `app-sidebar.test.tsx`'s
  "group-by-status view toggle (AC6)" describe block (default tree view,
  toggle swaps to grouped view with correct bucket, per-group collapse,
  persistence across remount).

- **AC7**: `test/no-hardcoded-colors.test.ts` passes unchanged (no new
  file introduces a raw Tailwind palette class or hex/oklch literal --
  every new color use goes through an existing token: `bg-primary`,
  `text-status-success`/`text-status-danger`, `bg-popover`, etc.). Full
  suite: 900 tests across 82 files, all green. `bun run --filter
  '@smind/ui' typecheck` clean. `task lint` (from the repo root) clean.

All seven acceptance criteria (AC1-AC7) are satisfied.
