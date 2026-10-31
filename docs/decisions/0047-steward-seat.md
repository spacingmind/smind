# 0047: Steward seat — a declarative permission policy and a structured review verdict, both landing in the Human inbox

## Status

**Accepted — 2026-10-31** (Supervisor, under the Human's 2026-10-31 directive "xử lí hết trừ nhóm E": every decision in section 11 accepted as written, including D1's amendment of ADR-0019; the Human may veto any of them). The Human directed "Steward-in-smind" (design a Steward seat inside smind that reviews task verdicts and
surfaces permission requests through the inbox). Every decision in [section 11](#11-decisions-taken-as-supervisor-defaults) is a **Supervisor default 2026-10-31 under the
Human's directive, Human may veto**; a veto changes the PR listed next to it. Spec, acceptance criteria and PR split: `docs/plans/active/steward-seat.md`.

Inputs: ADR-0017 (an orchestrating agent never approves), ADR-0019 (provider-native permission modes; "smind never again decides by itself that a tool call is safe"),
ADR-0041 (seats, tree budget A5, parent notifier), ADR-0044 (automations: rails, audit, human-only authority), ADR-0045 (validator seat, verdict bound to a commit,
inbox with derived items), ADR-0037/#632 (secretdeny rules), `docs/plans/active/agent-git-approvals.md` (the git/gh command classifier already in the decider),
`docs/WORKSPACE_PROTOCOL.md`, and the local-only Steward handoffs of 2026-10-25..27 (what a human Steward actually checks, below).

## Context

### What the Steward does by hand today

In the Paseo-era room and again in the smind trees of 2026-10-25..27 a human (or a Claude session acting for them) plays **Steward**: it briefs Leads, watches their
runs, answers their permission requests, reads their PRs and says "ACCEPTED, merge order …". Over ~45 PRs in one session this was the bottleneck and the main source of
mistakes that a person makes at 3 a.m.: a request approved without reading the command, a stale screenshot accepted, a stacked PR pushed. The handoffs record what the
Steward actually checks. Those checks are the specification of this ADR:

- **Permissions.** Peek at the command with heredoc bodies elided before approving (backtick-laden messages run as commands). Approve only a *plain push of the seat's own
  branch* and `gh pr create --base develop` with a read body. **Deny** `git stash`, any force push (including `--force-with-lease`), anything that touches `~/.smind`,
  `SMIND_HOME` of the real daemon or port `:4648`, `git reset --hard origin/develop` outside the seat's own worktree, and a mutation chain that points a harness home at the
  real `$HOME` (tests run with `HOME=$(mktemp -d)`). Log every approval and its reason.
- **Review.** Do not trust the summary: read the diff; check binaries (`git diff --numstat | awk '$1=="-"'`); mutation checks must run on **committed** work; screenshots
  must come from the **final** commit and must be *looked at* (a stale green check on a blocked verdict, an empty transition sheet were both nearly accepted); never stack PRs
  (a PR2 branched off an unmerged PR1, plan commits inside impl branches); require CI green, a Conventional-Commits title, and a body that says what was not verified;
  the GLM handoff block (`status`, `tests_run`, `not_verified`) is read first and is a claim, not evidence.
- **Verdict line.** `#NNN <title> - ACCEPTED` plus merge order. The human merges.

### What smind already has and what is missing

| Have | Where | Gap |
|---|---|---|
| Permission requests reach a human; unanswered ones auto-**deny** after 5 min (`defaultPermissionTimeout`, resolution `timeout`) | `internal/runs/registry.go`, `taskrunner.PermissionResolution` | no *standing* human decision: every `git push` of a seat's own branch is a manual click, so the Steward approves in bulk and reads less |
| A git/gh command classifier and a `gitRule` card line | `agent-git-approvals.md` (#595/#596) | classifies for display and for provider `permissions.ask`; it never decides |
| Inbox with `permission`, `verdict`, `stall`, `automation` rows | ADR-0045, `internal/inbox` | no "this task was reviewed" row; the validator verdict is judged against *the contract*, not against the protocol (CI, binaries, stacking, PR body, frames) |
| Validator seat: fresh context, contract, commit-bound verdict | ADR-0045 | it checks that the work meets *its own contract*; nobody checks that the contract and the process were adequate |
| Automation rails (runs/hour, dedupe, auto-pause, audit) | ADR-0044 | for signals, not for reviews |

ADR-0017 and ADR-0019 state, in the user's words, that **an agent never answers a permission** and that **smind adds no auto-approval of its own**. Both stay true in this ADR
for every *agent*. What this ADR adds is a **human-authored standing policy evaluated by the daemon** (no LLM, no agent in the loop), which is the "human's standing decision"
ADR-0019 §6 already accepts for a profile's mode; and a **review seat that can only write a verdict**, never an answer.

### Threat model in one paragraph

The inputs to the policy engine are a tool name, a command string and paths **chosen by an agent that may be prompt-injected**. The engine must therefore be conservative on
anything it cannot fully parse, must resolve paths before judging them, and must never let its own configuration be written by an agent. The inputs to the review seat are
a diff, PR text and Peer/Lead prose, which are **untrusted data**: the seat may be talked into a good verdict by them, so the daemon computes every objective gate itself and
clamps the seat's verdict to it (it can lower a verdict, never raise one past a failed gate). The seat has no shell, no write tool and no way to message another agent, so a
fully compromised seat can at worst write a wrong *advisory* verdict that the Human still reads and merges on.

## Decision

Two cooperating parts and one rule between them: **the policy engine decides requests deterministically; the Steward seat judges finished work; neither can merge, release,
touch the Human's install or read a secret; everything that needs a person goes to the inbox.**

### 1. The seat: `steward`

| Aspect | Decision |
|---|---|
| What | a fourth role-pack profile `steward` (after `lead`, `peer-sonnet`, `validator`), seeded by `smind profile seed roles` (idempotent, never rewrites an existing profile) |
| Provider/model | `claude-native`, `claude-sonnet-5-5`, mode **`default`** (asks), `childPolicy: none`, `onRateLimit: wait` |
| Trust | **read-only judge.** Runs in a **detached scratch worktree at the exact head sha** (the validator's mechanism, ADR-0045 §4), never the Lead's worktree |
| Tools | provider read tools only (`Read`, `Grep`, `Glob`) **plus** a two-tool MCP entry: `steward_digest_get` and `steward_verdict_submit` (and `steward_annotate`, see §6, off by default). **No `Bash`, no write/edit, no `task_*`, no profile, no permission, no validation-request tools**; the token is only in that entry's env, single use, stored hashed (the validator pattern) |
| Starts | by the daemon only: the Human (`steward.review`) or an opt-in trigger (§4). A Lead, a Peer or any agent cannot request one (not in `RunScopedMethods`) |
| Its own permission requests | go through §2's engine with a **seat floor**: allow only `Read/Grep/Glob` resolving inside the scratch tree; everything else is `deny` (not `ask`: a judge has no reason to ask) |
| Prompt | built by the daemon from the structured digest (§3) and fixed instructions; no Lead/Peer free text except fields that are quoted and length-capped inside a delimited "untrusted" block |

### 2. The permission policy engine (S1)

A **pure Go function** `Decide(Request, Policy) Decision` in a new package `internal/permpolicy`, called by `runPermissionDecider` (`internal/runs`) *before* the request is
parked for a human. It adds no LLM, no network and no I/O beyond `realpath` of the request's paths.

**Scope and default.** Engine **off by default** (`steward.policy.enabled: false` in `config.yaml`, read at every decision, like `plugins.enabled`). Off = today's behaviour,
byte for byte. When on, a request nothing allows is **`ask`**: it follows the existing path to a human and is auto-denied after **5 minutes** (`defaultPermissionTimeout`,
unchanged; now audited with `ask_timeout_deny`). **Nothing ever auto-allows by default** (default-deny: unmatched = not allowed). It only sees requests that reach smind: a
provider mode that already auto-approves (`bypassPermissions`, `autoAccept`) never asks, so the policy cannot narrow it (stated limit; the Claude deny rules of #632 stay the
backstop there).

**Where the policy lives.** `$SMIND_HOME/steward/policy.yaml`, **human-authored**. There is **no RPC and no MCP tool that writes it** (a test walks both against the handler map,
like `pluginMutating`). Reads (`policy.get`, `policy.test`, `policy.audit`) are open to every connection including the relay. The file is re-read when its mtime or size
changes; an unreadable or invalid file means **floor + ask** (never "allow everything"), reported by `policy.get {valid:false, error}` and a settings banner. Honest limit,
stated: a same-user process can edit the file (as with `plugins.enabled`); the secretdeny rules shipped in #632 deny agents the `SMIND_HOME` tree, and the audit shows the
policy sha256 on every decision so a change is visible after the fact.

**Schema (v1, strict YAML subset: no anchors, tags, merge keys; duplicate keys and unknown fields are errors).**

```yaml
version: 1
rules:
  - id: lead-push-own-branch        # unique, [a-z0-9-]{1,64}
    action: allow                   # allow | deny | ask
    why: "Lead pushes its own task branch"   # shown on the card and in the audit, <= 160 chars
    match:
      seats: [lead]                 # profile names or roles; omitted = any seat; "*" is rejected
      workspaces: [smind]           # workspace titles; omitted = any
      tool: Bash                    # exact tool name or "mcp:<server>.<tool>"; "*" is rejected
      command:                      # Bash only; structured, not regex-over-a-string
        argv0: git
        subcommand: push
        args: {allow_only: ["origin", "{taskBranch}"], deny_flags: ["-f","--force","--force-with-lease","--delete","--mirror","--all","--tags"]}
      paths: {within: ["{worktree}"]}    # for Read/Edit/Write/Bash cwd; resolved with realpath
```

**Evaluation (the whole algorithm).**

1. Normalise: parse a Bash command into simple commands with a shell-aware splitter (quotes, `;`, `&&`, `||`, `|`, `&`, redirects, `$(...)`, backticks, subshells, here-docs
   and here-strings). **Heredoc bodies are elided** (kept only as a length) in the audit and on the card, and are *not* matched as commands. Anything the splitter cannot fully
   parse (unbalanced quotes, `eval`, `bash -c "<non-literal>"`, process substitution, an env-var-assigned command we cannot resolve) makes the whole request **`ask`**.
2. For each simple command and each path it names (arguments, redirect targets, `cd`, cwd): `realpath` (symlinks resolved; a path that does not exist is resolved by its
   deepest existing parent; a failure to resolve = `ask`).
3. **Floor** (compiled in, not configurable, applied first; a match is `deny`): see §2.1.
4. Every rule whose `match` fits **every** simple command of the request is a candidate. Precedence is by **action, not file order: `deny` > `ask` > `allow`**. An `allow`
   needs a rule that fits *each* simple command; one uncovered segment makes the request `ask`. No candidate = `ask`.
5. An `allow` for a Bash request is **dropped to `ask`** when any segment has a redirect or path outside `paths.within`, when the rule has no `paths` and the segment writes
   (the rule loader rejects a mutating `allow` without a path scope, see below), or when the request is not for a task with a known worktree.
6. Return `{action, ruleId, policySha, reasons[], normalized (redacted, ≤ 512 chars), paths[]}`.

**Loader checks (a bad rule fails the whole file, field-path message):** unknown field; `*` in `seats`, `tool`, `argv0` or `subcommand`; an `allow` without `tool`; an `allow`
for `Edit`/`Write`/`Bash`-that-writes without `paths.within`; an `allow` whose pattern a floor rule would deny (rejected with the floor rule's id); `why` missing or > 160;
more than 200 rules; a regex (v1 has none: patterns are structured, which removes ReDoS and makes "what does this match" explainable by `policy.test`).

#### 2.1 The floor (always `deny`, even if a rule says allow, even for a Human-written rule)

These are the checks the Steward made by hand, hard-coded:

| id | Denied |
|---|---|
| `no-stash` | `git stash` (any form; the stash stack is shared across worktrees) |
| `no-force-push` | `git push` with `-f`, `--force`, `--force-with-lease`, `--force-if-includes`, `--mirror`, `--delete`, a `+refspec`, or a `:ref` delete |
| `no-merge` | `gh pr merge`, `gh api` mutating `…/merge`, `git merge`/`git push` targeting `develop`/`master`/`main`, `gh release`, `git tag` + push of a tag, `npm publish`, `cargo publish`, `gh workflow run` for a release workflow |
| `no-install-state` | any path under `$HOME/.smind`, the daemon's own `SMIND_HOME` (unless it is the seat's *isolated* sandbox home, which the daemon registers when a Lead starts one), `~/.claude*`, `~/.config` credentials, `~/.ssh`, `~/.gnupg`, keychains; `smind service *`; a connection to the daemon's own port or `:4648`/`:4870`; `pkill`, `killall`, `kill` of a PID the run did not start |
| `no-secrets` | reading or printing a secretdeny path (#632), `env`/`printenv`/`set` dumps, `cat` of `.env*`, token files, `gh auth token`, `security find-*` |
| `no-destructive-outside` | `rm -r`/`git clean -fdx`/`git reset --hard`/`git checkout -- .` whose target resolves outside the task's worktree; `sudo`; `chmod -R`/`chown -R` outside the worktree |
| `no-curl-sh` | a pipe from a network fetch into an interpreter (`curl … \| sh`, `wget -O- … \| bash`) |
| `no-real-home-tests` | a command that sets `HOME` or `SMIND_HOME` to `$HOME`, `~`, or unsets them for a test/mutation run (tests must use `HOME=$(mktemp -d)`) |
| `no-add-all` | `git add -A`, `git add .`, `git add -u` (files are added by name; binaries and churn are checked before every commit) |

The floor is the **only** place `deny` is non-negotiable; `deny` rules a human adds are in the same bucket for precedence but removable by editing the file. A floor `deny`
resolves the request to a deny option immediately and the agent sees an ordinary "denied" with the floor id in the message, so it can adapt (use `git add <file>`).

#### 2.1.1 Windows

The engine runs on every OS (follow-up of release 0.17). The path flavour is chosen per request (`platform`), not by the daemon's build: a Windows request uses
drive/UNC, case-insensitive and junction-aware path handling. The command splitter treats backslashes as Windows-literal path separators; a `\` before a shell
metacharacter (`"`, `\`, `$`, a backtick) keeps the POSIX escape meaning. Paths the engine cannot place (`C:foo`, device paths, alternate streams, trailing dots) escalate or
deny, never allow. A literal `powershell|pwsh -Command` / `cmd /c` payload is parsed in its own dialect and its inner commands are judged by the floor and the rules like any other; the wrapper itself never becomes an allow, and a payload the splitter cannot be sure of (encoded command, script block, escapes, expansions) still escalates. See `docs/plans/active/permpolicy-windows.md`.

#### 2.2 What it auto-approves and what it escalates

Auto-approves **only** what the human's file says, restricted by the loader to narrow, path-scoped, per-seat rules. Ships with `smind policy init`, a **commented template that
writes zero active allow rules** and shows these opt-in examples: `go test`/`task test`/`task lint`/`bunx tsc` inside `{worktree}` for any Lead or Peer; `gh pr view/checks/diff`
(read-only) ; `git add <named file>`, `git commit` (no `--amend` of a published commit) inside `{worktree}`; **plain `git push origin {taskBranch}`** for a Lead; `gh pr create
--base develop` only with `--body-file` or `--fill`-free explicit title+body (never `--web`). It **always escalates** (`ask`): `git push` to anything else, `gh pr create` with
another base, any write outside the worktree, any network command not in a rule, anything unparseable, any MCP tool not named by a rule, any request of a seat with no matching
rule, and every request while the policy file is invalid.

#### 2.3 Audit log

Table `permission_policy_audit` (new, append-only, no update/delete RPC): `id`, `createdAt`, `runId`, `taskId`, `requestId`, `seat` (profile name), `workspaceId`, `tool`,
`target` (normalised, redacted, heredocs elided, ≤ 512 chars), `paths` (resolved, ≤ 8), `decision` ∈ `allow | deny | ask | ask_human_allow | ask_human_deny | ask_timeout_deny`,
`ruleId`, `policySha`, `reasons`, `latencyMs`. **Every** evaluation writes a row, including `ask` (and the later human or timeout outcome is a second row linked by `requestId`).
`permission_resolved` events gain two resolutions, **`policy_allow`** and **`policy_deny`**, carrying `ruleId` (the timeline renders them with the rule's `why`); a human answer
is still `human`, a timeout still `timeout`. Retention 90 days (a daemon sweep). Surfaces: `policy.audit {taskId?, decision?, limit, before}`, `smind policy audit`, and a
read-only table in Settings. Audit text goes through the existing redactor (push-body rules); no env values, no command output.

#### 2.4 The 5-minute timeout

Unchanged in behaviour and made visible: a request the policy escalated and no human answered in 5 minutes resolves to a **deny option** (never an allow, whatever the mode),
writes `ask_timeout_deny`, and the inbox `permission` row leaves as it does today. The seat's *own* requests have no `ask` path (§1), so no timeout applies to them.

### 3. The review loop (S2)

**Trigger → collect → judge → clamp → publish.**

1. **Trigger** (§4): a finished Lead run (or the Human) for a task with a pushed branch.
2. **Collect, deterministically in the daemon, no agent.** A `Digest` (JSON, ≤ 64 KiB, the only thing besides the scratch tree the seat sees):
   `task` (id, title, branch, headSha, baseSha), `diffStat`, **binary check** (`numstat` `-` rows), `files` vs contract `writeScope`, `commits` (subjects, Conventional-Commits
   check, attribution trailer), `stacking` (is the base reachable from `origin/develop`; does the branch carry commits of another unmerged PR branch), `pr` (number, base, title,
   body sections: *what changed*, *how validated*, *not done*, mutations listed), `ci` (state at `headSha` from the existing forge status, ADR-0039: `success|failure|pending|none`
   + failing check names), `validation` (latest verdict, `commitSha == headSha`?, `mergeReady`, the contract's mutation list and each item's result), `frames` (the qaframes
   evidence set: per frame path, size, capturing commit sha, matrix coverage 1280/390 × light/dark), `peerHandoffs` (the json blocks: `status`, `not_verified`), `churn`
   (`gen/schemas`, lockfiles, build artifacts), `secrets` (secretdeny scan of added lines: counts only, no values), `testsChanged` (production files changed vs test files
   changed), and `policyAudit` (the task's denied/escalated permission decisions count).
3. **Gates (objective, computed by the daemon, each `pass|fail|unknown|n/a`)**:

| gate | fails when |
|---|---|
| `ci` | state `failure`; `pending` = verdict deferred (`unknown`), re-evaluated on the CI event |
| `validator` | a contract exists and there is no `pass` verdict at `headSha` (stale, fail, blocked, scope_violation) |
| `frames_fresh` | a diff touching `web/` or `mobile/` has no evidence, or its frames were captured at a sha other than `headSha`, or a matrix cell is missing |
| `no_binaries`, `no_churn`, `no_secrets` | numstat `-`, `gen/schemas`/lockfile/build artifact, a secretdeny hit |
| `not_stacked` | PR base is not `develop`, or the branch contains another open PR's commits |
| `pr_hygiene` | title not Conventional Commits, or body lacks *validated* / *not done* |
| `scope` | files outside the contract's `writeScope` (and the plan file) |
| `mutations_committed` | a mutation check reported in the validator verdict was run at a sha other than the committed head |
| `peer_claims` | a Peer handoff is `blocked`/`failed`, or has non-empty `not_verified` that the PR body does not repeat |

4. **Judge.** The seat reads the diff in its scratch tree, the frames (it **looks at every image**: it must submit an `observation` per frame, like the validator), the PR body
   and the handoffs, and attacks them as the Steward does (security first: input reaching a shell, git or SQL, widened permissions, a way for an agent to approve itself; data
   loss on cleanup paths; races; tests that do not test the behaviour; scope creep). Output: `steward.verdict.submit {reviewId, token, verdict, summary, findings[], gates ack}`.
5. **Verdict** — an enum with exactly these meanings:
   - **`ACCEPTED`** — every gate is `pass` or `n/a`, the seat found no blocking finding. *The Human may merge; smind never does.*
   - **`CHANGES`** — fixable findings (each has file, line, why, and the exact check that would show it fixed); the Lead can act and a new head triggers a new review.
   - **`BLOCKED`** — a hard gate failed that the Lead cannot fix by editing code in this branch (stacked PR, CI infrastructure down, the contract itself wrong, a secret
     committed, a permission/policy violation) or the seat could not finish (timeout, budget, crash — daemon-written, as with the validator; never `ACCEPTED`).
6. **Clamp (daemon).** The stored verdict is `min(seat, gates)` in the order `BLOCKED < CHANGES < ACCEPTED`: a failing gate caps at `CHANGES` (or `BLOCKED` for the
   `not_stacked`, `no_secrets` gates); an `unknown` gate (CI pending) holds the review in `pending` and nothing is published. The seat **cannot raise** a verdict above its
   gates and **cannot clear a gate**; the verdict row stores both `seatVerdict` and `verdict` so a clamp is visible. The daemon never writes `ACCEPTED`.
7. **Publish.** The row is stored in `steward_reviews` (commit-bound: `headSha`, `contractSha256`, `validationId`, `digestSha256`, `policySha`), the inbox item is derived (§5),
   and `steward.updated {}` is published. **Stale on any head move, newer contract version or newer review**; stale is never shown as ACCEPTED (`mergeReady` computed on
   read, as ADR-0045 AC6). There is **no merge RPC and no "merge" or "approve" button** anywhere.

### 4. Triggers and rails

- **Manual:** `steward.review {taskId, commitSha?}` — the Human (direct or relay), or the daemon's own trigger. Not available to run-scoped agents or the `automation` origin.
- **Opt-in auto trigger:** `steward.autoReview` per workspace in `config.yaml`, **default off**. When on, a **root task run that ends `done`** for a Lead-profile task with a
  pushed branch and a PR (or a recorded head) enqueues one review. Not on `error`/`stopped`, not for Peers (their candidate is judged inside the Lead's review).
- **Rails (all enforced by the daemon, named defaults, config ceilings, the ADR-0044 pattern):** single flight per task; **dedupe** by `(headSha, contractSha, ciState)` (an
  identical review is not re-run); `maxReviewsPerHour` default **6** (ceiling 24) workspace-wide; `maxReviewsPerTask` default **5**; `budgetUsd` default **1.5** per review
  (ceiling `steward.maxBudgetUsd`, default 5), `maxMinutes` default **20** (ceiling 60), rate-limit wait does not consume the wall clock; **auto-pause** after 3 consecutive
  `crashed`/`timeout` reviews (a visible reason, resumed only by the Human); global switch `steward.enabled` (default **false**; invalid config = off); a review over the tree
  budget is refused for an agent trigger and never for a Human one (ADR-0041 A5 semantics).
- The seat's cost is a normal task cost (usage rollups); **the policy engine costs nothing** (no model call).

### 5. Composition

- **Validator (ADR-0045).** The steward consumes the validator's verdict; it never replaces it and never issues a `validation.verdict`. A task with a contract needs a validator
  `pass` at head for `ACCEPTED`. The validator judges *the work against the contract*; the steward judges *the contract, the process and the evidence against the protocol*. They
  use the same scratch-worktree/token/clamp mechanics and share the helper package where the validator already exports it (S2 decides in the PR, see plan).
- **Inbox (ADR-0045 §5).** A new derived kind **`review`**, key `review:<reviewId>`, from the latest non-stale `steward_reviews` row of a task: `ACCEPTED` → group "ready for a
  merge decision" (`attention: normal`), `CHANGES` → "needs a decision" (`normal`), `BLOCKED` → "needs a decision" (`high`). **Supersession:** when a current review exists for
  the task, it replaces that task's `verdict` row (the review embeds the validator result and links `validationId`), so the Human sees one row per task, not two. Leaves when
  stale (a push), superseded, or the Human `resolve`s it (a display action, not a verdict). Summary ≤ 160 chars, redacted: verdict, PR number, head7, failing gate names. A
  permission the policy **escalated** is the existing `permission` row (the card gains a line: the matched rule id + `why`, or "no rule matched", and the normalised command with
  heredocs elided — the "peek"); a policy-allowed or policy-denied request is **not** an inbox item (it is in the audit), so the inbox stays the list of what needs a person. The
  inbox gains no approve-all, no "always allow", no merge button (ADR-0045 holds). Setters stay refused for run-scoped agents and the `automation` origin.
- **Automations (ADR-0044).** No new source in v1. An automation may start a *Lead* (its pilot); that Lead's `done` run can trigger a steward review through §4 like any other
  Lead. An automation can never create a steward review directly, edit the policy, or answer a permission (identity check on `origin=automation`, a test).
- **Parent notifier (ADR-0041 B3).** The Lead's parent (human Steward or Human) is woken as today; the steward review is a child-less side task and does not notify the Lead's
  chat. Findings reach the Lead only when the **Human** copies them (the card's "copy findings as a prompt" is a clipboard action; `task.send` is the Human's, ADR-0041).
- **Push (ADR-0040).** No new kind. `permission` still pushes as today; reviews appear on the badge only (D13).
- **Plugins (ADR-0042).** A plugin hook cannot answer a permission or write a verdict; it may *read* `steward.updated` like any topic.

### 6. Optional advisory annotation (off by default)

`steward.annotate: false`. When on, the seat (or a cheaper second run) may call `steward_annotate {requestId, recommendation: approve|deny|unsure, reason ≤ 160}` for an
**escalated** pending request. The card shows it as "Steward advice (advisory)", visually separate from the rule line, with the same redaction; it **cannot answer, snooze,
change the policy, or extend the timeout**; the seat sees only the engine's normalised request, never the agent's prose. This exists because a person approving in bulk reads
less; it is the one place an LLM text touches a permission card, which is why it is off by default and ships last (S2b, after the dogfood).

### 7. Safety limits (all enforced in the daemon, not by prompts)

1. **No merge, no release, no tag, no `gh pr merge`**: no RPC, no MCP tool, no card button; the floor denies the commands for any agent.
2. **No writes to the Human's install state**: the floor's `no-install-state`; the steward's scratch tree is under the worktree root; the policy file has no write path.
3. **No secrets**: seat has no shell and no env; the digest carries counts, not values; audit and card text go through the redactor; secretdeny covers file reads.
4. **Budget**: §4 caps; the policy engine is free; a seat over its budget gets a daemon-written `BLOCKED: budget`.
5. **A seat never answers a permission** (ADR-0017): only the engine (a human-authored standing decision) and humans resolve requests; a test walks the MCP catalogue.
6. **Human-only configuration**: policy file, `steward.*` config, auto-review switch; none is an RPC; relay sessions cannot change them (there is nothing to change).
7. **Everything auditable**: policy decisions, reviews (digest sha, seat verdict, clamp), triggers; audit is append-only.

### 8. Wire and schema changes (additive)

- **Store (new tables only, appended to `schema.sql`, no `migrate.go` entry):** `permission_policy_audit` (§2.3) and `steward_reviews` (`id`, `taskId`, `triggerKind`,
  `headSha`, `contractSha256`, `validationId`, `digest` JSON, `digestSha256`, `seatVerdict`, `verdict`, `findings` JSON, `gates` JSON, `runId`, `tokenHash`, `status`
  `pending|running|done|blocked`, `blockedReason`, `costUsd`, `createdAt`, `finishedAt`). No new column on `tasks`; the steward's task is identified by the row (the
  validator's trick).
- **taskrunner:** two `PermissionResolution` values, `policy_allow`, `policy_deny`; `PermissionDetail` gains optional `policy {ruleId, why, normalized}`; web and mobile
  decoders must tolerate unknown resolutions (they already do for `auto_safe`).
- **RPCs:** `policy.get`, `policy.test {tool, command?, paths?, taskId?, seat?}` (dry run, explains each segment), `policy.audit` (reads, open incl. relay);
  `steward.review`, `steward.cancel` (Human only), `steward.get`, `steward.list` (reads, open incl. relay), `steward.verdict.submit` / `steward.annotate` (**token only**, the
  seat's run). Events: `steward.updated {}`, folded into `inbox.changed {}`. Nothing is added to `RunScopedMethods`.
- **Config (`config.yaml`, human-only):** `steward.enabled` (false), `steward.policy.enabled` (false), `steward.autoReview` (workspace list, empty), `steward.annotate` (false),
  `steward.maxBudgetUsd`, `steward.maxReviewsPerHour`.
- **Inbox model:** kind `review`; `links.reviewId`; the dedupe with `verdict` is in derivation, not stored.
- Amendment 2026-11-01 (Supervisor, Human may veto): steward.digest.get and steward.verdict.submit are run-scoped, bound to the seat's own task and run, token-checked; no other method added.

### 9. CLI and UI

- **CLI:** `smind policy init|show|test|audit` (`init` writes the commented template, refuses to overwrite; `test` explains a decision, `--json`); `smind steward review <task>
  [--sha]`, `steward list|show|cancel` (`--json`); `smind profile seed roles` seeds `steward`. No `steward approve`, no `steward merge`.
- **Web:** Inbox `review` row/card (verdict chip with icon + text, gates as a checklist with the failing ones first, findings grouped by file, evidence frames inline,
  "copy findings", "open PR", "resolve"); the permission card's rule line and peek; Settings → **Permission policy** (read-only: validity banner, rules as a table, last
  decisions from `policy.audit`, and the on-disk path to edit); no edit control. Light+dark, 1280 and 390, every transition frame reviewed (the protocol's UI rules).
- **Mobile:** the same rows in the Inbox, one column, 44 pt, the review card full screen, no hover-only action; policy audit read-only.

### 10. Options considered

1. **A Steward agent that approves permissions** (an LLM answering requests, or an MCP `task_approve` for a Steward seat). **Rejected**: it is exactly what ADR-0017 rules out,
   a prompt-injected request would be judged by a model reading attacker text, and the failure is unrecoverable (a push or a delete happened).
2. **Regexes over the command string** (Claude Code's `permissions.allow` style). **Rejected for v1**: `git push origin x; curl … | sh` matches `git push *`; structured
   per-segment matching with path resolution is explainable and testable (`policy.test`).
3. **Default-allow for read-only tools.** Rejected: ADR-0019 already lets the provider mode do that; smind's policy is the narrower thing the human opted into.
4. **Policy edited from the UI.** Rejected for v1: a UI write path is an agent/relay write path; the human edits a file (D6). A read-only view is cheap and ships in S3.
5. **Steward writes `CHANGES` straight into the Lead's chat.** Rejected: an agent steering an agent without a person is the "self-approving loop" the protocol forbids; the
   Human copies findings (D14). Revisit after the pilot with data.
6. **Put the steward in the validator.** Rejected: the validator's strength is that it sees only the contract; mixing protocol/process judgement in would make the contract
   body or the PR text part of its prompt (ADR-0045 AC10 forbids it).
7. **One engine for permissions and reviews.** Rejected: one is a synchronous deterministic decision on the hot path of every run, the other is a slow agentic judgement; they
   share only the audit conventions and the inbox.

## What we are NOT doing

- No auto-merge, merge RPC, merge button or "approve and merge"; no release, tag or publish by any agent or by the engine.
- No LLM in the permission decision; no agent (Lead, Peer, steward, automation, plugin) can answer, approve, snooze or dismiss a permission or edit the policy.
- No bulk approve, "always allow" or "remember this" on any card.
- No regex rules, no wildcard tool/seat/argv0 in an `allow`, no `allow` for a write without a path scope.
- No auto-send of review findings to a Lead; no auto-fix by the steward; no steward on a Peer's task alone.
- No change to provider permission modes (ADR-0019) or to the validator's authority; no new push kind; no sandbox claim (the provider mode and the floor are the boundary).
- No policy editing UI, no policy sync across devices, no per-user policies, no remote-managed policy; no cross-workspace review; no required-check/PR-comment integration.

## Consequences

- A Human who opts in gets the routine, safe approvals (their own tests, their own branch push) answered by rules they wrote and can audit, and sees only the requests worth
  reading, each with the matched rule and a readable command. Bulk-approval fatigue goes down; nothing that was `ask` becomes `allow` unless a human wrote the rule.
- Reviews become a first-class, commit-bound row with objective gates; the "ACCEPTED" the Steward said aloud is now a recorded, clamped, stale-aware item. The Human still
  merges.
- Cost: a seat run per review (default cap USD 1.5, six per hour at most) and a store table pair; the engine is microseconds.
- Risk accepted: policy parsing is a security-critical parser (mitigated by `ask` on any doubt, a fuzz test, a differential test against `bash -n`/`shfmt` parsing where
  available, and the floor); a same-user process can edit the policy file (stated, audited, #632 denies agents the path).
- Risk accepted: an LLM judge can be wrong; mitigated by gates it cannot raise past, the validator's separate verdict, and human merge.
- This ADR **amends** ADR-0019 §"smind never again decides by itself" (narrowly: a human-authored, default-off, path-scoped standing policy evaluated by the daemon) and leaves
  ADR-0017 intact for every agent.

## 11. Decisions taken as Supervisor defaults

**Supervisor default 2026-10-31 under the Human's directive, Human may veto.** Each line names what a veto changes.

| # | Decision | A veto changes |
|---|---|---|
| D1 | The policy engine is deterministic daemon code, human-authored rules, no LLM; agents never answer. **This amends ADR-0019's "smind adds no auto-approval of its own"** (the one decision needing the Human's explicit eye) | drops S1's allow path; keep audit + floor-as-`ask`-with-hint (S1 shrinks to the peek/audit) |
| D2 | Engine **off by default**; when on, unmatched = `ask`, 5-minute timeout = deny (audited) | S1 config default |
| D3 | Precedence `deny > ask > allow`; a compiled-in floor of denies no rule can override | S1 floor table |
| D4 | Compound, unparseable, unresolvable-path commands = `ask` | S1 parser |
| D5 | Structured patterns, no regex, no wildcards in `allow`; mutating `allow` needs a path scope | S1 loader |
| D6 | Policy is a human-edited file; no write RPC, no UI editor; invalid = floor + ask | S1/S3 |
| D7 | The seat is read-only: no Bash, no write, scratch worktree at the head sha, `claude-native` Sonnet 5.5, mode `default`, its own requests allow only Read/Grep/Glob in the scratch tree, rest `deny` | S2 profile + engine seat floor |
| D8 | The daemon computes the gates and **clamps** the seat's verdict; the seat cannot raise a verdict | S2 clamp |
| D9 | Verdict enum `ACCEPTED/CHANGES/BLOCKED`; commit-bound; stale on head move; never merge; the Human only `resolve`s | S2 |
| D10 | The `review` inbox row replaces the `verdict` row for the same task | S2 derive/S3 UI |
| D11 | Auto trigger is opt-in per workspace, off by default, root Lead `done` only; manual always available | S2 trigger |
| D12 | Advisory permission annotation exists but is off by default and ships last (S2b) | S2b |
| D13 | No new push kind | later opt-in |
| D14 | No auto-send of findings to a Lead; the Human copies them | later, after the pilot |
| D15 | Policy audit retention 90 days, redacted, append-only | S1 sweep |
| D16 | Rails: USD 1.5 / 20 min per review (ceilings 5 / 60), 6 per hour, 5 per task, auto-pause after 3 consecutive crashes/timeouts, dedupe by (head, contract, CI) | S2 config |
| D17 | `steward` is seeded as the fourth role-pack profile by `smind profile seed roles` (idempotent, never rewrites) | S2 |
| D18 | New tables only, no `migrate.go` entry, no change to `tasks` | S1/S2 schema |

## Questions for the Human

None block the docs PR; every question above is answered by a default the Human may veto. The two to read first: **D1** (a deterministic, human-authored allow path exists at
all) and **D11** (auto-review on `done`). Recommendation: accept D1 as written with the engine off until S4's pilot, and start S4 with the Human enabling the policy for one
workspace with the commented template's `go test`/`task lint`/own-branch-push allows only.
