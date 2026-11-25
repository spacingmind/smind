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

(filled in with the list of every dial-after-close site found and what was done)

## Progress

- [ ] Spec and ADR amendment committed (this file)
- [ ] Pre-allow and its tests
- [ ] Flake fix and audit, 50 runs under -race
- [ ] public-docs, reviews, mutations, live check or not-verified

## Validation

(filled in when the work is done)
