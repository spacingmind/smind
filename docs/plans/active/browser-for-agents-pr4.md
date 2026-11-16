# Browser for agents, PR 4: the daemon-side permission gate that flips `GateReady`

Governing: ADR-0061 section 10 and amendments (decisions D-B and D-D of 2026-11-15, the two prerequisites of 2026-11-16). PR 1 to 3b are merged: the CDP client and URL policy, the enforcing authenticated proxy, the read tools behind the broker, the human-only enable flag and allowlist, the per-run socket with peer credentials. Both prerequisites (`PrereqProxyAuth`, `PrereqRunSocket`) are true. **This PR is the one that makes the browser reachable by an agent**: it flips `browser.GateReady` to true, and it is the only PR that may.

**Human decisions 2026-11-16 (recorded in ADR-0061 amendment 10):** (1) Claude-native browser: option (a), an upstream `Client.PID()` in `claude-agent-sdk-go` (separate PR in that repo, tagged v0.4.3 for this change only by the Supervisor), then smind pins v0.4.3 and the claude path binds an unbound socket to the CLI's pid; (2) Windows: option (a), not offered in the first release, the hub keeps refusing, the Job Object membership check goes on the later list. After PR 4 the browser lane stops: no PR 5 and no new work; a state note lists what is left.

**Status: built (see "PR 4: progress, deviations and validation" at the end). The sections below are the spec as accepted; where the built PR differs, the last section says so.** Size: L (8) — scope 2, PRs 1, risk 3, decisions 1, verification 1. HIGH-RISK: foreground two-sub-agent review (standards, attack), then the Steward's own, mutations.

## Open decisions (answered by the Human on 2026-11-16; kept for the record, recommendation first)

1. **The Claude CLI path has no browser** (PR 3b-2, deviation 1). The run socket is bound to the agent's process id; `claude-agent-sdk-go` v0.4.2 starts the CLI after the MCP rows are built and does not expose its pid. Options: (a) **add `Client.PID()` upstream (a one-line change in the same organisation's SDK), bump the SDK, and make the claude path build its rows with the CLI's pid** (the CLI would have to be started before its MCP config is written, or the socket bound after start and the row's path known beforehand: see "Claude path design" below); (b) accept the gap: claude-native has no browser and the Settings text says so. **Recommendation: (a)**, because claude-native is the primary provider and a browser that only GLM and the other ACP agents can use is a surprising product. If (b), nothing in this PR changes except the Settings/provider text.
2. **Windows.** The browser is not offered (the parent-chain check is spoofable; the Job Object membership check is not built). Options: (a) **stay off on Windows for the first release, say so in the Settings text and the docs**; (b) build the job-membership check first (plumb the run's Job handle to the hub, `IsProcessInJob`, authenticate the pipe's server end), then a manual Windows run. **Recommendation: (a)**; (b) is its own PR after this one.

## What this PR does

When a run's agent calls a browser tool, the daemon (not the provider) decides, in `browserHost.callRun`, **before** `broker.Call`: allowed now, refused, or held for a person. The provider's own permission mode is irrelevant (D-B): a provider in an auto-accept mode never asks, so a daemon-side hold is the only gate that holds. Provider-native prompts for `mcp__smind-browser__*` and `mcp:smind-browser.<tool>` policy rules stay a second layer.

### Tool classes

| Tool | Class | Gate |
|---|---|---|
| `browser_tabs`, `browser_snapshot`, `browser_screenshot`, `browser_console`, `browser_wait` | READ | one grant per run covers all five |
| `browser_open` | ACT, grantable | asks; its card offers "allow for this run" (D-D) |
| `browser_close` | ACT, same grant as open | closes a tab the run opened; covered by the run's open grant, asks otherwise (decision taken unless told otherwise) |
| any later acting tool (click, fill, type, press, scroll, history) | ACT | asks every call, never grantable (D-D) |

An unknown tool name is refused before the gate (the broker already refuses it; the gate must not allow a card to be raised for one).

### Decision order in the gate (first match wins)

