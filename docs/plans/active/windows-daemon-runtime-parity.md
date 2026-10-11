# Windows daemon runtime parity

Make the smind daemon itself run natively on Windows (`smind.exe serve`), not
just build there. `internal/terminal` is already native (ADR-0022, #245); the
rest of the daemon still assumes Unix. Today Windows users run the daemon in
WSL2 via the desktop app (ADR-0013). This plan covers the daemon and its
release binaries only.

**Out of scope:** the desktop app managing `smind.exe` natively (a
`Platform::WindowsNative` mode). It changes ADR-0013's host matrix and is
blocked on the user's decision. Also out of scope: an `install.ps1`, and
Windows 10 / bundled-OpenConsole questions
(`docs/research/local/windows-arm64-and-win10-refs.md`).

**Baseline** (2026-10-11, `develop` 50b2c78, Windows 11 26200, Go 1.27):
`go test -timeout 5m ./...` gives 15 failing packages and 192 failing tests.
The CI step that was supposed to report this timed out at 15 min on every run
and never produced a list. `ci/windows-full-suite-nightly` (item 0) moves it
to `windows-full-suite.yml`, which runs nightly and on demand, uses a
per-package timeout and writes a failure summary.

## Acceptance Criteria

1. **AC1 — Agents start through npm shims.**
   - On Windows, a provider command whose `command[0]` resolves through
     PATH + PATHEXT to a `.cmd`/`.bat` shim (`npx`, `claude`, `codex`, `kimi`
     as installed by npm) starts the agent.
   - The agent receives its argv byte-exact, including arguments with spaces,
     `"`, `&`, `|`, `^`, `%`, `!`, `<`, `>`.
   - If an argument can't be passed safely through `cmd.exe`, start fails with
     an error naming the argument. It must never run something else.
   - This applies to `internal/acp`, `internal/codex`, claude-native
     (`claude-agent-sdk-go`) and stdio MCP servers.
   - `provider.test` reports a shim the same way the real spawn would start it.
2. **AC2 — Agent process trees die with the run.**
   - On Windows every spawned agent process (acp, codex, claude-native, stdio
     MCP server) is created inside a kill-on-close Job Object
     (`PROC_THREAD_ATTRIBUTE_JOB_LIST`, or assigned before the process
     resumes).
   - `run.stop`, `Runs.CloseAll` and daemon exit, including a crash or
     `TerminateProcess`, leave no descendant alive within 5 s. Example:
     `npx → node → agent`.
3. **AC3 — Graceful shutdown on Windows.** The following make `smind serve`
   run the same shutdown path as SIGTERM on Unix: HTTP shutdown,
   `Runs.CloseAll`, terminals `CloseAll`, store close, and final persistence.
   Terminal sessions end `closed`, not `interrupted`.
   - The external mechanism chosen under **D2**.
   - CTRL_C_EVENT and CTRL_BREAK_EVENT.
   - CTRL_CLOSE_EVENT and CTRL_SHUTDOWN_EVENT (Go delivers these as SIGTERM).
4. **AC4 — Worktree setup/teardown scripts on Windows.**
   - A workspace with `setup`/`teardown` configured creates and archives
     tasks on Windows, running the scripts with the shell chosen under **D1**.
   - Cwd, environment (`SMIND_*`, `SMIND_PORT`), timeout and tree-kill
     semantics match ADR-0024's Unix behavior.
   - `ErrSetupUnsupported` is gone from Windows builds, or remains only for
     the D1 option "keep unsupported".
5. **AC5 — Unique task worktree names under concurrency.** Task worktree and
   branch names stay unique when many tasks with the same title are created
   concurrently on any OS. `UnixNano` in base36 currently collides on Windows
   (`internal/workspace/task.go:110`).
6. **AC6 — `SKILL.md` with CRLF line endings parses.** It parses the same as
   the LF version, as produced by a Windows editor or a `core.autocrlf=true`
   checkout.
7. **AC7 — Secrets are owner-only on Windows.** The auth token
   (`internal/auth`) and the relay keypair (`internal/relay/e2ee`) get the
   protection chosen under **D3**. Tests assert that protection on Windows
   instead of asserting POSIX mode bits.
8. **AC8 — Windows full suite green.**
   - `go test ./...` passes on `windows-latest` in `windows-full-suite.yml`
     with no failing package.
   - Every test that cannot apply on Windows is skipped with a reason. A
     test whose behavior does matter on Windows gets a Windows-equivalent
     assertion instead.
   - Once green, the suite runs on every PR as a blocking check, kept under
     12 min wall time.
9. **AC9 — Release binaries.**
   - Every release attaches `smind_<version>_windows_amd64.zip` and
     `smind_<version>_windows_arm64.zip`, each containing `smind.exe`, and
     lists both in `checksums.txt`.
   - `smind.exe version` prints the release version.
   - The `windows/arm64` binary passes `go test ./internal/terminal/...` on a
     `windows-11-arm` runner.

## Test Scenarios

Root-cause groups from the baseline run. Each group is fixed in product code
(**P**) or as a test-only adaptation (**T**).

- **A (T): test-built fake agents lack `.exe`.** The `go build -o …/fakeagent`
  calls in `internal/acp`, `internal/codex` and `internal/taskrunner` fail to
  start on Windows. This cascades into `runs`, `wsapi`, `wsclient` and
  `cmd/smind`, which accounts for most of the 192 failures. Fix: append
  `.exe` on Windows in each test helper.
- **B (P/T): POSIX mode assertions.** `auth`, `relay/e2ee` and `skills`
  assert 0600/0644. Fix: AC7 covers the secrets; plain files skip the mode
  assertion on Windows with a reason.
- **C (T): symlink tests need `SeCreateSymbolicLinkPrivilege`.** Affects
  `files`, `skills` and `config`. Fix: skip only when symlink creation fails
  with that privilege error, so the tests still run on Developer Mode or CI
  machines that allow symlinks.
- **D (P): task ID collision** (AC5). Affects `workspace`, `wsclient` and
  `runs`.
- **E (P): setup scripts unsupported** (AC4). Affects `workspace`.
- **F (T/P): `hostfs` home-dir default.** The test sets `HOME`, but Windows
  uses `USERPROFILE`. Check which side is wrong before fixing.
- **G (P/T): `config` home migration.** The concurrent-migration and symlink
  warning tests fail on Windows. Investigate whether file locking or rename
  semantics differ.
- **H (?): `store` pre-MCP schema migration test.** Fails with `no such
  table: main.workspace_mcp_servers`. Investigate.
- **I (P): `SKILL.md does not start with ---`** (AC6).

Named scenarios:

- `TestSpawnShim_ArgvRoundTrip_Windows`: a temp-PATH `.cmd` shim forwards to
  a helper exe that prints its argv as JSON. Spawn it through `acp.New` and
  `codex.New` with the tricky-arg table from AC1 and assert exact argv. (AC1)
- `TestSpawnShim_UnsafeArgRejected_Windows`: an argument that cannot be
  escaped for `cmd.exe` gives a start error and the helper never runs. (AC1)
- `TestProviderTest_ReportsShim_Windows`: `provider.test` on a shim-only
  install reports it as available. (AC1)
- `TestAgentStop_KillsDescendants_Windows`: the helper agent spawns a
  grandchild (`ping -t`); after `Close` and after `Runs.Stop` both pids are
  gone within 5 s. (AC2)
- `TestAgentJob_DaemonCrashKillsTree_Windows`: a subprocess daemon spawns an
  agent with a grandchild. `TerminateProcess` on the daemon kills both
  within 5 s. (AC2)
- `TestServe_GracefulShutdown_Windows`: start `smind serve` as a subprocess
  with a running terminal and a run, stop it via the D2 mechanism, and via
  CTRL_BREAK in a shared console. The terminal session persists as `closed`
  and the run is no longer `running`. (AC3)
- `TestSetupScript_RunsWithEnv_Windows` and
  `TestSetupScript_TimeoutKillsTree_Windows`: mirror the existing Unix
  `setup_run_test.go` cases with the D1 shell. (AC4)
- `TestCreateTask_ConcurrentSameTitle_UniqueNames`: 200 concurrent creates
  with one title give 200 distinct worktree paths and branches, on all OSes.
  (AC5)
- `TestParseSkill_CRLF`: the CRLF and LF fixtures parse to the same skill.
  (AC6)
- `TestTokenFile_OwnerOnlyACL_Windows` and
  `TestKeypairFile_OwnerOnlyACL_Windows`: the DACL grants only the current
  user (plus SYSTEM, if D3 allows it). (AC7)
- `windows-full-suite.yml` passes on `develop`, and then the PR-blocking job
  passes under 12 min. (AC8)
- A release dry run (`release-please.yml` with `workflow_dispatch` or a
  test tag) lists the two zips and their checksums. `smind.exe version` is
  checked inside the workflow. The arm64 terminal tests run on
  `windows-11-arm`. (AC9)

## Decisions

**D1–D3 decided 2026-10-11 by the user, all as recommended, and recorded in
ADR-0028** (`docs/decisions/0028-native-windows-daemon-runtime.md`): D1 (a)
Git for Windows `sh`; D2 (a) `POST /api/shutdown` + `smind stop`; D3 (a)
owner-only DACL. The option lists below are kept for the record.

- **D1: shell for setup/teardown scripts on Windows.**
  - (a) `sh -c` via Git for Windows' `sh.exe` when present, otherwise fail
    fast. Same script on every OS.
  - (b) `cmd /c`. Scripts would differ per OS, e.g. new `setup.windows`
    keys.
  - (c) `pwsh`/`powershell -Command`. Same per-OS split as (b).
  - (d) Keep unsupported, as ADR-0024 v1 has it.
  - Recommended: (a). ADR-0024's scripts are POSIX and almost every Windows
    dev machine running agents already has Git for Windows. This needs an
    ADR-0024 amendment.
- **D2: how a native daemon is told to stop gracefully.**
  - (a) Authenticated `POST /api/shutdown` using the existing token, plus a
    `smind stop` CLI command.
  - (b) A named Windows event `Local\smind-shutdown-<port>` that the daemon
    waits on.
  - (c) CTRL_BREAK only. That needs the caller to share a console, which a
    GUI app (the future desktop manager) doesn't have.
  - Recommended: (a). It works from the desktop app, the CLI and WSL alike,
    and reuses the auth that already protects the API. It adds a public API
    endpoint.
- **D3: protecting secrets on Windows.**
  - (a) Set an explicit owner-only DACL (current user, plus SYSTEM) when
    writing the token and keypair.
  - (b) Rely on the default `%USERPROFILE%` ACL inheritance and only assert
    it.
  - Recommended: (a). Inherited ACLs can be broadened by a parent directory,
    for example a redirected or shared profile.

Settled for this plan:

- Job Objects for agents reuse the approach from ADR-0022 /
  `internal/terminal/pty_windows.go`: `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`,
  created per process and closed on reap or kill.
- `.cmd` spawning follows the escaping rules for `cmd.exe` arguments
  (BatBadBut class). Refusing to start is preferred over best-effort
  quoting. Reference: `refs/paseo` `packages/server/src/terminal/terminal.ts`
  (shim resolution with `cmd /d /s /c` and caret escaping).

## Progress

- 2026-10-11: baseline run and root-cause grouping (above). Item 0
  (`ci/windows-full-suite-nightly`, not merged yet) moves the full suite out
  of PR CI.
- 2026-10-11: spec written. D1–D3 are open.

## Validation

_(fill per AC as each one lands)_
