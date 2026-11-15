# Browser for agents, headless first: implementation spec for the lane

Governing ADR: `docs/decisions/0061-browser-for-agents.md` (Accepted, all 12 decisions as recommended). Acceptance criteria for the whole feature live in `docs/plans/active/desktop-browser-for-agents.md` (AC1-AC20); this file orders the work for the headless-first lane and holds the detail, tests, decisions, progress and validation of each PR.

Size: L (7) — scope 2, PRs 2, risk 2, decisions 0, verification 1. Every PR is high-risk (SSRF, human-only surface, permissions): two fresh-context review sub-agents (standards, attack) on each, findings in the PR body.

## Order (one PR each, from a fresh `origin/wave/desktop`, never stacked)

| PR | Content | Covers |
|---|---|---|
| 1 | Go CDP client, headless session, URL policy, Chrome detection (library only, nothing reachable by an agent) | AC3, AC10, AC11, AC13 (parts), driver decision 2 |
| 2 | The `smind-browser` stdio MCP child with the read-heavy `browser_*` tools, broker envelope, limits, rate limits | AC2, AC4, AC5, AC7, AC9 |
| 3 | Human-only enable flag (default OFF) and the per-workspace loopback allowlist, on the shared human-only tables with the drift test | AC1 (switch), AC6 |
| 4 | Permission integration: read tools grantable per run, acting tools ask every time | ADR section 10 |
| 5 | Provider gating table (no row for codex-native, plugin strip rule, settings list text source) | AC8 |

Acting tools (click, fill, type, press, scroll, history) are not in PR 2 ("read-heavy set only"); they follow after PR 4 or as the Steward decides.

## PR 1: CDP client, session, URL policy (package `internal/browser`)

Decisions made inside the ADR (no new architecture): Go CDP client over a flattened browser-level websocket (gorilla, already a dependency), Chrome is a system dependency found by detection and never installed or downloaded; absent means `ErrUnavailable` with the install step. Nothing in this PR is wired to the daemon, a run, an RPC or an MCP row.

### Acceptance criteria

- P1-AC1 `CheckURL` is one pure function over a `Policy`: http/https only (`about:blank` exact is the only other URL), userinfo refused, backslash, whitespace, control characters, `%` in the host and non-ASCII hosts refused, the host normalised the way a browser parses it (decimal, octal, hex, short dotted IPv4, trailing dot, IPv6, IPv4-mapped IPv6) before it is compared. Loopback (`localhost`, `*.localhost`, 127.0.0.0/8, `::1`) is allowed on any port; any other host only if its exact normalised text is in `Policy.AllowHosts`; private, link-local and metadata addresses are refused unless the exact IP literal was added. `Policy.Deny` (the daemon's listen address and port, the desktop proxy origin, other listed daemon ports) is refused whatever the allowlist, and a wildcard bind or loopback entry denies that port on every loopback form.
- P1-AC2 `CheckResolved` checks the addresses a named host resolves to: every address must be loopback, or the name was allowlisted and the address is not link-local, metadata or unspecified; a loopback-named host that resolves off loopback is refused; `Policy.Deny` applies to the resolved address and port. A resolver error refuses.
- P1-AC3 `DetectChrome` finds a system Chrome/Chromium/Edge from an explicit path, then the standard install locations and PATH names; none found returns `ErrUnavailable` carrying the install step. It never runs an installer or fetches anything.
- P1-AC4 The CDP client multiplexes calls and events over one websocket, flattened sessions, per-call timeout and context cancel, error responses surface as `*CDPError`, connection loss fails every pending call.
- P1-AC5 The launcher starts the browser headless with a fresh user-data-dir, `--remote-debugging-port=0` (the port is read from `DevToolsActivePort`), no `--no-sandbox`, host-resolver rules that map everything outside loopback and the allowlist to nothing, a JS heap cap, and kills the whole process group on close; the profile dir is removed on close when ephemeral.
- P1-AC6 The session enforces the policy on every request of every attached target (document, subresource, worker, iframe) through `Fetch.requestPaused`, which fires on each redirect hop, and fails refused requests with `BlockedByClient`; the refusal is recorded as a notice.
- P1-AC7 Popups and new windows are tabs in a pool of at most 3 per session; one beyond is closed and reported. No OS window.
- P1-AC8 `confirm`, `prompt`, `beforeunload` are cancelled, `alert` is dismissed, each reported. Downloads are denied (`Browser.setDownloadBehavior`) and the attempt is reported with the file name.
- P1-AC9 Session lifecycle: `Manager` allows one session per workspace and at most 2 concurrent sessions daemon-wide; a session closes after 5 idle minutes, on `Close`, or when the connection drops, and then kills the browser; use after close fails.
- P1-AC10 `Session.Navigate` checks the URL before sending it; a refused URL never reaches the browser.

### Named tests (all against a fake CDP endpoint, no browser)

- `TestCheckURLTable` (every form in P1-AC1, deny rows, mutations below), `TestCheckURLRedirectChain` (each hop re-checked, the chain stops at a refused hop), `TestCheckResolvedTable`.
- `TestDetectChrome` (explicit path, fake PATH, none found is `ErrUnavailable` with the install step).
- `TestCDPCallEventAndErrors`, `TestCDPCallTimeout`, `TestCDPConnectionLossFailsPending`.
- `TestLauncherArgs` (args pinned, no `--no-sandbox`, resolver rules, profile dir removed), `TestLauncherReadsDevToolsActivePort`.
- `TestSessionBlocksRefusedRequestAndContinuesAllowed`, `TestSessionRedirectHopRefused`, `TestSessionPopupPoolCap`, `TestSessionDialogsCancelled`, `TestSessionDownloadDeniedAndReported`, `TestSessionNavigateRefusedNeverSent`, `TestManagerOneSessionPerWorkspaceAndCap`, `TestSessionIdleClose`, `TestSessionUseAfterClose`.
- `TestRealChromeSmoke` (opt-in `SMIND_BROWSER_SMOKE=1` and a detected Chrome; headless, loopback page the test serves, isolated profile dir; skips with a clear message otherwise).

### Mutations to run (file, change, the test that must then fail)

- `policy.go`: add `*` allow for any host -> `TestCheckURLTable`.
- `policy.go`: skip the `Deny` check -> `TestCheckURLTable` (daemon port rows).
- `session.go`: drop the policy check on a paused request -> `TestSessionBlocksRefusedRequestAndContinuesAllowed`.
- `session.go`: accept `confirm` -> `TestSessionDialogsCancelled`.
- `session.go`: raise the tab cap -> `TestSessionPopupPoolCap`.
- `launch.go`: drop the host-resolver rules -> `TestLauncherArgs`; add `--no-sandbox` -> `TestLauncherArgs`.
- `session.go`: do not deny downloads -> `TestSessionDownloadDeniedAndReported`.

## Decisions

- Resolved-address policy for an allowlisted name: its addresses may be private or public but never link-local, metadata or unspecified (a name cannot be pointed at the metadata service by rebinding even when the human allowlisted it). An IP literal the human typed exactly is allowed as typed (ADR section 5).
- The policy takes a `Deny` list as input; PR 3 fills it from the daemon's listen address, the proxy origin and the connection list.
- No `evaluate`, upload or download surface exists anywhere in the package.

## Progress

- [ ] PR 1 open
- [ ] PR 2
- [ ] PR 3
- [ ] PR 4
- [ ] PR 5

## Validation

Filled per PR (what ran, on which provider, what was verified only against the fake).
