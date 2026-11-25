# Browser for agents: pre-allow the browser's tools on the Claude path, and the proxy-test flake

Human decisions 2026-11-25 (ADR-0061 amendment 11). Two independent items in one PR; HIGH-RISK (it changes the permission configuration the Claude CLI is launched with).

## 1. The double prompt: pre-allow `mcp__smind-browser__*`

Why: the live run (`browser-for-agents-live-run.md`, finding 3) showed two cards for every Claude Code browser call, the CLI's own prompt and the daemon's. The daemon's card is the control; the CLI's adds nothing the person decides. Option (a) was chosen.

### Acceptance criteria

- AC1 For a claude-native run that receives the browser row (the pending-socket path built the row and a socket bound to the CLI pid is open), the CLI is launched with `--allowedTools mcp__smind-browser__*` and nothing else in that list.
- AC2 No browser row means no pre-allow: no `--allowedTools` flag at all. This covers: no browser policy in the run contract, an SDK that cannot report the pid, a claude provider the reach table says no to (an instance without MCP servers), an opener that fails, a run with only orchestration or user MCP servers.
- AC3 The allowed list is exactly the one rule: never `mcp__<other>__*` for a registered MCP server, never `mcp__smind__*` for the orchestration server, never a built-in tool; asserted on the full argv of the fake CLI for runs with and without the row, with user servers registered, with an orchestration seat, with a user server named like the reserved one.
- AC4 A user-registered MCP server named `smind-browser` is stripped (the reserved name) and never earns the rule: with no browser row the flag is absent, with the row only the daemon's row exists.
- AC5 Non-claude providers get nothing: the ACP and Codex paths have no allowed-tools mechanism, and the pre-allow helper returns nothing for a run whose row was not delivered; a table test over the helper pins it.
- AC6 The daemon's gate is untouched: with the pre-allow in place the daemon card still holds the call (`TestGate_*` unchanged); the live check below shows it.
- AC7 `public-docs/browser.md` and ADR-0061 (amendment 11) say what is pre-allowed and why; the browser plan records the decision.

### Named tests (`internal/taskrunner/claude_browser_preallow_test.go`)

`TestClaudePreallow_OnlyTheBrowserWildcardForARunWithTheRow`, `TestClaudePreallow_NoRowNoAllowedTools` (table: no policy, SDK without pid, a provider with no MCP servers, an opener that fails), `TestClaudePreallow_NeverAnotherServer` (user server, orchestration seat, user server named like the reserved one), `TestClaudePreallowRule` (the helper: rows and bound flag), `TestClaudePreallow_NotForANonClaudeProvider`.

### Mutations (committed files, each must fail a named test)

The rule widened to `mcp__*`; the rule added with no row; the rule added for an unbound socket; the user-named row earning the rule; the helper returning the rule for a non-browser row.

### Live check (only if cheap)

One Haiku run on the real `claude` binary in a dogfood daemon (own `SMIND_HOME` and port, rules as in the live-run plan, at most USD 0.05): the CLI's own prompt is gone, the daemon's cards still appear. Otherwise reported as not verified.

## 2. The flake: `TestSessionStartFailureStopsTheProxy`

CI run 45568952075 failed it: after the session closed, the test dialled the proxy's OLD port, and another test in the package (parallel) had taken that ephemeral port, so the dial succeeded. Dialling a port after closing its listener is not evidence about the listener. Fix: assert on the listener/proxy state, never on a reused port.

- AC8 The three proxy tests that dial an address after closing (`TestProxyCloseEndsListenerAndTunnels`, the session-close test, `TestSessionStartFailureStopsTheProxy`) no longer dial a closed port: a helper asserts the listener is closed through the listener itself (`Accept` returns `net.ErrClosed` at once, never blocks); the start-failure test holds a connection to the proxy from inside the launcher and requires that connection to be closed by the proxy after the failed start.
- AC9 The whole of `internal/browser`, `internal/browser/runsock`, `internal/listeners` and the browser tests of `internal/wsapi` are audited for the same pattern (a dial or request to a port after its listener was closed). Unix-socket dials after close are path-based (a path is unique per run and not reused by another listener) and are not this class; they are listed in the audit below, not changed.
- AC10 Proof: the changed tests pass `-race -count=50` while the rest of the package runs alongside (`t.Parallel`), and a mutation (the listener not closed by `Close`) fails each of them.

### Audit

Searched `internal/browser`, `internal/listeners`, `internal/wsapi` (test files) for `net.Dial`, `net.DialTimeout`, `DialContext`; `internal/browser/runsock` read separately.

