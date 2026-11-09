# Chat fork, edit-and-retry and per-turn file undo (ADR-0057)

Status: active (2026-11-09). ADR-0057 decisions D1-D6 accepted as recommended (Supervisor, under the Human's delegation). Size: L, four sequential PRs (not stacked). This file is the spec of PR 1; later PRs extend it.

## PR 1: data model, fork on Claude, labelled context-seeded fallback

Scope: a chat can be forked at a finished turn. The Claude harness forks natively; every other provider gets the labelled fallback; the Codex native fork is **waiting on a Codex probe** (the app-server calls need a live thread, and there are no Codex credits), so Codex chats get the fallback only. No file undo, no refs, no snapshots in this PR (PR 3). Edit-and-retry is PR 2.

### Acceptance criteria

- AC1: a side table `chat_forks (chat_id PK, parent_chat_id, forked_at_run_id, fork_kind, mode, seed, seed_used_at, created_at)` and a log table `chat_claude_totals (chat_id, at, totals_json)` records a fork; the `chats` table and every query over it are unchanged, so every existing reader sees the same bytes (a deviation from the ADR's column names, chosen to keep the readers of `chats` untouched).
- AC2: `chat.fork {chatId, atRunId, mode}` (human only: not run-scoped, not an MCP tool) creates a child chat of the same task bound to the parent's provider. `mode: native` (default) forks the Claude session at the anchor turn; `mode: seeded` starts a fresh session whose first prompt carries a bounded summary of the earlier turns.
- AC3: native fork launches the child's first run with `--resume <session of the anchor run> --fork-session --resume-session-at <entry uuid>` and stores the new session id like any session. The handle's pending-fork marker is cleared at the first `init` and at the result, so a second turn resumes the child's own session and never forks again.
- AC4: the entry uuid and the session id come from the anchor run's own `native_ref` and the transcript, through the ADR-0053 reader: a transcript that is gone, outside the root, drifted, or whose turn cannot be singled out (`slice_not_found`) refuses the native fork with the reader's reason; the seeded mode stays available.
- AC5: a native fork is refused for a harness that is not Claude Code (reason `fork_unsupported`; Codex says "waiting on the Codex probe"), for a chat with a running run (`native_session_in_use`), and for a run that is not a finished run of that chat. The parent's session file and chat row are never written.
- AC6: the seeded fallback is built from stored run rows only (prompt and `final_text` of the earlier turns, newest turns kept, capped), never from a provider session, and is prepended to the child's first prompt once (`seed_used_at`), not stored in `runs.prompt`. The child is marked lossy in `chat.list`.
- AC7: the first turn of a forked Claude chat reports its own usage, not the inherited conversation's: the baseline is the parent's cumulative totals as of the anchor run (the log is written when a Claude turn ends, by a session store that keeps one).
- AC8: `chat.list` and `chat.get` answer an additive `fork {parentChatId, forkedAtRunId, kind, mode, lossy}` for a forked chat (on `chat.list`, `chat.get`, `chat.fork` and the `chat.created`/`chat.updated` events) and nothing new for any other (byte-identical payload for unforked chats: golden test).
- AC9: web: a "Fork from here" action on a finished turn (native, or the labelled fallback when native is refused; a passing refusal such as a running chat or a busy daemon stays an inline error), and a banner naming the parent. The parent is named in the pane's banner only: no tab affordance in PR 1 (the banner says "another chat" when the parent is archived). CLI: `smind task chat fork <chatId> --at <runId> [--seeded]`. Mobile in PR 1b. Light and dark frames for each web state (taken on the demo build with the scripted daemon).
- AC10: deleting a task deletes its fork rows and totals log; archiving a chat leaves its children alone.

### Named tests (as written)

Store: `TestChatForks_SchemaAndAccessors`, `TestChatForks_ChatsQueriesUnchanged`, `TestChatForks_ClaimSeedOnce`, `TestClaudeTotalsLog_BaselineAtAnchor`, `TestChatForks_DeleteTaskRemovesRows`.
Reader: `TestAnchor_ClaudeLastAssistantUUIDOfTheMatchedTurn`, `TestAnchor_MissingUUIDIsEmpty`, `TestAnchor_SliceNotFoundOnDuplicatePrompt`, `TestAnchor_CodexHasNone` (path confinement is the existing reader's tests, which the fork reuses).
Runner: `TestRunner_ForkHandleUsesResumeForkAndResumeSessionAt`, `TestRunner_ForkMarkerClearedAtInitEvenWhenTheTurnDies`, `TestRunner_ForkFallsBackToFreshSessionWhenResumeFails`, `TestRunner_PlainHandleNeverForks`, `TestRunner_ForkWithMalformedEntryNeverPassesForkFlags`, `TestRunner_ClaudeTurnLogsItsTotals`, `TestValidForkEntry`.
Registry: `TestForkAnchorOf_ClaudeRunResolvesSessionAndEntry`, `TestForkAnchorOf_RefusalsAreReasons`, `TestForkSeed_*`, `TestChatFork_SeededPrefixOnceAndNotStored`, `TestChatFork_SeedGivenBackWhenTheFirstRunFails`, `TestChatFork_SeededRunsNativeRefHashesTheDeliveredPrompt`, `TestRun_NativePromptIsTheDeliveredOne`.
Wire: `TestChatFork_NativeCreatesChildWithPendingForkHandle`, `TestChatFork_ParentUntouched`, `TestChatFork_BaselineTotalsAtAnchorBecomeTheChildsUsageBaseline`, `TestChatFork_SeededCreatesLossyChildFromStoredTurns`, `TestChatFork_RefusedForOtherHarnessesWithReasonAndSeededOpen`, `TestChatFork_RefusesBadTargets`, `TestChatFork_RefusesARunThatIsNotAUserRun`, `TestChatFork_RPCHumanOnlyAndNotRunScoped`, `TestChatList_ForkFieldAdditive`, `TestChatLifecycleEventsCarryTheForkField`, `TestForkTitle`, `TestWsapiMethodMapGoldenFleetOff` (golden gains `chat.fork`).
A parent with a running run is refused through the reader's `native_session_in_use` (`rebuildTarget`'s own tests); there is no separate handler test.
Mutations (all fail a named test): `--fork-session` dropped, `--resume-session-at` dropped, the marker not cleared at init, seed never given back, seed prepended on every turn, the native_ref hash of the stored prompt, the handler run-scoped or without the chat/run/purpose checks, a baseline taken at the wrong time, the Codex refusal removed, the anchor not the turn's last record, the delete steps removed, the event payloads undecorated, the title cut by byte.

### Decisions in this PR

- Fork metadata lives in a side table, not in new `chats` columns (AC1).
- The anchor session is the anchor run's `native_ref.sessionId`, not the chat's current handle (a failed resume starts a new session, so older runs can belong to an older one).
- The Codex native fork and the Codex `thread/fork` probe are deferred: **waiting on the Codex probe** (no credits). Codex chats get the labelled fallback until it is run.
- A seeded fork's seed is claimed just before the run starts and given back if the run fails (a retry may then repeat a seed the provider already saw: redundant, never lost). The first run's native_ref hashes the delivered text, so its history rebuilds and it can be forked natively.
- A native fork whose resume fails at start-up degrades like any stale session (a fresh session and a session note); the chat keeps saying `native` (documented limit, no mark is written).
- `CreateChatThen` is not atomic: a failed setup archives the half-made chat and suppresses the announcement; an `includeArchived` list can show it.
- `chat.fork` has no dedupe: a double submit or two concurrent calls make two chats (harmless). `smind task chat fork` and the RPC are in the human-only tables of the agent self-approval hardening (CLI refusal in agent runs, `no-self-approve` floor rule, `HumanOnlyRPCs`).
- The CLI is `smind task chat fork` (under the existing `task chat` group), not `smind chat fork` as the ADR text says.
- Usage: the first turn of a native fork starts from the totals logged at or before the anchor turn's end; a turn that logged none falls back to the previous turn's (over-reports at worst).
- Condition from the Supervisor for the PR that creates `refs/smind/turns/` (PR 3, not this one): the refs are never pushed (no push path may carry them), are pruned with run retention and on task delete, with tests for each.

## PR 2: edit-and-retry and what a fork shows

Scope: a user prompt can be edited and retried without touching the original (fork at the previous turn, then the edited text sent in the child); a forked chat shows where it came from. No file undo (PR 3, 4).

### Acceptance criteria

- AC11: `chat.fork` takes `kind` (`fork` default, `retry`); a retry's fork record has kind `retry` and the child is titled "Retry of ...". Unknown kinds are refused. Human-only like fork.
- AC12: `chat.forkSeed {chatId}` (human-only, not run-scoped) returns the seed a seeded fork was given, "" for any other chat.
- AC13 (web): an "Edit & retry" action on a user prompt opens a dialog with the prompt in an editable box. Confirm forks at the nearest earlier DONE turn of the chat (native, with the labelled fallback exactly as in PR 1) and starts the edited prompt in the child with the replaced run's provider, model and thinking and the composer's current permission mode; the child's tab opens. A first turn (no earlier finished turn) starts the edited prompt in a plain new chat, and the dialog says so; the first own turn of a forked chat forks the PARENT at its fork point instead, so the inherited history survives. If the chat was created but the prompt could not be started, the dialog says so and offers the new chat. The original chat is never changed.
- AC14 (web): the fork dialog's heading line is `From "<chat title>", turn <n>`; a retry dialog says it replaces turn <n>.
- AC15 (web): in a forked chat the turns inherited from the parent, up to the fork point, are shown read-only and de-emphasised above the chat's own turns with a "Forked here" divider (also in the empty state, replacing "No runs yet" with an invitation to continue). A seeded fork shows instead the summary that was actually sent, labelled "Summary sent to the agent".
- AC16 (web): the banner reads "Forked from <chat> at turn <n>." (a retry: "Retry from <chat> after turn <n>.", since the replaced turn may follow the anchor after skipped runs) with a "View original" button that opens the parent chat; the 390 width keeps all of it. An archived or missing parent reads "another chat" without the button.
- AC17: light and dark frames at 1280 and 390 for each state.

### Named tests

Daemon: `TestChatFork_RetryKindIsRecordedAndTitled`, `TestChatForkSeed_ReturnsTheSeedOnlyForASeededFork`, the method-map golden. Web: named with the peers' tests (retry dialog, retry flow, first-turn path, inherited turns, seeded summary, banner link, dialog heading).

## Progress

Daemon side done and tested: side tables, reader anchor, runner fork handle, `chat.fork`, seeded fallback, additive chat payloads, events. CLI and web done (GLM peers, reviewed; frames light and dark at 1280 and 390: the turn action, the native dialog, the fallback dialog, the banner for a native and a lossy fork). Mobile is PR 1b.

## Validation

CLI: `TestTaskChatForkSeeded`, `TestTaskChatForkNativeRefusedSuggestsSeeded`, `TestTaskChatForkUsageErrors`. Web: `fork-chat-dialog.test.tsx` (native, fallback, no seeded option, a passing refusal, generic error, cancel, reason labels), `task-detail-fork.test.tsx` (action opens the dialog, banner for forked and lossy chats, none for unforked), `run-timeline.test.tsx` (the action's visibility rules), the demo guard (`chat.fork` rule). Go and web suites pass. Plugin and ACP providers get the native refusal and then the labelled offer (the ADR's "hidden for a plugin provider" is not implemented). Not verified: a live `claude --fork-session` run.

### PR 2 progress, validation and limits

Built: `chat.fork` kind and `chat.forkSeed`; the Edit & retry action, dialog and flow; inherited turns, the seeded summary, the divider, the banner and the dialog heading; demo rules. Frames (light and dark, 1280 and 390, demo build): the prompt action, the retry dialog (first turn, anchored, fallback), the retry child, the fork dialog with its heading, the native child with inherited turns, the seeded child collapsed and expanded. Mutations on committed code (kind ignored, retry title, forkSeed human-only, anchor choice, retry in a fork, bounded loading, double-submit guard, partial failure, banner wording, seed failure, auto-accept) each fail a named test.
Review round 2 (Steward): `chat.forkSeed` joined `chat.fork` in the human-only RPC table (every new RPC and CLI of PRs 1 and 2 is in it: `chat.fork`, `chat.forkSeed`, `smind task chat fork`; `chat.create` and `run.start` are the existing agent-reachable ones and the retry uses them as the composer does); a retry never inherits the replaced run's permission mode (the composer's when it is on the same provider, else the provider's default, auto-accept off) and the dialog says which applies; the replaced turn's attachments are shown in the dialog and sent again by default, with a checkbox to drop them (the choice is sent as `keepAttachments`); a retry of a turn whose provider is no longer configured is refused in the dialog before any chat is created (the daemon's `run.start` refuses an unknown provider or an unoffered mode too: `TestStart_UnknownProviderIsRefusedBeforeAnyRun`, `TestStart_PermissionModeOutsideTheCatalogIsRefused`); a fork point that is no longer among the parent's runs loads no history (it failed open before). Cancelling the dialog after "the chat was created but its prompt could not be started" leaves that empty chat in the task's chat list without opening its tab (the dialog offers "Open the new chat" first).

Limits: inherited turns come from the parent chat only (a fork of a fork shows the parent's own turns, not the grandparent's, and a seeded fork of a fork carries only the parent's own stored turns); "turn n" counts the chat's runs, not only user turns; the retry divider reads "Retry starts here"; on a narrow screen the seeded summary, once opened, can push its heading out of view because the empty chat follows its tail; a retry of a later turn in a chat whose earlier runs all failed starts from the chat's fork point or a plain new chat. The partial-failure state (chat created, prompt not started) is covered by tests but has no frame.
