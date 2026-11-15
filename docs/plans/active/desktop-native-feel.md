# Desktop native feel, round two (DT2): native context menus, validated menu state, window layout, window tabbing

Status: active (2026-11-15). Lead of DT2 on `wave/desktop`. Sequel to [`completed/desktop-native-feel.md`](../completed/desktop-native-feel.md) (chrome, de-webview, shortcuts and menu, scoped integrations) and `desktop-macos-app.md`. Size: L, four PRs one at a time, each from a fresh `origin/wave/desktop`, base `wave/desktop`, never stacked: (a) native context menus, (b) validated menu enable/disable state, later (c) layout saved and restored per window on Cmd+Q, (d) a SPIKE (write-up only) on "Open task in new window" and macOS window tabbing.

## Checked against origin/wave/desktop: already shipped (not gaps)

The macOS app-name menu and File/Edit/View/Window/Help with the accelerator table (`menu.rs`), the renderer actions through `menu-action` and `app_menu_action`, the tray with the attention count (`tray.rs`), the Dock badge on macOS and the taskbar overlay on Windows (`lifecycle.rs`, `overlay_icon.rs`), deep links (`deeplink.rs`, single instance), the overlay title bar on macOS and the drawn caption buttons elsewhere, sidebar vibrancy and Mica, a hidden-until-painted window, window geometry restore (`tauri-plugin-window-state`), zoom persistence, reveal-in-Finder and open-in-editor (`editors.rs`), the auto-updater, and the document-level policy that replaces the webview's default right-click menu with Cut/Copy/Paste only in editable fields and on a selection (`lib/desktop-context-menu.ts`, D2.2).

## What is genuinely missing (the gaps this plan closes)

1. **Context menus are in-page DOM menus** (Radix, `components/ui/context-menu.tsx`): the sidebar workspace and task rows, the workspace tabs, and the file and folder rows in the explorer. They are clipped by the window, do not follow the OS look, type-ahead or accessibility, and follow the OS only partly. A real Mac app pops a native menu.
2. **Menu items never reflect state.** The native app menu is built once; "Close Tab" is enabled with no tab, "Find" with no pane, and so on. Only the in-page Radix menus disable items.
3. **Quit does not keep the layout.** `tauri-plugin-window-state` keeps size and position only; open tabs, splits and the sidebar state are not saved per window, so a Cmd+Q and relaunch starts from a blank layout.
4. **One window.** There is no "Open task in new window" and no macOS window tabbing; whether either is worth building is unknown, hence the spike.

## PR (a): native context menus

### Acceptance criteria

- AC1 In the desktop build, a right-click on a sidebar workspace row, a task row (including the same item set as its "more" menu), a workspace tab, a file row and a folder row pops a NATIVE menu at the pointer (Tauri `Menu::popup_at`), with the same items, order, separators, disabled state and "Move to space" submenu as the in-page menu has today. Choosing an item runs exactly the handler the in-page item would.
- AC2 In the browser build the existing Radix menu renders exactly as today: same test ids, same behaviour (it is the same model drawn by the in-page renderer). No `@tauri-apps/api` import is reachable in the browser build. In the desktop build the Radix content is never rendered; if Rust refuses a model the menu is disarmed, reported closed and a warning is logged (there is no second, in-page, menu to fall back to).
- AC3 Menus are data, not JSX: each of the five menus is built by one pure function into a menu model (items with `id`, `label`, `enabled`; separators; one level of submenu), and BOTH renderers (Radix and native) consume that same model, so the two cannot drift.
- AC4 The IPC carries descriptors only: the webview sends labels, opaque item ids and enabled flags and gets item ids back. It cannot make Rust run a path, a command or a URL through this channel. Rust validates the model (at most 64 entries and depth 2, ids 1-64 chars of `[A-Za-z0-9._:-]` and unique per menu, labels at most 120 characters with no control characters, a menu with no item at all, only separators or empty submenus, is refused; a menu whose items are all disabled is still shown, as the in-page one is) and refuses one that fails. The new command is added to `capabilities/proxy.json` by name (ADR-0013 section 2), no wildcard.
- AC5 A native menu's selection reaches only the menu that opened it: a stale selection (an earlier menu, a duplicate) is ignored, opening a second menu replaces the first, and a menu that delivers nothing leaves no handler that fires after its time-out.
- AC6 The web webview's own default menu stays suppressed everywhere else (D2.2 unchanged); editable fields keep their native Cut/Copy/Paste menu, which the new handler never intercepts.
- AC7 Keyboard parity (best effort, not verified in a real window): the Menu key and Shift+F10 on the focused trigger open the same native menu, at the element's lower-left corner when the webview reports the event at 0,0.
- AC8 Windows and Linux use the same path (the menu API is cross-platform); only macOS is verified here (stated limit).

### Named tests

