# Audit: smind's web UI as it exists today

Read from the code on `feat/ui-redesign-parity` (based on `develop`, head
`b054dd0`). The dev server was **not** run for this audit — every claim
below is traced to a file. Where behaviour could only be confirmed by
running it, that is said explicitly rather than asserted.

Stack: React 19 + Vite 7 + Tailwind v4 + shadcn-style primitives (Radix),
CodeMirror 6, xterm 6, diff2html, react-markdown, react-resizable-panels,
lucide-react, Geist Variable. Tests: Vitest 4 + jsdom + Testing Library
(`web/packages/ui/package.json`). Built into `internal/server/dist` and
embedded in the Go binary.

Total: 62 files, ~10.4k lines under `web/packages/ui/src`.

---

## 1. Information architecture

**One screen. No router, no routes, no URLs.** `main.tsx` mounts `<App/>`
inside a `TooltipProvider`; that is the whole app shell.

```
┌────────────────────┬──────────────────────────────────────────┐
│ AppSidebar         │ header: [☰] | connection status           │
│  workspace         ├──────────────────────────────────────────┤
│   ├ space          │ [Chat] [Files] [Diff] [Terminal] [foo.go ×]│
│   │  └ task ●      │                                          │
│   └ (ungrouped)    │            active tab content            │
│  [accounts] [bell] │                                          │
└────────────────────┴──────────────────────────────────────────┘
```

- `App.tsx` — `SidebarProvider` > `ResizablePanelGroup` with two panels
  (sidebar, content) and a persisted sidebar width
  (`hooks/use-sidebar-width.ts`).
- Selection is a single `useState<Task | null>` in `App.tsx:56`. Selecting
  a task swaps the whole main pane. **Nothing is addressable**: no deep
  link to a task, no back button, no reload-survives-selection.
- **Object model**: `Workspace` → `Space` (optional grouping) → `Task`
  (→ `Run`s). `lib/types.ts:6-38`. A task owns a git worktree and branch.
  There is no "project" grouping above workspace and no "host" concept
  (one daemon, same origin).

**Tab registry** (`components/tab-registry.tsx`, ADR 0004): tabs are data
`{kind, key, taskId, title, closable}` with kinds
`task | files | file | diff | terminal`. Keys are task-scoped
(`${taskId}:file:${path}`), so the same path in two tasks is two tabs. Four
base tabs are seeded per task, plus closable file tabs. Per-task tab state
lives in `hooks/use-task-tabs.ts` as a `Map<number, {tabs, activeKey}>` in
component state — **not persisted**; a reload loses every open file tab.

`key={selectedTask.ID}` on the `Tabs` root remounts the strip per task, and
Radix unmounts inactive tab content — deliberately, since that is what
detaches live subscriptions without stopping runs (`App.tsx:29-45`).

**Not present**: splits, a side dock, an explorer dock, tab reordering,
drag-and-drop, "open to side", a command palette, multiple panes of any
kind. The side dock was scoped in `docs/research/dual-mode-ui.md` and named
in the `tab-registry-side-dock` plan's *title*, but it is not in that
plan's acceptance criteria and did not ship.

---

## 2. Agent/task interaction model

### Composer — `components/task-detail.tsx:PromptForm`

A single flex row pinned to the bottom: a native `<select>` for provider, a
native `<select>` for approval policy (`manual | auto-safe`), a
single-line `<Input>`, and a Send `<Button>`.

- Providers come from `provider.list` with a hardcoded two-item fallback
  (`task-detail.tsx:10-13`).
- **Single-line input.** No multiline, no Shift+Enter, no autogrow.
- No model selection, no mode, no thinking level, no attachments, no
  image paste, no `@file` references, no slash commands, no skills, no
  draft persistence, no queue, no steer, no stop-from-composer.
- No keyboard shortcut to focus it.

### Timeline — `components/task-detail.tsx:RunEntryView`, `hooks/use-run-timeline.ts`

Each *run* is one bordered card containing: provider name, status, a Stop
button while running, the prompt as a heading, and **one `<pre>` of
accumulated plain text**.

