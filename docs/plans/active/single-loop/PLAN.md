# Single loop plan

Audit date: 2026-10-25
Repo and commit: smind, `origin/develop` at `79f9b94e` (role pack at `internal/profiles/rolepack.go`, ADR-0041 in `docs/decisions/0041-in-smind-orchestration.md`)

This is the working folder of the `single-agent-loop` skill (`.agents/skills/single-agent-loop/`). The skill calls it `single-loop/`; in this repo it lives at `docs/plans/active/single-loop/` so it is committed with the plans. Raw exports and traces stay out of git in `docs/research/local/single-loop/` (gitignored; cited here as local-only).

System audited: smind's in-smind tree orchestration as used to develop smind itself: Supervisor seat -> Lead seats -> Peer seats (ADR-0041), role pack `peer-sonnet`, `peer-glm`, `lead`, `reviewer`, `supervisor` (+ the hand-made `supervisor-sonnet`).

Outside the system, so no rows and nothing to fold: the **Human** (goals, merges, releases) and the **Steward**, an external Paseo agent that plays Human and Lead-of-record: it sends briefs to the Supervisor seat (task 45) or straight to a Lead, reads every PR adversarially, approves seat pushes and reports verdicts (`docs/research/local/handoff-steward-2026-10-25.md`, local-only). The Supervisor session that delegated this audit is on this side too.

Step 0 changed no code, profile or prompt, and ran no model.

## Status

- [x] Verdicts agreed with the person (end of step 0): Human approved all verdicts as recommended, 2026-10-25
- [x] Main eval agreed and baseline measured (end of step 1): **Step 1 (eval) skipped by Human decision 2026-10-25**; changes applied directly, checked by the lightweight After-check below instead
- [x] Changes 1-5 applied (PR: see "Changes")
- [ ] After-check table filled from the next ~5 items briefed directly (keeps this plan in `active/`)

A new session picks up at the first unticked box here, then at the first unticked item in
the worklist.

## Verdicts

| Agent | Verdict | Why, in one sentence | Evidence | Confidence |
| --- | --- | --- | --- | --- |
| lead | main | It already owns a work item end to end (spec, delegation, review, PR) and is the seat that did the work in every Lead-only item. | `rolepack.go:49-61`; `WORKSPACE_PROTOCOL.md:24-32`; direct hop to Lead 105: 17m19s, USD 2.01, PR #611 (`dogfood-2026-10-22.md:154`); Lead-only items 52, 68, 82, 90 (traces) | medium |
| supervisor-sonnet | fold | It routes and relays: 24 of its 43 runs are 16 s wake-ups that pass a Lead's report on, and nothing it does needs its own context. | traces: 43 of 191 runs, 8% of cost; `dogfood-2026-10-22.md:113,120,154-155` (entries 62, 66 and the direct-hop note); `WORKSPACE_PROTOCOL.md:15-20,53` vs `smind-supervisor/SKILL.md:9` | medium |
| supervisor | fold | Same seat as `supervisor-sonnet` with no model; the pack seeds a profile the daemon never uses. | `rolepack.go:100-109`; `handoff-steward-2026-10-25.md:21`; 0 runs found | low |
| peer-sonnet | as-tool | Screenshot and test loops read far more than the Lead needs afterwards and parallel worktrees help, but its free-text return is cut to 1 KiB and re-checked by hand. | traces: 55 runs, 52% of cost, tasks 63, 110, 111 (USD 3.02-7.31, 27-40 min); `childevent_text.go:12`; `smind-peer/SKILL.md:20-26` | medium |
| peer-glm | fold | One run in 191 (28m31s, 3.0M input tokens vs ~1.5 min on Sonnet); a model is a profile setting, not a separate agent. | task 48; `dogfood-2026-10-22.md:118` (entry 64); `rolepack.go:55,80` | medium |
| reviewer | as-tool | Independent read-only review of an exact commit earns its own context on paper, but no run of it was found. | `rolepack.go:92-99`; `WORKSPACE_PROTOCOL.md:143-144`; 0 identified runs (inferred) | low |

Why no merge pair, although `coupling_map.py` flags four: `lead` <-> `peer-sonnet` (coupling 100% and 95%) is sequential by construction (every Peer end wakes the Lead, `internal/runs/parentnotify.go:83`) and Peers pass the earned-context test, so they stay separate; `supervisor` -> `lead` (86% over 19 items) is the fold in worklist item 1, done as one change because the Supervisor row is thin.

Verdicts least sure of: `reviewer` and `supervisor` (use inferred, not observed), and `main` on `lead` (a judgement: the router by position is the Supervisor, the single loop by function is the Lead).

