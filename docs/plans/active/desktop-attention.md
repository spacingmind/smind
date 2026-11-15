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
3. Opening such a route opens the chat tab, then asks the matching card to reveal itself: a small store (like the file
   reveal store) holds `{chatId, requestId}`; the permission card or question card whose request id matches scrolls into view,
   takes focus on its container (visible focus ring, no animation) and the request is consumed; a stale request expires; a card
   for another request is untouched; if the request is already resolved nothing happens.
4. The Rust watcher keeps the chat id and request id with each pending entry so the tray item opens the most recent card.

### B. More kinds, dedupe, badge (PR B)

5. A `question.pending` raises an OS notification "sMind: a question for you" with a fixed body that never contains the
   question text (the same rule as the mobile push); a run that ends with status `error` raises "sMind: run failed" with the
   task title as body when it is known; `needs_continue` and `done` stay silent.
6. Each (kind, request id or run id) notifies once: a redelivery after a reconnect is ignored (bounded memory).
7. The Dock badge and tray count include waiting questions (`question.resolved` or the run ending clears them); failed runs
   are not counted. The tray label reads "N waiting for you".

### C. Focus rules, coalescing, quiet while typing (PR C)

8. The web app reports its attention state to the shell (new desktop IPC command `attention_focus_report`, allowlisted in the
   capability file and its exact-allowlist test; it is not a daemon method): the task in front, whether the window is focused,
   whether the composer has focus.
9. No notification while the window is focused on that task (the badge still updates).
10. While the window is focused and the composer has focus, notifications are held (badge still updates) and delivered once,
    coalesced, when the composer or the window loses focus, if still pending.
11. A burst coalesces: the first notification of a 3-second window shows at once; the rest of the window collapse into one
    "N more need you" notification at the end; clicking it opens the most recent card.

## Test scenarios

A: `route.rs` (`chat_route_url_with_card`, `fallback_route_has_a_kind_the_web_accepts`, deep link and tray use the builder),
`route.test.ts` (`parses chat route with card`, `ignores unknown query`, `round-trips the request id`),
`card-reveal.test.tsx` (permission card and question card each reveal on a matching request, ignore another request, consume
once, expire when stale, do nothing for a resolved request), `app` route test (opening the route opens the chat tab and queues the reveal),
`attention.rs` (`most_recent_returns_chat_and_request`).
B: `protocol.rs` (`question_pending_becomes_a_fixed_body_notification`, `failed_run_notification_uses_the_task_title`,
`done_and_needs_continue_are_silent`), `attention.rs` (`questions_count_and_clear`, `failed_runs_are_not_counted`),
`dedupe.rs` (`a_redelivery_notifies_once`, `the_memory_is_bounded`).
C: `focus.rs` (`no_notification_when_focused_on_that_task`, `held_while_composer_focused_then_flushed_once`,
`held_items_resolved_meanwhile_are_dropped`), `coalesce.rs` (`first_shows_rest_collapse`, `window_resets`, `summary_opens_the_latest`),
`capability_tests.rs` (the allowlist includes the new command and nothing else), web `use-attention-report.test.ts` (reports on task,
focus and composer-focus changes; no report storm).

Frames (web, light and dark, 1280 and 390): the permission card and the question card focused after a click-through (the
focus ring), a resolved request (nothing focused). Real-window frames and a real OS notification are not part of this plan's
checks: the screen may be locked and the OS owns the banner; the Rust logic is covered by unit tests and the web half by frames,
and the PRs say so.

## Decisions

- Route fallback: a task with no known chat opens its Files tab (a base kind that always exists) instead of a kind the web rejects.
- The notification text for a question is fixed (no question text), for a failed run the task title only (no error text).
- The web reports attention state through a new allowlisted IPC command rather than the shell guessing from the webview URL
  (the URL does not show the composer focus). Reversible: remove the command and the shell falls back to "no suppression".
- Coalescing window 3 s; held-while-typing items are flushed once, not replayed.

## Progress

- [x] spec
- [ ] A: click-through to the exact card
- [ ] B: questions and failed runs, dedupe, badge
- [ ] C: focus rules, coalescing, quiet while typing

## Validation

(filled in per PR)
