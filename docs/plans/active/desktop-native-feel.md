# Desktop native feel (ZCode-style chrome + de-webview)

Goal (user, 2026-10-10): the Tauri desktop app should stop feeling like
"a web UI in a window" and feel like ZCode does. ZCode itself is not a
native-toolkit app: it is Electron + a bundled React/Tailwind/shadcn
renderer (`refs/zcode/packages/ui`) with a scoped preload bridge. smind
already has the equivalent architecture (ADR-0013, plan
`completed/desktop-bundled-ui.md`): bundled UI, loopback proxy,
per-command IPC allowlist (`desktop/src-tauri/capabilities/proxy.json`),
`isDesktop` build flag (`web/packages/ui/src/lib/platform.ts`). What is
left is the visible layer. The user chose this over a native-toolkit
rewrite (≈28k lines of UI) and over moving to Electron.

This plan also carries the unfinished **P5 (desktop-only chrome)** of
`completed/zcode-visual-parity.md`, which was left "blocked on ADR-0013";
ADR-0013 has since shipped, so P5 is unblocked and lives here now.

References (ZCode is Apache-2.0: keep `NOTICE` attribution for anything
ported):

- `refs/zcode/packages/desktop/src/main/desktopWindowChrome.ts`
  (`buildDesktopWindowVisualOptions`: per-platform window options)
- `refs/zcode/packages/ui/src/DesktopWindowFrame.tsx`,
  `DesktopTopOverlay.tsx`, `WorkspaceHeader.tsx`, `WindowsTopLeftLogo.tsx`
- `refs/paseo/packages/app/src/keyboard/keyboard-shortcuts.ts`
  (`when: { desktop }` gating)
- local research note `docs/research/local/zcode-desktop-2026-09.md`
  table (b)

## Acceptance Criteria

### D1 — Window chrome

1. **macOS:** the main window uses an overlay title bar (Tauri
   `TitleBarStyle::Overlay` + hidden title). The traffic lights sit
   inside smind's own top header row, vertically centred on it, and no
   native title text or title-bar strip is visible.
2. **Windows and Linux:** the main window is undecorated
   (`decorations: false`). smind draws its own minimize,
   maximize/restore and close buttons at the right end of the header
   row. Maximize shows a "restore" glyph while the window is maximized
   and switches back when it is restored (including via double-click,
   OS snap, or keyboard).
3. **Close keeps today's semantics:** the drawn close button behaves
   exactly like the native close did before. It goes through the
   existing close-requested → hide-to-tray path
   (`desktop-tauri-shell` AC3). It does not quit the app.
