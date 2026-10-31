# Fleet: connected hosts (ADR-0048)

Status: active (2026-10-31). Binding text: [`docs/decisions/0048-fleet-connected-hosts.md`](../../decisions/0048-fleet-connected-hosts.md) (Accepted; D1–D7 are
the Human's answers, S1–S18 are Supervisor defaults 2026-10-31, Human may veto; a veto changes the PR named in the ADR's table).

Size: XL as one item (Scope 2, PRs 2, Risk 2 (remote authority, replay, budget), Decisions 0 (settled), Verification 2 (two real daemons, UI
screenshots) = 8) → split into the PRs below, non-stacked, each from `origin/develop` after the previous one merges (a later phase may start
only when the earlier PR is merged: they share the `internal/fleet` package). Every wsapi/store-migration/permission/relay/UI PR gets a validation
contract (ADR-0045) before code.

## Acceptance Criteria

Identity, registry, link, pairing (F1)
- AC1 With `fleet.enabled` false (default) or absent: no `host-key` is created, no `fleet.*` method is served or answered (method-not-found), no table is touched, `smind --help` still lists `host` but every `smind host` subcommand says fleet is off; a golden test over the existing wsapi method map shows no other change.
- AC2 The host key is created lazily on first enable with mode 0600 through `internal/securefile`; `hostId = base32(SHA-256(pub))[:26]` equals the D35 derivation (shared vector); an unreadable/corrupt key file refuses to start fleet (never regenerates silently).
- AC3 `smind host invite` opens a pairing window with the `devices.Window` rules (default 10 min, 1 slot, only a peer first seen at/after `OpenedAt` takes it) and prints an invite whose payload is in the URL fragment only; the invite contains no daemon password and no provider secret.
- AC4 `smind host add <invite>` pins the invite's public key, runs `fleet.hello` mutually (both signatures verified), and shows the same 6-digit SAS on both hosts; the responder records the initiator as `pending` and only a human `approve` makes the inbound record `approved`.
- AC5 **Symmetric model (D1):** one pairing creates exactly two directed links. After pairing, initiator→responder is enabled (responder holds the default forward grant), responder→initiator is **disabled** (initiator holds no inbound record that allows it). A call over the disabled direction is refused `not_granted` on the callee.
- AC6 `smind host enable-reverse <peer>` (run on the receiving host, local-only) creates the inbound record with the **default reverse grant (read-only, 0 live agents, 0 wall, $0)** and the peer's outbound record becomes usable only after the callee's `fleet.link.state` notice; `disable-reverse` closes the live connection immediately. A mesh is enable-reverse on every pair; no mesh object exists (a test walks the schema).
- AC7 Host hello is mutual and pinned: a callee presenting another key is refused by the caller with `peer_identity_changed`, never accepted silently; a caller key not approved is refused with `awaiting_approval`/`revoked`; the fresh challenge makes a recorded hello transcript useless on a new connection (replay test).
- AC8 Direct links refuse a cleartext `ws://` route to a non-loopback address outside the Tailscale ranges unless `--allow-cleartext`; relay links use E2EE v2 and fail closed on a fingerprint mismatch; both transports end in the same hello.
- AC9 A host principal can call only `fleet.*`: every other method in the wsapi handler map is refused `forbidden` for it (handler-map walk), `fleet.host.*` admin methods are local-only and in neither `RunScopedMethods` nor the plugin allowlist, and no fleet method can write a grant, link, policy or secret (type-level: the fleet param structs have no such fields; test by reflection).
- AC10 `smind host ls --json` never prints a key, secret, relay host secret or password; `fleet-secrets` is 0600 and the relay secret is redacted in logs (canary test).
- AC11 Web Settings → Hosts lists hosts with state, direction toggles, grant editor, invite/add/approve with the SAS, light+dark at 1280 and 390; the inbox shows `host_pair` / `host_down` / `host_refused` rows (S9); mobile lists hosts and approves a pending one from the inbox row (no mobile pairing).

