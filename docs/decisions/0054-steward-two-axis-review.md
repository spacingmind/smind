# 0054: Steward review on two independent axes — Standards and Spec — in two fresh contexts

## Status

**Proposed — 2026-11-08.** Amends [ADR-0047](0047-steward-seat.md) §1, §3, §4, §8 and §9 (the seat, the review loop, the rails, the wire and the card). Everything else in
ADR-0047 stands: the policy engine, the gates, the clamp, the read-only seat, the Human merging. The Human asked for the review to run two independent passes; every choice
listed under [Decisions for the Human](#decisions-for-the-human) carries a recommendation and a veto changes only the PR named next to it. Spec, acceptance criteria and PR
split: `docs/plans/active/steward-two-axis.md`.

Inputs: ADR-0047 (seat, digest, gates, clamp), ADR-0045 (validator, verdict bound to a commit, inbox), ADR-0041 (seats, tree budget A5), ADR-0039 (forge status), ADR-0037
(secretdeny), `docs/WORKSPACE_PROTOCOL.md`.

## Context

### What the seat does today (evidence from the code)

One review is one row, one run, one judge:

- `steward.review` → `stewardHost.startReview` (`internal/wsapi/steward_seat.go`) collects the inputs, builds one `Digest` (≤ 64 KiB, `internal/steward/digest.go`), passes the
  rails (`internal/steward/rails.go`), inserts **one** `steward_reviews` row and calls `startSeat`.
- `startSeat` creates **one** scratch task (a child of the Lead's task, `WithBaseRef(sha)`), **one** chat, **one** one-time token (`steward.NewToken`, hashed in
  `steward_reviews.token_hash`, single use via `token_used`) and **one** run on profile `steward` (`claude-native`, `claude-sonnet-5-5`, mode `default`,
  `internal/profiles/rolepack.go`), with `taskrunner.PermissionSettings{AutoAccept: false}`; the seat's own permission requests resolve through the engine's seat floor
  (Read/Grep/Glob inside the scratch tree allowed, the rest `deny`).
- The seat sees the digest through `steward_digest_get` and the scratch tree through `Read/Grep/Glob`. `BuildPrompt` (`internal/steward/prompt.go`) gives it one prompt: the gate
  states, the adversarial checklist (security first, then data loss, races, tests that test nothing, scope creep), "look at every frame", the verdict contract.
- It submits **one** `SeatVerdict` (`internal/steward/verdict.go`: `verdict`, `summary`, `findings[]`, `gates[]` acks, `frames[]` observations, `blockedReason`).
  `CheckSeatVerdict` enforces completeness (ACCEPTED: every gate acknowledged, an observation per frame, no P0/P1 or unrated finding; CHANGES: findings with file, line, why and the
  check; BLOCKED: a reason). `Clamp` (`internal/steward/clamp.go`) stores `min(seatVerdict, gate caps)`; an unknown gate holds the review; the daemon never writes `ACCEPTED`.
- One `Watch` (`internal/steward/watch.go`) enforces `budgetUsd` (default USD 1.5, ceiling `maxBudgetUsd` 5) and `maxMinutes` (default 20, ceiling 60) on that one run, writes
  `BLOCKED` with `timeout|budget|crashed|scope_violation|cancelled`, and keeps the Lead's worktree snapshot check.
- Rails count **reviews**: single flight per task, dedupe by `(headSha, contractSha, ciState)`, `maxReviewsPerHour` 6, `maxReviewsPerTask` 5, auto-pause after 3 consecutive
  `crashed`/`timeout` reviews.
- Surfaces: `View` (`steward.get|list`), the inbox `review` row (`internal/inbox`), the web card (`web/packages/ui/src/components/inbox/steward-review-card.tsx`,
  `lib/steward-types.ts`), the mobile review screen (`mobile/app/review/[id].tsx`, `mobile/src/inbox/types.ts`) and `smind steward show` (`cmd/smind/steward.go`).

### The problem

The one pass does two different jobs in one context: "does this follow the repository's written rules" (a checklist, cheap, well bounded) and "does this do what was asked"
(needs the spec, the diff and judgement). One context that has formed an opinion on the first tends to anchor the second, a model strong enough for the second is wasted on the
first, and a finding does not say which question it answers. The Human wants two independent reads and per-axis visibility.

## Decision

**A review runs two passes, STANDARDS and SPEC, as two separate seat runs in two fresh contexts, started together, judged by the same daemon gates and the same clamp.**
Everything that decides the stored verdict stays in the daemon.

### 1. One run or two (the evidence)

Two passes inside one run cannot be independent: the second pass reads the first pass's conclusions from the same context, and the token, scratch tree, `Watch`, budget and
`steward_task_id` are all one-per-review in the code above, so "two passes in one run" would also leave nothing to enforce a per-pass budget or timeout on. Two runs reuse the
existing seat mechanics unchanged per pass (token, scratch worktree, watch, seat floor) and add a pass table. **Decision: two runs** (D1).

### 2. Model: the aggregate row and a pass row

`steward_reviews` stays the aggregate (one row per `(task, head)`, the clamp, the inbox, staleness, mergeReady). A new table holds one row per pass:

```sql
-- steward_review_passes: one seat run of one review on one axis (ADR-0054); additive.
CREATE TABLE IF NOT EXISTS steward_review_passes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    review_id INTEGER NOT NULL REFERENCES steward_reviews(id),
    axis TEXT NOT NULL,                       -- standards | spec
    status TEXT NOT NULL,                     -- pending | running | done | blocked | skipped
    profile TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    run_id TEXT NOT NULL DEFAULT '',
    steward_task_id INTEGER NOT NULL DEFAULT 0,
    scratch_dir TEXT NOT NULL DEFAULT '',
    token_hash TEXT NOT NULL DEFAULT '',
    token_used INTEGER NOT NULL DEFAULT 0,
    seat_verdict TEXT NOT NULL DEFAULT '',    -- '' | ACCEPTED | CHANGES | BLOCKED (what this pass submitted)
    summary TEXT NOT NULL DEFAULT '',
    findings_json TEXT NOT NULL DEFAULT '',
    criteria_json TEXT NOT NULL DEFAULT '',   -- spec axis only
    blocked_reason TEXT NOT NULL DEFAULT '',  -- timeout | budget | crashed | cancelled | scope_violation | no_spec
    cost_usd REAL NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL,
    finished_at TIMESTAMP,
    UNIQUE (review_id, axis)
);
```

New table only, appended to `schema.sql`, no `migrate.go` entry; no column on `steward_reviews` is removed or reinterpreted (the existing `token_hash`, `token_used`, `run_id`,
`steward_task_id`, `scratch_dir` columns stay for the single-pass mode and are empty for a two-axis review, which keeps `twoAxis: false` byte for byte today's). With the feature
on, `steward_reviews.seat_verdict` stores the **combined** seat verdict (§6), `cost_usd` the sum of the passes, `status` the aggregate (`running` until both passes are terminal).

### 3. The digest gains two optional sections; the gates do not change

The daemon builds one digest as today (deterministic, no agent, ≤ 64 KiB, the same shrink order with the new sections shrunk before the existing ones). It gains, **only when
`steward.twoAxis` is on**:

```jsonc
"standards": {
  "baseRef": "<merge-base sha>",
  "files": [ {"path":"AGENTS.md","sha256":"…","bytes":4180,"truncated":false,"changedInDiff":false} ],   // standards files at the BASE ref, in this fixed order
  "headDiff": "…",                                                                    // diff of those files base→head, ≤ 4 KiB, under untrusted
  "rules": [ "…" ]                                                                    // steward.standards.rules from config.yaml (human-authored), each ≤ 400 B, ≤ 20
},
"spec": {
  "sources": [ {"kind":"plan|pr_body|issue|adr|contract","ref":"docs/plans/active/x.md","status":"found|missing|unavailable","sha256":"…"} ],
  "criteria": [ {"id":"AC3","text":"…(≤ 300 B)","namedTests":["TestFoo"],"source":"plan"} ],   // ≤ 60 items
  "namedTestsInTree": {"TestFoo": true}                                                // a hint: the name exists as a test function at headSha, not a gate
}
```

- **Standards files** are discovered by a fixed, ordered list in the daemon: `AGENTS.md`, `CLAUDE.md`, `CONTRIBUTING.md`, `docs/CONTRIBUTING.md`, `.github/PULL_REQUEST_TEMPLATE.md`,
  plus files named by `steward.standards.files` (config, human-only, paths inside the repo, resolved with `realpath`, symlinks out of the tree rejected). **The bar is the base, not the head (D11):** the files are read at the **merge-base of the PR with its base branch** (`git merge-base origin/<base> headSha`), never at head. The daemon
  copies those base versions into a read-only directory `.smind-standards/` inside the Standards pass's own scratch tree before the seat starts (outside the git index, so it is never part
  of the diff) and the prompt tells the seat to judge against that directory only. The digest carries path, size and hash of the **base** text, and `headDiff` shows what the PR changes in
  those files, as data. A PR that edits `AGENTS.md` is therefore judged by the old `AGENTS.md`, and the edit itself is a finding the Human sees, never a way to lower the bar. When the
  base ref cannot be resolved the pass is `blocked` (`crashed`), never silently judged against head. The Spec pass is unaffected. `steward.standards.rules` are extra one-line rules the Human wants enforced; they are
  config text, not agent text, and still sit in a delimited block.
- **Spec resolution is the daemon's, deterministic, in this order**: (1) the task's validation contract (ADR-0045) acceptance criteria and named tests; (2) the plan file the
  digest already knows (`Inputs.PlanFile`) and its `## Acceptance Criteria` / `## Test Scenarios` sections; (3) the PR body: a `Spec:` line, `Closes|Fixes|Refs #N`, and `ADR-NNNN`
  mentions; an issue is fetched with the forge client of ADR-0039 (title and body only, capped) and listed as `unavailable` when there is no forge or the call fails; an ADR mention
  is resolved to `docs/decisions/NNNN-*.md` at headSha or marked `missing`. Criteria are extracted by a fixed parser (lines `ACn …`, bullets under the two headings, contract
  entries); free text found in an issue or PR body is quoted under `untrusted`, never promoted to an instruction. The seat has no network and no shell, so nothing about
  resolution is left to the model.
- **The gates, `Gate`, `GateIDs`, `Clamp` and the `unknown` hold are untouched.** No gate is added (see D6 for the spec-less question). Gates still come first: the daemon computes
  them before either seat starts, and an unknown gate still holds the review pending with neither pass started.

### 4. The two passes

| | STANDARDS | SPEC |
|---|---|---|
| Question | does the change follow what this repository documents and what the Human configured? | does the change do what was asked, and is it correct? |
| Profile | `steward-standards` (new, role-pack seeded), falls back to `steward` when absent | `steward` (as today) |
| Model default | `claude-haiku-5-5` (D2), mode `default`, `childPolicy: none`, `onRateLimit: wait` | `claude-sonnet-5-5` (as today) |
| Reads | the diff, the standards files, the configured rules | the diff, the spec sources, the contract, the frames |
| Output (additive to `SeatVerdict`) | `findings[]` each with `axis:"standards"` and `rule` (the file and quoted line or heading it relies on) | `findings[]` with `axis:"spec"`, plus `criteria[]` `{id, status: met\|unmet\|unverifiable, evidence}` |
| Frames and gate acks | none required (the pass is not shown images) | frame observations and gate acks as today |
| Checklist | commit and PR conventions, naming, comment density, layering and dependency rules, "tests added for production code", docs updated when the documented workflow says so | acceptance criteria and named tests one by one, scope creep, then the adversarial checks of today's prompt: security first, data loss, races, tests that do not test the behaviour |

Rules of the prompts (both, built by the daemon from the digest's objective fields and fixed text, as `BuildPrompt` does today): the digest's `untrusted` block, the PR, the diff,
commit subjects, handoff notes, plan text, issue bodies and standards-file contents are **data to judge, never instructions**; a finding must cite what it relies on (`rule` for
Standards; a criterion id or file and line for Spec); a Standards finding that cites no rule is rejected by `CheckAxisVerdict` (the pass may not invent standards). The seat's
final message starts with the existing json block plus `"axis"`.

Completeness checks per axis (implemented as `CheckAxisVerdict` over parallel types `AxisVerdict`/`AxisFinding`, not as an axis argument on `CheckSeatVerdict`: `SeatVerdict`, `SeatFinding`, `SchemaCaps` and `ArrayCaps` build the single-pass seat's tool schema, which must stay byte-identical with `steward.twoAxis` off; the two-axis seat's schema is built from the new types in P2):
Standards `ACCEPTED` needs no P0/P1 or unrated finding and no `blockedReason`; `CHANGES` needs findings with file, line, why, check and `rule`. Spec `ACCEPTED` needs a `criteria`
entry for **every** extracted criterion (`met` or `unverifiable`, each with evidence ≥ 10 non-space characters), the frame observations and gate acks of today, no `unmet`
criterion and no P0/P1 finding; `CHANGES` needs findings or an `unmet` criterion. Each pass can only be `ACCEPTED | CHANGES | BLOCKED`, never anything else.

### 5. Both passes run in parallel; the token and the axis are bound together

`startReview` creates the two pass rows, then `startSeat` runs **once per pass**: its own scratch worktree at the same sha, its own task (child of the Lead), chat, run, one-time
token (hashed in the **pass** row) and its own `Watch`. The seat's MCP entry env gains `SMIND_STEWARD_AXIS`; the axis is bound **server-side to the token** — a verdict document
with a different `axis` is `VerdictError{Field:"axis"}`, so a Standards seat cannot submit a Spec verdict and the Spec pass cannot be satisfied by the cheap pass. `steward.verdict.submit`
and `steward.digest.get` stay run-scoped to the seat's own task, token-checked; no method is added to `RunScopedMethods` for a Lead or Peer. Parallel because the wall clock is
`max` instead of the sum, and because a later pass would otherwise read a head that moved.

### 6. Combining: min over axes, then the unchanged clamp

```
seatCombined = min(Rank) over the passes that submitted a verdict   // BLOCKED < CHANGES < ACCEPTED
stored       = Clamp(seatCombined, gates)                           // internal/steward/clamp.go, unchanged
```

- A pass can only **lower** the combined value: it cannot raise it, and neither pass can clear a gate. The stored `verdict` is still `min(seat, gates)` and the daemon still never
  writes `ACCEPTED`.
- **Disagreement** (one axis `ACCEPTED`, the other `CHANGES`) is the normal case, not an error: the stored verdict is `CHANGES`, both axes' verdicts and findings are shown, and there
  is no third "reconciling" run. The card says which axis lowered it.
- **A failed axis** (a pass `blocked` with `timeout`, `budget`, `crashed`, `cancelled` or `scope_violation`): the review is never `ACCEPTED`. With D4 as recommended, the combined seat
  verdict is `min(other axis, CHANGES)` with the reason `axis <name> did not complete` appended to the summary; if no pass produced a verdict (both failed, or one failed and the other was skipped) it is `BLOCKED` with the first failed pass's reason, and `crashed` as the fallback when none carries one. Daemon-written
  reasons stay the ones in `DaemonBlockedReasons`. `scope_violation` (the Lead worktree changed during either pass) blocks the whole review as today: the review is `BLOCKED` with reason `scope_violation` at once, without waiting for the other pass, and the review's scope-violation flag is set.
- **No spec found** (`spec.sources` empty or every source `missing`/`unavailable`, and no criteria): the Spec pass is **not run**; its pass row is `skipped` with the daemon-written
  summary `no spec available`, no cost. The axis is reported as `n/a` and does not count as an `ACCEPTED` for the other axis. Whether the missing spec lowers the verdict is D5.
- Rails: a review still counts once toward `maxReviewsPerHour` / `maxReviewsPerTask`; auto-pause counts a review as `crashed`/`timeout` only when **every** non-skipped pass was.

### 7. Cost

Today a review is capped at one run: `steward.budgetUsd` USD 1.5 (ceiling `maxBudgetUsd` 5), `steward.maxMinutes` 20 (ceiling 60). New, all human-edited config:

| key | meaning | default | ceiling |
|---|---|---|---|
| `steward.twoAxis` | the switch; off = today's single run, byte for byte | `false` | — |
| `steward.budgetUsd` | **total cap of a review, both passes** (the existing key keeps its meaning and default) | 1.5 | `maxBudgetUsd` (5) |
| `steward.passes.standards.budgetUsd` | cap of the Standards pass | 0.4 | the total cap |
| `steward.passes.spec.budgetUsd` | cap of the Spec pass | 1.1 | the total cap |
| `steward.passes.<axis>.maxMinutes` | wall clock of that pass, rate-limit waits excluded as today | 20 | 60 |
| `steward.passes.standards.profile` / `.spec.profile` | seat profile per axis | `steward-standards` / `steward` | — |

Config load fails when the two pass budgets sum to more than the total cap, so per-pass caps **bound the sum** and no second watcher is needed (a pass over its own cap is
`BLOCKED: budget` as today). An agent trigger and a Human trigger follow ADR-0047 §4: the review is refused **before** either pass starts when the root's tree budget (ADR-0041
A5) is already reached for an agent trigger, never for a Human one; the passes themselves are `human`-origin runs (as the seat is today), so A5 does not refuse the second pass
mid-review, and both costs roll into the tree like any child's. Review cost for the tree budget is therefore up to USD 1.5 as today; it is not multiplied by two.

| | passes | typical cost | cap | wall clock |
|---|---|---|---|---|
| today | 1 × Sonnet 5.5 | unmeasured here (pilot rows in `steward_reviews.cost_usd`) | 1.5 | up to 20 min |
| two-axis, recommended | Haiku 5.5 + Sonnet 5.5 in parallel | to be measured in the pilot; the Standards pass reads only a diff and text files | 0.4 + 1.1 = 1.5 | up to 20 min (parallel) |
| two-axis, both Sonnet | 2 × Sonnet 5.5 | about the sum of two reads | 1.5 (e.g. 0.75 + 0.75) | up to 20 min |

No price is quoted from memory: the Usage page already prices runs, and the PR that ships the feature records the measured cost per axis from the pilot.

### 8. Wire and schema (all additive; an old client ignores every new field)

- **Store:** `steward_review_passes` above; nothing else changes.
- **`steward.View`** (`steward.get`, `steward.list`): `axes: ReviewAxis[]` (absent when the review ran single-pass), with
  `ReviewAxis{axis:"standards"|"spec", status:"pending|running|done|blocked|skipped", seatVerdict, summary, blockedReason, model, costUsd, runId, stewardTaskId, criteria?, specSources?}`;
  `findings[].axis` and `findings[].rule` (both optional strings). `verdict`, `seatVerdict`, `summary`, `findings`, `gates`, `mergeReady`, `stale` keep today's meaning (the combined
  and clamped values), so an old client renders a coherent single-axis review.
