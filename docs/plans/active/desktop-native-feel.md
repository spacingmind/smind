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
   Windows. It clears when the count reaches zero.
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

### Rust tests (`cargo test`, unit-level, no webview)

- `editors_list_only_allowlisted` — detection returns only allowlisted
  ids, given a fake filesystem/PATH.
- `open_in_editor_rejects_unknown_id` — an id not in the detected list
  is rejected.
- `open_in_editor_rejects_relative_or_missing_path`.
- `wslpath_translation_ok` and `wslpath_translation_failure_is_error` —
  run against a stubbed `wsl.exe` runner.
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

## Progress

- [ ] D1 — window chrome
- [ ] D2 — remove webview-isms
- [ ] D3 — desktop shortcuts + native menu
- [ ] D4 — native integrations (reveal/open-in-editor, badge)

## Validation

_Not started._