Glue that goes away as subagents fold (routers, classifiers, planners, context passing):

- The Lead -> Supervisor `child_event` wake hop: with the Supervisor gone, the Human/Steward hears about a finished tree through attention and push (ADR-0040), not through a model run. Removes 24 wake runs (USD 2.29) and the 4 zero-token wakes seen when the account limit hit (traces tasks 45, 46, 45, 47). Source: the quiescence rule for a root's wake, `internal/runs/parentnotify.go:83-163`.
- Supervisor-only configuration: `childPolicy: profiles` + `childProfileIds` for the seat (`rolepack.go:103`, the `profiles` branch of `mayLaunch`, `internal/wsapi/admission.go:102-113`), the `supervisor` and `supervisor-sonnet` profiles, the `smind-supervisor` skill and the sizing/launch/report duties in `docs/WORKSPACE_PROTOCOL.md:15-20,37-59`. The sizing table and the brief template move to the entry side (Human/Steward) and a short brief template.
- One tree level: `orchestration.maxDepth` 2 was spent exactly on Supervisor -> Lead -> Peer (ADR-0041); Lead -> Peer needs one.

Glue that stays (it is the channel the Lead <-> Peer loop and the Human rely on, and none of it is a subagent):

- The parent notifier for Peer -> Lead (`parentnotify.go`, `internal/store/child_event.go`, `internal/runs/childevent_text.go`): with Peers as tools it is the asynchronous tool-result delivery.
- Sticky seats (`internal/runs/queue.go:722`) and the profile copied into wake runs (`parentnotify.go:138`): both exist because a seat that loses its profile loses its role text and its MCP entry (dogfood entries 60, 61).
- Admission with `childPolicy: leaf` and the own-subtree check (`admission.go:102-170`), the per-tree concurrency guard (`internal/runs/tree_guard.go:13`, config `orchestration.maxConcurrentRunsPerTree`, 6 on the dogfood daemon), `task_new` with the parent filled in from the run identity (`internal/runs/contract.go:104-115`).
- The 25-tool MCP catalog each orchestrating seat gets (`cmd/smind/mcp*.go`). No evidence of wrong picks (no tool-call traces were exported), so no change is proposed; several tools look unused by seats (`account_usage`, `usage_summary`, `task_set_mode`, `task_options`, `task_set_option`, `queue_*`): inferred, to check in step 1 from `task logs` `[tool]` lines.

## Prompt notes

What the fold step will need from each prompt: hard limits that belong in tools, persona
to drop, instructions about other agents, router rules that conflict.