- **`SeatVerdict`** (seat → daemon): optional `axis` (must equal the token's axis), optional `criteria[]` `{id, status, evidence}`; `SeatFinding` gains optional `axis`, `rule`.
  `ParseSeatVerdict` stays strict (unknown fields still rejected) and the new fields are added to the struct, the caps (`SchemaCaps`/`ArrayCaps`) and the tool schema.
- **Inbox:** the `review` item gains an optional `axes: [{axis, verdict}]` next to today's verdict (`internal/inbox`), summary still ≤ 160 chars and redacted; supersession, staleness
  and `mergeReady` are unchanged.
- **RPCs and events:** none added; `steward.review` gains an optional `axes?: ["standards"|"spec"]` (Human and daemon trigger only, default both) so a single axis can be re-run from
  the CLI; the existing `steward.updated {}` fires on each pass change.
- **Config:** `steward.twoAxis`, `steward.passes.*`, `steward.standards.files|rules` above; all human-only, none an RPC.
- **Role pack:** a fifth profile `steward-standards` (read-only, claude-native, `claude-haiku-5-5`, `default`, `childPolicy: none`), seeded idempotently by `smind profile seed roles`
  as today; the instructions text differs per axis, the tool lists are the same two MCP tools.

### 9. UI, CLI

- **Web card:** the verdict chip stays the stored (combined, clamped) verdict. Under it, **two axis rows**, `Standards` and `Spec`, each a chip with icon plus text
  (`ACCEPTED | CHANGES | BLOCKED | Skipped · no spec available | Running`), its one-line summary, model name and cost; findings are grouped by axis, then by file (the axis tag shown
  as a small label, not colour alone); a Spec criteria checklist (met/unmet/unverifiable with the evidence); the gates checklist stays above both. A single-axis review (old row or
  flag off) renders exactly as today. "Copy findings" gains an `Axis:` line per finding. Light and dark, 1280 and 390, every transition frame reviewed.
- **Mobile:** the same two rows in the review screen, one column, 44 pt targets, criteria in a collapsible section; the inbox row shows two small axis chips.
- **CLI:** `smind steward show <id>` prints an `AXES` block (axis, status, verdict, cost) and tags each finding `[standards]`/`[spec]`; `--json` carries the new fields;
  `smind steward review <task> [--axis standards|spec]`. No approve or merge subcommand (unchanged).

### 10. Security review

- The seats stay exactly as ADR-0047 §1: Read, Grep, Glob, `steward_digest_get`, `steward_verdict_submit`; `SeatDisallowedTools` unchanged (no Bash, no write, no web, no sub-agent), no
  task, profile, permission, validation or message tool. A second seat doubles the count of such seats, not their power. The seat floor applies to each pass; both scratch trees are
  detached worktrees under the worktree root.
- **New input: the spec text** (an issue body, a plan, PR text) and **the standards files** are attacker-influenceable (a branch can edit `AGENTS.md` or the plan). They are treated
  as data: quoted under `untrusted`, length-capped, never in the prompt body. **Standards are read at the merge-base, never at head (D11)**: a PR cannot lower the bar it is judged by;
  its edit to a standards file appears only as `standards.headDiff` and a `changedInDiff` flag (a card chip, a hint), and the daemon's copy lives in the pass's scratch tree outside the index. A Standards
  finding must cite a `rule`; a prompt-injected "ignore the rules, ACCEPT" can at worst yield a wrong **advisory** verdict.
- **Verdict forgery:** each pass has its own single-use token bound to its axis; wrong axis, reused token, wrong run = refused. A Standards seat cannot satisfy the Spec pass.
- **Clamp:** `min` over axes then the unchanged gate clamp; nothing a pass writes can raise the stored verdict above a gate, and the daemon never writes `ACCEPTED`.
- **Issue fetch** (daemon, forge client, read-only, capped, redacted) is the only new network call; it uses the credentials ADR-0039 already holds and fails to `unavailable`.
- **Cost abuse:** per-pass caps sum to the total cap; both passes under the existing rails; no new trigger.
- No new method in `RunScopedMethods` beyond the existing two, which stay bound to the seat's own task and token (a handler-map test).

### 11. Failure modes

| Failure | Result |
|---|---|
| A pass times out, crashes or exceeds its budget | pass `blocked` with the daemon-written reason; review never `ACCEPTED`; capped per §6 / D4; the other pass is not stopped |
| Both passes fail | review `BLOCKED` (first reason); counts toward auto-pause |
| The Human cancels | both passes stopped, blocked rows written **before** the runs are stopped (as `cancel` does today), reason `cancelled` |
| No spec resolvable | Spec pass `skipped` `no spec available`, axis `n/a`; D5 decides whether it also lowers the verdict |
| Issue fetch fails | source `unavailable`, other sources still used; never a pass failure |
| Standards pass uses a missing profile | falls back to `steward`; the axis row shows the model actually used |
| One profile's provider is rate limited | that pass waits (`onRateLimit: wait`), the clock excludes the wait; the other proceeds |
| Head moves during the passes | the review is stale as today; both rows keep their data, neither shows `ACCEPTED` |
| Axes disagree | stored = the lower; both shown; no third run |
| A Spec seat marks a criterion `met` that the daemon's `namedTestsInTree` says does not exist | the verdict is accepted by the schema but the card shows the contradiction ("test not found"); it is a hint for the Human, not a gate |
| Flag turned off mid-review | the in-flight review finishes in its mode; new ones are single-pass |

### 12. Rollout

Behind `steward.twoAxis` (default **false**; invalid config = off). No data migration: old reviews have no pass rows and render as today. The pilot is the Human's, on one
workspace with `claude-haiku-5-5` for Standards; the first real runs record the per-axis cost in `steward_review_passes.cost_usd` so D2 and D3 are settled by data. The new default
(on) is a later, separate decision after the pilot.

## Test plan (named; the spec lists them against the acceptance criteria)

Daemon (`internal/steward`, `internal/store`, `internal/wsapi`, `internal/config`, `internal/profiles`, `internal/inbox`): `TestCombine_MinOverAxes`,
`TestCombine_AxisCannotRaiseAboveGates`, `TestCombine_FailedAxisNeverAccepted`, `TestCombine_BothFailedIsBlocked`, `TestClamp_UnchangedForSinglePass`,
`TestSeatVerdict_AxisMustMatchToken`, `TestSeatVerdict_StandardsFindingNeedsRule`, `TestSeatVerdict_SpecAcceptedNeedsEveryCriterion`, `TestSeatVerdict_UnknownFieldsStillRejected`,
`TestDigest_StandardsFilesOrderedHashedAndCapped`, `TestDigest_StandardsFileSymlinkOutOfTreeRejected`, `TestStandards_ReadAtMergeBaseNotHead` (a PR editing `AGENTS.md` is judged against the base text; the head edit appears only in `headDiff`), `TestStandards_BaseRefUnresolvedBlocksPass`, `TestDigest_SpecSourcesResolveInOrder`, `TestDigest_SpecIssueUnavailableIsListedNotFatal`,
`TestDigest_NoSpecSkipsSpecPass`, `TestDigest_TwoAxisOffIsByteIdentical` (golden), `TestPrompt_UntrustedInjectionNeverReachesPromptBody` (per axis), `TestStartReview_TwoAxisStartsTwoSeatsOwnTokens`,
`TestStartReview_GateUnknownStartsNeitherPass`, `TestWatch_PassBudgetsSumWithinTotal`, `TestConfig_PassBudgetsOverTotalRejected`, `TestCancel_StopsBothPassesBlockedRowsFirst`,
`TestRails_ReviewCountsOnceNotPerPass`, `TestAutoPause_CountsReviewNotPass`, `TestRunScopedMethods_NoNewSteward` (handler-map walk), `TestSeatCatalogue_NoShellNoWriteBothAxes`,
`TestStore_PassRowsUniquePerAxis`, `TestView_AxesAbsentWhenSinglePass`, `TestInbox_ReviewItemCarriesAxesAdditively`, `TestRolePack_SeedsStewardStandardsIdempotently`.
Web and mobile (`web/packages/ui`, `mobile`): `steward-review-card.test.tsx` (two axis rows, skipped state, disagreement, criteria list, old single-axis payload unchanged, findings
grouped by axis then file, copy-findings carries the axis), `steward-types.test.ts` (decoders tolerate missing and unknown axis fields), mobile smoke stage for the review screen.
CLI: `TestCLI_StewardShowPrintsAxes`, `TestCLI_StewardReviewAxisFlag`.
End to end (fake LLM, isolated `SMIND_HOME`): both passes ACCEPT → one `review` row ACCEPTED; Standards CHANGES + Spec ACCEPTED → CHANGES; CI failing + both ACCEPT → CHANGES; one pass
killed → never ACCEPTED; no spec → Spec `skipped`. One real run with `claude-haiku-5-5` on a throwaway repo only, by the Human's pilot.

## Options considered

1. **Two passes inside one run.** Rejected: not independent (same context), and there is nothing to enforce a per-pass budget or timeout on (§1).
2. **A third "reconciling" run when the axes disagree.** Rejected: more cost, and an LLM deciding between two LLMs adds a place where the clamp's `min` could be argued away. `min` is explainable.
3. **A new gate `spec_present`.** Held as D5's alternative, not the default: many legitimate PRs (docs, chores) have no spec and a failing gate caps them at `CHANGES`.
4. **Shared scratch worktree for both passes.** Rejected: one fewer checkout is not worth two seats able to see each other's files; seats are read-only so the saving is small.
5. **Standards pass without a model (a linter).** Rejected for this ADR: the repo's documented standards are prose; the deterministic parts are already gates (`pr_hygiene`, `no_churn`).
6. **Sequential passes (Standards first, Spec only if it passes).** Rejected as the default: saves cost on failing PRs but doubles the wall clock and lets the first verdict anchor the second pass's prompt. Cheap to add later behind a flag if the pilot shows Standards fails often.

## What we are NOT doing

- No change to the gates, the clamp, `DaemonBlockedReasons`, the verdict enum, the policy engine, or "the Human merges"; no merge, approve or auto-send button.
- No new tool for any seat; no network, shell or write for a seat; no LLM in a permission decision.
- No standards authored by the seat, no standards edited by an RPC (config is a human edit), no auto-fix, no auto-send of findings to a Lead.
- No third axis, no per-axis threshold config in v1, no change to the validator seat or its contract.

## Consequences

- Findings say which question they answer; the Human can read "the code follows the rules but misses AC4" at a glance, and a cheap model does the rule-checking.
- Cost per review stays within today's cap (1.5) by construction, but a review is two seat runs: more tasks in the tree (two scratch children) and more rate-limit exposure on one provider.
- A Standards pass on a small model may produce weaker or noisier findings; the `rule` citation requirement and the Human's veto on D2 are the controls, and the pilot measures it.
- A PR that edits the standards files is judged against the **base** text (D11); the edit is shown as a diff and flagged, and the Human decides whether to accept the new standard.

## Decisions for the Human

Each has a **recommendation**; a veto changes the PR in the last column.

| # | Question | Recommendation | A veto changes |
|---|---|---|---|
| D1 | Two seat runs, or two passes in one run? | **Two runs, in parallel.** One run cannot be independent and has no per-pass budget or timeout (§1). | P2 |
| D2 | Default model for Standards | **`claude-haiku-5-5`** via a new `steward-standards` profile (falls back to `steward`); Spec stays Sonnet 5.5. Pilot decides; a config edit switches it. | role-pack seed (P2) |
| D3 | Budget split and total | **Total stays USD 1.5; Standards 0.4, Spec 1.1;** load error if the pass caps sum above the total; wall clock 20 min per pass, parallel. | config defaults (P2) |
| D4 | Does a failed axis block the verdict? | **It never allows `ACCEPTED`; it caps at `CHANGES`** (`BLOCKED` only if both fail), with the failed axis named. Re-run with `--axis`. | combine function (P1) |
| D5 | Are spec-less PRs flagged? | **Report only:** Spec shows `Skipped · no spec available`, no gate, no cap. Alternative: a `spec_present` gate capping at `CHANGES`. | P2 (one gate) |
| D6 | Where does the security/data-loss checklist live? | **In the Spec pass** (the stronger model); Standards keeps conventions and hygiene. Alternative: in both (double cost, redundant findings). | prompts (P2) |
| D7 | Are frame observations and gate acknowledgements required of the Spec pass only? | **Yes, Spec only** (the Standards pass is not shown images and needs no gate acks). | verdict checks (P1) |
| D8 | Rails: a review counts once, not twice | **Once** against `maxReviewsPerHour` / `maxReviewsPerTask`; auto-pause counts a review. | rails (P2) |
| D9 | Ship flag default | **`steward.twoAxis: false`** until the Human's pilot; the default flips in its own PR after measured cost. | config default |
| D10 | Does a PR that edits `AGENTS.md`-class standards files get a visible warning? | **Yes**, a hint chip on the card from `standards.changedInDiff`, not a gate. | P2/P3 |
| D11 | At which ref does the Standards pass read the standards files? | **The merge-base, not head**; the head change is shown as a diff and flagged, so a PR cannot lower the bar it is judged by (§3, §10). | P2 |
