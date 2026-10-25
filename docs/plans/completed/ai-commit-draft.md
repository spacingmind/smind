# Draft with agent in the commit bar (G17, ADR-0043 rejected)

Status: completed (2026-10-25).

Size: S (2) — web only.

## History

First shape: a daemon-side drafter (`task.draftCommitMessage`, isolated Haiku run, style setting, caps, CLI). The Human rejected it on 2026-10-25 (ADR-0043 Rejected): ADR-0006 decision 2 stays, the daemon never makes a model call for commit messages. That code was removed entirely (RPC, `internal/commitdraft`, `commitmsg` run purpose, `commitMessageStyle` column and RPC, CLI `smind task commit-message`, fakeclaude scenario, their tests); no schema change from this work remains.

## Decisions

1. The commit bar gets a **Draft with agent** button (Sparkles via `lib/icons.ts`, tooltip "Ask the agent to draft a message").
2. Click sends a NORMAL prompt to the task's own chat via the same web send path as "Submit review" (`sendToRecipient` in `diff-viewer-pane.tsx`: the focused chat, its own provider/model/mode, `whenBusy: "queue"`, nothing pinned). Exact prompt: "Draft a Conventional Commits message for the currently staged changes (`git diff --cached`). Reply with the message only in a fenced block; do not commit."
3. The reply is read in the chat. No textarea auto-fill, no polling, no new RPC, no daemon change.
4. Feedback is the existing review pattern: a toast ("Draft request sent to / queued in <chat>") and the chat is revealed (`requestRevealChat`), as "Submit review" does.
5. Disabled with a reason tooltip: "Nothing staged" or "No agent chat to ask". The button is always present, so the bar never shifts.
6. Out of scope: mobile, auto-draft, PR title/body, any daemon change.

## Acceptance criteria

AC1 button disabled with reason "Nothing staged" when no file is staged.
AC2 button disabled with reason "No agent chat to ask" when the task has no chat.
AC3 click makes exactly one `run.start` with the exact prompt, `whenBusy: "queue"`, and only the chat's own provider/model/permission params (no pinned model).
AC4 the textarea content is untouched; no RPC other than that send.
AC5 the bar layout is unchanged apart from the new button: no element in the bar moves when the button is clicked.
AC6 light + dark frames from the real app (idle, disabled, click feedback, chat turn).

## Named tests (`diff-viewer-pane.test.tsx`)

- `draft with agent is disabled with the Nothing staged reason` (AC1)
- `draft with agent is disabled with the No agent chat to ask reason` (AC2)
- `draft with agent sends the exact prompt once through the chat's own send path and touches nothing else` (AC3, AC4)
- icon registry snapshot in `icons.test.ts` (`draft: Sparkles`)

Mutations: (a) pinning a model on the draft send fails the arguments test; (b) removing the staged guard fails the Nothing staged test.

## Progress

- [x] Daemon drafter built, then removed after the rejection
- [x] Button + tests
- [x] Frames, checks, PR

## Validation

- AC1-AC4: the three vitest cases above pass; mutation (a) fails `draft with agent sends the exact prompt once ...`, mutation (b) fails `draft with agent is disabled with the Nothing staged reason`.
- AC5: bounding boxes of the textarea, Commit and Draft buttons sampled in 14 frames at ~60 ms after the click are identical to the idle boxes in light and dark (`/tmp/commit-draft-shots/v3/bbox-report.json`). The tab switch to the chat happens within the first sampled frame, so frames show the bar only up to that point.
- AC6: `/tmp/commit-draft-shots/v3/`, frames viewed. Run against an isolated daemon with a fake claude CLI; no live model call.
