# Dogfood CLI batch 2

Size: **M (5)** by the protocol table: scope 1 (CLI plus three small daemon messages), PRs 2 (six, non-stacked), risk 1 (a new exit-code contract; one error-reason change), decisions 1 (two choices recorded below, neither changes an ADR), verification 0 (unit and fake-daemon CLI tests, re-checked on the dogfood daemon).

Follows [`dogfood-friction-fixes`](dogfood-friction-fixes.md). Source: the local dogfood log (2026-10-22) items 17-30 and the Human's request to fix the CLI bugs found while orchestrating through smind. A separate Lead owns the web UI findings.

## Goal

A Lead can drive Peers from the shell without polling loops, and can see from the CLI what happened to each run: why it failed, how long it took, what it cost, what it committed.

## Verified starting point (against `origin/develop` e63f2a23)

- Nothing blocks on a run except `task send` (foreground) and `task attach`/`logs -f` (stream); there is no "wait and give me the outcome".
- A GLM quota failure ends the run as `acp: rpc error -32603: Internal error`; the real cause (`[error] 429 Usage limit reached ... reset at 17:35:18`) is an agent text chunk in the history. The run row's `Err` and `task runs` never show it.
- `run.list` rows already carry `StartedAt`, `FinishedAt`, `Model`, `Usage` (`inputTokens`, `outputTokens`, `costUsd`, `model` when the backend reports them; claude-native does), `BaseSHA`/`HeadSHA` (provenance) and `Err`. `task runs` prints only five columns.
- `chat N is bound to provider "glm", got "claude-native"` (ADR-0016) is correct but names no fix.
- With a setup script running, `run.start` rejects with `...setup script is still running; wait for it to finish or pass whenBusy=queue`: an API field name, not a CLI flag.
- There is no task owner or label in the data model (`store.Task` has none), so an owner column needs a schema change; out of scope here (see Not doing).

## Pull requests

All branch from `origin/develop`; none stack. Each PR body carries the local `task test` and `task lint` results.

| PR | Content | Files |
|---|---|---|
| 1 | `smind task wait <runId>` | new `cmd/smind/task_wait.go` (+ a `case "wait"` line in `cmdTask`) |
| 2 | run error cause: promote the agent's `[error]` text to the run's terminal reason | `internal/runs`, `internal/acp/rpc.go` |
| 3 | `task runs` columns: STARTED, DURATION, COST/TOKENS, COMMITS, error note | `cmd/smind/task.go` (`cmdTaskRuns`) |
| 4 | chat/provider binding error names the fix | `internal/runs/registry.go` |
| 5 | `task send` right after `task new`: queue while setup runs, with a notice; clearer daemon error | `cmd/smind/task.go` (`cmdTaskSend`), `internal/runs/queue.go` |
| 6 | `smind status`: which daemon and port the CLI talks to | new `cmd/smind/status.go` |

## Acceptance criteria