4. **Dragging:** the empty area of the header row drags the window
   (`data-tauri-drag-region` or `startDragging`), and double-clicking it
   maximizes/restores (on macOS: follows the system "double-click title
   bar" setting, if Tauri exposes it; otherwise zoom). Buttons, inputs,
   tabs and menus inside the header are **not** drag regions and stay
   clickable.
5. **Resizing** still works from every window edge and corner on all
   three platforms after removing decorations.
6. **Header layout:** in the desktop build the app's top header
   (`App.tsx` `headerElement`, and the sidebar's top edge when it is
   expanded) reserves space for the traffic lights (macOS, left) or the
   caption buttons (Windows/Linux, right). Nothing overlaps them at any
   sidebar state (expanded, collapsed, mobile sheet hidden) or window
   width down to the existing minimum.
7. **Web build is unchanged:** none of D1 renders, and no
   `@tauri-apps/api` import is reachable, when `isDesktop` is false.
   Window-control calls go through `lib/platform.ts`, like the existing
   desktop API.
8. **Materials (best effort, per platform):**
   - macOS: sidebar vibrancy (`Effect::Sidebar` or equivalent) in both
     light and dark.
   - Windows 11: Mica or Acrylic behind the window; plain opaque on
     Windows 10.
   - Linux: opaque (no transparency). ZCode disables system shadow on
     Linux for frameless windows; do the same only if a double outline
     shows up in testing.

   Main content surfaces stay opaque token colours. Only the sidebar and
   header may show the material. Text contrast stays within the
   existing token guards.

### D2 — Remove webview-isms

1. **No white flash on launch:** the main window is created hidden with
   a background colour matching the current theme surface. It is shown
   once the bundled UI signals it has painted, or after a ≤3 s fallback
   timeout so a broken UI still shows a window. The offline/splash page
   shows immediately as today.
2. **Context menu:** the webview's default context menu (Back / Reload /
   Inspect…) never appears in a release build. Right-click inside an
   editable field (input, textarea, the file editor, the composer) or
   on a non-empty text selection still offers native-style
   Cut/Copy/Paste/Select All. Elsewhere, right-click does nothing unless
   smind has its own context menu there (e.g. the sidebar's). DevTools
   stays reachable via the existing View → Toggle DevTools item in
   debug builds.
3. **Selection:** app chrome (sidebar, headers, tab strips, buttons,
   menus, pane headers, status pills) is not text-selectable, so
   Cmd/Ctrl+A or a drag across the chrome no longer highlights the whole
   UI. Conversation text, code blocks, tool output, diffs, the file
   editor and the terminal stay selectable.
4. **No rubber-band overscroll** on the document root. Inner scroll
   containers still scroll normally.
5. **Zoom only via smind's own zoom.** Pinch-to-zoom and
   Ctrl/Cmd+mouse-wheel do not change the webview zoom. View → Zoom
   In/Out/Reset and its persisted level (`zoom_store.rs`) keep working.
6. **Cursor:** in the desktop build, buttons, tabs and menu items use
   the default arrow cursor, as native apps do. Only real hyperlinks
   (external URLs, `openExternal`) keep the pointer hand.
7. **Scrollbars:** on Windows, scroll containers show thin themed
   scrollbars, not WebView2's classic wide grey ones. They are visually
   consistent with macOS overlay scrollbars in both themes. The existing
   xterm scrollbar handling (`index.css` around line 664) is untouched.
8. All of D2 applies only when `isDesktop` is true. The browser build's
   behaviour is unchanged.

### D3 — Desktop shortcuts and native menu

1. The binding table (`web/packages/ui/src/keyboard/shortcuts.ts`)
   supports a `desktop`-gated row, following Paseo's
   `when: { desktop }` pattern, without changing the existing `Mod`
   single-row model. A binding can be desktop-only, web-only, or both.
2. **Desktop-only defaults**, with the web keeping its current
   browser-safe combos:
   - `Mod+T` → `tab.new` (web keeps `Alt+Shift+T`);
   - `Mod+1..9` → `tab.jump` (web keeps `Mod+Alt+Digit`).

   `Mod+W`, `Mod+,` and the rest stay as they are on both. The shortcuts
   dialog and Settings → Shortcuts show the combo for the current
   platform.
3. **Native menu → renderer actions:** the app menu (`menu.rs`) gains
   items for New Tab, Close Tab, Command Palette, Find, Settings… and
   Toggle Sidebar, with the same accelerators as D3.2 and the table.
   Choosing a menu item runs the same renderer action as the keystroke.
   It is emitted as one event (e.g. `menu-action` with the action id)
   that the UI dispatches through the existing action registry. Pressing
   a shortcut fires its action exactly once, not once from the menu
   accelerator and again from the renderer listener.
4. On macOS the menu follows platform convention: an app-name menu with
   About, Settings… (`Cmd+,`), Hide, Hide Others, and Quit, ahead of
   File/Edit/View/Window/Help. The Edit menu's
   Undo/Redo/Cut/Copy/Paste/Select All work in text fields.

### D4 — Native integrations (scoped IPC)

1. **Reveal in Finder/Explorer** and **Open in editor** for a workspace
   folder and for a file in the file tree, offered only when the
   current connection is the **local** daemon. They are hidden for URL
   and relay connections, whose paths are not on this machine.
2. **Editor list:** Rust detects installed editors from a fixed
   allowlist (VS Code, Cursor, Zed, and the platform file manager) and
   exposes them through an `editors_list` command. `open_in_editor`
   takes an editor **id** from that list plus a path. It never takes an
   executable path or arguments from the webview. A path is rejected if
   it is not absolute or does not exist.
3. **WSL2 host:** when the managed local daemon runs in WSL2
   (`DaemonStatus.platform == "wsl2"`), Linux paths are translated to
   Windows paths (`wsl.exe wslpath -w`) before reveal or launch. A
   failed translation surfaces as an error toast, not a silent no-op.
4. **Badge:** the existing tray attention count
   (`smind_daemon_client::attention`) also drives the dock badge on
   macOS (and Linux, where supported) and the taskbar overlay icon on
   Windows. It clears when the count reaches zero. (The macOS Dock badge
   ships in `desktop-macos-app.md` M3.3. D4.4 only adds the Windows
   overlay icon and the Linux badge.)
5. Every new command is added one by one to `capabilities/proxy.json`'s
   allowlist (ADR-0013 §2). No wildcard, and no grant to any origin
   other than the loopback proxy. Window controls use Tauri core
   `core:window:allow-*` permissions for exactly the operations D1 needs
   (minimize, toggle-maximize, close, start-dragging, is-maximized,
   plus the resize/maximize change events).

### Cross-cutting

- Light **and** dark screenshots of the desktop build, taken on macOS
  and Windows (Linux if a runner is available), are attached in
  Validation for D1 and D2. Tests alone are not accepted for visual ACs.
- `task test`, `task lint`, `cargo test` and `cargo clippy` for
  `desktop/` are green. The Windows desktop CI build is green.

## Test Scenarios

### Web unit tests (vitest)

- `platform-window-controls-stub` — in a non-desktop build, the window
  controls API rejects and `DesktopWindowControls` renders nothing.
- `window-controls-maximize-glyph` — a maximized → restored event flips
  the maximize button's glyph and `aria-label` in both directions.
- `window-controls-click-dispatch` — minimize, maximize and close each
  call exactly one platform method. Close calls `close`, not `quit` or
  `destroy`.
- `header-drag-region-excludes-controls` — the header root carries the
  drag-region attribute, and buttons, inputs and tabs inside it do not.
- `header-reserves-traffic-light-space` — macOS desktop: the header
  has left inset padding with the sidebar expanded, collapsed, and with
  the sidebar sheet closed at narrow width.
- `header-reserves-caption-space` — Windows/Linux desktop: right inset,
  and the last header action never overlaps the caption buttons.
- `context-menu-suppressed-on-chrome` — a `contextmenu` event on a
  sidebar row with no smind menu and on empty pane space calls
  `preventDefault`.
- `context-menu-allowed-in-editable` — `contextmenu` in an input, a
  textarea, the file editor, or over a non-empty selection is not
  prevented.
- `context-menu-own-menu-still-opens` — the sidebar row's own context
  menu still opens.
- `chrome-not-selectable` — sidebar, header and tab strip resolve to
  `user-select: none` in the desktop build. Timeline message body, code
  block and diff do not.
- `shortcuts-desktop-gating` — with `isDesktop` true, `Mod+T` fires
  `tab.new` and `Mod+3` fires `tab.jump` with index 3. With
  `isDesktop` false, neither fires and `Alt+Shift+T` / `Mod+Alt+3` do.
- `shortcuts-dialog-shows-platform-combo` — the dialog lists `Mod+T`
  on desktop and `Alt+Shift+T` on the web for New tab.
- `menu-action-dispatch` — a `menu-action` event with id `tab.new`
  runs the same handler as the keystroke, exactly once.
- `menu-accelerator-no-double-fire` — when both the menu accelerator
  and the renderer listener see `Mod+T`, the action runs once.
- `open-in-editor-hidden-for-remote` — with a URL connection or a
  relay connection current, the Reveal and Open-in-editor items are
  absent.
- `open-in-editor-error-toast` — a rejected `open_in_editor` shows an
  error toast containing the Rust error message.
- `open-in-editor-passes-id-and-absolute-path` — choosing an editor
  calls `openInEditor` with that editor's id and an absolute path (the
  workspace path, or the task worktree path joined with the file's
  relative path), never an executable or arguments.

