# Workspace protocol — Supervisor → Lead → Peer

This is the working contract for multi-agent work on this repo. It applies
to agents launched through Paseo profiles (`supervisor`, `lead`, `peer-glm`,
`peer-sonnet`, `reviewer`) and later to smind's own orchestration. `AGENTS.md`
rules still apply on top of it. This document adds roles and handoffs; it
does not relax any rule in `AGENTS.md`.

## Authority

- **Human** owns product goals, priority, material cost, external effects
  (pushing tags, releases, secrets, repo settings), irreversible risk, and
  architecture choices not already settled in `docs/decisions/`
  (AGENTS.md rule d).
- **Supervisor** (human-launched only):
  - routes Human intent and sizes incoming work;
  - spawns Leads, assigning each a non-overlapping write scope;
  - does S-sized work itself;
  - runs the merge queue only when Human has authorized merging;
  - reports to Human.

  Supervisor is not a second Lead: it intervenes only on concrete drift,
  overlap, or a missed checkpoint.
- **Lead** owns one work item end to end:
  - the spec;
  - the technical route;
  - delegation to Peers;
  - review;
  - an explicit accept or reject of each Peer candidate;
  - the PR.

  A Lead never merges.
- **Peer** owns one bounded outcome from a Lead brief. Peers have no
  orchestration tools and do not coordinate other agents. The `glm-peer` and
  `claude-s-peer` providers have Paseo tools disabled.

## Sizing (complexity points)

Supervisor scores each item from 0 to 2 on each axis and adds the scores up.

| Axis | 0 | 1 | 2 |
|---|---|---|---|
| Scope | one file/package | one layer | several layers (daemon + web + desktop/CI) |
| PRs | one small | one medium | several, with ordering |
| Risk | docs/tests | normal logic | security, data loss, concurrency, migration, release |
| Decisions | spec is clear | short spec needed | ADR or Human decision needed |
| Verification | unit tests | UI screenshots | real platform (macOS app, Windows, live provider) |

The total decides the route:

| Total | Size | Route |
|---|---|---|
| 0–2 | S | Supervisor does it, or one Peer directly |
| 3–5 | M | one Lead and one Peer |
| 6–8 | L | one Lead and up to two Peers; spec or ADR first; split write scopes |
| 9–10 | XL | split into several L items first, one Lead each |

Write the size in the Lead brief and at the top of the plan, for example
`Size: L (6) — scope 2, PRs 1, risk 1, decisions 1, verify 1`.

## Ownership

- Every moving write scope has exactly one owner at a time. Before spawning
  parallel Leads, Supervisor checks for overlap: shared files, the same plan
  doc, the same tool catalog.
- A Peer must tell its Lead before touching anything outside its scope or
  changing a shared contract.
- PRs branch from `origin/develop`. **Do not stack PRs.** Squash merges make
  the PR on top conflict. Sequence the work instead.

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
start without the earlier conversation.

## Peer handoff (to the Lead)

A writable Peer returns:
- the candidate commit SHA and its base;
- the complete list of changed paths;
- the verification environment, the commands it ran, and their actual
  results;
- residual risk;
- whether it keeps or gives up write ownership.

Keep verified, untested, failed, and unknown items separate. "Tests pass"
or "done" is evidence, not acceptance.

If the Peer can't hand off a candidate, it returns one of these signals,
each with evidence, the consequence, and the decision it needs:
- `REOPEN_REQUEST`: a premise of the brief is wrong.
- `DEPENDENCY_REQUEST`: it needs a prerequisite that no one owns.
- `BLOCKED`: no safe in-scope progress remains.

A Peer that hit a turn limit or produced no edits must say so. It must not
report "done".

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

For material uncertainty, the Lead may ask the `reviewer` profile for a
read-only review of an exact commit.

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

## Process safety

- Run sandbox daemons with an isolated `SMIND_HOME` and port. Never touch
  `~/.smind` or the user's running daemon.
- Record the PIDs you start and kill only those. **Never use `pkill` or
  `killall` by name pattern.** Other sessions' processes run on this
  machine.
- Never commit build artifacts: the root `smind` binary, lockfile churn, or
  `gen/schemas` noise.
