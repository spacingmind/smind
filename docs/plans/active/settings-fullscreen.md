# Settings as a full-screen screen with its own sidebar

Size: M (4) — scope 1, PRs 0, risk 1, decisions 0, verify 2

Goal (user, 2026-10-12): opening Settings replaces the *whole* shell (app
sidebar + main view) with a dedicated Settings screen that has its own
layout — left settings sidebar + main content view — like Paseo and ZCode.
Today `SettingsScreen` is a sibling of the task pane inside the shell, so
the app sidebar stays visible.

References (read-only, ported not invented):
- Paseo `packages/app/src/screens/settings-screen.tsx`: desktop = 320px
  sidebar (back-to-workspace row at the top, `TitlebarDragRegion` above it,
  grouped list) + detail pane with a title header; compact = root is the
  list, a section is a pushed detail page with a back header.
- ZCode `packages/ui/src/SettingsPage.tsx`: `grid-cols-[268px_1fr]`, `h-12`
  drag strip above a ghost "Back to workspace" row, group labels
  (`text-ui-sm font-medium text-foreground-subtlest`), `h-8` nav rows,
  content in a framed panel (`border bg-background rounded-xl`, 4px inset),
  `h-12` drag header, scrolling `main` with a max-width content column.

## Decisions

1. **Still view state, not a route.** Same as the dogfood-polish plan
   ("no router library introduction"); `activeView: "workspace" | "settings"`
   in `App.tsx` stays. No ADR: no data model / public API change.
2. **Hide, never unmount, the shell.** `AppShell` renders the whole existing
   shell (SidebarProvider tree) in a wrapper that, while Settings is open,
   gets `invisible` + `inert` + `aria-hidden`, and renders `SettingsScreen`
   as an absolutely-positioned full-viewport sibling. Rationale: the task
   pane (live chat stream, terminals, scroll, split sizes) keeps its React
   state; `visibility:hidden` keeps layout/size so `react-resizable-panels`
   does not see a 0-size group and snap the sidebar (which `display:none`
   would risk); `inert` stops focus/clicks reaching the hidden shell.
   Radix portals (dialogs, command palette, quick open) still render on top.
3. **Same `SettingsScreen` props** (`client, events, onNavigateBack,
   initialSectionId`) and test ids (`settings-screen`, `settings-back-button`,
   `settings-nav-<id>`, `settings-section-*`) — every existing section and
   its tests keep working; the registry (`settings-registry.ts`) is untouched.
4. **Layout owned by the screen.** Desktop (≥768px, `useIsMobile()` false):
   `settings-sidebar` (fixed width, border-r, sidebar surface) = `h-12` drag
   strip → "Back to app" row → scrolling grouped nav; right = framed content
   panel with an `h-12` header (drag region, section title) and a scrolling
   body with a max-width column. Narrow (<768px): root = full-screen list
   (header "Settings" + back-to-app, grouped nav as rows); tapping a row
   opens the section as a detail page (header with back arrow → list +
   section title). Active section is screen-local state; opening with an
   `initialSectionId` on narrow lands directly on that detail.
5. **Window chrome.** In the desktop build the sidebar's top strip and the
   content header carry `data-tauri-drag-region="deep"`; on macOS the strip
   sits where the traffic lights are (back row is below it); on narrow
   layouts the full-width header pads with `headerLeftInsetPx(os,
   fullscreen, 0)`; Windows/Linux render `DesktopWindowControls` at the end
   of the content header. macOS vibrancy sidebar styling is extended to the
   settings sidebar in `desktop-chrome.css`.
6. **Escape** closes Settings only when nothing else consumed it: bubble-phase
   listener, ignores `defaultPrevented` events and any open
   `[role=dialog|alertdialog|menu|listbox]`. (The old capture-phase handler
   fired even with a dialog on top.) The Cmd+, `settings.open` shortcut keeps
   opening; pressing it while open is a no-op (already there).
