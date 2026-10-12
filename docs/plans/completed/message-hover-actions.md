# Hover actions on chat messages

Size: M (4) — scope 1 (web UI only), PRs 1, risk 0–1 (composer prefill, re-run), decisions 0 (reuses `run.start` / `whenBusy:"queue"`; no new RPC), verification 2 (light/dark screenshots).

Human: "mỗi message item có các action khi hover, tham khảo zcode và paseo".

## References (read-only)

- **ZCode** `refs/zcode/packages/ui/src/v4/ConversationRowView.tsx`: a small `MessageActions` bar *under* the message, `opacity-0 transition-opacity group-hover/<row>:opacity-100 focus-within:opacity-100`, forced visible on touch (`[@media(hover:none)]:opacity-100`); user row: Copy, Edit; assistant row: Copy, feedback, Fork, Retry. Icon-only buttons with tooltips.
- **Paseo** `refs/paseo/packages/app/src/components/message.tsx`: user message trailing row on hover (timestamp, rewind/restore-to-composer, copy), assistant turn footer (copy, fork menu, duration ↔ end time).

## What smind can back (checked)

| Action | Backed by | Verdict |
|---|---|---|
| Copy prompt | clipboard | yes |
| Edit & resend (prefill composer) | composer draft (`use-composer-draft`), then the normal send → `run.start` | yes — honest label "Edit & resend" (it starts a new run; it does not rewrite history) |
| Re-run this prompt | `run.start` (`submitPrompt`) with the run's own provider/model/permission/thinking, `whenBusy:"queue"` | yes (also the natural retry for an errored/stopped run) |
| Copy answer (Markdown) / Copy full turn (plain text) | clipboard; assistant text items / existing `timelineToText` | yes |
| Fork into a new chat/task with this context | smind has no session fork (ACP sessions are per chat; `task.create` with a parent carries no conversation) | **not backed → follow-up** |
| Feedback (like/dislike), rewind files to this turn | no feedback store, no per-turn file snapshots | **not backed → follow-up** |
| Copy for ACP `user` rows (`timeline-user`) and per-tool-call actions | possible but low value | follow-up |

## Decisions

