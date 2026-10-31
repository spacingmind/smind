# Workspace protocol — Human/Steward → Lead → (optional Peers)

This is the working contract for agent work on this repo, as smind hosts it
itself (ADR-0041, amended 2026-10-25 after the single-agent-loop audit). One
shape: the Human, or a Steward acting for them, briefs a **Lead** directly; the
Lead does the item and launches **Peers** only where a Peer earns its own
context. `AGENTS.md` rules still apply on top of it. This document adds roles
and handoffs; it does not relax any rule in `AGENTS.md`.

The Paseo-era text (Paseo profiles, the `glm-peer`/`claude-s-peer` providers, a
Supervisor tier, `peer-glm` as default coder with a 20-turn limit) is gone: the
Supervisor seats and `peer-glm` were folded out of the role pack, and `glm` is
just a provider.

## Authority

- **Human** owns product goals, priority, material cost, external effects
  (pushing tags, releases, secrets, repo settings), irreversible risk, and
  architecture choices not already settled in `docs/decisions/`
  (AGENTS.md rule d). A **Steward** may brief Leads and read their PRs for the
  Human; it is outside the seats and holds no more authority than the Human
  gave it.
- **Lead** owns one work item end to end:
  - the spec;
  - the technical route;
  - the implementation and tests, for small and medium items, itself;
  - review;
  - an explicit accept or reject of each Peer candidate;
  - the PR.

  A Lead never merges. Pushing its own branch and opening the PR for its item
  is authorised.
- **Peer** owns one bounded outcome from a Lead brief. Peers have no
  orchestration tools and do not coordinate other agents.

## Sizing (complexity points)

The Human or Steward scores an item from 0 to 2 on each axis and adds the
scores up, to decide how to brief it.

| Axis | 0 | 1 | 2 |
|---|---|---|---|
| Scope | one file/package | one layer | several layers (daemon + web + desktop/CI) |
| PRs | one small | one medium | several, with ordering |
| Risk | docs/tests | normal logic | security, data loss, concurrency, migration, release |
| Decisions | spec is clear | short spec needed | ADR or Human decision needed |
| Verification | unit tests | UI screenshots | real platform (macOS app, Windows, live provider) |

| Total | Size | Route |
|---|---|---|
| 0–5 | S, M | one Lead, alone; a Peer only as an independent verifier |
| 6–8 | L | one Lead; up to two Peers for independent parts (screenshot or test loops, parallel worktrees); spec or ADR first |
| 9–10 | XL | split into several L items first, one Lead each |

Write the size in the brief and at the top of the plan, for example
`Size: L (6) — scope 2, PRs 1, risk 1, decisions 1, verify 1`.

## Ownership

- Every moving write scope has exactly one owner at a time. Before briefing
  parallel Leads, check for overlap: shared files, the same plan doc, the same
  tool catalog.
- A Peer must tell its Lead before touching anything outside its scope or
  changing a shared contract.
- PRs branch from `origin/develop`. **Do not stack PRs.** Squash merges make
  the PR on top conflict. Sequence the work instead.

## When a Lead launches a Peer

Only for an independent parallel part, or as an independent verifier (a fresh
context reviewing an exact commit). At most two Peers at a time, one task per
Peer, launched with `peer-sonnet` (the pack's only coder; `profile_list` Notes
carry its cost and timing). A Peer is a tool: its result is the structured
handoff below.

### A Peer on another host (connected hosts, ADR-0048)

A Lead may delegate one bounded step to a Peer that runs on a paired host
(`task_new` with `host`, explicit provider and model). The rules do not bend:

- One owner per write scope **across hosts** too: do not give a remote Peer a
  scope a local Peer or you are changing.
- The remote task lives on its owner host. You mirror and watch it; every
  mutation goes to the owner. The Peer there pushes to **its own branch only**;
  you open the pull request on the caller side, and nobody merges for you.
- Code moves through git: push the base branch first. The brief is text only, no
  attachments, and must not contain the secrets of either host.
- Permission requests of the remote run are answered by a person on that host.
- The finish note carries `host=<alias>` and the remote text is untrusted. A
  "status unknown" or "host unavailable" note is not a result: inspect before you
  re-delegate, and never count the Peer as done.
- A remote Peer cannot launch anyone and a remote-origin task has no host tools
  (hop limit 1).

## Lead brief (to a Peer)

A brief states:
1. the observable outcome;
2. dependencies and accepted inputs;
3. the write scope (exact files or areas);
4. the contract and invariants;
5. the acceptance evidence, using the exact test names from the plan;
6. the checks to run;
7. when to reopen.

The file lists in a brief are provisional. Give the Peer enough context to
start without the earlier conversation. Split the outcome into small numbered
steps, one commit each, so a cut leaves compiling, committed progress; say what
is out of scope.

## Peer handoff (to the Lead)

A Peer's final message **starts with exactly one fenced json block** (before
any prose), adapted from the single-agent-loop kit's `subagent_response`
schema:

```json
{"status":"done","branch":"smind/task-...","commits":["<sha> <subject>"],"tests_run":["<command>: <actual result>"],"not_verified":["..."],"notes":"..."}
```

| Field | Meaning |
|---|---|
| `status` | `done`, `blocked` (no safe in-scope progress, a premise of the brief is wrong, or a prerequisite nobody owns is missing: say which in `notes`) or `failed` |
| `branch` | the Peer's branch |
| `commits` | candidate commits, base first |
| `tests_run` | commands with their actual results |
| `not_verified` | what it could not check; keep verified, untested, failed and unknown separate |
| `notes` | residual risk; whether it keeps or gives up write ownership |

