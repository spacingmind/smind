# 0053: Store only the run skeleton; rebuild the full history from the provider

## Status

**Accepted — 2026-11-05 (Human decision, every recommendation).** Direction (2026-11-05): smind keeps a small **skeleton** of
every run and rebuilds the **full history on demand** from the provider's own session, instead of storing every chunk and every
tool output forever. The Human accepted H1–H8 as recommended (recorded under [Human decisions](#human-decisions)); implementation
proceeds one PR at a time, each reviewed by the Human. The spec and the PR split are in
[`docs/plans/active/run-skeleton-storage.md`](../plans/active/run-skeleton-storage.md).

Builds on the lazy history and byte budget of `docs/plans/active/daemon-memory.md` (#823) and **amends** the 30-day retention spec
`docs/plans/active/run-events-retention.md` (#827, being implemented in parallel; see [Retention](#retention-827)). Touches
[ADR-0008](0008-structured-run-events.md) (event shape; `EventType` stays append-only), ADR-0010 (unknown ACP kinds), ADR-0016 (one
provider session per chat), ADR-0041 (orchestration readers), ADR-0048 (fleet reads).

## Context

`run_events` stores one row per event, forever, in full: every streamed text/thinking chunk of an ACP or Codex run (`record`,
`internal/runs/registry.go`, appends one row per event; only the claude-native streamer coalesces), every tool input and every tool
result as raw JSON. `smind.db` is 848 MB and almost all of it is `run_events`. #823 bounded RAM; #827 bounds the disk by deleting
the detail of runs older than 30 days. Neither asks the question this ADR does: **why does smind store a copy of what the provider
already stores?**

What was read for this ADR (2026-11-05):

- **A run is one turn of a chat's provider session.** `SessionHandle{Provider, SessionID}` is persisted per chat
  (`chats.agent_session`, `internal/taskrunner/chat_session_store.go`), not per run: several runs share one session. So the native
  session holds a run's history only as a *slice*, and the handle alone does not say which slice.
- **Provider sessions are real, on-disk or RPC-reachable, and richer than our copy** (matrix below).
- **Real payloads are lighter than the 848 MB suggests.** Sizes only, from 5,674 Claude Code tool results and 1,298 Codex tool
  outputs in this machine's own transcripts: p50 295 B / 1.2 KB, p90 2.2 KB / 6.8 KB, p99 10 KB / 40 KB, max 58 KB / 40 KB (that sample was
  small; the real database has a fatter tail, see [Measurements on the real database](#measurements-on-the-real-database-pr0-2026-11-05)). Truncating every payload to 4 KB head + 4 KB tail would save only 10–27% of payload bytes. **The row
  count dominates**: with per-chunk rows a 40-byte streamed chunk costs about 280 B on disk (JSON key overhead, the table row, two
  indexes). See [Measurements](#measurements): coalescing rows, not excerpting payloads, is the larger lever, and the real
  distribution must be taken on the Human's database before the thresholds are frozen (PR0).

## Decision

### 1. What smind keeps for every run (the skeleton)

Skeleton = everything needed to list, audit, cost, hand off and navigate a run, and to find its full history elsewhere:

| Kept (forever, unless H2 says otherwise) | Form |
|---|---|
| User prompts | text, cap 16 KiB per message (longer: head+tail excerpt) |
| The agent's text answers | **coalesced** into one row per contiguous run of chunks, cap 32 KiB per row (longer: head+tail excerpt; the trailing text is also `runs.final_text` from #827) |
| Per tool call | one row: tool call id, name, **args summary** (≤ 512 B; `ElideToolCall` output, so heredoc bodies are elided exactly as today), status, duration, exit code, bytes in/out, `resultSummary` (≤ 200 B), and a `payload` descriptor `{bytes, sha256, excerpt: bool}` |
| Heredoc bodies | not kept; per elided body `{index, bytes, sha256}` so a rebuilt body can be verified |
| Permission requests and decisions | rows as today (summary, options, resolution, rule id); `PermissionDetail.RevealBodies` stays off disk as today |
| Usage / cost / rate-limit windows, failure class | `run_usage` and the run row, unchanged; `EventTypeUsage`/`EventTypeRateLimit` rows kept as rows |
| Commits / PRs / diff stat | the git provenance already on the run row, plus `diffStat {files, +, -}` captured at run end |
| Native persistence handle | new `runs.native_ref` (JSON): `{provider, sessionId, configDir/codexHome, rolloutPath?, cwd, providerVersion, turnWindow{startedAt, finishedAt}, promptSha256, handleAt}` captured when the run ends (and at start for a run that may crash) |
| Thinking | **not kept**: one row `{type: thinking, bytes, sha256}` (H1) |
| MCP server state, session notes, mode changes | unchanged (tiny) |

Everything else is a **large payload**: tool inputs and results, file reads, test logs, diffs.

### 2. Large-payload thresholds (D2; measured, see Measurements)

**Frozen 2026-11-05 (H6) after the real-data measurement: 512 B / 8 KiB / 4+4 KiB are unchanged.** The reasons are under
[Measurements on the real database](#measurements-on-the-real-database-pr0-2026-11-05); the shares quoted in the three bullets below come
from the earlier small sample and are superseded by the real ones there.

For a payload (tool input or result, or an over-cap text row):

- **≤ 512 B**: inline in the skeleton row (it *is* the summary). Covers 63% of Claude Code and 37% of Codex tool results and 73% of tool inputs in the sample.
- **512 B – 8 KiB**: stored whole in a new table `run_excerpts` (the **excerpt tier**), skeleton row holds only the summary and the
  descriptor. Holds 98.5% of Claude Code and 93% of Codex results whole; the cost is bounded because this tier is what #827 prunes.
- **> 8 KiB**: excerpt tier holds **head 4 KiB + tail 4 KiB**, plus `bytes` and `sha256` of the whole payload. 4 KiB is chosen
  because an error or a test summary lives in the tail and a command's intent in the head; the middle is what the provider session
  can give back. The hash lets a rebuild prove it returned the same payload.

All three numbers are config-free constants in v1 (a knob invites 3 untested corners). `sha256` is stored only for payloads > 512 B,
so a short low-entropy secret is never published as a brute-forceable hash.

### 3. Shape on disk: write full, finalize at the end (D3)

A **running** run keeps today's behaviour: every event is appended in full. This is the "restart resume" guarantee: an upgrade or a
crash mid-run loses nothing, and the live-history reads (attach, permission reveal) are unchanged. When the run **finishes** (after
`finish`'s callbacks, the same point where #823 puts the run into the byte-budget LRU), a **finalizer** converts its rows to
skeleton form inside one transaction per run (a run over 64 MiB of rows is left in full form and logged: the largest run on the real database is 6 MiB, so batching was dropped; rows stay self-describing, below):

1. coalesce adjacent text rows (the first chunk's `id` and `seq` survive; the others are deleted: `seq` and `id` have gaps, which
   readers already must tolerate, #827 decision 3 test and `learning_collect.go`);
2. rewrite tool-call rows to summary + descriptor and move big payloads into `run_excerpts(run_id, seq, head, tail, bytes, sha256)`;
3. replace thinking rows by one descriptor row;
4. set `runs.skeleton_at`. Rows are **self-describing** (`event_data` carries `"v":2`), so a reader never needs a cursor to know
   whether a row is skeleton or full, and an interrupted finalizer (process death) leaves a mixed run that reads correctly and is
   finished by the next boot pass, exactly like #827's two-phase prune.

A run left `running` by a crash is reconciled as today; its rows are finalized by the same pass at next boot (idempotent).
Rejected: shaping at write time (a crash or upgrade mid-run would lose the payloads of the one run the Human is looking at, and a
live attach would show a skeleton of a run still in progress); a nightly batch (a delay with no benefit: the finalizer is cheap and
the cache already serves the just-finished run).

Independent quick win (H3): **coalesce persisted text/thinking rows at write time** for ACP/Codex (flush on a boundary event, on 250 ms
idle, at 4 KiB, at `Done`, and at shutdown; subscribers still receive every chunk live). It removes most rows before the finalizer
ever sees them and shrinks the live write load.

**Implemented (PR1, `internal/runs/persistbuf.go`).** Each run has a persist buffer for consecutive plain text or thinking chunks
(a chunk that carries any other persisted field is a boundary, guarded by a field-count test). It is flushed, as ONE row written once
(never updated), when: a non-chunk event arrives; the kind changes (text/thinking); the next chunk's seq is not the one right after
the buffer's last (another goroutine recorded an event in between, so merging would reorder text around it); the buffer would pass 4 KiB
(one larger chunk is its own row); 250 ms pass since the last chunk (timer); or the run finishes (`finish` flushes before the terminal
status is stored, which is also what daemon shutdown and Stop go through). The row keeps the first chunk's `seq`; the other chunks'
seqs are gaps. `r.history` and every subscriber are untouched: they still get each chunk.

*Durability window.* A process that dies without running `finish` (kill -9, power loss) loses the open buffer: at most 250 ms of
streaming, and at most 4 KiB of text of one kind, per running run. Everything before the last boundary or flush is on disk. Stop, an
agent error and a daemon shutdown lose nothing. The window is acceptable because a crashed run is reconciled as interrupted anyway
and its history is a transcript, not state the run resumes from. A multi-byte character split across two chunks was stored as
replacement characters before (each chunk was encoded alone); merged, it is stored intact.

*What readers see.* A run that finished after this change reads back with fewer, larger text/thinking events than it streamed (the
content, order and tool events are identical; `Tail(n)` counts rows, so a window of n events holds more text). Runs stored before
it are unchanged. Equivalence and per-reader tests: `persistbuf_test.go`, `persistbuf_readers_test.go`.

### 4. Full history on demand, from the provider (D4)

A **rebuilder** per provider kind reads the native session and returns `[]Event` for the run's turn window, through the **same
elider and redaction pipeline** as a live event (`ElideToolCall`, `secretdeny`, provider secret redaction), so a rebuilt history
shows heredoc bodies elided exactly as a live one does. It runs **only on the daemon that owns the run** (D8), only for a finished
run whose chat has no running run (the session must not be read while it is being written), and with bounds: 2 concurrent
rebuilds, 10 s, 64 MB, cancellable.

**Locating the slice.** The native session holds all turns of the chat. The rebuilder selects the turn whose user message has
`sha256 == native_ref.promptSha256` and whose timestamp lies in `turnWindow ± 2 min`; both must agree. Neither alone: hash alone
fails on a repeated prompt ("continue"), a window alone fails after a clock jump. No match, or two ambiguous matches, is "slice not
found" and degrades (D6); smind never guesses.

**Per-provider capability matrix** (verified 2026-11-05 against the code and the installed CLIs: `claude` 2.1.310, `codex-cli`
0.166.0; ACP column from the client code and the conformance fixtures, **not** from live agents):

| | claude-native | codex-native | ACP agents (GLM, Kimi, OpenCode, Copilot, Cline, Goose, Cursor, Gemini, ...) |
|---|---|---|---|
| Where the session lives | `<CLAUDE_CONFIG_DIR or ~/.claude>/projects/<cwd with every character that is not an ASCII letter or digit → `-`, so `/.smind/` is `--smind-`>/<sessionId>.jsonl` (verified on disk). An instance with a private `claude/` config dir (`claude_config_dir.go`) stores it there, so `native_ref` records the dir. Survives deletion of the worktree (keyed by the cwd *string*) | `<CODEX_HOME or ~/.codex>/sessions/YYYY/MM/DD/rollout-<ts>-<threadId>.jsonl` (verified), `archived_sessions/` after archive; instances use a private `CODEX_HOME` (`codex_endpoint.go`) | opaque, agent-owned; smind cannot read it |
| How smind reads it | parse the JSONL file directly (read-only, streaming) | **prefer the typed app-server RPC**: `thread/read {threadId, includeTurns}` and `thread/turns/list` exist in the 0.166.0 generated schema; the rollout file is the fallback | spawn the agent and call `session/load` **only** when the agent advertised `loadSession` (`Client.SupportsLoadSession`); the agent replays the whole session as `session/update` notifications. `session/resume` does not replay and is useless here |
| Format and stability | unversioned, internal, mixed record types (`user`, `assistant`, `system`, `attachment`, `queue-operation`, internal bookkeeping types we do not read); each record carries the CLI `version`. **Not a contract** | rollout lines are internal (`session_meta` carries `cli_version`); the app-server v2 protocol is typed and schema-generated, so RPC is the stable surface | the ACP wire is a published protocol, but what an agent replays (tool content, thinking) varies by agent |
| Cost / side effects | file read: a 55 MB transcript parses in ~150 ms (Python upper bound); median file 126 KB | RPC: spawn `codex app-server` (no model call) | spawn an agent process, possibly auth and MCP wiring; slower and with side effects, so **explicit user action only** |
| Rotation / deletion | Claude Code documents `cleanupPeriodDays` (default 30 days of inactivity). On this machine the setting is unset and 142 of 163 transcripts are older than 30 days by mtime, so cleanup is **not reliable either way**; handle both | no automatic expiry seen; `thread/archive` moves the rollout | agent-defined; unknown |
| `session/load` unavailable / session gone | n/a | n/a | marker `native_session_unavailable` |

**Where rebuild is not possible** the answer is the skeleton plus a clear marker (D6). OpenCode/others that expose no
`loadSession` have no rebuild at all; that is a fact of the provider, shown to the user, not an error.

### 5. Read contract (D5): never an error, always labelled

`History/Subscribe/Tail`, `run.logs`, `run.attach` return events plus an additive **`historySource`**: `live` (running),
`stored-full` (legacy/unfinalized rows), `skeleton`, `rebuilt` (from the provider), and an optional `skeletonReason`
(`rebuild_not_requested`, `provider_cannot_rebuild`, `native_session_missing`, `native_session_in_use`, `slice_not_found`,
`format_drift`, `rebuild_failed`, `owned_by_host:<id>`, `excerpt_pruned`). A new request field `detail`: `auto` (default),
`skeleton`, `full`.

- `auto`: a human surface (web, desktop, mobile attach) gets the skeleton at once and **may** upgrade: for claude-native and
  codex-native the daemon rebuilds on first view (cache-first) when the owner can; for ACP the UI offers "Load full history"
  (explicit, H5). The response never blocks on a slow rebuild beyond the 10 s bound; on timeout it returns the skeleton with
  `rebuild_failed`.
- `skeleton`: machine readers (steward digest/collector, learning executor/collector, parent notification, automations, fleet
  reads) and anything that must be deterministic. They **never** trigger a rebuild (no background agent spawns, no I/O in a
  worker's hot path).
- `full`: rebuild or fail soft to the skeleton with the reason; human surfaces only. `task_logs` and every agent/orchestrator read return the skeleton and never rebuild (H7, decided).

### 6. Deleted, rotated, drifted or foreign sessions (D6, D7)

Every failure of the rebuilder maps to a `skeletonReason` and a `session_note` event in the response (a blockquote already
rendered by web, desktop and mobile: no client change needed for correctness): **never an error status, never an empty history**.

**Version drift.** Each reader validates, then degrades; it does not try to be clever:

1. it records the CLI version found in the session next to the version in `native_ref.providerVersion`;
2. it parses with a tolerant reader (unknown record types skipped) and then **self-checks** the turn: the user message matches the
   prompt hash, every `tool_result` pairs with a `tool_use` in ≥ 95% of calls, and the tool call count is within ±10% of the
   skeleton's. A failed check is `format_drift`, the skeleton is served, and the counts (never text) are logged;
3. a per-(provider, version) **breaker** stops attempting after 3 consecutive drift failures for 1 hour (no I/O storm);
4. golden fixtures (sanitized, one per supported CLI version) run in CI; `smind doctor` reports the supported range and any drift
   seen. A new CLI release is therefore a visible, tested event, not a silent bug.

**Security.** `native_ref` is written only by the daemon at run end. A read path is canonicalized and must resolve under the
provider's known root (`<configDir>/projects`, `<codexHome>/sessions` or `archived_sessions`); a forged or stale path outside it is
refused as `native_session_missing`. Rebuild is authorized exactly as `run.logs` is (run-scoped agents reach only their own
subtree, relay/mobile as today); rebuilt text passes redaction like live text; the content stays untrusted data.

### 7. Where it runs (D8, fleet)

The rebuild runs **only on the daemon that owns the run** (the session lives in that user's home). A fleet read
(`internal/fleet`, `ListRunEventsAfterSeq`) returns the skeleton and `skeletonReason: owned_by_host:<hostId>`; the UI offers "Full
history on host X", a direct read on that host (hop limit 1 as for every other read). A mirror re-syncs a run when its
`skeletonRev` changes (the finalizer bumps it), so a mirror that saw live chunks converges on the skeleton.

### 8. Interplay (one section, one test each in the plan)

- **Heredoc reveal (#810/#813/#816).** Elision and redaction are unchanged. A **pending** permission reveals from the live request
  (unchanged, always works). A **finished** run's body comes from the rebuilt session (`ToolCallHeredoc`) and is accepted only if
  its `sha256` equals the skeleton's descriptor for that `index`; a missing or mismatching body is a typed `ErrHeredocUnavailable{reason}`
  (never "no heredoc"). The F1 elider is applied to rebuilt events through `ElideToolCall`, so the `index` counting is shared code.
- **Steward digest/collector, learning, parent notify, automations.** `detail=skeleton`; each reader in the audit table of the plan has
  an equivalence test (skeleton gives the same text answer, tool names, statuses, final text as the full rows). A detector that needs
  an output excerpt reads the excerpt tier while it exists (≤ 30 days) and treats absence as "not a learning input", as #827 decided.
- **Relay / mobile replay and attach.** A running run attaches live and full (unchanged). A finished run backfills the skeleton
  first; mobile does not auto-trigger an ACP load. Wire fields are additive and `omitempty`.
- **Restart resume.** A running run is stored full; an upgrade must not change a running run's rows. The finalizer starts only
  after `New()` reconciliation, never touches `status = 'running'`, and is idempotent (D3).
- **#823 byte-budget cache.** Yes: a rebuilt history enters the same LRU as a finished run's history, counted against
  `runs.historyBudgetMB`, tagged `source=rebuilt` and keyed by the session file `(size, mtime)` or the RPC turn id, so a changed
  session invalidates it. It is **never written back** to the store (one source of truth; otherwise the size returns). A hit is
  free; a miss costs one rebuild.
- **Retention (#827).** Next section.

### Retention (#827)

The two compose; #827 lands first (it is the smaller change and is in flight). This ADR then **amends** its decisions 1, 4 and 6:

- Prune of a run **in skeleton form** deletes only its `run_excerpts` rows (and the thinking descriptors' nothing: they are tiny).
  The skeleton rows, `final_text`, usage and the run row survive. `history_pruned_at` is renamed in meaning to "excerpts pruned at";
  the new column from this ADR (`skeleton_at`) says the rows are skeleton.
- Prune of a run still in **full** form (legacy runs, a run the finalizer has not reached) keeps #827's behaviour exactly: delete
  the rows, keep `final_text`, serve the synthetic three-event history.
- Reader contract on a pruned skeleton run: the skeleton history, with `skeletonReason: excerpt_pruned` on the tool calls whose
  excerpts are gone; the full history is still available **if the provider still has the session** (`detail=full`). So after 30
  days a user keeps: prompts, answers, every tool call's name/summary/status/exit/duration, permissions, usage, failure, git facts,
  and the pointer to the provider session. They lose only the head/tail excerpts and, unless the provider kept them, the middle.
- The age basis, never-prune rules (running, live task) and the two-phase crash safety of #827 are unchanged and apply to the excerpt
  table the same way (candidate runs are selected by `finished_at`; `DELETE FROM run_excerpts` is batched like its row deletes).
- Reclaim: deleting excerpts frees pages for reuse; the file shrinks by #827's incremental vacuum / `smind db compact`.

### Migration of existing runs (D9)

No compat shims. The question is what to do with the ~200+ finished runs already stored in full:

| | (a) leave them until the 30-day prune (#829) removes them | (b) convert all to skeleton at upgrade |
|---|---|---|
| Data | nothing lost before 30 days | recent runs lose their full detail **now**, and old runs have **no `native_ref`**, so there is no way back |
| Disk | unchanged until the 30-day pass and a compact | reclaimed only after a compact anyway |
| Risk | none new | an 850 MB rewrite at upgrade, in the middle of work |
| Code | one path | one path (same finalizer) but a destructive default |

**Decision: (a).** Legacy runs stay `stored-full` (`historySource` says so); the 30-day prune (#829) ages them out. A Human command
`smind runs skeletonize --before <date> [--dry-run]` (later PR) converts on request, with the `native_ref` caveat printed. A
running run at upgrade is untouched (D3). A future hybrid, verify-then-skeletonize a legacy run only when the chat's current
session still contains its turn, is possible and deferred.

## Measurements

Method and limits (honest): the real `~/.smind/smind.db` was **not** touched. A synthetic database with the same `run_events`
schema and indexes was filled by sampling payload sizes (with replacement) from the real transcripts on this machine
(5,674 Claude Code tool results, 1,298 Codex outputs, 6,961 tool inputs, 2,408 assistant text blocks; sizes only), with the call
count per run log-normal (median ~55). The streamed-chunk size is an **assumption**, shown as a sensitivity. The generator is
a scratch script, `TestGenerateSyntheticDB` (#823) stays the in-repo generator and PR0/PR1 extend it with this distribution.

| Chunk size of the provider's stream | current (row per chunk) | skeleton + excerpts (within 30 days) | skeleton only (after the 30-day excerpt prune) |
|---|---|---|---|
| 20 B (token-ish, worst) | 87.9 MB | 19.8 MB (22%) | 10.9 MB (12%) |
| 40 B | 55.7 MB | 19.8 MB (35%) | 10.9 MB (20%) |
| 100 B | 35.9 MB | 19.8 MB (55%) | 10.9 MB (30%) |
| block-level (claude-native) | 24.6 MB | 19.8 MB (80%) | 10.9 MB (44%) |

Per 60 simulated runs. **Reading.** (1) On this distribution payload excerpting alone saves 2–6% (coalescing off, excerpting on:
40.4 MB vs 41.4 MB per 40 runs); the saving is coalescing and dropping thinking text, plus the retention prune of the excerpt tier.
(2) Projected on 848 MB, **skeleton form: roughly 20–55% of today (170–470 MB) and 12–30% (100–250 MB) after the excerpt prune**;
the range is the unknown chunk size and the real payload tail, and PR0 (`smind db stats`: bytes by event type, by provider, payload
size histogram, rows per run) replaces the assumption with the Human's numbers before the thresholds are fixed. (3) The thresholds
(512 B / 8 KiB / 4+4 KiB) keep 93–98.5% of results whole in the sample; if PR0 shows
a fat tail of 100 KB+ results (not seen in this machine's CLI data, whose tools self-cap at ~40–58 KB) the 8 KiB figure still holds
because the excerpt is a fixed cost per payload.

**Read latency of a rebuild** (the dominant cost is parsing the session, then the slice): a full parse of the largest local files in
Python, an upper bound for a Go streaming reader: Claude Code transcript 55.7 MB in ~150 ms; Codex rollout 5.6 MB in ~20 ms; median
files 126 KB / 780 KB are single-digit milliseconds. Codex `thread/read` and ACP `session/load` add a process spawn (hundreds of
ms to seconds, not measured here: no live agent was started). The bounds in D4 (10 s, 64 MB) cover them; PR2 records Go numbers
in the PR body.

## Implemented: the finalizer, skeleton rows and `native_ref` (PR2)

`internal/runs/skeleton.go`, `internal/store/run_skeleton.go`, `internal/runs/nativeref.go`.

- **When.** `finish` captures `native_ref` and then converts the run, before it signals closed (so shutdown never closes the
  store under a conversion). A boot pass (`FinalizePending`) converts runs `finish` never reached (a crash, a run running at an
  upgrade). A migration marks every run finished before this change `skeleton_rev = -1`: those stay in full form for good (H4); a
  running run and every later run is eligible (`0`); converted is `> 0`. The store re-checks all of it inside the transaction
  (never `running`, never pruned, never converted, never legacy).
- **What.** Thinking text is dropped (H1; the row says how big, plus a hash over 512 B). Adjacent answer-text rows become one
  row; an answer over 32 KiB or a user message over 16 KiB keeps head and tail. A tool input or result over 512 B moves to
  `run_excerpts` (whole to 8 KiB, else 4 KiB head + 4 KiB tail), the row keeps a summary (args: `ElideToolCall` output, at most
  512 B; result at most 200 B) and a descriptor `{n, sha256, ex, heredoc bodies {index, n, sha256}}`; the tool title is capped at
  512 B. Every other row (permissions, usage, done, notes, small tool calls) is untouched. Rows carry `"v":2` and `"skel"`.
  Not captured today and therefore not in the skeleton: a tool call's duration and exit code (the event stream has no such field).
- **Reads rehydrate.** Every store read of `run_events` (history, tail, tool-call input, fleet paging, learning windows)
  merges the excerpt back into the row, so an unchanged reader sees the events it always saw: payloads up to 8 KiB identical,
  larger ones as head + marker + tail, thinking as `[thinking not kept: N bytes]`. A row whose excerpt is gone (pruned) keeps its
  summary and is flagged `Truncated`. The heredoc reveal of a finished run therefore still works within the excerpt tier; once the
  input is only head and tail, or the excerpt is pruned, it is the typed `ErrHeredocUnavailable`, never "no such heredoc".
- **Cache (#823).** The resident history of a just-finished run is untouched (full, chunk level); a reload from the store shows
  the skeleton form. The label `historySource` that distinguishes them comes with the read-contract PR.
- **Prune (#829).** Unchanged for now: the prune deletes a run's rows and, with them, its excerpts. Keeping the skeleton rows
  and deleting only the excerpts is PR C.
- **`native_ref`** (JSON on the run row, written by the daemon at run end from what the runner launched the CLI with):
  provider, harness, session id, the effective config root (`CLAUDE_CONFIG_DIR` or the private instance dir; `CODEX_HOME`),
  the transcript path when it exists, cwd, the CLI version read from the first lines of the session file, the turn window and
  the prompt hash. Verified on a real machine: Claude Code keeps `<config dir>/projects/<cwd, each non-alphanumeric as
  "-">/<sessionId>.jsonl`, whose records carry `uuid`, `parentUuid`, `promptId`, `version`, `timestamp`; Codex keeps
  `sessions/YYYY/MM/DD/rollout-<ts>-<threadId>.jsonl` whose first record `session_meta` carries `id`, `cli_version`, `cwd`.
  ACP agents get the session id only. Open for the rebuilder PR: use the transcript's own `uuid` bounds instead of prompt
  hash plus time window.

## Measurements on the real database (PR0, 2026-11-05)

`smind debug run-stats` (hidden, offline, read-only) ran once on a `VACUUM INTO` snapshot of the Human's database (842 MiB file,
1,127 runs, 3,244,656 events, 293.7 MiB of `event_data`). Counts and sizes only; the snapshot was deleted afterwards.

**Where the bytes are.** The file is `run_events` table 567.7 MiB + indexes 266.1 MiB; the other tables are 8 MiB. Per-row overhead
measured from `dbstat`: **174 B** beyond `event_data`. So rows cost more than their content:

| Event type | rows | `event_data` | row size p50 / p99 |
|---|---|---|---|
| thinking | 2,994,288 | 68.7 MiB | 24 B / 31 B |
| text | 184,232 | 5.7 MiB | 24 B / 114 B |
| tool_call | 61,924 | 216.5 MiB | 403 B / 96 KB |
| permission_request | 771 | 1.7 MiB | 1.3 KB / 17 KB |
| raw | 345 | 0.8 MiB | 225 B / 6.9 KB |
| usage, done, permission_resolved, mcp_servers, session_note | 3,096 | 0.4 MiB | tiny |

98% of the rows are streamed chunks, almost all from the `glm` provider, whose chunks are **6 B at the median** (p99 13 B).
Per provider: `glm` 370 runs, 3.17 M rows, 102.6 MiB of data; `claude-native` 733 runs, 61 k rows, 190.7 MiB; `codex-native` 22 runs,
16.5 k rows, 0.4 MiB. Rows per run: p50 87, p90 10,218, p99 37,398, max 72,287. By run age: 277 MiB of the 294 MiB started in the last
7 days, 11.4 MiB 7-30 days, 5.3 MiB older.

**Payloads** (tool inputs and results, 51,012 of them, 209.5 MiB): p50 334 B, p90 5.5 KB, p99 113 KB, max 514 KB. `claude-native`
results: p50 804 B, p90 15.3 KB, p99 195 KB, max 514 KB (169 MiB, 81% of all payload bytes); `glm` results: p50 1.2 KB, p90 9.0 KB, p99
17.8 KB, max 79 KB. This **contradicts the earlier sample's "tools cap their own output at 40-58 KB"**: the real tail is several times
longer. The design holds because the excerpt is a fixed cost per payload (see the grid), but the old per-bullet shares are not facts.

**Threshold grid** (payload bytes only; whole cap W means "whole up to W, else head W/2 + tail W/2"):

| inline | W | payloads kept whole | payload bytes within 30 days | after the excerpt prune |
|---|---|---|---|---|
| 512 | 4 KiB | 87.4% | 52.3 MiB (-75%) | 7.7 MiB |
| **512** | **8 KiB** | **93.0%** | **70.9 MiB (-66%)** | **7.7 MiB (-96%)** |
| 512 | 16 KiB | 96.2% | 90.7 MiB (-57%) | 7.7 MiB |
| 512 | 32 KiB | 97.7% | 112.1 MiB (-47%) | 7.7 MiB |
| 256 / 1024 | 8 KiB | 93.0% | 71.7 / 70.4 MiB | 5.1 / 10.9 MiB |

**Why unchanged.** (1) Going from 8 to 16 KiB keeps 3.2 points more payloads whole for 19.8 MiB more; going to 4 KiB saves 18.6 MiB but
cuts the tail to 2 KiB, too little for an error summary. (2) The inline cap moves the long-run size by +-3 MiB only. (3) The answer
text runs are small: contiguous text runs p50 113 B, p90 2.1 KB, p99 5.1 KB, max 54.5 KB, so the 32 KiB answer cap clips only the
extreme tail. (4) The excerpt tier is exactly what the 30-day prune removes, so its 63 MiB is a bounded, recurring cost, not growth.

**Estimated effect** (model and assumptions printed by the tool; rows include the 174 B overhead):

| Step | whole database |
|---|---|
| today | 832 MiB |
| + write-time coalescing of chunk rows (PR1: boundary, 4 KiB, 250 ms idle) | 255 MiB (-69%) |
| + skeleton form, excerpt tier still present (512 B / 8 KiB / 4+4 KiB, thinking dropped) | 100 MiB (-88%) |
| + excerpt tier pruned after 30 days | 34 MiB (-96%) |

Coalescing alone is the dominant lever: 3,178,520 chunk rows become 21,076 (boundary + 4 KiB) or 27,071 (with the 250 ms idle flush),
saving about 577 MiB, of which most is row overhead, not text. Dropping thinking text (H1) then saves its remaining ~69 MiB of text,
and excerpting payloads saves about 139 MiB of payload bytes (209.5 to 70.9 MiB). Growth: the last 7 days wrote 277 MiB of event data,
about 40 MiB a day; after PR1 the same activity writes roughly a quarter of that.

Known limit: this run attributed tool results to "unknown" because a result update omits the tool name (fixed in #833); the
provider-level payload numbers above are unaffected.

## Consequences

- Disk: the file grows by a fraction of today's rate and #827's prune now leaves a useful audit trail instead of a 3-event stub.
- Memory: histories of finished runs are skeleton-sized by default; the #823 budget holds many more runs warm.
- A run's full history is no longer owned by smind after the finalizer: it depends on the provider keeping its session. That is the
  trade the Human chose; it is labelled in every reader, and detail within 30 days stays in smind regardless.
- New moving parts: one table, three columns (`runs.native_ref`, `runs.skeleton_at`, a `skeletonRev`), a `v:2` event shape, one
  finalizer, one rebuilder package, three provider readers with golden fixtures and a drift breaker.
- Breaking: none for clients (additive fields; `detail` defaults to today's behaviour of "give me what you have"); `history_pruned_at`
  changes meaning only after both ADRs land. A release note says that the full history of runs older than 30 days now depends on
  the provider session.

## Rejected alternatives

- **Keep full rows, only prune (#827 alone).** Simple, but keeps paying full cost for 30 days and leaves a stub afterwards.
- **Compress rows (zstd) instead.** Real and orthogonal; it shrinks bytes but not the row count or the retention question and
  makes every read pay a decode. Not rejected forever; measure after PR1.
- **Shape at write time / convert all old runs at upgrade / nightly batch.** Reasons above (D3, D9).
- **Store the rebuilt history back.** Brings the size and the staleness problem back.
- **A single "full history" feature flag.** The provider matrix is uneven; a per-provider reason is the honest interface.

## Human decisions

**Decided 2026-11-05 by the Human: every recommendation below is accepted.** Summary of what is binding:

- H1 thinking text is not kept (descriptor row only).
- H2 the skeleton is kept forever; `runs.skeletonRetentionDays` is reserved and **not built**.
- H3 write-time chunk coalescing is the first feature PR (after a docs PR and the read-only measurement PR0).
- H4 existing runs are left to the 30-day prune (#829); an opt-in `smind runs skeletonize` comes later.
- H5 rebuild is automatic for claude-native and codex-native; ACP rebuilds only on an explicit "Load full history".
- H6 thresholds are frozen only after PR0 reports the real distribution; **frozen 2026-11-05: 512 B / 8 KiB / 4+4 KiB, unchanged** (see
  Measurements on the real database).
- H7 orchestrators and agents read the **skeleton only** through `task_logs` and never trigger a rebuild; only a human request
  rebuilds. (This replaces the recommendation below, which proposed letting an orchestrator request `detail=full`.)
- H8 `smind doctor` warns about provider-side retention; smind never edits the CLI settings.

Original text, each with the Lead's recommendation first:

- **H1 Thinking text is not part of the skeleton** (descriptor only). Recommend **yes**: it is the largest payload class in the
  simulation, it is not in the agreed skeleton list, and the provider session usually still has it.
- **H2 How long the skeleton lives.** Recommend **forever** (it is ~10–30% of today's size and is the audit trail), with
  `runs.skeletonRetentionDays` reserved and not built; revisit with real growth after PR1.
- **H3 Write-time coalescing of persisted chunk rows as the first PR**, before any of the rest. Recommend **yes**: independent,
  small, and most of the measured saving for ACP/Codex.
- **H4 Existing runs** are left to the 30-day prune, #829 (option a). Recommend **(a)** plus the opt-in `smind runs skeletonize` later.
- **H5 Rebuild policy.** Recommend **auto for claude-native and codex-native, explicit "Load full history" for ACP agents**
  (spawn, auth and side effects).
- **H6 Thresholds 512 B / 8 KiB / 4+4 KiB**, frozen only after PR0 prints the real distribution. Recommend **accept as the
  starting values**.
- **H7 May an orchestrator/agent request `detail=full` through `task_logs`** (it spends a rebuild on the owning daemon)?
  Recommend **yes, for the run-scoped subtree it can already read**, under the same 2-concurrent bound.
- **H8 Provider-side retention.** smind cannot make Claude Code or Codex keep sessions. Recommend a **`smind doctor` warning**
  (the provider's retention setting and how many `native_ref`s are already gone), and that smind **never edits** the user's CLI settings.
