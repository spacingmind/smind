# Readable run footers

**Ask (2026-10-13, pre-v0.11 polish):** "cần UX ok hơn cho người đọc". Each
finished run ends with a raw meta line that reads like debug output:
`● done · Claude Code · 9.2s · [stop reason] · 🔗 16.4k in · 1.2k out · $0.032`.

**Size:** M (4) — scope 1, PRs 0, risk 0, decisions 1, verification 2.
**Branch:** `fix/run-footer-readable` → one PR `fix(web): readable run footers`.

## What the refs do (read 2026-10-13)

- **Paseo** (`agent-stream/turn-footer.tsx`, `components/message.tsx`
  `AssistantTurnFooter`, `utils/time.ts`): a completed turn's footer is
  `[copy] [fork] Worked for 14s`; hovering the label swaps it for the absolute
  completion time (`formatMessageTimestamp`), with an a11y label
  `"14s, ended 10:11 PM"`. `formatDuration` = `14s` / `1m 42s` / `1h 5m`.
  Running = a loader + live elapsed (1 s tick, isolated in `LiveElapsed`).
  No model, tokens, cost or stop reason in the footer at all. Relative time
  (`useTimeAgo`, "5m ago") is used in lists, not under a turn.
- **ZCode** (`v4/ConversationTurnGroup.tsx` `AssistantHistoryStatus`,
  `conversationWorkDuration.ts`): the turn header is a sentence — `Working for
  1m 2s` / `Worked for 1m 2s` / `Stopped` (interrupted). Message actions are
  hover-revealed. No usage in the turn.

smind keeps what neither ref has but smind users rely on (model, cost, the usage
card of ADR-0028 PR-E, stop-reason notes) — but quiet, and in sentences.

## Design

One line under the answer, muted, 70 % opacity until the answer is hovered or
focused (as today). Left to right:

| State | Line |
|---|---|
| running | `⟳ Working… 12s` (spinner, live 1 s tick) |
| done, `end_turn` / no reason | `Sonnet 5.5 · 9s · $0.03 · 2h ago` |
| `cancelled` | `Stopped by you · Sonnet 5.5 · 4s` (neutral) |
| `max_tokens` | `Stopped at the output limit · Sonnet 5.5 · 9s`  **[Continue]** (warning) |
| `max_turn_requests` | `Hit the tool-turn limit · Sonnet 5.5 · 9s`  **[Continue]** (warning) |
| `refusal` | `The agent declined to continue · Sonnet 5.5 · 9s` (warning) |
| status `error` | `Failed after 9s · Sonnet 5.5`  **[Retry]** (destructive); the red error box above keeps the full message and "Retry with higher effort" |
| status `stopped`, no reason | `Stopped · Sonnet 5.5 · 4s` (neutral) |
| any other stop reason | `Ended: <reason> · …` (neutral, verbatim reason) |

- **No "done" word, no provider id, no status dot on a normal finish.** The
  model is the human label from `provider.list` models (`Sonnet 5.5`, `GLM 5.3`);
  requested model → else the model the run reported (`usage.model`) → else the
  provider's default-model label → else the provider label. A raw id with no
  catalog entry is humanised (`claude-opus-5-5` → `Opus 5.5`), else shown raw.
- **Duration** `formatDuration` as Paseo (`9s`, `1m 42s`, `1h 5m`); the title
  holds the precise value (`9.2 s`). Sub-second runs show `<1s`.
- **Cost** is the only thing the usage pill shows inline (`$0.03`, two
  decimals ≥ $0.01 — the card has the exact `$0.032`); without a cost the pill
  shows compact total tokens (`17.6k tokens`); no usage → no pill. The usage
  hover/click card (ADR-0028 PR-E) is unchanged and carries the in/out/cache
  detail. The pill's `aria-label` keeps the full detail.
- **Relative finish time** (`2h ago`) is the last segment, only when ≥ 1 min
  old, a `<time dateTime>` whose `title` is the absolute local timestamp
  (Paseo `formatMessageTimestamp` style); it re-ticks every 30 s.
- **Actions.** *Continue* = the existing queue/`run.start` path with the prompt
  `Continue`, the run's provider/model/thinking and the composer's current
  permission settings (same as Re-run); shown only on the **latest** run.
  *Retry* = existing Re-run of the prompt (error only, latest run only).
  Copy / Re-run stay in the hover action bar. Nothing invents a new RPC.