Remote read (F2)
- AC12 `fleet.info`, `fleet.workspace.list`, `fleet.task.list|get`, `fleet.run.status`, `fleet.task.activity` work for a granted peer, return only owner-host data, never local paths, never credential-bearing URLs, assistant text ≤ 1 KiB per event, cursor-paged, and honour `workspaces.read`.
- AC13 **Provenance (D3):** every request carries `fleet.prov`; the callee verifies in order principal match, not-in-chain (cycle), `len(hops)==1` (hop limit), replay/staleness, grant + budget; each failure is a typed `fleet_refused{code}` with an audit row on the callee and on the caller.
- AC14 Audit on both ends: one `fleet_audit` row per call with `{direction, host, trace, reqId, method, outcome, code, latencyMs, bytes, summary}`; `summary` has no prompt body (SHA-256 + ≤ 120 redacted chars); 30-day retention; `smind host audit` and the Hosts page read it.
- AC15 CLI `smind host workspaces <host>` and `smind host tasks <host>`; MCP `list_hosts` and the read tools accept `host`; omitted host = local, byte-identical to today's results.
- AC16 **Version/caps (D7):** on every reconnect each side requires equal `fleetV` major and `caps` ⊇ required; a mismatch yields `fleet_version_unsupported{needs, has, host, guidance}` and the link stays `refused` (no downgrade, no local fallback, local operations unaffected); a daemon with no `fleet.*` (method-not-found) maps to the same guidance.

Remote create, send, finish (F3)
- AC17 `fleet.task.create` requires `create` and a workspace in `workspaces.write`; provider and model are **required**; the request type has no `permissionMode`, `profileId`, `env`, `cwd`, `attachments` or `mcp` field; a provider the callee lacks fails `provider_not_configured`; the branch is fetched from the callee's own origin (`branch_not_found` otherwise); no path, diff or file upload exists.
- AC18 `fleet.task.send` and `fleet.task.stop` act only on tasks this peer created (`not_owner` otherwise); prompts are text-only; `idempotencyKey` (24 h) makes an ambiguous retry return the same task/run; a different key creates another.
- AC19 Remote-created tasks have `source=remote`, `origin_host`, `origin_trace`; they run under the callee's own mode ceiling and the ADR-0047 floor in every mode; their permission requests go to the **callee's** inbox with a host chip, the caller sees only `waiting_permission` and cannot answer (no fleet method can).
- AC20 **Per-origin budget (D3):** the callee refuses with `budget_exceeded{which}` when remote-origin live agents ≥ `maxLiveAgents` or reported 24 h cost ≥ `maxCostUSD`, counted per origin host across all trace ids; a run over `maxWallSeconds` is stopped by the callee and reported `wall_limit`; nil cost is never estimated.
- AC21 **Durable finish notification (D4):** the caller writes the `fleet_watches` row and the mirror in one local commit; at restart it resumes with the cursor; a run that finished during the downtime is delivered once through the ADR-0041 B3 queue with `host=<alias>` and the remote text marked untrusted and ≤ 1 KiB; three consecutive failed observations (capped backoff, "connecting" attempts do not count) deliver "status unknown", never "finished"; a confirmed removed host delivers "host unavailable" at once; a definitely-unsent prompt creates no watch; a failed local write reports failure and the create is retry-safe.
- AC22 MCP `task_new/task_send/task_status/task_logs/task_stop/workspace_list` accept `host`; results carry `host` and `globalId` (`alias:id`); a Peer seat and a remote-origin task get no fleet tools and a fleet call from them is refused locally with `hop_limit`.

