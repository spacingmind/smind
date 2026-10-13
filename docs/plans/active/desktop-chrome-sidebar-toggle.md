# Desktop chrome: traffic lights and caption buttons stay clear when the sidebar toggles

Size: M — scope 1 (web UI chrome, no Rust change unless noted), PRs 1, risk 2 (real-macOS layout), decisions 0, verification 3 (real app screenshots).

Goal: the macOS traffic lights (native, overlaid at x=16..76, y-centre 24) and the
Windows/Linux drawn caption buttons never overlap a control or a rail item, in
any sidebar state (expanded / collapsed rail / compact sheet / Settings /
fullscreen), and nothing jumps while the 200ms collapse/expand transition runs.

Human report (2026-10-13, real macOS app): "UI trên macOS khi toggle panel bị lỗi
chỗ 3 nút màu" — chrome breaks around the three coloured buttons when the panel is toggled.

## Findings: how ZCode and Paseo lay out the top bar

Sources: `refs/zcode/packages/{desktop,ui}`, `refs/paseo/packages/{desktop,app}`
(read-only; paths below are relative to those clones).

### ZCode (Electron)

- **macOS window**: `titleBarStyle: "hidden"`, `trafficLightPosition {x:22,y:23}`
  (`desktop/src/main/desktopWindowChrome.ts:140`, `desktopWindowButtonPosition.ts:5`),
  transparent + `vibrancy: "under-window"`. The lights' y is re-derived on page zoom and the
  renderer is told the reserved left padding (`96px / zoom`, `MACOS_TRAFFIC_LIGHT_BASE_LEFT_PADDING_PX`)
  so UI follows the lights, not the reverse.
- **Windows/Linux**: `frame: false`; the renderer draws the caption buttons
  (`ui/src/DesktopWindowControls.tsx`), right inset reserved by a CSS var
  (`WINDOWS_WINDOW_CONTROLS_BASE_RIGHT_PADDING_PX = 136`), bar height 48.
- **One floating top overlay owns the left-of-header controls** (`ui/src/DesktopTopOverlay.tsx`):
  `absolute left-0 top-0 z-20 h-14`, `pointer-events-none` with an inner `pointer-events-auto … [app-region:no-drag]`
  cluster. On macOS its left padding is the lights' reserved width (non-fullscreen only; fullscreen
  falls back to `pl-3`/`pl-5`), and it holds the sidebar toggle + back/forward + new-task. **It is
  mounted independent of sidebar state**; when the sidebar is visible its width is
  `--workspace-sidebar-panel-width`, otherwise it is `w-fit`. So the toggle sits at the same x,
  right after the lights, expanded or collapsed → nothing moves when toggling.
  Windows/Linux put the same toggle (an app-logo button that morphs into the panel icon on hover)
  at `pl-3` on the left and the caption buttons on the right.
- **Collapsed = hidden, not a rail on desktop**: `collapsedSidebarWidthPx = 4` (the panel gutter) — the
  inset content panel then spans the window and its header reserves the overlay's width
  (`WorkspaceHeader.tsx:118` `shouldOffsetHeaderForWindowControls = !isSidebarVisible`, `pl-66`/`pl-58`
  on mac, `pl-48`/`pl-38` in fullscreen). The lights therefore always sit on one uniform surface:
  the sidebar (expanded) or the panel's header (collapsed). `WorkspaceSidebarCollapsedRail.tsx` is a
  36px-high logo/toggle strip only (non-desktop).
- **Drag**: a 4px `[app-region:drag]` strip above the panel group (`WorkspaceShellLayout.tsx:1645`,
  "fixes the macOS window buttons being covered by the header and unclickable"); header + sidebar top
  strip are `[app-region:drag]` with interactive children `no-drag`.
- **Transition**: panel width driven by one CSS var (`--workspace-sidebar-panel-width`) that the overlay
  also reads, so overlay and sidebar can't disagree mid-animation.

### Paseo (Electron + React Native Web)

- **macOS window**: `titleBarStyle: "hidden"`, `titleBarOverlay: true`, `trafficLightPosition {x:16,y:14}`
  (`desktop/src/window/window-manager.ts:17,58-60`) — the same x as smind. Fixed obstruction constants
  `DESKTOP_TRAFFIC_LIGHT_WIDTH = 78`, `DESKTOP_TRAFFIC_LIGHT_HEIGHT = 45` (`app/src/constants/layout.ts:25-26`).
- **Windows/Linux**: custom controls on the top-right, 46px (win) / 36px (linux) × 3, rail height 36
  (`desktop/window-chrome-presentation.ts`); `DesktopWindowControls` rendered at the root, hidden in fullscreen.
- **Explicit corner ownership model** (`app/src/utils/desktop-window.tsx`): the window publishes an
  `obstruction {topLeft, topRight}` (mac → topLeft 78×45; win/linux → topRight 138×36; fullscreen → none)
  and a `WindowChromeRegion corners` context says which surface currently owns which corner.
  `WindowChromeSafeArea` pads exactly the owned obstruction (`placement: "inline"` = padding, `"below"` = a spacer
  of the obstruction's height). Sidebar visible → it owns the top-left (its content starts *below*
  the 45px strip: `sidebarHeaderGroupBelowChrome`); sidebar hidden → content header owns top-left and pads 78px.
- **The sidebar toggle is window-level on macOS** (`app/_layout.tsx:581-590`, `desktop-sidebar-layout.ts:22-37`
  `sidebarToggleOwner: "window"`): rendered once, `inline` safe-area padded past the lights, never inside the
  sidebar or the header; the header's own toggle becomes an invisible same-size placeholder
  (`sidebar-menu-toggle.tsx:96-103`) so header content doesn't reflow. Same x in every state.
- **Hidden sidebar**: no icon rail on desktop; the content owns top-left.
- **Drag**: VS Code-style static `TitlebarDragRegion` (absolute full-size `-webkit-app-region: drag` as the
  first child of the bar; interactive children get `no-drag` from a global CSS backstop; a 4px no-drag top
  resizer on Windows/Linux only).
- **Compact/mobile**: sidebar is a modal sheet; `WindowChromeSafeArea placement="below"` pushes sheet
  content under the (mac) obstruction.

### Common pattern (what we port)

1. A **fixed-position** sidebar toggle immediately right of the lights, independent of sidebar state
   (ZCode overlay / Paseo window-level toggle). The thing that moved in smind was the toggle *and* the
   surface under the lights.
2. The lights always sit on **one surface** (never straddling the sidebar/panel seam).
3. The reserved zone is a single named constant (96px / 78px) the whole UI derives from; fullscreen drops it.
4. Windows/Linux: caption buttons reserved on the right, toggle on the left.

## smind before this change (diagnosis)

(filled in from the real-macOS baseline screenshots — see Validation)

## Decisions

(none yet)

## Acceptance criteria

(to be written once the baseline is captured)

## Progress

- [x] Refs research.
- [ ] Baseline screenshots (real macOS app build of origin/develop).

## Validation

(pending)