- **AC1 (wait)** `smind task wait <runId>` blocks until the run reaches a terminal state, then prints a one-paragraph summary on stdout (status and stop reason, duration, model, tokens and cost when reported, commits on the branch when known, then the agent's final message, trimmed) and exits with the outcome: **0** done with a normal stop (`end_turn`), **1** error, **3** stopped, **4** done but incomplete (a `needs-continue` disposition: turn, token or budget cap), **5** `--timeout` elapsed with the run still going (the run is untouched), **2** usage error as everywhere else. On an already-finished run it prints the summary and exits at once. While waiting, a newly pending permission prints one stderr line with the exact `task approve|deny` commands (it keeps waiting). An unknown run id exits 1 with the daemon's message.
- **AC2 (error cause)** When a run ends in error and the agent's last text before the failure starts with `[error]`, the run's terminal error is that text followed by the original error in parentheses, e.g. `429 Usage limit reached for 5 hour. Your limit will reset at 2026-10-22 17:35:18 (taskrunner: glm prompt: ... rpc error -32603: Internal error)`. An `acp.RPCError` with `data` includes it in its message. Runs without an `[error]` text are unchanged. The cause shows in `task send`'s final line, `task wait`, `task logs` and the web (same field).
- **AC3 (runs columns)** `task runs` prints `ID CHAT PROVIDER MODEL STARTED DURATION USAGE COMMITS STATUS` (a run still going shows its elapsed time; USAGE is `<in>/<out> tok $0.12` with whatever the backend reported, `-` when none; COMMITS is the number of commits between the run's `BaseSHA` and `HeadSHA`, computed with local git in the task's worktree, `-` when unknown or the path is not local). A run in error appends a trailing `error: <first line of the cause, 80 chars>`.
- **AC4 (chat binding)** A start whose provider differs from the chat's fails with `chat N is bound to provider "glm", got "claude-native": start a new chat for this provider (CLI: smind task chat new <taskId>, then --chat <id>)` with the real task id filled in.
- **AC5 (setup busy)** `smind task send` with no `--when-busy`, on a task whose setup script is running, sends with `whenBusy=queue` and prints a stderr notice (`setup script is still running; queued, it starts when setup finishes`). An explicit `--when-busy reject` keeps today's rejection. The daemon's rejection now names the CLI flag too (`--when-busy queue`).
- **AC6 (status)** `smind status` prints the daemon address the CLI resolves, `SMIND_HOME`, the config file path, whether it answers `/healthz`, and its version; exit 1 when unreachable (with the same dial error `task` commands print).

## Test scenarios

| Scenario | Covers |
|---|---|
| `TestTaskWait_ExitCodes` (table: done, error, stopped, done+needs-continue, timeout) against the real daemon + fake ACP agent scenarios | AC1 |
| `TestTaskWait_AlreadyFinished`, `TestTaskWait_UnknownRun` | AC1 |
| `TestTaskWait_PrintsPendingPermissionHint` | AC1 |
| `TestTaskWaitSummary` (pure: status/duration/usage/commits/final message formatting, trimming) | AC1 |
| `TestFinish_PromotesAgentErrorText`, `TestFinish_NoAgentErrorTextUnchanged`, `TestRPCErrorIncludesData` | AC2 |
| `TestRunColumns` (pure: duration, usage, error note), `TestTaskRunsColumns` (fake daemon), `TestRunCommitCount` (throwaway repo) | AC3 |
| `TestStart_ChatProviderMismatchNamesFix` | AC4 |
| `TestTaskSend_QueuesWhileSetupRuns`, `TestTaskSend_ExplicitRejectKept`, `TestSetupBusyErrorNamesCLIFlag` | AC5 |
| `TestStatusReportsDaemon`, `TestStatusUnreachable` | AC6 |

Checks per PR: `task test`, `task lint`; then re-check the behaviour on the rebuilt dogfood daemon.

## Decisions

- **D1 (item 4): a provider mismatch fails with an actionable message; it does not auto-create a chat.** A chat carries the agent's session and context (ADR-0016): silently opening a new one would drop that context without the user choosing it. The message carries the exact commands, so recovery is one paste. Consistent with ADR-0016's explicit chat binding.
- **D2 (item 5): the CLI queues by itself while the setup script runs; the daemon default is unchanged.** ADR-0024 §2 keeps `reject` as the default for an unset `whenBusy` and defaults only MCP to `queue`. The CLI is free to choose its request: it reads the task's setup status first and, only when setup is running and the user did not pass `--when-busy`, asks for `queue` and says so. A busy chat still rejects as before.
- **D3 (wait exit codes):** 0 ok, 1 error, 2 usage (the CLI's existing convention), 3 stopped, 4 incomplete, 5 timeout. Distinct codes let a Lead script branch without parsing text.
- **D4 (commits):** computed by the CLI with local git, not stored. It needs no schema change and costs one `git rev-list --count` per listed run; a remote daemon's worktree is not reachable, so the cell is `-` there.
- **D5 (error cause):** promoted in the daemon when the run finishes, into the existing `Err` field, so every client benefits and no wire field is added.

## Not doing

- An owner or lead column in `task ls` (log item 22): needs a data-model decision (who owns a task: the creator, a label, a parent Lead) and a schema change. Needs the Human.
- Falling back to another provider on a quota failure, and queueing until a quota resets (log item 19): a routing-policy decision, not a CLI fix. AC2 at least makes the cause and reset time visible.
- `task status <id>` (optional in the brief): `task runs` plus `task wait` cover it; revisit if the columns prove insufficient.

## Progress

- 2026-10-22: plan written (#504).
- 2026-10-22: shipped: #505 run error cause, #506 `task wait`, #507 `task runs` columns, #508 setup-busy queueing, #509 `smind status`, #510 chat/provider binding message. #506 and #507 both added the same `runSummaryRow` fields and merged into a duplicate declaration that broke the build (fixed by #513); the lesson is to rebase the second of two PRs that touch one struct before it merges.
- 2026-10-22: re-verified live on the rebuilt dogfood daemon (develop e9583ec5). Closed.

## Validation

"Unit" = a named test in the PR; "live" = run by hand on the dogfood daemon (:4870, develop e9583ec5, 2026-10-22, real claude-native Haiku runs and a real GLM run).

| AC | Result | Evidence |
|---|---|---|
| AC1 `task wait` | confirmed | unit `TestWaitExitCode`, `TestRenderWaitSummary`, `TestTaskWait_ExitCodes` (done 0, needs-continue 4, stopped 3, timeout 5), `TestTaskWait_AlreadyFinished`, `TestTaskWait_UnknownRun`, `TestTaskWait_Usage`, `TestTaskWait_PrintsPendingPermissionHint`, and (after this plan) `TestTaskWait_ErrorRun`; live: exit 0 with `1198 in / 127 out tokens / $0.01; no new commits`, exit 1 on a GLM 429 run, exit 3 after `task stop`, exit 5 with `--timeout 4s` and the run still `running` afterwards, summary with `head ab39bf5 (base e9583ec)` for a run that committed. **Exit 4 (needs-continue) is unit-tested only**; it could not be triggered live |
| AC2 error cause | confirmed | unit `TestAgentErrorCause`, `TestFinish_PromotesAgentErrorText`, `TestFinish_NoAgentErrorTextUnchanged`, `TestFinish_RPCErrorDataReachesTheRunError`, `TestRPCErrorIncludesData`; live: a real GLM 429 run reads `error: 429 Usage limit reached for 5 hour. Your limit will reset at 2026-10-22 17:35:18 (taskrunner: glm prompt: ... rpc error -32603: Internal error ({"details":"429 ..."}))` in `task wait` and `task runs`. GLM puts the cause in the JSON-RPC `data` too, so the text appears twice (cosmetic) |
| AC3 `task runs` columns | confirmed | unit `TestRunStartedCell`, `TestRunDurationCell`, `TestCompactCount`, `TestRunUsageCell`, `TestRunStatusCell`, `TestRunCommitCount`, `TestTaskRunsColumns`; live: STARTED, DURATION, USAGE `1.2k/671 tok $0.01`, COMMITS `2` for a run that made 2 commits (matches `git log` in the worktree), `error: 429 ...` in STATUS |
| AC4 chat binding message | confirmed by test | unit `TestStart_ChatProviderMismatchNamesFix`; the message was seen live earlier in the dogfood (the original friction), the new text was not re-triggered live |
| AC5 setup-busy | confirmed | unit `TestTaskSend_QueuesWhileSetupRuns`, `TestTaskSend_ExplicitRejectKept`, `TestTaskSend_NoSetupNoNote`, `TestSetupBusyErrorNamesCLIFlag`; live: on a workspace with a 12 s setup script, `task send` right after `task new` printed the notice and `queued as item 1`, the run started when setup finished (5 s run at 16:35:04); `--when-busy reject` failed with the message naming `--when-busy queue` |
| AC6 `smind status` | confirmed | unit `TestRenderStatus`, `TestStatusReportsDaemon`, `TestStatusUnreachable`, `TestStatusWrongService`, `TestStatusRejectsArgs`; live: `reachable, smind dev` for :4870, `NOT reachable: ... connection refused` with exit 1 for a closed port. Not run against a desktop-managed daemon (`managedBy`) |

**Known gaps, carried forward (none blocks the plan):**
- `--detach` combined with the queued path prints no run id (a queued prompt has no run yet), so a script cannot `task wait` on it. A possible fix is `task wait` accepting a queue item id.
- A run's error text can say the cause twice when the agent also puts it in the RPC error's `data` (GLM does).
- Exit code 4 has no live evidence.
- The task owner or lead column in `task ls` and provider failover on a quota error remain undone (see "Not doing"); both need a Human decision.