### Rust tests (`cargo test`, unit-level, no webview)

- `editors_list_only_allowlisted` — detection returns only allowlisted
  ids, given a fake filesystem/PATH.
- `open_in_editor_rejects_unknown_id` — an id not in the detected list
  is rejected.
- `open_in_editor_rejects_relative_or_missing_path`.
- `wslpath_translation_ok` and `wslpath_translation_failure_is_error` —
  run against a stubbed `wsl.exe` runner.
- `overlay_icon_pixels` — the Windows taskbar overlay image has the
  right size, is red in the centre and transparent in the corners, and
  differs per count.
- `badge_follows_attention_count` — the count goes 0 → 3 → 0 and the
  badge setter sees 3, then a clear.
- `window_shown_after_ready_or_timeout` — the show-on-ready logic shows
  on the ready signal, and also when the timeout elapses without one.
  Tested on the extracted pure state machine.
- `capability_allowlist_exact` — parsing `capabilities/proxy.json`
  yields exactly the expected permission set, with no wildcard and no
  non-loopback URL.

### Manual / screenshot checks (record in Validation)

- `macos-traffic-lights-in-header` — light and dark, sidebar expanded
  and collapsed.
- `windows-caption-buttons` — light and dark, maximized and restored.
  Snap via Win+Arrow updates the glyph.
- `linux-frameless-resize` — resize from all edges, drag, and
  double-click to maximize.
- `no-white-flash` — cold start in dark theme, screen-recorded or
  frame-checked.
- `close-hides-to-tray` — the drawn close button hides the window, the
  tray brings it back, and Quit really quits.
- `no-webview-context-menu` — right-click on chrome, in the composer,
  and on selected timeline text.
- `ctrl-wheel-no-zoom` — Ctrl/Cmd+wheel and pinch do nothing, and View
  → Zoom works.
- `wsl-reveal-and-open` — Windows + WSL2: Reveal a workspace in
  Explorer and open it in VS Code.
- `dock-badge` — a pending permission shows the badge, and resolving
  it clears the badge.

