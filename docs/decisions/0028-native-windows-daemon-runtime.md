# 0028: Native Windows daemon runtime — setup shell, shutdown endpoint, secret ACLs

## Status

Accepted (2026-10-11, chosen by the user as D1–D3 of
[`docs/plans/active/windows-daemon-runtime-parity.md`](../plans/active/windows-daemon-runtime-parity.md)).
Amends ADR-0024's "On Windows setup/teardown are not run in v1". It does not
change ADR-0013's host matrix: the desktop app still manages the daemon via
WSL2 on Windows until that is decided separately.

## Decision

1. **Setup/teardown scripts run under Git for Windows' `sh`.** When the
   daemon runs natively on Windows, ADR-0024 scripts run as `sh -c <script>`
   using the `sh.exe` from Git for Windows. That is the same POSIX script on
   every OS, with the same cwd, `SMIND_*` environment, timeout and tree-kill
   semantics; on Windows the tree kill uses a kill-on-close Job Object.
   - Lookup order: `sh` on PATH, then `<git install dir>\bin\sh.exe`, where
     the git install dir is derived from `git.exe` on PATH.
   - If no `sh` is found, a workspace with scripts configured fails task
     creation fast with a message naming Git for Windows. It never skips the
     scripts silently.
2. **Graceful shutdown is an authenticated API call.**
   - `POST /api/shutdown` is wrapped in `auth.RequireToken`, the same guard
     as the `/v1/*` proxy routes (`internal/server/server.go`). It returns
     `202 Accepted` and then runs the same
     shutdown path as SIGINT/SIGTERM: HTTP shutdown, `Runs.CloseAll`,
     terminals `CloseAll`, store close.
   - `smind stop` calls it, using the token and port from config, and waits
     until the port stops answering, with a bounded timeout.
   - It works on every OS and is the mechanism a future native Windows
     desktop manager would use, because a GUI process can't send a console
     Ctrl+Break.
3. **Secrets are owner-only on Windows.** The auth token and the relay
   keypair are written with an explicit protected DACL that grants only the
   current user and SYSTEM. This is the Windows equivalent of the 0600 used
   on Unix.

## Alternatives considered

- **Setup shell: `cmd /c` or PowerShell.** Either needs a second,
  per-OS copy of every script (`setup.windows` keys) and diverges from the
  POSIX semantics ADR-0024 documents. Keeping scripts unsupported leaves
  PR-E unusable on native Windows.
- **Shutdown: a named Windows event (`Local\smind-shutdown-<port>`).** It is
  Windows-only and adds a second, OS-specific control channel next to the
  API.
- **Shutdown: Ctrl+Break only.** The caller has to share the daemon's
  console, which a GUI desktop app doesn't have.
- **Secrets: rely on the inherited `%USERPROFILE%` ACL.** It is simpler, but
  a broadened parent ACL (a redirected or shared profile, a moved
  `SMIND_HOME`) silently exposes the token.

## Rationale

The goal is that `smind.exe serve` behaves like the Unix daemon rather than
growing a separate Windows dialect.

- One POSIX script language keeps workspace config portable. Git for Windows
  is already a hard prerequisite for smind's git worktrees, so this adds no
  new dependency.
- An authenticated endpoint is the one shutdown channel every caller can
  reach (desktop app, CLI, WSL, tests). It needs no new trust boundary,
  because the token already authorizes destructive WS methods such as
  `workspace.delete`. Note that `GET /api/token` deliberately serves the
  token unauthenticated for the bundled UI, so "has the token" means
  "can reach the daemon's port", exactly as it does for `/ws` today.
- An explicit DACL makes "only you can read the daemon token" true by
  construction instead of by inheritance.
