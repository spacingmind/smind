# App header (ZCode) and simple tabs (Paseo)

Size: M (5) — scope 1 (web UI only), PRs 1, risk 1, decisions 1 (all UI-level, recorded below), verification 2 (light+dark screenshots, desktop-chrome render).

Goal: pre-v0.11 UI polish. (1) One ZCode-style top bar over an inset, rounded
content panel, instead of "toggle + Connected to daemon" plus a separate task
header block. (2) Paseo-style tabs: no box, icon + truncated title, active =
subtle filled pill, close only on hover/active, "+" right after the tabs.
Web UI only; no daemon/Go change, no Rust change.

Sources (read, not copied): `refs/zcode/packages/ui/src/{WorkspaceHeader.tsx,DesktopTopOverlay.tsx,WorkspaceHeaderSections/WorkspaceHeaderActionSection.tsx}`,
`refs/zcode/DESIGN.md` (frames, radius); `refs/paseo/packages/app/src/screens/workspace/workspace-desktop-tabs-row.tsx`
(36px row, 24px chip, 8px chip padding, 14px icon, hover `surface1`, active `surface2`, close overlay on hover/active, 8px dirty dot).

Out of scope: a right/side panel (smind has none: panes are a split tree), back/forward
(smind has no navigation history: selection is a hash route, so omitted, no dead buttons),
share/export (no feature), touching the chat timeline/message components
(`feat/chat-timeline-rail` owns them), the sidebar body (only its top strip).

## Decisions

- D1 Layout: the content area becomes one inset panel (`app-content-panel`): `bg-panel`, rounded top-left,
  `border-l border-t`, sitting on the sidebar-coloured window background (darker zone = sidebar + window, lighter
  panel = content), as in ZCode's Windows build. The header is the panel's first row (h-12), the pane tree below.
  A single pane is frameless inside the panel; with more than one pane each pane keeps its own inset frame (`rounded-lg border`) as today.
- D2 Left zone: ZCode floats it over the sidebar. smind's sidebar already owns a top strip (logo + wordmark, traffic-light
  inset on macOS), so the zone is that strip: the sidebar toggle sits at its right end while the sidebar is expanded.
  When the sidebar is the 48px rail (or a mobile sheet) there is no room (traffic lights overhang the rail), so the toggle
  sits at the header's left edge, after the existing macOS traffic-light inset. Exactly one toggle is on screen.
- D3 Update badge: lives in the same left zone (strip when expanded, header when collapsed). Needs the update state
  shared between the dialog and the badge, so `useAppUpdate` gets a `pending` (available but not currently prompting) state.
  Click re-opens the prompt. Web: never rendered.
- D4 Connection status: the header no longer prints "Connected to daemon". It stays as an `sr-only` `role=status`
  (a11y + existing tests) and becomes a visible pill only while not connected (reconnecting / disconnected). The Host footer
  keeps its dot.
- D5 Task metadata: title in the header with a hover/focus card (branch, status, last activity, setup, port, worktree);
  the chat pane drops its duplicate title line (title + "· Chat" duplicated the header and the tab) and keeps one compact
  secondary row: MCP, usage, setup badge, port chip, an agent chip and the Detailed toggle (right-aligned). Branch + status move to the hover card.
  Review round 2: the run-config pill ("No agent · Claude Code · Accept File Edits · Standard") mirrored the composer toolbar right below it, so it is gone.
  The composer does not show the saved agent profile, so the pill survives only as a compact chip naming it ("<Agent>", "<Agent> · edited" after
  hand-edits; click still focuses the toolbar) and only when an agent is applied. The row stays because the Detailed toggle is chat-scoped (the header
  cannot know the active tab is a chat; folding it into the ⋯ menu would need new cross-component plumbing) and it also hosts the usage/setup/port badges.
- D6 Header ⋯ menu: Copy branch name, Copy worktree path, Reveal in <file manager> (desktop + local connection only),
  Archive task… (the existing `ArchiveTaskDialog`; the sidebar updates through lifecycle events). Pin / Mark unread / Move
  are sidebar-owned state and stay in the sidebar menu (a second `usePinnedTasks` instance would not see the same state).
- D7 Right cluster: open-in-editor split pill (primary = last chosen editor, chevron lists installed editors; desktop +
  local connection + task has a worktree), help (opens the shortcuts dialog), terminal (opens or activates the task's
  terminal tab; not a hide toggle because closing a tab ends the session), app menu (Windows/Linux), caption buttons.
- D8 Terminal tab icon `>_` (lucide `Terminal`) instead of `SquareTerminal`.

## Acceptance Criteria

