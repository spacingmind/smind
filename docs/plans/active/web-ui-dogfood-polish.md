# Web UI polish: composer redesign + flexible tabs + settings screen (dogfood feedback 2026-09-18)

## Context

First real dogfood session on the merged visual-identity pass (#152) produced five concrete feedback items. Four are layout/IA gaps against paseo parity; one is a composer visual redesign to the reference screenshot (a paseo-style agent composer: status pills above, selectors and run controls inside the input's bottom toolbar).

Current state, confirmed by reading the code:
- `app-sidebar.tsx:410-421`: theme toggle + settings + (collapsed) menu buttons in `SidebarHeader`. When the sidebar is icon-collapsed via the rail toggle, the header row hides its children (`group-data-[collapsible=icon]:hidden`) but the row and its padding still occupy vertical space — the "space is still there" complaint.
- `App.tsx` task pane: `TabsContent` fills the pane width with no max-width; on wide monitors the chat column stretches edge-to-edge.
- `tab-registry.tsx:53-57` `defaultTabsForTask` hardcodes 4 non-closable tabs (`task`, `files`, `diff`, `terminal`) per task; users cannot close or choose them.
- `settings/settings-screen.tsx:63-67`: settings is a `Dialog` popup.
- `composer/composer.tsx`: functional but visually a labeled form (Provider/Approval policy labels + selects beside the textarea, Send/Stop to the right); the reference screenshot wants a self-contained input card: status pills (diff stat, subagent count) above the box, selectors + mic/stop inside a bottom toolbar of the box itself.

## Reference patterns (refs/paseo, read 2026-09-18)

- Settings as a **screen**: `app/settings/index.tsx` + `[section].tsx` (expo-router screens), `navigation/settings-navigation.ts` (push/dismiss routing, root → section drill-down). For smind's web UI this maps to a tab/screen route (`settings` view state in App, or a dedicated route) with a section nav rail, replacing the Dialog — the existing `settings-registry.ts` sections (appearance/general) carry over unchanged.
- Composer pills: `composer/diff-stat-pill.tsx` — a compact pill (additions green / deletions red) rendered above the input, clickable to open the diff. smind already renders the same numbers in the diff tab; surfacing them at the composer is a placement change, not new data.
- Composer input: `composer/input/input.tsx` — single rounded input card; mode/tool selectors live in a toolbar at the bottom edge of the card (not beside it as labeled form fields).

## Acceptance Criteria

### Item 1 — icon-collapsed sidebar leaves no dead space
When `Sidebar` is icon-collapsed, the header area reserved for theme/settings buttons collapses with it (no empty row of padding). The buttons must remain reachable — either rendered as icon-only stack in collapsed mode, or moved so nothing they offered is lost (keyboard palette still opens settings is not sufficient alone; there must be a visible affordance in collapsed state too).

### Item 2 — chat tab max-width + center on wide screens
The chat timeline content column gets a max-width (e.g. `max-w-3xl`/`max-w-4xl`, tune to taste) and `mx-auto` when the viewport exceeds it. Applies to the chat tab's scrollable content only — files/diff/terminal panes keep full width. The composer (Item 5's input card) aligns to the same column width.