Tree across hosts, UI (F4)
- AC23 `task.tree` shows remote children under their local parent with a `remote` flag, host alias and mirrored state; usage and live-count rollups include remote reported cost and count; ADR-0041 A5's tree budget at admission counts remote spend; mirror rows are cache only (deleting them loses nothing; every mutation goes to the owner).
- AC24 Exactly one owner per task: a mutation on a mirror goes to the owner and a local mirror is never updated by anything but the owner's events; ids are `alias:id` in UI/CLI/MCP and `{hostId, id}` on the wire; an alias rename changes no stored wire id.
- AC25 Web shows a host label chip on remote nodes in tree, inbox and task header (light+dark, 1280/390, transition frames of the tree reviewed); mobile shows the chip.

Capability routing (F5)
- AC26 Hosts carry operator-set labels (`os:windows`, `arch:arm64`, `gpu:cuda`, free `k:v`, ≤ 16, validated); `host` in MCP/CLI may be `labels:{...}`; the selector must resolve to exactly one enabled, connected host unless `any:true` (then least live remote agents, ties by alias); zero matches or ambiguity are model-readable errors; there is no automatic retry on another host.

## Named test scenarios (must exist by these names)

Required by the Human:
- `TestFleetCycleRefused_AtoBtoA` — A creates a task on B; the task tries to call A: refused locally (`hop_limit`) and, with B's guard bypassed by a forged request, refused at A with `cycle`; audit rows on both.
- `TestFleetHopTwoRefused` — a request with `hops=[A,B]` (or empty hops) reaching C is refused `hop_limit` before any handler runs; a task created by A on B cannot create on C.
- `TestFleetReverseDisabledByDefault` — after one pairing A→B works and B→A is refused `not_granted`; after `enable-reverse` on A, B→A works for read ops only and a create is refused; after `disable-reverse` the live connection is closed.
- `TestFleetSecretNeverLeavesHost` — a canary secret in A's provider env, secret store, config and relay secret; run every fleet method both ways with capture of every frame and B's db/log dir; the canary appears nowhere; B's `provider_list` equivalent returns ids and models only.
- `TestFleetCallerRestartStillGetsFinishNotification` — A creates and prompts on B, A's daemon restarts, B's run finishes while A is down, A restarts: exactly one `child_event` arrives with the remote result; and the three-failed-observations case ends in "status unknown".
- `TestFleetVersionSkewRefused` — `fleetV` or caps mismatch on first connect and on a later reconnect (peer downgraded): `fleet_version_unsupported` with guidance, no call admitted, local ops still work.

Additional:
- `TestFleetOffIsByteForByte`, `TestHostIDMatchesDeviceIDVector`, `TestHostKeyFileModeAndCorruption`, `TestInvitePayloadFragmentOnlyNoSecrets`, `TestPairingWindowOneSlotFirstSeenAfterOpen`, `TestPairingSASMatchesBothSides`, `TestPairingCreatesTwoDirectedLinksOneEnabled`, `TestHelloMutualPinnedIdentity`, `TestHelloReplayOnNewConnectionRefused`, `TestHelloUnapprovedRefused`, `TestDirectCleartextRefusedOutsideOverlay`, `TestRelayFingerprintMismatchFailsClosed`, `TestHostPrincipalOnlyFleetMethods`, `TestFleetAdminMethodsLocalOnlyNotRunScopedNotPlugin`, `TestFleetParamsHaveNoGrantOrSecretFields`, `TestHostLsNeverPrintsSecrets`.
- `TestFleetReadReturnsNoPathsNoCredentialURLs`, `TestFleetActivityCappedAndPaged`, `TestFleetProvenanceOrderOfChecks`, `TestFleetPrincipalMismatchRefused`, `TestFleetReplayAndStaleRefused`, `TestFleetAuditBothEndsNoPromptBody`, `TestFleetAuditRetention`, `TestMCPListHostsAndHostParamLocalUnchanged`.
- `TestFleetCreateRequiresExplicitProviderModel`, `TestFleetCreateTypeHasNoModeEnvCwdFields`, `TestFleetCreateBranchFromOwnOrigin`, `TestFleetCreateProviderNotConfigured`, `TestFleetSendOnlyOwnTasks`, `TestFleetIdempotencyKeyRetryReturnsSame`, `TestFleetRemoteTaskUnderCalleeFloorAndMode`, `TestFleetRemotePermissionGoesToCalleeInbox`, `TestFleetBudgetPerOriginAcrossTraces`, `TestFleetWallLimitStopsRun`, `TestFleetWatchObservationBackoffAndConnectingNotCounted`, `TestFleetWatchHostRemovedUnavailable`, `TestFleetWatchUnsentPromptNoWatch`, `TestFleetLocalWriteFailureReportsAndRetrySafe`, `TestFleetPeerSeatGetsNoFleetTools`.
- `TestTaskTreeShowsRemoteChildrenAndRollups`, `TestTreeBudgetCountsRemoteSpend`, `TestMirrorRowsAreCacheOnly`, `TestAliasRenameKeepsWireIds`, `TestOwnerHostIsSingle`.
- `TestSelectorExactlyOneOrError`, `TestSelectorAnyLeastLoadedTieByAlias`, `TestSelectorNoSilentRetryOnOtherHost`, `TestLabelsValidation`.
- UI (Vitest + frames): Hosts page states, grant editor required-workspace rule, SAS display, inbox rows, chip in tree/inbox/task header.