### Header
- AH-AC1: the content is an inset panel (`app-content-panel`) holding one top bar (`app-header`, 48px, faint bottom border) and the panes; the panel has a rounded top-left corner and the panel background token; the sidebar column keeps the darker window background.
- AH-AC2: with a task selected the bar shows a task icon, the task title (`app-header-title`, truncating) and a ⋯ button (`app-header-task-menu`); with no task it shows neither, nor any task-scoped control.
- AH-AC3: hovering or focusing the title opens a card with branch, status, last activity (and setup status/port when present).
- AH-AC4: ⋯ menu offers Copy branch name, Copy worktree path, Archive task…; Reveal in <file manager> only on a desktop build with a local connection. Archive opens the existing confirm dialog and calls `task.archive`.
- AH-AC5: right cluster order: open-in-editor pill, help, terminal, app menu (Windows/Linux), window controls (Windows/Linux). No right-panel or share control.
- AH-AC6: the open-in-editor pill renders only on a desktop build with a local connection and a task worktree; its main button opens the worktree in the remembered editor (default: first installed), the chevron lists every installed editor and picks one (remembered across reloads); web/url/relay: absent.
- AH-AC7: the help button runs `shortcuts.help`; the terminal button opens (or activates) the terminal tab for the selected task.
- AH-AC8: "Connected to daemon" is not visible text any more; it stays in the DOM as `sr-only` `role=status`; while reconnecting/disconnected a visible pill carries the text.
- AH-AC9: sidebar expanded (desktop layout) → toggle in the sidebar top strip; sidebar collapsed to the rail or compact layout → toggle at the header's left. Never both, never neither.
- AH-AC10: desktop chrome intact: header keeps `data-tauri-drag-region="deep"` and the macOS traffic-light left inset when the header is at the window edge; Windows/Linux still end in the drawn caption buttons; interactive controls carry no drag attribute; browser build has none of it.
- AH-AC11: update badge: desktop build with an update available (prompt open or dismissed with Later) → badge in the left zone showing the version in its title; click re-opens the prompt; none when idle; none in a browser build.
- AH-AC12: chat pane no longer renders the `h2` "<task> · <chat>" line; the MCP indicator, usage total, setup badge, port chip and the Detailed toggle stay (same test ids) in one compact row; the run-config pill appears only as an agent-name chip (no provider/model/mode/thinking, which the composer shows).
- AH-AC13: at 390×844 the header fits without horizontal overflow: toggle, title (truncating), ⋯, terminal; editor pill and help hidden.
- AH-AC14: light and dark: header, panel and sidebar read as two tones; no clipped corner or double border.

### Tabs
- TB-AC1: the tab list has no background, border or shadow; a tab is icon + truncated title; inactive is muted text on no fill; hover fills subtly; the active tab is a subtle filled rounded pill (`data-state=active`), text foreground.
- TB-AC2: the close × is mounted for every closable tab but visible only on hover, keyboard focus within the tab, or when the tab is active; the Split affordance follows the same rule.
- TB-AC3: the "+" button directly follows the last tab (no gap to the far edge), same row.
- TB-AC4: terminal tabs use the `>_` icon (`data-icon="terminal"` stays); dirty and activity markers unchanged.
- TB-AC5: overflow: the tab row scrolls horizontally without wrapping or showing a native scrollbar; the active tab is scrolled into view when it changes; "+" stays reachable.
- TB-AC6: all tab behaviour is unchanged: click activates, ×/Enter/Space closes, context menu (Close, Close others, to the left/right, Rename, Archive chat, Copy path, Split), inline rename, drag between panes and split drop zones, keyboard `tab.jump/next/prev/new/close`, empty state.
- TB-AC7: single pane renders frameless inside the content panel; two or more panes render inset frames with the focus ring as today.

## Test scenarios (names)

Web (vitest):
- `app-header.test.tsx`: "AH-AC2 shows icon, title and menu only with a task", "AH-AC4 task menu items and archive confirm", "AH-AC4 reveal only for local desktop", "AH-AC5 right cluster order", "AH-AC6 editor pill: gated, remembers editor, opens worktree", "AH-AC7 help runs shortcuts.help", "AH-AC7 terminal button opens terminal tab", "AH-AC8 connection text is sr-only while connected and visible when not", "AH-AC9 toggle in header only when sidebar collapsed or compact".
- `app-header-title.test.tsx`: "AH-AC3 hover card shows branch, status, last activity".
- `desktop-chrome.test.tsx` (updated): "AH-AC10 header drag region + traffic-light inset + caption buttons".
- `update-badge.test.tsx` + `app-update.test.ts`: "AH-AC11 badge shows for prompt and for dismissed update, click re-prompts, hidden when idle/web".
- `task-detail.test.tsx` (updated): "AH-AC12 no title heading; chips and Detailed toggle remain".
- `App.test.tsx` / `App.responsive.test.tsx` (updated): "AH-AC8/AH-AC9 via the app shell", "single pane is frameless, two panes framed (TB-AC7)".
- `tab-strip.test.tsx` (new, or the existing App tab tests): "TB-AC1 list carries no box classes, active pill", "TB-AC2 close visible rule", "TB-AC3 plus adjacent", "TB-AC4 terminal icon", "TB-AC5 active tab scrolled into view", "TB-AC6 existing behaviours" (existing tests keep passing unmodified except class assertions).
- Visual: light + dark screenshots at 1440×900 and 390×844: no task, task selected, chat+terminal tabs, many tabs overflow, tab hover, editor pill (forced desktop flag with a stubbed Tauri bridge), macOS and Windows chrome renders.

