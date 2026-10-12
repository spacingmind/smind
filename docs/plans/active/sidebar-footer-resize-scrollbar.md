# Sidebar footer actions, smoother resize/toggle, themed scrollbars

Size: M (6) — scope 2 (sidebar + shell + CSS), PRs 1, risk 2 (replaces the
sidebar's resize primitive), decisions 0 new (all derived from refs; the two
judgement calls are recorded below), verification 2 (screenshots + frames).
Web UI only; no daemon/Go change.

Human ask (2026-10-12): "polish smind UI: settings pushed to the bottom of the
sidebar like Paseo (add project, usage, host, help & support, settings) and
ZCode (user name, mobile control, settings); the resize-by-mouse / toggle
animation is bad -- check refs and follow them, keep it simple; custom
scrollbars to match."

Sources (read, not copied): Paseo `packages/app/src/components/left-sidebar.tsx`
(`SidebarFooter`: one icon line, `border-t`; `SidebarHelpMenu`; `SidebarHostPicker`;
footer *rows* slot above the line used by Usage/plugins),
`sidebar-resize-handle.tsx` (direct width tracking, no transition while dragging,
hover highlight after 100ms); ZCode `WorkspaceSidebarFooter.tsx` (profile row →
menu with theme etc. + settings icon), `app-shell/WorkspaceShellLayout.tsx`
(width in a CSS var written straight to the DOM during drag, React state
committed on release, `transition-[width,opacity] duration-200 ease-out` on the
panel switched off by a `data-…-resizing` attribute while dragging, pointer
capture, keyboard ±step/Home/End), `styles.css` (global themed scrollbar,
transparent track, rounded thumb), Paseo `styles/web-scrollbar.ts` (8px,
resting opacity 0.4).

Out of scope: the Usage plan modal / footer usage summary (ADR-0028 PR-D1/D2 —
only the slot is created here, empty); a user avatar/name (smind has no
accounts); "mobile control" (the relay/mobile pairing is Phase 3 and has no
UI entry yet — not added, would be a dead button); i18n; `components/composer/*`.

## Why the current resize/toggle feels bad (measured from the code)

`App.tsx` wraps the shadcn `Sidebar` in a `react-resizable-panels` Panel and
feeds the live width back into React state → `--sidebar-width` on every drag
frame. The shadcn Sidebar's gap + `position: fixed` container both carry
`transition-[width] duration-200 ease-linear`, so during a drag the pointer
and the visible sidebar are 200ms apart (rubber-band lag), and a toggle snaps
the Panel instantly while the sidebar content animates linearly — the two
disagree. Fix = ZCode's model: one panel box, width from a CSS var, no
transition during drag, one ease-out transition on toggle.

## Decisions

- **D1 Footer layout (Paseo icon line + ZCode-style status).** A footer
  region with `border-t`: an optional **rows slot** (empty for now; the
  registry hook ADR-0028 D1/D2 will fill with the footer Usage summary,
  default off) above **one icon line**: `Add workspace · Usage · Host` … spacer
  … `Help · Settings`. Expanded sidebar = icon line with tooltips (Paseo).
  Collapsed 48px rail = same actions as a vertical icon stack.
- **D2 Providers/Agents rows are folded into the Host popover**, not kept as
  rows: the Host button (Server icon, status dot = connection state, worst of
  connection/provider health) opens a popover with *Connection* (the same
  `STATUS_LABEL` text the header shows), *Providers · N healthy* (→ Settings →
  Providers), *Agents · N* (→ Settings → Agents) and, on desktop, *Daemon /
  Daemon server* (→ Settings sections `daemon`/`connections`, which only exist
  on desktop). Both stay reachable in one click and via the palette
  ("Settings: Providers/Agents" already exist). Rationale: they were two
  full-width rows of low-frequency info; the icon line is the Paseo shape and
  Host is the nearest real analogue of Paseo's Hosts button. Providers health
  fetch (`provider.list`) stays where it is.
- **D3 Usage** opens Settings → Usage (`onOpenSettings("usage")`) until PR-D1's
  modal exists. No footer percentages (ADR-0028 D5: off by default).
- **D4 Help & support** = dropdown (Paseo `SidebarHelpMenu`): Keyboard shortcuts
  (→ Settings → Shortcuts, same as `shortcuts.help`), Command palette,
  Documentation (repo README on GitHub), Report an issue (GitHub issues).
  External URLs open via the platform's existing external-link helper if there
  is one, else `window.open(url, "_blank", "noopener,noreferrer")`. No version
  row on web (no build version is injected); on desktop only if an existing
  helper already exposes it (no new Rust).