- **Accessibility.** The line is `role="group"` with an `aria-label` carrying
  the full detail ("Finished in 9.2 seconds with Sonnet 5.5, cost $0.032,
  2 hours ago"); the running label is `role="status"`; the spinner is
  `aria-hidden`; tones are never colour-only (the sentence says it).
- **Queued** prompts are not runs (they live in the composer's queue list,
  unchanged). *Interrupted* is the `Stopped by you` row above.

### Decision (no ADR needed — presentation only, no wire/data change)
Error detail stays in the existing red box rather than being folded into the
footer sentence: errors can be long, and the box already hosts "Retry with
higher effort".

## Acceptance criteria

1. A normal finish shows no "done" text and no provider id; it shows the model's
   human label, a human duration and a compact cost (`Sonnet 5.5 · 9s · $0.03`).
2. In/out tokens no longer appear inline; they are in the usage card, whose
   trigger is still reachable by hover, focus and click (ADR-0028 PR-E).
3. Every abnormal ending in the table reads as a sentence with the stated tone;
   Continue / Retry appear only where stated, only on the latest run, and call
   the existing submit path.
4. A running run shows a spinner + `Working… <live elapsed>`.
5. A finished run ≥ 1 min old shows a relative time whose title is the
   absolute time.
6. Model label resolution follows the order above, including the humanised
   fallback.
7. `aria-label`s carry the full detail; the usage pill keeps its detailed
   label; the footer is readable at 390 px (wraps, no overflow) in light and
   dark.
8. `task test`, `task lint`, web typecheck/tests pass.

## Test scenarios (named)

`web/packages/ui/src/components/timeline/run-footer-model.test.ts` (pure):
- `formatDuration renders 9s, 1m 42s, 1h 5m and <1s`
- `resolveRunModelLabel prefers the requested model, then usage.model, then the provider default, then the provider label`
- `resolveRunModelLabel humanises an uncatalogued claude id and leaves unknown ids raw`
- `describeRunFooter: a normal finish has no status word and no provider id`
- `describeRunFooter: cancelled reads "Stopped by you" with a neutral tone`
- `describeRunFooter: max_tokens and max_turn_requests are warnings with a continue action`
- `describeRunFooter: refusal is a warning with no action`
- `describeRunFooter: an errored run offers retry`
- `describeRunFooter: an unknown stop reason is shown verbatim as "Ended: <reason>"`
- `describeRunFooter: the aria label carries duration, model, cost and age`
- `formatRelativeTime and formatAbsoluteTime`: `just now`/`5m ago`/`2h ago`/`3d ago`, the absolute title

`run-timeline.test.tsx` (component; existing footer tests migrated):
- `a finished run shows model, duration and cost and no "done"`
- `a running run shows Working… with a live elapsed time`
- `Continue appears on the latest max_tokens run and calls onContinue`
- `Continue and Retry are hidden on an older run`
- `Retry on a failed latest run calls onRerun`
- `the relative time carries the absolute time as its title`
- `the usage card is still reachable from the footer` (existing hover/focus tests stay green)

`run-usage-pill.test.tsx`: `the pill shows cost only, else total tokens`; `the aria-label keeps in/out/cost`.
`task-detail.test.tsx`: `Continue sends "Continue" with the run's provider and model through the queue path`; status assertions migrated off the "done" text.

## Verification

Light + dark screenshots at 1440×900 and 390×844, before and after: normal
finish, hover, stopped by user, output limit, turn limit, refusal, error,
running. Saved (gitignored) under `docs/research/local/run-footer-shots/{before,after}/`,
rendered from a throwaway Vite fixture page (one `RunTimeline` per state, not
committed) at 1440×900 and 390×844, light and dark; plus `*-normal-hover.png`
and `*-usage-card.png` (desktop).

## Progress

- [x] spec
- [x] pure model + tests (`run-footer-model.ts`)
- [x] footer component + usage pill (`run-footer.tsx`, `run-usage-pill.tsx`)
- [x] task-detail wiring (modelLabels, isLatest, onContinue)
- [x] screenshots + independent review (Sonnet reviewer; fixes applied)

## Validation

| AC | Evidence |
|---|---|
| 1 normal finish | `run-timeline.test.tsx` "a finished run shows model, duration and cost and no "done""; `*-normal.png` / `*-normal-glm.png` (after) |
| 2 tokens in the card | `run-usage-pill.test.tsx` (pill text, aria-label, card fields); `run-timeline.test.tsx` usage-card reachability tests; `desktop-*-usage-card.png` |
| 3 abnormal endings | `run-footer-model.test.ts` `describeRunFooter: …`; component tests for Continue/Retry on the latest run only; `task-detail.test.tsx` "Continue sends "Continue"…"; `*-stopped-by-user|output-limit|turn-limit|refusal|error.png` |
| 4 running | `run-timeline.test.tsx` "a running run shows Working…" (fake timers); `*-running.png` |
| 5 relative time | `run-timeline.test.tsx` "the relative time carries the absolute time as its title"; `formatRelativeTime` tests |
| 6 model label order | `resolveRunModelLabel …` tests incl. dated / `[1m]` ids (review finding 1) |
| 7 a11y + 390 px | `aria-label` tests; running label is `role="status"` for the static word and `role="timer"` for the ticking time (review finding 2); `mobile-*-all.png` both themes |
| 8 suites | web typecheck clean; web suite 2272/2272 (3 clean runs; under machine load, load avg 10-13, unrelated tests flake — the untouched baseline `913448e6` fails 1–42 tests under the same load); `go test ./...` ok; desktop node tests ok; `task lint` clean |

Independent review (Sonnet, read-only) — accepted and fixed: dated/`[1m]`
model ids, a ticking live region, lost requested-model title (ADR-0025), 30 s
timer per finished run, stale tick `now`, flex+truncate on the model label.
Rejected: (3) repeated Continue clicks while a queue is busy — the footer
action only shows on the latest run, which is the busy run itself whenever a
queue exists, and the in-flight guard covers the click; (5) the failed run's
duration lives in the sentence on purpose; (10) `cancelled` is ACP's user
cancel stop reason.