## Decisions

- **Stay on Tauri 2 + the bundled React UI (user, 2026-10-10, option
  A).** No native-toolkit rewrite and no Electron move. This is within
  ADR-0013, which already names "window controls, open-URL, dialogs" as
  scoped IPC, so no new ADR is needed. Revisit if D4 grows beyond
  per-command allowlisted calls.
- **No backend changes** (standing constraint, 2026-09-25). Everything
  here is `desktop/` or `web/packages/ui` behind `isDesktop`.
- **Out of scope, deferred:**
  - **Paste image into composer:** prompts carry no image payload
    today, so this needs daemon/provider changes.
  - **Native save dialog:** the UI has no download/export flow to
    attach it to.
  - **Auto-update**, as already deferred in `desktop-tauri-shell`.
  - **Multi-window:** conflicts with the single-instance and
    one-window assumptions.
  - **Windows 11 Snap Layouts flyout** on the drawn maximize button.
    Undecorated windows lose it, and ZCode accepts the same loss.
    Revisit if Tauri exposes `HTCLIENT`/`HTMAXBUTTON` hit-testing.
- **Reload stays** in the View menu (`CmdOrCtrl+R`). It is an explicit
  app action, useful after switching connections, not a webview
  default.
- **Order:** D1 → D2 → D3 → D4. Each is its own PR into `develop`.
  D1 + D2 together give most of the visible change.
- **D1 and D2 run in parallel (2026-10-10).** D1 is Sonnet 5.5 on
  `feat/desktop-chrome`. D2 is GLM 5.3 on `feat/desktop-de-webview`.
  To keep the two PRs from colliding:
  - D1 owns every change to the window builder in
    `desktop/src-tauri/src/lib.rs`, so **all of D2.1** (no white flash:
    hidden window, theme background, a web-side "painted" call through
    `lib/platform.ts`, and the timeout fallback) moves to D1. D2 does
    not touch `lib.rs`.
  - D1 owns the header markup in `App.tsx` and the sidebar top edge.
  - D2 installs its context-menu handling in its own module, not in
    `App.tsx`, and puts its CSS under a desktop-only selector in
    `index.css`.

## Progress

- [ ] D1 — window chrome (+ all of D2.1, no white flash): implemented on `feat/desktop-chrome`, automated checks green; native visual checks outstanding, see Validation
- [ ] D2 — remove webview-isms (D2.1 owned/implemented by D1 above): D2.2–D2.8 implemented on `feat/desktop-de-webview` (stacked on `feat/desktop-chrome`), automated checks green; see Validation
- [ ] D3 — desktop shortcuts + native menu: implemented on `feat/desktop-shortcuts-menu`, automated checks green; native menu check outstanding, see Validation
- [ ] D4 — native integrations (reveal/open-in-editor, badge): implemented on `feat/desktop-native-integrations`, automated checks green; native Windows/Linux/WSL2 checks outstanding, see Validation

## Validation

### D1 + D2.1 (`feat/desktop-chrome`, 2026-10-10)

Environment: macOS dev box, Rust 1.99 / Tauri 2.11.6. **No Windows or
Linux machine was available, so nothing below claims Windows/Linux
behaviour beyond the unit tests and a Chromium render with a Windows UA.**
The agent sandbox also kills WKWebView's content process ("web content
process terminated"), so the built app's *webview content* was blank even
with chrome disabled, same as the baseline window. Native window chrome
(traffic lights) was observable; the in-webview UI was not, so UI
screenshots come from headless Edge rendering the real `build:desktop`
bundle (Vite dev, `VITE_SMIND_DESKTOP=1`) against a live daemon, with a
macOS or Windows user agent and a stubbed `__TAURI_INTERNALS__`.

Automated, all green: `bun run test` (1364 web tests), `tsc -b`,
`go vet` + gofmt + `go test ./...`, `cargo test` (4 new tests), `cargo
clippy --all-targets` (only the pre-existing `notify.rs` unused-variable
warning on macOS). `task` itself is not installed here; its underlying
commands were run directly. `cargo fmt --check` is not clean on `develop`
already (not touched); the new module is rustfmt-formatted.