Rust (`cargo test`, unit-level): `context_menu_validate_*` (table: empty, only separators, too many, too deep, duplicate id, bad id, long or control-character label, disabled-only), `context_menu_ids_are_namespaced_per_open_and_roundtrip`, `context_menu_selection_of_a_stale_open_is_ignored`, `capability_allowlist_exact`, `context_menu_model_deserializes_only_its_own_fields` (the argument type has only the descriptor fields), `context_menu_disabled_items_are_never_armed`, `context_menu_labels_with_bidi_or_zero_width_characters_are_refused`.
Web (vitest): `menu-models.test.tsx` (one table-driven test per menu: workspace row, task row, tab, file row, folder row: for each state the exact entries, order, separators, enabled flags), `app-context-menu.web.test.tsx` and `app-context-menu.desktop.test.tsx` (the Radix fallback renders the model with the existing test ids; the desktop path calls the bridge with descriptors and not the DOM; selection resolves the handler once; a stale id, a dismissed menu and a second menu behave per AC5; the browser path never loads the bridge), the existing sidebar, tab and file-explorer suites unchanged and green.
Real window: frames of each native menu with `screencapture -l` from the isolated audit build (`scripts/desktop-audit-build.sh`, its scratch daemon, never the real daemon) when the screen is unlocked; while it is locked the PR states so and carries web frames of the Radix menus (unchanged) and the unit tests.

### Decisions

- **One bridge, descriptors only** (AC4): the same shape as `app_menu_action` (an allowlisted command, ids not actions); the renderer owns the handlers.
- **No icons in native menus**: the model carries a label only; the Radix menu keeps its icons.
- **Dismissal is not observable** in Tauri's menu API: `context_menu_show` returns when the native menu closes (macOS, Windows), which the renderer treats as "closed"; the handlers stay armed for a late selection event and are discarded by the next open, a selection or a 10-minute time-out (`ARMED_TTL`), never kept.
- **Shared model, two renderers** (AC3) rather than patching the DOM menu: the one place the menus' contents live, which is also what (b) tests.

## PR (b): validated menu enable/disable state

### Acceptance criteria

- AC9 (deferred, not in this PR) The native APP menu's enable/disable snapshot (`app_menu_set_state`) is a separate piece with its own IPC command; the Steward's brief for this PR is the context menus, so it moves to a later PR if the Supervisor still wants it.
- AC10 Every context menu's enabled flags come from the same pure model functions as AC3 and are table-tested per menu. An item that stays visible but cannot run is DISABLED (and, where a reason helps, keeps it as a note: the in-page menu shows it and keeps the item keyboard-reachable; the native label carries it in parentheses). The rules:
  - Task row: Archive task is off for an archived task ("already archived"); Mark unread is off for an archived task or one that is already unread; New sub-task (tree provider only) is off for an archived task ("archived") instead of vanishing; Pin and Move to space stay as they were (Move is absent, not disabled, when there is nowhere to move to); the tree actions keep their availability and reasons.
  - Tab: Close, Close others, Close to the left and right are off when they would close nothing; Archive chat is off for a chat tab with no chat id ("no chat yet"), where it used to do nothing.
  - File row: Reveal in diff is off for an unchanged file; Open to side is off without a handler (both unchanged).
  - Workspace row (sidebar): Reveal and Open in editor (desktop on its local daemon only) are off when the workspace has no folder ("no folder"). The row's items now come from one pure builder (`workspaceRowItems`) that both its context menu and its more menu use; the more menu greys disabled items too.
- AC11 The web fallback and the native menu agree: for every table row the native descriptors carry exactly the model's enabled flags, only enabled items have a handler, and the in-page menu is disabled exactly where the model says (a render test over each menu's states).

### Named tests

Web: one table-driven test per menu in `lib/menu-models.test.tsx` (`taskRowMenu`, `tabMenu`, `fileRowMenu`, `workspaceRowItems`; each row also runs `expectNativeAgrees`), and `lib/menu-models.render.test.tsx` (the in-page menu is disabled exactly where the model says, for each menu in its disabling states). No Rust change in this PR.

### Decisions (b)

- Not every item gets a disabled state: Pin, Add task, Add space, Worktree settings and Delete workspace can always run, so they never carry a flag a test would have to invent. Items that are absent when they cannot apply (Add to chat without a handler, the Split items when the tab cannot move, Move to space with nowhere to go) stay absent, as the in-page menus have always done.
- "No run" and "no worktree" from the brief: a task row has no menu item that needs a run, and the only items that need a folder are Reveal and Open in editor, which exist on the workspace row.

## PR (c): layout per window on Cmd+Q (later; needs a decision)

Open question for the Supervisor before it is specified: where a window's layout lives (the daemon, which also serves the web build; the Tauri store file; or the renderer's `localStorage`). It touches architecture (a new persisted state and who owns it), so no spec or code until it is decided.

## PR (d): spike (write-up only)

"Open task in new window" (a second Tauri window on the same loopback proxy, the single-instance and deep-link behaviour, shared connection state, per-window zoom and layout) and macOS window tabbing (`tabbingIdentifier`, the native Window menu items). Output: a note in this plan with findings and a recommendation, no product code.

## Progress

