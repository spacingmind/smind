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

## Validation

Docs-only PR: markdown links and the ADR/plan cross-references checked by hand; `task lint` run. Implementation PRs fill this section per AC with evidence (test names, mutation results, frames) as they land; the plan moves to `docs/plans/completed/` only when every AC is confirmed here.
