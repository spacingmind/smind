# Desktop chat surface: running status, composer chips, model picker, diff toolbar, density

Status: decided (2026-11-15). **Human decision 2026-11-15: all 11 decisions as recommended.** Docs only: no code starts until the cards, shell and sidebar lanes have landed (they share the web package and its stores). Size: L (7): scope 2 (several web components, one shared composer), PRs 3, risk 1 (UI, no wire change), decisions 2 (eleven design choices below), verification 2 (light and dark frames, real window).

## Why

The desktop design review ranked five items for the task view that are not about the permission cards (those shipped separately). Measured on the 1280 by 800 window: the run's "Working… 41m 34s" sits as small grey text under the last message while a second "Working" chip sits above the composer, so the state of the run is shown twice and neither is where the eye is; the composer's chips (provider, mode, auto-accept, thinking) read as plain 11 to 12 px text with no on or off state ("Auto-accept" is just a word); the model picker opens upward as a narrow popover over the transcript; the diff toolbar is a row of eight equal-weight controls, three of them disabled; and the chat's small text and a content column capped at 768 px of a roughly 1000 px pane leave the screen looking sparse while the text is small.

## Scope and ownership

Three PRs, each with one owner scope so they never edit the same file:

- **E-a, chat footer and composer (rank 7, 8, 15):** `components/timeline/run-footer.tsx`, the run view model next to it, `components/composer/composer.tsx`, `components/composer/run-config-toolbar.tsx`, `components/composer/composer-status-chips.tsx`, `components/pickers/mode-select.tsx`, `components/pickers/auto-accept-toggle.tsx`, the column width in `components/task-detail.tsx`, the type-scale tokens used by those components.
- **E-b, model picker (rank 9):** the model picker popover and its provider list (`components/pickers/`), nothing else.
- **E-c, diff toolbar (rank 11):** `components/diff-viewer-pane.tsx`, `components/diff-header-menu.tsx` and the git action wiring they call.

Not in scope: the permission and question cards (shipped), the sidebar and tabs, the desktop shell, mobile (it has its own composer and no diff pane), any daemon or wire change.

## Decisions (each with its recommendation; all taken as recommended by the Human on 2026-11-15)

### Running-turn status (E-a)

1. **Where does the run's state live?** Today there are two: a footer line in the transcript and a chip above the composer.
   - A: keep the footer line, drop the chip. B: one status line anchored directly under the composer (always visible, even when the transcript is scrolled), and the transcript footer only states how a finished run ended. C: leave both.
   - **Recommend B.** The state stays on screen while the user scrolls, it is where the eye is when deciding to send or queue, and the transcript footer stops competing with the last message. The live label keeps its polite live region and the elapsed timer.
2. **What does the status line carry?** Recommend: the working label and elapsed time, the waiting-for-you state (same wording and tone as today), and the branch and diff stat that today sit in the second chip; no new data and no new wire fields.
3. **Collapse finished tool runs?** A finished run of many tool rows is as tall as the run was long.
   - A: always show every row (today). B: after a run ends, three or more consecutive tool rows collapse to one summary row ("Ran 6 tools"), expandable, honouring the existing detail-level setting (a user on the most detailed level sees every row). C: collapse during the run too.
   - **Recommend B.** It only changes what a finished run looks like, it reuses the detail-level control that already exists, and an in-flight run keeps showing each tool as it happens.

### Composer chips (E-a)

4. **How does a chip show its state?** Recommend the state is in the label ("Auto-accept: off" and "Auto-accept: on", with the check or dash icon from the icon registry and `aria-pressed`), never colour alone; the mode and provider chips already name their value. Option: a switch glyph instead of text; not recommended because it reads as a form control inside a text line.
5. **Sizes.** Recommend 28 px high chips and 13 px text at the desktop breakpoint (they are 24 px and 11 to 12 px now), the 44 px touch size unchanged below the compact breakpoint, and the hit area always at least the visible chip.

### Model picker (E-b)

6. **Provider rail.** The picker is a single 240 px list (search, profiles, models). Recommend a two-column popover: a narrow icon rail of providers on the left (icons from the registry, a tooltip and an accessible name each, the current provider selected) and that provider's models on the right, keyboard reachable (arrow keys move through the rail, Tab into the list). A provider with no ready account stays visible and says why, as the picker does today.
7. **Does it cover a pending card?** The picker opens upward from the composer, so while it is open it hides a permission or question card above the composer.
   - A: accept it (the picker is modal and Escape closes it). B: open it downward or sideways when a card is pending. C: move the picker into the header.
   - **Recommend A** for this PR, with the popover's height capped so a card stays partly visible. It is the smallest change that keeps one mental model; if the real window shows it confuses, revisit as C.

### Diff toolbar (E-c)

