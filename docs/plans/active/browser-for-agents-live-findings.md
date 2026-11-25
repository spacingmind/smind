# Browser for agents: the live-run findings

Source: the live run's findings 1, 2, 4, 5, 7 and 9 (`browser-for-agents-live-run.md`, on its own PR). Finding 3 (the double prompt: the CLI's own prompt and the daemon's card for every call) is NOT here: it waits on a decision. Finding 6 (cost reporting) and 8 (the GLM timeline icon) are not here either.

## Acceptance criteria

### A. The permission card (web, and the shared detail on mobile)

- AC1 Every long option label WRAPS in the shared permission option layout (`options-card`): at 390 px nothing is cut off, at 1280 px nothing runs past the card's border. The fix is in the shared button class for permission options, so every card with a long label benefits, not only the browser's. A layout test pins the classes (no `whitespace-nowrap`, a minimum height instead of a fixed one, `max-w-full`) and e2e frames show a long option at both widths.
- AC2 The close card says what its grant covers in its own words: "Allow closing tabs, and opening any page the browser policy allows (localhost and your added hosts), for this run" (the grant is the run's open grant, which covers `browser_open` and `browser_close`, so the label states both). The open card keeps its breadth wording. The label is the daemon's (`internal/browsercard`), the web and mobile cards only show it.
- AC3 On a browser security card "Allow once" is NOT drawn as the green recommended action: none of the options is recommended. The rule is the card's kind (`detail.kind === "browser"`: the daemon sends no recommended flag on the wire for any card, "first allow is recommended" is a client rule), so every other card keeps its recommended styling. Mobile: `permissionButtonVariants` takes the detail kind and returns no filled primary for a browser card.
- AC4 Mobile: a long option label is checked for the same overflow; the options row gets a full-width, wrapping layout for long labels (a pure helper decides what is long), with unit tests and `tsc`. NOT verified on a device or simulator (no native build in this item).

### B. The daemon

- AC5 (finding 4) The browser's own background requests (the Google sign-in cookie query, the search pre-connect) never produce a `blocked:` notice for the agent. Two parts: (i) the launch flag measured on the installed Chrome (158): `PreconnectToSearch` added to the single `--disable-features` list removes the `www.google.com` pre-connect; the other flags this finding suggests (`--disable-background-networking`, `--disable-component-update`, `--disable-sync`, `--disable-default-apps`, `--no-first-run`) are already set; a Gaia `ListAccounts` request to `accounts.google.com` remained under every flag tried (about forty candidate features and switches, measured with the real Chrome). (ii) The rule for what remains: a proxy refusal becomes a notice only when a PAGE named the same target (host and port), as the DevTools protocol reports it: a `Fetch.requestPaused` URL, a `Network.requestWillBeSent` URL or a `Network.webSocketCreated` URL (a WebSocket reaches the proxy and nothing else, so it must stay visible). A refusal with no page request is counted (`Session.Unattributed`), not reported. The CDP event can trail the proxy's refusal, so a refusal waits 400 ms for it. `Network.enable` runs on every target before it is resumed.
- AC6 (finding 5) The snapshot never prints `InlineTextBox` or `LineBreak` nodes (named or not) and never counts them: the 1,200-node and 40,000-character budgets are spent on what remains. A tree that is half InlineTextBox is not truncated.
- AC7 (finding 9) The agent never reads the package's own `browser: ` label inside a refusal: "browser_open: address X is not allowed (only loopback and hosts the person added)", not "browser_open: browser: address X ...". Every `errResult` that wraps an error in the broker, and the MCP child's forwarding of a daemon refusal, drop one leading `browser: `; reasons are unchanged.

### C. Evidence

- AC8 Light and dark frames at 1280 and 390 from the real web UI of an isolated daemon with the fake agent: the open card, the close card, the read card and a long-option case, with a contact sheet and a README; every pair opened by hand.
- AC9 The real-Chrome smoke (`SMIND_BROWSER_SMOKE=1`) asserts no notice names `google.com` after a navigation, and logs how many refusals were kept from the agent.

## Named tests

Web: `permission-card.test.tsx` (the wrapping classes, no recommended action for a browser card, the recommended action elsewhere), `web/e2e/specs/browser-cards.spec.ts` (e2e: the open, close, read and long-option cards stay inside the card at both widths, none is the approval action). Mobile: `permissionButtons.test.ts` (browser card: no filled primary), a long-label helper test.
Go: `internal/browsercard` (the close card's grant wording), `internal/browser` `TestProxyCredentialNoticeIsNeverFiltered`, `TestRepeatedUnattributedRefusalsArmOneTimer`, `TestPageTargetsEvictOldestOneAtATime`, `TestSnapshotLongStaticTextIsReadableToItsEnd`, `TestHostPortOf`, `TestRefusalNobodyAskedForIsCountedNotReported`, `TestRefusalOfATargetAPageRequestedIsReportedOnce`, `TestRefusalWaitsForThePageRequestThatNamesIt`, `TestFetchRequestNamesItsTarget`, `TestNetworkIsEnabledBeforeATargetRuns`, `TestBuildArgsSilencesBackgroundNetworking`, `TestSnapshotDropsRenderOnlyNodesAndCountsWhatRemains`, `TestSnapshotBudgetIsNotSpentOnRenderOnlyNodes`, `TestAgentSeesNoRepeatedBrowserLabelInARefusal`; `cmd/smind` `TestBrowserForwardDropsTheDaemonsBrowserLabel`.

## Mutations (committed files, each must fail a named test)

`PreconnectToSearch` removed from the flags; the page-target check removed (every refusal reported); the WebSocket event not recorded; the Fetch URL not recorded; `Network.enable` removed; `InlineTextBox` printed again; render-only nodes counted; `AgentReason` not applied; the child not stripping; the close card's label unchanged; the recommended action given back to a browser card; the wrapping classes removed.

## Decisions

1. The recommended action is a client rule today (the first allow option); the browser kind switches it off rather than adding a wire field now (a daemon flag per option is a wider change for one card kind).
2. The page-initiator rule lives in the session, keyed on host and port; a refusal for a target no page named is counted, not logged per line, so a flood of browser-internal requests costs nothing.
3. `Network.enable` is best effort: a target that cannot enable it still starts, its refusals are then unattributed (the agent loses a notice, the policy still refuses the request).
4. What the page-initiator rule does not report (stated in `pageinit.go`): connection hints with no request (`<link rel=preconnect>`, DNS prefetch), a service worker's own script fetch, and a page that names its target only after the 400 ms grace: those refusals are counted, not reported, and the proxy refuses them all the same. A refusal that is not a request target at all (the proxy's own "wrong credential" notice) is never filtered. A page can unlock the notice for a host it requested itself (it named it): accepted, it is noise only for that target.
5. The agent-facing label strip is done where the text is built (broker) and where a daemon refusal is forwarded (child), not by editing every error constant: the `browser: ` label stays in logs and in the CLI.

## Not in this PR

Finding 3 (the double prompt), 6 (cost reporting), 8 (the GLM timeline icon); an on-device mobile check; a Gaia flag (none found).

## Progress

- [x] Spec committed (this file)
- [x] Daemon: flags, page-initiator rule, snapshot, refusal text, close-card wording
- [x] Web card: wrapping options, no recommended on browser cards
- [x] Mobile: no filled primary, long labels (not verified on a device)
- [x] Tests, full e2e matrix, real-Chrome smoke
- [x] Frames, contact sheet, README, every pair opened
- [x] Reviews, mutations

## Validation

- AC1: `permission-card.test.tsx` (classes), `browser-cards.spec.ts` BC-open/close/read/long at four projects (every option inside the card, nothing clipped, no sideways scroll, the long labels take several lines). AC2: `TestCloseCardHasItsOwnWording`. AC3: `permission-card.test.tsx` (no approval variant, `recommendedOptionId` by kind, other cards unchanged), e2e (`data-variant` is not `approval`), mobile `permissionButtons.test.ts`. AC4: mobile `permissionLabels.test.ts`, 121 mobile test files (1173 tests) and `tsc` pass; **the native wrapping of a long label was NOT observed on a device or simulator**.
- AC5: measured on Chrome 158 (the installed Google Chrome, headless): the start-up pre-connect to `www.google.com` is removed by `PreconnectToSearch`; a Gaia `ListAccounts` request to `accounts.google.com` stays under about forty candidate features and switches, so the page-initiator rule carries it. The real-Chrome smoke (`SMIND_BROWSER_SMOKE=1`) passes with no `google.com` notice, every page-caused refusal (fetches, WebSockets by name and by IP literal, link-local, reserved ports) still reported, and one refusal kept from the agent. Tests: `TestHostPortOf`, `TestRefusalNobodyAskedForIsCountedNotReported`, `TestRefusalOfATargetAPageRequestedIsReportedOnce`, `TestRefusalWaitsForThePageRequestThatNamesIt`, `TestFetchRequestNamesItsTarget`, `TestNetworkIsEnabledBeforeATargetRuns`, `TestProxyCredentialNoticeIsNeverFiltered`, `TestRepeatedUnattributedRefusalsArmOneTimer`, `TestPageTargetsEvictOldestOneAtATime`, `TestBuildArgsSilencesBackgroundNetworking`.
- AC6: `TestSnapshotDropsRenderOnlyNodesAndCountsWhatRemains`, `TestSnapshotBudgetIsNotSpentOnRenderOnlyNodes`, `TestSnapshotLongStaticTextIsReadableToItsEnd`. AC7: `TestAgentSeesNoRepeatedBrowserLabelInARefusal`, `TestBrowserForwardDropsTheDaemonsBrowserLabel`. AC8: frames in `~/.smind/dogfood/browser-live-findings/` (README lists the 16 pairs opened). AC9: the smoke above.
- Reviews (two fresh sub-agents, standards+spec and attack). Accepted and fixed: the proxy's own "wrong credential" notice was filtered by the new rule (regression: now never filtered); long paragraphs lost their tail once InlineTextBox was dropped (StaticText cap 2000); one timer per refused target and none after close; oldest-first eviction of the page-target set instead of a wipe; `Network.enable` without post data; the mobile wide button also stretches; plan wording and test names corrected; the grace test proves it waited. Documented, not changed: connection hints (`<link rel=preconnect>`), a service worker's own script fetch and a late page event are counted, not reported; a page can unlock the notice for a host it requested itself.
- Mutations on committed files, each failing a named test (committed first, restored with `git checkout`, tree verified clean): `PreconnectToSearch` removed, the page-target check removed, the WebSocket event not recorded, the Fetch URL not recorded, `Network.enable` removed, InlineTextBox printed again, `AgentReason` not applied, the child not stripping, the close card keeping the open label, the recommended action given back to a browser card, the wrapping class removed, the mobile browser primary restored; and after the review round the credential notice filtered again, a timer per refusal, the set wiped when full, the StaticText cap back to 200.
- Verified: go vet, `GOOS=windows go vet ./...`, go tests for `internal/browser`, `browsercard`, `runs`, `cmd/smind`, `hygiene`, `crash`; the full web vitest suite (5736 passed) and `tsc -b`; mobile `tsc` and vitest; the docs-site tests; the full e2e matrix locally (`E2E_FULL=1 --grep-invert @flaky --workers=2`): 267 passed, 41 skipped, 0 failed, including the 16 browser-card tests.
- Not verified: the mobile layout on a device; a run with a real model (this item changes no model-facing behaviour except the refusal and snapshot text, covered by tests and the Chrome smoke).
