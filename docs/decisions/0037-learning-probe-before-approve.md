# 0037: Probe before approve (sandboxed replay of a proposed learned skill)

## Status

**Accepted — 2026-10-20.** The Human accepted decisions D1–D14 of the Harness² study exactly as recommended (see
[Decisions](#decisions)), including the go-ahead for a minimal live spike (P0) against the real `claude` CLI.
**Amends ADR-0026 AC17** ("A `proposed` skill is never delivered to any agent"): a proposed skill may now be delivered to
an agent, but only inside a probe sandbox that satisfies R1–R7 below. Everything else in ADR-0026 (gate, provenance rule,
human approval, projection of `active` skills only) is unchanged. Plan:
[`docs/plans/active/learning-probe.md`](../plans/active/learning-probe.md).

**The isolation tier in §3 is contingent on the P0 spike.** If P0 cannot show R1–R3 with the Claude CLI's own command
sandbox, the decision is the container tier (§3, fallback), and if neither is available, there are no probes. The P0
outcome is recorded in a dated addendum at the end of this ADR.

## Context

ADR-0026 gives smind a good *Propose* step (a reflector reading real transcripts), a strong deterministic *gate* and a
human as *Compose*. It has **no evidence that a proposed skill changes behaviour before it lands**: the offered/loaded
counters, probation and graduation only measure *use* after landing, never *benefit*.

[Harness²](https://arxiv.org/abs/2610.14224) (read as `refs/harness2`, Apache-2.0, never executed beyond its deterministic
demo; nothing copied) has a tested recipe for exactly that gap, **Probe**: re-run the same task with and without an edit,
under matched conditions, and read the *behavioural difference* between the runs, with every claim tied to a trajectory
moment, "never loaded" counted as "no effect", and one sample treated as weak evidence. Its other two steps (editors that
rewrite the harness, an LLM composer that merges edits) do not fit smind: our reflector is the editor and the human is the
composer.

What Harness² does *not* give us is safety. It runs the executor with its sandbox off and unrestricted network
(`--dangerously-bypass-approvals-and-sandbox`; its own README tells you to firewall it), and a probe rollout once downloaded
its own grading rubric unprompted. The thing smind would execute is an **LLM-authored, not-yet-approved instruction**,
followed by an agent with Bash and edit tools, on a real repository whose content may itself carry injected text. ADR-0026's
central defence against that was AC17: a proposed skill reaches no agent. A probe removes that defence on purpose, so a
sandbox has to carry it instead.

Cost is the second blocker. One probe is about three task rollouts, roughly 5–6x the whole per-workspace daily learning cap
(USD 0.25, `internal/config/learning.go`). It cannot live under the learning budget.

Reference study: `docs/research/local/harness2-study.md` (local-only, gitignored).

## Decision

### 1. Shape: manual, advisory, source-episode replay

A **probe** is started by a human click (or `smind learn probe`) on a *pending* proposal. It replays the proposal's source
episode in a sandboxed throwaway copy of the repository, in three **arms**, and records deterministic contrasts. It never
approves, rejects or gates anything; the verdict chip is advisory (D9) and the card always says "behavioural contrast on one
replay of the source episode, not a measurement".

| Arm | Skills delivered | Role |
|---|---|---|
| A | the current *active* projection (what the next run would see today) | control |
| A′ | same as A | noise floor: B only counts as different if it differs from A by more than A′ differs from A |
| B | active projection plus the candidate (create: add; patch: replace the target body; bundle: apply) | candidate |

`--quick` runs A and B only; with no noise floor every verdict except `never_loaded`, `probe_failed` and `unsafe` is capped at
`inconclusive` (D6). Arms run sequentially; one probe at a time per daemon.

What is replayed is the **source episode only** (D5): the persisted `runs.prompt` of the run that the proposal's evidence
came from, at its recorded `base_sha`. The result means "would have helped *there*", never "will help later"; the UI label
says "fixes the observed failure here", not "generalises".

**Eligibility** (otherwise the button is disabled with a stated reason, fail closed): the proposal is pending and passed the
gate; the evidence run is claude-native (D4; a GLM/Kimi ACP agent owns un-gateable tools, ADR-0026 D13); it has a persisted
`runs.prompt`, a captured `base_sha` and `dirty = false`; it was a single-prompt run, not a mid-chat continuation or a session
resume; the sandbox self-check (§3) passes; the budget and quota checks (§5) pass. Windows daemons (ADR-0029) are not
eligible in v1: there is no command sandbox to attest.

### 2. Delivery: the one place AC17 is relaxed

The candidate reaches an agent through the same content-addressed, read-only projection and `WithAddDirs` that production
delivery uses (ADR-0026 §5). The amendment, stated as a rule:

> A skill in status `proposed` may be delivered **only** to an arm of a probe run (`runs.purpose = 'probe'`), **only** when
> the sandbox attestation of §3 is present, and **only** through the probe projection builder. Production delivery
> (`internal/learning/projection`, `delivery`) keeps filtering to `active` skills and never sees a proposal.

Enforcement is structural, not procedural: the probe projection builder takes a `sandbox.Attestation` value that only the
sandbox package can mint after a passing self-check, so a code path that skips the sandbox does not compile. The arm's
projection hash is recorded on the run (ADR-0026 §9.7). A probe run is excluded from the collector, the cadence counter, the
offered/loaded counters and every learning trigger, exactly like a reflector run (`runs.purpose` already exists), and runs
under a hidden `tasks.kind = 'system'` task.

### 3. Isolation: R1–R7 are acceptance criteria

A probe arm may not start unless all of R1–R7 hold (D3). Each is an AC in the plan with a named test; those that need a real
CLI are proven by the P0 spike and then kept as a gated live suite.

| # | Requirement | Mechanism |
|---|---|---|
| R1 | **Filesystem**: writes only inside the arm directory and a private temp/HOME; no read of the real checkout, `~/.smind`, `~/.claude` (beyond what the CLI process itself needs), `~/.ssh`, other worktrees or other arms | arm dir is a fresh `git archive` export with its own `git init`, no link to the real `.git`; agent Bash runs in the CLI command sandbox with `denyRead` on credential dirs; built-in Read/Edit/Write are held to the arm dir by a smind permission decider that auto-denies anything outside it (no human is attending) |
| R2 | **Network**: the model endpoint only; no web fetch/search, no package registries, no git remotes | CLI sandbox network allowlist, `failIfUnavailable`, unsandboxed commands disabled, Bash auto-allowed only *inside* the sandbox; `WebFetch`/`WebSearch` denied; MCP closed with the flags the isolated runner already uses; connectors off |
| R3 | **Credentials**: the arm gets only the model credential through normal account routing; the environment is an **allowlist** built from scratch, not a deny-list | PATH, private HOME, LANG, the one auth variable the CLI needs |
| R4 | **Resources**: per-arm wall timeout, turn cap, cost cap, output cap; one probe per workspace and one per daemon | process-group kill (ADR-0024); caps in §5; probes must self-clean (every arm dir removed at arm end, orphans swept at daemon start) |
| R5 | **Side effects**: no push, publish, network write or file outside the arm | follows from R1–R3; no remote and no git credentials exist in the arm |
| R6 | **Data**: transcripts and diffs stored redacted; purged with the decision (D13) | existing redactor (`internal/learning/redact`); see §6 |
| R7 | **Tripwires** after every arm: the real checkout's `git status`/HEAD unchanged, human skill dirs (`~/.claude/skills`, repo skill dirs) unchanged by hash, sandbox/proxy violation log empty | any hit quarantines the probe and marks it `unsafe`, in the style of `TestNeverTouchesHumanSkills` |

**Minimum tier (D3):** (a) the Claude CLI's own command sandbox, plus a tool denylist, plus `failIfUnavailable`, provider-native
and with no new infrastructure; contingent on the P0 spike. (b) A container with an egress-allowlist proxy is the fallback if
P0 fails and the tier for the homelab (ADR-0031). (c) *No isolation is never an option.* "Sandbox unavailable" disables the
button; it never degrades to an unsandboxed probe.

Honest limits, restated from the study: the CLI command sandbox covers Bash, not necessarily built-in tools (so R1 relies on
the decider for those); a past CLI bug interacted with `--setting-sources=` and `--settings` (so P0 tests exactly smind's
launch shape); a sandbox attestation can go stale (so the self-check is repeated per daemon start and on a TTL, and every arm
re-checks the tripwires).

### 4. What is measured (tier 1 only in this ADR's scope)

Deterministic facts from persisted run events, no model call:

- **Loaded?** In B, was the candidate loaded (`Skill` call or `Read` under the projection path), via
  `internal/learning/loads.Match` unchanged. Never loaded is the verdict `never_loaded` ("no observed effect: the trigger cue
  did not fire"), the highest-value, cheapest signal.
- **Targeted-failure recurrence**: for `failure_recovery` / `permission_denied` evidence, a machine predicate on the
  prevalence signature (§7): did the same failure or request occur in A, A′ and B.
- **Counts**: tool calls, failed tool calls, permission requests and denials, turns, tokens, cost, wall time, stop reason,
  files touched, diff size, whether anything was delivered (non-empty diff).
- **Optional `probeCheck`** (D8): a per-workspace, **human-authored** command (the repo's test or vet task) run after each arm
  inside the same sandbox with no network, giving pass/fail. Human-authored only, like `setup_script` (ADR-0024); never set by
  an agent, an MCP tool or the reflector.
- **Noise floor**: delta(A, A′) beside delta(A, B), per metric.

Verdict vocabulary (advisory, never styled as "approved", never gating): `Helped (consistent)`, `No observed effect`,
`Never loaded`, `Hurt / regressed`, `Inconclusive`, `Probe failed (infra)`, `Unsafe (tripwire)`. `Helped` is only reachable for
evidence kinds that have a failure signature, and requires: B loaded the skill; the failure appears in both A and A′ and not
in B; no metric worse than the noise floor; `probeCheck` (if configured) not worse in B. Evidence of kind `user_message` or
`loaded_skill_wrong` can at best reach `No observed effect` or `Inconclusive`.

An LLM judge and a contrast writer (blind pairwise, both orders, tie allowed, no tools, advisory) are **deferred until real
probe data exists** (D1, D9); they would be a separate PR and, if they change anything beyond advice, a new ADR.

### 5. Budget: its own bucket, manual only

Probes never share the learning cap (D7). Defaults, all in `config.yaml` under `learning.probe.*`:

| Knob | Default | Meaning |
|---|---|---|
| `perProbeBudgetUSD` | **1.50** | hard cap across all arms of one probe |
| `dailyBudgetUSD` | **3.00** | per workspace, trailing 24 h of `run_usage` where `runs.purpose = 'probe'` |
| `quotaSkipPercent` | 80 | skip when the routed account's cached usage window is at or above this; reads only the cached snapshot (ADR-0028; provider usage endpoints are rate-limited, never called here) |
| `armTimeoutMinutes` | 15 | wall timeout per arm |
| `maxTurnsCap` | 80 | arm turn cap is `min(2 × source run turns, maxTurnsCap)` |

**Estimate before the click:** `est_arm = 1.2 × source run cost` (from `run_usage`; a source run with unreported cost makes the
probe ineligible, "source cost unknown"); `est_probe = n_arms × est_arm`. The button shows the estimate and expected duration.
The probe is disabled when `est_probe > perProbeBudgetUSD` or `est_probe + spent_24h > dailyBudgetUSD`. During a run the cap is
enforced from `EventTypeUsage` (ADR-0020): after every usage event, if cumulative probe cost reaches the cap the running arm is
stopped (status `capped`) and the remaining arms are not started; a capped probe is `Inconclusive`. Unreported usage on a
probe run counts as that arm's cap. The estimate is a guess until measured: the first-week `run_usage` for `purpose = 'probe'`
calibrates it, as ADR-0026 D4 did for the reflector. Spend shows as a **Probe** bucket in Settings → Usage beside "Learning".

### 6. Storage, retention, privacy

Two tables, `learning_probes` (proposal, eligibility inputs, estimate, verdict, projection hashes, cost, status) and
`learning_probe_arms` (role, run id, metrics, redacted diff and final message, check result, cap/outcome). Arm directories exist
only while the arm runs and are deleted when it ends; the transcript is the probe run's own `run_events`.

**Retention (D13):** probe artifacts (arm rows, redacted diffs and final messages, probe runs, tasks and events) are purged **14
days after the proposal's decision** (approve, reject or archive); a pending proposal keeps its probe until decided. A
**forget-workspace** hard-deletes every probe artifact with the rest (the ADR-0026 AC32 test extends to cover probe rows, runs
and arm directories). The ledger keeps one `probed` row (verdict, cost, run ids, projection hash) for the life of the skill
history.

No telemetry; the only egress a probe adds is the model call to the provider that already saw the source run (same-recipient
rule, ADR-0026 D4), and nothing leaves via the agent, by R2.

### 7. Companion changes that need no sandbox

- **S0, prevalence scan (no model call).** For a pending proposal, derive a signature from its evidence (`failure_recovery`:
  tool name + normalised error prefix; `permission_denied`: tool + request kind) and count occurrences in the workspace's
  persisted `run_events` over a window (default 30 days): "seen in N runs across M chats". It is Harness²'s "a failure repeated
  across the base set is a real property, a defect in 1 of k is noise" computed from history smind already has. It also defines
  the signature that §4's recurrence predicate reuses.
- **D11, reflector retry.** When the gate rejects an intent only for *structurally repairable* findings (missing trigger cue,
  body/description/line caps, name charset/prefix, frontmatter keys, links), the reflector is called once more with the gate's
  findings appended. Provenance, guard, secret and duplicate findings are **never** retried: a retry there would only coax the
  model into inventing a quote. The retry re-passes the same gate, the budget is re-checked first, its cost lands on the same job
  row, and it happens at most once per job.

### 8. Surface

Settings → Skills → Proposed gains a collapsed **Probe** card: the prevalence line; `Run probe (about USD 1.4, about 8 min)`
with an estimate popover or a disabled reason; three live rows while running (Control / Control replicate / With this skill:
status, elapsed, cost so far, link to the hidden run's log; Cancel); a result with the verdict chip, a Control vs With-skill
contrast table with the noise-floor column, `loaded`, `targeted failure recurred`, check result, produced-diff viewer and the
disclaimer line. Approve / edit / reject are unchanged. CLI: `smind learn probe <proposal> [--quick] [--dry-run]` and
`smind learn probe show <proposal>`. Events `learnedSkill.probe.*` feed the existing toast/badge path. **No MCP tool**: agents
do not manage the learning loop (this is also part of its recursion guard).

## Decisions

Accepted by the Human on 2026-10-20, all as recommended.

| # | Decision | Decided |
|---|---|---|
| D1 | Pursue "probe before approve" | **Yes, staged**: S0 prevalence scan → P0 sandbox spike → O1/probe-lite; LLM judge only after data |
| D2 | Amend ADR-0026 AC17 | **Yes**, via this ADR; R1–R7 are acceptance criteria |
| D3 | Minimum isolation | **CLI command sandbox + tool denylist + fail-if-unavailable**, contingent on P0; **container (egress allowlist) as fallback and for the homelab; never none** |
| D4 | Provider scope | **claude-native only in v1** |
| D5 | What to replay | **Source episode only**, labelled "fixes the observed failure here", not "generalises" |
| D6 | Rollouts per probe | **3 (A, A′, B); `--quick` = 2** |
| D7 | Budget | **Separate probe bucket, manual click only**, per-probe USD 1.50, daily USD 3.00 per workspace, quota-aware skip |
| D8 | `probeCheck` | **Optional, human-authored per-workspace command** |
| D9 | LLM judge / contrast report | **Advisory only**, and deferred until probe data exists |
| D10 | Profile-notes edit proposals | **After probes prove out**; permission modes, MCP, child policy, budgets, scripts, hooks, model/account are **never automated** |
| D11 | Reflector retry with gate findings | **Yes**, one shot (§7) |
| D12 | Probation holdout | **Deferred** (it withholds an approved skill from real work) |
| D13 | Retention | **Purge 14 days after the decision; hard-delete on forget-workspace** |
| D14 | Live P0 spike with the real `claude` CLI | **Go-ahead**, a few cents; call count and cost are reported |

## Alternatives considered

- **No probe; keep counters only.** Rejected: counters show use, never benefit, so the human approves blind.
- **Probe with the sandbox off, as Harness² does.** Rejected: runs an unapproved LLM-authored instruction with Bash on a real
  repo; this is the exact risk AC17 existed to remove.
- **`git worktree` of the real repo for arms.** Rejected: shares `.git`, refs and objects with the user's checkout, so an arm
  could reach history, hooks and remotes. A `git archive` export is isolated by construction.
- **Probe on GLM/Kimi via container now.** Deferred: needs container ops and the strongest isolation; revisit after claude-native
  probes prove out.
- **Auto-probe every new proposal (shadow mode).** Rejected: unbounded spend; manual only.
- **Judge-first (LLM verdict) before deterministic facts.** Rejected: an LLM judge without a measured baseline would be the
  most-trusted and least-checked number on the card.
- **Share the learning budget.** Rejected: one probe is several times the whole daily learning cap.

## Consequences

- AC17's guarantee becomes "never delivered to a production run, and to a probe only inside an attested sandbox". The code
  keeps the old property (`TestProposedSkillNeverDelivered`, production projection) and adds the new one.
- New: `internal/learning/probe` (arm materialiser, sandbox, contrast, service), two tables, a `learning.probe.*` config block,
  a `learning.probe.*` wsapi surface, `smind learn probe`, a Probe bucket in usage, one UI card.
- Real money is spent only on a human click, capped and visible; probes that cannot be isolated do not run.
- A probe is evidence of *behaviour on one replay*, not of correctness. Harness² itself documents regressions from over-trusting
  such contrasts; the chip vocabulary and disclaimer are the guard, and the verdict never gates approval.
- Windows daemons and ACP providers get no probes in v1.

## Open questions (resolved by the P0 spike, not by the Human)

1. Does Bash network denial and `denyRead` on credential dirs hold under smind's exact launch (SDK, `--setting-sources=`,
   `--settings`)?
2. Are built-in Read/Edit/Write outside the arm directory denied non-interactively, and by which mechanism?
3. Does `failIfUnavailable` stop the CLI from running a command when the sandbox cannot start?
4. Which single auth variable does a private-HOME arm need, and how does the self-check attest the sandbox without a model call?

## Addendum: P0 outcome

*(Filled in by the P0 PR.)*