## Phases and PRs (non-stacked; each from `origin/develop`; one owner per write scope)

Each phase lists wire/schema changes, docs to update and the disjoint write scope. `public-docs/` must not name ADRs or PRs (docs drift test).

### F1: host registry, daemon-as-client link, `smind host add|ls|rm|enable-reverse` (AC1–AC11)

- **F1a: identity and store.** Write scope: new `internal/fleet/identity` (host key, hostId), `internal/fleet/registry` (hosts, directed links, grants, validation, defaults), `internal/store` migration + queries, `internal/config` (`fleet.*`). Schema: tables `fleet_hosts(host_id PK, alias UNIQUE, pub, label, state, created_at)`, `fleet_links(host_id, direction, enabled, grant_json, transport_json, updated_at, PK(host_id,direction))`, `fleet_audit` (shape in the ADR, used from F2), `fleet_watches`, `fleet_idem`, `fleet_remote_tasks`, `tasks.origin_host`, `tasks.origin_trace`, `runs.origin_host` (create all migrations now so F2/F3 do not reopen `internal/store`). Docs: `docs/ARCHITECTURE.md` (host key, tables). Named tests: `TestFleetOffIsByteForByte`, `TestHostIDMatchesDeviceIDVector`, `TestHostKeyFileModeAndCorruption`, grant validation, default grants (S5).
- **F1b: hello, transports, pairing, CLI.** Write scope: `internal/fleet/hello`, `internal/fleet/link` (caller-side manager using `internal/wsclient` and `internal/relay/client`), `internal/fleet/pairing` (invite encode using `internal/relay/pairing`, SAS), `internal/wsapi/fleet_host.go` (principal, hello, local-only admin methods, host-principal allowlist), `cmd/smind/host.go` (+ `main.go` dispatch line). Wire: `fleet.challenge`, `fleet.hello`, `fleet.hello.ok`, `fleet.pair.request`, `fleet.link.state`, `fleet.host.*` admin. Docs: `public-docs/hosts.md` (new page, no ADR numbers), `public-docs/cli-reference.md` via `task docs:cli`, `docs/decisions/0031` pointer line. Named tests: pairing/hello/transport tests above, `TestFleetReverseDisabledByDefault` (first half), `TestHostPrincipalOnlyFleetMethods`, `TestFleetAdminMethodsLocalOnlyNotRunScopedNotPlugin`.
- **F1c: UI and inbox.** Write scope: `web/packages/ui` (Hosts settings, inbox rows), `internal/inbox` (derived kinds), `mobile/` (list + approve). Light+dark at 1280 and 390, every frame looked at. Docs: `public-docs/hosts.md` UI section, `public-docs/inbox.md`.