1. The existing checks (run live, kill switch, workspace switch, task lookup).
2. **The URL pre-check for `browser_open`**: `CheckURL` against the workspace policy first. A URL the policy would refuse is refused at once with the policy's reason and raises no card (a person is never asked about a navigation that can never happen).
3. **Human policy rules** (ADR-0047): the engine is asked about `mcp:smind-browser.<tool>`. `deny` refuses without a card; `allow` (the human's own saved rule, workspace-scoped) satisfies the gate for that tool; `ask` or no rule goes on. Default is ask, never allow.
4. **The run's grants** (in memory, per run id): a read grant admits the five read tools; an open grant admits `browser_open` and `browser_close`.
5. **Otherwise: hold the call and raise a card** (below). The broker is not called until the card is answered.

`AutoAccept`, bypass modes and auto-accepting seat profiles do **not** satisfy the gate. A card is raised for them too; a run with nobody to answer waits for the permission timeout and then the call is refused ("nobody answered the permission request"), so unattended automation runs get no browser unless the human wrote an `allow` policy rule for them.

### The card

It is an ordinary run permission request (the same pending-permission path, inbox, answer RPCs, web, desktop and mobile cards as every other request: no new UI surface and no new answer RPC; answering stays human-only, so an agent cannot approve itself). The detail uses the existing structured permission detail (a summary line plus labelled fields), kind `browser`.

- **Read grant card.** Title: "Let this run read browser pages?" Body: "`<task title>` wants to use the browser in `<workspace>` (read-only tools: tabs, snapshot, screenshot, console, wait). It can reach this computer's localhost and these hosts you added: `<allowlist or "none">`. Everything a page shows is treated as untrusted text." Options: **Allow reads for this run** (creates the read grant), **Allow once**, **Deny**.
- **Open card.** Title: "Open `<host><path>` in the browser?" Body: the URL without query or userinfo, the host's policy status ("localhost" or "added by you"), and "This run has used the browser N times." Options: **Allow once**, **Allow `browser_open` for this run** (creates the open grant, which also covers `browser_close`), **Deny**.
- **Acting tool card** (future tools): the tool and its target, **Allow once** and **Deny** only.
- Every card says in one line that it is the daemon's own check, not the provider's, so a person who also sees the provider's prompt understands the double prompt.
- A denial returns a plain tool error to the agent ("the person said no"); **three denials in one minute stop further cards for five minutes** (the call is refused at once, "the person declined; do not ask again for now"), so a model cannot nag.
- One card at a time per run (calls are already one in flight per run); a second tool call waits behind the first.
- Hold limits: the card waits for the existing permission timeout, capped at 120 s for this gate (then the call is refused with "no one answered; ask again if still needed"); the child's per-call deadline is raised to 150 s so the held call is not cut by the child; the broker's 20 s call budget starts after approval.

### Grants

In memory, in `runs.Registry`, keyed by run id: `readGrant`, `openGrant`. **No grant outlives its run, none is saved, none has an "always" scope.** They are dropped at run end (`OnRunFinished`), when the workspace switch turns off, when the kill switch turns on, and when the run's socket is closed. A grant is recorded in the run timeline ("browser: read access granted for this run by `<actor>`", no page content). The permission gate for a call re-reads the grant at the moment of the call, after the checks of step 1, so revoking the switch ends a held grant at once.

### Flipping the gate

`browser.GateReady = true`, in this PR only. `TestGateIsDarkUntilThePermissionGatePR` becomes `TestGateIsOpenOnlyWithBothPrerequisitesAndTheGate` (it asserts `GateReady` implies both prerequisites). The settings view's `available` becomes true and the CLI stops saying "not yet offered". The workspace switch is still OFF by default and human-only, so nothing becomes reachable for any workspace until a human turns it on with `smind browser enable` (the Settings screen is the separate web PR B3; enabling from the CLI only is acceptable for the first release).

## Claude path design (only if decision 1 is (a))

The Claude CLI is started by the SDK from `claudecode.New` after the daemon wrote its MCP config. Design: add `Client.PID() int` to the SDK (upstream); in the runner, start the claude client first with the MCP config written afterwards is not possible (the config is an input of the start). So: the daemon **pre-creates the run's socket path and a root slot** at row-build time (`Socket(pendingRoot)` returns the path and a `Bind(pid)` function), starts the client, calls `Bind(client.PID())`; connections that arrive before `Bind` are refused (the child dials per call and its first call comes after the CLI has started it, so the race is a few milliseconds; a refused first call is retried once by the child after 200 ms). The hub gets `OpenPending(runID, handler) (path, bind func(pid int) error)`; `Bind` records the root's start time exactly as `Open` does. Tests: a connection before `Bind` is refused, after `Bind` the same peer is served, a `Bind` with an unreadable pid fails closed, the SDK pid is the CLI itself (an exec keeps it).

## The "must be resolved before `GateReady`" checklist (from PR 3)

Each item needs code and a test, or a recorded proof that the address is unreachable; the PR body links each. They are the reason this is an L.

- [ ] **Other listeners of the daemon process.** Design: one registry, `listeners.Register(name, addr)`, used by every listener the daemon opens (web/ws server, any debug or pprof server, gRPC-web, the OAuth callback listener, the desktop proxy). The browser's deny list is built from it for every session start, AND the proxy consults the live registry on every request for dynamic listeners (an OAuth callback listener is up only during a login). Test: a listener registered after a session started is refused at once by that session.
- [ ] **The relay and gRPC-web local ports** (flags of other `smind` commands, separate processes). Design: every smind listener, in any process, writes a small file `<home>/listeners/<pid>.json` (name, address, pid) and removes it on exit; the deny list reads the directory and ignores files whose pid is dead. Test: a second process's listener is refused; a stale file is ignored.
- [ ] **The ephemeral loopback OAuth callback listener** (`internal/accounts/oauth_server.go`, live only during a login): registers through the same two mechanisms; test: during a fake login the callback port is refused, after it the port is not on the list.
- [ ] **Fleet-linked daemons.** The addresses and relay hosts of every linked daemon (from the host store) are denied; a loopback tunnel to a linked daemon is covered by the listener files of the tunnelling process. Test: a linked host's address and a tunnel port are refused even when the human allowlisted the IP literal (the deny list beats the allowlist).
- [ ] **The desktop proxy origin** (arrives with the desktop host; denied through the registry the moment the desktop registers it).
- [ ] **Real daemon end to end** (below).
- [ ] **Linux CI of the peer credentials.** The main CI job runs `go test ./...` on ubuntu; record in the PR body which job ran `internal/browser/runsock` (the unit tests plus the helper-process trees against real `SO_PEERCRED` and `/proc`), with the run link. If it is not covered (a build constraint, a skip), add it. The Linux code has so far only been compile-checked locally.
- [ ] **Windows**: per decision 2; the Settings text and the docs say the browser is not offered on Windows; `TestWindowsHubIsSwitchedOff` stays.
- [ ] The Claude CLI path: per decision 1; the Settings/provider text matches the outcome.
- [ ] `public-docs` page for the feature (the setting, the allowlist, the permission cards, what a page can and cannot do, the Windows and Claude statements), with the repo's forbidden-string rules respected.

## Acceptance criteria

- P4-AC1 `browserHost.callRun` consults the gate before `broker.Call` for every tool; an unknown tool never raises a card.
- P4-AC2 Reads: with no grant, the first read raises one read-grant card; "Allow reads for this run" admits all five read tools for the rest of that run only; "Allow once" admits that one call; "Deny" returns a tool error.
- P4-AC3 `browser_open` asks every call unless the run has an open grant; the card offers the per-run grant; the grant also covers `browser_close`; no other acting tool is ever grantable.
- P4-AC4 A URL the workspace policy refuses is refused with the policy's reason and raises no card.
- P4-AC5 Human policy rules: `deny` refuses without a card, `allow` admits that tool, absent or `ask` raises a card; the default is ask.
- P4-AC6 `AutoAccept`, bypass modes and auto-accepting profiles do not satisfy the gate; a run with nobody answering gets a refusal after the timeout (cap 120 s).
- P4-AC7 Only a human can answer the card (the existing human-only respond path); an agent cannot approve its own card (a run-scoped caller of the respond RPC is refused, tested for these cards).
- P4-AC8 Grants live in memory, per run, and end with the run, with the workspace switch turning off and with the kill switch; no grant is persisted; a second run of the same task starts with none.
- P4-AC9 Three denials in a minute stop cards for five minutes (refused at once, with the "do not ask again for now" text); a later answer after the window works.
- P4-AC10 Every grant and every held-then-answered call is a run-timeline line without page content; the card's URL carries no query or userinfo.
- P4-AC11 The held call survives the child's deadline: the child's per-call deadline is 150 s; a call held for 100 s then approved succeeds; one held past 120 s is refused cleanly.
- P4-AC12 `GateReady` is true, `TestGateReadyRequiresBothPrerequisites` passes, and with the workspace switch OFF (the default) no run gets a row and `callRun` refuses.
- P4-AC13 Each deny-list item of the checklist is refused by a test; the listener registry and listener files exist and are used by every smind listener.
- P4-AC14 The real-daemon end-to-end below passes and its evidence is stored.
- P4-AC15 Frames: the four card states (read grant, open, denied, timed out) and the inbox row, light and dark, at 1280 and 390, reviewed in pairs (the UI rule).

## Named tests

`runs`/`wsapi` gate: `TestGate_ReadGrantCoversFiveReadToolsForThatRunOnly`, `TestGate_AllowOnceAdmitsOneCall`, `TestGate_OpenAsksEveryCallUnlessGranted`, `TestGate_OpenGrantCoversCloseNotAnythingElse`, `TestGate_UnknownToolRaisesNoCard`, `TestGate_RefusedURLRaisesNoCard`, `TestGate_PolicyDenyAllowAskRules`, `TestGate_AutoAcceptAndBypassDoNotSatisfyIt`, `TestGate_NobodyAnswersRefusesAfterTheCap`, `TestGate_AgentCannotApproveItsOwnCard`, `TestGate_GrantsDieWithTheRunTheSwitchAndTheKillSwitch`, `TestGate_GrantIsNotPersistedAcrossRuns`, `TestGate_ThreeDenialsStopCardsForFiveMinutes`, `TestGate_OneCardAtATimePerRun`, `TestGate_TimelineLinesCarryNoPageContent`, `TestGate_HeldCallSurvivesTheChildDeadline` (child 150 s vs hold), `TestGate_CardsCarryNoQueryOrUserinfo`.
`browser`: `TestGateIsOpenOnlyWithBothPrerequisitesAndTheGate`, `TestGateReadyRequiresBothPrerequisites`.
Deny-list items: `TestListenerRegistry_RegisterDeregisterAndLiveDeny`, `TestListenerFiles_OtherProcessAndStaleFile`, `TestDeny_OAuthCallbackOnlyDuringALogin`, `TestDeny_LinkedHostsBeatTheAllowlist`, `TestDeny_ListenerRegisteredAfterSessionStartIsRefusedAtOnce`.
Claude path (if decision 1 is (a)): `TestHub_OpenPendingRefusesBeforeBind`, `TestHub_BindThenServes`, `TestHub_BindWithUnreadablePidFailsClosed`, `TestClaudePathDeliversTheRowWithAPendingSocket`.
UI/frames: the four states in `web` (light/dark, 1280/390), and a Go test of the card detail JSON.
End to end: below.

## Real-daemon end to end (acceptance, run by me on an isolated daemon, stored under `~/.smind/dogfood/browser-e2e/`)

Isolated `SMIND_HOME`, isolated port (never :4648, never the real database or config). Layer A (CI-able): extend the scripted fake ACP agent with a scenario that starts the browser row the daemon delivered (`smind mcp browser`, as its own child) and calls tools, against a loopback page served by the test: no card -> the call is held; answering the card through the real human-only RPC -> the call completes; a read grant lets later reads through; `browser_open` asks every call; a refused URL raises nothing. Layer B (needs the Human's go): one live run with the cheapest model of a provider that can use the browser (ACP; claude-native if decision 1 is (a)), against a loopback page, on the isolated daemon, with a real headless Chrome; screenshots of the cards on the web UI; cleanup of every process and profile directory; no non-loopback URL.

## Mutations (file, change, the test that must then fail)

Gate skipped for reads; gate skipped for `browser_open`; unknown tool raises a card; read grant admits `browser_open`; open grant admits an unknown acting tool; the grant keyed by task instead of run; the grant surviving `OnRunFinished`; grants persisted; `AutoAccept` satisfying the gate; policy `deny` ignored; policy default `allow`; a refused URL raising a card; an agent-scoped respond accepted; the timeout cap removed; three denials not counted; cards carrying the query; the child deadline back to 2 minutes; `GateReady` true with a prerequisite false; the listener registry not consulted per request; a stale listener file honoured; linked hosts not denied; the OAuth listener never deregistered.

## Staffing

Lead (me): the gate, grant store, decision order, timeouts, the socket/child deadline, the flip, the listener registry wiring, reviews. GLM peers (step-by-step briefs): the card detail builder and its tests (pure functions over a struct), the listener-file reader/writer package with fake-filesystem tests, and the per-listener registration call sites with a test each; peer-sonnet (UI) for the four card frames only if a card renderer needs a change. The Claude path (if chosen) is mine because it touches the hub and the runner start order.

## Not in PR 4

The Settings UI (web PR B3), the desktop host and its indicator and stop control, the provider table text (PR 5, with decision 1's outcome), acting tools beyond `browser_open`/`browser_close`, a persistent profile, Windows (unless decision 2 is (b), then its own PR), any "always allow" scope.

## Progress

- [x] decisions 1 and 2 answered (Human, 2026-11-16)
- [x] deny-list items built and tested (listener files, live deny, fleet links)
- [x] gate, grants, cards, timeouts
- [x] Claude path (pending socket + bind), active once smind pins SDK v0.4.3
- [x] flip
- [ ] frames, real-daemon live run (see the progress section)

## PR 4: progress, deviations and validation

Built as specified, with these deviations decided while building:

- **The gate lives in `runs.Registry.BrowserGate`** (called by `browserHost.callRun` through a hook) and reuses the run's ordinary permission machinery (`runPermissionDecider`): the card is a normal pending request (inbox, web, desktop and mobile cards, `run.respondPermission`, human-only). Three flags on the decider keep it from becoming a loophole: the AutoAccept shortcut is skipped, the policy engine is not asked again (the gate asked it with the right tool name first), and no saved "always" grant is offered. A new `PermissionDetail.Kind = "browser"` with a `Browser` block carries the structured card; its text comes from `internal/browsercard` (a peer wrote it).
- **Claude path:** `BrowserPolicy.OpenPending` opens the socket before the CLI exists; `bind(pid)` runs right after the client starts. `Runner.claudeReportsPID` is a reflection check of the SDK's `Client` for a `PID` method, so smind builds against an SDK with or without it and the claude browser turns on exactly when the pinned SDK can report the pid. With SDK v0.4.2 (the current pin) the claude path still gets no browser; bumping to v0.4.3 once it is tagged activates it (a one-line `go.mod` change; the SDK PR is a separate PR in that repo and is not merged or tagged by this lane).
- **The URL pre-check** for `browser_open` uses the workspace policy with the live deny list and refuses with the policy's reason and no card; a name that needs DNS resolution is checked by the proxy at connect time as before.
- **Listener files** (`internal/listeners`, a peer wrote it): `serve.go` registers each daemon listener, the OAuth callback registers for the life of a login, `smind relay` registers its native and gRPC-web listeners. The desktop proxy origin is not registered (the desktop host does not exist in this lane). Test packages that start such listeners pin `SMIND_HOME` to a temp directory.
- **Not done:** the four card states' light/dark frames at 1280 and 390 (the cards use the existing generic permission card renderer, which shows the summary and the options; a richer renderer for `Kind: "browser"` is future UI work), and the live real-provider run (Layer B), which needs the Human's go. Layer A is covered by `TestBrowserE2E_AHeldCallIsAnsweredThroughTheRealRegistry` (real registry, real hub and kernel credentials, real broker without Chrome) and the unit tests below.

Validation: gate (`internal/runs/browser_gate_test.go`: read grant covers five tools for one run only, allow once, open asks every call and its grant covers close only, unknown tool raises no card, policy deny/allow/ask, AutoAccept does not satisfy it, no answer refuses after the cap and is not a denial, grants die with the run/switch/kill switch, three denials pause cards, one card at a time, no query or userinfo in a card, no saved always option, the grant on the timeline); host (`internal/wsapi/browser_gate_test.go`: every call asks the gate first, a refusal never reaches the broker, a refused URL raises no card, live deny and fleet links beat the allowlist and a late listener is refused at once, switches drop grants, a finished run forgets them, the child deadline outlasts a held call, only a human answers a card, the end to end); listener files; Claude path (`internal/taskrunner/claude_browser_test.go`); the hub's `OpenPending`/`bind`. Not verified: Windows (not offered), the live run, the frames, the SDK v0.4.3 path against a real `claude` binary (no SDK with `PID` is pinned yet).

Review-round fixes (two fresh-context sub-agents): a held card's answer taken across a revoke is void (a per-run epoch bumped by every revoke; `allow_run` can no longer resurrect a grant), and `callRun` re-reads the kill switch and the workspace switch after the gate returns; a queued call leaves when its context ends and at most four calls queue behind a run's card; an unanswered card pauses new cards for that run for 30 s (timeouts are not denials, but nobody is there); a call for a run that has ended raises no card; listener files are read only if they are regular files (a FIFO planted in the directory blocked the reader) and never under the source's mutex; the relay records its listeners only when `Config.ListenersDir` is set (tests and harnesses leave it empty, so they never write into a real home); card text drops bidi/format characters, neutralises double quotes in the task title and workspace, caps the variable text and always ends with the "(daemon check)" marker; `allow once` and a refusal are timeline lines too; the held-call test is behavioural (a call held for a second succeeds under the default deadline and is cut by a shorter one) and the only-a-human-answers test uses a real card and an identified connection.

Stated residuals: the listener files are not authenticated (a same-user process can delete the real daemon's or the relay's file, which lifts the live deny for that port; the static daemon deny list is unaffected); a fleet-linked daemon whose address is a DNS name is denied by name only, not by every address it resolves to; grants are not dropped when only the run's socket closes (they end with the run, the workspace switch or the kill switch); a tool added to the broker without a class would be admitted by the gate (the broker's own catalogue test now fails the build first: `TestGate_EveryBrokerToolIsClassified`); with SDK v0.4.2 pinned, claude-native runs still get no browser.