This is the structural gap. The daemon's event vocabulary is four values
(`internal/taskrunner/event.go:7-29`): `EventTypeText`, `EventTypeDone`,
`EventTypePermissionRequest`, `EventTypePermissionResolved`. The wire shape
(`web/packages/ui/src/lib/types.ts:136-186`) carries `{text}` for chunks.

Consequences, all of them downstream of that one fact:
- No assistant/user message distinction inside a run — the prompt is the
  card title, the response is the `<pre>`.
- **No tool calls.** No file-edit cards, no bash cards, no read/search
  cards, no tool status, no click-through to a file.
- No reasoning/thinking blocks. No todo lists. No compaction markers.
- **No markdown rendering** in the timeline. (`react-markdown` is a
  dependency but is only used by `components/file-preview.tsx`.)
- No turn model, no elapsed timer, no token/context usage, no copy button,
  no fork, no rewind, no retry, no subagents.
- No virtualization and no scroll anchoring: `div[data-testid=run-log-scroll]`
  is a plain `overflow-y-auto` (`task-detail.tsx:68`). Whether it
  auto-follows the tail is not implemented anywhere — it does not.

`hooks/use-run-timeline.ts` does the real work well: `run.list` for
history, `run.logs` for finished runs, `run.attach` for live ones,
detach-not-stop on switch/unmount, stale-response guarding. The data path
is sound; the *presentation* is a `<pre>`.

### Permission prompts — `components/task-detail.tsx:PendingPermissionView`

Correct in shape and already ahead of a naive implementation: rendered in a
**dock pinned between the scrolling log and the composer**
(`data-testid="pending-permission-dock"`), so it cannot scroll out of
view; buttons come from the provider's option list; the card clears on the
`permission_resolved` event (from this tab *or* another connection), not on
local click.

Missing vs the references: no diff/command preview of *what* is being
requested beyond `summary`; no structured multi-question form; no plan
review; no per-option kind styling (`allow_once` vs `reject_always` look
identical); no keyboard affordance; no OS notification specifically for a
permission (the generic attention notification covers it).

### Diff / review — `components/diff-viewer-pane.tsx`

Genuinely good, and the most complete pane. Per ADR 0006: `task.files`
gives the changed-file list, each row expands to its own `task.fileDiff`
rendered by diff2html, with a per-file stage/unstage checkbox
(`task.stage`), a local "viewed" flag, a commit bar (`task.commit`), and
PR creation (`task.createPr`).

Missing: no line comments / review drafts, no whole-diff view, no
side-by-side toggle, no unstaged/staged split, no commit history, no
branch switcher, no push, no merge-from-base, no suggested commit message,
no diff stat surfaced anywhere outside this pane.

### Files / editor — `components/file-explorer-pane.tsx`, `file-editor-pane.tsx`

Lazy directory tree (`file.list`) with keyboard-operable rows. Clicking
opens a file tab. The editor does `file.read` → CodeMirror buffer → dirty
tracking → `file.write` with `expectedMtime`, an explicit conflict banner
(changed-on-disk / deleted-on-disk, no auto-reload), a re-probe on
terminal `run.status` while dirty, and an Edit/Preview toggle for
`.md`/`.svg`/`.html` that keeps CodeMirror mounted so cursor/undo survive.

Missing: no file icons beyond a generic glyph, no syntax-aware theming
tied to app theme, no search, no multi-file search, no context menu
(new/rename/delete), no breadcrumb, no git status decoration in the tree,
no dirty indicator on the tab itself.

### Terminal — `components/terminal-pane.tsx`

xterm + FitAddon behind a `TerminalHandle` interface so the wiring is
testable without a real emulator. `terminal.create/attach/write/resize`,
detach-not-close on unmount, status handling including `interrupted`
(post-daemon-restart).

Missing: one terminal per task only (no multiple terminal tabs), no
profiles, no activity indicator, no scrollback setting, no copy/paste
affordances, no link detection, no split.

---

## 3. Workspace / task model surface

`components/app-sidebar.tsx` (767 lines) + `components/crud-dialogs.tsx`:

