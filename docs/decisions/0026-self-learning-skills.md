# 0026: Self-learning skills (daemon-side reflector, curator and lifecycle)

## Status

**Accepted** (2026-10-18). The user accepted D1-D11 exactly as recommended in
"Decisions" below and added D12 (deterministic similarity check in the gate).
Phase B (code) is unblocked; the spec is
[`docs/plans/completed/self-learning-skills.md`](../plans/completed/self-learning-skills.md).
Originally proposed 2026-10-11.

**Amended by [ADR-0037](0037-learning-probe-before-approve.md) (2026-10-20):** AC17 ("a `proposed` skill is
never delivered to any agent") now has one exception, a probe arm inside an attested sandbox. Production delivery is unchanged.

Numbering: 0023 (desktop auto-update), 0024 and 0025 are taken, so this is
0026.

## Context

The user wants smind to learn from its own sessions the way
[autoharness](https://github.com/tigerless-labs/autoharness) does for Claude
Code: distil reusable skills from real work, merge near-duplicates, measure
whether skills get used, and prune the dead ones, without ever touching skills
a human wrote. A read-only clone is at `refs/autoharness` (MIT, commit
9eec623). It was read, never executed, and nothing is copied; this ADR keeps
the pattern and re-derives the mechanics for smind.

### What autoharness actually does (verified in `refs/autoharness`)

| Mechanic | Where | Real values |
|---|---|---|
| Trigger | `hook/on_stop.py`, `hook/dispatch.py`, `config.py` | Every main-session `PreToolUse` bumps a per-session counter; the `Stop` hook reflects when it reaches `REFLECT_EVERY_N` = **50 tool calls**, then resets. `SessionEnd` flushes the tail. `/learn` (`skills/learn`) stages intents from the live session. |
| Input window | `hook/capture.py` | Raw transcript bytes since a per-session byte watermark; each record clipped to **4 000 B**, window to **200 000 B** (tail kept), then passed through the redaction rules. A digest of the **20** exchanges before the window (text and tool names only, 200 chars each, 20 KB) is background only, never evidence. |
| Reflector | `agents/reflector.md`, `hook/spawn.py` | Detached child `claude -p --agent autoharness:reflector --dangerously-skip-permissions`, model `haiku`, tools `Read, Grep, Glob` plus one write face, `stage_skill`, which only appends an intent to a queue. Input is the redacted window, the description index of every existing skill, and the format spec. Preference order: patch a skill that was loaded this episode, patch a class-level skill, update with subfiles, create (born as an umbrella). Must reconcile new against old and may fold unmistakable existing duplicates (`delete` with `absorbed_into`). |
| Promoter | `hook/promoter.py`, `lib/validate.py`, `lib/skills_guard.py` | The only writer. Validates in memory (six classes: safety regex scan over six families, structure, ledger present, content complete, global repo-agnostic, only-self-created), then writes atomically, `SKILL.md` last. Rejected intents write nothing. Body cap **25 non-blank lines**, description one sentence with a trigger cue, **<= 60 chars** (index budget), subfiles only under `references/ templates/ scripts/ assets/` (8 files, 64 KB each, 256 KB total) and each must be pointed at from the body. Evidence is re-redacted and materialized as `references/evidence-<hash>.md`. |
| Marker | `lib/sidecar.py` | `.sidecar.json` next to the skill: `created_by: agent`, counters, anchor. Every counter bump, patch, archive and curator input checks it; human and third-party skills are never written or counted. |
| Recall | `hook/on_session_start.py` | A compact index of the agent's own skills, grouped by category, is injected at `SessionStart` (`additionalContext`). The host's native recall is left alone; the index is added on top (`AUTOHARNESS_INDEX_SUSPENDED` turns it off to measure its worth). |
| Validation in use | `hook/on_skill_call.py`, `lib/lifecycle.py` | `use` = `PreToolUse(Skill)`; `view` = a main-session `Read` into the managed dir (their measurement found direct Reads are the dominant consumption path, the `Skill` tool is rare). Per-layer **request counter** (+1 per turn) is the denominator; the skill's `anchor` is the counter at creation. No benchmark, no LLM judge. |
| Probation / maturity / capacity | `lib/lifecycle.py`, `config.py` | Denominator below `MATURITY` (project **100**, global **300**) = probation: live, indexed, never evicted, outside the cap. At maturity, `use == 0 and view == 0` is archived (graduation review). Graduates die only by capacity: when the mature pool exceeds `CAPACITY` (project **50**, global **20**), the lowest `use/denominator` go first, ties broken by a deterministic hash. Pure function, no disk. |
| Curator | `agents/curator.md`, `hook/spawn.py:run_curator` | Every `CONSOLIDATE_EVERY_N` = **250** tool calls (about 5x the reflection cadence). Sees only agent-created skills. Folds prefix clusters into class-level umbrellas by `patch` + `delete(absorbed_into=...)`, demotes detail to `references/`, backfills `category`. Told explicitly not to use usage counts to decide. A **tar.gz snapshot** of each layer is taken first (`SNAPSHOT_KEEP` = 5); `delete` is an archive move, never a hard delete. |
| Ledger | `lib/ledger.py` | Per-skill append-only `.ledger.jsonl`: action, reason, evidence path, `absorbed_into`. Lets a merge be told from a death. |
| Recursion and privacy | `config.py`, `hook/dispatch.py` | `AUTOHARNESS_CHILD_SESSION` env set only by spawn; Stop, tool counting and skill counting all short-circuit on it, and the reflector's `Write/Edit` are denied by a top-level hook backstop. Redaction (`lib/redaction_rules.toml`): AWS keys, private-key headers, GitHub and Slack tokens, bearer tokens, `api_key=`-style assignments, plus PII (email, SSN, phone, Luhn-gated card numbers). |
| Notification | `lib/notify.py` | Opt-in desktop or command notification of the run account; otherwise a one-line summary at the next session start. |

Two things worth carrying over that are easy to miss: the reflector is told to
capture liberally because **retirement is cheap and forgetting is not**, and
the curator is told **not to trust counters** that are still mostly zero.

### What smind already has

- **One daemon runs every agent** (claude-native, GLM/Kimi over ACP,
  codex-native) behind `internal/taskrunner.Runner`, so learning can be
  provider-agnostic and daemon-side. No per-provider hooks.
- **Structured run events, persisted** (ADR-0008, `run_events`):
  `EventTypeToolCall` (id, name, input, status, result), permission request
  and resolved (with `PermissionResolution`: human vs policy/timeout),
  `EventTypeUsage`, `Done` with `StopReason`, errors, `McpServers` (names
  only), thinking, user message. **Gap:** Codex-native emits no tool-call
  events (ADR-0008), so Codex runs are text-only evidence and cannot show
  skill loads.
- **Run usage** (ADR-0020, `run_usage`) for the cost-control side.
- **Profiles** (ADR-0014/0019/0025): provider, model, thinking, permission
  mode. A reflector profile is a profile.
- **Prompt queue** (ADR-0021, `chat_queue.source` = human | orchestrator |
  agent): mid-run human prompts are identifiable, which is exactly the
  "user correction" signal the reflector prizes.
- **Redaction already exists** in three places: `taskrunner.scrubSecrets`
  / `mcpSecrets` (exact-value scrub of configured MCP env/header values),
  `permission_detail.go` (diffs capped, content withheld), and ADR-0018's
  rule that env/header values never appear on a wire, in a log or in an
  event.
- **Skills machinery** (#279, `internal/skills`): embedded bundle, per-skill
  manifest `.smind-managed-files.json` (path -> sha256), `smind skill
  install|ls|uninstall` into `~/.claude/skills`, `~/.agents/skills`,
  `~/.codex/skills`, touching only files smind wrote.
- **Delivery hooks**:
  - Claude Code CLI loads `.claude/skills/` from any `--add-dir` directory
    (`refs/claude-code/CHANGELOG.md:4321`), and `claude-agent-sdk-go` v0.3.2
    has `WithAddDirs` (`client.go:319`).
  - Codex reads `<cwd>/.agents/skills/<name>/SKILL.md`
    (`refs/codex/codex-rs/core/tests/suite/skills.rs`). It has no per-thread
    add-dir or MCP (ADR-0018), so no clean injection.
  - ACP agents (GLM, Kimi) have **no skill concept**, and smind's
    `fs/read_text_file` boundary is the task worktree (`internal/acp/fs.go`),
    so a GLM agent cannot read a skill file kept outside the worktree.
- **Hazard found while reading:** in this repo `.claude/skills` is a symlink
  into the committed `.agents/skills`. Any design that writes learned skills
  into a repo's `.claude/skills` or `.agents/skills` can write through that
  symlink into tracked files, and in any repo produces untracked noise. This
  is the main argument for not using the repo as the storage or delivery
  location.

## Decision

### 1. Shape

A new `internal/learning` package inside the daemon, with no hooks into any
provider:

```
runs finish ──► collector ──► redactor ──► reflector job ──► gate ──► proposal ──► (human approve) ──► active
                  ▲ per-chat watermark          │ hidden system run            │ deterministic         │
                  │                             │ (profile, no tools)          │ validation            ▼
 run start ◄── projector (index preface / add-dir) ◄── active skills ◄── lifecycle (counters, probation, capacity)
                                                              ▲
                                  curator job (hidden system run) + snapshots + ledger
```

The model only **proposes** (a JSON object in its final message; it has no
tools and no write path). A deterministic Go gate is the only code that can
turn a proposal into a skill, as autoharness's promoter is. This is the most
valuable property to keep.

### 2. Trigger and input

- **Unit of learning: a chat's runs since its watermark.** Each chat stores
  `learn_watermark` = last `run_events.id` reflected. A window is the events
  of that chat's runs after the watermark.
- **Triggers** (all daemon-side, off the user's critical path):
  1. **Cadence:** when a run reaches a terminal state, add its tool-call
     count to `chat.learn_pending`; if `>= 50` (autoharness's default; one
     knob) and no job is in flight for the workspace, enqueue a reflection.
  2. **Explicit:** "Learn from this chat" (UI) / `smind learn reflect
     <chat>` (CLI): the `/learn` equivalent. Bypasses the cadence and the
     cool-down, not the budget or the gates.
  3. **Tail flush:** a chat archived or idle for 30 min with
     `learn_pending >= 10` reflects once (autoharness's `SessionEnd`).
- **Eligible runs:** `runs.purpose = 'user'`, workspace opted in (section 8),
  provider that emits tool events (claude-native, GLM/Kimi). Codex runs
  contribute only their prompt/text events and never count toward cadence,
  until Codex tool events exist.
- **What the reflector sees** (one rendered, redacted, capped bundle):
  - user turns, **marked by origin** (`human` queue/composer vs
    `orchestrator`/`agent`; a mid-run human prompt is tagged `correction`
    when it was delivered while a run was active or right after a failure);
  - assistant text, clipped;
  - tool calls: name, a one-line input summary, status, and the result only
    for failures (error text, clipped) and for edits (path + diff counts, not
    content, reusing `permission_detail` semantics);
  - **failure -> recovery pairs** (a `failure` tool call followed within the
    run by a successful call to the same tool or path), marked as such;
  - permission requests and how they resolved (denials by a human are a
    strong signal; auto-approved ones are noise);
  - the run's stop reason and terminal error;
  - which learned skills were loaded this window (from the counters, section
    6), so the reflector can patch what was in play;
  - the description index of the workspace's learned skills **plus the names
    and descriptions of human/third-party skills in the workspace**
    (read-only, for dedupe; their bodies are never sent).
- **Never included:** run env, MCP `env`/`headers`/args/URLs (the
  `McpServers` event is already names-only), thinking events, `ToolResult`
  of reads of secret-shaped paths (`.env*`, `*.pem`, `id_*`, `*credentials*`,
  `*.key`, keychains), file bodies beyond a 200-byte head, binary or base64
  blobs, the account and credential tables.
- **Redaction** is layered and runs on the daemon **before** the bundle is
  built and again on the way **in** to the store (belt and braces, as
  autoharness redacts at capture and at evidence materialization):
  1. exact-value scrub of every configured MCP secret and every env value of
     the run (`scrubSecrets`/`mcpSecrets`, ADR-0018);
  2. a Go port of a rule set equivalent to autoharness's
     `redaction_rules.toml` (AWS key id, private-key header, GitHub/Slack
     tokens, bearer tokens, `key=value` secrets, JWT shape, Luhn-gated card
     numbers), plus emails and home-directory path prefixes -> `~`;
  3. high-entropy token heuristic for strings >= 24 chars of base64/hex
     alphabet not in a known-safe list (git SHAs).
  Over-redaction is the safe side. A redaction unit test corpus is a Phase B
  acceptance item.
- **Caps** (initial, all in `config.yaml`): record 4 KB, per tool input 1 KB,
  per tool result 1 KB (failures 2 KB), window **120 KB** (tail kept, with a
  truncation mark), at most **1 reflection per workspace per 30 min**, a
  prior-context digest of the 10 preceding user turns (text only, evidence
  forbidden).

### 3. Reflector

- **Who runs it:** a **hidden system run** through the existing
  `taskrunner.Runner`: a task row with `kind = 'system'` (not shown in the
  task list or sidebar, no worktree, `cwd` = an empty daemon temp dir), a
  run with `runs.purpose = 'reflect'`, and a dedicated **profile**
  (`learning-reflector`, stored like any profile, editable in Settings ->
  Agents but flagged system).
  Rationale: it reuses provider auth (a claude.ai login cannot call the API
  directly, ADR-0020), model selection (ADR-0025), usage metering and
  cancellation, and adds no second LLM client.
- **Provider and model (proposal):** the **same-recipient rule**: the
  reflector uses the same provider as the source run, so reflection never
  sends a transcript to a provider that did not already see it. For
  claude-native that is the cheapest current Claude model (Haiku 5.5),
  configured as the **full model id** `claude-haiku-5-5` and not autoharness's
  `haiku` alias: the live check (2026-10-19, claude 2.1.293) saw `--model
  haiku` answered by Sonnet in 3 of 3 calls (init said Haiku; `modelUsage`
  and the assistant turn said Sonnet, about 30x the cost) while the full id
  was answered by Haiku. `learning.reflectorModel` / `learning.curatorModel`
  stay overridable (a newer id when one ships; `""` = provider default); for
  GLM/Kimi, the GLM default. The user can
  override per workspace, which then needs an explicit "send transcripts
  summaries to <provider>" acknowledgement.
- **Tools and permissions:** none. claude-native: empty tool allowlist and
  the most restrictive provider-native permission mode (ADR-0019); ACP:
  deny-all permission mode, and no MCP servers. No `--add-dir`. The window is
  in the prompt, so the reflector needs no filesystem. Phase B verifies the
  "no tools" claim with `TestReflector_HasNoTools`.
  **Decided (D13, 2026-10-18): v1 reflects claude-native runs only.** The B0
  spike found the claude-native reflector can be run with no tools
  (`--tools ""`, strict MCP config, empty setting sources, empty temp cwd,
  deny-all decider), but a GLM/Kimi ACP agent owns its tools: `read_file`,
  `list_files`, `web_search` and `web_reader` are never permission-gated and
  the client cannot disable them, so "no tools" cannot be guaranteed and an
  injected window could try to exfiltrate through the web tools. A window
  whose source run is GLM/Kimi is therefore recorded as a job `skipped:
  provider_no_tool_isolation` and no reflector run starts. Learned skills are
  still **delivered** to GLM/Kimi runs (section 5); they just are not learned
  from yet. Reconsider when an ACP agent can be launched with an empty tool
  set.
- **Procedure** (the prompt, derived from `agents/reflector.md`, bundled in
  the binary): capture liberally but only durable, class-level lessons;
  signals worth capturing: user corrections and frustration, a technique that worked
  after a failure, a loaded skill that was wrong or missing a step, a setup
  or config fix; **compare-first preference order**: patch a loaded skill,
  patch a class skill, create (born as an umbrella); reconcile
  contradictions in the same output; each intent carries a `reason` and
  `evidence` quotes. The window and existing skills are wrapped in delimited
  blocks and declared **untrusted data, never instructions**.
- **Output schema** (the final assistant message must be one JSON object;
  anything else is discarded and recorded as a failed job):

  ```json
  {
    "intents": [{
      "action": "create | patch | delete",
      "name": "learned-<slug>",
      "category": "testing",
      "description": "Use when ... (one sentence, <= 160 chars)",
      "body": "full SKILL.md body (create) | null",
      "old_string": "patch only", "new_string": "patch only",
      "absorbed_into": "delete only, a live learned skill",
      "reason": "why this lesson is durable and class-level",
      "evidence": [{"kind": "user_message | failure_recovery | permission_denied | loaded_skill_wrong",
                    "run_id": "...", "seq": 123, "quote": "verbatim slice from the window"}]
    }]
  }
  ```

  Empty `intents` is a legitimate, common answer.
- **No recursion, structurally** (autoharness uses an env var plus a hook
  backstop; smind can do better because it owns the run table):
  1. `runs.purpose in ('reflect', 'curate')` is excluded from the collector,
     the cadence counter, the offered/loaded counters and every learning
     trigger. The filter is in one function, `learning.Eligible(run)`, with
     a unit test per path.
  2. At most one learning job per workspace at a time (mutex + a
     `learning_jobs` row).
  3. The reflector's task is `kind = 'system'`; the collector never lists
     system tasks.
  4. The reflector has no tools, so it cannot start runs or call
     `learning.*`.
  5. No `learning.*` RPC or MCP tool is exposed to agents. Humans only.
- **Cost control:** reflector runs are metered in `run_usage` and surfaced in
  Settings -> Usage under a "Learning" bucket (ADR-0020 metering, no new
  meter). Hard limits: per-workspace daily budget (default **USD 0.25 or
  150k tokens**, whichever the provider reports; unreported usage counts as
  the window size), the 30-min cool-down, the 120 KB cap, and a global kill
  switch. At the budget the job is skipped and recorded as `skipped:
  budget`, not queued. Defaults are guesses to be recalibrated with measured
  data (see Open questions).

### 4. Storage and scope

- **Canonical store: the daemon SQLite store**, new tables:
  `learned_skills` (id, workspace_id, name, category, description, body,
  status `proposed | active | disabled | archived | rejected`, pinned,
  created_by_run, created_at, updated_at, counters, anchor),
  `learned_skill_versions` (skill_id, version, body, description, actor,
  created_at: the undo history), `learned_skill_ledger` (append-only:
  skill_id, action, reason, evidence json, `absorbed_into`, run_id, actor
  `reflector | curator | human`, ts), `learning_jobs` (kind, workspace_id,
  chat_id, window range, status, verdicts, cost, error), and columns
  `workspaces.learning_mode`, `chats.learn_watermark`, `chats.learn_pending`,
  `runs.purpose`, `tasks.kind`.
  Bodies are DB text, not files, so landing is one transaction, snapshots are
  rows, and there is no filesystem write that an agent could race. Pattern:
  `agent_profiles` / `mcp_servers` (ADR-0014/0018).
- **Self-authored marker:** every row in `learned_skills` is smind-authored by
  construction; the projection (section 5) is generated from those rows and
  carries the manifest convention from #279. smind **never reads for write,
  edits, counts or archives anything under `~/.claude/skills`,
  `~/.agents/skills`, `~/.codex/skills`, or a repo's `.claude/skills` /
  `.agents/skills`**. The only contact with human skills is read-only
  name+description indexing for dedupe, and the `learned-` name prefix is
  reserved so a learned skill can never shadow one.
- **Scope: per workspace** (the daemon's `workspaces` row, i.e. one repo
  across all its task worktrees). A skill learned in repo A is never offered
  in repo B. There is **no global layer in v1** (decision D5). A future
  "promote to global" would be an explicit human action that re-runs the
  repo-agnostic gate (no absolute paths, repo or host names, ids).
- **Not in the repo.** A repo-side copy is an explicit export (`smind learn
  export <skill> --to repo|claude|agents|codex`, reusing the #279 install
  machinery and manifest). After export the file is the user's, and smind
  stops managing it.

### 5. Delivery to agents

One projection, two carriers, chosen per provider. `learning.Project(ws)`
renders the workspace's `active` skills (and nothing else) into a
content-addressed, read-only directory
`$SMIND_HOME/learning/projection/<workspace_id>/<sha>/.claude/skills/learned-*/SKILL.md`,
regenerated from the DB when the active set changes. Agents never get a
writable path to the canonical store.

| Provider | v1 mechanism | Why | Load signal |
|---|---|---|---|
| claude-native | `claudecode.WithAddDirs(<projection dir>)`. The CLI loads `.claude/skills/` from added directories natively (`refs/claude-code/CHANGELOG.md:4321`), so Claude sees them through its normal skill list and can use the `Skill` tool or `Read` them. Optional index preface (off by default, D8). | Zero repo writes, no symlink hazard, native recall. | `Skill` tool call with that name, or `Read` of a path under the projection dir. |
| GLM, Kimi (ACP) | **Inline preface**: a leading text content block on `session/prompt` for the first prompt of an agent session (and after a session reset), holding the index and the full bodies of active skills within a byte budget (bodies are <= 25 lines each; budget 6 KB, over-budget skills fall back to index-only lines, ordered by load rate). The persisted `runs.prompt` and the UI transcript stay the user's original text. | ACP agents have no skill concept, and `fs/read_text_file` is limited to the worktree, so a file reference cannot work. Inline bodies need no tool support. | **Not measurable in v1** (no read happens). Marked `measured = false`; see lifecycle. |
| codex-native | **Not delivered in v1.** Codex reads `<cwd>/.agents/skills` but has no per-thread injection and no tool events; the only way is writing into the repo, which this ADR rules out. Explicit `smind learn export --to codex` remains available. | Honest about the gap rather than writing into the repo. | none |

Follow-up (Phase C, not v1): a built-in `smind-skills` MCP server exposing
`skill_get(name)` and `skill_list()` to ACP agents through the ADR-0018
mechanism, which would give GLM a measurable explicit load and let the
preface shrink to an index. Codex stays blocked on a per-thread MCP or skill
path (ADR-0018 "Codex deferred").

### 6. Validation in use (no benchmark)

Per skill, in `learned_skills`:

- `offered_runs`: runs (eligible, measured provider) that started while the
  skill was `active` and projected. This is autoharness's "denominator" in
  smind's natural unit, replacing its per-turn request counter.
- `loaded_runs`: distinct runs with at least one load event; `loads`: total
  load events. A load is a tool event matching the signal in section 5.
  autoharness learned (its E8 measurement, `hook/on_skill_call.py`) that a
  direct `Read` is the main path and the `Skill` tool is rare, so smind
  counts both, tagged `via: skill | read`.
- `patched`, `reused_after_patch` (first load after a patch: the closest
  available "the improvement helped" signal), `last_offered_at`,
  `last_loaded_at`.
- **Adherence is not guessed from counters.** The reflector gets the list of
  skills loaded in its window and is told to patch or flag (`loaded_skill_
  wrong`) any that the episode contradicted. That is autoharness's
  "patch a currently-loaded skill first" step and the only adherence signal
  with real content behind it.
- `measured`: false for skills whose only offered runs were on providers with
  no load signal. Unmeasured skills are **never archived for zero use**
  (absence of evidence, as the curator prompt says); they are subject only
  to capacity and to the human.

**Lifecycle** (pure function over rows, unit-testable, no disk, as in
`lifecycle.py`; defaults are knobs):

- **Probation:** `offered_runs < 25`. Active and offered, never evicted,
  outside the capacity count.
- **Graduation review** at `offered_runs >= 25`: `measured` and `loaded_runs
  = 0` -> `archived` (reversible, status change only). Unmeasured skills
  skip the review. Pinned skills are exempt from review and capacity.
- **Capacity:** at most **30 mature active skills per workspace** (autoharness:
  50 project, 20 global). Over the cap, archive the lowest `loaded_runs /
  offered_runs`, ties by a deterministic hash. New proposals are not
  discarded by capacity: if the cap is hit and nothing is evictable, new
  proposals wait in `proposed`.
- Archive never deletes: the row, versions and ledger remain; "Delete" is a
  human action and is the only hard delete.
- **Snapshots and undo:** every landing writes a `learned_skill_versions`
  row, so any create/patch/merge is undoable from the UI or `smind learn
  undo <skill>`. Before a curator run the daemon writes a **library
  snapshot** (JSON export of the workspace's rows, 5 kept), since a merge
  touches many skills at once and per-skill undo is not enough. Restoring a
  snapshot is explicit.

### 7. Curator

- **Cadence:** a curator job is enqueued when the workspace has had **5
  landed changes** since the last curation and the library has >= 8 skills,
  at most once per 7 days, or on demand (`smind learn curate`). Same budget
  and cool-down rules as reflection. (autoharness uses 250 tool calls, about
  5x its reflection cadence; smind has no per-turn counter, so landed
  changes is the equivalent "the library grew" quantum.)
- **Procedure** (`agents/curator.md`, adapted): sees only the workspace's
  learned skills with full bodies (<= 30 x 25 lines); folds clusters into
  class-level umbrellas by `patch` + `delete(absorbed_into)`; may demote
  detail into the umbrella body (v1 has no subfiles, see 9); backfills
  categories; must not decide on counters; must resolve contradictions
  inside the merge. Output uses the reflector's schema.
- **Landing:** merges are bundles (one approval covers the umbrella patch
  and the absorbed deletes it justifies); the absorbed skills are archived
  with `absorbed_into` only when the bundle lands, so a merge is never a
  silent death. The gate rejects an `absorbed_into` that is not a live
  learned skill of the same workspace (fail closed, as in
  `promoter.py`).
- **Ledger:** `learned_skill_ledger` records every create, patch, merge,
  archive, restore, approve, reject and delete with actor, reason and
  evidence; shown as the skill's provenance in the UI.

### 8. UX, approval, notifications

- **Settings -> Skills (learned)**, one section per the existing Settings
  registry (`settings-registry.ts`), plus a workspace switcher. Tabs:
  *Proposed* (the inbox), *Active*, *Archived*.
  - List row: name, category, description, source chip (reflector /
    curator), counters (`offered / loaded`, load rate, last loaded),
    badges (`probation`, `pinned`, `unmeasured`).
  - Detail: body, **provenance** (the runs that taught it, each linking to
    the run log at the cited `seq`, with the redacted evidence quote and its
    kind), version history with diffs, full ledger.
  - Actions: **approve**, **reject** (with optional note fed back to the
    reflector as a negative example), **edit then approve**, **pin**,
    **disable** (stop offering, keep), **archive/restore**, **undo to
    version**, **delete**, **export**.
  - Proposals show the diff against the current body for a patch, and a
    flagged banner when the guard scan found anything near the line.
  - **Possible duplicate (D12):** a proposal whose similarity to a live skill
    is above the medium threshold is flagged "Possible duplicate of
    `<skill>`" and shows both skills side by side with the similarity
    score. A learned match offers **Merge into this skill**: the proposal
    becomes a `patch` of the existing learned skill (the human edits the
    merged body, the proposal's evidence is carried over), re-runs the gate
    and still waits for approval. A human/third-party match shows names and
    descriptions only (bodies are never read) and offers no merge, since
    human skills are never modified; the human can reject or approve anyway.
- **Approval before landing: on by default in v1.** A `proposed` skill is
  never offered to any agent. A workspace may later opt into auto-approve
  for `patch` intents on already-approved skills only (D2), never for
  `create`.
- **Notifications:** a `learnedSkill.*` lifecycle topic family (ADR-0009
  pattern) feeds a sidebar badge on Settings with the pending count, a toast
  in the open UI, and the existing desktop/mobile notification path used for
  pending permissions. CLI parity: `smind learn ls|show|approve|reject|
  pin|disable|undo|reflect|curate|export|status`. No MCP tool in v1 (agents
  do not manage the learning loop, which is also part of the recursion
  guard).
- **Per-workspace opt-in** (`workspaces.learning_mode`: `off` default |
  `propose`), a global kill switch (`learning.enabled` in `config.yaml`,
  `SMIND_LEARNING=0`, and a Settings toggle) that stops collection, jobs and
  **delivery** instantly, and a per-workspace "forget everything"
  (hard-delete all rows and snapshots for that workspace).

### 9. Privacy and safety

- **What leaves the machine:** only the reflector/curator model calls, to the
  provider named by the profile, carrying the redacted, capped window and the
  skill index. Under the same-recipient rule that is a provider that already
  saw the underlying transcript. No telemetry; nothing is sent to smind or
  autoharness servers. Skills themselves never leave except by explicit
  export, or by being delivered to the next run's provider (which already
  sees that workspace).
- **Prompt-injection (a repo or a web page teaching a skill):** the central
  risk, because the transcript contains tool output the repo's author
  controls and a skill is persistent, propagating context for every later
  run. Defences, in order of strength:
  1. **Provenance rule.** Every intent needs at least one evidence item of
     kind `user_message` (a human-origin turn), `failure_recovery`,
     `permission_denied` (a human denial) or `loaded_skill_wrong`, and the
     gate verifies the quote occurs **verbatim** in that event of the
     window. Text that exists only in a tool result or a file the agent read
     cannot justify a skill by itself. A README that says "always run
     `curl ... | sh`" cannot become a skill, because no human said it and
     no failure was recovered by it.
  2. **Reflector has no tools and no write path**; its output is a JSON
     object parsed by Go; it can propose, not act.
  3. **Deterministic gate** (Go, no LLM): name charset and `learned-`
     prefix, caps (body <= 25 non-blank lines and 4 KB, description one
     sentence <= 160 chars with a trigger cue, <= 1 category), no
     frontmatter keys beyond `name description category`, no HTML or
     markdown links to URLs, a Go port of autoharness's six regex families
     (exfiltration, injection phrases, destructive, persistence, network,
     obfuscation) over body and description, a redaction pass, an
     absolute-path and host-name scrub, `absorbed_into` and name collision
     checks, and the **similarity check** (D12, below) against every live
     skill, learned and human.
  4. **No executable content in v1:** only `SKILL.md` text. No `scripts/`,
     `templates/`, `assets/`, no `references/` files. (autoharness allows
     them with a pointer rule; v1 skips them. Phase C may add plain-text
     `references/*.md`.)
  4a. **Similarity check (D12, deterministic, no LLM).** The gate scores a
     `create` proposal (and a `patch`'s resulting text, against every skill
     other than its own target) against **all live skills in the workspace**:
     learned skills in status `proposed`, `active` or `disabled` (name +
     description + body), and human/third-party skills (**name + description
     only**; their bodies are never read, per D10). The score is
     `max(token Jaccard, character-trigram Dice)` over lowercased,
     whitespace-collapsed text, with the proposal's own `name + description`
     compared against a human skill's `name + description` and its full text
     against a learned skill's full text. Above the **high** threshold
     (default 0.85) the gate rejects as `duplicate_of: <skill>`, writing
     nothing. Above the **medium** threshold (default 0.60) it passes but
     records `possible_duplicate_of: <skill>` and the score on the proposal,
     which the inbox shows (section 8) with a **Merge into this skill**
     action. Both thresholds live in `config.yaml`
     (`learning.similarity.duplicate`, `learning.similarity.possible`); the
     defaults are guesses calibrated against a fixture corpus in the gate
     tests and revisited with real data. This closes the gap that the gate
     only compared names against human skills; the reflector and curator keep
     their LLM-side dedupe on top.
  5. **Human approval** with the evidence on screen.
  6. **Blast radius:** workspace-scoped, read-only projection, no global
     layer, so a poisoned skill affects only that repo's later runs and one
     click removes it.
  7. **Integrity:** the projection is read-only and rebuilt from the DB on
     every change; an agent that edits it only changes a copy. Hash of the
     projection is recorded on each run.
- **Residual risk, stated plainly:** a skill that looks benign but is subtly
  wrong (a wrong "always"), approved by a hurried human, will steer later
  runs. Mitigations are the load-rate and patch loop, visible provenance,
  one-click disable, and the fact that skills are text, not code. The
  user should treat approval as code review.
- **Secrets at rest:** evidence quotes are redacted before storage; the DB
  already holds credentials in plaintext (ADR-0018 decision 4), so this adds
  nothing worse, but evidence is bounded (<= 3 quotes x 500 chars per
  intent) and purged with the skill.

## Gap and fit table

| autoharness feature | smind equivalent today | New work |
|---|---|---|
| Hooks (`SessionStart`, `PreToolUse`, `Stop`, `SessionEnd`) | Daemon run lifecycle (`runs` terminal state, ADR-0009 topics, chat archive/idle) | `learning.Collector` subscribed to run-finished; no provider hooks |
| Tool-call counter trigger (N = 50) | `EventTypeToolCall` events persisted in `run_events` | `chats.learn_pending/watermark`, cadence + tail flush + explicit trigger |
| Raw transcript window + digest | `run_events` rows (structured), `chat_queue.source` | Renderer producing a redacted, capped, origin-tagged bundle; failure->recovery pairing |
| Redaction rules + egress redline | `scrubSecrets`/`mcpSecrets` (exact values), `permission_detail` redaction, ADR-0018 no-env-on-wire | Go regex rule set + entropy pass, secret-path denylist, corpus tests |
| Reflector subagent (Haiku, `stage_skill` MCP, `--dangerously-skip-permissions`) | `taskrunner.Runner` + profiles (ADR-0014/0025), usage (ADR-0020) | Hidden `system` task/run, `learning-reflector` profile, no-tools mode per provider, JSON-output schema, prompt bundle |
| Recursion guard (`AUTOHARNESS_CHILD_SESSION`, hook deny) | `runs` table is ours | `runs.purpose`, `tasks.kind='system'`, single `Eligible()` filter, job mutex |
| Promoter (sole writer, in-memory validation, atomic land) | Go, SQLite transactions | Gate package: schema, caps, guard scan, provenance check, transactional landing |
| `skills_guard` six regex families | none | Go port of the families |
| `created_by:agent` sidecar | `.smind-managed-files.json` manifest (#279) | DB-owned rows (self-authored by construction), reserved `learned-` prefix |
| Project and global layers | per-workspace rows (`workspaces`) | Workspace layer only in v1; global = explicit promote, later |
| Session-start index injection | none | ACP inline preface; Claude via native add-dir skill listing, optional index preface |
| Skills dir the host reads | Claude `.claude/skills`, Codex `.agents/skills` | Read-only content-addressed projection + `WithAddDirs`; no repo writes |
| `use` (Skill tool) / `view` (Read) counters | tool events with names/inputs (Claude, ACP) | Load-event matcher, `offered_runs`, `loaded_runs`, `measured` flag; Codex gap (no tool events) |
| Probation, graduation review, capacity pruning | none | Pure lifecycle function + knobs |
| Curator + tar snapshots + `absorbed_into` | none | Curator job, library snapshot, merge bundle |
| Per-skill ledger | none | `learned_skill_ledger`, `learned_skill_versions` |
| Body/description caps (25 lines, 60/1024 chars) | none | Gate caps (25 lines, 4 KB, 160 chars) |
| (none: autoharness dedupes only through the reflector/curator prompt) | n/a | **Deterministic similarity check** in the gate against all live skills, with an inbox "Possible duplicate" flag and merge action (D12, new) |
| `/learn` skill | CLI, UI, MCP | "Learn from this chat" button + `smind learn reflect` |
| Opt-in notification | desktop/mobile notification path, lifecycle events | `learnedSkill.*` topics, badge, toast |
| (none: autoharness has no approval step) | n/a | **Human approval inbox** (new; smind's addition) |
| (none: autoharness trusts the transcript) | n/a | **Provenance rule** against injection (new) |
| (none: autoharness writes into `.claude/skills`) | n/a | Daemon-owned storage, projected read-only (new; avoids the `.claude/skills` -> `.agents/skills` symlink hazard) |

## Decisions

Accepted by the user on 2026-10-18: D1-D11 exactly as recommended, plus D12
added by the user, and D13 (reflector tool isolation) decided later the same day.

| # | Decision | Options | Decided |
|---|---|---|---|
| D1 | Opt-in vs on by default | per-workspace opt-in, default off / on by default with approval / global-only toggle | **Per-workspace opt-in, default off.** Learning sends transcript excerpts to a model and writes persistent context; the first run in a new repo should not do that silently. A one-time prompt in Settings offers to enable it. |
| D2 | Approval before a skill lands | always / on for create, off for patch of approved skills / off | **Always in v1** (every proposed skill and every curator merge waits in the inbox). Revisit an auto-approve for patches after a few weeks of measured approve/reject rates. |
| D3 | Storage location | daemon DB + projection / repo `.claude/skills` / `~/.claude/skills` / both | **Daemon DB canonical, delivered by read-only projection; repo/`~/.claude` only via explicit export.** Avoids git noise, the symlink write-through in this repo, and agents editing approved skills. |
| D4 | Reflector model and cost budget | same provider as the source run / always Claude Haiku / always GLM | **Same-recipient rule**, Haiku-class for Claude, GLM default for GLM; per-workspace daily cap **USD 0.25 / 150k tokens**, 1 job per 30 min, 120 KB window. These numbers are guesses; calibrate from the first week of `run_usage` before widening. Confirmed by the user as proposed. |
| D5 | Can skills ever be global | never / explicit promote / reflector-proposed | **No global layer in v1; later, only an explicit human "promote" that re-runs a repo-agnostic gate.** A global skill's blast radius is every repo, and cross-repo injection is the worst case. |
| D6 | Provenance rule strictness | evidence must include a user message or recovered failure / any evidence / user message only | **User message, recovered failure, human permission denial, or a loaded skill proved wrong.** It is the main injection defence; it will cost some recall (lessons seen only in tool output are dropped), which is the intended trade. |
| D7 | Measuring GLM/Kimi skill use | v1 `skill_get` MCP / v1 inline only / never | **Inline only in v1, `skill_get` MCP in Phase C.** v1 treats those skills as unmeasured (never auto-archived for zero use). Cheaper and it ships GLM delivery now, at the cost of no GLM adherence data for a while. |
| D8 | Claude index preface | native listing only / add an index preface | **Native only** to start; autoharness found the index helps recall, so build the switch and decide with measured `loaded_runs / offered_runs`. |
| D9 | Codex | exclude in v1 / write `.agents/skills` in the worktree | **Exclude in v1** (no tool events, no clean injection, repo writes ruled out). Explicit export still works. |
| D10 | Show human skill descriptions to the reflector for dedupe | yes (names+descriptions only) / no | **Yes, read-only, descriptions only.** Avoids learning what the repo already documents. Bodies never sent. |
| D11 | Numbers: cadence 50 tool calls, probation 25 runs, capacity 30, curator every 5 landings | keep / change | **Keep as defaults, all knobs in `config.yaml`.** They are autoharness's, rescaled; there is no measured basis yet. |
| D12 | Duplicate detection beyond names | name collision only / LLM-side dedupe only / deterministic similarity check in the gate | **Deterministic (no LLM) similarity check against all live skills, learned and human** (human: name + description only, bodies never read). Above a high threshold the gate rejects as duplicate; above a medium threshold the inbox flags "Possible duplicate of `<skill>`", shows both side by side and offers "Merge into this skill" (the proposal becomes a patch of the existing learned skill, still approval-gated). Thresholds in `config.yaml` (defaults 0.85 / 0.60). Reflector and curator keep their LLM-side dedupe. Added by the user 2026-10-18. |
| D13 | Reflector tool isolation on GLM/Kimi | (1) claude-native-only reflection in v1, GLM/Kimi windows skipped / (2) GLM reflector with mitigations, accepting web egress / (3) tool-less direct API call (a second LLM client) / (4) Claude reflects GLM transcripts (breaks D4 same-recipient) | **(1) Decided by the user 2026-10-18.** v1 reflects claude-native runs only; GLM/Kimi-sourced windows are recorded `skipped: provider_no_tool_isolation`; learned skills are still delivered to GLM/Kimi. `TestReflector_HasNoTools` asserts no-tools for claude-native; a test asserts the skip for ACP-sourced windows. |

## Alternatives considered

- **Port autoharness as a Claude Code plugin.** Rejected: Claude-only, so GLM
  and Codex runs (most of smind's value) would not learn; relies on host
  hooks and a Python runtime; writes into `.claude/skills`.
- **Reflect inside the source run's session (fork/resume, autoharness's
  `fork` carrier).** Rejected for v1: ties the reflector to a provider's
  resume semantics (not available for ACP) and puts the model in a session
  with tools. Reconsider for claude-native for cache cost.
- **Write learned skills straight into the repo.** Rejected: untracked
  noise, symlink write-through in this repo, a poisoned skill becomes a
  committed file, agents can edit it.
- **Let the reflector write files or call a `stage_skill` MCP tool.**
  Rejected: needs a tool-permissioned run; a JSON answer parsed by Go gives
  the same proposal channel with no write path.
- **Auto-land with a regex guard only.** Rejected: regex stops explicit
  strings, not a plausible wrong rule (autoharness's own comment: "regex
  heuristics, not sandboxed execution analysis").

## Consequences

- New daemon subsystem (`internal/learning`), five tables and four columns,
  a `learning.*` wsapi surface, `smind learn` CLI, one Settings section.
  Additive; no existing RPC changes shape. `runs.purpose` and `tasks.kind`
  need every task/run listing and every metric to filter system rows.
- Runs gain a read-only add-dir (claude-native) and, for ACP, a leading
  prompt block; both only when the workspace has active skills.
- Reflection costs tokens. Bounded by the budget; visible in Usage.
- Providers differ: Claude is measured, GLM is delivered but unmeasured,
  Codex is out. That asymmetry is documented, not hidden.
- Reversible: kill switch stops everything; skills are rows with history.

## Open questions (resolve in Phase B spikes, not by the user)

1. Confirm the `claude` CLI, in the SDK's stream-json mode, loads
   `<add-dir>/.claude/skills` and reports the `Skill` tool call input shape
   (field name) so the matcher is exact. Fall back: `WithPluginDirs`.
2. Confirm a no-tools claude-native run (empty allowlist) cannot run Bash,
   per provider, with a test.
3. ACP `session/prompt` accepts a leading text `ContentBlock` without
   changing `PromptResponse.usage` semantics (ADR-0020, GLM 1.14.0 note).
4. Actual cost of a 120 KB window on Haiku-class and GLM, to set D4.
5. Whether a human-origin queued prompt is reliably distinguishable from an
   orchestrator prompt in `chat_queue.source` for runs started directly (not
   through the queue).
