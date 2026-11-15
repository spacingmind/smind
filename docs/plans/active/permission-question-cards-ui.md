# Permission and question cards: one calm surface, a clear Allow/Deny, Deny reason on request (web + mobile)

Status: active (2026-11-15). Size: M (6): scope 2 (web and mobile), PRs 1, risk 1 (UI, but the cards gate every agent run), decisions 0 (the Human decided the shared badge stays), verification 2 (light/dark frames at 1280 and 390, both test suites).

## Why

The desktop design review ranked these cards first. Today the permission card is a card inside a card (the shared "Waiting for your response" container plus a warning `Alert`), its command text and buttons are 11 px with a 24 px (`h-6`) desktop button, Allow and Deny are two filled buttons of equal weight, and a Deny-reason text area as big as the command is always open, so the eye lands on an empty box. On mobile the same reason box is always open and a resolved row can fall back to a raw option id. The question card is already a keep-your-answers form but its error sits above the fields, far from the Submit it belongs to.

## Decisions

- The shared green "Waiting for your response" badge stays exactly as it is (Human's parity decision b): same element, test id, classes and text on both cards.
- One surface: the permission variants render straight inside the shared card container, with no nested bordered `Alert`. The summary is a heading row (warning icon from the icon registry, 14 px medium text); the detail view and the buttons follow.
- Weight: the first allow option stays the filled `approval` button; other allow options are outlined; reject options are the outlined variant with destructive text, so Deny is clearly present but quieter than Allow. Desktop buttons are 32 px (`md:h-8`, 14 px text); below the compact breakpoint the 44 px touch size stays.
- Deny reason is on request: a text button "Deny with reason" (only when a reject option exists) reveals the existing `DenyReasonField`; the field's text survives collapse and re-open and is sent only with a reject answer, exactly as today. Mobile does the same with a `TextButton` and its existing multi-line input.
- Errors are plain sentences next to the buttons, never a raw message alone: "Could not send your answer. Try again." with the technical detail on a second, muted line. Typed text and selected answers are kept and the buttons come back. The question card's error moves to directly above Submit/Decline.
- Plain text for every resolution: the web row keeps its labels ("You approved", "You denied", "Timed out", ...) and an unknown reason reads "Resolved" instead of its raw wire text; mobile reads "You allowed this", "You denied this" or "Resolved by the agent" instead of `chosen: <option id>` and never shows a raw option id. (The `chose:deny-1` text seen in earlier frames was the test agent's own reply, not interface text.)
- The cards' word list is checked against the safety test for steward and question sources (no `merge`, `approve` or `always allow` wording in question sources); question-card copy changes must pass both suites.
- No animation is added: the reason field appears and disappears with the toggle. Frames capture both states and the error state.

## Acceptance criteria

- AC1 (web): the permission card has no nested `Alert`; the summary row uses a registry icon and 14 px text; the card keeps `permission-waiting-badge` unchanged.
- AC2 (web): the first allow option is `approval`, other allows `outline`, rejects `outline` with destructive text; desktop height 32 px, touch height 44 px.
- AC3 (web): the Deny reason field is not in the document until "Deny with reason" is pressed (`aria-expanded` reflects it); typed text survives a collapse and re-open and is sent with a reject answer only; the button is absent when no reject option exists.
- AC4 (web): a failed answer shows the plain sentence plus the detail above the buttons (`role="alert"`), keeps the typed reason, and re-enables the buttons; the question card does the same, keeps every draft, and shows its error directly above Submit/Decline.
- AC5 (web): an unknown resolution reason renders "Resolved", never raw text.
- AC6 (mobile): the reason input is hidden until "Deny with reason" is pressed; text survives collapse; sent only with a reject tap; hidden with no reject option.
- AC7 (mobile): the resolved row reads "You allowed this" / "You denied this" / "Resolved by the agent" and never an option id or `?`; the error line is the plain sentence plus detail; the question card keeps its answers after a failed submit and shows the error above its buttons.
- AC8: light and dark frames at web 1280 and 390 and mobile 390 for: pending (command, with the heredoc link), reason expanded, error, resolved rows, question card (single, multi, text, error); every pair opened and compared.
- AC9: both suites pass (`task test` web part and the mobile vitest suite), `task lint`, and `go test ./internal/hygiene/...`.

## Test scenarios

Web (`permission-card.test.tsx`, `question-card.test.tsx`, `permission-detail.test.tsx`, timeline tests): `permission-card-has-no-nested-alert`, `permission-card-keeps-the-waiting-badge`, `option-variants-allow-filled-deny-outline-destructive`, `deny-reason-collapsed-until-requested`, `deny-reason-text-survives-collapse`, `deny-reason-only-with-reject-answer`, `deny-with-reason-absent-without-reject-option`, `respond-failure-shows-plain-sentence-and-keeps-reason`, `question-error-sits-above-submit-and-keeps-drafts`, `unknown-resolution-reason-reads-resolved`.
Mobile (`mobile/src/__tests__`, a new `permissionCardView.test.ts` for the pure copy and a render test of the card): `reason-hidden-until-deny-with-reason`, `reason-survives-collapse`, `resolved-copy-never-shows-an-option-id`, `resolved-copy-by-kind`, `error-copy-is-plain-with-detail`, `question-keeps-answers-after-failed-submit`.
Mutations (each must fail a named test): put the nested Alert back; make the reason field always rendered; send the reason with an allow; drop the plain error sentence; clear the reason on collapse; resolve copy falls back to the option id; unknown reason shown raw; remove the waiting badge; reject rendered `default` (filled).

## Progress

- [ ] spec (this file)
- [ ] web cards (peer, GLM)
- [ ] mobile card (peer, GLM)
- [ ] frames, light and dark, web 1280 and 390, mobile 390
- [ ] mutations, full suites, review
- [ ] PR to `wave/desktop`

## Validation

Filled in when the PR is ready.
