# UX audit small fixes, PR D: deny reason field polish, labelled chips on the task composer

Follow-up nits from the review of PRs B and C (see `ux-audit-small-fixes.md` and `ux-audit-small-fixes-c.md`).

## Acceptance criteria

1. **Deny reason is a small auto-growing textarea** (web and mobile): two lines at rest, growing to three, then scrolling; Enter inserts a newline; label and placeholder unchanged; disabled while a response is in flight.
2. **Counter near the limit.** From 400 characters the field shows "used / 500"; at 500 it says the limit is reached and further typing is refused. Nothing is shown below 400.
3. **Runes, one constant.** The web counts characters the way the daemon does (runes, `Array.from`), not UTF-16 units; the cap and the counter threshold live in one place per client (`lib/deny-reason.ts` on web; one exported constant in `mobile/src/permissionRequests.ts`), and no other literal 500 remains in the card code.
4. **Labelled chips on the task composer at 390.** The task composer's model, effort and mode chips keep their text labels below `sm` (the `showLabel` prop from PR C) and wrap onto their own row above the attach and send controls, as on the home composer. At `sm` and above the task composer renders as before.

## Test scenarios (named)

- web: `deny_reason_textarea_grows_to_three_lines`, `deny_reason_counter_from_400`, `deny_reason_counts_runes_not_utf16`, `deny_reason_at_limit_refuses_more_text`, the existing `deny_reason_is_sent_only_with_deny` / `allow_never_sends_the_reason` / `old_onRespond_callers_unchanged` stay green (the field is a textarea now: update them)
- web: `task_composer_chip_labels_visible_below_sm`
- mobile: `denyReasonParam` keeps its tests; add `denyReasonCounter` tests (hidden below 400, shown from 400, flagged at 500) and a rune-vs-UTF-16 case

## Decisions

- Enter in the reason field is a newline (the Deny button is the only submit), so a multi-line reason is possible.

## Validation

- Web: full vitest (460 files, 5209 tests, includes `src/demo`) and `tsc -b` pass; mobile vitest (110 files, 1049 tests) and tsc pass. Mutations on committed code: dropping the rune clamp in the field fails `deny_reason_at_limit_refuses_more_text` and `deny_reason_counts_runes_not_utf16`; dropping `showLabel` on the task composer's mode chip fails `task_composer_chip_labels_visible_below_sm`.
- Frames (demo build, web 1280 and 390, light and dark, luminance and no horizontal overflow asserted): the Deny reason field empty, filled, near the limit (420 / 500) and at the limit (500 / 500, a longer paste is cut at 500); the task composer with labelled chips (wrapped on their own rows at 390, one row at 1280). The first dark frame showed the field with no visible border, so the field now uses the shared input tokens.
- Not verified: the mobile field (no device or simulator: logic tested, layout not seen); a real daemon send with a multi-line reason; the field disabled mid-response in frames; transition frames.
