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
outcome (2026-10-20: **PASS on macOS**; Linux and Windows not covered) is recorded in the addendum at the end of this ADR.

**Scope decision, 2026-10-22 (Human): probe-lite on macOS only.** Linux and Windows get no Probe button; the proposal card says why
(`platform_unsupported` / `windows`) until each platform is measured. The read **allow-list** and the per-probe **canary** are mandatory
(there is no deny-list mode in the production sandbox package). The Human approved the keychain test; its result is in the addendum (R3).

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
resume; the sandbox canary attestation (§3) passes; the budget and quota checks (§5) pass. Windows daemons (ADR-0029) are not
eligible in v1: there is no command sandbox to attest.

### 2. Delivery: the one place AC17 is relaxed

The candidate reaches an agent through the same content-addressed, read-only projection and `WithAddDirs` that production
delivery uses (ADR-0026 §5). The amendment, stated as a rule:

> A skill in status `proposed` may be delivered **only** to an arm of a probe run (`runs.purpose = 'probe'`), **only** when
> the sandbox attestation of §3 is present, and **only** through the probe projection builder. Production delivery
> (`internal/learning/projection`, `delivery`) keeps filtering to `active` skills and never sees a proposal.

Enforcement is structural, not procedural: the probe projection builder takes a `sandbox.Attestation` value that only the
sandbox package can mint after a passing canary call, so a code path that skips the sandbox does not compile. The arm's
projection hash is recorded on the run (ADR-0026 §9.7). A probe run is excluded from the collector, the cadence counter, the
offered/loaded counters and every learning trigger, exactly like a reflector run (`runs.purpose` already exists), and runs
under a hidden `tasks.kind = 'system'` task.

### 3. Isolation: R1–R7 are acceptance criteria

A probe arm may not start unless all of R1–R7 hold (D3). Each is an AC in the plan with a named test; those that need a real
CLI are proven by the P0 spike and then kept as a gated live suite.

| # | Requirement | Mechanism |
|---|---|---|
| R1 | **Filesystem**: writes only inside the arm directory and a private temp/HOME; no read of the real checkout, `~/.smind`, `~/.claude` (beyond what the CLI process itself needs), `~/.ssh`, other worktrees or other arms | arm dir is a fresh `git archive` export with its own `git init`, no link to the real `.git`; agent Bash runs in the CLI command sandbox (`allowWrite` = the arm only, plus the CLI's own per-session tmp; `denyRead` on the real `$HOME`, `CLAUDE_CONFIG_DIR`, the real repo and worktree roots and the arm's parent, `allowRead` on the arm and the projection, or better an allow-list: `denyRead:["/"]` plus the minimal system paths); built-in Read/Edit/Write outside the arm are refused by the CLI itself under `acceptEdits` + `--permission-prompts none` (P0: the SDK decider is never consulted headless, so it is belt-and-braces only), and the read-only projection is protected from the built-in write tools by `permissions.deny` |
| R2 | **Network**: the model endpoint only; no web fetch/search, no package registries, no git remotes | CLI sandbox `strictAllowlist` with no allowed domains and no local binding, `failIfUnavailable`, unsandboxed commands disabled, Bash auto-allowed only *inside* the sandbox; a `--tools` **allowlist** (Bash, Read, Edit, Write, Glob, Grep, NotebookEdit; P0: the default set has 27 tools including outward-facing ones, so a Web* denylist is not enough); MCP closed with the flags the isolated runner already uses; connectors off |
| R3 | **Credentials**: the arm gets only the model credential through normal account routing; the environment is an **allowlist** built from scratch, not a deny-list | a generated `/usr/bin/env -i` launcher script passed as the CLI path (the SDK inherits the daemon's whole environment and `WithEnv` cannot remove variables; P0 measured daemon canary variables reaching the agent's Bash without it) that keeps only `PATH`, `LANG`, `TMPDIR`, `HOME`, `CLAUDE_CONFIG_DIR` (if set) and `USER` (the login needs it). The arm uses the user's real login location, not a private HOME; the sandbox `denyRead` is what keeps the credential files from the arm's Bash |
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
launch shape); a sandbox attestation can go stale (so the canary below is repeated per probe, and every arm
re-checks the tripwires).

**Attestation (P0):** the CLI offers no model-free way to report command-sandbox status, so the `sandbox.Attestation` is minted by **one tiny canary call per probe** (about USD 0.002–0.003, charged to the probe bucket, 2 turns): the agent tries one write outside the arm and one inside, and the daemon asserts on the filesystem (outside absent, inside present) and on the init tool list (exactly the allowlist). A failing canary disables the probe.

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

## Open questions (resolved by the P0 spike; answers in the addendum)

1. Does Bash network denial and `denyRead` on credential dirs hold under smind's exact launch (SDK, `--setting-sources=`,
   `--settings`)?
