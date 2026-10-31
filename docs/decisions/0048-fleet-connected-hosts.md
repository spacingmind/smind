# 0048: Fleet — connected hosts: a daemon drives another daemon, through paired, directed, granted links

## Status

**Accepted — 2026-10-31.** Decisions D1–D7 are the Human's, given 2026-10-31 (section "Human answers 2026-10-31"). Everything else
is a **Supervisor default 2026-10-31 under the Human's directive, Human may veto** (section "Supervisor defaults"); a veto changes
the PR named next to it in `docs/plans/active/fleet-hosts.md`. Spec, acceptance criteria and PR split: `docs/plans/active/fleet-hosts.md`.

Inputs: ADR-0031 (D9 multi-device, pinned relay identity; D35 Ed25519 `device_hello`), ADR-0035 (relay E2EE v2), ADR-0041 (seats,
tree budget A5, parent notifications B3), ADR-0017/0019 (an agent never answers a permission), ADR-0044 (rails, audit), ADR-0045
(inbox), ADR-0047 (policy floor S1), `docs/WORKSPACE_PROTOCOL.md`, Paseo's connected hosts (`refs/paseo/docs/connected-hosts.md`,
`.../connected-hosts/`, `.../agent/tools/paseo-tools.ts`), and a local research note (`docs/research/local/fleet-connected-hosts.md`,
gitignored).

## Context

Today one smind daemon is one machine. The Human has several (a Mac, a Linux homelab box, a Windows box for Windows-only
verification). A Lead on one of them cannot ask another to run a Peer, check a build on Windows, or read what a remote agent is doing.
The Human relays by hand, and the work that needs a different OS or a bigger GPU box is the work that gets skipped. Paseo ships
"connected hosts": daemon A saves daemon B and its agent tools take an optional `serverId`.

smind already has the pieces of an identity-checked link, but each is built for a *human's device*: persistent Ed25519 device keys with a
pairing window and approval (ADR-0031 D35), E2EE v2 over a self-hosted relay (ADR-0035), a Go wsapi client (`internal/wsclient`), a
pinned relay identity (D9). What is missing is a daemon that is a *caller*: its own identity, a wsapi principal that is "another
daemon" with a closed method set, a durable cross-daemon watch, and limits that hold across hosts.

### Threat model in one paragraph

A host link gives a *remote party* the power to start agents on this machine, so it is the most dangerous door smind has added since the
relay. Assume the peer daemon can be **compromised** (its Lead prompt-injected, its key stolen); that **a prompt written on one host and
executed on another is hostile data**; that traffic can be **replayed**; that a peer will try to **widen its own grant** and to **evade a
budget** by changing its trace id or spawning from many trees. Therefore: authority lives only on the callee and is set only by a human over
a local-only method; every cross-host request carries a provenance chain the callee verifies against the *authenticated* link, not
against what the caller claims; the callee's own policy floor and budgets always apply; nothing a link carries can change a grant,
a link, a policy or a secret; and every call is audited on both ends.

## Human answers 2026-10-31

All **ACCEPTED**. Wording is the Human's; the ADR text below implements it.

- **D1 Symmetric data model.** Pairs are created by one pairing ceremony that makes two directed links; each direction has its own
  grant; by default only ONE direction is enabled (the pairing initiator → the other); turning on the reverse direction is an
  explicit toggle with a narrower default grant; a full two-way mesh is just that toggle turned on for every pair.
- **D2 Transport.** A daemon connects to another daemon as a normal client/device and reuses the existing transports: direct
  (LAN/Tailscale, password plus pinned identity per ADR-0031 D9) and the self-hosted relay with E2EE v2 (ADR-0035) are both in v1;
  the pairing approval reuses Ed25519 `device_hello` (ADR-0031 D35); SSH comes later; hosted relay stays excluded.
