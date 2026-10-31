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
- **Write path** (`internal/fleet/serve`, `internal/wsapi/fleet_write.go`): `fleet.task.create|send|stop` and `fleet.run.wait`
  run behind the same provenance, grant and audit as the reads, then ownership (`tasks.origin_host` must be the caller), an
  idempotency lookup (`fleet_idem`, 24 h), the per-origin budget (live runs, 24 h reported cost and wall seconds, counted
  per origin host across trace ids) and a `Backend` that creates the task from the callee's own `origin` and starts the
  run with the `remote` source (never AutoAccept, floor and rules decide every request, the origin host is stamped on the
  run row when it starts). A sweeper stops remote runs past the grant's wall limit and stamps `stop_reason=wall_limit`.
  A non-refusal error never reaches the peer verbatim (it can carry a local path).
- **Config** (`fleet.*`, `internal/config`): the switch and the limits (hosts, connections per peer, handshake, frame size,
  outstanding requests, observation backoff, idempotency and replay windows, audit retention). Only a human edit of
  `config.yaml` can change them; there is no environment override.

## Office mode: documents as deliverables (ADR-0049)

Agents' files are first-class results, on every transport. None of this changes a prompt, tool, permission or store schema.

- **Binary-safe reads** (`internal/files/binary.go`, `internal/wsapi/files_binary.go`): `file.stat` classifies a file from its
  first bytes (never its extension) and flags a mismatch; `file.readRange` returns one base64 chunk of at most the transport's
  ceiling (`daemon.info.files`: 4 MiB direct, 512 KiB over the relay, 50 MiB per file). The file is opened once and checked on
  the open descriptor; every chunk can be pinned to the stat'd `mtime`; UNC, device and drive-relative paths are refused before
  any filesystem access; errors never carry an absolute path.
- **PDF preview** (`web/packages/ui/src/components/pdf-preview/`, `file-tab-router.tsx`): a binary-looking file in a file tab
  asks `file.stat` first. A PDF is read in chunks and handed by `postMessage` to `pdf-viewer.html`, a separate page in an
  `<iframe sandbox="allow-scripts">` (opaque origin, its own CSP with `connect-src 'none'`, no network, annotation layer and
  scripting off, pdf.js pinned in `package.json`). The viewer is a classic script with the pdf.js worker inlined as a classic
  blob worker; the desktop proxy serves exactly its two static files without the cookie gate (an opaque origin cannot send
  the cookie). The router, panels and pane are one lazy chunk.
- **Interface mode and concise cards** (`lib/interface-mode.ts`, `stores/interface-mode-store.ts`,
  `components/timeline/concise-tool-card.tsx`): the mode is a client-only preference (`smind:interface-mode`, the bare string
  `office`; anything else reads `coding`) and is never sent to the daemon. In office mode a `terminal`- or `edit`-intent tool
  card is one plain line ("Ran a command", "Edited report.md") plus a status, with the one-line error summary when it failed;
  every other card, permission row and error box is unchanged. The home screen takes its greeting from `OFFICE_GREETINGS` and
  shows four suggestion chips (`lib/suggested-prompts.ts`) that fill the composer and never send.
- **Deliverable cards** (`components/timeline/deliverables.ts`, `deliverable-card.tsx`): after a run, candidates are the
  `path` of successful write-intent tool calls plus workspace-relative paths in the final assistant message (markdown links,
  inline code; shell text is never parsed). Each candidate gets one `file.stat` per run, and a card only if the file exists,
  has an allow-listed extension, is not a mismatch and was written at or after the run started. At most 6 cards and 20 checks
  per run. A card never opens anything by itself.
- **Open in app, Download, Reveal, Copy path** (`file-actions.tsx`, `lib/download-file.ts`,
  `desktop/src-tauri/src/open_path.rs`): the desktop's `open_path_default(root, rel_path)` canonicalises, requires a regular
  file inside the canonical root with an allow-listed extension, refuses UNC paths and any daemon that is not the local one,
  and opens through the opener plugin (no shell); its errors never carry a path. The web build's Download assembles chunks
  into a Blob (pinned to the stat'd `mtime`, 50 MiB cap, sanitised basename, object URL revoked at once).

## Surfaces roadmap

1. **Web** — the initial surface, served by the Go daemon (`web/packages/ui`).
2. **Mobile** — Expo app, reusing the same API.
3. **Desktop** — Tauri app, reusing the same API and web UI bundle.
