# Multiple chats per task (ADR-0016)

## Context

The user decided this on 2026-09-27: a task can hold several chats, each its own conversation, sharing the task's git worktree. This mirrors Paseo's workspace → agents model. All 8 sub-decisions in `docs/decisions/0016-multiple-chats-per-task.md` are Accepted; read that ADR first.

Along the way we found a real bug: **no runner resumes an agent session today**, so every follow-up prompt starts with no memory of the earlier ones.

- claude-native: `internal/taskrunner/runner.go:402-490` creates a fresh SDK client every time.
- ACP: `internal/acp/client.go:268` always sends `session/new`.
- Codex: `internal/codex/client.go:188` always sends `thread/start`.

Phase 2 fixes this.

## Phases

Each phase ships as its own PR.

- **P1 — backend data model and wire** (branch `feat/multi-chat-backend`): the `chats` table, migration, RPCs, events and the concurrency guard.
- **P2 — agent session resume** (branch `feat/agent-session-resume`): resume support in each runner behind a session-handle abstraction, then hooked up to the chat row once P1 lands.
- **P3 — web**: chat tabs, New chat, rename/archive, a per-chat toolbar and pill, and task-level attention aggregation.
- **P4 — CLI and mobile**: `--chat` and `task chat` subcommands; mobile stays compatible through the default chat.

## Acceptance Criteria

### P1 — backend

- **P1.1 `chats` table** (`internal/store`, `CREATE TABLE IF NOT EXISTS` plus an additive migration):
  - Columns: `id, task_id (FK, cascade on task delete), title, provider (NULL until first run), agent_session (JSON handle, NULL), created_at, archived_at (NULL)`.
  - `runs.chat_id` is a nullable FK.
  - **Migration:** each existing task gets exactly one default chat (`provider=NULL`, title "Chat"), and all of that task's existing runs get its `chat_id`.
  - The migration is idempotent: running it twice changes nothing.
- **P1.2 RPCs** (wsapi, additive):
  - `chat.create {taskId, title?}`
  - `chat.list {taskId, includeArchived?}`
  - `chat.get {id}`
  - `chat.rename {id, title}`
  - `chat.archive {id}`: refused while the chat has a running run.
  - Lifecycle events `chat.created`, `chat.updated` and `chat.archived`, in the ADR-0009 shape.
- **P1.3 `task.prompt` / `run.start`** take an optional `chatId`. When it is omitted, the task's default chat is used, so old clients keep working unchanged.
  - A chat's `provider` binds on its first run and is immutable afterwards.
  - Prompting a chat with a different provider is rejected with a clear error.
- **P1.4 Run-scoped events** carry `chatId`: `run.status`, `permission.pending`, and any other event that carries a runId. `run.list` / `run.logs` can filter by `chatId`.
- **P1.5 Concurrency:** at most one running run per **chat**. A prompt to a busy chat is rejected with a clear error. Different chats of the same task may run concurrently.
- **P1.6 Compatibility:**
  - Every existing wsapi test passes unmodified. The only exception is an assertion that enumerates topics or fields, which may be extended.
  - The mobile app and the CLI keep working without changes, via the default chat.

### P2 — session resume

- **P2.1 Session handle.** A handle `{provider, sessionId, nativeHandle?, metadata?}` is returned by each runner after a run and passed back on the next one.
  - Until P1 lands, it is kept behind an interface (`SessionStore`) with an in-memory implementation for tests.
  - Once P1 lands, it is persisted on `chats.agent_session`.
- **P2.2 claude-native** resumes through the vendored SDK's `WithResume` / `WithSessionID`. Paseo's pattern is `refs/paseo/packages/server/src/server/agent/providers/claude/agent.ts:3153`.
- **P2.3 ACP**, capability-driven, following Paseo's `providers/acp-agent.ts:1777-1800`:
  1. use `session/load` if `agentCapabilities.loadSession`;
  2. otherwise use `unstable_resumeSession` if `sessionCapabilities.resume`;
  3. otherwise start a new session **and surface it**, as an event or log plus a UI-visible note that context was not resumed.

  Verify against the real GLM ACP binary which capability it advertises, and record the result.
- **P2.4 Codex** uses `thread/resume` with the stored thread id. Check `thread/loaded/list` first, and on the archived-thread error call `thread/unarchive` and retry. This follows Paseo's `providers/codex-app-server-agent.ts:3963-3980`.
- **P2.5 Failure handling:** a stale or unknown session id falls back to a new session with a surfaced note, never a hard failure of the user's prompt.

### P3 — web (after P1)

- Chat tabs are keyed `${taskId}:chat:${chatId}`. "+ → New chat" creates a chat and opens its tab.
- Tab rename maps to `chat.rename`. Closing a tab only detaches it; archiving the chat is a separate explicit action with confirmation.
- Each chat has its own `RunConfigToolbar` state (key `smind:run-config:${taskId}:${chatId}`) and its own header pill. Once the chat's provider is bound, the provider selector is read-only.
- The timeline shows only that chat's runs.
- When another chat of the same task is running, a small banner warns: "Another chat is running in this worktree".
- Task-level attention/unread in the sidebar aggregates across chats. Notifications and permission cards name the chat.
- The UI follows `docs/design.md` (ZCode tokens, `text-ui-*`), and light/dark screenshots are reviewed.

### P4 — CLI and mobile (after P1)

- `smind task send <taskId> <provider> <prompt> [--chat <id>]`.
- `smind task chat ls|new|rename|archive`.
- `task logs/attach` take `--chat`.
- The mobile app keeps working on the default chat. A chat picker there is out of scope for now.

## Test Scenarios

**P1**
- **Migration:**
  - a DB with 2 tasks and 5 runs gets 2 default chats with every run linked;
  - running the migration twice is a no-op;
  - a fresh DB works.
