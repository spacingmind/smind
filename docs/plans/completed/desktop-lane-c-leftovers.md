# Desktop Lane C leftovers (inbox grant control, terminal surface, palette subtitle, settings type scale, chat density)

Status: completed (2026-11-15). Five small PRs on `wave/desktop`, one at a time: C, A, B, D, E. Only C was HIGH-RISK (permission UI); C is dropped (below).

## PR C: the inbox permission card offers "Always allow in this project" (DROPPED)

**Dropped by the Human decision of 2026-11-15:** saved grants stay in the task view only. ADR-0045 ("no always-allow shortcut on the inbox card") and `inbox-safety.test.ts` are unchanged; nothing from this section shipped. The criteria below are kept only as the record of what was proposed.

### Problem

`RequestCard` in `web/packages/ui/src/components/inbox/inbox-screen.tsx` renders the task page's `PermissionCard` without `onAlwaysAllow`, so the scoped always-allow control (ADR-0055 PR4b) is missing from the inbox. The mobile inbox never renders a card (a permission row only opens the task, whose `PermissionCard` already passes `onAlwaysAllow`), so mobile needs no code change; its existing tests stay the guard.

### Acceptance criteria

1. The inbox card shows the same `GrantControl` as the task page when the request carries a grant offer and an `allow_once` option.
2. A runner offer: the opt-in is unticked by default, save is disabled until it is ticked, then sends `allowProjectCode: true`. A read-only offer sends no `allowProjectCode`.
3. A request with no grant offer (a git rule carries none) shows no control.
4. Saving calls `run.answerPermission` with `{runId, requestId, optionId: <the request's own allow_once id>, scope: "project"}` and no new RPC; the inbox list refreshes afterwards.
5. A failed save shows the error on the card and keeps the control usable.
6. The card's plain Allow / Deny path is unchanged (`run.respondPermission`).

### Named tests

- `use-inbox.test.tsx`: `answerPermissionAlways params: scope project, allowProjectCode only when true`.
- `inbox-screen.test.tsx`: `InboxGrant: a read-only offer saves with the request's allow_once option`, `InboxGrant: a runner offer starts unticked and save is disabled until ticked`, `InboxGrant: no offer, no control`, `InboxGrant: a failed save shows the error`.
- Frames (light and dark, 1280 and 390): inbox with the opt-in unticked and ticked.

### Decisions

- Reuse `run.answerPermission` through a new `useAnswerPermissionAlways` in `use-inbox.ts`, mirroring `useRespondPermission` (one place per surface, same wire).
- Mobile: no change (see Problem).

## PR A: terminal surface matches the pane

AC: the terminal background and cursor accent are the pane surface (`--panel`, what `bg-panel` paints), light and dark. Decision: the `--terminal-bg` token keeps its reference-parity value (the token test forbids dark overrides, and plugins read it); the terminal resolves `--color-panel` instead. Test: `lib/terminal-theme.test.ts`, `terminal-pane.test.tsx` (wrapper class `bg-panel`). Validation: frames light/dark 1280/390 (inset == pane colour asserted); tabs e2e specs on 4 projects.

## PR B: the palette's task subtitle is the project name

AC: a Tasks row's subtitle is its workspace title (path when untitled), never the branch slug; the slug stays a search keyword. Test: `App.test.tsx` "a task row's subtitle is its project name, not the branch slug, and the slug still finds it" (fails when the subtitle is reverted to the branch). Shipped in #939.

## D: settings type scale (audit, no change)

Measured, nothing off: the settings source uses only `text-ui-base`, `text-ui-sm`, `text-ui-lg` and `text-ui-settings-title`, no raw sizes. Computed sizes across all 15 sections at 1280 are 14, 12, 24, 10 and 16 px, every one a token step; the nav items are 14 px, the same as the app sidebar's task rows (group labels 12 px, page title 24 px). Not type scale, noted for later: General's content column is narrow while Providers' is wide, and Settings shows the app sidebar and the settings nav side by side (two 256 px columns).

## E: chat density (measured, no change)

At 1280x800: the text column is 768 px in a 1023 px pane (E-a widens it to 896 px only from 1100 px panes, decision 11 of `chat-surface-ui.md`), the composer is 736 px, the header and the prompt are 14 px, the chips 13 px, and the gap from the last message's meta line to the composer is 33 px. The empty band in a short chat is the composer pinned to the bottom, which decision 11 calls expected. Nothing left after E-a.

## Progress / Validation

- A: #937 merged. B: #939 merged. C: dropped (Human decision). D and E: audited, no change, results above. Frames and measurements were kept as local scratch material, not committed.
- Every acceptance criterion of the shipped items is confirmed in its PR body (unit, frames light and dark at 1280 and 390, local e2e); the not-verified items (Windows and desktop shells) stay CI-only. This plan is complete.