| AC | Confirmed by |
|---|---|
| D1.1 macOS overlay title bar, traffic lights in header | `window_chrome::configure` (`TitleBarStyle::Overlay`, `hidden_title`, `traffic_light_position`). **Manual:** native screenshot of the built app shows the lights vertically centred on the 48px row (y≈24, x 16–76), no title text/strip. The sidebar's own header row (logo + theme + settings) *is* the 48px title row on macOS, left-padded 84px (`TRAFFIC_LIGHT_CLUSTER_PX`), `data-tauri-drag-region="deep"`, bottom border at y=48 matching the main header (measured in Chromium: both rows 0–48, same border colour; zoomed screenshot `v2-zoom-border.png` shows the two lines level). No separate empty strip. Tests: `AppSidebar window chrome` in `desktop-chrome.test.tsx`. Chromium renders (simulated vibrancy backdrop, see D1.8) light + dark, expanded / collapsed / narrow: `/tmp/smind-d1-shots/v2-mac-*.png` (local only). The coordinator also launched the debug build on a real Mac; the user does the final visual check. |
| D1.2 Win/Linux undecorated + drawn buttons, restore glyph | `decorations(false)` on non-macOS; vitest `window-controls-maximize-glyph`. Chromium render with a Windows UA, restored + maximized, light: `w-win-*.png`. **Not run on real Windows/Linux:** OS snap / double-click / keyboard maximize rely on Tauri's resize event; unverified. |
| D1.3 Close = hide to tray, not quit | vitest `window-controls-click-dispatch` (calls `close`, never `quit`/`destroy`); Rust path is `core:window:allow-close` → `CloseRequested` → the existing `prevent_close` + `hide` handler, which now also marks the show-gate dismissed. **Manual `close-hides-to-tray` not run** (no working webview here). |
| D1.4 Drag + double-click | vitest `header-drag-region-excludes-controls` (`data-tauri-drag-region="deep"` on the header; Tauri's drag script, read in `tauri-2.11.6/src/window/scripts/drag.js`, refuses to drag from buttons/inputs/links/`role=tab`). Double-click uses Tauri's `internal_toggle_maximize` (zoom on macOS); it does **not** read the system "double-click title bar" setting, since Tauri doesn't expose it. **Not exercised in a live window.** |
| D1.5 Resize from all edges | Relies on tao's undecorated-resize handling (`tauri-runtime-wry` `undecorated_resizing`). **Unverified** on Windows/Linux (`linux-frameless-resize` not run). macOS keeps its native frame. |
| D1.6 Header/sidebar insets | vitest `header-reserves-traffic-light-space` (header: expanded 0, collapsed rail 8+32px, hidden sidebar 8+84px), macOS fullscreen test, `AppSidebar window chrome` tests (expanded row padding/height/border/drag; collapsed rail keeps icons below the lights via a borderless, collapsed-only 48px spacer, no empty bordered box; fullscreen and Windows/Linux/web leave the sidebar header untouched), `window-chrome` maths tests. **Deviation from the scenario text:** with the sidebar expanded the header needs no left inset, because the sidebar's header row holds the lights. At the 192px minimum sidebar width the wordmark truncates ("s…") because 84px of the row is the lights' clearance; logo and buttons stay. |
| D1.7 Web build unchanged | vitest `platform-window-controls-stub` (platform + component), "web build unchanged" test; `@tauri-apps/api/window` is only dynamically imported inside the `isDesktop` branch of `platform.ts`. |
| D1.8 Materials | **macOS:** `Effect::Sidebar` + transparent window, sidebar background `color-mix(var(--sidebar) 72%, transparent)`, `body` transparent. Because the body is transparent, every non-sidebar surface now carries its own opaque token: main content container (`app-main-content`), empty state, `DesktopUnreachable`, `SettingsScreen`, header (all `bg-background`); the sidebar/content resize handle gets `var(--background)`. Audit of what else is outside `SidebarInset`: only the sidebar, the handle, the mobile sheet (`bg-sidebar`), and portalled dialogs/toasts (own backgrounds). Test: `opaque surfaces on macOS` (renders `App` with an unreachable daemon, asserts `bg-background` on main content, unreachable screen, header; empty state and settings in a second test; fails if the class is removed). Requires `macos-private-api`. **Headless artifact, not a bug:** in headless Edge the collapsed rail renders light grey with a bright right edge in dark mode because the sidebar is 72%-alpha `#161616` and the border is the `--sidebar-border` token (`oklch(1 0 0 / 10%)`, i.e. white at 10%), both composited over headless's white canvas (probe: `bodyBg rgba(0,0,0,0)`, sidebar bg `0.72` alpha, border `rgba(255,255,255,0.1)`). With a dark backdrop standing in for vibrancy the rail is dark and the edge is a faint 1px line, as designed. The real vibrancy compositing is still to be eyeballed by the user. **Windows 11 Mica/Acrylic: not implemented**, untestable here; Windows stays opaque. Linux opaque. Follow-up. |
| D2.1 No white flash | Rust `window_shown_after_ready_or_timeout`, `fallback_is_at_most_three_seconds`, `surface_colors_match_index_css`; vitest `DesktopPaintedSignal` tests. Window is created `visible(false)` with the last session's surface colour (persisted by `window_set_theme`; dark on first ever launch), shown on `window_ready` or after 3 s. The signal is a post-commit timer, **not** `requestAnimationFrame`: a hidden webview suspends rAF. **Manual `no-white-flash` not run** (frame-checking needs a working webview). |

Also done, not in the spec: `tauri-plugin-window-state` no longer restores
`VISIBLE` (it would show the window before paint on every launch after the
first) or `DECORATIONS` (an existing saved `decorated: true` would put the
native frame back on an undecorated Windows/Linux window, giving two sets
of caption buttons). New capability grants in `proxy.json`: `allow-window-ready`,
`allow-window-set-theme`, and `core:window:allow-{minimize,toggle-maximize,
internal-toggle-maximize,close,start-dragging,is-maximized,is-fullscreen}`.
`is-fullscreen` and `internal-toggle-maximize` go beyond the list in D4.5
(macOS hides the lights in fullscreen; Tauri's drag script calls the
internal command on double-click), so D4's `capability_allowlist_exact`
must include them. No ZCode code was copied (layout and approach only),
so `NOTICE` is unchanged.

#### Native checks on a real Mac (commit `49060a7`, run by the coordinator outside the sandbox)

| Check | Result |
|---|---|
| `macos-traffic-lights-in-header`, **dark, expanded** | **PASS (native).** Lights vertically centred in the 48px sidebar header row, left of the logo. Sidebar header border and main header border are level. |
| macOS vibrancy (D1.8), dark | **PASS (native).** Blurred content from windows behind shows through the sidebar; the main content area is opaque. |
| `no-white-flash` (D2.1) | **PASS (native).** 12 burst screen captures from launch: luminance went straight from the background windows (avg 0.237) to the dark window (0.126); bright-pixel fraction stayed 0.003–0.005 in every frame. No white frame. |
| `macos-traffic-lights-in-header`, light theme and collapsed rail | **Not checked** (no accessibility permission to click). Needs the user. |
| Drag / double-click on the header (D1.4) | **Not checked.** Needs the user. |
| `close-hides-to-tray` (D1.3) | **Not checked.** Needs the user. |
| `windows-caption-buttons`, `linux-frameless-resize`, Windows desktop CI build | **Not run.** No Windows/Linux machine. |

D1 stays unticked in Progress: the unchecked rows above, and Windows/Linux,
are still outstanding.

### D3 (`feat/desktop-shortcuts-menu`, 2026-10-10)

Automated, all green: `bun run test` (1409 web tests, incl. the new
`desktop-shortcuts` suite), `tsc -b`, `go vet`/gofmt/`go test`, `cargo
test` (14 tests, incl. `menu_actions_match_web_action_ids`), `cargo
clippy --all-targets -- -D warnings`. No new capability grant:
`core:event:allow-listen` already covers the `menu-action` listener.

| AC | Confirmed by |
|---|---|
| D3.1 desktop-gated rows | `BindingWhen.desktop` (`true`/`false`/absent) + `platformBindings`, filtered before matching, the help dialog and conflict checks; vitest `platformBindings keeps only rows whose desktop gate matches`. |
| D3.2 desktop-only defaults | vitest `shortcuts-desktop-gating`: desktop fires `Mod+T` → `tab.new` and `Mod+3` → `tab.jump {digit: 3}`; web fires neither and `Alt+Shift+T` / `Mod+Alt+3` do. `shortcuts-dialog-shows-platform-combo`: the list shows Ctrl+T (desktop) vs Alt+Shift+T (web) for New tab. `Mod+W`/`Mod+,` etc. untouched on both. |
| D3.3 menu → renderer + no double fire | `menu.rs` items (File: New/Close Tab + Settings… off-mac; View: Command Palette, Find, Toggle Sidebar) emit `menu-action` with the ActionId; `desktop.onMenuAction` dispatches through `runAction` — vitest `menu-action-dispatch` (same handler as the keystroke, exactly once). Double fire: a 300 ms same-action dedupe between the keydown and menu sources (whichever arrives first wins); `menu-accelerator-no-double-fire` covers both orders plus a 400 ms repeat running again. Accelerators on the D3 items are **macOS-only** (`renderer_accel`): on Windows/Linux `CmdOrCtrl` is Ctrl, and Ctrl+W/K/B/F/T are readline keys in the terminal pane a native accelerator would steal; the items still emit `menu-action` when clicked. |
| D3.4 macOS app menu | App-name submenu (About, Settings… `Cmd+,`, Hide, Hide Others via `PredefinedMenuItem`, Quit `Cmd+Q`) ahead of File/Edit/View/Window/Help; Edit keeps Undo/Redo/Cut/Copy/Paste/Select All predefined. Windows/Linux unchanged besides the new items. **Not exercised in a live app** (same sandbox webview constraint as D1/D2); needs the user's native menu check. |

### D2.2–D2.8 (`feat/desktop-de-webview`, 2026-10-10)

Environment: same macOS dev box and constraints as D1's entry above (no
Windows/Linux machine; WKWebView content process killed in the sandbox,
so visual checks use headless Chromium rendering the real
`build:desktop` bundle with `VITE_SMIND_DESKTOP=1`, a macOS/Windows user
agent and a stubbed `__TAURI_INTERNALS__`).