- **D3 Loop and blast-radius safety.** A provenance chain (origin host, trace id, hop list) on every cross-host call, refused if the
  callee appears in the chain; hop limit = 1 (no transitive routing); a per-origin budget (live agents, wall time, cost) counted across
  hosts; the callee's own permpolicy floor (S1, ADR-0047) always applies; cross-host calls audited on both ends.
- **D4 v1 remote operations.** List hosts, list workspaces and tasks, create a task or agent in a destination workspace (explicit
  provider and model), send a prompt, get status and activity, with finish notifications routed back and durable across a caller
  restart (Paseo semantics: three failed observations = "status unknown", never "finished"). Remote terminal and file access are NOT in v1.
- **D5 Code moves through git only.** The destination fetches the branch from origin into its own worktree; no file sync; results come
  back as commits, PRs and run events.
- **D6 Accounts and secrets never cross hosts.** Each host uses its own provider accounts; no borrowing of another host's accounts or
  `/v1` proxy.
- **D7 Global ids `host:id`.** Exactly one owner host per task; capability plus version checks on every reconnect; an older peer gets
  update guidance.

## Decision

Terms. A **host** is one smind daemon, identified by a **host key** (an Ed25519 keypair the daemon creates on first use,
`$SMIND_HOME/host-key`, mode 0600 through `internal/securefile`). `hostId = base32(SHA-256(pub))[:26]` (the D35 device-id derivation, so
one identity scheme). An **alias** (`^[a-z][a-z0-9-]{0,31}$`, unique per registry, editable locally) is the human name. A **pair** is two
hosts that completed the pairing ceremony. A **directed link** `X→Y` means "X may call Y". The **caller** initiates the connection;
the **callee** holds the authority. A **grant** is the callee's list of what a given caller may do.

### 1. Data model: pairs, directed links, grants (D1)

```
pair  P(A,B)        created once by one ceremony (section 2)
 ├─ link A→B         enabled by default if A is the pairing initiator; grant held by B ("inbound record for A")
 └─ link B→A         DISABLED by default; grant held by A ("inbound record for B"), default grant narrower
```

Each host stores, per peer: its **outbound record** (is my link to the peer enabled, how to reach it: addresses and/or relay offer,
pinned peer public key, last status) and its **inbound record** (does the peer have any authority over me: `enabled`, `grant`). A call
works only when *both* the caller's outbound record and the callee's inbound record are enabled; the callee's record is authoritative and
the caller learns its state only from the link (`link_state`: `active | peer_disabled | revoked`).

- The pairing **initiator** is the host that runs `smind host add`. After pairing: initiator outbound = on, responder inbound (for the
  initiator) = on with the **default forward grant**; initiator inbound (for the responder) = **off**, responder outbound = off.
- **Reverse** is enabled on the host that will *receive*: `smind host enable-reverse <peer>` on the initiator creates/enables the
  initiator's inbound record for the responder with the **default reverse grant** (narrower: read-only, see below) and tells the
  responder over the link (`fleet.link.state`), which then marks its outbound record usable. Only a human on the receiving host can
  do this (local-only method; never a link method, never an agent). `smind host disable-reverse <peer>` turns it off immediately
  (drops the live connection).
- A full mesh is `enable-reverse` run on every pair; no mesh object exists.
- **Grant** (callee-side, per caller; stored as JSON, validated on write, defaults shown):

  | Field | Default forward grant (A→B, B holds) | Default reverse grant (B→A, A holds) |
  |---|---|---|
  | `ops` | `read` (list hosts/workspaces/tasks, status, activity), `create`, `send`, `stop` | `read` only |
  | `workspaces.read` | all | all |
  | `workspaces.write` (create/send/stop allowed only inside these) | **none until the Human selects ≥ 1 at approval** | none |
  | `providers` | `[]` = any provider this host has configured, but the model must be explicit | `[]` |
  | `maxLiveAgents` | 2 | 0 |
  | `maxWallSeconds` per remote run | 7200 | 0 |
  | `maxCostUSD` per rolling 24 h, reported cost only | 5.00 | 0 |
  | `permissionMode` ceiling of remote-created runs | the callee's configured default for remote-origin (`default`, asks) | n/a |

  A grant can only be created or edited by a human through `fleet.host.grant` (local-only, section 8); a peer cannot read another peer's
  grant and can read only its own *effective* limits (`fleet.info.limits`).

