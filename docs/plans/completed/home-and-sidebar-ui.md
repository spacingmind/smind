# Home first run, the trimmed-history notice, and the sidebar rows

Status: active (2026-11-15). Three small PRs in this order: the history notice (rank 4), the sidebar (rank 5), the first-run home (rank 3). Size: M (5): scope 1 (web package, one mobile word list check), PRs 3, risk 1 (UI), decisions 1 (below, recommendations taken as the default), verification 2 (light and dark frames, web and mobile suites).

## Why

The desktop design review found three problems in the first minute of use. Every finished run opens with "Showing a summary: large tool output was trimmed" even when nothing was trimmed (the daemon stores every finished run as a skeleton and the reason `rebuild_not_requested` only says "nobody asked for the full history"). The sidebar row gives its width to the diffstat first, so a task title is cut ("Refactor config loa…") while "+2 -0" is fully shown, and the "Needs attention" bucket only exists after switching the group mode. And a new user with no project sees a composer that cannot be used and one small "Add project" button.

## PR 1: the notice only when something was trimmed (rank 4)

What the skeleton really drops, from `internal/runs/skeleton.go` and `internal/store`: thinking text is not kept (the row says how big it was); a tool payload over the inline cap moves to the excerpt tier and is served whole up to 8 KiB, else as head + `…[N bytes omitted]…` + tail; an answer or user message over its row cap keeps head and tail the same way. So the reader can tell whether anything is missing from the rows it was given.