- **D1 One action bar per message, below it, hover/focus revealed.** `MessageActions` (new, `timeline/message-actions.tsx`) + `MessageAction` (icon button with tooltip, `aria-label`, `size-6`-ish, `text-ui-sm` muted → foreground on hover). Revealed by `group-hover` / `focus-within`, so it is keyboard reachable (the buttons are tabbable even while visually hidden; focus reveals the bar). On `(hover: none)` devices it is always visible, compact (ZCode's rule).
- **D2 User prompt bubble**: Copy, Edit & resend. Bar is right-aligned under the bubble.
- **D3 Assistant turn footer**: the footer stays one quiet line (status · provider · model · elapsed · stop reason · usage pill — the ADR-0028 hover card is unchanged). Its right side becomes the action bar: a Copy menu (`Copy answer (Markdown)` — the turn's assistant text joined, `Copy full turn (plain text)` — the old Copy-turn behaviour) and Re-run (hidden while the run is `running`). The old text "Copy" button is replaced (testid `run-copy-button` kept on the menu trigger; aria-label stays "Copy turn" for the full-turn item so existing tests keep their meaning).
- **D4 Re-run** calls the pane's `submitPrompt(run.provider, run.prompt, permission, run.thinkingLevel, "queue", run.model || undefined)`, so it starts immediately when idle and queues behind a live run (ADR-0021; a queued result refreshes the composer's queue list). Provider, model and thinking come from the run; **permission settings come from what the composer shows now when it is on the same provider** (otherwise the run's recorded ones), so a re-run never silently replays a looser mode the old run was live-switched to. One re-submit per run at a time (double click = one run). A failure surfaces as an inline alert at the end of the log column, where the log follows the tail.
- **D5 Edit & resend** prefills the composer: draft empty → replaced; non-empty → prompt appended after a blank line (never destroys unsent text); then the textarea is focused with the caret at the end. Implemented as a `prefill?: { text: string; nonce: number }` composer prop (effect keyed on `nonce`, so re-clicking the same prompt re-applies).
- **D6** Footer dimming from #318 stays, but action buttons reveal at full strength on hover of the answer side / keyboard focus (`:focus-visible`, so a mouse click does not pin the bar) / while their menu is open (`aria-expanded`). The answer side (items, error, footer) is its own hover group, so hovering the prompt reveals only the prompt bar. Copy never confirms when nothing was copied (no clipboard on a non-secure origin).

## Acceptance criteria

- **M-AC1** Every run's user prompt shows a bar with Copy and Edit & resend, hidden until hover/focus-within, always visible under `(hover: none)`.
- **M-AC2** Every finished (done/error/stopped) run's footer shows Copy (menu: answer Markdown / full turn) and Re-run; a running run shows Copy but no Re-run.
- **M-AC3** Copy writes the exact text (prompt; answer Markdown = assistant text items joined by a blank line; full turn = existing plain text) and shows a transient check; clipboard failure is silent (existing behaviour).
- **M-AC4** Re-run submits the same prompt/provider/model/thinking level with `whenBusy:"queue"` and the composer's current permission settings (same provider; else the run's); a double click submits once; errors show inline after the list.
- **M-AC5** Edit & resend prefills the composer per D5 and focuses it.
- **M-AC6** All actions are buttons reachable by Tab with accessible names and tooltips; Escape/Tab behave natively; no new border classes in message rows (the B-AC1 sweep still passes).
- **M-AC7** Screenshots, light + dark, 1440×900 and 390×844: hover on a user prompt, hover on a finished run footer with the Copy menu open, a touch-width view (always visible), error run with Re-run, running run (no Re-run).

## Named test scenarios

`timeline/message-actions.test.tsx`
- `the bar is hidden until hover or focus-within and always visible on touch`
- `Copy writes the text, shows the check, and stays silent when the clipboard is unavailable`
- `actions are focusable buttons with accessible names`

`timeline/run-timeline.test.tsx`
- `the user prompt bar offers Copy and Edit & resend; Edit calls onEditPrompt with the run`
- `finished runs offer Re-run; a running run does not`
- `Copy menu: answer as Markdown copies only the assistant text; full turn copies the plain-text turn`
- `assistantMarkdown joins assistant text items with a blank line and ignores tools and thinking`
- existing suites incl. the border sweep stay green.

`components/task-detail.test.tsx`
- `Re-run submits the same prompt, provider, model and permission with whenBusy queue`
- `Re-run failure shows an inline alert`
- `Edit & resend prefills the composer draft: empty is replaced, non-empty is appended after a blank line, and the textarea is focused`

`composer/composer.test.tsx`
- `a prefill with a new nonce sets the draft once and re-applies on the same text with a new nonce`

## Follow-ups (not backed by smind today)

Fork into a new chat/task with conversation context; like/dislike feedback; rewind files to a turn; Copy/actions on ACP `user` rows and individual tool calls; timestamps swapping in the footer on hover (Paseo).

## Progress

- [x] Spec — `420766cb`
- [x] Step 1 `message-actions.tsx` + tests — `bfa79506` (peer-sonnet)
- [x] Step 2 run-timeline wiring (user bar, footer bar, `assistantMarkdown`) + tests — `5b94eae5` (peer-sonnet)
- [x] Step 3 pane wiring (re-run, edit & resend, composer prefill) + tests — `b22c7735` (peer-sonnet)
- [x] Step 4 Lead: per-side hover groups; adversarial review (sonnet, read-only) → fixes (menu-open visibility via `aria-expanded`, `:focus-visible` reveal, no false "Copied", re-run guards + composer permissions + queue refresh + alert after the list, lazy answer markdown, label-in-name); screenshots; `task test`, `task lint`; PR

## Validation

`task test` (Go + web: 175 files / 2176 tests), `task lint`, `tsc -b` green.

Sandbox daemon (isolated `SMIND_HOME`, own port, fake-LLM harness for the live run). Before (develop `37abdf69`): `/private/tmp/ctr-shots/before3/`; after: `/private/tmp/ctr-shots/final-actions/` — `{light,dark}-{desktop,mobile}-{20-actions-idle,21-hover-user-prompt,22-hover-footer,23-copy-menu,25-touch-top,26-live-run}.png` plus `dark-desktop-24b-keyboard-focus.png`; probes in `report3.json`.

- **M-AC1** idle bar opacity `0`, hovering the prompt `1` (desktop, both themes); under touch emulation `(hover: none)` matches and the bar is `1` while idle (mobile, both themes).
- **M-AC2** 4 Re-run buttons on the 4 finished runs of the mixed chat; on the live running run: 0 Re-run, 1 Copy.
- **M-AC3** unit tests: answer Markdown = assistant text only, full turn = existing plain text, check after copy, no confirmation without a clipboard / on rejection.
- **M-AC4/M-AC5** `task-detail.test.tsx`: re-run payload (provider, model, thinking, `whenBusy:"queue"`), composer permissions on the same provider, double click = one `run.start`, failure alert; Edit & resend prefill (empty replaced, non-empty appended after a blank line, textarea focused).
- **M-AC6** keyboard probe (real `Tab`): focus lands on Copy then Edit & resend and the bar reaches opacity `1`; a mouse click on Copy followed by moving away hides the bar again (`0`); with the Copy menu open the bar stays visible (`23-copy-menu`); border sweep green.
- **M-AC7** screenshots above, all viewed.

Deferred / follow-ups (not backed by smind today): Fork into a new chat/task with conversation context; like/dislike feedback; rewind files to a turn; actions on ACP `user` rows and individual tool calls; footer duration↔timestamp swap. Minor: with a disabled composer (disconnected) Edit & resend appends the text but cannot focus the textarea; the per-bar `TooltipProvider` duplicates the app-level one (cheap, keeps tests self-contained).