Automated, all green: `bun run test` (1372 web tests, incl. the new
`desktop-context-menu`, `desktop-webview` and `desktop-webview-css`
suites), `tsc -b`, `go vet`/gofmt/`go test`. jsdom has no CSS cascade,
so the CSS rules are guarded by source tests over `index.css`
(`src/test/desktop-webview-css.test.ts`), the same pattern
`no-hardcoded-colors.test.ts` uses.

| AC | Confirmed by |
|---|---|
| D2.2 context menu | vitest `context-menu-suppressed-on-chrome` (sidebar row, pane space → `preventDefault`), `context-menu-allowed-in-editable` (input/textarea/contenteditable/`.cm-content` file editor pass through; a non-empty selection only keeps the menu when the click lands *on* it — rects hit-test with an `intersectsNode` fallback — and a selection elsewhere does not unlock right-click on chrome; `contenteditable="false"` subtrees are suppressed), `context-menu-own-menu-still-opens` (an already-`defaultPrevented` event — Radix `ContextMenuTrigger` on the file-explorer rows — is left alone). Release-build only in principle: the policy runs whenever `isDesktop`, which is exactly the bundled builds. DevTools stays on the View menu (D3 scope). |
| D2.3 non-selectable chrome | vitest `chrome-not-selectable` (root `user-select: none` under `html[data-desktop-os]`, inside `@layer base` so explicit `select-text`/`select-none` utilities still win; re-enabled on input/textarea/contenteditable, `pre`/`code`, the timeline column, `.d2h-wrapper` diffs). Controls inside the timeline column stay unselectable. The terminal is deliberately NOT re-enabled: xterm draws its own selection and DOM selection would double-highlight. **Cmd+A over the sidebar not screenshot-verified** (sandbox webview constraint, see above); covered by the CSS guard + Chromium render check. |
| D2.4 no overscroll | `overscroll-behavior: none` on `html`/`body` only, guarded by `desktop-webview-css` ("applies to the document root only"; nothing outside the desktop gate). Inner containers keep the default. Not verifiable in jsdom; rubber-banding is a WebKit/Chromium visual behavior — **not screenshot-verified**. |
| D2.5 zoom guard | vitest in `desktop-webview.test.ts`: ctrl/meta+wheel `preventDefault` (listener registered `passive: false`), plain wheel/shift pass through; WebKit `gesturestart/change/end` prevented (jsdom has no `GestureEvent`, so a plain `Event` stands in — the guard only calls `preventDefault`). WebView2 pinch reports as a synthetic ctrlKey wheel, covered by the wheel branch. Web-side only; `lib.rs` untouched per the ownership split. View → Zoom (`zoom_store.rs`) untouched. **Manual `ctrl-wheel-no-zoom` (real trackpad pinch) not run.** |
| D2.6 default cursor | `cursor: default` on `button`, `[role=button|tab|menuitem|menuitemcheckbox|menuitemradio|option]`, `summary`, `label[for]` — in `@layer base` so `cursor-grab`/`cursor-col-resize`/`cursor-not-allowed` utilities still win — plus one deliberate unlayered `html[data-desktop-os] .cursor-pointer { cursor: default; }` for the file-tree rows / timeline summaries that ask for the hand. Real `<a href>` hyperlinks (the accounts dialog's authorize link) keep the pointer. All guarded by `desktop-webview-css`. |
| D2.7 Windows scrollbars | `scrollbar-width: thin; scrollbar-color: var(--color-border) transparent` gated to `html[data-desktop-os="windows"]` — **rationale for the OS gating:** macOS must keep its overlay scrollbars (spec: "visually consistent with macOS overlay scrollbars"), and Linux uses the desktop theme, so a global thin-scrollbar rule would be a regression on both; the same `data-desktop-os` marker D1's `desktop-chrome.css` established is the gate. Guarded by `desktop-webview-css` (Windows-only; the browser build's only `scrollbar-width` is xterm's own hiding rule, which is untouched). **Not verified on real Windows/WebView2** — no Windows machine. |
| D2.8 desktop-only gating | Every D2 path is behind `isDesktop` (`lib/platform.ts`): the context-menu and zoom-guard installs are no-ops in the browser build (each has a vitest "inert in the browser build" case), and every CSS rule hangs off `html[data-desktop-os]`, which `main.tsx` sets only when `desktopOS` is non-null. `desktop-webview-css` asserts no `user-select`/`cursor: default`/`overscroll-behavior`/`scrollbar-width` outside the gate. Reuses D1's `detectDesktopOS`/`desktopOS` (this branch's duplicate `data-smind-desktop` marker and UA detector were dropped during integration). |

