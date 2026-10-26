# 0045: Validator seat and Human inbox — a fresh-context verdict against a contract, and one place for what needs a person

## Status

**Accepted — 2026-10-27** (Human pre-approved the recommendations for a 12-hour window on 2026-10-27 01:10; Supervisor applied them, see [Human answers](#human-answers-2026-10-27)). Proposed the same day. Spec and PR split (written after acceptance): `docs/plans/active/validator-inbox.md`.

Inputs: ADR-0041 (seats, role pack, tree guard, tree budget A5, the single-loop amendment: the `reviewer` seat "unchanged, Human decides later"), ADR-0019
(provider-native permission modes, no smind allowlist, human-authored configuration), ADR-0017 (a seat can never answer a permission), ADR-0040 (push kinds),
ADR-0042 (plugins: human-only pattern, skills contribution), ADR-0044 (automations: the "never auto-approve" and audit ideas; its Context says judging the
quality of an automation's work is "the validator/inbox lane, not this ADR", so this is that lane), `docs/plans/active/single-loop/PLAN.md` (item 4: the
`reviewer` has no observed runs), `docs/plans/completed/stalled-tree-alert.md`, and local-only research (`docs/research/local/factory-docs.md` sections 1.3, 2, 9.2:
Factory's Missions run a validator with fresh context per milestone against success criteria fixed before code, but **document no contract format**, so ours
below is our own; `docs/research/local/dogfood-2026-10-22.md`, the Steward's notes).

## Context

### The problem: the author reviews itself

The Lead (the single loop, ADR-0041 amendment) writes the spec, the code, the tests and the review. Its adversarial review is done with the context it built
while writing: it knows what the code is meant to do, so it reads what it meant, not what it wrote. A reviewer that is the author's own context is the
rubber-stamp risk the protocol already names ("done is a claim, not evidence"). What exists to counter it today:

- the Lead's own pass ("attack it", `smind-leader`), which shares the author's context;
- a `reviewer` seat (`internal/profiles/rolepack.go`): claude-native Sonnet, `plan` mode, no orchestration tools, read-only, instructions "review the exact commit
  adversarially". `plan` mode cannot run a test, a mutation or a screenshot, so it can only read; the single-loop audit found **no run of it** (inferred) and left
  "does it stay" to the Human;
- the Steward (outside the seats) re-reading each PR, which is the only independent pass that actually happens, and it does not scale.

### Evidence from this repo's dogfood (Steward notes, local-only `dogfood-2026-10-22.md`; entry numbers there)

What independent review found that the author's context did not:

- **86** the Lead's plan promised to check `permissions.ask` against the real CLI and left it "not verified"; the Steward ran it and found `--permission-mode auto`
  pushed silently without the rule. That became #596. The gap was invisible to the author because the author had marked it "not verified" and moved on.
- **83** (#593) the Peer needed five rounds, "every one found by looking at real-app frames, none by unit tests"; **61** copy inherited from another context passed
  every test and was caught only by a screenshot; **88** (#597) a title squeezed to ~70 px, a defect the Lead had flagged and left to the Steward.
- **79/81/82** adversarial reviews by the Steward found real residuals (TOCTOU between a freshness check and a merge, a fork marker the agent can edit, a
  consent-surface mismatch); **79** and **93** record the cheapest honest check of a test: one mutation, and the test must fail.
- Memory of an earlier round: an adversarial code review "caught 4 real bugs before merge" (daemon-restart-resync).

What goes wrong without a contract fixed up front:

- the review checks what the author thought of, not what the spec asked (nothing binds the checks to the ACs);
- evidence is lost: **84** frames lived in an archived Lead's worktree and the paths in the PR body were dead; **86/87** survived only because the Lead wrote to a
  stable `/tmp` path;
- "verified" and "not verified" are prose in a PR body: a reader cannot tell which AC has evidence and which was skipped.

### What the daemon already has to build on

- **Seats are profiles** with a small contract (ADR-0041 A1), launched by the daemon with a run identity (`session.identify`, `RunScopedMethods` in
  `internal/wsapi/session.go`), admission by `childPolicy`, a per-tree concurrency guard, a tree budget (`tasks.budget_usd`, admission only), `onRateLimit=wait`,
  and a parent notifier that wakes the Lead with a `[child event]`.
- **The Peer handoff** (`docs/WORKSPACE_PROTOCOL.md`): a fenced json `{status, branch, commits, tests_run, not_verified, notes}`. It is the author's claim.
- **Human-only is a pattern** (`humanLocalOnly`, `refuseRunScoped`; ADR-0019 #6, ADR-0042, ADR-0044 §5): configuration or decisions that must not be taken by an
  agent are refused for run-scoped callers; relay sessions are refused for the ones that arm something.
- **A permission** is a live request in the runner, announced by `permission.pending`, answered through `run.respondPermission` (the guarded RPC), retired by
  `permission.resolved`. The web derives a per-task attention dot (`use-task-attention.ts`: `error`, `finished`, `permission`); mobile has `taskAttention.ts`.
  Nothing lists "everything waiting for me" in one place; a person finds a permission by noticing a dot in the sidebar tree.
- **Stall alert**: `internal/stall` (`Detect`, `Episode`, a persisted per-seat marker so one episode alerts once), topic `task.stalled`; web notification and
  tree marker; mobile tree `stalled` field. It reports and never recovers.
- **Push** (ADR-0040): `permission`, `needs_continue`, `done`, `error`, filtered per device.

## Decision

Two things, one ADR because the second consumes the first: (A) a **validator**: a seat that decides "does this commit satisfy the contract" in a fresh context
and issues a structured **verdict** bound to a commit sha; (B) a **Human inbox**: one derived list of what needs a person (permissions, verdicts awaiting a
merge decision, stall alerts, later automation pauses). smind **never merges by itself** and nothing here adds an approving shortcut.

### 1. The validation contract

A **contract** is the per-milestone document the Lead writes **before implementation** and hands to the validator. A milestone is the Lead's own unit (a PR, or a
coherent slice of one); a plan with three PRs has three contracts. It states what "done" means as checkable items, not as prose.

**Format: a YAML front matter validated against a JSON Schema, then a free markdown body for humans.** Only the front matter binds; the body is **not** given
to the validator (anti-collusion, §4). Justification: the Lead is a model and writes YAML/markdown reliably; a person can read and review it in the PR; the same
schema is the daemon's validation, so a malformed contract is refused at submit with a model-readable error (field path + reason) and never reaches a validator.
YAML is parsed with a strict subset (no anchors, no tags, no merge keys, duplicate keys are an error), converted to JSON, then validated. Pure JSON was the
alternative (no parser ambiguity, but a Lead writes worse prose-in-JSON and a PR reader gets a wall of quotes): Question 1.

```yaml
v: 1
milestone: "G14 PR-A: daemon read model"          # ≤ 120 chars
base: origin/develop                              # ref the diff is taken against
planRef: docs/plans/active/validator-inbox.md#pr-a  # optional pointer, not read by the validator
acs:                                              # ≥ 1, ids unique, [A-Z][A-Z0-9-]{0,15}
  - id: AC1
    text: "A stale-sha verdict is not merge-ready."          # ≤ 400 chars
    covers: { tests: [TestMergeReady_StaleSha], frames: [] } # ids from the lists below
tests:                                            # named tests the validator must run and see pass
  - { name: TestMergeReady_StaleSha, run: "go test ./internal/validation -run '^TestMergeReady_StaleSha$' -race -count=1", timeoutSec: 120 }
mutations:                                        # one change, a named test must FAIL, then restore
  - { id: M1, file: internal/validation/verdict.go, change: "flip `sha != head` to `sha == head` in mergeReady", expectFail: [TestMergeReady_StaleSha] }
frames:                                           # what to look at, when the milestone has UI
  - { id: F1, surface: "web /inbox", state: "one permission, one verdict, one stall", viewport: [1280, 390], theme: [light, dark] }
commands:                                         # run once, exit status recorded (build, lint, whole suite)
  - { id: C1, run: "task test", timeoutSec: 900 }
writeScope: { paths: [] }                         # default: nothing outside the scratch worktree (§4)
evidenceDir: auto                                 # always auto: the daemon picks the stable path (§3)
limits: { budgetUsd: 1.5, maxMinutes: 30 }
```

Rules the daemon enforces **at submit** (`validation.contract.submit`, §2): size ≤ 64 KiB (front matter) and ≤ 24 KiB body; ≤ 40 ACs, ≤ 80 tests, ≤ 20
mutations, ≤ 24 frames, ≤ 20 commands; every AC is covered by at least one test, mutation or frame (an AC with none is rejected: "AC3 has no check"); every id
referenced exists; every mutation `file` is a relative path with no `..` that exists at `base`'s tree or is reported `new-file`; `expectFail` names tests in
`tests`; `writeScope.paths` are relative, no `..`, no absolute paths, no globs wider than a directory prefix; `limits.budgetUsd` ≤ the daemon ceiling
(`validation.maxBudgetUsd`, default 5); unknown fields are an error (a contract does not smuggle instructions in an extra key). `commands[].run` and `tests[].run`
are **text for the validator to run under its own permission mode**: the daemon never executes them and a contract cannot widen what the validator is allowed to
do (the mode decides, ADR-0019).

**Storage: an immutable DB row, authored from a file.** The Lead writes the contract as a file in its worktree (`docs/plans/active/<slug>.contract.md`, so it is
reviewable in the PR like the plan) and submits it; the daemon parses, validates and **stores a copy** in `validation_contracts(id, task_id, version,
sha256, body_json, created_at)`; the validator reads the stored copy through a read RPC, never the file, so a Lead that edits the file after submit changes
nothing. **Versioning:** a task has `version` 1, 2, …; a new submit is a new row (never an update); a verdict names the `{contractId, sha256}` it judged, and the
**current** contract of a task is the highest version. A verdict against a superseded version is stale exactly like a stale sha (§3). Why not "DB only, no
file": the Lead needs a reviewable artifact in the PR and a cheap way to author a long document; why not "file only": the file is mutable by the author after the
fact, which defeats the contract. Question 3.

The contract file is committed with the plan before the code, per AGENTS.md rule (c); the daemon also records the first submit time so "written before
implementation" is observable (the contract's `created_at` against the task's first commit on the branch; shown, not enforced).

### 2. RPCs and who may call them

| RPC | Caller | Effect |
|---|---|---|
| `validation.contract.submit {taskId, source: "file"|"inline", path?, body?}` | the task's own run (run-scoped, its own task) or the Human | parse, validate, store a new version; returns `{contractId, version, sha256}` or a field-path error |
| `validation.contract.get {contractId \| taskId}` | any connection; the validator's run (by run identity, only its own contract) | read; relay sessions included |
| `validation.request {taskId, contractId?, commitSha?}` | the task's own run, or the Human | creates the validator's task and run (§4), binds `{validationId, contractId, commitSha, runId}`; `commitSha` defaults to the task branch head (resolved by the daemon, never trusted from a Lead as free text beyond a full 40-hex that must be an ancestor of the branch head); refused when the tree is over budget for a run-scoped caller (it is an agent launch, ADR-0041 A5) |
| `validation.verdict.submit {validationId, token, verdict}` | **only the validator's run** (§3) | validate and store the verdict |
| `validation.list {taskId?, status?}` / `validation.get {id}` | any connection | read; `merge-ready` is computed on read (§3) |
| `validation.cancel {validationId}` | the Human, or the requesting Lead before the validator starts | stops the validator run; no verdict |

`validation.contract.submit` and `validation.request` are added to `RunScopedMethods` (the Lead needs them) with an extra check: the `taskId` must be the
caller's own task. `validation.verdict.submit` is **not** in `RunScopedMethods`' generic allow: it is accepted only with a one-time secret (§3). The MCP catalogue
gains `validation_contract_submit`, `validation_request`, `validation_get` for the Lead and `validation_contract_get`, `validation_verdict_submit` only in the
validator's own entry (§4). A test walks the MCP catalogue against the handler map, like `pluginMutating`.

### 3. The verdict

```json
{
  "v": 1,
  "contractId": 12, "contractSha256": "…", "commitSha": "<40 hex>",
  "status": "pass | fail | blocked",
  "blockedReason": "no_browser | no_toolchain | timeout | budget | flaky_unresolved | contract_unrunnable | scope_violation | crashed",
  "summary": "≤ 800 chars",
  "items": [
    { "kind": "test",     "id": "TestMergeReady_StaleSha", "status": "pass", "attempts": 1, "evidence": [{"type":"excerpt","text":"ok  internal/validation 0.31s","sha256":"…"}] },
    { "kind": "mutation", "id": "M1", "status": "pass", "evidence": [{"type":"excerpt","text":"--- FAIL: TestMergeReady_StaleSha"}], "restored": true },
    { "kind": "frame",    "id": "F1", "status": "pass", "evidence": [{"type":"file","path":"frames/F1-web-1280-dark.png","sha256":"…"}], "note": "≤ 400 chars" },
    { "kind": "command",  "id": "C1", "status": "pass", "evidence": [{"type":"excerpt","text":"exit 0"}] },
    { "kind": "ac",       "id": "AC1", "status": "pass", "via": ["TestMergeReady_StaleSha", "M1"] }
  ],
  "findings": [{ "severity": "P0|P1|P2|P3", "file": "…", "line": 0, "text": "≤ 400 chars" }],
  "usage": { "costUsd": 0.0, "minutes": 0 }
}
```

- **Per-item evidence.** Every `test`, `mutation`, `command` and `frame` item carries evidence: an `excerpt` (≤ 2 KiB of real tool output) or a `file` (an
  evidence file, hashed). Every AC has a roll-up item naming the checks it rests on. A frame's evidence is the screenshot path **and** a one-line observation:
  "I looked" without a stated observation is rejected.
- **Completeness rules at submit** (the daemon, not the model, decides what a `pass` is): `pass` requires an item for every contract test (status `pass`), every
  mutation (status `pass` meaning "the named test failed as expected and the file was restored"), every command, every frame, and every AC (status `pass`, all
  its checks `pass`); `fail` requires at least one failing item and a finding or excerpt saying why; `blocked` requires `blockedReason` and, in `summary`, what the
  validator lacks (the browser, a credential, a toolchain). A verdict that declares `pass` with a missing item is **rejected at submit** (the validator is told
  which item), not stored as pass. Evidence files are copied by the daemon into `SMIND_HOME/validation/<validationId>/` (stable, outside the worktree, so it
  survives archive, dogfood entry 84) and the verdict's paths are rewritten to it; no path may escape the validator's scratch worktree or the evidence dir.
- **The verdict binds a commit.** It stores `commitSha` and the contract's `sha256`. It is **current** only while the task branch head equals `commitSha` and the
  contract version is the task's highest. A push makes it **stale**: still shown, struck, never merge-ready. Re-validate to get a new one.
- **Who may issue it.** Only the validator's own run. The daemon creates the validator's run and a **one-time verdict token** (random 256-bit, stored hashed,
  bound to `{validationId, runId}`), passed **only** in that run's `smind` MCP entry env (as `SMIND_VERDICT_TOKEN`); `validation.verdict.submit` requires the
  token, the run identity and `runId == validation.runId`, is accepted once, and is refused after the run ends. **The Lead cannot write a verdict**: it has no
  token, the method is not in its allowed set, `validation.verdict.submit` is refused for a caller whose run identity is not the validation's run, and a test
  asserts each. **Honest limit** (same as ADR-0041 A3): identity and token are env vars; a same-user process can read another process's environment, so this is a
  drift guard against a *model* writing its own verdict, not a sandbox against a hostile same-user shell. The stronger form (a fail-closed run-scoped token,
  ADR-0027 §3.2) is the same deferred work; the verdict row records the validator run id and profile so a forged one is visible in the audit. The daemon itself may
  write one kind of verdict: `blocked` with `timeout`, `budget` or `crashed` when the validator run ends without submitting (never `pass`/`fail`).
- **What "merge-ready" means.** Computed on read, never stored: the latest verdict for the task is `pass`, its `commitSha` equals the branch head now, its
  contract is the current version, it carries no `scope_violation`, and (when a PR is known, ADR-0039) the PR head equals that sha. It is a **label for a
  human's decision** ("the contract is satisfied at this sha"), not an action: **smind never merges by itself**, there is no merge RPC and no merge button
  (ADR-0039's forge rules still forbid merge). A `fail` or `blocked` verdict is shown with its reason; neither is overridable by the Lead.

### 4. The seat

- **Profile `validator`** in the role pack, `profile seed roles` (idempotent by name, a Human's edits kept). claude-native `claude-sonnet-5-5`, `orchestrationTools`
  false for the generic tools but the daemon adds the **validator MCP entry** (`validation_contract_get`, `validation_verdict_submit`, nothing else: no
  `task_new`, no `task_send`, no profile or permission tools), `childPolicy: none`, `onRateLimit: wait`. It **replaces** the unused `reviewer` seed (§7 and
  Question 4); the reviewer's adversarial checklist moves into the validator's instructions and skill.
- **Fresh context, enforced by the daemon.** `validation.request` creates a **new task** (child of the Lead's task, `purpose = validation`, hidden from the
  ordinary "what is this Lead doing" view but visible in the tree with its tier) and a **new chat**; no chat history is inherited. The first prompt is
  **built by the daemon** from three things only: the stored contract's structured fields, the diff `base..commitSha` (or the pointer to it in the scratch
  worktree), and the fixed validator instructions. The Lead supplies **no free text** to it. **Anti-collusion rule:** the validator never sees the Lead's
  reasoning: not the Lead's chat, not the contract's markdown body, not the Peer handoffs' `notes`, not the PR description. It sees the repo (including the plan,
  ADRs and AGENTS.md: that is the shared, reviewable record) and may find that the diff does something the contract does not mention; that is a finding.
- **Scratch worktree, exact sha.** The task is created with `baseRef = commitSha` in a **detached scratch worktree**: it never has the Lead's branch checked
  out, so it cannot move the branch. Mutation checks edit files **in that worktree** and restore them (`git checkout -- file`; the verdict item says
  `restored: true`, and a post-check that the worktree's tracked tree equals `commitSha` is part of completeness). The Lead's worktree is never entered.
- **Tools and permission mode.** Read, search, run tests/build/lint, write inside the scratch worktree, write evidence under the evidence dir; anything else asks
  a human (the permission card, the inbox shows it). Mode: **`default` plus the scratch worktree** is the recommendation's weakest form and `auto` (the Peer's mode)
  its practical one; ADR-0019 says smind has no allowlist, so the boundary is the provider's mode plus the worktree, and the **always-ask** git rules (#596)
  already cover `git push`/`gh pr create`. Question 5 asks the Human to choose; the recommendation is `auto` in the scratch worktree with the daemon-side
  post-check: before and after the run the daemon records the Lead's worktree `HEAD` and `git status --porcelain` digest (read-only), and a change marks the
  verdict `scope_violation` (blocked, not merge-ready). `writeScope.paths` in the contract is for the rare milestone whose checks must write elsewhere in the
  scratch worktree (a generated file); everything outside it is reported, not prevented, on `auto` (honest limit: a shell can write anywhere the user can).
- **Cost control.** The validator is an agent launch under the **tree budget** (ADR-0041 A5): counted in the tree's cost, refused at admission when the tree is
  over budget for a run-scoped caller (a Human-requested validation is never refused). Per validation: `limits.budgetUsd` (default 1.5, ceiling
  `validation.maxBudgetUsd` 5; measured Sonnet steps are $0.3-0.9, a full contract with a build and a mutation loop is a few steps) and `limits.maxMinutes`
  (default 30, ceiling 120). The daemon stops the run at the wall clock (the tree budget does not kill a running run, ADR-0044 §6.5 makes the same point) and
  writes a `blocked: timeout` verdict. One validation at a time per task (single flight); a `fail` does not auto-retry.
- **Failure modes.**
  - *Validator times out or crashes:* `blocked` verdict written by the daemon (`timeout`/`crashed`), reason shown; the Lead may request again (a new
    validation, same contract), the Human may request one too.
  - *Rate limited:* `onRateLimit: wait` re-queues the same prompt at the reset (ADR-0041 B); the wall clock keeps running from the start of the first attempt
    only while a run is live, so a wait does not eat the budget (tested).
  - *Validator disagrees with the Lead:* the verdict stands. The Lead's remedies are to fix and re-request, or to submit a corrected contract (a new version,
    visible as such, the old verdict becoming stale) if the contract was wrong. The Lead cannot override, delete or edit a verdict. The Human sees both and decides;
    there is no waiver concept in v1 (Question 8).
  - *Flaky test:* a failing test is re-run **once** by the validator; fail-then-pass is recorded as `pass` with `attempts: 2` and a `flaky` flag shown in amber and
    in the PR summary; fail-twice is `fail`. A contract may mark a test `quarantine: true` (known flaky from `docs/WORKSPACE_PROTOCOL.md`'s list): it is run and
    reported, and its failure alone does not fail the verdict but it makes the verdict `pass` with an explicit "quarantined test failed" finding. More retries are
    not offered: three tries make flakiness invisible, the same rationale as ADR-0044's no-retry for lost signals.
- **Relation to the Peer handoff.** The Peer's json block is the **author's claim** and stays as is. The validator's final message also starts with a fenced json
  block `{status, validationId, verdict: pass|fail|blocked, evidenceDir}` so the existing parent notifier keeps it whole and the Lead is woken with a
  `[child event]`; the Lead reads the structured verdict through `validation_get`, not the prose. The Lead's final report cites `validationId` per milestone,
  next to its own "verified / not verified" list. A verdict does not replace the Lead's review of a Peer's diff: it replaces the Lead grading its **own** work.

### 5. The Human console (inbox), web and mobile

One place for what needs a person. **Derived, not stored**, with a small read-state table.

**Item kinds (v1) and where each comes from**

| Kind | Source (derived on read) | Key | Leaves the inbox when |
|---|---|---|---|
| `permission` | the runner's live pending requests (what `permission.pending` announces) | `perm:<requestId>` | answered or retired (`permission.resolved`); **cannot be resolved, snoozed or dismissed from the inbox**, only answered |
| `verdict` | `validations` whose verdict is `pass` and merge-ready ("awaiting a merge decision"), or `fail`/`blocked` ("needs a decision") for the latest validation of a task | `verdict:<validationId>` | superseded (stale sha, newer validation) automatically; or the Human marks it `resolved` |
| `stall` | the `internal/stall` persisted marker for a seat | `stall:<taskId>:<sinceUnix>` | the seat's subtree runs again (the marker clears) or the Human `resolved` it |
| `automation` (later, ADR-0044) | `automations.status = paused` with `pausedReason` | `automation:<id>:<pausedAt>` | the Human `resume`s it (ADR-0044) or `resolved`s the item |

- **Item model** (wire): `{key, kind, taskId?, title, summary (≤ 160 chars, redacted like push bodies: no command text for a permission beyond the tool name and
  target path), createdAt, state, snoozedUntil?, attention: "high"|"normal", links: {task?, permissionRequestId?, validationId?, prUrl?}}`.
- **States:** `new`, `seen`, `snoozed` (until a time, max 24 h), `resolved`. Derived items have a natural "gone" state; the stored table
  `inbox_state(key PK, state, snoozed_until, actor, updated_at)` only holds what a Human did, and is garbage-collected when its key no longer derives. **No
  stored inbox items**: a missed event cannot lose an item, a restart cannot duplicate one, and the list is always consistent with the real state.
- **RPCs** (all read-only except the three state setters): `inbox.list {kinds?, states?, limit, before}`, `inbox.count {}` (`{total, unseen, byKind}`, what the
  badge reads), `inbox.seen {keys}`, `inbox.snooze {keys, until}`, `inbox.resolve {keys}`. **Reads are open to every connection including the relay.** The three
  setters change display state only, so they are allowed over the relay too (a phone may mark an item seen), but they are **refused for run-scoped agents**
  (a run-scoped agent cannot mark inbox items resolved on behalf of the Human; a task an automation started likewise) and `inbox.snooze` / `inbox.resolve` are
  refused for `permission` keys. **Acting** on a permission from the inbox calls the existing `run.respondPermission` with the same params, the same
  validation and the same audit as the task page; the inbox adds **no** new way to answer one.
- **Live updates:** one new, ids-free topic **`inbox.changed`** `{}` published (coalesced, 250 ms) when `permission.pending/resolved`, a verdict or the stall marker
  change or an inbox state setter runs; clients refetch `inbox.count` (badge) and, if open, `inbox.list`. Existing topics stay as they are (the task tree keeps
  using `task.stalled` and the attention hook). Reconnect: `inbox.count` on connect, like every other resync.
- **Badge counts:** `unseen` of items in state `new` plus every pending `permission`, snoozed excluded until their time; a permission is always counted.
- **Push interplay (ADR-0040):** **no new push kind in v1.** `permission` already pushes and is the only item that blocks a run. A verdict awaiting a merge
  decision, a stall and an automation pause are not urgent enough to buzz a phone by default and each would add a kind to the registration filter (`kinds`)
  and a text policy; the inbox badge and the existing stall notification on the web cover them. Argued the other way: a `fail` on the Lead's last milestone is
  exactly what a person away from the desk wants to know; that is Question 7 (an opt-in `validation` kind, off by default, is the cheap follow-up). A tap on
  an existing push opens the task as today; the inbox does not change push routing.
- **Surfaces.** **Web:** an **Inbox page** (`/inbox`, a route) reached from a sidebar entry with the badge count; list rows grouped by "needs you now" (permission,
  with the full permission card inline, same component as the task page so the full request detail, path and diff are visible), "ready for a merge decision",
  "stalled", "paused"; each row opens its task; filters by kind; `j/k` and arrow navigation, Enter opens, `s` snooze, `e` resolve (never on a permission).
  **Mobile:** an **Inbox** entry in the tasks area with the same badge; one column, 44 pt rows, the permission card full-screen, no hover-only actions.
  **Accessibility:** the list is a `list` of buttons; the badge change is announced through a polite live region (never an assertive one, never a focus steal);
  state is never colour alone (icon + text); every action has a visible label; reduced-motion removes row transitions; the narrow layout is a single column with
  the actions in a bottom sheet. Light and dark, 1280 and 390; the same ZCode visual parity as the rest of the app.
- **What the inbox must NOT do:** no "approve all" or any bulk answer of a permission (answers are one request at a time, as today); no merge button or merge
  RPC; no auto-approve, "always allow" or "remember" shortcut on the inbox card (ADR-0019, ADR-0017); no way for an agent to create, mutate or clear an item;
  no hiding of a pending permission (it cannot be snoozed or dismissed); no content beyond the redacted summary in the list (detail lives in the card, on a
  direct or relay-authenticated connection).

### 6. Options considered, with the recommendation each

1. **Validator as a profile + MCP tools (recommended)** vs **a first-class task kind.** A task kind would add a state machine, a UI tier and migrations for
   what is an ordinary child task with a special profile and one table (`validations`). The profile keeps ADR-0041's "a seat is a profile" and the tree view
   works unchanged. Recommendation: profile + a `validations` table + `purpose = validation` on the task.
2. **Contract as an immutable DB row authored from a file (recommended)** vs **file only** (mutable after the fact, the author can edit what the judge reads)
   vs **DB only** (not reviewable in the PR, awkward to author).
3. **Front-matter YAML + markdown (recommended)** vs **pure JSON** vs **pure markdown with conventions** (not machine-checkable: an AC with no check would pass
   unnoticed).
4. **Inbox as a page with a sidebar badge (recommended)** vs **a panel/drawer** (competes with the right-hand panels the PR panel and terminal already use, and
   does not translate to a narrow screen) vs **a sidebar section** (the sidebar is already the task tree; an inbox section there grows without bound and cannot
   carry a full permission card).
5. **Derived inbox + read-state table (recommended)** vs **stored items** (event loss, duplicates, drift from the truth, needs migration of every producer).
6. **Verdict binds a sha and a contract version; merge-ready is computed (recommended)** vs **verdict stored as "approved"** (goes stale silently after a push).
7. **One-time verdict token to the validator's env (recommended)** vs **trust the run identity alone** (a Lead could submit under a Peer's id) vs **the
   fail-closed run-scoped token of ADR-0027 §3.2** (right answer, a larger change: deferred, listed as the follow-up that would harden this).
8. **A separate `validation` push kind now** vs **none in v1 (recommended)**.

### 7. The "user-testing QA skill" (productized Playwright frame harness)

That queued item is: an isolated daemon, light and dark, 1280 and 390, transition contact sheets, a diff-to-app mapping and an evidence report. Factory's Missions
split a milestone's validation into a *scrutiny* pass (read the diff) and a *user-testing* pass (drive the running app); ours is the same shape, and the
contract's `frames` section **is the interface** between them.

- **Absorbed, as the validator's skill, later.** The validator seat should **carry** the frame harness as a bundled skill (`smind-validator-frames`), delivered
  with the seat the way `smind-leader` is, and contributed through the plugin **skills** point (ADR-0042 Wave A) so it is versioned and replaceable without
  a daemon release. The contract's `frames[].harness` (optional, default `ui-frames`) names it.
- **Left alone now.** v1 of this ADR ships the validator **without** the harness: a milestone with `frames` and no harness gets a `blocked: no_browser` frame
  item, which is a legitimate, honest verdict reason (the Lead's own screenshot pass is unchanged and still required by AGENTS.md). The harness is a separate
  item with its own plan.
- **Implications of carrying it:** the validator then needs a browser binary and an isolated `SMIND_HOME` and port per validation (process safety rules:
  PIDs recorded, killed by PID only, burners self-clean), a larger default budget (`limits.budgetUsd` for UI milestones, reading images costs), a cap of 12
  images per contact sheet, and that *looking* is a model judgement: the verdict records an observation per frame and the Human can open the evidence
  files from the inbox row. Frame results are the weakest evidence kind (a model's reading of a picture); a `frame` item alone never makes a milestone
  `pass` unless the AC it covers also has a test or command (completeness rule: every AC needs a non-frame check unless the contract marks it `visual: true`
  and says why).

## Alternatives considered

- **Strengthen the `reviewer` (keep `plan` mode).** Cannot run a test or a mutation; "read-only review" is exactly the mode that cannot refute a claim. Rejected as
  the validator, kept as an idea: the adversarial checklist.
- **Have the Lead launch a Peer as verifier by convention (today's rule).** Works, costs a brief per milestone, and nothing binds the check to the ACs, the sha
  or evidence; no structured verdict, nothing for the Human to see. The validator is that practice with a contract and a result type.
- **Make the validator a CI job / GitHub check.** Right for what CI can run, wrong for the agent loop (a PR has to exist and the checks are fixed in YAML);
  a validator verdict can later be mirrored to a PR comment (not built).
- **Auto-merge on `pass`.** Refused: ADR-0039's forge rules and every standing rule say a human merges; a verdict that gates nothing automatically is also
  the only kind that cannot be gamed into a merge.
- **Unify the inbox with the task tree attention dots.** The dots stay (they are per-task); the inbox is cross-task, so it cannot be a decoration of the tree.

## What we are NOT doing

- No auto-merge, no merge RPC, no merge button, no "approve and merge".
- No auto-approve, no "approve all", no "always allow" in the inbox; no new way to answer a permission.
- No agent-authored contract text delivered into the validator beyond the structured fields; no Lead reasoning to the validator.
- No verdict written by the Lead, by a Peer or by the Human through the validator path (a Human acts through the inbox `resolve`, which is not a verdict).
- No waiver/override concept for a failed verdict in v1.
- No new push kind in v1 (unless Question 7 is answered otherwise).
- No sandbox claim: the validator's boundary is the provider's permission mode and the scratch worktree; the fail-closed run-scoped token (ADR-0027 §3.2) stays
  deferred.
- No stored inbox items; no inbox for chat messages or task completion ("done" stays a push and a dot, not an inbox row).
- No Playwright/browser harness in v1 (Section 7); no PR-comment mirroring of verdicts; no required-check integration.
- No change to the Peer handoff schema; no change to ADR-0044's surfaces (Settings → Automations stays; only its `paused` state feeds an inbox row later).
- No cross-repo or cross-workspace validation; one task, one branch, one contract at a time.

## Consequences

- The Lead's "adversarial review" splits in two: it still reviews its Peers' diffs, and a validator reviews the **milestone** against a contract it did not write
  into its head. Cost per milestone rises by roughly one validator run ($0.3-1.5); the tree budget and `limits` bound it.
- New persistent state: `validation_contracts`, `validations` (with a hashed token and the run binding), `inbox_state`. One migration in the store; no
  existing table changes. Shared-file touch points to sequence with other lanes: the store migration, the wsapi handler map and `RunScopedMethods`, the event
  topic list (`inbox.changed`, `validation.updated`), the role pack seed and the web/mobile query-key modules.
- ADR-0041's `reviewer` seat is replaced by `validator`; a daemon seeded before keeps its `reviewer` profile (seeding never rewrites), and nothing refers to it.
  No installed base, so no migration (project rule).
- `docs/WORKSPACE_PROTOCOL.md` gains one paragraph (the Lead writes a contract before code and requests a validation per milestone; cites `validationId`) when
  the seat ships; the protocol's "reviewer ... Human decision, open" sentence is closed by Question 4.
- Honest limits stated: verdict authority is a drift guard (env token), `auto` mode cannot prevent a shell writing outside the worktree (it is detected for the
  Lead's worktree, reported otherwise), frame evidence is a model's reading of an image, and a validator that shares the author's model family shares blind spots
  (a different model for the validator is a profile setting, Question 6).

## Questions for the Human

Each is one real decision; the **Recommendation** is what binds if you accept "as recommended".

1. **Contract format.**
   - (a) YAML front matter (strict subset) + markdown body not shown to the validator, validated by a JSON Schema.
   - (b) Pure JSON document.
   - (c) Markdown only, conventions checked by a linter.
   *Recommendation:* **(a).** Readable in the PR, machine-checked at submit, the prose can never leak the Lead's reasoning to the judge.

2. **Does a milestone need a contract, always?**
   - (a) Required by protocol for L-size items and for any change in `internal/wsapi`, store migrations, permissions, relay/E2EE or UI; optional for S/M docs/test changes.
   - (b) Always.
   - (c) Optional everywhere (the Lead decides).
   *Recommendation:* **(a).** It matches the sizing table's Risk and Verification axes (security, data loss, UI) and does not tax a one-file fix; the daemon does not enforce it in v1, the protocol and `smind-leader` ask for it and the PR body cites the `validationId` or says "no contract: reason".

3. **Contract storage.**
   - (a) Authored as a file, stored as an immutable DB row per version; the validator reads the row.
   - (b) File only (in the worktree).
   - (c) DB only.
   *Recommendation:* **(a).**

4. **The `reviewer` seat.**
   - (a) Replace it with `validator` in the role pack (the checklist moves into the validator's instructions); close the single-loop item 4.
   - (b) Keep both.
   - (c) Delete `reviewer`, add nothing.
   *Recommendation:* **(a).** The audit found no run of `reviewer`; the validator is the same job with tools that can refute a claim.

5. **Validator permission mode.**
   - (a) `auto` in a detached scratch worktree at the exact sha, plus the always-ask git rules and a daemon post-check of the Lead's worktree (`scope_violation`).
   - (b) `default` (asks for every command): safest, and a validator that asks for `go test` is unusable unattended.
   - (c) `plan` (today's reviewer): cannot run tests or mutations.
   *Recommendation:* **(a).** It is the Peer's mode in a disposable worktree, with the honest limit written down; (b) is the per-profile override for a Human who wants it.

6. **Validator model.**
   - (a) Same as the Lead (`claude-sonnet-5-5`) in v1; the Human can change the profile.
   - (b) A different provider/model by default, to decorrelate blind spots.
   *Recommendation:* **(a).** No Opus anywhere (standing rule); a non-Claude validator needs the provider's own test-running setup and is a profile edit later, not a default now.

7. **Push for verdicts and stalls.**
   - (a) No new push kind in v1; the inbox badge (and the existing web stall notification) carry them.
   - (b) An opt-in `validation` push kind (fail/blocked/merge-ready), off by default.
   - (c) Push all three inbox kinds.
   *Recommendation:* **(a).** `permission` already wakes a phone for the only blocking item; (b) is a small follow-up once the inbox exists and the Human says they want it.

8. **Overriding a `fail`.**
   - (a) No waiver in v1: fix and re-request, or submit a corrected contract; the Human decides about the PR outside smind.
   - (b) A Human-only `validation.waive {id, reason}` recorded in the verdict history.
   *Recommendation:* **(a).** Since smind never merges, a waiver gates nothing; building it would invite the habit of waiving.

9. **Inbox shape.**
   - (a) A page with a sidebar entry and badge (web) and an Inbox entry (mobile).
   - (b) A drawer/panel.
   - (c) A sidebar section.
   *Recommendation:* **(a).**

10. **Inbox state actions over the relay.**
    - (a) `seen`, `snooze`, `resolve` allowed over the relay (display state only), refused for run-scoped agents; permission answers as today.
    - (b) Read-only over the relay; state changes direct only.
    *Recommendation:* **(a).** They change nothing but the Human's own list, and "mark seen" from a phone is the point of a phone inbox; the rule that matters (an agent cannot touch them) holds either way.

11. **The QA frame harness.**
    - (a) Validator carries it as a bundled/plugin skill, shipped as its own later item; v1 reports `blocked: no_browser` for frame items.
    - (b) Build it into this item.
    - (c) Keep it separate from the validator.
    *Recommendation:* **(a).** The contract's `frames` and the verdict's frame items are the stable interface; the harness is a separate, browser-heavy build with its own cost and process-safety questions.

12. **Where `validation.maxBudgetUsd` and the default per-validation budget live.**
    - (a) Daemon config (`validation.maxBudgetUsd`, default ceiling $5, per-contract default $1.5), like `automations.maxBudgetUsd`.
    - (b) Hard-coded.
    *Recommendation:* **(a).**

## Human answers (2026-10-27)

All twelve questions are answered **as recommended** (option (a); Human pre-approval, applied by the Supervisor):
1 YAML front matter (strict subset) + markdown body not shown to the validator, JSON-Schema validated; 2 contract required by protocol for
L-size items and for changes in `internal/wsapi`, store migrations, permissions, relay/E2EE or UI, optional for S/M docs/test changes (not
daemon-enforced in v1); 3 authored as a file, stored as an immutable DB row per version; 4 the role pack's `reviewer` seat is replaced by
`validator` (single-loop item 4 closed); 5 validator runs in `auto` in a detached scratch worktree at the exact sha, with the always-ask git
rules and a daemon post-check of the Lead's worktree (`scope_violation`); 6 validator model = the Lead's (`claude-sonnet-5-5`) in v1, never
Opus; 7 no new push kind in v1 (inbox badge + existing stall notification); 8 no waiver of a `fail` in v1; 9 inbox = a web page with a sidebar
entry and badge, plus a mobile Inbox entry; 10 `seen`/`snooze`/`resolve` allowed over the relay (display state only), refused for run-scoped
agents; 11 the QA frame harness becomes the validator's bundled skill as its own later item, v1 reports `blocked: no_browser` for frame items;
12 `validation.maxBudgetUsd` in daemon config, ceiling $5, per-contract default $1.5.