- **D5 Theme toggle** moves from the header into the footer. It is a footer
  icon (next to Help) rather than a menu inside Host/Settings, because it is a
  one-click toggle today and the Settings → Appearance section already holds
  the full control. Header keeps only logo + wordmark (+ traffic-light inset on
  macOS desktop).
- **D6 "+" in the Workspaces group header stays** (it is the contextual create
  affordance next to the list) **and** the footer gets Add workspace (Paseo
  parity); both open the same `CreateWorkspaceDialog`.
- **D7 Resize = ZCode's custom split**, replacing `react-resizable-panels` for
  the *sidebar split only* (the task-pane splits keep it). Collapse target
  stays the 48px icon rail (smind's existing feature), not 0. Snap-to-collapse:
  releasing a drag whose raw width is below `SIDEBAR_SNAP_WIDTH` (144px)
  collapses to the rail; while dragging, width never goes below
  `SIDEBAR_MIN_WIDTH`. Width persisted on release (same `STORAGE_KEYS.sidebarWidth`).
- **D8 Scrollbars = one global ::-webkit-scrollbar rule set** (all platforms),
  with a `@supports not selector(::-webkit-scrollbar)` Firefox fallback using
  `scrollbar-width: thin; scrollbar-color`. Why webkit pseudo-elements first:
  the existing Windows QA finding (arrow buttons cannot be removed with the
  standard properties) still holds and Tauri macOS WebKit supports them. Thumb
  uses the foreground token at low alpha (themes in light + dark), brighter
  when the scroller or the thumb is hovered; 8px with a 2px transparent frame.
  The Windows-only block is generalised, not duplicated.

## Acceptance Criteria

### F — footer
- SFR-F-AC1: the sidebar header no longer contains the theme toggle or the Settings button (expanded and collapsed); the wordmark row remains.
- SFR-F-AC2: expanded, the footer renders `border-t` + one icon line with, in order, Add workspace, Usage, Host, a spacer, Help, Theme, Settings; each has an accessible name and a tooltip; no "Providers"/"Agents" rows remain.
- SFR-F-AC3: Settings → opens Settings (no section); Usage → Settings at section `usage`; Add workspace → the create-workspace dialog (same one as the header "+").
- SFR-F-AC4: Host opens a popover showing the connection status text and, once `provider.list` has answered, "N healthy" with a health dot; Providers row → Settings `providers`; Agents row (count = profiles) → Settings `agents`; on desktop only, Daemon and Daemon server rows → `daemon`/`connections`; the popover closes on selection.
- SFR-F-AC5: Host trigger shows a status dot: danger when not connected, else provider-health tone (success/warning/danger), none while providers unknown and connected.
- SFR-F-AC6: Help opens a menu with Keyboard shortcuts (→ Settings `shortcuts`), Command palette, Documentation and Report an issue (external links, `noopener`).
- SFR-F-AC7: the theme control in the footer keeps the existing three options and `aria-checked`.
- SFR-F-AC8: icon-collapsed rail: the same actions render as a vertical icon-only stack (no text), all reachable, no horizontal overflow at 48px.
- SFR-F-AC9: the footer has an empty, documented slot above the icon line (`data-testid="sidebar-footer-rows"` only rendered when it has children) — nothing renders for it today (ADR-0028 D5 default off).
- SFR-F-AC10: compact (<768px) Sheet sidebar renders the same footer.

### R — resize + toggle
- SFR-R-AC1: the desktop sidebar split is one flex row: sidebar panel (width from `--sidebar-width`, or `--sidebar-width-icon` collapsed), handle, content; `react-resizable-panels` is no longer used for it, the shadcn Sidebar has no `fixed` container/gap on desktop.
- SFR-R-AC2: while dragging the handle, the sidebar panel has `data-resizing="true"` and no width transition; width is written to the `--sidebar-width` CSS var on the wrapper from a pointermove handler (rAF-coalesced) with **no React state update per frame**; state + localStorage are committed once on release.
- SFR-R-AC3: toggle (button, Ctrl+B, command) transitions the panel width 200ms `ease-out`; `prefers-reduced-motion: reduce` disables the transition.
- SFR-R-AC4: width clamps to [192, 512]; a release below 144 raw collapses to the icon rail; keyboard on the focused handle: ArrowLeft/Right ±16, Home/End min/max (`role=separator`, `aria-orientation=vertical`, `aria-valuenow/min/max`).
- SFR-R-AC5: the persisted width is restored on reload; `sidebarPx` for the header/desktop chrome follows committed state (open ? width : icon).
- SFR-R-AC6: the handle keeps the 4px hit area + 2px hover/drag indicator (existing look), pointer capture so fast drags don't lose the handle, `cursor: col-resize` while dragging via a body class that is removed on release/cancel/unmount.
- SFR-R-AC7: compact layout (<768px) unchanged: Sheet, no handle.

### S — scrollbars
- SFR-S-AC1: one global scrollbar rule set (index.css) themes every native scroller: 8px, transparent track, rounded thumb from a foreground-alpha token, no arrow buttons, hover-brighter; light and dark both visible.
- SFR-S-AC2: no `scrollbar-width`/`scrollbar-color` outside `@supports not selector(::-webkit-scrollbar)` and the xterm viewport rule (Chromium ignores `::-webkit-scrollbar` if they are set).
- SFR-S-AC3: the xterm viewport scrollbar stays hidden; `.no-scrollbar` still hides.
- SFR-S-AC4: the sidebar list's Radix `ScrollArea` bar, chat timeline, Settings, popovers/menus and dialogs use the same look (no second style).
- SFR-S-AC5: `desktop-webview-css.test.ts` Windows scrollbar guards are replaced with equivalent guards on the global rules (no Windows gate).

## Test Scenarios

`components/app-sidebar-footer.test.tsx` (new):
- `header has no theme toggle or settings button` (F-AC1)
- `expanded footer icon line order and accessible names` (F-AC2)
- `settings, usage and add-workspace actions` (F-AC3)
- `host popover: connection text, provider health, providers/agents rows deep-link` (F-AC4)
- `host status dot: disconnected is danger, partial provider health is warning` (F-AC5)
- `host popover shows daemon rows only on desktop` (F-AC4)
- `help menu items and external links` (F-AC6)
- `theme options in the footer` (F-AC7)
- `collapsed rail footer is a vertical icon-only stack` (F-AC8)
- `footer rows slot renders nothing when empty` (F-AC9)

`hooks/use-sidebar-resize.test.tsx` (new, + `use-sidebar-width.test.ts` kept):
- `dragging sets data-resizing and writes --sidebar-width without a React commit` (R-AC2)
- `release commits and persists the clamped width` (R-AC2, R-AC4)
- `release below the snap width collapses` (R-AC4)
- `keyboard resize: arrows, Home, End` (R-AC4)
- `pointercancel restores state and body class` (R-AC6)

`App.test.tsx` / `App.responsive.test.tsx` (re-baselined from react-resizable-panels):
- `toggle swaps the panel to the icon width var and keeps the transition class` (R-AC3)
- `sidebar handle present on desktop, absent in compact` (R-AC1, R-AC7)
- the old "icon-collapsing shrinks its ResizablePanel" test is rewritten to assert the panel's width var.

`test/desktop-webview-css.test.ts` / new `test/scrollbar-css.test.ts`:
- `global ::-webkit-scrollbar rules exist and are not desktop-gated` (S-AC1)
- `no scrollbar-width/scrollbar-color outside the @supports fallback and xterm` (S-AC2)
- `thumb colour comes from a theme token, no hex` (S-AC1; existing no-hardcoded-colors test keeps passing)

Visual (not unit-testable; recorded under Validation): light + dark at 1440×900
(sidebar 256 and narrow 192, plus collapsed rail) and 390×844 — footer default /
hover / Host popover / Help menu / collapsed rail; scrollbars in a long sidebar
list and a long chat; resize + toggle frame sequences before/after.

## Steps (one commit each)

1. Spec (this file).
2. Footer: `components/sidebar-footer.tsx` (+ test), remove header Theme/Settings and the Providers/Agents rows from `app-sidebar.tsx`, thread `connectionStatusText` prop from `App.tsx`.
3. Resize: `components/ui/sidebar.tsx` non-fixed desktop container, `hooks/use-sidebar-resize.ts`, `App.tsx` split + handle, tests.
4. Scrollbars: `index.css`, `components/ui/scroll-area.tsx`, tests.
5. Screenshot/frame verification, fixes, PR.

Write scopes: step 2 = `components/app-sidebar*.tsx`, `components/sidebar-footer*`; step 3 = `components/ui/sidebar.tsx`, `hooks/use-sidebar-*`, `App*.tsx`; step 4 = `index.css`, `components/ui/scroll-area.tsx`, `test/*css*`. Disjoint except `App.tsx` (step 2 passes one prop; done before step 3 starts).

## Progress

- [ ] 1 spec  - [ ] 2 footer  - [ ] 3 resize  - [ ] 4 scrollbars  - [ ] 5 verify + PR

## Validation

(filled at the end: AC → evidence, screenshot paths, frame notes)
