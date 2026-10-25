# AI-drafted commit message, opt-in (G17, ADR-0043)

Status: active (2026-10-25).

Size: M (5) — scope 2 (daemon + CLI + web), PRs 1, risk 1 (model call on a human click; no commit), decisions 1 (ADR-0043 amends ADR-0006 decision 2), verification 1 (UI frames + one live Haiku call).

The commit bar gets a "Draft message" (Sparkles, tooltip "Draft with AI") button. It asks the daemon to draft a message from the STAGED diff of the task worktree and fills the textarea. The daemon never commits, stages, or touches the index/worktree. Binding text: `docs/decisions/0043-opt-in-ai-commit-message-draft.md` (Proposed; Human must accept).

## Decisions (Supervisor defaults 2026-10-25, Human may veto)

1. Mechanism: the shared learning `executor.Executor` (`internal/learning/executor`): hidden per-workspace system task, `RunPromptIsolated`, purpose `commitmsg`, claude-native, model `config.DefaultHaikuModel` (full id, not the alias). No chat, no queue item, no notification (system runs publish none), not listed in task runs. Cost lands in `run_usage` of that system run (`runs.purpose='commitmsg'`), outside the learning budget queries. It shares the executor's per-workspace slot with learning: a concurrent learning run yields `busy`.
2. Input: `git diff --cached` only (run with `GIT_OPTIONAL_LOCKS=0`, read-only; no index write), byte budget 24 KiB, per-file header lines kept, marker `[... N bytes omitted ...]`; `--stat` summary; last 5 commit subjects. Binary files by name. Files named `.env*`, `*.pem`, `*.key`, `id_rsa*`, `credentials*` appear by name only, marked omitted (also listed in `omitted[]`). Untracked/unstaged never sent. Empty staged diff: `nothing_staged`, no model call.
3. Output: Conventional Commits, subject <= 72, body wrapped at 72, no fences/quotes/trailers; post-processed; empty => `empty_draft`.
4. Style: workspace column `commit_message_style` (<= 1000 chars) sent as untrusted guidance; set by `workspace.setCommitMessageStyle` (mirrors `workspace.setDefaultBase`) and a field in the existing workspace settings dialog. No new settings page.
5. Guards: human callers only (`refuseRunScoped`); single-flight per task (second concurrent call joins the in-flight result); per-task floor 3 s and hourly cap 30 per workspace (`rate_limited`); 60 s timeout; cancel when the connection request is cancelled; nothing cached on disk.
6. Errors are typed codes: `nothing_staged`, `no_draft_provider`, `rate_limited`, `busy`, `empty_draft`, `draft_failed`, `refused` (agent caller).
7. RPC `task.draftCommitMessage {taskId}` -> `{message, model, truncated, omitted[]}`; CLI `smind task commit-message <taskId>` prints the message only. No MCP tool.
8. Web: button in the commit bar (diff-viewer-pane). Disabled with tooltip when nothing staged; spinner while drafting; fills the textarea only when it is empty, otherwise shows an inline "Replace the current message?" with Replace / Keep buttons (no modal); caret placed at the end, textarea focused; errors inline under the bar; `aria-live="polite"` status for completion.
9. Out of scope: mobile, auto-draft, PR title/body, changing `task.commit`, any agent tool.

## Acceptance criteria

AC1 staged diff only: prompt contains the staged diff and the style text, excludes unstaged and untracked content.
AC2 truncation at the byte budget keeps headers, marks omission, sets `truncated`.
AC3 secret-named files are sent by name only and listed in `omitted`.
AC4 nothing staged => `nothing_staged`, zero model calls.
AC5 output normalised (fences, quotes, overlong subject, wrapped body); empty => `empty_draft`.
AC6 run-scoped caller refused, zero model calls.
AC7 single-flight: two concurrent calls => one model call.
AC8 floor and hourly cap return `rate_limited`; the cap is per workspace.
AC9 timeout/cancel releases the in-flight slot.
AC10 the call creates no chat, no queue item, no notification, no visible run in task history, and leaves index/worktree/HEAD unchanged (git state before == after).
AC11 style text > 1000 chars is rejected; <= 1000 saved and returned in the workspace payload.
AC12 no claude-native provider/account usable => `no_draft_provider`, no crash.
AC13 CLI prints the message and does not commit.
AC14 web: disabled when nothing staged; fills empty textarea; does not overwrite text without Replace; typed errors inline; spinner state; keyboard reachable; light + dark frames.
AC15 one live Haiku call on an isolated daemon via the CLI; model, cost, message reported.

