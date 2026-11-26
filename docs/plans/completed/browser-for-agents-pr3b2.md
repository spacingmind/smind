# Browser for agents, PR 3b-2: a per-run socket with a peer-credential check

Closed 2026-11-26: PR 3b-2 shipped inside the browser lane PRs (#957 and the gate PR); Progress below was never ticked and Validation was never filled in this file.

Governing: ADR-0061 amendment item 7 (Human decision 2026-11-16) and "PR 3b spec" in `browser-for-agents-designs.md`. 3b-1 (proxy authentication, #958) is done; this is the second hard prerequisite of opening the gate. It flips `browser.PrereqRunSocket` and nothing else: `GateReady` stays false until PR 4.

Size: L (7) — scope 2, PRs 1, risk 3, decisions 0, verification 1. High-risk: foreground two-sub-agent review (standards, attack), then the Steward's own.

**Branching.** This PR rewrites `internal/wsapi/browser_call.go` and its tests, which PR 3 (#957) also edits heavily. It is built from a fresh `origin/wave/desktop` AFTER #957 merges, never stacked. Until then this file is the only artifact.

## Why

Today the `smind-browser` MCP child reaches the daemon over the daemon websocket (`session.identify` as the run, then `browser.call`) and proves itself with a per-run capability token that lives in its environment. A same-uid process (another run's agent shell) can read a process environment on some systems and so drive another run's browser. The Human accepted that for PR 2 as a stated limit and made its removal a hard prerequisite of PR 4.

## Design

**Channel.** The daemon listens on one local socket per browser run. The child dials it instead of the daemon websocket; the token and the websocket are no longer used by the child. The socket speaks one method, `browser.call` (a closed set of one), in a minimal protocol (below), and nothing else.

**The check, on every accepted connection, before a byte is read:**
1. the peer's uid equals the daemon's uid;
2. the peer's pid is the run's root agent process or one of its descendants (walk the peer's parent chain to the root; the root is the process the daemon itself spawned for the run);
3. the root is still the same process (its start time equals the one recorded when the socket was created: a reused pid is refused);
4. any failure, or a process that exits mid-check, closes the connection at once without a reply.

What this protects: another run's agent shell (a different tree), another user, any process outside the run's tree. What it does not: processes inside the run's own tree (the run's own agent and its shells are the browser's legitimate caller; same trust as the run). Descendants that detach and reparent (double fork) fall outside the tree and are refused (fail closed).

**Identity of the root.** The daemon starts the agent (ACP, or codex) before it builds the MCP rows (`runner.go` starts the client, then `mcpRowsForDelivery`). The launcher exposes the started process's pid (and its start time); the row's `Browser` policy gets a `Socket func(root ProcessID) (path string, err error)` that replaces `Issue`: called at the delivery point with the agent's pid, it creates the run's listener bound to (run id, root) and returns the path. A provider that cannot report a root pid gets NO browser row (never a weaker check), and a row the provider then skipped revokes the listener (as the token was revoked).

**Where it lives.** A small package `internal/browser/runsock` holds everything that is not daemon wiring, so it is testable without a daemon:
- `PeerCred{UID uint32; PID int}` and the interface `PeerReader { Peer(net.Conn) (PeerCred, error) }`, with `peercred_linux.go` (`SO_PEERCRED`), `peercred_darwin.go` (`LOCAL_PEERCRED` for the uid, `LOCAL_PEERPID` for the pid, via `golang.org/x/sys/unix`), `peercred_windows.go` and `peercred_other.go` (unsupported: the reader returns an error, so no browser is delivered).
- `ProcTree { Parent(pid int) (ppid int, start uint64, err error) }` with `proctree_linux.go` (`/proc/<pid>/stat`: fields 4 and 22, parsed from the end of the line past the `)` of the command name), `proctree_darwin.go` (`unix.SysctlKinfoProc("kern.proc.pid", pid)`), `proctree_other.go`.
- `InTree(tree ProcTree, peer, root int, rootStart uint64) (bool, error)`: walks parents, depth cap 64, cycle guard, stops at pid <= 1; the root's start time is re-read after the walk and compared.
- the server: the socket directory and file hygiene, accept loop, the check, the protocol, close at run end.
- `//go:build windows` code (named pipe) behind the same two interfaces.

**Socket hygiene.** Directory `<daemon home>/browser/sock` created `0700`, verified with `Lstat` (a directory, not a symlink, owned by the daemon's uid, mode `0700`); otherwise refused. The socket is created with a restrictive umask and verified `0600`. Name: 12 hex characters of `sha256(run id)` plus `.sock`. Unix socket paths are limited (104 bytes on macOS, 108 on Linux): if the path is longer, fall back to `/tmp/smind-<uid>/` created and verified the same way; if neither fits, no browser row is delivered. At daemon start, every file in the directory is removed (no run survives a daemon restart). At run end (the existing `OnRunFinished` observer) the listener and every open connection are closed and the file is removed.

**Protocol.** Newline-delimited JSON, one request per line, at most 16 KiB (a tool call is a few hundred bytes), one call in flight per connection, replies are the broker's envelope as JSON (screenshots are base64 in it; replies are not capped by the request cap). Requests: `{"tool": string, "args": object}`; unknown fields, a missing tool or malformed JSON close the connection. No method name: the socket can only call `browser.call`. Read and write deadlines (idle 5 minutes, 30 s to write a reply).

**One shared call core.** `browserHost.call` is split into the websocket-free core `callRun(ctx, runID, taskID, tool, args)` (run live, kill switch, workspace gate, broker) used by the socket handler. The websocket `browser.call` RPC is removed (not registered, not on `RunScopedMethods`, golden regenerated), so an identified run connection can no longer reach the browser at all.

**Token path removed.** `Registry.issueBrowserToken`, `CheckBrowserToken`, `ForgetBrowserToken`, `browserTokens`, `BrowserTokenEnv`, the `SMIND_BROWSER_TOKEN` env entry and `BrowserPolicy.Issue/Revoke` go. The child's environment carries `SMIND_BROWSER_SOCKET` (and `SMIND_RUN_ID` for its own messages) and nothing secret; the plan and ADR text about the token's residual is rewritten as "closed by the peer check".

**Child.** `smind mcp browser` requires `SMIND_BROWSER_SOCKET`, dials it (no `dialDaemon`, no `session.identify`), forwards each tool call as one line and converts the envelope exactly as today. It fails closed if the socket is unreachable.

**Windows (written before coding, not verifiable on this macOS machine).** A named pipe `\\.\pipe\smind-browser-<12 hex>` created with `FILE_FLAG_FIRST_PIPE_INSTANCE` (no name squatting), a DACL granting access to the daemon's user SID only (`D:(A;;GA;;;<sid>)`, no inheritance) and `PIPE_REJECT_REMOTE_CLIENTS`. On each connection: `GetNamedPipeClientProcessId`, then `OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION)` and `IsProcessInJob(process, run's Job)` where the job is the run's kill-on-close Job Object that `internal/winjob` already creates for the agent (created without `JOB_OBJECT_LIMIT_BREAKAWAY_OK`, so a descendant cannot leave it). "In the run's job" is the Windows equivalent of "descendant of the run's agent". If a provider's agent is not job-contained, that provider gets no browser on Windows. This code is compile-checked (`GOOS=windows go vet`/`go build`) and unit-tested against interfaces (a fake pipe connection that reports a client pid, and a fake job-membership oracle), and **its first real run is the Windows CI job plus a manual run the Human must do**; the PR body says so in a NOT verified section. The Linux code is compile-checked here (`GOOS=linux`) and run by the Linux CI job (the unit tests that need `/proc` use fixtures and run everywhere).

## Acceptance criteria

- P3b2-AC1 The browser child reaches the daemon only through the per-run socket; the daemon websocket `browser.call` is removed and unreachable; no token exists anywhere (`SMIND_BROWSER_TOKEN`, `CheckBrowserToken` etc. are gone) and `go vet` fails on a reference.
- P3b2-AC2 On every accepted connection, before a byte is read, the daemon reads the peer's credentials and refuses unless the uid is the daemon's and the pid is the run's root or a descendant of it with the root's start time unchanged. A refused connection is closed with no reply and counted (a bounded, once-noted refusal record, no process data in it).
- P3b2-AC3 A peer of another run's tree, a process outside every tree (the test process itself), a different uid (through the fake reader), an exited peer, a pid whose root start time changed, a process whose parent chain loops or is deeper than 64, and a process that reparented to pid 1 are all refused.
- P3b2-AC4 The socket directory and file are `0700`/`0600`, owned by the daemon's uid, never a symlink; a directory with the wrong owner, mode or type is refused (no browser); the path-length fallback works and a path that fits neither is refused with no browser row.
- P3b2-AC5 The listener, its connections and the file are gone when the run ends, when the row is skipped, and after a daemon restart (stale files swept at start).
- P3b2-AC6 The protocol is closed: only `{"tool","args"}`, 16 KiB requests, unknown fields and malformed JSON close the connection; one in flight; deadlines.
- P3b2-AC7 The shared call core enforces the same gates as before on this path: run live, kill switch (and `CloseAll` at the next call), the workspace gate, the broker's limits; the flag-off, kill-switch and ended-run cases refuse.
- P3b2-AC8 A provider that cannot report its root pid (or an unsupported platform) gets no `smind-browser` row and no listener; a plugin provider never gets one (unchanged).
- P3b2-AC9 `browser.PrereqRunSocket` is `true`; `GateReady` is still `false`; `TestGateReadyRequiresBothPrerequisites` passes with both prerequisites true and the gate false.
- P3b2-AC10 The Windows named-pipe path compiles (`GOOS=windows`), is unit-tested against the interfaces, and is described as not run on Windows in the PR body and the plan.

## Named tests

`runsock` (Linux and macOS, real sockets): `TestPeerReaderReportsOwnCredentials` (a connected pair: uid and pid are this process), `TestInTreeTable` (fake `ProcTree`: the root itself, a child, a grandchild, a sibling tree, loop, depth 65, pid 1 reparent, vanished process, start-time mismatch), `TestProcTreeLinuxFixture` (a `/proc` fixture directory with a command name that contains `)` and spaces), `TestProcTreeDarwinParent` (the parent of this process is `os.Getppid()`), `TestServerAcceptsADescendant` (a helper process tree: the test starts a root helper, which starts the child helper that connects; the call succeeds), `TestServerRefusesOutsideTheTree` (the test process connects: closed with no bytes), `TestServerRefusesAnotherRunsTree` (two roots, the child of B dials A's socket), `TestServerRefusesAnotherUID` (fake reader), `TestServerRefusesAnExitedPeer`, `TestServerRefusesAReusedRoot`, `TestSocketDirAndFileModes`, `TestSocketDirRefusedWhenWrongOwnerOrSymlink` (symlink real; owner through an injected `Lstat`), `TestSocketPathFallbackAndTooLong`, `TestStaleSocketsSweptAtStart`, `TestCloseRemovesListenerFileAndConnections`, `TestProtocolClosedSet` (oversize, unknown field, bad JSON, second concurrent request), `TestDeadlines`.
`wsapi`: `TestBrowserCallIsNotAWebsocketMethod` (not registered, golden, `RunScopedMethods`), `TestCallRunHonoursTheGates` (live, kill switch with `CloseAll`, workspace gate), `TestNoRowWithoutARootPid` (the policy hook returns an error: no row, no listener, no token env), `TestRowEnvHasNoSecret`.
`runs`/`taskrunner`: `TestBrowserPolicyHasNoToken` (the contract has no `Issue`/`Revoke`), `TestSocketCreatedAtDeliveryAndRevokedWhenSkipped`.
`cmd/smind`: the child tests move to a fake socket server (`TestBrowserMCP_ForwardsToTheSocket`, `TestBrowserMCP_RequiresTheSocket`, the existing envelope and schema tests unchanged).
`browser`: `TestGateReadyRequiresBothPrerequisites` (updated).
Windows (compile-checked, unit-tested against interfaces): `TestPipeSecurityDescriptor` (the SDDL string is the daemon's SID only), `TestWindowsPeerCheckUsesTheJobOracle` (fake client pid, fake `IsProcessInJob`).
Real run (opt-in `SMIND_BROWSER_SMOKE=1` on this Mac): a real helper agent tree dialling a real socket, then the read-tools smoke through it.

## Mutations (file, change, the test that must then fail)

The uid comparison removed; the ancestry check replaced by "any same-uid peer"; the root start-time check removed; the post-walk re-read removed; the depth cap or cycle guard removed; socket mode widened to `0666`; directory mode `0755`; the `Lstat` owner/symlink check removed; the removal at run end skipped; the stale sweep skipped; the websocket `browser.call` left registered; the token env left in the row; a request over 16 KiB accepted; unknown fields accepted; the kill-switch check dropped from `callRun`; a missing root pid delivering the row anyway; `PrereqRunSocket` true while the listener is not wired (a test that a delivered row's env names a real, listening socket).

## Staffing

GLM peers (one per self-contained, interface-bounded step, step-by-step briefs, one commit per step, no push): **peer A** `internal/browser/runsock/peercred*.go` (the interface, `peercred_linux.go`, `peercred_darwin.go`, `peercred_other.go`, a fake reader, and `TestPeerReaderReportsOwnCredentials`); **peer B** `internal/browser/runsock/proctree*.go` and `intree.go` (the interface, the Linux `/proc` parser with its fixture test, the Darwin sysctl reader, `InTree`, a fake tree, `TestInTreeTable`, `TestProcTreeLinuxFixture`, `TestProcTreeDarwinParent`). I own: the server (directory hygiene, accept, the check, the protocol, close), the wsapi `callRun` split and the removal of the websocket method, the token removal, the runner and launcher plumbing of the root pid, the child, the Windows code and its tests, the prerequisite flip, the docs, the mutations and the review fixes. I review and integrate both peers' diffs.

## Decisions taken unless told otherwise (none architectural)

- Trust boundary is "the run's tree", not "the run's MCP child alone": the run's own shells are the run's own principal.
- A provider without a reportable root pid gets no browser (fail closed), including every provider on a platform with no peer-credential reader.
- Replies are not size-capped by the socket (the broker's own caps apply); requests are capped at 16 KiB.

## Not in 3b-2

Isolating same-user processes from the daemon itself (the daemon token on `/ws`, ADR-0056's open question), authenticating the page content, the permission gate and `GateReady` (PR 4), the Settings UI.

## Progress

- [ ] #957 (PR 3) merged
- [ ] peers A and B (platform helpers) integrated
- [ ] server, wsapi, token removal, child
- [ ] Windows code compile-checked
- [ ] prerequisite flipped, reviews, PR

## Validation

Filled when built. The Windows path will be marked NOT verified on Windows.
