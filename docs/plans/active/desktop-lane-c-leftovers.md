# Desktop Lane C leftovers (inbox grant control, terminal surface, palette subtitle, settings type scale, chat density)

Status: active (2026-11-15). Five small PRs on `wave/desktop`, one at a time: C, A, B, D, E. Only C is HIGH-RISK (permission UI).

## PR C: the inbox permission card offers "Always allow in this project"

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

## Progress / Validation

(filled in per PR)