Decision (recommendation taken): a client-side rule `runWasTrimmed(items)` in `lib/history-source.ts`:
- trimmed when any assistant or user text, tool input or tool result contains the omission marker (`…[` digits ` bytes omitted]…`), or any `thinking` item has empty text (its text was not kept);
- the notice still shows for every reason other than `rebuild_not_requested` (those are real degradations: no session, drift, pruned excerpts), and for `rebuild_not_requested` only when `runWasTrimmed` is true;
- copy: unchanged "Showing a summary: large tool output was trimmed." when a marker was found; "Showing a summary: the agent's reasoning was not kept." when only thinking text was dropped; both keep the "Load full history" action when the daemon offers it.
No daemon or wire change. A run with no thinking and no large payload shows no notice and no "Load full history" (the action is still reachable from the run's menu where it exists today).

Acceptance criteria: AC1 a run with nothing trimmed and reason `rebuild_not_requested` renders no `history-source-note`; AC2 a marker in a tool result, a tool input or an answer renders the large-output copy; AC3 an empty thinking item alone renders the reasoning copy; AC4 any other skeleton reason renders its note as before regardless of content; AC5 `rebuilt` is unchanged.
Named tests: `no-notice-when-nothing-was-trimmed`, `notice-when-a-tool-result-has-the-omission-marker`, `notice-when-an-answer-has-the-omission-marker`, `notice-when-only-thinking-was-dropped`, `other-skeleton-reasons-still-show-their-note`, `run-was-trimmed-table` (marker spellings, empty and non-empty thinking, nested tool payloads). Mutations: always show for `rebuild_not_requested`; never show for it; ignore the marker in tool inputs; treat any thinking item as trimmed.

## PR 2: the sidebar rows (rank 5)

1. **The title keeps its width.** The diffstat and the git glyph are dropped by a container query below 14 rem; the 256 px default sidebar is wider than that, so the title is cut first. Change: the row's secondary parts (diffstat, git glyph) hide below 18 rem and the title gets `min-w-0 flex-1` priority; below 18 rem the diffstat stays reachable in the hover card and the tooltip. No second line (it would make rows taller than the 32 px the list is built on).
2. **A visible "Needs you" group.** Today the grouped view's first bucket is called "Needs attention" and exists only in the status view. Changes: rename the bucket label to "Needs you" (a permission ask is a pending decision only the person can make; an unseen finish is split into its own "Ready to review" bucket, because frames showed it filling the pinned section with rows of green dots); and in the default workspace view show a pinned "Needs you" section above the workspaces, only while at least one task is in that bucket, listing those tasks (a task appears there and under its workspace, the same row, the same selection). It uses the existing `groupTasksByStatus` and the existing attention data; no new daemon field. The status view keeps all buckets, now six: needs-attention, ready-to-review, error, running, done, idle. Deviation from the first draft, recorded here: the pinned section lists permission asks only.
3. **Selection.** The selected row is already a rounded (`rounded-lg`) fill inside the sidebar's row inset, and the active-hover fill is a lighter token; the review's complaint was the fill reading as a flat bar. Change: none to geometry; pin it with a test (rounded, inset from both edges, `bg-selected`) and check it in frames; inactive-window dimming belongs to the native pass, not here.
Acceptance criteria: AC6 at the default width a title that fits without the diffstat is not truncated; AC7 below 18 rem the diffstat and glyph are hidden and the title is not; AC8 "Needs you" is the label of the first status bucket; AC9 the workspace view shows a pinned "Needs you" section only when a task needs the person, with rows identical to the tree's; AC10 selecting a row from the pinned section selects the same task; AC11 the selected row keeps its rounded inset fill.
Named tests: `row-title-keeps-priority-over-the-diffstat`, `row-secondary-parts-hide-below-18rem`, `status-bucket-is-labelled-needs-you`, `pinned-needs-you-appears-only-when-a-task-needs-the-person`, `pinned-needs-you-rows-select-the-same-task`, `pinned-section-hidden-while-searching`, `selected-row-is-a-rounded-inset-fill`. Mutations: restore the 14 rem breakpoint; always render the pinned section; drop a task from its workspace when it is pinned; rename the label back.

## PR 3: the first-run home (rank 3)

The guided Get started card (ADR-0059 item A, merged to `develop`) shows agent and project rows on the home screen but the composer under it is still unusable with no project. Dependency: that work must be in this wave branch first (the Supervisor merges `develop` in); this PR starts from the merged code and re-reads it, so the exact components are named then.
Decision (recommendation taken): with no project registered, the main area shows two action cards, "Add a project" and "Set up providers", built from the Get started block's own rows and actions (the same readiness data and handlers), with a third slot left empty and a comment where an "Import a session" card goes later; the composer is not rendered until a project exists; with a project the home is as today, and the Get started card keeps its dismissal.
Acceptance criteria: AC12 zero projects: two cards and no composer; AC13 each card runs the action the Get started row runs; AC14 one project or more: the composer returns and the cards are gone; AC15 the card actions are real buttons with text labels, keyboard reachable; AC16 no import card, no dead slot visible.
Named tests: `zero-projects-shows-two-action-cards-and-no-composer`, `add-a-project-card-opens-the-add-project-flow`, `set-up-providers-card-opens-providers-settings`, `first-project-brings-the-composer-back`, `cards-are-buttons-with-text-labels`, `no-import-card-yet`. Mutations: render the composer with zero projects; swap the two actions; render the cards with a project present.

## Frames

Light and dark, web 1280 and 390, for every changed state, every pair opened: PR 1 (a run with a marker, a run with nothing trimmed), PR 2 (long titles with and without a diffstat at the default width, the workspace view with and without the pinned section, the status view, selection and hover), PR 3 (zero projects, first project added). Luminance and no horizontal overflow asserted by the frame script. Mobile: PR 1 and PR 3 change no shared mobile label; if a shared string changes the mobile suite runs too.

## Staffing

GLM Peers first with step-by-step briefs, one Peer per PR; the Lead reviews each diff, runs the suites, mutations and frames. Each PR branches fresh from the wave branch and is reviewed before the next starts.

## Progress

- [x] spec (this file, first commit of PR 1)
- [x] PR 1 history notice (GLM peer task 643; reviewed and accepted)
- [x] PR 2 sidebar (GLM peer task 646; reviewed and accepted)
- [x] PR 3 first-run home (develop merged into the wave branch via #931; PR open 2026-11-15)

## Validation

### PR 1
- AC1-AC5: `no-notice-when-nothing-was-trimmed`, `notice-when-a-tool-result-has-the-omission-marker`, `notice-when-an-answer-has-the-omission-marker`, `notice-when-only-thinking-was-dropped`, `other-skeleton-reasons-still-show-their-note`, `run-was-trimmed-table`, `describe-history-source-thinking-copy`; full web suite passes, `tsc` clean.
- Mutations (7, each killed by a named test): always show the note for `rebuild_not_requested`; never show it; marker ignored in tool inputs; marker ignored in answers; any thinking item counts as trimmed; the thinking copy never used; a loose marker pattern.
- Frames: light and dark, 1280 and 390, a run with nothing trimmed (no notice) and the same run with an injected omission marker (notice with its action); 8 frames, luminance and no-overflow checks pass, every pair opened.
- Not verified: a real skeleton run with a genuinely large tool result (the marker is injected into the attach result); the thinking-only copy in a frame; real-window desktop.

### PR 2
- AC6-AC11: the named tests above plus `pinned-needs-you-excludes-tasks-that-only-finished` and the bucket-split table in `sidebar-status-groups.test.ts`; full web suite 5473 passed, `tsc` clean. No mobile file reads these buckets.
- Frames: pinned section, selection from the section, collapsed section and status view, light and dark, web 1280 and 390 (the 390 sidebar is the drawer); 24 frames, luminance and no-overflow pass, every pair opened. The quiet state (six unseen finishes, no pending ask: no pinned section; all seen: none) was re-framed on the final code after the bucket split, and `pinned-section-absent-when-many-tasks-are-unseen-finishes-and-nothing-is-pending` pins it (mutations feeding the section from the ready bucket, from needs-attention plus ready, or routing finishes to needs-attention each fail it).
- Mutations: see the PR body.
- Not verified: real-window desktop; long-title rows with a diffstat in frames (unit-tested).

### PR 3
- AC12-AC16: `zero-projects-shows-two-action-cards-and-no-composer`, `add-a-project-card-opens-the-add-project-flow`, `set-up-providers-card-opens-providers-settings`, `first-project-brings-the-composer-back`, `cards-are-buttons-with-text-labels`, `no-import-card-yet`, plus: a workspace list that has not arrived shows the composer (no flash), a disconnected daemon shows no cards, the readiness sentence matches the Get started card, `ensure` once, no dead button without a handler.
- Design notes: the cards replace the Get started card and the composer while no project exists (they are the same two steps, so both on screen would repeat each other); the actions are the Get started card's own (`onAddProject`, the Settings -> Providers navigation, the readiness sentence), shared through two exported helpers; App tells the landing when the first workspace list has arrived (`workspacesLoaded`) so a returning user never sees the cards flash.
- Frames and mutations: see the PR body.