7. **Shell-affecting commands leave Settings.** Command-palette entries that
   change the selection (task/tab/chat) call `closeSettings` first, so
   they are not applied invisibly.
8. PR-D1 (Plan usage block in Usage) must not be blocked: sections still
   render into the content body unchanged; no section-level API changes.

## Acceptance criteria

- AC1 Opening Settings (sidebar button, Cmd+, shortcut, `smind:open-settings`
  event, shortcuts help, Providers entry points, desktop-unreachable
  "switch connection") shows a screen that covers the entire viewport: the
  app sidebar and the main view are not visible nor focusable.
- AC2 The Settings screen has its own left sidebar (back-to-app control at
  the top, then the registry's sections grouped by `groupLabel`, active
  highlighted with `aria-current`) and a content view for the active
  section.
- AC3 Closing (Back button, Escape with no dialog open) restores the exact
  prior shell: same selected task, same open tabs/panes, sidebar
  collapsed/width unchanged.
- AC4 The task pane is not unmounted while Settings is open: the same DOM
  node (and its chat state) is present before, during and after.
- AC5 Escape with a dialog/menu open inside Settings does not close
  Settings.
- AC6 Narrow viewport (<768px): list → detail drill-down; back from detail
  returns to the list (Escape/back-to-app from the list closes Settings).
- AC7 Desktop chrome: drag regions on the sidebar strip + content header;
  macOS inset clears traffic lights on the narrow header; Windows/Linux
  show window controls on the settings screen.
- AC8 Registry model untouched: a section registered via
  `registerSettingsSection` appears in the nav and renders; all existing
  section tests pass unchanged.
- AC9 Light + dark screenshots at 1440×900 and 390×844 reviewed (shell,
  Settings ×2–3 sections, narrow list + detail).

## Test scenarios (named)

`settings-screen.test.tsx` (SettingsScreen alone)
- `SettingsScreen layout > renders a settings sidebar with a back control above the grouped nav and a content view`
- `SettingsScreen layout > marks the active section with aria-current and switches content on nav click`
- `SettingsScreen layout > Back button calls onNavigateBack`
- `SettingsScreen layout > Escape calls onNavigateBack when no dialog is open`
- `SettingsScreen layout > Escape does not call onNavigateBack when a dialog is open`
- `SettingsScreen layout > Escape does not call onNavigateBack when the event was default-prevented`
- `SettingsScreen narrow > shows only the section list first, then a detail page after tapping a row`
- `SettingsScreen narrow > back from the detail returns to the list, and back from the list calls onNavigateBack`
- `SettingsScreen narrow > initialSectionId lands directly on that section's detail`
- `SettingsScreen desktop chrome > the sidebar strip and content header are drag regions in the desktop build`
- `SettingsScreen desktop chrome > renders window controls on Windows/Linux and pads the narrow header on macOS`
- existing shell/nav-order/section tests keep passing (AC8).

`App.test.tsx`
- `App settings view > opening settings hides the shell (inert, aria-hidden) but keeps the task pane mounted` (same node identity before/during/after)
- `App settings view > Back restores the previous task/tab and sidebar state`
- `App settings view > Escape from the settings screen returns to the task view`
- `App settings view > a command-palette task pick while in settings leaves settings`
- existing "settings view shows with no task selected" test keeps passing.

## Review round 1 (Supervisor, PR #297) — changes

