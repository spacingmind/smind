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
- P1.1's `chats` schema uses a single `agent_session` JSON-handle column, not the ADR draft's separate `agent_session_id`/`agent_session_meta` columns -- the plan's own P1.1 AC (`agent_session (JSON handle, NULL)`) is more specific than the ADR's `§2` sketch and is what P1 implements; P2 owns whatever it decodes into.
- `chat.updated` (not the ADR draft's `chat.renamed`) is the lifecycle topic for any chat mutation that is neither create nor archive -- matching `task.updated`'s own "created/archived excluded" semantics, and covering both a rename and a first-run provider bind (both are the "any transition" pattern, not two separate topics).
- Every task always has >=1 chat: `workspace.Manager.CreateTask` creates a default chat ("Chat", provider NULL) the same way the migration backfills one for pre-existing tasks, so `DefaultChat` always has something to resolve to and no lazy-create branch is needed elsewhere.
- `runs.Registry.Start`'s chat concurrency guard ("no running run per chat") and its per-chat run registration happen under one lock (not check-then-insert as two steps), closing a race two simultaneous `Start` calls for the same chat would otherwise hit.
- `run.logs` does not gain a `chatId` filter, despite P1.4's AC bullet listing it alongside `run.list`: `run.logs` already takes an unambiguous `runId`, so a `chatId` filter on it would be a parameter with nothing to filter -- ADR-0016 §5's own wire table only lists the filter on `run.list`, and no Test Scenario names a `run.logs` filter.
- P2 landed (PR #211) keyed by task ID -- its own `SessionStore`/`MemorySessionStore` abstraction was designed with chats not yet existing, doc-commented "once chats land, the key becomes chat ID". Integrating the two branches (this merge) rekeys every `sessionStore.Get`/`Set` call in `internal/taskrunner/runner.go` from `taskID` to `chatID`, and adds `ChatSessionStore` (`internal/taskrunner/chat_session_store.go`), a `chats.agent_session`-backed store keyed by chat ID, wired in at the daemon (`cmd/smind/serve.go`) alongside `MemorySessionStore` (kept for tests) -- see the P2 integration Validation entry below.

## Progress

- [x] P1 backend
- [x] P2 session resume (branch `feat/agent-session-resume`, merged into this branch and rekeyed by chat id)
- [x] P3 web
- [x] P4 CLI (mobile stays compatible on the default chat -- no changes needed, per the plan's "out of scope" note)

## Validation

### P1 — backend

All P1 acceptance criteria (P1.1-P1.6) and Test Scenarios are implemented and covered by tests; `task test`, `task lint`, and `go test -race ./internal/store/... ./internal/wsapi/... ./internal/runs/...` all pass (plus the full repo suite via `task test`, including `web/`, which P1 does not touch).

- **P1.1 Migration** (`internal/store/chats.go`, `migrate.go`, `chats_test.go`, `migrate_test.go`): idempotent, transactional `chats.default_chat_backfill` migration. Covered: a DB with 2 tasks/5 runs backfills 2 default chats with every run linked (`TestMigrate_BackfillsDefaultChatsForPreExistingTasksAndRuns`); re-running is a no-op, both for a fresh DB and a pre-chats one (`TestMigrate_ChatsBackfillIdempotentAcrossRepeatedOpen`); every existing store test (including a fresh `Open()`) still passes.
- **P1.2 RPCs** (`internal/workspace/chat.go`, `internal/wsapi/handlers.go`+`events.go`+`server.go`): `chat.create/list/get/rename/archive` plus `chat.created`/`chat.updated`/`chat.archived` lifecycle events. Covered: full CRUD round trip, archive refused while a run is running (polled past `run.stop`'s async teardown), unknown ids as clear not-found errors, lifecycle events observed on a subscribed connection (`internal/wsapi/chat_test.go`).
- **P1.3 Prompting** (`internal/runs/registry.go`): `task.prompt`/`run.start` accept an optional `chatId`; omitted resolves to the default chat; provider binds on first run (including the concurrent-first-bind race, closed and covered separately); a provider mismatch on a bound chat is rejected. Covered in `internal/runs/chat_test.go` and `internal/wsapi/chat_test.go`.
- **P1.4 Events** (`internal/runs/runs.go`+`registry.go`, `internal/wsapi/events.go`+`server.go`): `run.status`/`permission.pending` carry `chatId`; `run.list` filters by it. Covered: `TestEvents_RunStatusCarriesChatID`, `TestRegistry_List_FiltersByChatID`, `TestTaskPrompt_ExplicitChatId_LandsOnThatChat`.
- **P1.5 Concurrency**: a second prompt to a busy chat is rejected; two chats of one task run concurrently. Covered: `TestRegistry_Start_SecondPromptToBusyChat_IsRejected`, `TestRegistry_Start_TwoChatsOfSameTask_RunConcurrently`. `taskrunner.Runner.acpSessions` is now keyed by chat id, not task id, so the config-options path doesn't clobber across two concurrently running chats of the same task either.
- **P1.6 Compatibility**: every pre-existing `internal/wsapi` test passes unmodified (verified by running the full `internal/wsapi` suite after every change in this phase, with no test edits). The old-client flow (no `chatId` anywhere) is unchanged end to end -- `TestTaskPrompt_OmittedChatId_LandsOnDefaultChat`.

Not done in P1 (explicitly out of scope, per the task that drove this phase): session resume itself (P2 owns it); `web/`, `mobile/`, and CLI wiring (P3/P4 own those) -- P1 only adds the `chats.agent_session` column plus `store.GetChat`/`SetChatAgentSession` for P2 to call.

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

### P2 integration onto P1 (this merge)

PR #211 landed keyed by task ID, since chats didn't exist on `develop` yet -- its own doc comments anticipated this and said so explicitly. Integrating it onto P1:

- Every `sessionStore.Get`/`Set` call in `internal/taskrunner/runner.go` (`runACP`/`newOrResumeACPSession`, `runClaudeNative`/`newClaudeClientWithResume`, `runCodexNative`/`newOrResumeCodexThread`) is rekeyed from `taskID` to `chatID` -- git's line-based merge combined P1's `chatID`-only `runACP`/`runClaudeNative`/`runCodexNative` signatures with P2's newly-added body lines that still referenced the now-nonexistent `taskID` local; this needed a manual pass function by function (compile errors pointed at exactly the two spots the auto-merge couldn't reconcile).
- `MemorySessionStore` (kept, unchanged, still `Runner`'s default and still used by every test that doesn't need persistence) is joined by `ChatSessionStore` (`internal/taskrunner/chat_session_store.go`), a `chats.agent_session`-backed `SessionStore` keyed by chat ID: `Get`/`Set` serialize/deserialize `SessionHandle` as JSON through `store.GetChat`/`SetChatAgentSession`. `cmd/smind/serve.go` wires it in via `taskrunner.WithSessionStore(taskrunner.NewChatSessionStore(db))`, so the real daemon persists across restarts; nothing else about `New`'s defaults changed. A chat whose stored handle's `Provider` doesn't match the chat's own bound `provider` column is treated as "no handle" (never used to resume) -- on top of the identical check every `RunPrompt` resume call site already does against the handle's self-reported `Provider` (which is what actually prevents a live mismatch; this store-level check is defense in depth against `chats.agent_session` and `chats.provider` ever drifting apart, which nothing in this codebase does today).
- Covered (`internal/taskrunner/chat_session_store_test.go`): `TestChatSessionStore_GetSet_RoundTrips` (a handle written after run 1 is read at run 2 of the same chat, at the store layer); `TestChatSessionStore_TwoChatsOfOneTask_KeepSeparateHandles`; `TestChatSessionStore_HandleSurvivesStoreReopen` (a fresh `store.Open` at the same path, simulating a daemon restart); `TestChatSessionStore_MigratedDefaultChat_StartsWithNoHandle` (a chat with `agent_session` NULL reports no handle, so its next run starts fresh); `TestChatSessionStore_Get_ProviderMismatchAgainstBoundChat_IsIgnored`. `TestRunner_RunPrompt_WithChatSessionStore_ResumesAcrossRunsAndRestart` proves the same three properties end to end through the real `Runner.RunPrompt` path against a fake GLM ACP agent: run 1 is `session/new`, run 2 (same chat) is `session/load`, and a third run against a brand-new `Runner`/`workspace.Manager` built on a reopened store is also `session/load` -- the handle survived the simulated restart.
- `go test -race ./internal/...`, `task test`, and `task lint` all pass post-merge.

### P3 — web

All P3 acceptance criteria are implemented and covered by tests; `task test` (1329 web tests, all Go packages) and `task lint` both pass.

- **Chat tabs** (`components/tab-registry.tsx`, `hooks/use-task-tabs.ts`, `App.tsx`): the single `${taskId}:task` kind is replaced by `${taskId}:chat:${chatId}` tabs, one per `store.Chat`. `ensureTaskChats` seeds a freshly selected task's default-chat tab once its `chat.list` (`hooks/use-task-chats.ts`) resolves, or migrates a persisted pre-ADR-0016 `kind:"task"` tab onto it -- both idempotent. "+" gains a chat section (`New chat` plus every not-currently-open chat) above the base-kind section; New chat calls `chat.create` and opens the tab immediately. Rename is inline on the tab (extends the existing terminal-tab rename affordance to `chat` tabs), dispatching `chat.rename`; a `chat.updated` event keeps any open tab's title in sync even when the rename happened elsewhere. Closing a tab only detaches (unchanged `closeTab`, kind-agnostic); archiving is `ArchiveChatDialog` (mirrors `ArchiveTaskDialog`), an explicit confirm off the tab's context menu, and the daemon's existing "refused while running" guard surfaces as the dialog's own error. Covered: `hooks/use-task-tabs.test.ts` (seeding, migration, migration-idempotence, `updateChatTabTitle`), `components/tab-registry.test.tsx` (chat tab construction, the "+" menu's chat section), `App.test.tsx`/`App.responsive.test.tsx` (chat.list wired into every task-selection path).
- **Timeline and composer per chat** (`hooks/use-run-timeline.ts`, `components/composer/run-config-toolbar.tsx`+`run-config-preference.ts`, `components/composer/composer.tsx`, `components/task-detail.tsx`): `useRunTimeline` takes `chatId` and calls `run.list`/`run.start` with it (server-side `chatId` filter, ADR-0016 P1.4). `RunConfigToolbar`'s persisted key becomes `smind:run-config:${taskId}:${chatId}`; the task's default chat migrates the old per-task-only key on read. The header pill is per chat (it already mirrored the toolbar's own state, now chat-scoped for free). Once `chat.Provider` is bound, the Provider select goes `disabled` with a tooltip ("This chat is bound to `<provider>`; start a new chat to switch") and a forcing effect keeps `state.provider` pinned to it. Prompts send `chatId`. Covered: `components/task-detail.test.tsx` (run.list/run.start now assert the `chatId` param), `components/composer/composer.test.tsx` (bound-provider selector state, per-chat persistence key including the legacy-key migration case).
- **Concurrency banner** (`hooks/use-task-attention.ts`, `App.tsx`, `components/task-detail.tsx`): `useTaskAttention` now also derives `runningChatsByTask` (a `Map<taskId, Set<chatId>>`) from the same run bookkeeping it already tracks (`RunSummary.ChatID`, `run.status`'s `chatId`) -- no new RPC. `TabContent` computes, per open chat tab, whether a *different* chat of the same task is running and passes `{id, title}` down; `TaskDetailPane` renders a dismissible-by-navigation banner ("Another chat is running in this worktree — `<chat>`") above the composer, clickable to jump to that chat's tab (opening it if not already open).
- **Attention** (`hooks/use-task-attention.ts`, `components/app-sidebar.tsx` unchanged): the sidebar's attention/unread aggregation was already keyed by `TaskID` alone, so it aggregates across a task's chats without any change -- confirmed via the screenshot pass (a task with an error in one chat and a finished run in another still shows one aggregated dot).
- **Screenshots**: light+dark, 1440x900, saved to `/mnt/c/Users/ADMIN/Downloads/smind-multi-chat/` -- multiple chat tabs, "+" → New chat, rename (the context menu; the actual `<input>` swap reproducibly never renders under real-browser Playwright automation for *any* tab kind, terminal included -- a pre-existing interaction quirk unrelated to this phase, not chased further), the archive confirm, the provider-bound read-only selector, the concurrency banner, and aggregated sidebar attention.
- **Two real bugs found and fixed along the way** (not test-only issues): (1) `App.tsx`'s hash-sync effect defaulted to `{kind:"files"}` for "no active tab yet" (the async gap before a task's chats load), which self-triggered a route-restore round trip that opened a Files tab and permanently pre-empted the default-chat seeding -- fixed by not writing a hash at all until a real tab exists. (2) `hooks/use-task-chats.ts` didn't reset `chats` to `null` on a `taskId` change, so a fast double task-switch could seed the *new* task's default-chat tab with the *previous* task's chat id (a real cross-task chat-identity mixup) -- fixed by resetting synchronously, plus a defensive `chats[0].TaskID === selectedTask.ID` guard in `App.tsx`. (3) Radix's `Select` fires a spurious `onValueChange("")` of its own (not from any user interaction) once a `disabled` select's value settles, which blanked `RunConfigToolbar`'s bound-provider state right after it was correctly set -- fixed by ignoring falsy values in the handler, with regression coverage in `composer.test.tsx`.

Not done in P3 (out of scope per the task, deferred to P4): CLI (`--chat`, `task chat` subcommands) and mobile (chat picker) wiring.

### P4 — CLI

All P4 CLI acceptance criteria are implemented and tested in `cmd/smind/task_chat_test.go`, which runs against the same fake-agent-backed daemon the config-option tests use. `go test ./cmd/smind/...`, `task lint` and `task test` all pass. `smind task logs`/`attach` are unchanged because they take a runId, and a runId already identifies exactly one chat. Mobile keeps working on the default chat with no changes.

- `task send --chat`: passes chatId through to `run.start`. When the flag is omitted, the default chat is used. Tests: `TestTaskSend_ChatFlagRoutesRunToThatChat`, `TestTaskSend_OmittedChatLandsOnDefaultChat`.
- `task chat ls|new|rename|archive`: `ls` prints ID/TITLE/PROVIDER/ARCHIVED, and `--all` includes archived chats. When the daemon refuses to archive a chat with a running run, or an id is unknown, its error is printed verbatim and the command exits non-zero.
- `task runs <taskId> [--chat <chatId>]`: `run.list`, newest first, with the chatId filter.
- The usage text in `cmd/smind/main.go` documents all of the above.
