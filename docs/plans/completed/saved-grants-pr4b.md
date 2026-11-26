# Saved grants PR4b: the card control, the Saved grants lists, and the mobile question-card polish

Status: active (2026-11-15). Surfaces for the daemon half in PR #886 (`docs/plans/completed/saved-grants.md`: strict classes, runners need a per-grant opt-in, human decision 2026-11-15). Branched from develop; it needs #886 merged to work against a real daemon, and its tests use the wire shape of #886 (fixtures and an injected daemon socket), so it builds and tests without it. Size: M (UI on two surfaces, no daemon change).

## Acceptance criteria

- AC1 A permission request that carries `detail.grant` shows an "Always allow in this project" control on web and mobile beside the single-allow option, with the EXACT scope: the command, the project and folder, how long (30 days, or until revoked), and the class as text ("Read-only" / "Runs project code"), plus the daemon's note. A request with no `detail.grant` (the demo, a floor-judged or git request, any other tool) shows no control, and the demo card is unchanged.
- AC2 A runner offer shows the opt-in "Also allow running this project's code (the agent can change it)": UNTICKED by default, only for the runner class, and the save control is disabled with a stated reason until it is ticked. A read-only offer shows no opt-in. An unknown class, or a runner flagged as needing no opt-in, is treated as a runner (the class decides). The daemon still enforces it; this is the human's view.
- AC3 Saving sends `run.answerPermission {runId, requestId, optionId: <the request's own allow_once option>, scope: "project", allowProjectCode: true}` (the key only for a runner with the opt-in ticked); no class is ever sent by the client. A refusal shows the daemon's words, leaves the card usable and (mobile) rolls the optimistic state back.
- AC4 A Saved grants list (web Settings, mobile screen from the Inbox) shows every grant with its exact command, class, status (active, expired, void with "tool setup changed", revoked), expiry, use count, project folder, whether a runner was opted in, and a Revoke (with a confirmation that says what will ask again) for any grant not already revoked. It cannot create a grant. Commands render as plain text.
- AC5 Mobile question card nits: the card surface is neutral (the shared green badge stays); Decline is a bordered secondary button with a 44 pt target; the note field says "Note for the agent (optional)" with the helper "Sent only if you decline." identical on web and mobile (the daemon sends a note only with a decline); a refused answer reads as words with the daemon's text under Details ("That choice is no longer offered. Pick again.").
- AC6 Light and dark frames at web 1280 and 390, and mobile 390, for every changed state, each pair opened.

## Named tests

Web (vitest): `grant-control.test.tsx` (scope text, read-only no opt-in, runner unticked and disabled until ticked, unknown class is a runner, class decides over the flag, inert text, disabled while sending, the card offers it only with `detail.grant` and a handler, answers with the request's own allow-once option and the opt-in, refusal shown), `saved-grants-section.test.tsx` (registered, empty, every status and class as text, inert command, revoke confirmation copy and call, refusal in the dialog, load error), `question-copy.test.ts`, `question-card.test.tsx` (Details).
Web (Playwright, injected socket): `grants.spec.ts` G-01..G-07 on light-1280, dark-390, dark-1280, light-390.
Mobile (vitest): `grantsView.test.ts` (the opt-in rule, words, expiry, rows, and copy identical to the web's by reading its source), `grantsSafety.test.ts` (only the three calls and their exact params, no class sent, no links or markdown), `permissionRequests.test.ts` (`respondToPermissionAlways`), `questionCopy.test.ts` (copy identical to the web's), `questionSafety.test.ts` (`questionCopy.ts` scanned).

## Decisions

- The control lives on the web options card and the mobile permission card only; the inbox permission row and the demo are unchanged (no grants in the demo).
- The class, the note and the opt-in label come from the daemon's `detail.grant`; the UI adds only the words "Read-only" / "Runs project code" and a fallback opt-in label.
- The list is read-and-revoke only; the list refetches on open and after a revoke (no daemon topic announces a grant).
- Web and mobile copy are kept identical by a mobile test that reads the web source; the web is the source of truth.

## Not in this PR

A refused save for a project at its grant cap is shown as the daemon's raw error on the card (a dedicated sentence is a follow-up). No native device was used: mobile frames are component-level.

## Progress

- 2026-11-15: built and verified (frames in the dogfood directory, README inside).

## Validation

AC1-AC3 `grant-control.test.tsx`, `grantsView.test.ts`, `permissionRequests.test.ts`, G-01..G-04; AC4 `saved-grants-section.test.tsx`, `grantsSafety.test.ts`, G-05, G-06; AC5 `question-copy.test.ts`, `questionCopy.test.ts`, `question-card.test.tsx`, G-07 and the mobile frames 09-11; AC6 the frames.
