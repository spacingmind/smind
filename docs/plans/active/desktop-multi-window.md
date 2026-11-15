# Desktop multi-window, Part 1: one window per task

Human decision (via the Supervisor, 2026-11-15, HIGH-RISK): multi-window GO, Part 1, ONE WINDOW PER TASK. Spike: `desktop-multi-window-spike.md` (#903). Governing ADR: ADR-0013 and its 2026-11-15 amendment (capability scope). macOS window tabbing is NOT part of this plan: it is a probe after Part 1, on a real window, and ships only if the overlay title bar survives.

Delivery: ONE Rust PR (this plan, the ADR amendment, the shell) and ONE web PR after it (the "Open in new window" item and the task-window layout). Single-window layout restore (#914) keeps working for `main` only.

## Design

- `window_open_task(task_id: i64)` (new allowlisted command, integer only). Rust validates `1 <= task_id <= 2^53-1`, finds or allocates a window for that task, and returns its label. If the task already has a window it is un-minimised, shown and focused; otherwise a new window is built from a fixed pool of labels `task-1`..`task-8` (`MAX_TASK_WINDOWS = 8`), loading `http://127.0.0.1:<port>/?k=<secret>#/task/<id>` (URL built in Rust from the proxy origin and the per-launch secret). Beyond the cap the command refuses with a clear error ("too many task windows open (8); close one first"). A page cannot name a URL, a label or any option.
- Task windows use the same chrome (`window_chrome::configure`, overlay title bar and vibrancy on macOS, drawn caption buttons elsewhere, the 800x520 minimum), the same navigation guard (only the loopback origin navigates; other links open in the OS browser), `disable_drag_drop_handler`, and refuse `window.open` (`on_new_window` denies). They are created hidden and shown on the first-paint signal or after the same fallback timeout.
- Task windows DESTROY on close (the hide-to-tray handler stays on `main` only); closing releases the pool slot. Window-state persistence (`tauri_plugin_window_state`) keys by label, so the fixed pool bounds `.window-state.json` to at most 8 task entries plus `main`.
- `main` keeps the identity: tray, Dock, badge, single-instance focus, Dock reopen, notification click target, deep-link navigation, hide-to-tray close, and the quit-time layout save (#914 reads `main` only).
- Per-window targeting: the context-menu selection event goes to the window that opened the menu (the armed menu records its label); `window_ready` and `window_set_theme` act on the calling window; the app-menu items Reload, Fullscreen, Minimize and Inspect act on the focused window (else `main`); Zoom applies to every open window; Hide stays on `main`.
- Capabilities (ADR-0013 amendment): `proxy.json` unchanged (`main` only, exact list plus the new `allow-window-open-task`); new `task-window.json` with `windows: ["task-*"]` and the smaller explicit list; both pinned by the exact-list test.
- Web (second PR): the task context menu gains "Open in new window" (desktop build only; hidden in the browser build), `parseRoute` accepts `#/task/<id>` (select the task and seed its default chat tab), and a task window hides the connection picker, daemon banner and update badge so it never calls a main-only command.

## Threat model

| Threat | Mitigation | Test |
|---|---|---|
| Label injection: a page names a window label that gets a capability or collides with `main` | No label crosses the IPC: the command takes an integer and Rust picks from `task-1..task-8`; the capability glob `task-*` can only match those; `main` can never be allocated | `pool_labels_are_fixed_and_never_main`, `capability_task_window_scope_is_exactly_the_task_glob` |
| URL injection: a page makes the shell load an arbitrary URL in a privileged window | Rust builds `http://127.0.0.1:<port>/?k=<secret>#/task/<id>` itself from the proxy origin; the id is an integer; no page string is concatenated | `task_url_is_built_from_the_proxy_origin_and_an_integer`, `task_id_validation_table` |
| A task window invokes main-only commands (connection mutation, daemon lifecycle, updater, quit, app settings, relay passwords) | Separate capability with an explicit smaller list under `task-*`; the proxy capability stays `main`-only; the exact-list test forbids any main-only command in the task list | `capability_task_window_has_no_main_only_command`, `capability_proxy_stays_main_only` |
| A task window's commands act on `main` (theme, ready, context-menu selection delivered to `main`) | Each such command uses the calling window; the armed context menu records the opening label and the selection goes only there | `context_menu_selection_goes_to_the_window_that_opened_it`, `window_commands_use_the_calling_window` (source guards) |
| Window-count exhaustion: a loop of `window_open_task` calls opens windows without bound | Hard pool cap of 8 task windows (plus `main`); a ninth distinct task is refused with a clear error; re-opening a task that has a window opens nothing new | `pool_refuses_beyond_the_cap_with_a_clear_error`, `opening_the_same_task_again_reuses_its_window` |
| Focus stealing: content makes the app grab focus repeatedly | The command only focuses a window for a task the page named, it is reachable only from the bundled UI (never from daemon or agent content, per ADR-0013), and it never raises `main` or any other window; stated limit: the bundled UI could call it without a gesture | `focus_only_the_requested_task_window` (source guard) |
| `window.open` / link targets spawn an unmanaged window | `on_new_window` denies in task windows; navigation outside the loopback origin goes to the OS browser | `task_window_builder_denies_new_windows_and_guards_navigation` (source guard) |
| Pool slot leak (a closed window keeps its slot, eventually blocking all opens) | The slot is released on the window's `Destroyed` event | `closing_a_window_releases_its_slot`, `the_destroyed_handler_releases_the_slot` (source guard) |
| A task window uses the core window permissions to act on another window by label (`core:window:allow-*` accept a target label): its page could minimise or toggle-maximize `main` and close other task windows | Accepted residual: the caller is the bundled UI, `main`'s close only hides it, and `main` already holds the same permissions toward task windows; the permissions are the minimum a drawn caption bar needs | none (stated limit) |
| A subframe on ANY loopback port (a dev-server preview, a local service shown in the UI) matches `remote.urls: http://127.0.0.1:*` and could call a task window's commands (`open_path_default`, `open_in_editor`, `open_external`, `window_open_task`, window controls) | Pre-existing scope of `main`, now covering 8 more windows; each command is validated in Rust. Tightening (pin the proxy port, or check the caller URL in sensitive commands) is a separate item for the Supervisor, NOT done here | none (stated limit; see the ADR amendment) |
| A pooled slot reopens an unrelated task maximised, fullscreen or at the last occupant's frame (window-state keys by label) | The window-state plugin is denied every pool label, so task windows always open at the default size | `window_state_does_not_track_pool_slots` |
| A second open shows a still-hidden task window before its first paint; or a timer from a destroyed window shows its rebuilt successor early | `focus_window` leaves an unpainted window alone (`window_ready` shows it at first paint); the fallback timer holds the window handle, not the label | `an_unpainted_task_window_is_not_shown_by_a_second_open`, `the_fallback_timer_holds_the_window_not_its_label` |
| Layout restore replays a task window's route into `main` | Restore reads and writes the `main` label only (#914) | `a_task_window_never_writes_the_main_layout` |

## Acceptance criteria

- AC1 `window_open_task(task_id)` exists, is allowlisted for `main` and for `task-*`, takes one integer and returns the window's label; non-positive and out-of-range ids are refused.
- AC2 One window per task: a second call for the same task focuses the existing window and creates no new one; a call for a task without a window creates one in the lowest free pool slot.
- AC3 The pool has 8 task slots; the ninth distinct task is refused with a clear error and nothing is created.
- AC4 The URL is built in Rust from the proxy origin, the per-launch secret and the integer id; the page supplies nothing else.
- AC5 Task windows use the standard chrome and minimum size, are created hidden and shown on first paint (or the fallback), deny `window.open`, and keep the navigation guard.
- AC6 Task windows destroy on close and release their slot; `main` still hides to the tray.
- AC7 `main` keeps tray, Dock, badge, single-instance, reopen, notification click, deep-link and layout-save behaviour unchanged.
- AC8 The context-menu selection reaches the window that opened the menu; `window_ready` and `window_set_theme` act on the caller; Reload, Fullscreen, Minimize and Inspect act on the focused window; Zoom applies to all windows.
- AC9 `capabilities/task-window.json` exists with `windows: ["task-*"]`, loopback-only remote scope and the explicit list in ADR-0013's amendment; `proxy.json` stays `["main"]`; the exact-list test pins both and forbids main-only commands in the task list.
- AC10 #914's quit-time layout save and launch restore still read and write `main` only.

## Named tests (Rust PR)

Pure: `pool_labels_are_fixed_and_never_main`, `task_id_validation_table`, `task_url_is_built_from_the_proxy_origin_and_an_integer`, `opening_the_same_task_again_reuses_its_window`, `lowest_free_slot_is_used_first`, `pool_refuses_beyond_the_cap_with_a_clear_error`, `closing_a_window_releases_its_slot`, `a_task_window_never_writes_the_main_layout`, `context_menu_selection_goes_to_the_window_that_opened_it`.
Capabilities: `capability_task_window_scope_is_exactly_the_task_glob`, `capability_task_window_allowlist_exact`, `capability_task_window_has_no_main_only_command`, `capability_proxy_stays_main_only` (the existing exact-list test updated for `allow-window-open-task`), `capability_app_grants_match_build_rs_commands` (extended to the second file).
Source guards: `task_window_builder_denies_new_windows_and_guards_navigation`, `the_destroyed_handler_releases_the_slot`, `window_commands_use_the_calling_window`, `focus_only_the_requested_task_window`, `main_window_still_hides_on_close_and_task_windows_do_not`.

## Mutation checks (Supervisor's second read and mine)

Pool cap removed; a label taken from the page; `task-*` widened to `*`; a main-only permission added to the task list; the armed menu selection sent to `main`; the slot not released on destroy; the URL built from a page string; `on_new_window` removed.

## Not in this plan

macOS window tabbing (probe after Part 1, real window); per-window connection selection; per-window theme sync beyond the calling window; notification clicks focusing an existing task window (they keep targeting `main`, as decided); Windows/Linux verification (the screen is locked too: no real-window evidence in either PR).

## Progress / Validation

### Rust PR

- Peers: two GLM peers (profile peer-glm, no fallback to Sonnet needed): #644 wrote `windows.rs` (pool, registry, URL builder, 9 tests), #645 made the armed context menu remember the opening window (3 tests). I reviewed both diffs and ACCEPTED both as written; I wrote the capability files and their tests, the `window_open_task` command and builder, and the retargeting of `window_ready`, `window_set_theme` and the app-menu actions.
- `cargo test` 246 pass, clippy `-D warnings`, fmt. Review (code-review skill, foreground, two fresh Sonnet sub-agents, Standards and Spec/attack): findings fixed in this PR are a stale `Destroyed` event or a failed build freeing a slot that a live window still holds (now `release_if_gone` with the OS's answer), `window_ready` re-grabbing focus on every reload signal, and the app-wide `menu-action` / update-check events being broadcast to every window (now one window). Left as stated limits or judgement: comment width, duplicated test helper, window-state restores the last occupant's size for a pooled label.
- Mutations (committed code, each failed its named test): task glob widened to `*`, `allow-app-quit` added to the task list, proxy capability widened to task windows, pool cap changed, slot not released on destroy, `window.open` allowed, an existing window not focused, selection sent to `main`.
- Not verified: everything on a real window (screen locked): that the glob matches the pool labels at runtime, the overlay title bar and vibrancy on a second window, the first-paint show, destroy-on-close, focus. Windows and Linux builds.

#### Which tests are behaviour and which are text-pins (stated honestly)

Behaviour tests (they run the code): the pool, registry and URL builder (`windows::tests` pool, label, id, URL, reuse and release cases including `release_if_gone` with the alive flag), the context-menu resolve rules, the capability JSON checks (they parse the real files), `route_fragment`/layout tests from #914. Text-pins (`include_str!` plus `contains` on a function body; they pin that a call or a string is present, not that it works at runtime, and they were mutation-checked): `task_window_builder_denies_new_windows_and_guards_navigation`, `the_destroyed_handler_releases_the_slot`, `focus_only_the_requested_task_window`, `main_window_still_hides_on_close_and_task_windows_do_not`, `an_unpainted_task_window_is_not_shown_by_a_second_open`, `the_fallback_timer_holds_the_window_not_its_label`, `window_state_does_not_track_pool_slots` (its first half is behaviour, the lib.rs wiring half is a pin), the `window_chrome` and `menu` guards, `app_wide_menu_events_are_never_broadcast`. The window-creation, destroy, focus and plugin behaviour itself needs a real window.

