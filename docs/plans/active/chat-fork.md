# Chat fork, edit-and-retry and per-turn file undo (ADR-0057)

Status: active (2026-11-09). ADR-0057 decisions D1-D6 accepted as recommended (Supervisor, under the Human's delegation). Size: L, four sequential PRs (not stacked). This file is the spec of PR 1; later PRs extend it.

## PR 1: data model, fork on Claude, labelled context-seeded fallback

Scope: a chat can be forked at a finished turn. The Claude harness forks natively; every other provider gets the labelled fallback; the Codex native fork is **waiting on a Codex probe** (the app-server calls need a live thread, and there are no Codex credits), so Codex chats get the fallback only. No file undo, no refs, no snapshots in this PR (PR 3). Edit-and-retry is PR 2.

### Acceptance criteria

- AC1: a side table `chat_forks (chat_id PK, parent_chat_id, forked_at_run_id, fork_kind, seed, seed_used_at, created_at)` records a fork; the `chats` table and every query over it are unchanged, so every existing reader sees the same bytes (a deviation from the ADR's column names, chosen to keep the readers of `chats` untouched).
- AC2: `chat.fork {chatId, atRunId, mode}` (human only: not run-scoped, not an MCP tool) creates a child chat of the same task bound to the parent's provider. `mode: native` (default) forks the Claude session at the anchor turn; `mode: seeded` starts a fresh session whose first prompt carries a bounded summary of the earlier turns.
- AC3: native fork launches the child's first run with `--resume <session of the anchor run> --fork-session --resume-session-at <entry uuid>` and stores the new session id like any session. The handle's pending-fork marker is cleared at the first `init` and at the result, so a second turn resumes the child's own session and never forks again.
- AC4: the entry uuid and the session id come from the anchor run's own `native_ref` and the transcript, through the ADR-0053 reader: a transcript that is gone, outside the root, drifted, or whose turn cannot be singled out (`slice_not_found`) refuses the native fork with the reader's reason; the seeded mode stays available.
- AC5: a native fork is refused for a harness that is not Claude Code (reason `fork_unsupported`; Codex says "waiting on the Codex probe"), for a chat with a running run (`native_session_in_use`), and for a run that is not a finished run of that chat. The parent's session file and chat row are never written.
- AC6: the seeded fallback is built from stored run rows only (prompt and `final_text` of the earlier turns, newest turns kept, capped), never from a provider session, and is prepended to the child's first prompt once (`seed_used_at`), not stored in `runs.prompt`. The child is marked lossy in `chat.list`.
- AC7: the first turn of a forked Claude chat reports its own usage, not the inherited conversation's: the baseline is the parent's cumulative totals as of the anchor run (a small log of the totals written when a Claude turn ends).
- AC8: `chat.list` and `chat.get` answer an additive `fork {parentChatId, forkedAtRunId, kind, lossy}` for a forked chat and nothing new for any other (byte-identical payload for unforked chats: golden test).
- AC9: web: a turn menu "Fork from here" (native, or the labelled fallback when native is refused), the fork banner and the parent shown on the tab. CLI: `smind chat fork <chatId> --at <runId> [--seeded]`. Mobile in PR 1b. Light and dark frames for each web state.
- AC10: deleting a task deletes its fork rows and totals log; archiving a chat leaves its children alone.

### Named tests

`TestChatForks_SchemaAndAccessors`, `TestChatForks_ChatsQueriesUnchanged` (golden over the chat payloads), `TestChatForks_DeleteTaskRemovesRows`, `TestClaudeTotalsLog_BaselineAtAnchor`, `TestAnchor_ClaudeLastAssistantUUIDOfTheMatchedTurn`, `TestAnchor_SliceNotFoundOnDuplicatePrompt`, `TestAnchor_MissingUUIDIsFormatDrift`, `TestAnchor_SymlinkOutsideRootRefused`, `TestRunner_ForkHandleUsesResumeForkAndResumeSessionAt`, `TestRunner_ForkMarkerClearedAfterFirstTurn`, `TestRunner_ForkFallsBackToFreshSessionWhenResumeFails`, `TestChatFork_RPCHumanOnlyAndNotRunScoped`, `TestChatFork_RefusedWhileParentRuns`, `TestChatFork_RefusesNonClaudeNative`, `TestChatFork_SeededPrefixOnceAndNotStored`, `TestChatFork_ParentUntouched`, `TestChatList_ForkFieldAdditive`.
Mutations: fork flag dropped; `--resume-session-at` dropped; marker never cleared; seed prepended on every turn; the handler run-scoped; the parent's session handle written.

### Decisions in this PR

- Fork metadata lives in a side table, not in new `chats` columns (AC1).
- The anchor session is the anchor run's `native_ref.sessionId`, not the chat's current handle (a failed resume starts a new session, so older runs can belong to an older one).
- The Codex native fork and the Codex `thread/fork` probe are deferred; Codex chats get the labelled fallback until the probe is run.
- Condition from the Supervisor for the PR that creates `refs/smind/turns/` (PR 3, not this one): the refs are never pushed (no push path may carry them), are pruned with run retention and on task delete, with tests for each.

## Progress

## Validation