### 2. Pairing ceremony (D2, reuse of D35)

1. **Invite (callee side).** On the host that will be called first (the responder), `smind host invite [--window 10m] [--via direct|relay]`
   opens a pairing window (reusing the `devices.Window` rules: default 10 min, **one** slot, only a peer first seen at/after the window
   opened can take it) and prints/renders (QR) an **invite**: `smind-host://v1#<base64url json>` with `{hostId, pub, addresses[],
   relay: {endpoint, hostId, secret?, fingerprint}, window, nonce}`. The offer reuses `internal/relay/pairing` encoding (fragment only,
   never sent to a server).
2. **Add (caller side).** `smind host add <invite> [--alias x]` on the initiator pins `pub`, dials the responder (direct or relay), runs
   **host hello** (section 3) as a *pending* peer, and sends `fleet.pair.request{hostId, pub, label, addresses, relay offer, version, caps}`.
3. **Approval (callee side).** The responder records the initiator in the existing device-style registry with kind `host`, state `pending`
   (never auto-approved *without* the window's slot). A **short authentication string** (6 digits derived from the handshake transcript,
   Supervisor default S3) is shown on both hosts; the Human compares and approves with `smind host approve <id>`, in the web Hosts page, or
   in the inbox row. Approval selects the forward grant (workspaces required for write ops).
4. **Result.** Both sides persist the pair (section 1). Pairing never moves a secret except the *relay* host secret in the invite (stored
   `0600` in `fleet-secrets`, never returned by any read API, redacted from logs); the daemon password is **not** part of the ceremony
   over relay and, over direct, is only sent inside a verified `wss` or an encrypted overlay (section 3).

### 3. Transport and host hello (D2)

A link is one persistent wsapi connection from the caller to the callee, opened by the caller's daemon with `internal/wsclient` (direct)
or `internal/relay/client` + the E2EE v2 handshake (relay). The caller is "a normal client/device"; no new daemon-to-daemon protocol or
port exists.

**Host hello** replaces the browser/device principal on that connection. First request `fleet.hello`:

```
callee → caller   fleet.challenge{nonce_c, hostId_callee, fleetV, caps}                (fresh per connection)
caller → callee   fleet.hello{v:1, hostId, pub, nonce_a, version, fleetV, caps,
                  sig = Ed25519("smind-fleet-hello-v1" ‖ transcript_hash ‖ hostId_callee ‖ hostId ‖ nonce_c ‖ nonce_a)}
callee → caller   fleet.hello.ok{sig_callee over the same fields with roles swapped}   (caller verifies against the PINNED pub)
```

The signature is **mutual**: the callee proves it holds the pinned key (the caller refuses on mismatch: `peer_identity_changed`, no
fallback); the caller proves it holds the key recorded at approval. `transcript_hash` is the E2EE v2 transcript on relay, and the TLS exporter
value on `wss`. A host principal is admitted only if its `hostId` has an **approved inbound record** and (version/caps check, section 9) passes.
Revocation (`smind host rm`/`disable-reverse`/inbound off) closes live connections at once.

**Direct transport rule (Supervisor default S4).** The daemon refuses a direct link whose route is a cleartext `ws://` to a non-loopback
address unless the address is inside an encrypted overlay (Tailscale CGNAT `100.64.0.0/10` / `fd7a:115c:a1e0::/48`) or the URL is `wss`
with a pinned certificate fingerprint; `--allow-cleartext` overrides with a printed warning that prompts and results are readable on that
network. Pairing-time password (if the callee has one set) is sent only on those routes.

### 4. Provenance, loops and hop limit (D3)

Every cross-host request carries, in a typed `fleet.prov` object (not free text):