- TCP dial after close (the flake class), fixed: `TestProxyCloseEndsListenerAndTunnels`, the session-close test (`TestSessionStartsAProxyAndDeniesItToThePage`) and `TestSessionStartFailureStopsTheProxy` in `proxy_test.go`. They now ask the closed proxy's listener (`assertListenerClosed`: `Accept` must return `net.ErrClosed`), and the start-failure test holds a connection opened inside the launcher and requires the proxy to close it (read deadline under the proxy's own 2 s header timeout, so an idle cut cannot pass for a Close).
- Dials to a LIVE proxy or server (`proxy_test.go` rig/hit servers, `proxy_auth_test.go`, `smoke_test.go`): not the class; nothing released first.
- `runsock` unix-socket dials after close: path-based, no ephemeral port to be reused; not changed. `internal/listeners`, `internal/wsapi`: no dial-after-close sites.

## Progress

- [x] Spec and ADR amendment committed (this file)
- [x] Pre-allow and its tests
- [x] Flake fix and audit, 50 runs under -race
- [x] public-docs, mutations; reviews and live check below

## Validation

- Flake: the three tests pass `-race -count=50` while the rest of the package runs in a second process; with `Close` not stopping the server all three fail (committed file, restored, tree clean).
- Pre-allow mutations (all restored, tree clean): rule widened to `mcp__*`, unbound check removed, name-only row match, wiring removed: each fails a named `TestClaudePreallow*`. Passing `true` instead of `bindBrowser != nil` is equivalent (rows only hold the browser row when bound), kept as defence in depth.
- Live Haiku check: not verified (see the PR).
- Reviews (two fresh sub-agents, standards and attack). Accepted and fixed: a server registered as `smind-browser__x` (tool `mcp__smind-browser__x__t`) would sit under the wildcard, so any name under that prefix is now never configured (`TestClaudePreallow_NeverAnotherServer`, mutation fails it); the vacuous non-claude test was renamed to what it checks. The CLI-precedence question is answered below with evidence and closed with strict mode.

### Evidence: can another server answer to the reserved name? (real CLI 2.1.325, Haiku, total about USD 0.06)

Scratch under the dogfood root: a stdio MCP server with one tool `whoami` that returns its label, a project `.mcp.json` defining `smind-browser` (label PROJECT) or `smind-browser__x`, and a `--mcp-config` defining `smind-browser` (label CONFIG); every call `claude -p ... --model haiku --allowedTools 'mcp__smind-browser__*' --output-format json`.

| Case | Result |
| --- | --- |
| `claude mcp list` in the project dir | the project server shows "Pending approval", but see next row |
| project `.mcp.json` `smind-browser` only (no `--mcp-config`), pre-allowed | the tool RAN: `LABEL=PROJECT`, no approval (project servers load in `-p` mode, and the wildcard covers them) |
| same project file plus `--mcp-config` defining the same name | `LABEL=CONFIG`: `--mcp-config` wins a clash with project scope |
| project server named `smind-browser__x`, wildcard pre-allow | NOT matched: permission denial for `mcp__smind-browser__x__whoami` (same denial without the pre-allow) |
| project `smind-browser` plus `--mcp-config` plus `--strict-mcp-config`, pre-allow | `LABEL=CONFIG`, project file ignored |
| project `smind-browser__x` plus `--strict-mcp-config` | its tool is absent; the model reached only the CONFIG server |

Not provable without editing the real login state: user-scope and local-scope clashes (a scratch `CLAUDE_CONFIG_DIR` has no login). So the guarantee for those scopes comes from strict mode, not from precedence.

Answers: (a) yes, project servers load without approval in `-p` mode; (b) `--mcp-config` beats project scope on a clash, user/local not tested; (c) `--strict-mcp-config` makes the run immune (only the daemon's config is loaded).

Decision (option 1, strongest): a run that gets the pre-allow also gets `--strict-mcp-config`, so nothing from a project `.mcp.json`, user or local scope can carry the reserved name or any other name. Product impact: such a run (a Claude run with the browser on for the workspace) does not see MCP servers configured in the CLI's own files; servers registered in sMind are still delivered through `--mcp-config`. A run without the row keeps today's behaviour (not strict, no pre-allow). Pinned by `TestClaudePreallow_OnlyTheBrowserWildcardForARunWithTheRow` (strict present) and `TestClaudePreallow_StrictOnlyWithThePreallow`; removing the option fails the first.