- **Conflict: default Peer.** `docs/WORKSPACE_PROTOCOL.md:85-105` still calls `peer-glm` the "default coder" and tells the Lead to brief it; `rolepack.go:55` (the Lead's own instructions) says default to `peer-sonnet`. The Lead reads the protocol first (`rolepack.go:49`), and even with the right brief a Lead chose GLM for a small step (dogfood entry 64: 28m31s, 3.0M tokens).
- **Conflict: who does S-sized work.** `WORKSPACE_PROTOCOL.md:53` routes S to "Supervisor does it, or one Peer directly", and `:18` says the Supervisor does S work itself; `smind-supervisor/SKILL.md:9` says it does not write a Lead's code, and its child list is `[lead]` only (`rolepack.go:103`), so the Supervisor cannot launch a Peer at all. The experiments show S-sized items done by a Lead alone.
- **Conflict: wait or end the turn.** `smind-leader/SKILL.md:35` tells the Lead to `task_wait`, while `:34` and `:63` say do not poll or wait, end the turn and let the `[child event]` arrive. The skill mixes an external orchestrator and an in-seat Lead.
- **Hard limits written as prose.** "End your turn after you dispatch", "never end a turn waiting on a background command" (`rolepack.go:47,57`), "never merge" and "launch Peers only" (the last is enforced by admission, the others are not). The turn rules failed three times in the dogfood: a Lead stalled 18 min on its own Monitor (entry 76), ended on an intention for 16 min (entry 90) and ended on "may I push?" for 5 min (entry 91). They belong in a tool or in the daemon (the `stalled` flag on `task_tree` is the start), not in more prompt text.
- **Instructions about other agents.** The Lead prompt carries Peer timings and prices (`rolepack.go:55`) that `peer-glm`'s Notes repeat (`rolepack.go:80`). `profile_list` already returns Notes, so one place is enough.
- **Stale scope.** `WORKSPACE_PROTOCOL.md:3-7,33-35` still say it applies to Paseo profiles and the `glm-peer` provider; `:85-105` assume GLM's 20-turn limit.
- **Duplicated checks.** The Supervisor skill's "review the report, not the claim" (`smind-supervisor/SKILL.md:22`) and the protocol's "not a second Lead" (`WORKSPACE_PROTOCOL.md:22`) pull in different directions; the first run re-ran the same tests three times (Peer, Lead, Supervisor: dogfood entry 62).
- **Persona.** No persona or tone instructions in any seat prompt. Nothing to drop.
- **Size at start.** A Lead starts with about 6,200 tokens of instructions and reads (instructions, skill, protocol, `smind` skill); a Peer with about 450 plus the brief; the Supervisor with about 3,400.

## Evals

Filled in during step 1. What step 0 found: no eval exists for the seats or the chains. The repo has unit tests of the mechanics (`internal/profiles/rolepack_test.go`, `internal/runs/parentnotify_test.go`, `internal/runs/sticky_seat_test.go`, `internal/wsapi/admission_test.go`) and one manual end-to-end run, the ADR-0041 AC26 dogfood tree (USD 0.99, 6 runs, 11m40s: dogfood entries 58-63, local-only). Nothing grades a work item's outcome.

| Eval | What it measures | What to do with it |
| --- | --- | --- |
| none found for the whole system | | built in step 1, with the person |

Main metric:

Gaps noticed: no task on the chains `supervisor -> lead -> peer -> lead -> supervisor`, `lead -> peer` in parallel (2-4 Peers), `peer -> lead` after a rate limit or a CLI crash, or on a direct `human -> lead` hop.

Chains with no task (from `chain_coverage.py`): not run, there is no eval to cover them; all chains in `coupling_map.by-item.md` are uncovered.

Break map: which tasks would notice each kind of break. "none" is a gap to raise.

| What could break | Tasks that would notice | Gap, and the task to add |
| --- | --- | --- |
| Dropped handoff | none | A task whose Peer finding (a file, a SHA, a failing test name) must reach the PR; a Peer that ends on a CLI error with no usage (tasks 109, 110) |
| Lost rule | none | One task per hard limit: never merge, Peers cannot launch, no push without approval |
| Shortcut | none | A task where the Lead could answer without reading the Peer's diff |
| Wrong pick | none | A task worded to fit both a Peer and the Lead's own edit (S-size) |
| Unwanted write | none | Every task lists the files in its write scope; any other write fails it |
| Lost thread | none | A multi-turn item (rework rounds: task 72 took 5 rounds, 88 took 5 human runs) |
| Unhappy path | none | A rate limit mid-tree (ADR-0041 B), a refused `task_send` at the tree cap (entries 65, 72) |

Baseline (variant `baseline`): not run.

## Worklist

Merges first, then folds, then as-tool changes. Highest traffic first inside each group.
One line per change. Each variant is compared with the last one that was kept. A reverted
variant keeps its number and is skipped. Variants are numbered in the order the changes
are made, so leave the variant blank until a change starts. If the person takes the list
out of order, say so here.

No merges (see above).

- [x] 1. fold `supervisor-sonnet` + `supervisor`: the Human/Steward sends the brief straight to a Lead; sizing and write-scope checks move to the brief template; the first fold also makes `lead` the entry (its instructions get the conflicts above fixed, no other edit) · variant: · compared with: baseline · result: applied, no eval (Human decision 2026-10-25)
- [x] 2. fold `peer-glm`: take it out of the seed pack and out of the protocol's "default coder" text; keep it only as an optional profile · variant: · compared with: last kept · result:
- [x] 3. as-tool `peer-sonnet`: a structured handoff result (candidate SHA and base, changed paths, commands with results, residual risk, keep/give up ownership) instead of free text cut to 1 KiB · variant: · compared with: last kept · result:
- [ ] 4. as-tool or delete `reviewer`: owner decision first (no traffic, so nothing to measure) · variant: · compared with: · result: **unchanged, Human decides later**

## Changes

No variants were run (eval skipped), so there are no `compare.py` tables. Each change records what was edited and the evidence it relied on. Branch `smind/task-lead-apply-single-agent-loop-verdicts-*`, one PR against `develop`.

### Item 1: fold `supervisor-sonnet` + `supervisor`, Lead as the single loop

- Evidence relied on: natural experiment #611, a direct hop to a Lead: 17m19s, USD 2.01, against about USD 4 through seat 45; 24 of the Supervisor's 43 runs were 16 s relay wake-ups (USD 2.29), plus 4 zero-token wakes at the account limit; the Supervisor re-ran the same tests a third time (dogfood entry 62).
- Edited: `internal/profiles/rolepack.go` (no `supervisor` seat or constant, `noBackgroundWait` is the Lead's only; Lead instructions rewritten: about 2.2 KB down to about 1.9 KB, no persona, no Peer prices, S/M done by the Lead, Peer only for parallel parts or independent verification); `internal/skills/bundle/smind-supervisor/` deleted; `smind-leader` skill rewritten; README seat and skill lists.
- Kept in the daemon: `childPolicy: profiles`, `mayLaunch`, `orchestration.maxDepth` (default 2; a test pins it, left alone), tree guard, sticky seats, notifier. The `supervisor` label in `smind profile ls` and tree tiers stays.
- Checked with no model: `TestSeedRoles_SeatsExactly`, `TestSeedRoles_LeadIsTheSingleLoop`, `TestProfileSeedRoles_*`.
- Not measured: cost and time per item after the fold (see After-check).
- Decision: Human, 2026-10-25, keep.

### Item 2: fold `peer-glm`

- Evidence relied on: 1 run in 191 (task 48, 28m31s, 3.0M input tokens against about 1.5 min on Sonnet; entry 64); a model is a profile setting.
- Edited: `rolepack.go` (no `peer-glm` seat; `peer-sonnet` Notes carry the cost and timing facts and no longer mention a GLM fallback); `docs/WORKSPACE_PROTOCOL.md` (GLM default-coder section removed). The `glm` provider and its tests are untouched.
- Checked with no model: `TestSeedRoles_SeatsExactly` (no `peer-glm`, no `glm` provider in the pack).
- Decision: Human, 2026-10-25, keep.

### Item 3: `peer-sonnet` as a tool with a structured handoff

- Evidence relied on: the free-text return is cut to 1 KiB and re-checked by hand (`childevent_text.go`); Peers are 55 runs and 52% of cost.
- Edited: `peer-sonnet` seed `Instructions` (final message STARTS with one fenced json block `{status, branch, commits[], tests_run[], not_verified[], notes}`); `smind-peer` skill; `internal/runs/childevent_text.go` (a well-formed fenced json block is kept whole, hard cap 4 KiB escaped, on top of the 1 KiB cut); the shape is documented in `docs/WORKSPACE_PROTOCOL.md` and the skill, no separate schema file. `status` replaces the `REOPEN_REQUEST`/`DEPENDENCY_REQUEST`/`BLOCKED` signals (`blocked` plus notes).
- Checked with no model: `TestBuildChildEventBlock_KeepsHandoffJSONBlock` (block first; block after 3 KiB of prose; over-cap and invalid blocks cut like prose), `TestSeedRoles_PeerEndsWithJSONHandoff`.
- Not verified: that a live Sonnet Peer actually emits the block first (no model call was made).
- Decision: Human, 2026-10-25, keep.

### Item 4: `reviewer`

Unchanged. Human decides later (no traffic found, nothing to measure).

### The three contradictions

- (a) Protocol default coder: the Peer section was rewritten to `peer-sonnet` only; GLM is not default.
- (b) Who does S work: the protocol's Supervisor role is gone; S and M items go to a Lead directly. The Paseo-era scope text (`:3-7,33-35`) was dropped, with one sentence saying so.
- (c) `smind-leader` `task_wait` versus "never wait": the skill is now the single-loop Lead's and never tells it to wait or poll; the protocol says an outside Human or Steward may use `smind task wait`.

## Open questions for owners

1. **Which seat is the single loop?** Recommended: the Lead (`main`). The alternative is the Supervisor, which would take over the Lead's spec/delegation/PR duties and make the Leads subagents. Blocks everything. (Human)
2. **Is the Supervisor's cross-Lead job real?** Sizing, disjoint write scopes for parallel Leads, the merge queue and the tree cap are in its duties (`WORKSPACE_PROTOCOL.md:15-20`), yet the cap bit twice with a Supervisor present (dogfood entries 65, 72) and the Steward/Human did the merge queue and the reviews. Who owns the cap after a fold? (Human, Steward)
3. **Peer return shape.** Which fields must survive a handoff for a Lead to accept a candidate without re-reading everything? Proposed in item 3; the Steward knows which of them it asked for most. (Steward)
4. **`reviewer`:** was it ever run (in smind or Paseo)? If not, delete it or wire it as a tool? (Human)
5. **`peer-glm`:** keep as an optional profile outside the seed pack, or drop it? It is also the only seat with a configured fallback (`rolepack.go:79`). (Human)
6. **Cost is a lower bound.** Runs that end in an error report no usage (tasks 109 and 110: 11m28s and 28m06s, 'cli exited before sending a result'), GLM reports no cost, and the CLI's input column looks like uncached input only (4-30 tokens per run against ~2M cache-read per run in the rollup). Should step 1 record the provider-side totals too? (Human)
7. **Two tree-1 style facts to confirm:** that tasks 108 and 113 were roots started by the Human/Steward directly (inferred: tree 45's rollup excludes them), and that profile 5 `supervisor` was never run. (Steward)
8. **Stale protocol text** (the three conflicts under Prompt notes) is a bug fix independent of this plan: fix it now, or only as part of item 1? (Human)

## After-check

A lightweight comparison instead of the paid eval: for the next ~5 items briefed directly to a Lead, read `smind task tree --task <lead> --json` (cost, runs, wall clock, peers launched) and `smind task runs <lead>`, and fill this table. The audit's 25-item numbers are the reference (Supervisor tree 45: 174 runs, USD 90.15, about 3.6 per item; #611 direct: 17m19s, USD 2.01). Compare medians, not single items; items differ in size.

| Item (Lead task, PR) | Size | Lead runs | Peers launched | Cost USD | Wall clock | Json handoff present? | Rework rounds |
| --- | --- | --- | --- | --- | --- | --- | --- |
| audit, 25 items (median) | mixed | | | about 3.6 | | n/a | 5 on the worst |
| | | | | | | | |
| | | | | | | | |
| | | | | | | | |
| | | | | | | | |
| | | | | | | | |

Decide after five rows: keep, or reopen the seat that was folded.

## Left as it is, and why

- No `keep` rows. Nothing in the system holds a conversation with the user on its own account or belongs to another company.
- The Human and the Steward: outside the system, not rows to fold.
- Peer worktree isolation, admission, the tree cap, sticky seats, the notifier and rate-limit failover (ADR-0041 part B): glue and guard rails, not agents. They stay unless a later change makes one dead.

## Audit notes and limits

How the traces were made, because the CLI does not show what the audit needed:

- Source: only the CLI of the dogfood daemon (`SMIND_HOME=/tmp/smind-dogfood /tmp/smind-dogfood/smind-dev`), read-only: `task tree --task 45 --json`, `task tree 1 --json`, `task ls 1`, `profile ls`, `task runs <id>` for tasks 1-130. Nothing was written to `/tmp/smind-dogfood`; the daemon and :4648 were not touched.
- **The CLI hides archived tasks.** `task tree` refuses an archived task ("task 46 is archived"), `task ls` omits it, and `task runs` shows neither the profile nor the parent. Of the 69 tasks 45-113, 68 are archived; the tree rollup only says `archivedDescendants: 62`. So `agent` and `parent_task` in `traces.jsonl` are **reconstructed**: task id -> title from the branch names in git (`smind/task-<slug>-<suffix>`; the suffix sorts in creation order; 68 branches for 69 ids, so task 65 has none), role from the `lead-` title prefix and from child_event counts, `peer-glm` from the provider, parents from the 2026-10-23 handoff (marked `handoff`) or from titles and creation order (marked `inferred`).
- Checks on the reconstruction: tasks 46-57 and 61, 88, 92, 105 match the handoffs exactly; the Lead's wake count equals its Peer count for 20 of 24 Leads (the other four had rework rounds); trace 45 sums to 175 runs and USD 90.12 against the rollup's 174 runs and USD 90.15.
- `origin` is only `human` or `child_event`: an agent's `task_send` also shows as `human`, so the column cannot tell the Steward from a Lead.
- `trace_id` is the root task id as the skill asks, but that gives three traces (45, 108, 113), so `coupling_map.by-root.md` is degenerate. `coupling_map.by-item.md` groups by work item instead (`item` = the Lead task; Supervisor runs are attached to an item by a time heuristic, labelled in `item_source`; 3 Supervisor runs belong to no item). Read it as 25 traces, still under the script's 50-trace line.
- Tasks 1-44 (Paseo-led, no seat profiles; run rows for most were pruned) are left out. Input tokens in the CLI are uncached only, so the "median input tokens" column of the coupling map means nothing; cache-read totals come from the `task tree --json` rollup.
- Tree 45 as exported: 174 runs, USD 90.15, 354M cache-read, 3.3M uncached input, 2.0M output tokens; the Supervisor's own 43 runs: USD 8.90, 21.8M cache-read.

## Check-in 1 (after the audit)

Written to the Human by the delegating Supervisor session; see the PR description. Answer pending.