8. **What is the primary action?** Recommend the review-then-ship order is visible: the two view toggles (By file / Whole diff, Unified / Side by side) become two compact segmented controls on the left; Refresh becomes an icon button; and the three git actions (Publish branch, Update from main, Create PR) collapse into one primary button that shows the next sensible action for the task's state with the others in its menu. The commit box and its button keep their place at the bottom.
9. **Disabled actions.** Recommend a disabled git action stays in the menu with its reason as plain text (the reasons the task menu already gives), instead of three greyed buttons in the row.

### Density (E-a)

10. **Type scale in the chat view.** Recommend raising the smallest sizes used by the composer chips, the task header meta and the status line from the 10 to 12 px steps to 13 px (the caption step) and leaving the message body at 14 px; the tokens exist, no new scale.
11. **Column width.** The message and composer column is capped at 768 px. Recommend widening it to 896 px only when the pane is at least 1100 px wide, leaving the 768 px column below that; and no change to the vertical layout (the gap above the composer in a short chat is the composer being pinned to the bottom, which is expected).

## Acceptance criteria

- AC1 (E-a): exactly one run-state element is on screen while a run is live; it lives under the composer, carries the working or waiting label, the elapsed timer and the branch and diff stat, keeps `role="status"` and `role="timer"` as today, and the transcript footer shows only the ended-run sentence.
- AC2 (E-a): after a run ends, three or more consecutive tool rows render as one expandable summary row, a user on the most detailed level sees every row, and a live run never collapses.
- AC3 (E-a): the auto-accept chip's text includes its state and exposes it with `aria-pressed`; no chip conveys state by colour alone.
- AC4 (E-a): chips are 28 px high with 13 px text at the desktop breakpoint and keep the 44 px touch size below it.
- AC5 (E-a): the column is 896 px wide from 1100 px panes up and 768 px below; the small text steps named in decision 10 are 13 px.
- AC6 (E-b): the model picker shows a provider rail and that provider's models; the rail is keyboard operable and every icon has an accessible name; a provider without a ready account is listed with its reason.
- AC7 (E-b): the picker's height is capped so a pending card above the composer stays partly visible, and Escape closes it.
- AC8 (E-c): the diff header shows two segmented view controls, a Refresh icon button and one primary git action with the rest in its menu; a disabled action shows its reason as text in the menu.
- AC9 (all): light and dark frames at web 1280 and 390 for every changed state, every pair opened, luminance and no horizontal overflow asserted; the full web suite, `tsc`, `task lint` and the hygiene test pass.

## Test scenarios (named)

E-a: `run-state-shown-once-under-the-composer`, `run-state-keeps-live-region-and-timer`, `transcript-footer-only-states-an-ended-run`, `finished-run-collapses-three-or-more-tool-rows`, `most-detailed-level-never-collapses`, `live-run-never-collapses`, `auto-accept-chip-states-on-or-off-in-text`, `chip-state-never-by-colour-alone`, `chips-are-28px-with-13px-text-on-desktop`, `column-widens-only-from-1100px`.
E-b: `picker-lists-providers-in-a-rail`, `picker-rail-is-keyboard-operable`, `picker-provider-without-account-says-why`, `picker-height-is-capped`, `picker-closes-on-escape`.
E-c: `diff-header-has-two-segmented-view-controls`, `diff-primary-git-action-follows-task-state`, `diff-disabled-action-shows-its-reason`.
Mutations (each must fail a named test): show the footer and the status line together; collapse a live run; collapse at the most detailed level; drop the state from the chip text; chips back to 24 px; widen the column below 1100 px; remove the rail's accessible names; remove the picker height cap; render every git action as its own button again.

## Staffing and order

One Lead per PR scope. Coding Peers are GLM first with step-by-step briefs (E-a is the largest and splits into the footer, the chips and the width/type steps). The three PRs are independent once the decisions are answered; E-a goes first because it touches the shared composer. Nothing starts until the cards, shell and sidebar lanes have merged into the wave branch.

## Risks

The composer is shared with the mobile web target through the same chips in some views: the chip changes must be checked at 390 wide. Collapsing tool rows changes what the chat find and the outline rail see: both must still match text inside a collapsed summary or the find would silently miss it. The status line moving under the composer changes the composer's height, which the scroll-to-latest logic measures.

## Progress

- [x] spec (this file)
- [x] decisions answered by the Human (2026-11-15: all 11 as recommended)
- [x] E-a: status line under the composer (GLM peer 649), chip state text / 28 px / 13 px / 896 px column (GLM peer 650), finished-run tool-row folding (Lead); one PR from `smind/dtui-e-a-chat-surface`
- [x] E-b (PR open 2026-11-15, `smind/dtui-e-b-model-picker`). Design: the rail exists on the DESKTOP popover only (the mobile sheet keeps its root list and drill-down unchanged), when more than one provider is visible; there the root list and the Back button are gone (the rail replaces them), the right column is the selected provider's models, and typing searches ALL providers (the search results view) while the rail keeps its selection; ArrowLeft or Backspace on an empty search moves focus to the rail, ArrowUp/Down inside the rail select the neighbour, Tab goes rail -> search -> list; a provider without a ready account stays in the rail with a warning dot, its reason in the accessible name and tooltip, and its view shows the reason; the popover is 25 rem wide and capped at 400 px so a pending card stays partly visible. Also folded in: the header detail control reads "Detail: Auto" with a chevron (the transcript detail level), not a bare "Auto". Staffing: a GLM peer writes the rail component and the header label; the Lead integrates the rail into the picker and its tests.
- [x] E-c (PR open 2026-11-15)