## Named test scenarios

Daemon (`internal/commitdraft`, `internal/wsapi`, `internal/store`, `cmd/smind`), fake claude CLI (`internal/taskrunner/fakeclaude`) and a throwaway git repo:
- `TestDraft_PromptHasStagedDiffAndStyle_ExcludesUnstagedAndUntracked` (AC1)
- `TestDraft_TruncatesAtBudgetKeepsHeaders` (AC2)
- `TestDraft_SecretNamedFilesOmittedByName` (AC3)
- `TestDraft_NothingStaged_ZeroModelCalls` (AC4)
- `TestNormalize_FencesQuotesLongSubjectBody`, `TestDraft_EmptyOutputTypedError` (AC5)
- `TestWSAPI_DraftCommitMessage_RunScopedRefused_ZeroModelCalls` (AC6)
- `TestDraft_SingleFlight_TwoCallsOneModelCall` (AC7)
- `TestDraft_RateFloorAndHourlyCap` (AC8)
- `TestDraft_CancelAndTimeoutReleaseSlot` (AC9)
- `TestDraft_NoChatNoQueueNoNotification_GitStateUnchanged` (AC10)
- `TestWorkspaceCommitMessageStyle_ValidatesLength` (AC11)
- `TestDraft_NoProvider_TypedError` (AC12)
- `TestTaskCommitMessageCLI_PrintsAndDoesNotCommit` (AC13)
Web (`diff-viewer-pane.test.tsx`): `draft button disabled when nothing staged`, `draft fills an empty textarea`, `draft does not overwrite existing text without Replace`, `draft shows typed errors inline`, `draft shows spinner while pending` (AC14).

Mutations (reported in the PR): (1) `git diff` instead of `--cached` fails AC1; (2) drop the nothing-staged guard fails AC4; (3) drop the run-scoped refusal fails AC6.

## Progress

- [x] ADR-0043 + plan committed
- [x] Daemon: `internal/commitdraft`, RPC `task.draftCommitMessage`, wiring on the shared learning executor, CLI `smind task commit-message` (Peer candidate 13d480e9, ACCEPTED: diff reader read-only with GIT_OPTIONAL_LOCKS=0, per-task single-flight, nothing_staged before any rate budget, tests assert on the prompt sent)
- [x] Web: button, hook, icon, tests (Peer candidate 6bfba534, ACCEPTED; Lead fix 18f4f244: reset draft status on task change)
- [x] Style: store column, `workspace.setCommitMessageStyle`, settings field, drafter wiring (Peer candidate 9979490c, ACCEPTED)
- [x] Review, mutations, frames, live call, PR

## Validation

- AC1-AC13: named tests in `internal/commitdraft`, `internal/wsapi`, `internal/workspace`, `cmd/smind` pass (see PR body for the exact commands).
- AC14: six vitest cases in `diff-viewer-pane.test.tsx`; light + dark + transition frames in `/tmp/commit-draft-shots/` (idle, tooltip, drafting spinner, filled, replace prompt, rate-limited error, disabled, disabled tooltip), taken from the real app against an isolated daemon with a FAKE claude CLI.
- AC15: one live Haiku call (`claude-haiku-5-5`) via the CLI on an isolated daemon: USD 0.00087, 1385 in / 262 out tokens, message "feat: add Greet function" + short body. Cost lands in `run_usage` of a hidden `commitmsg` run on the workspace's system task.
- Mutations: (1) `git diff` instead of `--cached` fails `TestDraft_PromptHasStagedDiffAndStyle_ExcludesUnstagedAndUntracked` (and 9 more); (2) dropping the nothing-staged guard fails `TestDraft_NothingStaged_ZeroModelCalls`; (3) dropping `refuseRunScoped` fails `TestWSAPI_DraftCommitMessage_RunScopedRefused_ZeroModelCalls`.
- Known unrelated: `TestTaskWait_QueueItemCancelled` races under `-race` on origin/develop too.
