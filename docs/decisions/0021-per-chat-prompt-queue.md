# 0021: Per-chat prompt queue

## Status

Accepted (2026-10-10). The user accepted the direction with
`docs/plans/active/orchestration-and-metering.md` on 2026-09-28: one
persisted per-chat queue with `reject|queue|interrupt` delivery modes, in
place of a separate inbox and a separate steering mechanism. On
2026-10-10 the user reviewed the sub-decisions and accepted them, with one
change: decision 5. After a daemon restart the queue **keeps delivering**
instead of pausing. Depends on ADR-0019 (run-config fields) and on the
plan's O1 fix (a stopped run keeps its session).

## Context

- A prompt to a chat that already has a running run is **rejected**:
  `runs: start: chat %d already has a running run`
  (`internal/runs/registry.go:470`, ADR-0016 §4).
- The only queue is **client-side**, in the web composer
  (`composer.tsx`). It is lost when the tab closes and invisible to the
  CLI, mobile, and MCP orchestrators.
- A lead/peer workflow on a user's project needs two things smind can't
  do today:
  - **a peer escalating to a busy lead** ("the mount can't take this
    load") without the message being lost;
  - **a human redirecting a running agent** without becoming a
    per-message dispatcher.
- Mid-turn input injection is impossible: ACP v2 has no verb for it and
  `claude-agent-sdk-go` has no primitive (`smind-control-parity.md`).

How the references handle a message to a busy agent (verified
2026-09-28):

| Project | Behavior | Durable? |
|---|---|---|
| Paseo | Replaces: cancels the running turn and starts the new prompt (`agent-prompt.ts:306-335`); optional provider steering | nothing queued |
| Codex v2 | Mailbox with `send_message` = QueueOnly and `followup_task` = TriggerTurn (`multi_agents_v2/message_tool.rs`) | pending queue in memory only |
| dsh | FIFO Agent inbox; `interrupt()` keeps the inbox (`docs/subsystems/subagent.md:138-144`) | yes |
| Claude Code | File-backed teammate inbox; refuses bursts beyond capacity (CHANGELOG) | yes |

## Decision

1. **A persisted queue per chat.** New table `chat_queue`, FIFO by `id`
   within a chat:

   ```sql
   CREATE TABLE IF NOT EXISTS chat_queue (
       id INTEGER PRIMARY KEY AUTOINCREMENT,
       chat_id INTEGER NOT NULL REFERENCES chats(id),
       prompt TEXT NOT NULL,
       run_config TEXT NOT NULL DEFAULT '{}', -- JSON: provider, permissionMode, thinkingLevel, viaProxy
       source TEXT NOT NULL,                  -- 'human' | 'orchestrator' | 'agent'
       from_task_id INTEGER,                  -- sender, when source='agent'
       from_chat_id INTEGER,
       status TEXT NOT NULL,                  -- 'queued' | 'delivered' | 'cancelled'
       run_id TEXT,                           -- set on delivery
       cancel_reason TEXT NOT NULL DEFAULT '',
       created_at TIMESTAMP NOT NULL,
       updated_at TIMESTAMP NOT NULL
   );
   ```

2. **`whenBusy` on `task.prompt`/`run.start`**, optional:
   - `reject` is the default for wsapi callers, so today's behavior is
     unchanged for every existing client.
   - `queue` appends to the chat's queue.
   - `interrupt` inserts at the front of the queue and stops the running
     run.

   If the chat is idle, all three start the run immediately. If it is
   busy and the mode is `queue` or `interrupt`, the call returns
   `{queued: true, queueItemId}` instead of `{runId}`. That shape is only
   reachable by passing `whenBusy`, so old clients never see it.
3. **Delivery in `Registry.finish`** (`registry.go:785`). After a run's
   terminal state is recorded, the oldest `queued` item for that chat is
   started with its stored `run_config`, and marked `delivered` with its
   `run_id`. If the item fails validation at delivery (for example, its
   provider no longer matches the chat's bound provider), it is marked
   `cancelled` with `cancel_reason`, an event is emitted, and delivery
   moves on to the next item. Delivery never loops on a bad item. An
   `interrupt` item is delivered onto the resumed session once the
   stopped run finishes (O1).
4. **Provenance.** An item with `source='agent'` is delivered with a
   one-line header, `[message from task #T, chat #C]`, so the receiving
   agent knows who is talking. Human and orchestrator items are delivered
   verbatim. Provenance is self-declared by the MCP caller; it is not
   authentication, because every caller shares the daemon's trust domain
   (ADR-0017).
5. **After a daemon restart the queue keeps delivering** (user,
   2026-10-10). Queued items survive the restart. This is the reliability
   lesson from Paseo's restart data loss.
   - Once `runs.New` has reconciled `running` rows to `interrupted`, the
     daemon delivers the oldest `queued` item of every chat that has one,
     through the same path as decision 3. The interrupted run is not
     retried.
   - The delivered prompt starts on the chat's resumed session (O1), so
     the agent still sees the earlier context.
   - There is no paused state and no `chat.queueResume`. To stop pending
     work, a user cancels items with `chat.queueCancel`.
   - Rationale: work the user queued was meant to run. Restarts are rarer
     now that the desktop app no longer restarts a daemon that has runs
     in flight (`desktop-macos-app` M5).
6. **Permission rules apply at enqueue time.** An item's
   `permissionMode` is validated exactly like a direct prompt, including
   ADR-0019 resolved decision 6: an orchestrator cannot queue an
   auto-approving mode unless it comes from a human-authored profile.
7. **A queue bound.** At most 20 `queued` items per chat. Beyond that,
   enqueue fails with a clear "queue full" error, following Claude Code's
   precedent of refusing bursts.
8. **Wire additions**, all additive:
   - `chat.queueList {chatId}` returns the items;
   - `chat.queueCancel {itemId}` cancels one;
   - event `chat.queueUpdated {chatId, items}` carries a full snapshot
     (ADR-0009 shape).
   - `run.status` is unchanged.
9. **Clients.**
   - MCP `task_send` defaults to `whenBusy=queue`.
   - CLI: `smind task send --when-busy=queue|interrupt`.
   - The web composer's client-side queue is replaced by the server
     queue, as a UI follow-up. Queued items become visible and
     cancellable in every client.
10. **No separate inbox, and no change to `task_wait`.** A message to a
    lead is a queued prompt on the lead's chat. When the lead is itself a
    smind chat, delivery starts its next turn automatically. When the lead
    is an external orchestrator (a user's own Claude Code session driving
    smind over MCP), it learns of peer results through `task_wait` on the
    peer's run, as ADR-0017 already provides. This supersedes the plan
    draft's "`task_wait` returns early when a message is queued".

## Alternatives considered

- **Paseo's replace-by-default.** Rejected as the default, because it
  silently throws away the running turn's in-flight work. It remains
  available explicitly as `interrupt`.
- **An in-memory mailbox (Codex).** Rejected. A daemon restart would lose
  undelivered escalations, which is exactly the Paseo reliability failure
  smind is designed against.
- **A separate inbox entity, distinct from the prompt path.** Rejected.
  It would be two delivery mechanisms with the same semantics. In smind,
  a message to an agent *is* a prompt to its chat.
- **Keeping the queue client-side.** Rejected. It is invisible to CLI,
  mobile, and MCP, and lost on tab close.
- **Mid-turn injection.** Not possible with today's protocols; revisit
  only if ACP or the Claude SDK add it.

## Rationale

One primitive covers both SLP needs. Peer→lead escalation is `queue`,
and a human redirect is `interrupt` on top of session resume. It also
fixes an existing product gap: queued follow-ups that only exist in one
browser tab. Persisting the queue and pausing it after a restart puts the
two failure modes the other way around from Paseo: nothing is lost, and
nothing runs unattended by surprise. The wire stays backward compatible
because `reject` is the default and the new result shape needs an opt-in
field.

## Cross-references

- ADR-0009 (snapshot events).
- ADR-0016 §4 (one running run per chat, which this keeps).
- ADR-0017 (MCP `task_send`/`task_wait`).
- ADR-0019 (permission-mode validation).
- ADR-0020 (`viaProxy` in `run_config`).
- `docs/plans/active/orchestration-and-metering.md` (O1, Step 2).
