# Chat timeline rail + borderless messages

Size: M (4) — scope 1 (web UI only), PRs 1, risk 0–1 (scroll perf), decisions 0 (port of Paseo; no architecture choice), verification 2 (light/dark screenshots).

Human (pre-v0.11 polish): "chưa có thanh timeline để scroll nhanh" and "message có các border ko cần thiết, bỏ đi".
Reference: Paseo's agent chat — a thin rail of tick marks at the left edge of the chat (one per user turn, current one longer/brighter, hover → preview, click → jump), and borderless messages (plain assistant text, subtle user bubble, compact muted tool rows).

Port source (read-only): `refs/paseo/packages/app/src/agent-stream/chat-outline/{rail.web.tsx,model.ts,hover-intent.ts}`.

## Decisions

- **D1 One tick per run (turn).** A smind run is exactly one prompt → answer turn (`RunEntry.prompt`), the equivalent of Paseo's "user prompt". Tick preview = first non-empty line of `run.prompt`. No daemon/API change: the whole chat is already loaded client-side, so there is no prompt index RPC like Paseo's.
- **D2 Evenly stacked slots, not scroll-proportional.** Same as Paseo: slots tile the rail with no gaps (flex-shrink), so 200 turns still fit; the viewport's current turn is the highlighted (longer/brighter) tick. This is "positions mapped to the scroll content" in index order.
- **D3 Active turn** = the last run whose top is at or above `scrollTop + 24px` (binary search over the run `<li>`s), the last run when scrolled to the bottom. Computed in a rAF-throttled passive scroll listener and published through an external store (`createActivePromptPublisher`, ported); only the rail subscribes (`useSyncExternalStore`) so scrolling never re-renders `TaskDetailPane` or any message.
- **D4 Visibility.** Rail renders only when ≥ 2 runs AND the scroll content overflows the viewport AND the pane is ≥ 860px wide (reading column is `max-w-3xl` = 768px; rail + gap needs ~44px each side). Hidden below that (mobile 390px, split panes). Measured with `ResizeObserver` (guarded when absent).
- **D5 Interaction.** Hover shows a preview card (first line, 2-line clamp) after Paseo's hover-intent delay and dock-style magnification (ported). Click jumps with `scrollTo({behavior:"smooth"})` (instant under `prefers-reduced-motion`). Pointer-drag across the rail scrubs (instant jumps). Keyboard: roving tabindex (the active tick is the only tab stop), ArrowUp/Down moves focus, Home/End, Enter/Space jumps; focus shows the same preview. `role="tablist"`/`tab`, `aria-selected`, labels "N of M: preview".
- **D6 Borderless run.** The run card (`rounded-xl border`) with header/body/footer dividers goes away. The prompt is a right-aligned subtle bubble (`bg-surface`, no border, top-right small radius — Paseo's `surface3` bubble), assistant output is plain, runs are separated by spacing. The old run header (provider · model chip · status) moves into the footer meta line: `● status · provider · model · elapsed · stop reason · usage pill · Copy`. The footer is one muted line; for finished runs it is dimmed and brightens on run hover/focus-within; running/error/stopped runs keep it fully visible.
- **D7 Tool calls / thinking / tool groups / unknown events** become compact muted single-line rows (chevron + icon + name + summary, status dot; the status word is screen-reader-only for `success`), hover background, no card border. Expanded detail is indented with a subtle `bg-surface` block, no `border-t`. Inner code/IN-OUT/read-preview boxes lose their borders (background only). Semantic borders stay: markdown tables, blockquote rule, destructive/warning permission-detail badges, the pending-permission dock and composer (not messages).
- **D8** The usage pill stays a popover trigger (ADR-0028 hover card unchanged) but drops its `border`.

## Acceptance criteria

Rail
- **R-AC1** One tick per run, in order; ticks are `role=tab`, labelled `N of M: <first line of prompt>`; no rail with 0–1 runs.
- **R-AC2** Rail hidden when content does not overflow, and when pane width < 860px; shown again when either flips (no reload).
- **R-AC3** The active tick follows scrolling (rAF-throttled); scrolled to bottom → last tick; scrolled to top → first tick. Scrolling never re-renders `RunTimeline` rows or ticks other than the two whose active state flips.
- **R-AC4** Hover (after intent delay) or focus shows a preview with the prompt's first line; leaving hides it; magnification band as in Paseo.
- **R-AC5** Click on a tick scrolls the run's `<li>` to the top of the viewport (smooth; instant with reduced motion). Drag across the rail scrubs through runs.
- **R-AC6** Keyboard: Tab enters on the active tick only; ArrowUp/Down/Home/End move focus; Enter/Space jump.
- **R-AC7** Streaming a chunk into the last run does not re-render ticks (props stable; prompts array identity stable when ids/previews unchanged).
- **R-AC8** Does not change the existing auto-follow / jump-to-latest / chat find behaviour.

Borderless
- **B-AC1** No element of a run/message row (`run-entry`, header/footer, `timeline-tool-call`, `timeline-tool-group`, thinking, unknown, permission-trace row, usage pill) carries a `border*` / `divide-*` class (test sweeps rendered DOM of a rich run in both detail levels), except the allow-list in D7 (markdown tables/blockquote).
- **B-AC2** Existing testids and behaviours survive: `run-entry`, `run-prompt`, `run-status`, `run-model-chip`, `run-elapsed`, `run-stop-reason`, `run-usage-pill` + popover, `run-copy-button`, `run-error`, `run-retry-higher-effort`, tool-call toggle/open-path/status/detail, group toggle, "Detailed"/"Overview" toggle, permission dock, queued prompts.
- **B-AC3** Footer of a finished run is dimmed until hover/focus-within; running/error/stopped are not dimmed.
- **B-AC4** Screenshots (light + dark, 1440×900 and 390×844) show: long chat (20+ turns) with rail, rail hover tooltip, mid-scroll highlight, running run, permission prompt, error run, tool calls, Detailed and Overview views — no stray card borders, clean spacing.

## Named test scenarios

`components/timeline/chat-outline.test.ts` (model)
- `promptPreview takes the first non-empty line, trimmed and truncated`
- `buildOutlinePrompts maps runs to {id, preview} in order`
- `stable outline identity: same ids and previews return the previous array`
- `resolveActiveRunIndex picks the last run at or above the anchor`
- `resolveActiveRunIndex picks the last run when scrolled to the bottom`
- `promptTickMagnification falls off to zero at the radius` (ported)
- `active prompt publisher notifies only on change` (ported)
- `hover intent` suite (ported)

`components/timeline/chat-outline-rail.test.tsx`
- `renders one tab per prompt with an "N of M: preview" label`
- `renders nothing for fewer than two prompts`
- `marks the active prompt aria-selected and only it is tabbable`
- `ArrowDown/ArrowUp/Home/End move focus between ticks`
- `Enter on a focused tick calls onJump with its run id`
- `focusing a tick shows the preview with the prompt's first line`
- `pointer drag across the rail jumps to each run it crosses`
- `does not re-render ticks when only the active prompt of a sibling changes` (render counts)

`components/task-detail.test.tsx` / `components/timeline/chat-outline-hook.test.tsx`
- `rail hidden when content fits, shown when it overflows`
- `rail hidden below 860px pane width`
- `scroll publishes the active run without re-rendering the pane`
- `click on a tick scrolls the run into view`
- `streaming a chunk does not change the prompts array identity`

`components/timeline/run-timeline.test.tsx`
- `run entry carries no border or divide classes in detailed and overview levels` (B-AC1)
- `run footer is dimmed when finished and full strength when running or errored` (B-AC3)
- existing RunTimeline/TimelineRow/tool-call/permission suites stay green (B-AC2), updated only where they asserted card structure.

## Progress

- [x] Spec (this file) — `1a91e0ef`
- [x] Step 1 rail model + hover intent port + tests — `bdb369d6` (peer-sonnet; peer-glm hit a 429 usage limit before writing anything)
- [x] Step 2 rail component + hook + task-detail wiring + tests — `b977809a`, `351ade84`, `ee24bd9c`
- [x] Step 3 borderless restyle + tests — `cd638b3e`, `58b8b85d`, `8c432988`, `b3da533c` (peer-sonnet, in parallel with step 2: disjoint write scopes)
- [x] Step 4 sandbox seed + screenshots before/after, fixes — Lead: pointer→slot mapping bug found by the live click probe (`b60f9eed`), row alignment / footer / interrupted dot / path-row layout (`440bfa02`), merge of origin/develop (usage pill card from PR-E made borderless via a `borderless` prop), `text-ui-sm` token
- [x] Step 5 adversarial review (sonnet, read-only; findings 1,2,3,4,6,7,11 fixed in `fix(web): review fixes…`; 5,8,9,10 deferred, see Validation), `task test`, `task lint`, PR

## Validation

Tests: `task test:web` 171 files / 2129 tests green; `task lint` green; `tsc -b` clean. `task test` Go: `internal/wsapi` fails intermittently under load (a different test each run — `TestServer_RunAttach_DetachDoesNotStopRun` once) and passes on rerun 5/5; the known CI flake, untouched by this web-only change. A concurrent `task test` run also hit one 5 s timeout in `App.test.tsx` splits; the file passes alone (73/73) and in the final full run.

Sandbox daemon (isolated `SMIND_HOME`, own port, GLM agent against the fake OpenAI-SSE server for the running + permission states). Screenshots, light + dark × 1440×900 + 390×844, before (`develop`) and after: `/private/tmp/ctr-shots/before/` and `/private/tmp/ctr-shots/final/` (local only; scripts copied next to them).

- **R-AC1/R-AC4/R-AC6/R-AC8**: 28-run chat → 28 ticks `role=tab` labelled `N of M: …`; hover preview (`*-03-long-rail-hover.png`); keyboard probe: focus tick 1, ArrowDown ×2 → "3 of 28: …", Enter → top run `long-02`.
- **R-AC2**: short chat and 390px mobile have no rail (`report.json`: `*-short-rail: false`, `*-mobile-rail: null`); desktop rail present once the content overflows.
- **R-AC3**: scrolled to 45% → active "12 of 28" (`*-02-long-mid.png`); scroll publishes via the external store (hook test asserts the pane does not re-render).
- **R-AC5**: live click probe: ticks 2/20/5 land on `long-02`/`long-20`/`long-05` (smooth, settles ≈1 s on a long distance); mouse drag 3→15 lands on `long-15`; the pointer→slot mapping bug this probe found is fixed and unit-tested (`pointer jumps map over the slots' extent…`).
- **R-AC7**: `streaming a chunk does not change the prompts array identity`, `does not re-render ticks when only the active prompt of a sibling changes`.
- **B-AC1**: `run entry carries no border or divide classes in detailed level` / `… in overview level` (allow-list: markdown table/blockquote, usage popover, Button primitive's `border-transparent`, StatusBadge, outline retry button).
- **B-AC2**: all listed testids preserved; web suite green.
- **B-AC3**: `run footer is dimmed when finished and full strength when running or errored` (`opacity-70`, full on hover/focus-within and on `(hover: none)`).
- **B-AC4**: `*-01-long-bottom`, `*-02-long-mid`, `*-03-long-rail-hover` (desktop), `*-04-long-after-click`, `*-05-long-overview-top` (Overview), `*-06-mixed` (done, error, turn-limit, interrupted), `*-07-mixed-tool-expanded`, `*-08-short`, `*-09-live-permission` (running run + permission dock) — all viewed.

Deferred from review: per-frame O(runs) rect reads (fine at 200 runs; cache/binary-search if chats grow to thousands), the rail not forwarding wheel events (same as Paseo), a streamed chunk can cancel a smooth jump when the view was pinned to the tail (rare; needs a following-setter in `useAutoFollow`), O(runs) preview split per render.