2. Are built-in Read/Edit/Write outside the arm directory denied non-interactively, and by which mechanism?
3. Does `failIfUnavailable` stop the CLI from running a command when the sandbox cannot start?
4. Which variables does an arm need to authenticate, and how is the sandbox attested without a model call? (Answer: no private HOME; `USER` + the user's real `HOME`/`CLAUDE_CONFIG_DIR`; no model-free attestation exists, one canary call per probe.)

## Addendum: P0 outcome

**P0 sandbox spike, 2026-10-20: R1 write, R2 network and R3 process-env hold on macOS; read confinement and built-in tools hold for the
measured configurations; four items stay PARTIAL.** claude 2.1.303 (native binary), SDK claude-agent-sdk-go v0.3.2, macOS 27 arm64,
model `claude-haiku-5-5`; **24 live calls, USD 0.068** (caps raised twice, to 18/0.70 and 24/0.80, each by the Lead for a named follow-up
and reported to the Human). An independent read-only review of the first round found the original "PASS" overstated; the hardening round
below re-measured or downgraded every flagged claim. The live suite is `internal/learning/probe/spike` (`-tags liveprobe`,
`SMIND_LIVE_PROBE=1`); assertions are on ground truth (filesystem, a local listener's connection count, canary strings, file hashes), not on
what the model said. A single live run of the final code was not possible within the cap, so the evidence is staged: **MEASURED(#n)** below
means observed in call n under the stated configuration. Linux (bubblewrap) and Windows were **not** tested.

| Req | Status | Detail |
|---|---|---|
| R1 write (Bash) | PASS, one exception | `filesystem.allowWrite:[arm]`: the arm and its `.git` are writable (`git commit` lands); sibling dir, real-checkout decoy, decoy HOME, `/tmp` and a dir under the real `$HOME` are denied. **Exception:** the CLI's per-session `$TMPDIR` (`/tmp/claude-<uid>`) is writable (MEASURED #19, #24); treat it as part of the arm. Arm git needs `GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1`. |
| R1 read (Bash) | **Allow-list: PASS for every decoy, including an unlisted path at a third location** (MEASURED #24). Deny-list: PARTIAL by construction | A deny-list (`denyRead:[$HOME, parent(arm)]` plus explicit extra paths) denied every listed decoy but an *unlisted* sibling stayed readable (negative control, #19), so a deny-list cannot promise "no read of other worktrees". The **allow-list** (`denyRead:["/"]`, `allowRead` = arm, projection, the minimal system paths, including `/private/var/select` and the raw symlink nodes `/var`, `/tmp`, `/etc`) works for sh, cat, ls, curl and git (with scrubbed git config) and denied every decoy (#20, #21, #24). The allow-list is therefore the **required default**; the deny-list is a documented fallback. Not measured: toolchains beyond sh/git/curl (a Go build needs more allowed paths), and the built-in-tool run (#22) used the deny-list settings, not the allow-list. |
| R1 built-in tools | PASS for the one measured configuration | ONE configuration (#22): `acceptEdits` + `--permission-prompts none` + the 7-tool `--tools` allowlist + `SettingsJSON` + the scrubbed launcher, `--permission-prompt-tool` unset: outside Read/Glob/Grep/Write refused ("haven't granted", 7 `permission_denials`); Write, Edit (after a permitted Read) and NotebookEdit on the projection refused with the bytes unchanged; in-arm Read/Write/Edit work. The SDK decider was consulted 0 times *with no prompt tool configured*; `--permission-prompts none` is what makes denial unattended, so the decider is not an enforcement point. `permissions.deny` on `Edit/Write/NotebookEdit` for the projection is required (`--add-dir` makes it writable to the built-in tools); `sandbox.filesystem.denyWrite` is redundant *only while* `permissions.deny` is present (both removed: a sandboxed write into the projection succeeds, #23), keep both. Not exercised: Edit/NotebookEdit denial on an existing *outside* file (the read-before-write check fires first; files unchanged), `dontAsk`. |
| R2 network | PASS for TCP/curl and DNS answers | A self-dial proved the listener counter; the arm's curl to it failed (rc 7), the listener saw 0 connections, an external curl got a proxy 403, and `dscacheutil` for an external name returned nothing inside the arm (#19, #24). The base sandbox already blocks: removing `strictAllowlist` and `allowLocalBinding` changed nothing (#23), so keep them as policy but do not credit them. **Not tested:** DNS-query exfiltration, UDP, ICMP, unix sockets. The model call (the CLI process, outside the command sandbox) is unaffected; init lists 0 MCP servers, built-in plugins only, 0 skills. |
| R3 env | PASS for the process env; keychain PASS under the allow-list (2026-10-22) | The `env -i` launcher keeps `PATH, LANG, TMPDIR, HOME, CLAUDE_CONFIG_DIR, USER` (`USER` is required for the login, found by name tuning on one machine): the CLI's own env was exactly that set and the canary variables reached neither it nor the arm's Bash. With the inherited environment the canaries **did** leak, so the launcher is mandatory. **Keychain (Human-approved test, 2026-10-22, 2 live calls, USD 0.0034):** on this machine the Claude login lives in the macOS login keychain (a `Claude Code-credentials` item exists; existence checked by attributes only, output discarded, no secret value ever requested, read or printed). One throwaway generic-password item `smind-probe-keychain-canary-<random>` was created in the login keychain, readable from outside the arm (control), and **not** readable from the arm's sandboxed Bash under the allow-list read policy: `security find-generic-password -s <svc> -w` returned rc 44 "item could not be found", with the default keychain search list and with the login keychain named by explicit path; `security list-keychains` showed only the System keychain; `security default-keychain` failed ("could not be found"); the real login item was likewise unreachable (existence check rc 44). The throwaway item was deleted and its deletion confirmed. Consequence: no extra keychain mitigation is needed *because the read allow-list is mandatory*; the deny-list fallback was **not** tested for the keychain and must not ship (the allow-list is the only production read mode in `sandbox.Settings`). Not tested: other Keychain access paths than `security`, XPC services other than securityd, the data-protection keychain. `TestLive_KeychainReachableFromArm` (`SMIND_LIVE_KEYCHAIN=1`) keeps the check as a gated regression test. |
| `failIfUnavailable` | PARTIAL | Documented by the CLI but not isolatable on macOS (`sandbox.ripgrep` and `enabledPlatforms` do not make the sandbox unavailable). Fail-closed at exec time is proven (outer `deny process-exec /usr/bin/sandbox-exec` gives exit 126). Hence the per-probe canary. |
| Violations (R7) | no native log | Only tool_result text (`Operation not permitted`, `haven't granted`) and `result.permission_denials`; CLI stderr was empty. R7's "violation log empty" is built from those plus the filesystem canaries and hashes. |

**Consequences for §3 (applied above):** `--tools` allowlist; `acceptEdits` + `--permission-prompts none` (no prompt tool); the `env -i`
launcher created `O_EXCL|O_NOFOLLOW` in a verified 0700 directory (it writes only non-secret allowlist values; **never add a token to
it**); **allow-list read policy by default**; the projection write-deny; `denyRead` also covering `CLAUDE_CONFIG_DIR` (to implement; the
spike does not); `SettingsJSON` fails closed on an unresolvable `$HOME` and canonicalises paths; the per-probe canary attestation; git's
config env; the CLI's `$TMPDIR` counted as part of the arm.

**Not covered / residual:** Linux and Windows (decision 2026-10-22: **probe-lite on macOS only**; Linux and Windows get no Probe button and a stated reason until measured); other keychain paths (see R3); real credential files (never read; proven only with a
test-created file under the real `$HOME`); DNS-query exfiltration, UDP, ICMP, unix sockets; symlink races; `dontAsk`;
`CLAUDE_CODE_SUBPROCESS_ENV_SCRUB` and `sandbox.credentials`; what `CLAUDE_CODE_MESSAGING_SOCKET`/`TOKEN` (visible to the arm) permit.
`USER` was found on one machine, so the canary attestation also re-runs the scrubbed login.

## Addendum 2: implementation decisions of the arm PR (2026-10-22)

Recorded here because they refine §3 and were forced by measurement:

- **One production sandbox shape.** `internal/learning/probe/sandbox` has only the allow-list read mode; the spike's deny-list and ablation switches stay in
  the spike package. Off macOS, `Prepare` and `RunCanary` return `ErrUnsupported`.
- **Process cleanup (R4) uses a process group, not a marker.** The SDK only signals the CLI process, so a command the agent detaches (`cmd &`) outlives it.
  macOS 27 hides other processes' environments from `ps` and `sysctl kern.procargs2` (measured), so an env marker cannot be scanned. The launcher therefore
  writes its pid and execs the CLI through the system `perl` as `setpgrp(0,0); exec`, which makes the CLI a process-group leader (pgid = pid) and keeps the
  pid; `arm.KillTree` signals `-pgid` plus the process tree below it, after checking that the pid's command line names the arm directory (a reused pid is never
  signalled). A missing `/usr/bin/perl` fails closed (`ErrUnsupported`). A process that deliberately leaves the group *and* is reparented is reported as a
  survivor, fails the arm, and still runs inside the command sandbox.
- **Denied attempts are metrics, not tripwires.** The CLI has no native violation log (P0), so R7's "violation log empty" is realised as: the real checkout
  (HEAD, refs, `git status` hash) and the human skill dirs (content hash) unchanged, and the CLI's init tool surface equal to the allowlist with no MCP server
  or non-builtin plugin. A *denied* write or read is the sandbox working; it is counted (`permission_denials`, denied-looking failed tool results) identically in
  every arm and never makes a probe `unsafe`. Residual: the `git status` hash does not see a further edit to a file that was already dirty before the arm.
- **Attestation.** `sandbox.Attestation` is minted only by `Canary.Verify` after the filesystem assertions and the init tool list (`RunCanary`: one 2-turn
  haiku call, measured USD 0.0014 on 2026-10-22), is valid for 10 minutes (`AttestationTTL`) and is required by `projection.ProjectProbe`, the only path that
  delivers a proposed skill. All arm projections of a probe are built when the probe starts, under that attestation.
- **Cap enforcement.** The CLI enforces `--max-turns` and `--max-budget-usd`; the arm additionally enforces wall time, output bytes and (via the usage
  event) an unreported cost charged at the cap. Each end has a distinct status (`timeout`, `turns`, `capped`, `output`, `killed`).
