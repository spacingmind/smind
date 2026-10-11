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
6. **AC6 — Embedded skills stay LF on Windows checkouts.** _(Revised
   2026-10-11: smind never parses `SKILL.md` itself. The failure was the
   go:embed bundle being checked out CRLF under `core.autocrlf=true`, so a
   `smind.exe` built there would ship CRLF skills.)_
   `internal/skills/bundle/**` is `eol=lf` in `.gitattributes`, and
   `TestSkillBundle_FrontmatterValid` passes on Windows.
7. **AC7 — Secrets are owner-only on Windows.** Every file the daemon
   writes that holds a secret gets the protection chosen under **D3**:
   the auth token, the relay keypair (including tightening an existing
   loose file), the relay bridge config, the relay server's cert key and
   workspaces file, and the claude `--mcp-config` temp file. Tests assert
   that protection on Windows instead of asserting POSIX mode bits.
10. **AC10 — Sandbox checks are not escaped by Windows junctions.**
    _(Added 2026-10-11, a security finding of this plan.)_ Every "path stays
    inside the root" check follows directory junctions as well as symlinks:
    acp `resolveScopedPath`, `internal/files`, `internal/skills`, the
    cmd/smind MCP prompt file, and taskrunner permission details. The
    escape tests run on Windows against a real junction, which needs no
    privilege to create, instead of failing at symlink setup.
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
- 2026-10-11: D1–D3 decided (ADR-0028).
- 2026-10-12: branch `fix/windows-test-agent-exe` (not merged yet):
  - **Group A:** fake agents built with `.exe`. Failures in the 7 agent
    packages drop from ~170 to 26.
  - **wsclient home isolation** (all OSes): its tests wrote worktrees into
    the developer's real `~/.smind` / `~/.spacingmind`. Five orphaned test
    worktrees were found in the real `~/.spacingmind` on this machine and
    removed.
  - **AC5:** `taskStamp` gives strictly increasing task-name suffixes.
  - **AC7:** new `internal/securefile` (protected owner+SYSTEM DACL on
    Windows, 0600 elsewhere), adopted at all six secret-file writes.
  - **AC10:** new `internal/realpath` (`GetFinalPathNameByHandle` on
    Windows) replaces `filepath.EvalSymlinks` in the five sandbox checks,
    and the new `internal/testutil.Link` falls back to junctions. All four
    junction escape tests failed before the fix and pass after it.
  - Full suite after group A, D and the isolation fix: 29 failing tests in
    13 packages, before securefile/realpath landed.
- 2026-10-12: branch `ci/windows-full-suite-nightly` also gets the
  `.gitattributes` rule for `internal/skills/bundle/**` (AC6).
- 2026-10-12: four slices implemented by GLM agents (glm-5.3) in separate
  worktrees, each reviewed and re-run by the coordinator, then merged into
  `fix/windows-test-agent-exe` together with `ci/windows-full-suite-nightly`:
  A `fix/windows-test-side-groups`, B `feat/api-shutdown-smind-stop`,
  C `feat/windows-setup-scripts`, D `feat/windows-agent-spawn`.
  Coordinator follow-ups: per-repo lock around `git worktree` mutations
  (concurrent-create flake, 3/25 under load); one `internal/winjob`
  replacing four Job Object copies (and checking `NtResumeProcess`'s
  status, which D ignored); one `internal/shimguard`; two taskrunner test
  assumptions the merge unmasked.
- 2026-10-12: **full `go test ./...` on native Windows: 34 packages ok, 0
  failing tests, 214 s** (baseline: 15 packages / 192 tests failing,
  >5 min).
- Open: claude-native agent trees (AC2) and AC8's PR-blocking job, AC9
  release binaries, ADR-0024 wording, README.

## Validation

All on Windows 11 25H2 build 26200, amd64, Go 1.27.0, branch
`fix/windows-test-agent-exe` at the merge of A-D + CI.

- **AC1** — measured first (slice D): through a `.cmd` shim Go 1.27 does NOT
  escape or reject cmd.exe metacharacters (`a&b` runs `b`; `%PATH%`
  expands; `^` eaten; CR/LF split) while `.exe` targets round-trip every
  argument. Fix = refusal: `internal/shimguard` (tests: refuses
  `% & | < > ^ CR LF` on shims, allows quotes/`!`/spaces/empty/unicode, never
  refuses an .exe or an unresolvable command) used by acp and codex
  (`TestSpawnShim_*` in internal/acp and internal/codex),
  `TestProviderTest_ReportsShim_Windows` and the `npx.cmd` fixtures.
  **Not covered:** claude-native (see AC2).
- **AC2** — `internal/winjob` (suspended start, assign, `NtResumeProcess`
  with status check, once-guarded close): its own tests (resumed child runs,
  Close kills child + grandchild, idempotent/nil-safe) plus
  `TestAgentStop_KillsDescendants_Windows` and
  `TestAgentJob_DaemonCrashKillsTree_Windows` for acp and codex (`-count=5`
  stable). **Open:** `claude-agent-sdk-go@v0.3.2` spawns `claude` itself with
  no hook, so claude-native trees can still orphan; needs a decision
  (launcher wrapper vs upstream SDK change).
- **AC3** — `POST /api/shutdown` (token-guarded like `/v1/*`, 202, one
  `sync.Once` channel shared with the signal path) and `smind stop`:
  `TestShutdown_*` (401 / GET / POST once / twice), `TestServe_GracefulShutdown`
  (real daemon subprocess with a terminal; `smind stop` exits 0; session
  persisted `closed`), `TestStop_NotRunning`, `TestStop_WrongToken`,
  `TestServe_GracefulShutdown_CtrlBreak_Windows` (ran, not skipped).
- **AC4** — `setup_windows.go` (sh from PATH or Git for Windows, suspended
  start into a Job): `TestSetupScript_RunsWithEnv_Windows`,
  `_TimeoutKillsTree_Windows`, `_CancelKillsTree_Windows`,
  `_NoSh_FailsFast`, plus the previously Windows-skipped `TestSetup_*` and
  teardown tests now running.
- **AC5** — `taskStamp` (`TestTaskStamp_StrictlyIncreasingUnderConcurrency`)
  and `TestCreateTask_ConcurrentSameTitle_UniqueNames`, which exposed a git
  race fixed with a per-repo lock (40/40 under load, was 22/25).
- **AC6** — `.gitattributes` (`*.sh` and `internal/skills/bundle/**` eol=lf);
  `TestSkillBundle_FrontmatterValid` passes after re-checkout.
- **AC7** — `internal/securefile`; `icacls` shows only the user and SYSTEM, no
  inherited ACEs; auth/e2ee/taskrunner permission tests assert it (also
  green on Linux via WSL).
- **AC8** — suite green locally (above). **Not yet:** the PR-blocking CI
  job and a green `windows-full-suite.yml` run on GitHub.
- **AC9** — not started.
- **AC10** — `internal/realpath` + `internal/testutil.Link`; four junction
  escape tests failed before, pass now.