- **RPCs:**
  - the create/list/get/rename/archive round trip;
  - archive is refused while a run is running;
  - unknown ids return not-found.
- **Prompting:**
  - `task.prompt` without `chatId` lands on the default chat;
  - with `chatId`, it lands on that chat;
  - a provider mismatch on a bound chat is rejected.
- **Concurrency:**
  - a second prompt to a busy chat is rejected;
  - two chats of one task run concurrently.
- **Events:** events carry `chatId`, and `run.list` filters by it.
- **Compatibility:** the old-client flow (no `chatId` anywhere) is unchanged end to end.

**P2**
- Per runner, a unit test proves the second run passes the stored session id (resume) and the first run doesn't.
- ACP capability matrix: `loadSession`, `resume` and neither, with the "neither" case surfacing a note.
- Codex: resume, loaded-already, and archived → unarchive → resume.
- A stale session id falls back to a new session with a note.
- **Live check (recorded in Validation):** against real GLM ACP and claude-native, a follow-up prompt that asks "what did I ask you before?" answers correctly.

**P3 / P4**
- Component tests: tabs per chat; New chat; rename; the archive confirm; toolbar state isolated per chat; the concurrency banner; the aggregated attention.
- CLI tests for `--chat` and `task chat`.

## Decisions

- ADR-0016 is the source of truth.
- Migrated default chats resume starting from their next run. This deliberately departs from the draft's "never resume", because the user needs follow-ups to keep context.

## Progress

- [ ] P1 backend
- [x] P2 session resume (branch `feat/agent-session-resume`)
- [ ] P3 web
- [ ] P4 CLI + mobile

## Validation

### P2 session resume

- **P2.1 (SessionHandle/SessionStore).** Added `taskrunner.SessionHandle`
  {Provider, SessionID, NativeHandle, Metadata} and a `SessionStore`
  interface, with `MemorySessionStore` wired in as `Runner`'s default,
  keyed by task ID (the same key the pre-existing in-memory ACP session
  map used). `TestMemorySessionStore` covers the get/set/replace contract.
- **P2.2 (claude-native).** `runClaudeNative` now resumes via the vendored
  SDK's `claudecode.WithResume(sessionID)`, storing the next handle from
  `ResultMessage.SessionID` after each successful turn.
  `TestRunner_RunPrompt_ClaudeNative_ResumesSessionAcrossRuns` proves the
  second run passes `--resume=<id>` and the first doesn't.
- **P2.3 (ACP).** Verified live 2026-09-27 against the real
  `glm-acp-agent@1.3.0` binary: its `initialize` response advertises
  `{loadSession: true, sessionCapabilities: {resume, list, fork, close}}`.
  `newOrResumeACPSession` tries `session/load` first, falls back to
  `session/resume`, then to a fresh `session/new` with a surfaced
  `EventTypeSessionNote` if neither is offered.
  `TestRunner_RunPrompt_GLM_ResumeCapabilityMatrix` covers all four
  combinations (loadSession, resume, both, neither) against an
  extended fake ACP agent.
- **P2.4 (Codex).** Added `codex.Client.ResumeSession`: checks
  `thread/loaded/list` first, then `thread/resume`, retrying once via
  `thread/unarchive` on the archived-thread error. `TestClient_ResumeSession`
  covers not-loaded/already-loaded/archived-then-unarchived/unknown-id.
  Verified live 2026-09-27 against the real `codex app-server` 0.149.1
  binary: `thread/start`, `thread/loaded/list`, `thread/resume`,
  `thread/archive`, and `thread/unarchive` all behave as documented,
  including the exact `"no rollout found for thread id ..."` /
  `"no archived rollout found for thread id ..."` error wording.
- **P2.5 (fallback).** Every runner falls back to a fresh session on a
  resume failure (stale/unknown id) instead of failing the prompt, logging
  and emitting `EventTypeSessionNote` first.
  `TestRunner_RunPrompt_{GLM,ClaudeNative,CodexNative}_StaleSessionFallsBackWithNote`
  cover all three providers.

**Live check, against the real daemon** (temp `SMIND_HOME`, port 4714,
never touching `127.0.0.1:4648` or the real `~/.spacingmind`; reused the
existing logged-in `claude`/`glm-acp-agent` CLIs read-only):

- **GLM:** prompt 1 ("Remember this secret word: purplecatapult42...")
  then prompt 2 ("what exactly did I ask you in my previous message?") on
  the same task correctly answered "Your previous message asked me to
  remember the secret word 'purplecatapult42' and to reply with just
  'OK'." No fallback note logged -- `session/load` succeeded.
- **claude-native:** same two-prompt pattern (secret word
  "tangerinefalcon77"); the follow-up correctly recalled it. No fallback
  note logged -- `WithResume` succeeded.
- **codex-native:** not fully verifiable live -- `thread/start` succeeded,
  but the first turn itself failed with the CLI's own
  `"You've hit your usage limit ... try again at Oct 7th, 2026"` (an
  account-level quota, not a protocol or code issue). The resume mechanics
  (`thread/loaded/list`, `thread/resume`, `thread/archive`/`thread/unarchive`)
  were separately verified against the same real binary outside the
  daemon (see P2.4 above and `TestClient_ResumeSession`), and covered
  end-to-end (including the fallback path) by
  `TestRunner_RunPrompt_CodexNative_ResumesSessionAcrossRuns` and
  `TestRunner_RunPrompt_CodexNative_StaleSessionFallsBackWithNote` against
  the fake app-server. A full live prompt-resume-followup round trip for
  Codex remains to be confirmed once the account's quota resets
  (2026-10-07) or against a different account.

`go test -race ./internal/taskrunner/... ./internal/acp/... ./internal/codex/...`
and `task lint` both pass.