### Item 3 — flexible tabs per task
Tabs become user-owned: closing the last tab of a task shows an empty state (with quick "open Chat/Files/Diff/Terminal" buttons) instead of forcing 4 tabs; a "+" affordance (or the empty state's buttons, or both) lets the user (re)open any tab kind for the task. `defaultTabsForTask` may still seed the initial set on first visit, but nothing is non-closable anymore. Tab state persists per task like today (existing tab persistence mechanism carries over).

### Item 4 — settings as a screen, not a popup
`settings-screen.tsx` stops being a Dialog. It becomes a full-pane screen (same surface as the task pane area) with a section nav (Appearance / General today, extensible via `settings-registry.ts`), opened from the sidebar settings button and closable back to the previous view (Esc / back affordance). Follows paseo's root→section pattern but web-simple: one screen, section rail, no router library introduction.

### Item 5 — composer redesign to the reference input card
Reshape `composer.tsx` into the reference card anatomy:
- **Above the box**: contextual status pills — diff stat pill (`+N −M`, green/red, click → open/switch to Diff tab) when the task has changes; subagent/run pill when a run is live ("1 running…" — data available from existing run state; keep honest to what smind exposes today, do not invent subagent data smind doesn't have).
- **The box**: rounded input card (border, `bg-surface-2`-tier), autogrow textarea inside, placeholder mentions current affordances honestly ("Message the agent…" — no @files//commands claims until those exist).
- **Bottom toolbar inside the card**: `+` (no-op placeholder until attachments exist — omit entirely if a dead button violates the codebase's no-placeholder rule, decide during implementation), provider selector (compact, label-less, icon or short id), approval-policy selector (compact), spacer, Stop (red, only while running) / Send. Selects keep their accessible labels via aria-label now that the visible `<label>` text moves out of the toolbar.
- Keep all existing behavior: draft persistence, queue-while-running, Escape-stops, provider.list fallback, disabled states with reasons, testids (tests must keep passing — update assertions where visual structure legitimately changed).

## Test Scenarios

- Item 1: existing app-sidebar tests extended — collapsed mode asserts no phantom header gap (or buttons render collapsed); snapshot-free, class/structure assertions.
- Item 2: chat content wrapper has max-width class; other panes unaffected (assert on rendered class of the chat scroll container vs file pane).
- Item 3: tab-registry tests — all tabs closable; closing last tab → empty state with reopen buttons; "+"/reopen adds tab of chosen kind; persistence keeps user's set across task switches.
- Item 4: settings-screen tests rewritten from Dialog-open assertions to screen-render assertions; sidebar settings button navigates; Esc returns.
- Item 5: composer tests updated for new structure (pills, in-card toolbar, label-less selects with aria-labels); new test: diff pill renders when task has changes and click switches to diff tab; stop/send behavior assertions preserved.
- Manual: dogfood pass in browser, both themes, wide + narrow.

## Decisions

- Settings screen is a view-state screen in App (no router lib) — matches "smallest coherent change" and paseo's pattern translated to web.
- Item 5 pills show only data smind actually has (diff stat yes; subagent count only when smind exposes it — otherwise omit, never fake).
- Item 1 solution shape (collapsed icon stack vs remove) left to implementation, gated on "no dead space + still reachable".

## Progress

- [x] Item 1 — collapsed sidebar header dead space
- [x] Item 2 — chat column max-width + center
- [ ] Item 3 — user-owned flexible tabs
- [x] Item 4 — settings screen
- [ ] Item 5 — composer input card redesign
- [ ] `task test` / `task lint` green
- [ ] Manual dogfood pass

## Validation

Track 1 (Items 1, 2, 4), 2026-09-18:

- `cd web && bun run --filter '@smind/ui' test` — 764 tests / 70 files, all passing. New/updated coverage:
  - Item 1: `app-sidebar.test.tsx` "AppSidebar collapsed header" — collapsed mode renders a `flex-col` icon stack (`sidebar-collapsed-header-actions`) with ThemeToggle/settings/accounts reachable; expanded mode asserts the CSS-variant swap (both variants live in the DOM, Tailwind `group-data-[collapsible=icon]:hidden` / `hidden` classes asserted since jsdom loads no stylesheets).
  - Item 2: `App.test.tsx` "App chat column" — chat tab's `run-log-column` has `mx-auto max-w-3xl`; Files pane root has no `max-w` class.
  - Item 4: `settings-screen.test.tsx` rewritten for the screen (no dialog role, Esc + Back navigate back); `app-sidebar.test.tsx` entry-point tests assert `onOpenSettings`; `App.test.tsx` "App settings view" — sidebar button swaps main area to the screen (with and without a selected task), Back/Esc return to the task view.
- `task lint` (go vet + gofmt) — clean.
- Not yet done: `task test` full Go suite (no Go files touched); manual dogfood pass (Track-wide, pending).