```
{ origin: hostId, trace: <uuid, minted by the origin>, hops: [hostId...], originTask?: id, reqId: <uuid>, ts }
```

Callee checks, in order, **before any handler runs**; the first failure is a typed error `fleet_refused{code}` and an audit row:

1. `origin` and `hops[0]` equal the **authenticated** link principal (`principal_mismatch`: a peer cannot launder another origin).
2. The callee's own `hostId` is neither `origin` nor in `hops` (`cycle`). A→B→A is refused at A.
3. `len(hops) == 1` (`hop_limit`). The chain is `[origin]` for a direct call. Anything longer, or empty, is refused: there is **no
   transitive routing** in v1.
4. `reqId` has not been seen on this link in the replay window (`replay`), `ts` is within ±120 s (`stale`).
5. The grant allows the op in that workspace and the per-origin budget has room (section 6) (`not_granted`, `budget_exceeded`).

Caller-side guard (defence in depth, a *drift guard not a sandbox*, as ADR-0041 A3): a task whose `origin_host` is not this host (it was
created by a peer) gets **no fleet tools** and any fleet call it attempts is refused locally with `hop_limit` before a byte is sent. The
callee-side check in 3 is the real boundary, because a compromised B ignores its own guard.

### 5. Operations in v1 (D4) and what is not (D4, D5)

Wire methods (a host principal can call **only** `fleet.*`; every other method is refused `forbidden`, fail-closed; the allowlist is
a static map, walked by a test):

| Method | Needs | Notes |
|---|---|---|
| `fleet.info` | link | `{hostId, alias, version, fleetV, caps, os, arch, labels, limits}` |
| `fleet.workspace.list` | `read` | id, title, repo origin URL, default branch, **no local paths** |
| `fleet.task.list`, `fleet.task.get` | `read` | owner-host tasks only; `{id, title, branch, state, parent, origin}` |
| `fleet.task.create` | `create` + workspace in `workspaces.write` | `{idempotencyKey, workspaceId, provider, model, title, prompt, branch, baseRef}`; provider and model **required**; **no** `permissionMode`, `profileId`, `env`, `cwd`, `attachments`, `mcp` fields exist |
| `fleet.task.send` | `send` | `{taskId, idempotencyKey, prompt}`; text only; to a task this peer created |
| `fleet.task.stop` | `stop` | Supervisor default S6; to a task this peer created |
| `fleet.run.status`, `fleet.task.activity` | `read` | typed status; activity = bounded events, assistant text capped at 1 KiB, cursor-paged |
| `fleet.run.wait` (stream) | `read` | resumable by cursor; ends with a terminal event |
| `fleet.link.state`, `fleet.pair.*` | link / pairing | state notices only; cannot carry a grant |

**Code moves through git only (D5).** `fleet.task.create` carries `branch`/`baseRef` names. The callee fetches from its own `origin` into a
new worktree (`task.create` already takes `baseRef`); if the branch is not on origin the create fails `branch_not_found` and the caller
pushes first. There is no file upload, diff upload or path argument. Results are the callee's commits (pushed to origin by the remote
agent under the callee's normal git approvals), PRs and run events. **No remote terminal and no file access in v1.**

**Remote-created tasks** carry `source=remote`, `origin_host`, `origin_trace` in `tasks`/`runs`; they run under the callee's own seat
profile logic and the callee's permission mode (ceiling in the grant), their permission requests go to the **callee's** inbox (a remote
caller can see "waiting_permission" in status but can never answer, ADR-0017), and the ADR-0047 floor applies in all modes. Prompts from
a peer are stored verbatim and shown in the callee's UI with an origin badge (they are data from a less-trusted origin; no rewriting).

### 6. Budgets across hosts (D3)

- **Callee, per origin host** (hard bound; independent of any trace id a caller picks, so splitting trees evades nothing): live remote-origin
  agents ≤ `maxLiveAgents`; wall time per run ≤ `maxWallSeconds` (a run over it is stopped by the callee and reported `wall_limit`);
  reported cost per rolling 24 h ≤ `maxCostUSD`. Admission refuses with `budget_exceeded{which}`. Running runs are not killed for cost
  (same stance as ADR-0041 A5); a wall-limit stop is.
