# Desktop attention (DT5): notifications that reach the right card

Desktop-track item 5. The desktop app already raises an OS notification for a pending permission request, counts pending
requests in the tray/Dock badge, and focuses the window on a click. This plan lists only what is missing. No daemon wire
change: every event used below already exists (`permission.pending`, `question.pending`, `run.status`, `permission.resolved`,
`question.resolved`, each with `taskId`, `chatId` and `requestId` where it applies).

## What exists (checked on `wave/desktop` and `develop`, not rebuilt)

- `desktop/daemon-client`: the watcher turns `permission.pending` into a `Notification`, tracks pending requests in
  `attention::Attention` (cleared by `permission.resolved`, a run starting or ending, or a reconnect), resolves the task's
  workspace id into `WorkspaceCache`.
- `desktop/src-tauri`: `notify.rs` shows the OS notification per platform with a click handler that calls
  `navigate_to_task`; `tray.rs` and `lifecycle.rs` drive the tray tooltip, the macOS Dock badge (`Badge`, with retry) and the
  Windows overlay icon from the pending count; `deeplink.rs` routes `smind://` links through the same navigation.
- The daemon's mobile push notifier (`internal/push`) already defines the kinds, dedupe and a rate limit; the desktop does not
  reuse it (it watches the event stream directly) and this plan does not touch it.

## Gaps (all real)

1. **Click-through does not open the task.** `route::DEFAULT_KIND` is `"task"`, a tab kind ADR-0016 removed; the web
   `parseRoute` returns null for `.../task/<id>/task`, so a click only focuses the window.
2. **No exact card.** Even with a valid route nothing scrolls to or focuses the permission or question card of that request.
3. **Only permissions notify.** A question (`question.pending`) and a failed run (`run.status` with status `error`) raise
   nothing, and the Dock badge does not count a waiting question.
4. **No dedupe.** After a reconnect the daemon redelivers pending requests and each one notifies again.
5. **No focus rules.** A notification fires even while the window is focused on that very task.
6. **No coalescing and no quiet while typing.** A burst fires one banner each; a banner can land while the person types.

## Acceptance criteria

### A. Click-through to the exact card (PR A)

1. The route builder produces a route the web app parses: `#/workspace/<ws>/task/<task>/chat/<chat>`, and with a request
   `...?card=<requestId>`. A task with no known chat uses the terminal-independent fallback `.../task/<task>/files`
   (a base kind that exists) rather than a kind the web rejects. The tray's attention item and `smind://` links use the same
   builder (a deep link has no chat or request, so it uses the fallback).
2. The web `parseRoute` accepts `chat/<chatId>?card=<requestId>` (a chat route with an optional request id; any other
   shape of query is ignored, and `formatRoute` of a route with a request id round-trips it).
3. Opening such a route opens the chat tab, then asks the matching card to take focus: a small store keyed by request id
   (like the file reveal store) holds the request; the permission card or question card whose request id matches focuses its
   container (visible focus ring, no animation) and consumes the request, **even when a text field has focus** (the card's own
   mount focus skips that case; a click on a notification is an explicit action). The pending card is pinned above the composer,
   so no scrolling is needed. A card for another request is untouched; a stale request expires; a request whose card never
   appears (already resolved) does nothing.
4. The Rust watcher keeps the chat id and request id with each pending entry so the tray item opens the most recent card.

### B. More kinds, fixed bodies, dedupe, badge, settings and consent (PR B)