- 2026-11-15: spec written against `origin/wave/desktop` cb8593cce. The screen was locked, so real-window frames wait.
- 2026-11-15: PR (a) built: `context_menu.rs` (the validated descriptor-only command, namespaced per open, TTL), `lib/menu-model.ts` + `lib/menu-models.tsx` (the five menus as pure builders), `AppContextMenu` + `MenuEntries` (two renderers), `lib/native-context-menu.ts` and the `platform.ts` bridge, the five sites converted (task row, workspace row, tab, file row, folder row); the Radix menus are byte-for-byte the same in the browser build (the existing sidebar, tab and explorer suites are unchanged and green).

## Validation

### PR (a)

- AC1 (native menus with the same items, order, separators, disabled state, submenu): per-menu table tests `menu-models.test.tsx` (file row 6 states, folder row, tab 7 states and handlers, workspace row, task row 11 states incl. Move to space and the tree actions) and `app-context-menu.desktop.test.tsx`. **Not verified in a real window:** the screen was locked (`CGSSessionScreenIsLocked`), so no `screencapture -l` frames of the native menus exist; they wait for an unlocked screen.
- AC2: `app-context-menu.web.test.tsx` (the Radix menu from the model with its test ids, disabled and reachable-disabled items, never the desktop bridge), the unchanged sidebar, App and explorer suites, the e2e spec `context-menus.spec.ts` CM-01..CM-03 on the four projects, and the platform stub test.
- AC3: one model, two renderers: `menu-model.ts`/`menu-models.tsx` feed both `MenuEntries` and `toNative`.
- AC4: Rust `context_menu_validate_table`, `context_menu_model_deserializes_only_its_own_fields` (a path, a command or an unknown kind is rejected at decode), `capability_allowlist_exact` and `capability_app_grants_match_build_rs_commands` (the new command named in `proxy.json` and `build.rs`, no wildcard).
- AC5: Rust `context_menu_selection_of_a_stale_open_is_ignored` (an id both menus carry, so only the open id separates them), `..._after_the_ttl_...`; web `app-context-menu.desktop.test.tsx` (stale open, unknown id, disabled id, second menu replaces the first, a selection that beats Rust's answer, the time-out, a selection after the menu returned, the blink guard, a refused model).
- AC6: unchanged `lib/desktop-context-menu.ts` and its tests; the handler stops the event only on a trigger that has a menu.
- AC7: `app-context-menu.desktop.test.tsx` (Menu key, Shift+F10, the 0,0 keyboard report). Not verified in a real window.
- AC8: the same Rust path on every platform; only macOS builds were run (`cargo test --lib`: 181 passed, `cargo clippy --all-targets` clean). Windows and Linux are not built or run here.
- Mutations (committed code, one at a time, each failed its named test): duplicate ids accepted, a stale open accepted, the TTL ignored, `/` allowed in an id, a disabled item armed, no `stopPropagation`, the bridge accepting a stale open, the tab menu's Close-left always enabled, the tree action ignoring its availability.
- Review: see the PR body.

### Stated limits of PR (a)

- No real-window frames: the screen was locked, so the native menu was never seen on a real window; coverage is Rust unit tests, vitest against the descriptors, and Playwright on the in-page fallback.
- Windows and Linux use the same command but are unbuilt and unverified; off macOS a literal `&` in a label is doubled for the mnemonic rule.
- `context_menu_show` has no rate limit (it blocks the main thread while a menu is open, and the renderer drops an open that follows another within 100 ms); the model is built eagerly per open; the zoom is read from the store per call.
- The renderer sanitises labels (control, bidi and zero-width characters out, whitespace collapsed, 100 characters); Rust refuses what is left that is invalid.
- No native-side gesture or rate limit: the popup is modal on macOS and Windows, so one menu is open at a time, and the page-side 100 ms guard is a courtesy, not a security control. On Linux the popup is asynchronous (the command may return before the menu closes), so "closed" is reported early there and the TTL is the bound; Linux is unverified.
- The armed state is not cleared on navigation or reload: an armed menu delivers until its selection, the next open or the 600 s TTL. Selections are matched on the open id AND on a per-show id namespace (`m<n>.<id>`), so a late selection from an earlier menu cannot run a later menu's handler.
- The label rule is the Unicode general category (Cc, Cf, Cs, Co, Cn, Zl, Zp) plus the blank-looking characters outside those categories (Hangul fillers, Braille blank, combining grapheme joiner, variation selectors); the renderer strips the same set, so an emoji title loses only its variation selector.

### PR (b)

- AC10: `menu-models.test.tsx` tables for `taskRowMenu` (archived, already unread, tree actions), `tabMenu` (no chat id, nothing to close), `fileRowMenu` (unchanged, no handler), `workspaceRowItems` (no folder). AC11: each row runs `expectNativeAgrees`; `menu-models.render.test.tsx` checks the in-page menu per state; e2e CM-04 (light and dark, 1280 and 390, pairs opened).
- Mutations (committed code, each failed its named test): Archive always on, Mark unread ignoring unread, New sub-task always on, Archive chat always on, Reveal ignoring the folder, Reveal in diff always on, Close others always on, the native layer arming disabled items.
- Not verified: no real-window frames of the native menu (screen locked); the archived-task rules apply to task rows outside the archived list (that list has its own row component and actions).