- `useWorkspaceTree` loads `workspace.list`, then per workspace
  `space.list` + `task.list` in parallel, and groups tasks by `SpaceID`
  client-side. Ungrouped tasks are always rendered.
- CRUD dialogs: create workspace (with a folder picker,
  `components/folder-picker-dialog.tsx` over `fs.listDir`), create space,
  create task, archive task, delete space, delete workspace.
- There are **no workspace/space/task lifecycle events** on the wire —
  the acting client calls `refresh()` locally after each mutation
  (`app-sidebar.tsx:97-100`). Another open tab does not see the new task.
- No rename, no reorder, no pin, no drag, no labels, no search/filter, no
  archived-task view, no grouping options, no per-row kebab beyond
  archive/delete, no branch display on the row, no diff stat on the row.

---

## 4. Settings / accounts / providers

**There is no settings screen.** The only configuration surface is
`components/accounts-dialog.tsx` (459 lines), opened from a sidebar
button.

It derives its rows from `provider.list`
(`internal/taskrunner.SupportedProviders`) rather than a hand-maintained
list, which is right. Per provider it shows: presence of a credential, a
health dot from `provider.test`, a Connect button for
`credentialKind: "oauth"` (`account.oauthStart`), a manual paste form for
`"api-key"` (`account.add`), and "managed externally" for
`kind: "cli"` (GLM).

Missing vs `audit-cliproxyapi.md`: no status message, no token expiry /
last refresh, no rate-limit recovery time, no success/failure counters, no
usage/quota, no per-account label or note, no disable toggle, no delete,
no "which models does this credential serve", no cancel on an in-flight
OAuth session, no multiple accounts per provider. And the known
vocabulary seam is documented in the file itself: `ProviderInfo.id`
(`claude-native`/`glm`/…) vs `accountProvider`
(`anthropic`/`openai`/…), with `xai`/`antigravity` reachable only from the
CLI.

Nothing else is configurable from the UI: no theme, no font size, no
density, no layout preferences, no shortcuts, no notification settings, no
default provider/policy, no terminal settings.

---

## 5. Status / attention signalling

`hooks/use-task-attention.ts` + `hooks/use-attention-notifications.ts` +
`hooks/use-notification-permission.ts`:

- Reasons: `error | finished | permission` — the same vocabulary Paseo
  uses. Computed on every (re)connect from `run.list` + `run.logs` per
  running run (an unresolved `permission_request`), plus a seen-snapshot
  taken when a task is selected.
- Renders as a **single dot** (`span[data-testid="task-attention"]`) on the
  sidebar task row. Selecting the task clears it.
- Browser `Notification` when a *new* (task, reason) pair appears while
  `document.hidden`. Edge-triggered, with a first-render baseline so a
  reload doesn't re-notify. A bell button toggles permission, labelled per
  state (`default/granted/denied/unsupported`).
- `hooks/use-daemon-events.ts` subscribes to the three daemon topics
  (`task.status`, `run.status`, `permission.pending` —
  `internal/wsapi/events.go:16-18`) and fans them out.
- Connection status is a text string in the header
  (`app-connection-status`) plus an amber banner inside the task pane while
  reconnecting (`task-detail.tsx:65-69`). Reconnect itself
  (`lib/reconnect.ts`) creates a *new* `WsClient` instance so every
  client-keyed hook resyncs for free.

Missing: no status dot colour family (the dot is monochrome), no status
pills, no per-run status beyond an uppercase word, no workspace-level
aggregate, no unread/viewed timeline model, no badge counts, no toasts, no
alert component, no diff stat / branch / CI on sidebar rows.

---

## 6. Theming and appearance

`web/packages/ui/src/index.css` (129 lines). Standard shadcn token set in
oklch: `background/foreground/card/popover/primary/secondary/muted/accent/
destructive/border/input/ring/chart-1..5/sidebar*`, `--radius: 0.625rem`,
Geist Variable as `--font-sans`.

**A complete `.dark` block exists and is never applied.** Nothing sets the
`dark` class, nothing reads `prefers-color-scheme`, and there is no
toggle — confirmed by grep across `web/packages/ui/src`. This was already
flagged as open question 1 in `docs/research/uiux-audit.md` §4 P2 item 10
and is still open. In practice smind is light-mode only, which for an
agent tool used at night is a real daily-driver complaint.