5. A `question.pending` and a run that ends with status `error` join `permission.pending` as notification kinds. The text is
   **fixed per kind and never contains prompt, question, command, error or task-title text** (the daemon's push kinds put
   some of these on a lock screen; the desktop is stricter): permission "A run needs your approval" (the push notifier's own
   fixed fallback), question "An agent has a question" (ADR-0055 D7), failure "Run failed". Titles: "sMind: permission needed",
   "sMind: a question for you", "sMind: run failed". `needs_continue` and `done` stay silent. (Today the permission
   notification shows the request's summary, i.e. a command; that goes.)
6. Each (kind, request id or run id) notifies once: a redelivery after a reconnect is ignored (bounded memory).
7. The Dock badge and tray count include waiting questions (`question.resolved` or the run ending clears them); failed runs
   are not counted. The tray label reads "N waiting for you".
8. **Consent and per-kind switches.** The app never assumes notification permission: until the person turns desktop
   notifications on, nothing is shown (the badge still counts). Settings > Notifications (desktop only) shows a "Turn on desktop
   notifications" action that records the consent and sends one test notification (which is what makes macOS ask for its own
   permission); three switches (permission, question, failure) default on and each turns its kind off; a kind that is off
   shows nothing but still counts toward the badge. The values live in `app-settings.json` next to `onboardingCompleted`,
   through the existing `app_settings_get/set` commands; one new allowlisted command `notify_test` sends the test banner. macOS is
   the first target: Windows and Linux use the same logic through their existing notification code but are not verified here.
   The OS-level permission cannot be read back through the plugin; if the person denied it in System Settings nothing shows and
   the settings text says to check System Settings.

### C. Focus rules, coalescing, quiet while typing, badge check (PR C)

Numbering continues after B (AC 5-8). Nothing here changes what a notification says (fixed text, B) or who may see one
(consent and per-kind switches, B); it decides *when* a banner shows. The badge never depends on any of it.

9. **The web app reports its attention state to the shell.** One new desktop IPC command `attention_focus_report`
   `{ taskId: number | null, windowFocused: boolean, composerFocused: boolean }`, allowlisted in `capabilities/proxy.json`, declared in
   `build.rs`, and added on purpose to the exact-allowlist test; it is not a daemon method. The web sends it only when the
   tuple changes (task in front, window focus or blur, composer focus or blur), with rapid changes collapsed into one trailing
   report (100 ms), and sends `windowFocused: false` when the page hides or unloads. The shell keeps it in memory only, validates it
   (`taskId` null or positive) and treats "no report yet" as "nothing focused" (no suppression: a missing report must never
   hide a banner). Nothing is sent in the browser build.
10. **No banner while the window is focused on that task.** A notification whose task is the task in front, with the window
    focused, shows nothing (a failed run on that task included). The badge still counts it.
11. **Quiet while typing.** While the window is focused **and** the composer has focus, a banner for another task is held
    (bounded to 32; the oldest is dropped). Held items are delivered once, together, when the composer or the window loses
    focus; an item whose request was resolved meanwhile (a permission or question no longer pending) is dropped, a failure is
    not (it has no pending state). Items held for the task that then comes to the front are dropped too.
12. **Coalescing.** The first banner of a 3-second window shows at once. Banners that arrive inside the window are queued; when
    the window ends one queued banner is shown as itself, two or more become **one** banner "sMind: N things need you" with the
    fixed body "Open sMind to see what is waiting.", whose click opens the most recent card. The next banner after the flush
    starts a new window. Held items flushed under AC 11 go through the same coalescer.
13. **Badge check.** The Dock badge and the tray count equal the waiting permissions plus questions at every moment,
    independent of focus, holding, consent and the per-kind switches; it clears at zero and keeps the existing retry on a
    failed OS call. (Verified, not rebuilt: it already counts both since B; this adds the test that pins it.)

## Test scenarios