## Progress

- [x] Spec, baseline screenshots (`/tmp/ahat-sbx/shots/before`, local, not committed)
- [x] T1 tab strip restyle (Peer, peer-sonnet; `ab8d46d6`..`22fae36c`), then Lead rework of the affordances into a floating wrapper (`6b2db009`)
- [x] H1 update badge state + component (Lead, `17e6efb1`)
- [x] H2 header + inset panel + sidebar left-zone strip (Lead, `cd6f3ee0`, `6b2db009`)
- [x] H3 header tests (Peer, peer-sonnet; `ee4a3eff`..`92cabab3`)
- [x] Review, screenshots, `task lint`, `task test`

Peer log: peer-glm (`3ba397b7`) hit a 429 usage limit before any edit (tree clean), so everything went to peer-sonnet (`fecb1c74`): tabs, then header tests. Candidates ACCEPTED after review.
Review notes: (1) Peer keyed the active pill on `aria-selected` because `ContextMenuTrigger asChild` overwrites the trigger's `data-state` (confirmed) -- accepted. (2) Peer's in-flow opacity-0 close/split left a clickable invisible x and wide gaps between tabs; REJECTED in part and reworked by the Lead into one `pointer-events-none` floating wrapper (Paseo reserves 0px for the close too: `TAB_CLOSE_BUTTON_RESERVED_WIDTH = 0`) with a title fade on hover and an in-flow spacer for the active tab. (3) Tests accepted; AH-AC13's test selects at 1024px then resizes (compact sidebar is a closed sheet), not a phone-width first load.

## Validation

Screenshots (local, `/tmp/ahat-sbx/shots/{before,after}`; 1440x900 and 390x844, light and dark): no task, task selected, title hover card, ⋯ menu, chat+terminal+files+diff tabs, tab hover, 11-tab overflow, collapsed rail / mobile sheet, plus a forced desktop build (`VITE_SMIND_DESKTOP=1` with a stubbed Tauri bridge and a spoofed user agent): macOS and Windows chrome, update prompt then badge after Later, editor pill and menu.

- AH-AC1: `App.test.tsx` "AH-AC1 content panel holds header and panes"; screenshots show two tones (panel vs sidebar) in light and dark.
- AH-AC2/3/4/5/6/7/8/9/10: `app-header.test.tsx`, `app-header-task.test.tsx`, `App.test.tsx`, `desktop-chrome.test.tsx` (names as in the scenarios above). Reveal / editor pill verified live only through the stubbed bridge, not a real Tauri build.
- AH-AC11: `app-update.test.ts` (reducer + `pendingUpdate`), `update-badge.test.tsx`; forced-desktop screenshot shows the badge in the sidebar strip (expanded) and in the header (collapsed rail, macOS inset). Not verified against a real updater.
- AH-AC12: `task-detail.test.tsx` "AH-AC12 ..."; screenshot shows only the compact meta row.
- AH-AC13: `App.responsive.test.tsx`; 390px screenshots show toggle, title, menu, terminal, no overflow.
- Round 2 (desktop strip): in the macOS desktop build the 4px sidebar/content split showed the bare transparent body as a bright stripe, because the old `#sidebar-resize-handle` rule never matched (the handle has no id). Now `[data-slot=sidebar-resize-handle]` and `[data-slot=sidebar-inset]` paint `var(--sidebar)`; re-shot macOS dark/light (and Windows) desktop renders show no strip.
- AH-AC14: light and dark screenshots; no clipped corner. macOS vibrancy behind the rounded corner (`[data-slot=sidebar-inset]` made transparent in `desktop-chrome.css`) is unverified on a real macOS window.
- TB-AC1..7: new `App.test.tsx` describe "App tab strip (app-header-and-tabs TB)" plus screenshots (hover, active pill, overflow scroll with the active tab in view, `>_` terminal icon).
- On the branch merged with `origin/develop` (`0ecd4f13`): `bun run typecheck` clean; web vitest 170 files / 2126 tests pass; `task lint` clean; `task test` exit 0 (Go packages, desktop node tests). No Go, Rust or desktop file changed.

Known residual: archiving the selected task keeps showing it (header and pane) until the user navigates; identical to archiving it from the sidebar before this change, so not addressed here.
