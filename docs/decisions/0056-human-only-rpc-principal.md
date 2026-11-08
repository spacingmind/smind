# 0056: Who is "the human" on the daemon socket — revisiting the rule that an unidentified local connection counts as the human

## Status

**Proposed — 2026-11-09. Not accepted: the Human decides D1-D4 (a default must not be assumed).** Narrows [ADR-0017](0017-mcp-server.md) (an orchestrating agent can never answer) and
[ADR-0019](0019-provider-native-permission-modes.md) (smind never decides a request is safe) at the one place they depend on an assumption this record questions. Builds on the floor rule
and CLI refusal of `docs/plans/active/agent-self-approve-hardening.md`, which narrow the hole without closing it. The first draft recommended a default; a review found that default
breaks the unattended workflow of external orchestrators (see [Automation principals](#automation-principals-and-why-they-are-not-told-apart-from-an-agent)), so this revision
presents the analysis and leaves the decisions open. Nothing is built for this record.

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

## Automation principals, and why they are not told apart from an agent

A pending permission or question exists only while its run is live. So under the first draft's rule ("while a run of the same task is live") **every** CLI approve, deny or answer needs a
second-channel confirmation, and under the password-proof option no automation can type a password on a terminal. That ends unattended approval by anything outside the daemon: an
orchestrator's approve loop, a supervisor's scripts, a person's own wrapper.

The root cause is not the option, it is the principal. An external orchestrator and an agent run **as the same OS user**, hold the same daemon token and call the same socket. Whatever
lets the orchestrator approve also lets the agent approve, unless the agent is kept away from what the orchestrator holds: a token file, a socket path, the CLI. There are only two ways to
separate them:

1. **Isolate the agent** (sandbox it away from the socket, the loopback port and the token file) and give the automation a credential the agent cannot read.
2. **Prove a human, per action** (a second channel or a typed password), which no unattended automation can supply.

A credential the agent can read is not a principal, it is a shared secret: marking it "automation" changes a label, not who can use it. So an "automation principal" is only meaningful for
runs whose agent is isolated; for any other run it adds nothing over the unidentified local connection.

## Option 3 re-weighed, per provider (documents and source read, no probes)

What each provider's own sandbox can block for the daemon's socket, loopback port and token file, from the vendored reference sources and the SDK types:

| Provider | What is sandboxed | Daemon unix socket / loopback port / token file | Costs and gaps |
|---|---|---|---|
| **Claude Code** | Only the Bash tool's commands (macOS and Linux; not Windows), when `sandbox.enabled` | The sandbox's network section has `allowedDomains`, `allowUnixSockets`, `allowAllUnixSockets`, `allowLocalBinding` (macOS), proxy ports; the filesystem section has read and write deny lists. With none allowed, a sandboxed command can reach neither a socket path nor a loopback port, and `denyRead` on the smind home hides the token file | MCP servers (the `smind mcp serve` entry) are not Bash commands, so orchestration keeps working. `allowUnsandboxedCommands` must be false, or the tool can retry outside the sandbox; `excludedCommands` (often `git`, `docker`) run outside it. Network goes through a domain allowlist, so `git push`, package installs and `gh` need entries |
| **Codex** | The shell commands of a turn, per sandbox mode | In `workspace-write` (smind's `auto`) network is off unless enabled, and the macOS profile's unix-socket policy defaults to an empty allowlist (`allow_unix_sockets`, `dangerously_allow_all_unix_sockets`). In `danger-full-access` (smind's full-access mode) nothing is sandboxed | Linux enforcement was not read here, only the macOS profile. Network-off blocks fetches the agent legitimately needs, unless enabled per run |
| **ACP agents** (GLM, OpenCode, Cursor, Kimi, ...) | Nothing: the protocol has no sandbox concept; each agent decides, and its shell runs as the agent process | Not blockable by smind unless it wraps the agent's whole process in an OS sandbox, which would also confine the `smind mcp serve` child that needs the socket | No per-provider switch to rely on; a wrapper is new work and not portable to Windows |

So option 3 is **real for Claude Code on macOS and Linux and for Codex in workspace-write, absent for ACP agents and for any run in a full-access or bypass mode.** It is also not free:
it constrains what agents do (network), and it protects only the commands the provider sandboxes, not the agent process itself.

## Hybrid to consider: isolate where the provider can, confirm where it cannot

- The daemon records an **isolation level** per run: `sandboxed` when smind itself launched the run with the provider sandbox on and the daemon socket, loopback port and token path denied
  (smind writes the settings, as it already does for the git and secret rules of claude-native), `open` otherwise (ACP, full-access, bypass, an unsandboxed Claude).
- An **automation principal** is a credential smind issues separately from the daemon token, stored where a sandboxed agent cannot read it (outside the smind home, or owned by another
  OS user), and usable only for answer, approve and deny (never pairing, policy or reveal). A paired relay device is an existing non-local principal that could play the same role.
- For a `sandboxed` run, an approval by the automation principal is accepted directly: the agent cannot hold the credential or reach the socket, so the principal really is the orchestrator.
- For an `open` run, the agent could read the credential, so the daemon does not honour it: an approval from any unproven connection for that run needs the second-channel confirmation (option 2)
  or the terminal password proof (option 1, where a password is set). External automation can therefore not approve `open` runs unattended. That is the price of the guarantee, not a
  bug to design away.
- **Effect on external orchestration:** unchanged for sandboxed Claude and Codex runs (the orchestrator uses its principal and approves as today); for ACP agents and full-access or bypass runs
  an orchestrator can still see and report the pending request but a human confirms it, or the Human explicitly accepts the same-OS-user trust for those runs (below).

## What this record can and cannot promise

The permission gate is a drift guard against an agent's mistakes and injected instructions, not a boundary against an agent that is deliberately hostile and runs as the same user: such an agent
can already run any code the user can. The hardening already merged (the CLI refusal and the `no-self-approve` floor rule) removes the easy paths. Closing the rest costs either isolation of the
agent (option 3, partial by provider) or a human in the loop per action (options 1 and 2). The Human chooses where to stand; the record does not choose for them.

## Decisions for the Human (all open: the Human decides in the morning, with the analysis above, not a default)

- **D1: the principal rule.** Choices: (a) keep "unidentified local connection = human" and rely on the hardening already merged; (b) the hybrid above (isolate where possible, confirm elsewhere);
  (c) options 1 or 2 for every run. Trade-off: (a) keeps every unattended workflow and leaves the same-OS-user hole open; (b) keeps unattended approval for sandboxed runs and needs the sandbox work and an
  isolation-level field; (c) closes the hole for everyone at the cost of all external automation.
- **D2: whether pairing a host or device is gated the same way.** Pairing is a standing grant. Choices: always confirmed on a second channel, or follow D1. An agent has no reason to pair.
- **D3: whether smind turns the provider sandbox on for runs** (per profile, off by default, with a network allowlist and a way to opt a command out) as the groundwork for D1(b), or leaves sandboxing
  entirely to the user's provider settings.
- **D4: what an `open` run does when no human is reachable** (the confirmation times out as denied, the 5-minute rule of ADR-0047): keep that, or let an explicit per-profile setting accept
  the same-OS-user trust for that profile's runs.

## Consequences

- Nothing changes until the Human decides; the merged hardening stays as defence in depth in every case.
- Under (b), `runs` gains an isolation-level field, a principal table and a credential file; a confirmation is itself a human-only action shown only on authenticated channels.
- Under (c), unattended external approval ends for every provider.

## What we are NOT doing

- Not building anything for this record: no sandbox switch, principal or confirmation until the Human decides.
- Not making the daemon password mandatory.
- Not changing which methods an identified run-scoped connection may call.