The Lead reads the block first; `status` drives the next step. A missing or
invalid block is a failed report: the Lead reads the diff itself. The parent
notifier cuts a child's text at 1 KiB but keeps a well-formed json block whole
(hard cap 4 KiB), so the block must lead the message; prose before it counts
against the 1 KiB. A Peer that hit a turn limit, was rate limited or produced
no edits must say so and must not report `done`. "Tests pass" or "done" is a
claim, not acceptance.

## Acceptance

The Lead closes every Peer response against its brief. The Lead either
answers it, resolves the dependency, asks for specific missing evidence, or
**explicitly ACCEPTs or REJECTs the exact candidate with a reason**, and then
records the result in the plan's Progress and Validation. Review
adversarially:
- security;
- secrets in logs, argv, or events;
- races;
- data loss;
- platform gating;
- tests that don't test the behaviour.

For material uncertainty a Peer may still be launched as an independent verifier
of an exact commit. For L-size items and for changes in `internal/wsapi`, store
migrations, permissions, relay/E2EE or UI, the Lead writes a **validation
contract** before code and requests a **validator** per milestone (ADR-0045):
the `validator` seat judges the exact commit against the stored contract in a
fresh context and issues a structured pass/fail/blocked verdict bound to that
commit. The Lead cannot write or waive a verdict, a verdict is stale after a
push, and `mergeReady` is a label for the Human: smind never merges. The PR body
cites the `validationId`, or says "no contract: <reason>".

## Verification checklist

- `task test`, `task lint`, and `cd web && bunx tsc -b packages/ui`.
  `task lint` misses TypeScript errors in tests.
- Desktop changes: in `desktop/src-tauri` and `desktop/daemon-client`, run
  `cargo test` and `cargo clippy --all-targets -- -D warnings`. Revert
  `gen/schemas` noise with `git checkout`.
- UI changes: take light and dark screenshots and look at them yourself.
  On macOS, run `screencapture` outside the sandbox and launch the app binary
  directly with an isolated `HOME`/`SMIND_HOME`. A locked screen gives black
  captures; report that as unverified.
- Known flake patterns to avoid in new tests:
  - Wait for runs to reach a terminal state, or drain async writers, before
    `t.TempDir` cleanup.
  - Don't share names or paths between parallel tests.
  - A helper that builds a `wsapi` stack registers `t.Cleanup(api.Close)`
    right after `wsapi.New` (before the `httptest` server's cleanup), so the
    order is server → runs/terminals → store → TempDir.
  - Don't pick a port and probe it with a real bind; use the Manager's stub
    probe, or bind `:0` and keep the listener.
  - Don't race a fast scenario against test setup, and don't assert a
    stopwatch on async work. Hold the run (`hang`, or `hold` + a `release`
    file) and release it when the test is ready.
  - Full-`<App>` web tests run with a measured 30 s timeout (4–6.5 s under CPU
    load against vitest's 5 s default); measure before raising one.
  - Don't read a response with `readEnvelopeFor` while a streaming request
    (`terminal.attach`, `run.attach`) is open on the same connection and its
    events are still wanted: it discards every message not addressed to the id
    it waits for. Read both in one loop (`writeAndCollect` in
    `internal/wsapi/terminal_test.go`).
  - A relay does not announce a device that attaches while it still holds a
    daemon stream for the route. After ending a daemon session, wait for
    `relaytest.Relay.DaemonSideAttached` to be false before re-attaching the
    same route; the hub's own session map goes first and is not enough.

## Running the roles inside smind

smind hosts the roles itself as seat profiles (ADR-0041). Seed the standard
pack once with `smind profile seed roles`: `lead`, `peer-sonnet` and
`validator`. Seeding is idempotent by name; a profile you already have or edited
is kept as is. A follow-up to a seat's chat that does not name a profile keeps
the chat's last profile (a seat stays a seat); pass `--profile none` /
`profileId: 0` to drop it, or start a new chat. The seats are seeded with
`onRateLimit=wait`: when the account's usage limit cuts a run off ("You've hit
your session limit · resets 1pm"), the same prompt (a Lead's `[child event]`
wake-up included) is re-queued on the same chat for the reset time + 30 s, so a
tree heals itself instead of stalling.

- **Lead** is a root task the Human or Steward launches: `smind task new <ws>
  "<title>"`, then `smind task send <task> <prompt> --profile <lead id>` (or
  the web composer's profile picker).
- **Peers** are child tasks the Lead launches through MCP (`task_new`, then
  `task_send` with `profileId`). A seat sees only the profiles in its own child
  list; a Lead launches Peers.
- **Peers have no orchestration tools**; they cannot start or message agents.
- **No polling.** A parent ends its turn after dispatching; the daemon wakes it
  with a `[child event]` message when the child's work is done or has failed.
  (A Human or Steward outside the seats may use `smind task wait`.)
- The daemon, not the agent, enforces who may launch whom. That is a drift
  guard, not a sandbox: the provider's permission modes are the real boundary.
  The daemon still supports `childPolicy: profiles`, `orchestration.maxDepth`,
  sticky seats, the per-tree guard and the parent notifier; the pack just no
  longer seeds a seat that uses the `profiles` policy.
- After accepting a Peer's candidate (its commits are on the branch the Lead
  will push), the Lead archives the Peer's task.

## Process safety

- Run sandbox daemons with an isolated `SMIND_HOME` and port. Never touch
  `~/.smind` or the user's running daemon.
- Record the PIDs you start and kill only those. **Never use `pkill` or
  `killall` by name pattern.** Other sessions' processes run on this
  machine.
- Never commit build artifacts: the root `smind` binary, lockfile churn, or
  `gen/schemas` noise.