A: `route.rs` (`chat_route_url_with_card`, `fallback_route_has_a_kind_the_web_accepts`, deep link and tray use the builder),
`route.test.ts` (`parses chat route with card`, `ignores unknown query`, `round-trips the request id`),
`card-focus.test.tsx` (permission card and question card each focus on a matching request even with the composer focused, ignore another
request, consume once, expire when stale), `app` route test (opening the route opens the chat tab and queues the reveal),
`attention.rs` (`most_recent_returns_chat_and_request`).
B: `protocol.rs` (`permission_notification_has_a_fixed_body`, `question_pending_becomes_a_fixed_body_notification`,
`failed_run_notification_has_a_fixed_body`, `no_notification_body_contains_event_text`, `done_and_needs_continue_are_silent`), `attention.rs` (`questions_count_and_clear`, `failed_runs_are_not_counted`),
`dedupe.rs` (`a_redelivery_notifies_once`, `the_memory_is_bounded`), `app_settings.rs` (`notification_defaults_are_not_asked_and_kinds_on`, `patch_changes_only_the_named_fields`, `an_old_file_without_the_fields_loads`),
`notify_policy.rs` (`nothing_shows_before_consent`, `an_off_kind_is_silent_but_counts`, `a_kind_on_with_consent_shows`), `capability_tests.rs` (the allowlist gains `allow-notify-test`),
web `notifications-section.test.tsx` (the desktop group: consent action, three switches, test result, hidden in the browser build).
C: `attention_focus.rs` (`no_banner_when_focused_on_that_task`, `a_banner_for_another_task_shows`, `no_report_means_no_suppression`,
`held_while_the_composer_is_focused`, `held_items_flush_once_when_the_composer_loses_focus`, `held_items_resolved_meanwhile_are_dropped`,
`a_held_failure_is_not_dropped`, `the_held_list_is_bounded`, `the_command_rejects_a_non_positive_task_id`),
`coalesce.rs` (`first_shows_rest_collapse`, `one_queued_item_shows_as_itself`, `the_window_restarts_after_a_flush`,
`the_summary_targets_the_latest`, `the_summary_text_is_fixed`, time-injected), `attention.rs` (`badge_count_is_waiting_permissions_plus_questions`,
`the_count_clears_at_zero`), `capability_tests.rs` (the allowlist gains `allow-attention-focus-report`), web `use-attention-report.test.ts`
(reports on a task change, window focus and blur, composer focus and blur; an identical state is not re-sent; rapid changes collapse to one
trailing report; a page hide reports window unfocused; a no-op in the browser build) and a `platform.test.ts` case for the bridge method.

Frames: PR C has no new UI (the reporter has no visible state), so it carries no frames; the logic is covered by the unit tests above
and the PR says so. A real-window run (focus, typing, a real banner) is not possible here (the OS owns the banner, and the screen may be locked).

## Decisions

- Route fallback: a task with no known chat opens its Files tab (a base kind that always exists) instead of a kind the web rejects.
- Notification text is fixed per kind, with no task title either (a task title is the first line of the prompt).
- Consent is the app's own setting (default not asked); the OS permission is the OS's and is asked when the first notification is sent.
- The web reports attention state through a new allowlisted IPC command rather than the shell guessing from the webview URL
  (the URL does not show the composer focus). Reversible: remove the command and the shell falls back to "no suppression".
- Coalescing window 3 s; held-while-typing items are flushed once, not replayed.
- Focus state is memory-only in the shell, and a missing report means no suppression (fail open: a banner is never hidden by a bug).

## Progress

- [x] spec
- [x] A: click-through to the exact card (PR A)
- [ ] B: questions and failed runs, dedupe, badge
- [ ] C: focus rules, coalescing, quiet while typing, badge check (PR C)

## Validation

PR A: `route.rs` (card route, fallback kind, percent-encoding), `protocol.rs` and `attention.rs` (chat id), `notify.rs` (`route_for`), `route.test.ts`,
`card-focus.test.tsx`; daemon-client `cargo test` (305 + doc/integration tests), src-tauri `cargo test --lib` (177), web 471 files / 5305 tests, `tsc -b`,
`vite build`. Mutations on committed code, all killed: stale-request check removed; card focus made to respect a focused text field; overlong card id
accepted; fallback kind back to `task`; `route_for` never using the card route. Frames: 16 (light and dark, 1280 and 390) all opened.
Not verified: a real OS notification and a real click (no real-window run); Windows and Linux click handlers (the same `navigate` path, compiled for macOS only).