- **Callee, per trace** (soft, informational): sums are shown against `origin_trace` in the audit and in status.
- **Caller, per tree**: the remote task's reported cost and live count roll into the local task tree (`task.tree` rollups mark them `remote`),
  so ADR-0041 A5's tree budget at admission includes remote spend; a remote create counts as one agent toward the tree's concurrency guard.
- Costs a provider reports as nil are never estimated (ADR-0020); the cap bounds *reported* cost only, stated in the UI.

### 7. Finish notifications, durable across a caller restart (D4)

The **caller's** daemon owns the subscription. On a successful (or ambiguous) `fleet.task.create`/`send` with notify on (default), it writes
a `fleet_watches` row **in the same transaction** that records the remote task mirror (local commit; the remote create is a separate
commit, as in Paseo; a failed local write reports failure and tells the Lead to inspect before retrying; the create is idempotent
by `idempotencyKey`, so retry is safe).

- The watcher resumes at daemon start from `fleet_watches` and opens `fleet.run.wait` with its cursor; a run that finished while the
  caller was down is read as terminal from the callee (`fleet.run.status`), so the notification is not lost.
- Observation failures: **three consecutive failed attempts** (capped backoff; an attempt while the link is still *connecting* does not
  spend the budget) end the watch with a notification "could not observe task `host:id`; **status unknown**". It never says "finished". A
  confirmed removed host reports "host unavailable" at once, same path. The row is closed; reconnecting does not reopen the watch; the
  Lead decides whether to inspect or re-delegate.
- A definitely-unsent prompt (refused before send) creates no watch. An ambiguous one keeps the intent and follows the retry policy.
- Delivery uses ADR-0041 B3 verbatim: one `child_event` item in the parent chat's queue (outbox column, coalescing, debounce, quiescence),
  with a `host=<alias>` field and the remote text marked `<child-response host="..." untrusted="true">`, capped at 1 KiB.

### 8. wsapi, CLI, MCP, UI surface

- **Local-only admin methods** (`localOnly`, absent from `RunScopedMethods` and from the host-principal allowlist; unavailable over the relay for
  a *device*, as `relay.device.*`): `fleet.host.list|get|invite|add|approve|reject|rename|rm|grant|enable_reverse|disable_reverse|test|audit`.
  Reads that return no secrets (`fleet.host.list|audit`) may work over an *approved human device* on the relay; every write is local-only.
- **CLI** `smind host`: `add <invite>`, `ls [--json]`, `rm <host>`, `invite`, `approve <id>`, `grant <host> ...`, `enable-reverse <host>`,
  `disable-reverse <host>`, `rename`, `test <host>`, `audit`. Later phases add `smind host workspaces|tasks <host>` and `smind task new --host`.
- **MCP** (Lead seat only; Peers cannot spawn, ADR-0041 A3): new `list_hosts`; existing `workspace_list`, `task_new`, `task_send`,
  `task_status`, `task_logs` (activity), `task_stop` gain an optional `host` (alias or id; omitted = this daemon). Result structures
  always include `host` and a `globalId` (`alias:id`).
- **Ids (D7).** On the wire `{hostId, id}`; in UI/CLI/MCP `alias:id`, with `alias` resolved through the local registry only. A task row
  exists on exactly one owner host; the caller keeps a **mirror** row (cache, safe to delete, never authoritative) for the tree and the
  inbox; every mutation is sent to the owner.
- **UI.** Web Settings → Hosts: list with state dot, direction toggles, grant editor, pairing (invite/add/approve with the SAS), audit
  tab. A **host label chip** on any remote task or tree node (F4). Inbox kinds (section "Supervisor defaults" S9). Mobile (Expo): read
  the host list, see the chip and approve a pending host from the inbox row; **no pairing from mobile in v1** (needs the SAS flow on a
  small screen; deferred).