Screenshots: `no-webview-context-menu` (right-click on chrome vs in the
composer) and the Cmd+A-over-sidebar check were rendered in headless
Chromium with the desktop bundle, light and dark — files under
`/tmp/smind-d2-shots/` (local only, like D1's). What was **not**
verifiable here: real WKWebView/WebView2 menus (context menu, pinch
zoom), Windows scrollbars, and rubber-band overscroll. Those need a
machine with a working webview and a Windows box.

### D4 (`feat/desktop-native-integrations`, 2026-10-11)

Environment: macOS dev box. **No Windows, Linux or WSL2 machine was
available.** The Windows overlay-icon call and every Windows/Linux/WSL2
launch path are covered by unit tests against fake probes/runners only;
none of them was executed on a real OS. UI screenshots are headless Edge
renders of the real Vite dev bundle (`VITE_SMIND_DESKTOP=1`) against a
live daemon with a stubbed `__TAURI_INTERNALS__` (editors: Finder, VS
Code, Cursor) and a macOS user agent, light and dark — not the built app
(the agent sandbox kills WKWebView's content process, see D1).

Automated, green: `task test` (Go + 1419 web tests), `task lint`, `bunx
tsc -b`, `cargo test` (27 tests), `cargo clippy --all-targets`
(warning-free).

| AC | Confirmed by |
|---|---|
| D4.1 Reveal / Open in editor, local only | Items on the workspace row menu (`workspace.Path`) and the file-tree file row menu (`WorktreePath` + relative path; hidden when `WorktreePath` is null). vitest `open-in-editor-hidden-for-remote` (url, relay, non-desktop absent; local present; a connection switch without reload drops them via `smind:connection-changed`), `open-in-editor-error-toast`, `open-in-editor-passes-id-and-absolute-path`. Screenshots: `workspace-menu-{light,dark}`, `file-menu-{light,dark}` (local only, `/tmp/d4-shots`). Folder rows in the tree have no context menu today, so only files get the items. |
| D4.2 Allowlist, id-only | Rust `editors_list_only_allowlisted`, `open_in_editor_rejects_unknown_id`, `open_in_editor_rejects_relative_or_missing_path`, plus argv tests (path is always one element; file manager only reveals). The command also re-checks the connection kind on the Rust side and rejects url/relay. **Not run:** real launches of VS Code/Cursor/Zed or Finder (no accessibility, and the sandbox can't show windows). |
| D4.3 WSL2 | `wslpath_translation_ok`, `wslpath_translation_failure_is_error` against a stubbed runner. **Not verified on Windows + WSL2** (`wsl-reveal-and-open` not run), including `wsl.exe` UTF-16 output handling. |
| D4.4 Badge | `badge_follows_attention_count`, `overlay_icon_pixels`. macOS Dock badge shipped earlier; Linux uses `set_badge_count` (launcher support depends on the desktop environment). **Windows overlay icon never ran on Windows**, and the Windows-only `set_overlay_icon` branch could not be compiled here (re-read against tauri 2.11.6 sources only); the Windows desktop CI build is the first compile. `dock-badge` manual check not run. |
| D4.5 Allowlist exact | `capability_allowlist_exact`, `capability_app_grants_match_build_rs_commands`. |

Adversarial review (fresh Sonnet 5.5 agent, read-only) found and this PR
fixed: **(high)** `wsl.exe ... -- wslpath` ran the path through the distro
shell (word-splitting, `$()`/`;` injection) -> now `--exec`, test
`wslpath_runs_without_a_shell_and_keeps_the_path_one_argument`; `open` /
`xdg-open` failures after spawn were swallowed -> now waited on and
surfaced as the toast error; `editors_list` ran on the main thread -> now
`spawn_blocking`; a failed overlay/badge set was recorded as shown ->
retried, and re-applied on window focus (`badge_retries_after_a_failed_set_and_reapplies`);
stale `useLocalPathActions` loads could re-show items after a connection
switch -> newest-load-wins. **Known gaps, not fixed:** Windows editor
detection only sees `%LOCALAPPDATA%\Programs\...\*.exe` installs (system-wide
`Program Files` installs and `.cmd` shims on PATH are not detected, so those
editors are simply not offered); on a Windows host with WSL installed,
`Platform::Wsl2` is chosen even if the Local daemon were a native Windows
one, which yields an explicit "not an absolute Linux path" error.

