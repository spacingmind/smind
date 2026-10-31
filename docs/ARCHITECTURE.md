# Architecture

sMind (Spacing Mind) runs as a single Go daemon (`cmd/smind`, default `:4648`) that
serves the API and the embedded web UI from one binary — see
[decisions/0001-go-single-binary.md](decisions/0001-go-single-binary.md).

## Data model (planned)

```
Workspace -> Space -> Task -> Tabs
```

- **Workspace** — a top-level project/environment an agent operates in.
- **Space** — a grouping of related tasks within a workspace.
- **Task** — a unit of agent work; owns one or more tabs.
- **Tabs** — individual views/sessions (e.g. terminal, diff, chat) within a task.

## Routing engine (phase 1)

The routing engine maps agent traffic to provider accounts:

- **Accounts** — individual provider credentials (e.g. an Anthropic account).
- **Pools** — groups of accounts that can share load.
- **Hard isolation vs. pool policies** — some workspaces require a dedicated
  account with no sharing (hard isolation); others can draw from a pool under
  a routing policy (e.g. round-robin, least-loaded).

## Fleet: connected hosts (ADR-0048, off by default)

With `fleet.enabled: false` (the default) none of this exists at run time: no host key, no `fleet.*` method, no table
row. The schema is created by migration either way, so enabling later never reopens the store.

- **Host key** (`internal/fleet/identity`): an Ed25519 seed in `$SMIND_HOME/host-key`, mode 0600 through
  `internal/securefile`, created lazily the first time fleet is enabled. It is separate from the relay's X25519 key and from
  device keys. `hostId = base32(SHA-256(pub))[:26]`, the same derivation as a device id (ADR-0031 D35; a shared test vector
  pins it). A missing file is created; an unreadable, corrupt or group/world-readable one stops fleet and is never
  regenerated or chmodded.
- **Registry** (`internal/fleet/registry`): one pairing creates one host row and two directed links. Each link row is either
  the *outbound* record (may I call this peer) or the *inbound* record (may it call me, plus the grant I hold for it).
  Defaults (S5): the initiator's outbound is on and its inbound is off; the responder's inbound is on only after a human
  approves with at least one write workspace. The default reverse grant is read-only with 0 agents, 0 wall seconds and $0.
  A grant is validated on every write; no peer-facing method can write one.
- **Tables** (`internal/store`): `fleet_hosts` (identity, alias, pinned public key, state), `fleet_links` (host, direction,
  enabled, grant JSON, transport JSON), `fleet_audit` (one row per cross-host call on each end, 30-day retention),
  `fleet_watches` (durable finish watches; no foreign key, so a watch outlives its host), `fleet_idem` (idempotency keys),
  `fleet_remote_tasks` (caller-side mirrors, cache only), and the columns `tasks.origin_host`, `tasks.origin_trace`,
  `runs.origin_host` (empty for local work). No table models a mesh: a mesh is the reverse toggle on every pair.
- **Config** (`fleet.*`, `internal/config`): the switch and the limits (hosts, connections per peer, handshake, frame size,
  outstanding requests, observation backoff, idempotency and replay windows, audit retention). Only a human edit of
  `config.yaml` can change them; there is no environment override.

## Surfaces roadmap

1. **Web** — the initial surface, served by the Go daemon (`web/packages/ui`).
2. **Mobile** — Expo app, reusing the same API.
3. **Desktop** — Tauri app, reusing the same API and web UI bundle.