- **Shell shortcuts inert while Settings is open.** New `useActionRestriction(allowed)` in the keyboard provider narrows dispatch (keys, native menu, palette `runAction`) to an allow-list; a restricted key is left to the browser (no `preventDefault`). `App.tsx` restricts to `SETTINGS_SAFE_ACTIONS` = `palette.open`, `shortcuts.help`, `settings.open`, `theme.cycle`, `app.quit`. Tests: `useActionRestriction` (2, provider), `App settings view > shell shortcuts are inert while settings is open: task.next does not change the selection` (verified to fail without the restriction), `settings-safe shortcuts still work over settings`.
- **Nav grouping (Paseo-style):** ungrouped app sections first — General · Appearance · Notifications (110) · Shortcuts (120) — then the Connection group (desktop only: Connections, Daemon), then Agents & providers (Agents, MCP servers, Providers, Usage). A divider ends a group if an ungrouped section is registered after one. Tests: nav-order, "Notifications and Shortcuts precede the Agents & providers heading", separator.
- **No duplicate titles/actions:** `settings-title.tsx` (`SettingsSectionTitle` + `SectionTitleInHeaderProvider`). The screen provides `titleInHeader`; sections with a name-repeating `<h3>` (Agents, MCP servers, Providers, Usage, Notifications, Daemon) drop it and keep their actions (and Usage's description) right-aligned; rendered standalone they keep the title. Agents/MCP empty states lost their second "New …" CTA — the header button is the single primary action.
- **Content column:** centred `max-w-4xl px-8` (ZCode's `SETTINGS_FRAME_CONTENT_CLASSNAME`; Paseo centres 720px) owned by the screen, identical for every section (gutter at 1440 ≈ 174px, was ≈ 230px).
- **Known limitation (accepted):** crossing the 768px breakpoint remounts the active section (desktop ↔ narrow are different trees), so a section's unsaved local state (an open form) is lost on resize.

## Progress

- [x] Spec (this file)
- [x] S1a Peer (glm): two-column desktop layout — ACCEPT (7303fb1)
- [x] S1b Peer (glm): narrow drill-down + desktop chrome — ACCEPT (4c5a5e9, a0343ae)
- [x] S2 Lead: `App.tsx` keep-mounted overlay + palette hook + App tests (605cecb)
- [x] S1c Peer (glm): section nav icons — REJECT as delivered (hit turn limit, uncommitted, stray `m` before `order:` in 6 files); Lead fixed + committed
- [x] Screenshots light/dark at 1440×900 and 390×844 (found + fixed: ragged label alignment without icons; empty 48px strip above the back row in a browser)
- [x] Review round 1 changes above (commits after a0343ae)
- [x] `task test` (web 1682/1682, Go + desktop scripts), `task lint`, `bun run typecheck`

Deviations from the spec: Decision 6 also lets Escape on a *narrow detail*
pop back to the list before closing; `settings.open` while open is a no-op;
`SettingsScreen` is keyed by an open-request counter so a deep link
(`shortcuts.help`) while Settings is open lands on its section.

## Validation

- AC1 App.test `opening settings hides the shell (inert, aria-hidden) but keeps the task pane mounted`; screenshots `wide-*-2-settings-*` show no app sidebar/main view.
- AC2 settings-screen.test `renders a settings sidebar with a back control above the grouped nav and a content view`, `marks the active section with aria-current…`; screenshots.
- AC3 App.test `Back restores the previous task/tab and sidebar state` (collapsed sidebar, Files tab, sidebar.toggle disabled while open); `wide-*-5-restored` matches `wide-*-1-shell`.
- AC4 App.test: same `primary-pane` node before/during/after (shell is `invisible`+`inert`, never unmounted).
- AC5 settings-screen.test `Escape does not call onNavigateBack when a dialog is open` / `…default-prevented` / `…dialog that sits inside an inert subtree`.
- AC6 settings-screen.test `SettingsScreen narrow` (list → detail, back, initialSectionId, Escape); screenshots `narrow-*-2-list`, `-3-detail-appearance`, `-4-list-again`.
- AC7 PARTIAL: settings-screen.chrome.test (drag regions, window controls on Windows/Linux, macOS left inset), desktop-chrome.test (macOS opaque panes + vibrancy sidebar hook). **Not verified in a real Tauri window** (no headless desktop run available); plan stays in `active/` for that reason.
- AC8 registry untouched; all 11 settings test files pass (123 tests).
- AC9 (round 2 shots) `tmp/screenshots/` (gitignored) — wide/narrow × light/dark, reviewed.
