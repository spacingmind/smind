# 0024: Worktree setup/teardown scripts and per-task ports

## Status

Accepted (2026-10-11). The user approved D1 of
[`docs/plans/active/orchestration-parity-paseo.md`](../plans/active/orchestration-parity-paseo.md):
setup/teardown plus per-task port, stored **daemon-side and
human-authored**, never read from a repo-committed file.

## Context

A fresh task worktree (`internal/workspace.CreateTask`,
`git worktree add`) has no installed dependencies and none of the ignored
files (`.env`, local config) the repo needs, so every task starts broken
for most real repos. Paseo solves this with `worktree.setup`/`teardown` in
a repo-committed `paseo.json`, read from the base branch, plus
`$PASEO_PORT` allocation (`refs/paseo/public-docs/worktrees.md`).

Reading commands from the repo means a cloned, untrusted repository
registered as a workspace gets code execution on the first `task_new`. The
user rejected that model. smind already has a precedent for standing,
human-authored configuration that agents cannot write: agent profiles
(ADR-0014) and the auto-approving modes they may carry (ADR-0019 #6). This
ADR applies it to setup scripts.

`Space.EnvData` exists but nothing reads it; this ADR does not repurpose
it.

## Decision

### Storage and editing

Two nullable text columns on `workspaces`: `setup_script` and
`teardown_script` (NULL = none). They are edited **only** by a human, via
`smind workspace setup show|set|clear <workspaceId>` and the web UI's
workspace settings, both over a new wsapi method `workspace.setScripts`.
There is **no MCP tool** that reads or writes the script text, `task_new`
accepts no script argument, and no file in the repo is ever consulted.
`task_get`/`workspace_list` expose only whether a script is configured, not
its text.

This is not a boundary against a process that already has the user's smind
token or shell (the CLI is the same wsapi); it is the same trust line as
ADR-0017: MCP callers get no way to author commands, and repository
contents never do.

Daemon-wide knobs live in `config.yaml` (`worktrees:` block):
`ports.range` (e.g. `"31000-31999"`, default unset = no ports),
`setupTimeoutSeconds` (default 600), `teardownTimeoutSeconds` (default 120).

### Execution model

- Shell: `sh -c <script>` (POSIX). On Windows setup/teardown are not run in
  v1: a configured script makes task creation fail fast with a clear
  message rather than silently skipping (ADR-0022 covers terminals, not
  this). **Amended by ADR-0028:** a native Windows daemon runs them with Git
  for Windows' `sh`, and fails fast only when no `sh` is found.
- cwd = the task worktree. The script runs in its own process group so a
  timeout, archive, or daemon shutdown kills the whole tree.
- Environment: the daemon's environment plus `SMIND_SOURCE_CHECKOUT_PATH`
  (the workspace repo), `SMIND_WORKTREE_PATH`, `SMIND_BRANCH`,
  `SMIND_TASK_ID`, `SMIND_WORKSPACE_ID`, and `SMIND_PORT` when a port is
  allocated. Values are passed as environment variables, never interpolated
  into the script text.
- Output (stdout+stderr merged) is kept as the **tail, capped at 64 KiB**,
  in `tasks.setup_log`; it is not part of any run's event timeline (a
  setup is not a run). Secrets in the script's output are the author's
  responsibility; smind never logs the environment.
- Timeout kills the process group and records `failed` with a timeout
  error.

### Lifecycle and failure semantics

New columns on `tasks`: `setup_status` (NULL = no script; `running`, `ok`,
`failed`), `setup_error`, `setup_log`, `port`.

1. `CreateTask` creates the worktree and rows exactly as today, allocates a
   port if a range is configured, and, if the workspace has a
   `setup_script`, sets `setup_status = running` and starts the script **in
   the background**. `task.create` returns immediately with the task
   (`setupStatus: "running"`); it never blocks for minutes of `npm ci`.
2. While `setup_status = running` the task counts as **busy** for
   `run.start`/`task.prompt`: `whenBusy` `queue` (the MCP default) and
   `interrupt` enqueue the prompt and it is delivered when setup finishes
   (ADR-0021's queue, with the finish of setup as the delivery trigger);
   `reject` (or unset, today's default) returns an error naming the setup.
3. On success `setup_status = ok`. On failure (non-zero exit, timeout,
   daemon restart mid-setup) `setup_status = failed`, `setup_error` carries
   the reason, and the **worktree is left intact**. A failed setup does not
   block runs (the agent or human may fix the environment); it is shown
   prominently (task badge, `task_get`). `task.retrySetup` re-runs the
   script on a `failed`/`ok` task and is the only way to run it again.
   On daemon start, any task left `running` becomes `failed`
   (`interrupted by daemon restart`); it is not silently re-run.
4. **Teardown** runs on archive/delete: after the checkpoint commit and
   **before** `git worktree remove` (so user work is already safe), best
   effort: a teardown failure or timeout is appended to the task's
   `setup_log`, never blocks the archive. A checkpoint failure aborts before
   teardown, as it aborts before removal today. Archiving a task whose setup
   is still `running` first cancels (kills) the script.
5. Setup runs once per task, at creation; a task from `branch`/`prNumber`/
   `baseRef` (the PR-B modes) gets it too.

### Ports

If `worktrees.ports.range` is set, `CreateTask` allocates the lowest port in
the range that (a) no non-archived task holds (`tasks.port`, enforced by a
partial unique index on `port` where `archived_at IS NULL`, so archiving
frees it) and (b) a probe `net.Listen` on `127.0.0.1` shows free. Allocation
runs under a mutex with a retry on a unique-constraint race. Range
exhaustion fails `CreateTask` with a clear error. The port is exposed to
the setup/teardown scripts as `SMIND_PORT`, recorded on the task
(`task.port`), and set in the environment of the task's terminals and of
Claude-native runs (the spawn paths that already take an env). ACP and Codex
agent spawns have no env seam today; adding one is out of scope here, so
the portable channel for them is the setup script writing `.env.local` from
`$SMIND_PORT`.

### Scripts and services

Named scripts, supervised services, proxy URLs and health (Paseo's
`scripts.*`) are **not** part of this decision (gap G19 stays P2).

## Alternatives considered

- **Repo-committed `smind.json` / `paseo.json`-style file read from the base
  branch.** Rejected by the user: a cloned untrusted repo gets code
  execution on the first `task_new`. A trust-on-first-use hash approval
  could be added later as a separate decision.
- **Run setup synchronously inside `task.create`.** Rejected: dependency
  installs take minutes, would hold an MCP tool call and a daemon
  goroutine, and would time out MCP clients.
- **Treat setup as a run in the run timeline.** Rejected: runs belong to a
  chat and a provider; a setup has neither, and it would drag the run
  registry/usage model into a non-agent process.
- **Fail the task on setup failure (and remove the worktree).** Rejected:
  loses the half-installed state a human or agent needs to diagnose; the
  user asked for the worktree to stay intact.
- **Reuse `Space.EnvData`.** Rejected: space-scoped and undefined; setup is
  per workspace.
- **Re-run setup automatically after a daemon restart.** Rejected: a
  half-run script re-running is not idempotent in general.

## Rationale

smind's trust model is that standing configuration affecting what executes
is written by the human, and that agents and repositories reach only
prompts. Daemon-side scripts keep that line while giving every task a
working checkout. Treating setup as "busy" reuses the ADR-0021 queue so an
orchestrator's `task_new` then `task_send` flow works unchanged, and keeping
failed setups non-blocking avoids turning a flaky `npm ci` into a dead task.