### 9. Capability and version checks (D7)

`fleet.info`/`fleet.challenge` carry `fleetV` (int, v1 = 1) and `caps` (strings such as `fleet.read`, `fleet.write`, `fleet.watch`,
`fleet.labels`) plus the build `version`. On **every** physical (re)connect, before admitting any call, each side requires equal
`fleetV` major and that the peer's `caps` ⊇ the caps needed by the enabled grant. Failure = `fleet_version_unsupported{needs, has, host,
guidance}` with guidance "update smind on `<alias>` (has `<v>`, needs ≥ `<v2>`)" and the link stays `refused` (no downgrade, no
local fallback). Local operations are never affected. A daemon without `fleet.*` answers method-not-found to `fleet.challenge`, which the
caller maps to the same guidance.

### 10. Secrets and accounts never cross hosts (D6)

- The fleet wire schema is a closed set of typed structs; none has an env, token, key, account id, URL-with-credentials or path field.
  `fleet.workspace.list` returns repo remote URLs with any userinfo stripped.
- Provider accounts are never listed or addressed across hosts: the caller names a **provider id and model**, the callee resolves them
  against **its own** accounts (`providers.list` equivalent returns ids and model names only). A create naming a provider the callee
  lacks fails `provider_not_configured`.
- No `/v1` proxy forwarding and no account borrowing; the relay host secret in an invite is the only credential an invite carries and is
  transport, not a provider secret.
- The callee's `secretdeny` rules and ADR-0047 floor apply to remote-origin runs, so a prompt cannot ask the remote agent to read the
  host's secret store.

### 11. Audit

`fleet_audit` on **both ends** (caller: outgoing; callee: incoming), one row per call: `{ts, direction, host, trace, reqId, method, taskId,
runId, outcome, code, latencyMs, bytes, summary}`. `summary` carries no prompt body: a SHA-256 and the first 120 characters passed through
the existing redactor (the ADR-0047 audit pattern). A `smind host audit [--host] [--since]` and the Hosts page read it; retention
30 days (config).

### 12. Limits (config `fleet.*`, caller and callee)

Feature **off by default** (`fleet.enabled: false`; off = today's behaviour byte for byte, host key not created, no `fleet.*` method served).
Caps: `maxHosts` 16, `maxConnectionsPerPeer` 1, handshake 10 s, frame 256 KiB, outstanding requests per link 16, observation backoff
1 s → 60 s, idempotency window 24 h, replay window 10 min.

## Options considered

1. **Mesh object / cluster membership.** One shared membership list. Rejected: a registry every node trusts is a new central authority;
   pairs with per-direction grants (D1) give the same mesh as a toggle without a coordinator.
2. **Forward the Human's own device credentials / a shared token.** Rejected: one stolen token = every host; no per-direction grant.
3. **Multiplex virtual sessions over one physical link (Paseo).** Deferred: needed for terminal and file bytes (not in v1). One wsapi
   connection per link is enough for control operations and keeps the principal model simple.
4. **Transitive routing with a hop budget.** Rejected for v1 (D3): hop limit 1; revisit only with a signed per-hop chain.
5. **Let the caller choose the permission mode or profile of a remote run.** Rejected (ADR-0019 #6 pattern): authority is the callee's.
6. **Sync worktrees / upload diffs.** Rejected (D5): git is the only transport for code; it already carries review and provenance.

## What we are NOT doing

Remote terminal or file access; SSH transport; hosted relay; transitive routing; borrowing another host's accounts or `/v1` proxy;
a remote caller answering permissions, editing grants, links or policy; automatic scheduling across hosts (F5 is a selector, not a
scheduler); attachments or images in remote prompts; per-hop signatures; replication of tasks between hosts; cross-host merge.

## Consequences

- **Good:** the two-host pattern the Human does by hand (Mac Lead → Windows Peer) becomes one tool call with limits and an audit;
  nothing requires a new port, protocol family or central service; the callee stays sovereign.
- **Cost:** a second identity (host key) next to device keys; a new wsapi principal with a fail-closed allowlist; a durable watch table;
  new migrations (`fleet_*`, `origin_*` columns). Breaking schema changes are acceptable (no installed base).
- **Risks and how they are bounded:** a stolen host key lets the thief act as that host *within the grant the callee gave it*, so grants
  are small by default, revocation is immediate, and a daily cost/agent/wall cap applies per origin; cleartext LAN is refused by default;
  grants are never writable from a link.
- **Honest limits:** the callee cannot verify that the caller's *Lead* is not injected (it sees only a trusted host); a malicious prompt
  accepted into a granted workspace runs under the callee's floor and mode and surfaces permission requests to the callee's Human, which
  is the intended boundary. Cost caps bound reported cost only. Clock skew beyond ±120 s refuses calls (clear error).

## Supervisor defaults 2026-10-31 under the Human's directive, Human may veto

| # | Default | If vetoed |
|---|---|---|
| S1 | **Host key is a separate Ed25519 key** (`host-key`, hostId = D35 derivation), not the relay daemon X25519 key and not a device key; created lazily when fleet is first enabled | F1a |
| S2 | **Feature flag `fleet.enabled`, default false** | F1a |
| S3 | **SAS** (6 digits from the handshake transcript) shown on both hosts at pairing; approval is a human act on the responder | F1b |
| S4 | **Cleartext `ws://` to a non-loopback, non-overlay address refused**; `--allow-cleartext` with a warning; password only on verified routes | F1b |
| S5 | **Default grants** as the table in section 1 (forward: read + create/send/stop, writes only in workspaces the Human picks, 2 live agents, 2 h/run, $5/24 h; reverse: read-only) | F1a, F3a |
| S6 | **`fleet.task.stop` is in v1** (the Lead must be able to stop a runaway remote agent); no archive, no delete, no mode change | F3a |
| S7 | **Text-only prompts**: no attachments, images or `@file` expansion across hosts | F3a |
| S8 | **No remote permission answering**; remote-created runs ask the callee's Human via the callee's inbox; the caller sees only `waiting_permission` | F3a |
| S9 | **Inbox kinds** (derived rows, ADR-0045 style): `host_pair` (pending approval, high), `host_down` (a link stayed down > 5 min after 3 failed observations, normal), `host_refused` (version skew or identity change, high). Remote permission requests appear as ordinary `permission` rows with a host chip. Cross-host calls are **not** inbox rows (they are audit) | F1c |
| S10 | **Audit stores a hash and ≤120 redacted characters**, never the prompt body; 30-day retention | F2a |
| S11 | **Replay**: ±120 s timestamp and a 10-minute `reqId` window per link; create/send use an `idempotencyKey` (24 h) so an ambiguous retry returns the same task | F2a, F3a |
| S12 | **Host label chip and mirror rows** (cache only) rather than replicating tasks | F4 |
| S13 | **Mobile**: list + chip + approve from the inbox; **no pairing on mobile in v1** | F1c |
| S14 | **F5 selector** (`labels`) must resolve to exactly one enabled, connected host unless the caller passes `any:true` (then least live agents, ties by alias); no hidden scheduling or retry on another host | F5 |
| S15 | **Plugin sessions** (ADR-0042) never receive `fleet.*`; added to the plugin method denylist | F1b |
| S16 | **One connection per peer, no multiplexing**; terminal/file channels will need the multiplexing design in a later ADR | n/a |
| S17 | **Callee wall-limit stop**: a remote run over `maxWallSeconds` is stopped by the callee and reported `wall_limit` (the only forced stop) | F3a |
| S18 | **Alias, not hostId, appears in UI**; the hostId prefix (8 chars) is shown on conflict and in `host ls --json` | F1b |

## Questions for the Human

None blocking. If the Human wants a different default grant (S5) or wants mobile pairing (S13) or the stop op removed (S6), say so
and the named PR changes.