## Validation

### E-b
- AC6: `picker-lists-providers-in-a-rail`, `picker-rail-is-keyboard-operable`, `picker-provider-without-account-says-why` (component tests in `provider-rail.test.tsx` and the wired ones in `provider-model-picker-rail.test.tsx`). AC7: `picker-height-is-capped` (400 px cap, `min(400px, ...)`), `picker-closes-on-escape`. Also: typing searches all providers, the Profiles tab, the current unavailable provider stays pickable, short reasons, the sheet keeps its root list, `detail-control-reads-as-the-transcript-detail-level`. Full web suite 5622 passed, `tsc` clean; the full e2e matrix (`E2E_FULL=1`, four projects) 223 passed, 41 skipped, no flaky.
- Mutations (each killed by a named test): rail never active; an unavailable provider's models shown; typing stays in the provider; Left does not enter the rail; rail Arrow does not focus; popover not wider; Profiles tab removed; the height cap lifted; the warning dot removed; the rail on mobile.
- Design change recorded: the old root list (Profiles band + Providers drill-down) now exists only in the mobile sheet; the desktop popover has a Profiles tab instead. Existing picker, composer, toolbar, profiles-section, task-detail and client-state tests that went through the root list were moved to the sheet or to the Profiles tab, with the same assertions.
- Not verified: real-window desktop; providers with no brand mark share a generic icon, so the rail can show several identical glyphs (a follow-up could add an initial).

### E-c
- AC8: `diff-header-has-two-segmented-view-controls` (also asserts the Refresh icon button and a single git control), `diff-primary-git-action-follows-task-state` (model table: ready -> Create PR, unpublished -> Publish, behind -> Update, open PR -> Update PR, merged -> Archive), `diff-disabled-action-shows-its-reason` (plain text under the label, reachable, selecting it does nothing), plus the primary runs its action, a disabled primary keeps its reason, the forge-setup link, the update error card, and no dead control while the git state is unknown. The Diff header's `SyncButtons` is removed (the narrow header and the task menu keep `SyncMenuItems`).
- Primary-action rule: archive when merged; else the PR step (Update PR or Create PR) when enabled; else Publish/Push; else Update from base; with nothing enabled the first step in flow order, disabled. View PR is never primary.
- Not done here: the desktop header keeps the two existing segmented controls as they were (they already are compact segmented controls); the narrow (mobile) header is unchanged.

### E-a
- AC1: `run-state-shown-once-under-the-composer`, `run-state-keeps-live-region-and-timer`, `transcript-footer-only-states-an-ended-run`, `status-line-chips-are-28px-with-13px-text`. AC2: `finished-run-collapses-three-or-more-tool-rows`, `most-detailed-level-never-collapses`, `live-run-never-collapses`, `find-open-never-collapses`, the default-level test, and a RunTimeline test ("Ran 3 tools"). AC3: `auto-accept-chip-states-on-or-off-in-text`, `chip-state-never-by-colour-alone`. AC4: `chips-are-28px-with-13px-text-on-desktop`. AC5: `column-widens-only-from-1100px` (also measured: 768 px at 1280, 896 px at 1600). Full web suite 5568 passed, `tsc` clean.
- Mutations (each killed by a named test): footer shows a live run again; status line moved back above the card; timer or status role dropped; chip text without the state; same icon for both states; chip text back to 12 px or height to 24 px; status chips back to 12 px; column widening at 700 px; auto collapsing a live run, collapsing at the detailed level, collapsing two rows, default level detailed, find not expanding, RunTimeline ignoring `finished`, summary wording reverted.
- Frames: see `~/.smind/dogfood/dt-ui-e-a/` (README): waiting (real), working/live/tools (patched websocket results), idle at 1280/390/1600; every pair opened.
- Decision 3 detail: the existing header control gets a third level, `auto` (default), cycling auto, detailed, overview; a stored `detailed` or `overview` keeps its meaning. Chat Find turns `auto` into `detailed` while it is open (it walks the rendered DOM, so folded rows would be missed).
- Not done in E-a: the task header meta and the other small texts of decision 10 (only the composer chips, pills and the status line moved to 13 px). Not verified: real-window desktop; a real multi-tool run (the tool rows and the running state in the frames are injected).

