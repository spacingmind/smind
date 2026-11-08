# 0056: Who is "the human" on the daemon socket — revisiting the rule that an unidentified local connection counts as the human

## Status

**Proposed — 2026-11-09.** Narrows [ADR-0017](0017-mcp-server.md) (an orchestrating agent can never answer) and [ADR-0019](0019-provider-native-permission-modes.md) (smind never
decides a request is safe) at the one place they depend on an assumption this record questions. Builds on the floor rule and CLI refusal of
`docs/plans/active/agent-self-approve-hardening.md`, which narrow the hole without closing it. Every choice under [Decisions for the Human](#decisions-for-the-human) carries a
recommendation; the Supervisor accepts the recommendation under the Human's delegation, and the Human may veto. No code is written before that.

## Context

The daemon decides who is calling from the connection, not from the person:

- A `/ws` connection that presents the daemon token is **local**. The token is a file in the home directory, readable by every process of the same OS user.
- A local connection that sends `session.identify {runId}` becomes **run-scoped**: it may call only `RunScopedMethods`, and every human-only handler refuses it (`refuseAgent`,
  `refuseRunScoped`). `smind mcp serve` identifies itself this way from `SMIND_RUN_ID`, which only the MCP entry's environment carries.
- A local connection that does **not** identify is treated as the human. The CLI does this for every command except `mcp serve`.
- A relay-served session (a paired device) is not local, and the web app over a password session is a different, authenticated principal.

An agent with a shell runs as the same OS user, can read the daemon token, and can run the CLI or open the socket itself without identifying. So it can call
`run.respondPermission`, `run.answerQuestion`, `inbox.resolve`, host and device pairing and the heredoc reveals as the human: approve its own request, answer its own
question, pair a device. The hardening PR adds an agent-run marker the CLI honours and a compiled-in policy floor rule over the shell command text. Both read what the agent
does in its shell; neither can see a script written earlier, a name assembled at run time, another language's socket client, or a run that needs no permission. They are
defence in depth. The principal itself still says "no identify = human".

The CLI is also the agent's tool (`smind task new|send|wait|logs`), so any secret the CLI holds, an agent that can run the CLI obtains. That rules out a secret handed "to the CLI
and the app" as the distinguishing factor; something the agent cannot do has to be involved.

## Options considered

1. **A per-boot human proof.** Human-only RPCs on a local connection need an assertion the daemon mints only after a user-presence check, short-lived and bound to the connection:
   from the CLI, the daemon password typed on the controlling terminal (`/dev/tty`, never stdin, argv or env; `smind set-password` already stores the hash); from the app, its
   authenticated session. A bare token file is not enough, and neither is a per-boot token readable by the CLI binary (the agent runs the binary). Strong, but it needs a password
   (many local-only installs have none), and a pseudo-terminal lets an agent drive the prompt, though not know the answer.
2. **Confirmation on a second channel.** A human-only call that arrives on an unproven local connection while a run of the same task is live is held pending, and the daemon
   raises a confirmation in a channel the agent does not reach (the app, the mobile app or the inbox, which are authenticated sessions or paired devices). The call completes
   only when a human confirms there. No new secret, builds on the inbox of ADR-0045, and works with no password. It adds a step for a person at a terminal, and it needs a second
   device or the web app open.
3. **Agent runs without socket access.** Run the agent where it cannot reach the socket or read the token (a provider sandbox, a network namespace, a different OS user).
   The only option that closes the hole by construction. But the agent needs the socket for orchestration (`smind mcp serve` connects to it), the providers sandbox
   differently or not at all, and a different OS user breaks the shared worktrees and caches. It is a per-profile hardening, not a default.

## Recommendation

Take **2 as the default and 1 as the stronger mode where a password exists**, and leave **3** as an opt-in profile setting for later.

- Default (option 2): `run.respondPermission`, `run.answerQuestion`, `inbox.resolve` and pairing from an unproven local connection, while a run of the same task (or any run, for
  pairing) is live, return `confirmation_required` with a pending confirmation id; the approve/answer card appears in the app or inbox, and the call is released by the human
  there. With no live run of that task the call goes through as today (a person at a terminal approving a stopped task has nothing to defend against).
- Stronger mode (option 1), switched on by a setting and available only when a password is set: the same calls need the proof and never fall back to confirmation.
- Option 3 is recorded, not built: provider sandboxes differ too much to promise it, and orchestration needs the socket.
- The principal rule changes in one place (the handler wrapper that already calls `refuseAgent`), with one table of human-only methods shared with the CLI refusal and the floor rule.

## Decisions for the Human

- **D1: which mode is the default.** Recommendation: option 2 (confirmation), option 1 as an opt-in stronger mode. Veto changes the PR that builds the default.
- **D2: whether pairing is gated the same way.** Recommendation: yes, always confirmed (pairing a device is a standing grant, and an agent has no reason to do it).
- **D3: whether option 3 becomes a profile setting.** Recommendation: defer; record it in the plan, build nothing.
- **D4: what a stopped task does.** Recommendation: no confirmation when no run of that task is live, so the terminal workflow keeps working.

## Consequences

- The hole closes for the cases the floor rule cannot see, at the price of a confirmation step for CLI approvals while a run is live. Users who approve from the app or mobile
  see no change.
- A confirmation is itself a human-only action, shown only on authenticated channels; an agent that spams confirmations gets none through, and the card says which task asked.
- Nothing here lets an agent approve: if the confirmation channel is unreachable the call stays pending and times out as denied (the 5-minute rule of ADR-0047).
- The principal rule, the CLI refusal and the floor rule stay as defence in depth; none is removed.

## What we are NOT doing

- Not sandboxing agents (option 3) in this record.
- Not making the daemon password mandatory.
- Not changing which methods an identified run-scoped connection may call.