Also absent: no font-size/density settings, no syntax theme (CodeMirror
and diff2html and highlight.js each bring their own, unharmonized with the
app tokens), no terminal ANSI palette tied to the theme, no status-colour
tokens, no identity colours.

The one chromatic token in an otherwise fully achromatic palette —
`--sidebar-primary: oklch(0.488 0.243 264.376)` in `.dark`
(`index.css:112`) — is leftover shadcn default, previously flagged in
`uiux-audit.md` §2.5.

---

## 7. Keyboard

Essentially none. Grep across `web/packages/ui/src` for key handling
returns exactly three real hits:

- `components/ui/sidebar.tsx:107` — the shadcn sidebar's own `Cmd/Ctrl+B`.
- `components/code-mirror-editor.tsx:60` — `Mod-s` to save.
- `components/file-explorer-pane.tsx:158` — Enter/Space on tree rows.

No command palette, no focus-composer shortcut, no tab switching, no task
switching, no interrupt, no shortcut help, and the tab-close `×` is a
`role="button"` `<span>` with no `tabIndex` or key handler
(`App.tsx:208-222`) — flagged in `uiux-audit.md` §4 P1 item 9, still open.

---

## 8. Mobile / responsive

`hooks/use-mobile.ts` exists (a 768px `matchMedia`) and is consumed by
exactly one file: the shadcn `components/ui/sidebar.tsx`, which switches
the sidebar into a `Sheet` below the breakpoint. That is the entire
responsive story.

Everything else is desktop-shaped: the resizable two-panel group, the
horizontal tab strip, the single-row composer with two `<select>`s and an
input, the diff pane, the terminal. No compact layout is designed; no
touch targets are sized for it; nothing has been checked on a phone.

Given `docs/plans/active/relay-e2ee-mobile.md` (Phase 3 — reach the daemon
from a phone), the web UI being unusable on a phone is a scheduled
problem, not a hypothetical one.

---

## 9. Empty / loading / error states

Handled per-pane, inconsistently — the specific finding of
`docs/research/uiux-audit.md` §2.2, partially addressed since (the
empty-database crash is fixed; CRUD dialogs exist so the empty state is no
longer a dead end).

Current inventory:
- Sidebar: "No workspaces yet." / loading text / error text.
- Task pane: "Select a task to get started." (`app-empty-state`),
  "Loading runs…", "No runs yet. Send a prompt to start one.".
- Diff pane, file explorer, editor, terminal: each rolls its own copy and
  spacing.
- No shared `PaneHeader` primitive — four panes hand-roll a header row
  with two different padding scales (`uiux-audit.md` §4 P1 item 5, open).
- No skeletons anywhere despite `components/ui/skeleton.tsx` existing.
- No toast/alert primitive; errors are inline `<p className="text-destructive">`.
- In-flight feedback is inconsistent: Save has it, Commit/Stage/Reload
  don't (`uiux-audit.md` §4 P1 item 8, open).

---

## 10. What is genuinely good and must not be regressed

Worth stating, because a redesign is a good way to lose these:

1. **Detach-not-stop** everywhere: switching tasks, closing a tab, or
   unmounting a pane never kills a run or a terminal
   (`use-run-timeline.ts`, `terminal-pane.tsx`, `App.tsx:29-45`).
2. **Reconnect resync by construction** — a new `WsClient` instance forces
   every client-keyed hook to re-fetch (`lib/reconnect.ts`, PR #48).
3. **Cross-connection correctness** — a permission resolved in another tab
   clears here; a run started from the CLI streams here.
4. **Conditional writes** — `file.write` carries `expectedMtime` and
   surfaces a conflict rather than clobbering.
5. **Provider list derived from the daemon**, not duplicated in the
   frontend (`provider.list`).
6. **Task-scoped tab keys** (ADR 0004) — the right primitive to build
   splits on.
7. **Real component tests** against a fake `WsClient`
   (`test/fake-ws-client.ts`, `test/fake-socket.ts`) — 16 test files today.
