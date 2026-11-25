# Browser for agents, PR 6: grants end with the run's socket; fleet peers named by DNS are denied by address

Governing: ADR-0061 sections 5 and 10 and the two "observed items from the reviews" in `desktop-browser-for-agents.md` ("State at stop"). HIGH-RISK (the deny list and the permission grants): foreground two-reviewer pass (standards, attack), then the Steward's own, mutations on committed files. Size: M (5).

## Why

- (a) A run's read and open grants live in `runs.Registry` until the run finishes, the workspace switch turns off or the kill switch turns on. When ONLY the run's socket closes (the MCP child exits, or the socket is revoked) the grants stay, and a card held for that run can still be answered into a grant afterwards. The grants must end with the socket, like they end with the run.
- (b) The live deny list holds the host the stored fleet link names. A link addressed by DNS name (`peer.example`) is denied by that name only: a page that reaches the same peer by its IP address, or by another name that resolves to it, is not matched. The named host's addresses must be on the deny list too.

## Acceptance criteria

- AC1 Closing a run's browser socket (`browserHost.CloseBrowserSocket`, the single path used by run end and by revoke) drops that run's read and open grants and voids every card answer that was taken before the close: an answer applied after the close leaves no grant.
- AC2 A call held on a card for that run when its socket closes ends (its context ends with the socket) and the card does not stay pending in the inbox; no grant results even if the person answers late.
- AC3 The denial limiter, `uses` and run state are NOT deleted by a socket close alone (only run end forgets them): a socket that is closed and re-created for the same run id does not give the agent a fresh denial budget.
- AC4 Every fleet link whose transport host is a DNS name adds, besides the name entry, one entry per resolved address (same port). IP-literal hosts are not resolved.
- AC5 Resolution failure: when a name has never resolved successfully and the lookup fails or times out, the live deny source returns an error (the browser fails CLOSED, as for an unreadable listener directory). When a name resolved before, the last good answer is served together with the name; a failure never drops entries.
- AC6 No rebinding hole: the name is re-resolved at every live-deny refresh (the refresh is already cached for 500 ms), and the previous good answer is kept for one more refresh, so an address the name moved away from is still denied once more.
- AC7 The call path is never blocked by a hung resolver: lookups run in the background, bounded by a timeout (2 s); a refresh waits at most 250 ms for them and then serves the last good answer plus the name (or the error of AC5). Concurrent refreshes share one lookup per name.
- AC8 Nothing is added to the wire, RPC or store.

## Named tests

- `internal/runs/browser_gate_test.go`: `TestDropBrowserRunGrantsEndsGrantsAndVoidsAnswers`, `TestDropBrowserRunKeepsTheDenialLimiter`.
- `internal/wsapi/browser_call_test.go`: `TestClosingTheSocketDropsTheRunsGrants` (real hub, a grant given, the socket closed, the gate asks again), `TestClosingTheSocketEndsAHeldCard`.
- `internal/wsapi/browser_resolve_test.go`: `TestResolverAddsResolvedAddressesAndKeepsTheName`, `TestResolverIPLiteralIsNotResolved`, `TestResolverFailureWithoutAGoodAnswerFailsClosed`, `TestResolverFailureServesTheLastGoodAnswer`, `TestResolverReresolvesEveryRefreshAndKeepsThePreviousAnswerOnce`, `TestResolverHungLookupDoesNotBlockTheCallPath`, `TestResolverSharesOneLookupPerName`.
- `internal/wsapi/browser_deny_test.go`: `TestFleetPeerByNameIsDeniedByAddress` (end to end through the policy: a URL with the peer's IP is refused).

## Mutations (committed files, each must fail a named test)

- The grant drop removed from `CloseBrowserSocket` -> `TestClosingTheSocketDropsTheRunsGrants`.
- The epoch not bumped by the drop -> `TestDropBrowserRunGrantsEndsGrantsAndVoidsAnswers`.
- The drop deleting the run state (limiter) -> `TestDropBrowserRunKeepsTheDenialLimiter`.
- The resolved addresses not added -> `TestFleetPeerByNameIsDeniedByAddress`.
- The name entry dropped -> `TestResolverAddsResolvedAddressesAndKeepsTheName`.
- A failed first lookup serving "no entries" -> `TestResolverFailureWithoutAGoodAnswerFailsClosed`.
- A failed lookup dropping the last good answer -> `TestResolverFailureServesTheLastGoodAnswer`.
- The previous answer not kept -> `TestResolverReresolvesEveryRefreshAndKeepsThePreviousAnswerOnce`.
- The refresh waiting for the lookup without a bound -> `TestResolverHungLookupDoesNotBlockTheCallPath`.

## Decisions

1. The socket close drops grants and voids answers but keeps the run's denial limiter and use count (AC3): only run end forgets them.
2. Resolution lives in a small helper in `internal/wsapi` next to the deny source (`browser_resolve.go`), with an injectable lookup function for tests; it uses the system resolver (`net.DefaultResolver.LookupHost`).
3. The "short TTL" is the existing 500 ms live-deny cache: every refresh re-resolves, nothing is cached longer than that except the last good answer and the one before it.
4. A name that has never resolved fails the whole live deny read (fail closed): a linked peer whose DNS is broken makes the browser refuse until it resolves. This is the Steward's stated rule; the cost is stated in the PR.

## Not in this PR

PR 5 follow-ups, Settings UI, persistent profiles, a live-model run, the desktop host, Windows.

## Evidence recorded here

Linux CI evidence for the peer-credential path (item 2 of the resumed lane, no code change): the `ubuntu-latest` run https://github.com/spacingmind/smind/actions/runs/45555292074 (`go test -v -count=1 ./internal/browser/runsock/`) shows 42 distinct passing tests, 0 skipped, 0 failed, among them `TestSystemPeerReaderReportsThisProcess` (the real `SO_PEERCRED` reader), `TestSystemProcTreeChild`, `TestSystemProcTreeNotUnderChild`, `TestProcfsTreeFixture`, `TestParseProcStat`, `TestServerRefusesOutsideTheTree`, `TestServerRefusesAnExitedPeer` and `TestHub_BoundPendingSocketRefusesOutsideTheTree`. The regular `ci` job runs `task test` (`go test ./...`, not verbose), so its log shows only the package line.

## Progress

- [ ] Spec committed (this file)
- [ ] (a) grants dropped with the socket
- [ ] (b) resolver and deny source
- [ ] Reviews, mutations, full test run

## Validation

(filled in when the work is done)