### F2: remote read (AC12–AC16)

- **F2a: callee handlers, provenance, audit.** Write scope: `internal/fleet/serve` (read handlers, provenance checks, grant enforcement, audit writer), `internal/wsapi/fleet_read.go`. Wire: `fleet.info`, `fleet.workspace.list`, `fleet.task.list|get`, `fleet.run.status`, `fleet.task.activity`, `fleet.prov`. Named tests: provenance, replay, audit, read-shape tests, `TestFleetCycleRefused_AtoBtoA` (refusal at A half), `TestFleetHopTwoRefused`, `TestFleetVersionSkewRefused`.
- **F2b: caller client, CLI, MCP read.** Write scope: `internal/fleet/client` (typed calls over the link), `cmd/smind/host_read.go`, `cmd/smind/mcp_hosts.go` (`list_hosts`, `host` on read tools). Docs: `public-docs/hosts.md`, `public-docs/orchestration.md` (read-only host section), `cmd/smind/mcp_instructions.go` text.

### F3: remote create, prompt, finish notifications (AC17–AC22)

- **F3a: callee write path and budgets.** Write scope: `internal/fleet/serve` (create/send/stop, idempotency, per-origin budget, wall-limit), `internal/runs` (admit remote-origin source, mode ceiling, floor in all modes: touch only the admission hook), `internal/wsapi/fleet_write.go`. Wire: `fleet.task.create`, `fleet.task.send`, `fleet.task.stop`, `fleet.run.wait` (stream, cursor). Named tests: the create/send/budget/floor/permission tests, `TestFleetSecretNeverLeavesHost`.
- **F3b: caller watches and MCP write tools.** Write scope: `internal/fleet/watch` (durable watcher, backoff, notification through `internal/runs` B3 hook), `cmd/smind/mcp_hosts_write.go`. Named tests: `TestFleetCallerRestartStillGetsFinishNotification` and the watch tests. Docs: `public-docs/orchestration.md` (remote delegation, finish notices, status unknown), `docs/WORKSPACE_PROTOCOL.md` (a Lead may delegate to a remote Peer; one owner per scope across hosts; the remote Peer's branch rules).

### F4: the Lead/Peer tree across hosts, host label in UI (AC23–AC25)

Write scope: `internal/tasktree` (remote children, rollups), `internal/fleet/mirror`, `internal/runs` budget rollup hook only, `web/packages/ui` (chip), `mobile/` (chip). Wire: `task.tree` nodes gain `host`, `remote`, `globalId`. Docs: `public-docs/orchestration.md`, ADR-0041 amendment pointer.

### F5: capability routing by host labels (AC26)

Write scope: `internal/fleet/selector`, label fields in `internal/fleet/registry` (one column, migration in this PR only), `cmd/smind` flags, MCP `host` union type. Docs: `public-docs/hosts.md` labels section.

## Peer plan (GLM policy)

Design and ADR text: the Lead, no Peer. Implementation PRs: peer-glm (profile 8) with step briefs (one coherent step, ≤ 15 min, exact files, named tests, "commit after each step, do not push, no `gh pr create`, no `git stash`, no installs, touch only the named files"), at most two in parallel with disjoint scopes (for example F1b `internal/fleet/hello` + `cmd/smind/host.go` against F1c UI after F1a merges). The Lead reviews every diff line, runs the named tests and one mutation per load-bearing test, and writes code only for a review finding GLM failed to fix twice. Screenshot loops: peer-sonnet (profile 6).

Planned mutations (file + change → test that must then fail): hop check `len(hops)==1` → `<=2` → `TestFleetHopTwoRefused`; drop the not-in-chain test → `TestFleetCycleRefused_AtoBtoA`; reverse inbound default `enabled:true` → `TestFleetReverseDisabledByDefault`; add an env field to the create params → `TestFleetParamsHaveNoGrantOrSecretFields`/`TestFleetSecretNeverLeavesHost`; delete the watch row at shutdown → `TestFleetCallerRestartStillGetsFinishNotification`; skip the caps check on reconnect → `TestFleetVersionSkewRefused`; count budget per trace instead of per origin → `TestFleetBudgetPerOriginAcrossTraces`; accept a replayed `reqId` → `TestFleetReplayAndStaleRefused`.

## Decisions

Settled: ADR-0048 D1–D7 (Human) and S1–S18 (Supervisor defaults, Human may veto). Open for later ADRs, not this plan: multiplexed channels with terminal/file transfer, SSH transport, signed per-hop chains and transitive routing, hosted relay.

## Progress

- 2026-10-31: ADR-0048 and this plan written (docs-only PR). No code started. Peer ledger for this item: peer-glm 0 runs, peer-sonnet 0 runs (design and docs written by the Lead).
- 2026-10-31 F1a (identity and store), contract `fleet-hosts-f1a.contract.md`. peer-glm runs 1-2 (store; identity+config) failed at once with `429 Usage limit reached for 5 hour`, reset 14:20:58 +07; peer-glm waits (no Sonnet fallback). After the 14:21 reset the daemon's rate-limit failover re-ran the store run and it finished; identity+config, registry and two review-fix runs followed. Peer ledger F1a: peer-glm 5 runs ok (store 1 [+1 duplicate run I stopped], identity+config 1, registry 1, two fixes 2), 1 run lost to the 429 (resets 14:20:58 +07), peer-sonnet 0. Review fixes GLM made in one pass: revoke ordering, rollback test, foreign link host.

## Validation

Docs-only PR: markdown links and the ADR/plan cross-references checked by hand; `task lint` run. Implementation PRs fill this section per AC with evidence (test names, mutation results, frames) as they land; the plan moves to `docs/plans/completed/` only when every AC is confirmed here.

### F1a (identity and store) validation, 2026-10-31

- AC1 (off): `TestFleetOffIsByteForByte`, `TestFleet_DisabledByDefaultAndDefaults`, `TestFleet_EnvCannotEnable`, `TestFleetMigration_ExistingRowsKeepEmptyOrigin`. The wsapi golden over the method map and the `smind host` CLI half are F1b (no wsapi/CLI change here).
- AC2: `TestHostIDMatchesDeviceIDVector` (shared device_hello vector), `TestHostKeyFileModeAndCorruption`, `TestHostKeyConcurrentCreateOneIdentity`.
- Schema: `TestFleetMigration_TablesAndColumns`, `TestFleetPair_*`, `TestFleetHost_NoSchemaForMesh`, audit/watch/idem/mirror/origin tests in `internal/store/fleet*_test.go`.
- Grants/registry (AC5 data model half): `TestGrantValidate_Table`, `TestGrantValidateForApproval_RequiresWriteWorkspace`, `TestDefaultForwardGrant_S5`, `TestDefaultReverseGrant_S5`, `TestRegistryPairingDirectedLinksReverseOff`, `TestRegistryResponderAcceptApprove`, `TestRegistryEnableDisableReverse`, `TestRegistryAuthorize`, `TestRegistryMaxHosts`. The wire-level `TestFleetReverseDisabledByDefault` is F1b.
- Mutations (all fail the named test): M1 hostId 25 chars; M2 regenerate on corrupt; M3 skip IsPrivate; M4 reverse grant gains create; M5 initiator inbound enabled; M6 Authorize ignores enabled; M7 LoadIdentity ignores Enabled; M8 commit on link failure (`TestFleetPair_RollsBackOnMidInsertFailure`); M9 no summary truncation; M10 no maxHosts range check. Not mutation-proven: revoke ordering (needs fault injection; `TestRegistryRevokeStateFirst` pins the invariant that a revoked state refuses even with an enabled link).
- Commands: `go test ./...` ok; `go test -race` on fleet/store/config ok; `GOOS=windows go vet` on the touched packages ok; `task lint` ok; `task test` Go part ok, one unrelated web test (`desktop-chrome.test.tsx` macOS toggle) timed out at 5 s under load and passes alone.

### F1c UI wire contract (what the UI codes against; F1b implements it in `internal/wsapi/fleet_host.go`)

F1b had no code when F1c started, so F1c fixes the UI-facing shapes here and tests against fixtures; F1b adapts or says what differs. All
methods are the local-only admin set of ADR-0048 section 8 (`fleet.host.list` may also work for an approved device). With fleet off every
method answers `method_not_found`; the UI reads that as "Fleet is off".

- `fleet.host.list {}` -> `{hosts: HostView[]}`.
  `HostView = {hostId, alias, label, state: "pending"|"approved"|"revoked", pendingApproval: bool (responder side: this human must approve),
  link: "active"|"connecting"|"down"|"refused"|"peer_disabled"|"disabled"|"revoked", linkCode?: string (refusal code, e.g. peer_identity_changed,
  fleet_version_unsupported), linkGuidance?: string, outboundEnabled: bool, inbound: {enabled: bool, grant: Grant}, sas?: string (6 digits, only
  while pending), createdAt, lastSeenAt?, version?, os?, arch?}`. No key, secret, address or relay field exists in it.
  `Grant = {ops, workspacesRead:{all?,ids?}, workspacesWrite:number[], providers:string[], maxLiveAgents, maxWallSeconds, maxCostUsd, permissionMode?}` (the `registry.Grant` JSON).
- `fleet.host.invite {window?: "10m"} -> {invite: "smind-host://v1#...", expiresAt}`; `fleet.host.add {invite, alias?, allowCleartext?} -> {host: HostView}` (the SAS is in `host.sas`).
- `fleet.host.approve {hostId, sas, grant}` -> `{host}`: the typed six digits travel with the call so a client cannot confirm blindly. `fleet.host.reject {hostId}`, `fleet.host.rm {hostId}`, `fleet.host.rename {hostId, alias}`, `fleet.host.grant {hostId, grant}`, `fleet.host.enable_reverse {hostId}` / `disable_reverse {hostId}` (all `-> {host}` or `{}`), `fleet.host.revoke {hostId}`.
- Event topic `fleet.hosts.changed` (empty payload): the client refetches `fleet.host.list`.
- Inbox: `inbox.list` items gain kinds `host_pair`, `host_down`, `host_refused` and `links.hostId`. `internal/inbox` derives them from two injected sources (`HostPairs`, `HostLinkIssues`); F1c wires the pairs from the registry and leaves link issues nil until F1b's link manager exists.

## F1c progress (2026-10-31)

- Contract `fleet-hosts-f1c.contract.md` written before code (validation contract ids 14, 16). Wire shapes fixed in "F1c UI wire contract" above; F1b adapts (see the open items there).
- Steps: GO-1 inbox derived kinds + wsapi pair source, WEB-1 types/hook/fixture, WEB-2a/2b/2c Hosts page (list, approve+grant, invite/add/edit), WEB-3 web inbox rows, MOB-1/MOB-2 mobile client and screens, two fixes (fixture invite vs the hex-color scan; row layout at 390 found in frames), frames loop. Peer ledger: peer-glm 3 runs ok (GO-1, MOB-1, +1 lost to the 429 below), peer-sonnet 9 runs (WEB-1, WEB-2a, WEB-2b, WEB-3, MOB-2, WEB-2c, frames, two fixes). GLM hit `429 Rate limit reached for requests` on WEB-1 (0 work done) and the 5 h usage window on WEB-2a (reset about 18:28 +07); both re-run on peer-sonnet at once. Failure modes: 2 rate limits, 0 turn caps (`glm-acp-agent --max-turns 500` seen in `ps`), 0 wrong edits needing a rewrite.
- Open items for F1b (asked of the Steward): `fleet.host.list`, `fleet.host.approve` and `fleet.host.reject` must also work for an approved human device over the relay (mobile approves read-only from the inbox row, ADR S13); the approve call carries the typed `sas` (the daemon should verify it equals the pending SAS); `fleet.host.revoke` and `fleet.hosts.changed` exist; `inbox` link issues (`host_down`, `host_refused`) are fed by F1b's link manager through `inbox.Service.HostLinkIssues` (nil today).

### F1c (UI and inbox) validation, 2026-10-31

- AC11 (web list/states/SAS/grant): `TestHostsPage_RendersStates`, `TestHostsPage_FleetOffState`, `TestHostsPage_SASNeverAutoConfirmed`, `TestHostsPage_GrantRequiresWriteWorkspace`, `TestHostsPage_ReverseToggleShowsNarrowerGrant`, `TestHostsPage_RevokeConfirms`, `TestHostsPage_InviteShowsFragmentInviteNoPassword`, `TestHostsPage_AddShowsSasAndDoesNotApprove`, `TestHostsPage_EditGrantSaves` (`hosts-section.test.tsx`), hook tests in `use-fleet-hosts.test.tsx`.
- AC11 (inbox kinds): `TestInboxHostPairDerived`, `TestInboxHostDownOnlyAfterFiveMinutes`, `TestInboxHostRefusedHigh`, `TestInboxHostKindsAbsentWithoutSource`, `TestInboxHostSourceOffWhenFleetDisabled` (Go), `TestInboxScreen_HostKinds` (web).
- AC11 (mobile): `hostsList`, `hostsApprove`, `hostsSafety`, `hostsRoute`, `inboxView` host kinds (vitest, 869 mobile tests green, `tsc --noEmit` clean).
- Mutations (all fail the named test, restored): M1 drop the 5 minute age check (`TestInboxHostDownOnlyAfterFiveMinutes`), M2 host_refused attention normal (`TestInboxHostRefusedHigh`), M3 Approve without the typed-SAS match (`TestHostsPage_SASNeverAutoConfirmed`), M4 drop the write-workspace term (`TestHostsPage_GrantRequiresWriteWorkspace`), M5 reverse copy without read only (`TestHostsPage_ReverseToggleShowsNarrowerGrant`), M6 mobile approve sends no sas (`hostsApprove`).
- Frames (real app, isolated daemon, `fleet.host.*` and the host inbox items injected through Playwright `routeWebSocket` because F1b's RPCs do not exist yet), light+dark at 1280 and 390, every frame looked at: list, fleet off, empty, approve (3 states), reverse confirm, invite, add/SAS, revoke, edit access, inbox, Review hosts landing, settings list at 390, transition bursts of the approve dialog open/close, the add dialog form to SAS view and the page load. Found and fixed: the 390 row was squeezed by the beside-the-text switch (restacked), a duplicate direction line next to the switch (dropped), the third number label wrapping in the grant editor. Not capturable: the add-dialog form-to-SAS swap is an instant content replacement (first burst frame already shows the SAS view; the dialog re-centres to the smaller height). Stored under `~/.smind/dogfood/fleet-ui/frames`.
- Not verified: the real fleet daemon RPCs (F1b), mobile on a device or simulator (no native frames; logic and source scans only), the link-issue inbox rows end to end (source is nil until F1b).
- Commands: `go test ./...` (only the known flaky `TestArchivePreviewLookup_SecondPreviewInTheFloorMakesNoSecondCall` failed once and passed on rerun; `TestDaemonLogCrashHelperProcess` is a helper-process test), `go test -race` and `GOOS=windows go vet` on `internal/inbox` and `internal/wsapi`, `task lint`, `task test` (web 4169 tests), `node --test docs-site/scripts/*.test.mjs`.
