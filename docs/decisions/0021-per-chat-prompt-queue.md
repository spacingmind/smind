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

**Amended 2026-11-09 (Proposed): mid-turn steering** — see [Amendment 2026-11-09](#amendment-2026-11-09--mid-turn-steering-proposed). The Context
bullet "Mid-turn input injection is impossible" and the "Mid-turn injection" rejection hold only for ACP; Claude native and Codex native can steer a running turn. The
three modes of decision 2 are unchanged; a fourth, `steer`, is added.

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
| A reference workbench | Replaces: cancels the running turn and starts the new prompt (`agent-prompt.ts:306-335`); optional provider steering | nothing queued |
| Codex v2 | Mailbox with `send_message` = QueueOnly and `followup_task` = TriggerTurn (`multi_agents_v2/message_tool.rs`) | pending queue in memory only |
| A reference agent harness | FIFO Agent inbox; `interrupt()` keeps the inbox (`docs/subsystems/subagent.md:138-144`) | yes |
| Claude Code | File-backed teammate inbox; refuses bursts beyond capacity (CHANGELOG) | yes |

## Decision

1. **A persisted queue per chat.** New table `chat_queue`, FIFO by `id`
   within a chat:

   ```sql
   CREATE TABLE IF NOT EXISTS chat_queue (
       id INTEGER PRIMARY KEY AUTOINCREMENT,
       chat_id INTEGER NOT NULL REFERENCES chats(id),
       prompt TEXT NOT NULL,
       run_config TEXT NOT NULL DEFAULT '{}', -- JSON: provider, permissionMode, autoAccept, thinkingLevel
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
   lesson from a reference workbench's restart data loss.
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

- **A reference workbench's replace-by-default.** Rejected as the default, because it
  silently throws away the running turn's in-flight work. It remains
  available explicitly as `interrupt`.
- **An in-memory mailbox (Codex).** Rejected. A daemon restart would lose
  undelivered escalations, which is exactly the reference workbench reliability failure
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
two failure modes the other way around from a reference workbench: nothing is lost, and
nothing runs unattended by surprise. The wire stays backward compatible
because `reject` is the default and the new result shape needs an opt-in
field.

## Cross-references

- ADR-0009 (snapshot events).
- ADR-0016 §4 (one running run per chat, which this keeps).
- ADR-0017 (MCP `task_send`/`task_wait`).
- ADR-0019 (permission-mode validation).
- ADR-0020 (per-run usage from agent events; no `viaProxy`).
- `docs/plans/active/orchestration-and-metering.md` (O1, Step 2).

## Amendment 2026-11-09 — mid-turn steering (Proposed)

Spec, acceptance criteria and PR split: `docs/plans/active/mid-turn-steering.md`. Every choice under [Decisions for the Human](#decisions-for-the-human-steering) carries a
recommendation. No code is written before the Human answers.

### Why the claim was wrong

Decision 2 and the Context said mid-turn injection is impossible because "ACP v2 has no verb for it and `claude-agent-sdk-go` has no primitive". Both halves need correcting:

- **Claude native can steer, and the SDK already has the primitive.** `Client.Query` writes a user line to the CLI's stdin and does not wait; `Client.Interrupt` sends the CLI's
  `interrupt` control request (`engine.go`). smind simply never writes while a turn is in flight.
- **Codex native has two verbs** smind never calls: `turn/steer` and `turn/interrupt` (app-server protocol, `v2/turn.rs`).
- **ACP has no explicit steer capability.** The v1 protocol has `session/cancel` (a notification that ends the running turn); the v2 draft decouples the prompt request from the turn so a
  prompt "lasts until the user message is inserted", lets a `session/prompt` sent during active work be inserted at once or held by the agent, and leaves explicit steering and queueing capabilities to a later RFD (a future ACP agent may inject on a plain prompt; the catalog flag is per provider and re-probed, not assumed). smind sends neither today: `Registry.Stop` cancels the run's
  context and the agent process is closed (`internal/runs/registry.go`, `internal/acp/client.go`), and an `interrupt` item is delivered on the resumed session.

### Probe results (Claude Code CLI 2.1.310, Haiku, stream-json, `--replay-user-messages`; two runs, about USD 0.013 in total)

Prompt A asked the model to run a 12-second `sleep` in Bash and then answer one word.

| What the probe did | What the CLI did |
|---|---|
| **Steer:** wrote a second user message about 1 ms after the Bash `tool_use` line | The running tool was **not** cut short (the sleep ran its ~13 s). The message was echoed back as a user line **immediately after the tool's result**, inside the **same** turn. The model then answered with both instructions satisfied. **Exactly one `result` line** for the whole exchange (two model turns inside it); no second result followed. |
| **Interrupt:** sent the `interrupt` control request, then a new user message | The control response came back in ~16 ms. The running tool was **aborted about 1 s later** (its result carried the CLI's "tool use was rejected" text). The interrupted turn ended with its **own `result`** of subtype `error_during_execution` (`is_error: true`, no text, still billed). The new message then ran as a **second turn on the same session** and was answered. |

Consequences, stated as facts of this CLI version only: (a) a mid-turn message is **queued by the CLI and injected at the next tool boundary**, not instantly and not as a
new turn; (b) an interrupt produces **two results** for one `Prompt`, which smind's reader (`forwardTurn`, one result per prompt, with its own skip rule for background-task
notifications) does not expect; (c) the CLI exited cleanly after stdin closed even with the second turn pending, so closing stdin early drops nothing but also cannot be used
as a cancel. **Not probed** (open, D9): a steer while the model is streaming text with no tool running; a steer during a permission wait; the same on a newer CLI (the injection
point is the CLI's own behaviour and can change; the probe script is kept for a re-check on upgrade).

### Capability matrix

| Provider | `steer` (inject into the running turn) | `interrupt` (abort the turn, keep the session) | What smind does today | After this amendment |
|---|---|---|---|---|
| claude-native | yes, native: a user line on stdin; lands at the next tool boundary (probe) | yes: control request `interrupt`; two results (probe) | `interrupt` mode = `Registry.Stop` (context cancel, CLI closed) then the item runs on the resumed session | `steer` via `Query` while the turn runs; `interrupt` stays Stop-and-resume (D4) |
| codex-native | yes: `turn/steer {threadId, expectedTurnId, input}` → `{turnId}`; fails if the active turn is not `expectedTurnId` | yes: `turn/interrupt {threadId, turnId}` | neither called; `interrupt` mode = Stop | `steer` via `turn/steer`, the turn id taken from the `turn/start` response; `turn/interrupt` for `interrupt` (D5) |
| acp (GLM, OpenCode, Cursor, …) | **no** | v1: `session/cancel` notification (the agent should end the turn with `cancelled`) | `interrupt` mode = Stop (agent closed) | `steer` unavailable → fallback (D2); `interrupt` unchanged, `session/cancel`-then-prompt deferred (D6) |

The catalog gains additive per-provider flags, `supportsSteer` and `supportsInterrupt` (next to `SupportsAutoAccept` and `LiveModeSwitch` in `taskrunner.ProviderInfo`), so every
client decides from the catalog and none hard-codes a provider name. ACP providers report false/false (their `interrupt` still works through Stop).

### Decision

**Add `whenBusy: "steer"` as a fourth delivery mode. `queue` and `interrupt` keep their meaning.**

| Mode | Meaning for a busy chat | Durable | Result shape |
|---|---|---|---|
| `reject` | error (default for wsapi callers) | n/a | error |
| `queue` | append; delivered at the next turn boundary (the default for MCP `task_send`; the CLI defaults to `reject`, except that it asks for `queue` while the task's setup script is still running) | yes (`chat_queue`) | `{queued, queueItemId}` |
| `interrupt` | insert at the front and stop the run; delivered on the resumed session | yes | `{queued, queueItemId}` |
| **`steer`** | **inject into the running turn** (provider-native); the turn continues and the model sees the text at its next boundary | **no** while in flight, **never lost** (below) | **`{steered: true, runId, steerId}`** (additive; reachable only by passing `steer`) |

Rules:

1. **Only when a run is actually running and its provider `supportsSteer`.** An idle chat starts the run normally (like every mode). A chat whose provider cannot steer
   applies the fallback of D2 (recommended: **degrade to `queue`**, with the result carrying `steerFallback: "queue"` and the reason, so the caller and the card say what
   really happened; never a silent drop).
2. **Never lost.** The steer is recorded **before** it is written: a `user_steer` event (§History) with `state: "sent"`. If the injection then fails — the turn ended in the race
   (Codex `expectedTurnId` mismatch, Claude process already gone), the write errors, or the run finishes with the steer unacknowledged — the text is **enqueued as an ordinary
   queue item** in the same chat, the event is updated to `state: "queued"`, and normal delivery continues. A steer never runs twice (the enqueue happens only after the
   write is known not to have been accepted; for Claude, which acknowledges nothing, "accepted" means the write returned without error and the run was still `running`, and
   the echoed user line confirms it).
3. **Text only in v1.** A steer with attachments, a model change or a permission-mode change is refused with a clear error (the run's configuration is never altered by a
   steer); the caller may send it as `queue`.
4. **A pending permission or question** (ADR-0055, Proposed, adds the question request; it is not merged yet) does not block a steer; Claude delivers it after that tool's result like any other (the permission is still waiting for its
   human). The card says "delivered after the pending request is answered".
5. **Size:** the existing prompt size cap; one steer in flight per run (a second is rejected with "a steer is already pending" until the first is echoed or queued), so a loop
   cannot flood a turn.

### Who may steer (steering text is input to a running agent)

- A **human** (web, mobile, CLI, desktop): yes.
- An **orchestrator** (a Lead steering its child; ADR-0017): yes, with the existing provenance. This is the high-value case: redirecting a Peer without killing its turn.
- An **agent** (`source='agent'`, the peer escalating to a busy Lead, ADR-0021 §4) and a **remote** caller (ADR-0048): **downgraded to `queue`**, because an agent breaking into
  another agent's running turn is exactly what the queue's turn boundary protects against. Recommended (D3).
- A steer from a non-human source is delivered with the one-line provenance header of §4 so the receiving model knows who spoke; the **text is data to the model, never a
  command to smind**: it changes no setting, grants no permission and cannot reach `run.respondPermission` (ADR-0017 stands; a test pins it).

### History: what the run records

- New additive event `user_steer` {steerId, text (capped), source, state: `sent` | `echoed` | `queued` | `failed`, turnId (Codex), at}. Old clients must ignore unknown event types (pinned by
  `TestSteer_NewEventIgnoredByOldDecoder`; compare ADR-0010 for unknown ACP update kinds).
- **Ordering.** Claude: smind adds `--replay-user-messages` (via the SDK's extra args) so the CLI's own echo anchors the steer **where the model actually saw it**, after the
  tool result; the event moves `sent` → `echoed` at that line. Without the echo the timeline would show the steer at send time, which is wrong by up to a whole tool call. Codex:
  the `turn/steer` response's `turnId` and the item order from the app-server. The timeline renders "You steered: …" at the anchor with the state ("queued instead" on fallback).
- The `runs` row, usage and cost are unchanged (a steer is part of the running run, not a new run); native-history rebuilders (ADR-0053) see the steer as the provider's own
  user message in the transcript.
- `interrupt` is unchanged in history: the interrupted run is `stopped` with its stop reason and the item starts a new run.

### Surfaces

- **wsapi** `task.prompt` / `run.start` accept `whenBusy: "steer"`; the result carries `steered`/`steerId` (or the queue shape on fallback). Additive: an old daemon rejects an
  unknown mode with its existing validation error, so a new client checks `supportsSteer` first.
- **CLI** `smind task send --when-busy steer|interrupt|queue|reject`; MCP `task_send.whenBusy` gains `steer` (human-source gating of §Who as above).
- **Web composer** (`components/composer`): while a run is running and the provider `supportsSteer`, a **Steer** button next to Send; Enter keeps today's behaviour (queue),
  the alternate-send shortcut keeps `interrupt` (D7). Mobile: the same as a secondary action in the send sheet. When the provider cannot steer the button is absent, not
  disabled, so no copy has to explain a missing feature.

### Failure modes

| Failure | Behaviour |
|---|---|
| run ends between the click and the write | enqueue as a queue item, `state: queued` (rule 2) |
| Codex `expectedTurnId` mismatch | same (the turn already moved on) |
| the CLI/agent process died during the write | the run finishes `error` as it does today; the steer is enqueued; delivery follows the existing restart rules (decision 5) |
| stdin write is not serialized with other writers (control responses are written from other goroutines) | the SDK already serializes `writeLine` with `writeMu`; PR1 adds a race-detector regression test |
| double-click / repeated steer | one in flight per run (rule 5) |
| provider cannot steer | fallback per D2 with the reason in the result |
| steer arrives while no tool runs (model streaming text) | **unknown today** (D9); until probed the card says "delivered at the model's next step" and the event is `echoed` only when the echo arrives, so the timeline never claims more than it saw |
| a CLI upgrade changes the injection point | the kept probe script is re-run; the contract test uses a recorded fixture, so a changed shape fails loudly |

### Options considered

1. **Reinterpret `queue` as steer where possible.** Rejected: `queue` means "after this turn" and is durable; changing it breaks the peer-to-lead escalation it was built for.
2. **Always interrupt instead of steering.** Rejected: it throws the in-flight tool and the model's reasoning away (the probe shows the sleep aborted and a billed error result).
3. **Use the SDK's `Interrupt` for `interrupt` on Claude.** Deferred (D4): two results per prompt need a reader change, and Stop-and-resume already works.
4. **Emulate steer on ACP (cancel, then send the text as a new prompt).** That is `interrupt`, not a steer; it is offered as `interrupt`, not under the steer name.

### Consequences

- A human or a Lead can redirect a running Claude or Codex agent without losing its turn; the cost is a new event type, one mode and two catalog flags.
- The Claude injection point is the CLI's, observed on one version; a regression there degrades to "the steer lands later", never to a lost message (rule 2).
- ADR-0021 decision 2's text and the "impossible" bullets are superseded only for providers that report `supportsSteer`.

### What we are NOT doing

- A steer on ACP; steer with attachments or setting changes; letting an agent steer an agent; making `steer` the default; persisting an in-flight steer across a daemon restart as
  anything other than the queue item of rule 2.

### Decisions for the Human (steering)

| # | Decision | Recommendation |
|---|---|---|
| D1 | Add **`steer` as a fourth `whenBusy` mode**, leaving `queue` and `interrupt` as they are | **Yes** |
| D2 | When the provider cannot steer (ACP): **degrade to `queue`** and say so in the result, rather than reject | **Degrade to queue** |
| D3 | Who may steer: human and orchestrator yes; **agent-source and remote callers downgraded to `queue`** | **Accept** |
| D4 | Claude `interrupt` stays **Stop-and-resume** (not the SDK `Interrupt`) for now | **Keep** |
| D5 | Codex `interrupt` uses **`turn/interrupt`** (keeps the thread warm) instead of Stop | **Yes, after a Codex probe (D8)** |
| D6 | ACP `session/cancel` then a new prompt as a cheaper `interrupt` than closing the agent | **Defer** (separate ADR/PR once an ACP agent is probed) |
| D7 | Composer: a separate **Steer** button; Enter stays queue, the alternate shortcut stays interrupt; the button is absent when unsupported | **Accept** |
| D8 | One **Codex app-server probe** (needs a Codex login; asks first, a few cents) to confirm `turn/steer`/`turn/interrupt` and the failure shape | **Yes, if a login exists; ask before running** |
| D9 | One more **Haiku probe** (cents, existing login, pinned model, no daemon): steer while the model streams text with no tool, and during a permission wait | **Yes** |
| D10 | Add `--replay-user-messages` to Claude runs so steer ordering is exact | **Yes** |
