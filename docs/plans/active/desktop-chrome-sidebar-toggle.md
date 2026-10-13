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

Baseline = origin/develop (e8f30640), rendered from the desktop-flavoured web build with a stubbed
Tauri bridge + a drawn 3-light overlay at x=16..76 (the real app could not be rendered: the macOS
session was locked, see Validation). Measured on a 1280×800 viewport:

1. **Collapsed rail: the lights straddle the rail and the content panel.** Rail is 48px; the lights end at
   x=76, so the yellow light sits on the seam: the panel's `border-l`, its rounded top-left corner and the
   surface change (vibrancy sidebar → opaque panel) all run through the cluster. (ZCode/Paseo never have
   this: they hide the sidebar, so the lights are always on one surface.)
2. **The toggle jumps and loses focus.** Expanded it lives at the right end of the sidebar's top strip
   (x=220); collapsed it is re-mounted in the header at x=93. Flipping `toggleInSidebar` unmounts the button
   the user just activated (keyboard focus lost) and the header inset flips 0→36px instantly while the sidebar
   width animates for 200ms, so the title/toggle jump rather than slide.
3. Compact sheet / Settings / fullscreen were already clear of the lights (header pads 84, Settings strip is
   empty), no change needed beyond keeping them intact. Windows/Linux: no lights; only the toggle jump (2).

## Decisions

All within the existing model (collapsible="icon" rail, inset panel); no new architecture, no ADR.

- D1. Port the shared ZCode/Paseo pattern: in the desktop build (non-compact) the sidebar toggle is ONE
  window-level element at a fixed x, mounted outside the sidebar and the header, so it never moves, never
  re-mounts (focus survives) and nothing reflows under it. macOS: x=84 (right after the lights);
  elsewhere/fullscreen: centred in the 48px rail (x=10). The sidebar strip and the header pad around it.
- D2. macOS collapsed rail: the title bar (top 48px) is ONE panel-coloured bar spanning the window with the
  rail starting BELOW it; the seam/rounded corner start below the bar. The bar is painted by an absolutely
  positioned strip inside the animated sidebar panel wrapper, so it is uniform across the lights for the whole
  200ms transition. Not done: hiding the rail on desktop like ZCode/Paseo -- the rail (#314) is a deliberate
  smind feature.
- D3. The header's left inset animates with the same 200ms ease-out as the sidebar width, so the title slides
  monotonically instead of jumping and can never pass under the toggle zone.
- D4. Browser build and compact sheet keep today's behaviour (toggle in the sidebar strip / header).

## Acceptance criteria

- DC-AC1: desktop non-compact, any sidebar state: exactly one sidebar toggle in the DOM, at the same x and y
  before, during and after collapse/expand (macOS x=84, else x=10) and it is the same DOM node (focus persists).
- DC-AC2: macOS with lights: no interactive element, rail item, or logo intersects the lights rect
  (x 16..76, y 17..31) in expanded, collapsed rail, mid-toggle (sampled per frame), Settings and compact
  states; the surface under the lights is one colour (no seam/corner/border inside x 0..84, y 0..48).
- DC-AC3: collapsed rail starts below the 48px bar: rail items' top >= 48; the bar's bottom border lines up with
  the header's.
- DC-AC4: during the 200ms toggle the header title's x moves monotonically (no jump) and never goes below
  the toggle zone end (macOS 120, else 56).
- DC-AC5: fullscreen (no lights) reclaims the zone: toggle at x=10, sidebar strip pad 46, header inset 0,
  and the bar/seam override is off (panel keeps its rounded corner).
- DC-AC6: Windows/Linux: caption buttons stay the last items of the header's right edge and do not overlap the
  header actions at 1280 and 640 wide; the sidebar strip and the header strip stay window drag regions.
- DC-AC7: drag regions: the sidebar top strip is a deep drag region on every desktop OS; the toggle itself is a
  button (never a drag start).
- DC-AC8: browser build and compact layout unchanged (existing tests untouched).

## Test scenarios

- T1 `window-chrome.test.ts`: `toggleLeftPx`, `toggleZoneEndPx`, `headerLeftInsetPx` (reserveToggle true/false;
  lights/no lights/fullscreen; rail 48 vs expanded 256 vs 0).
- T2 `desktop-chrome.test.tsx`: header paddingLeft per OS/fullscreen/sidebarPx; the window toggle renders once
  at the OS's left; sidebar strip paddingLeft; rail bar present only with lights + collapsed + not compact.
- T3 App-level: toggling keeps the same toggle node (focus not lost).
- T4 harness (Playwright, desktop build + stubbed bridge, mac/win/linux × light/dark): the probe in
  `/tmp/smind-tl/harness/shoot.mjs` (hits against the lights rect, per-frame traces) - results in Validation.

## Progress

- [x] Refs research.
- [x] Baseline diagnosed (stubbed bridge render).
- [ ] Implementation.
- [ ] Real-macOS screenshots (blocked while the session is locked).

## Validation

(pending)
