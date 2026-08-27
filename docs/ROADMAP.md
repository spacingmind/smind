# Roadmap

## Phase 0 — Scaffold (week 1)

Goal: repo lives, binary runs.

- [x] Init repo `spacingmind/smind`: go.mod, cmd/smind, internal/{config,server}
- [x] Health endpoint `GET /healthz` listen :4648, config load from `~/.spacingmind/config.yaml`
- [x] Bun workspace `web/` + Vite React placeholder, embed into binary
- [x] Taskfile: `task build` (1 binary); `task dev` (hot reload both) still TODO
- [ ] CI: GitHub Actions — go test + bun build + lint
- [ ] npm reserve `smind` stub

Definition of done: `smind` binary runs, `localhost:4648` shows placeholder UI.

## Phase 1 — Core routing (weeks 2-5) — *the heart*

Goal: proxy Anthropic/OpenAI-compatible requests across multiple accounts.

- [ ] Accounts registry: OAuth/API key credentials (`~/.spacingmind/accounts/`), automatic token refresh
- [ ] Proxy endpoints: Anthropic `/v1/messages`, OpenAI `/v1/chat/completions`
- [ ] Session affinity: same conversation → same account (24h TTL)
- [ ] Failover chain: exhausted account → next in pool
- [ ] Quota poller: per-account usage, TTL cache
- [ ] Routing policy v1: `hard` (single account) + `pool` (fill-first)
- [ ] SQLite: accounts, routing decisions, quota snapshots
- [ ] TLS fingerprinting (utls) for outbound provider requests

Definition of done: Claude Code pointed at `ANTHROPIC_BASE_URL=localhost:4648` works across 2 accounts with real failover.

## Phase 2 — Workspaces + Tasks + Web UI (weeks 6-10)

Goal: replace Paseo as daily driver.

- [ ] Workspace CRUD: `smind workspace create <path> --account X / --pool Y`
- [ ] Space CRUD (optional layer) + per-space env
- [ ] Task lifecycle: create (auto worktree) → run → archive (clean worktree)
- [ ] Agent spawning via ACP: Claude Code, Codex, GLM
- [ ] Web UI: split panes + tabs; workspace/space/task tree sidebar; agent
      timeline (streaming chat); file explorer with git status; CodeMirror 6
      editor + preview; custom per-hunk diff viewer; xterm terminal (PTY in
      task cwd); permission prompts UI
- [ ] Auth: simple token
- [ ] `smind` CLI: `task new`, `ls`, `attach`, `send`, `logs`, `stop`

Definition of done: a real scopedocs feature built end-to-end from smind UI, replacing Paseo.

## Phase 3 — Relay E2EE + Mobile (weeks 11-14)

Goal: usable from a phone outside the home network.

- [ ] Relay server (Go, dumb pipe), self-hostable as `smind relay`
- [ ] E2EE handshake: X25519 + ChaCha20-Poly1305, QR pairing
- [ ] Reconnect grace, correct key rotation
- [ ] Mobile app (Expo + @expo/ui): pairing + workspace/task list, realtime
      agent timeline + follow-up, push notifications, mobile permission approval
- [ ] Deploy relay at `relay.spacingmind.sh` (Cloudflare TLS)

Definition of done: from a phone off-network — assign new work, get notified, approve an agent.

## Phase 4 — Tauri desktop + polish (weeks 15-17)

- [ ] Tauri 2 wrapper: tray icon, global shortcuts, native notifications, auto-update
- [ ] Complete native-call abstraction layer
- [ ] Full settings UI (accounts, pools, per-workspace policies)

## Phase 5 — Automation (week 18+, demand-driven only)

- [ ] Heartbeats: task self-evaluates and continues
- [ ] Schedules: cron creates new tasks (e.g. daily triage)
- [ ] Subagents: session spawns session, cross-provider
- [ ] GitHub integration: PR status / checkout PR as a task
- [ ] Per-workspace audit log
- [ ] Quota prediction

## Guiding principles

1. **Dogfood-first** — each finished phase replaces a piece of Paseo in real use.
2. **No feature without real demand** — Phase 5+ items get built only when
   actually missing in daily use, never speculatively.
3. **Go core API stays stable** — UI/mobile/desktop are just clients; the
   protocol locks in starting Phase 1.
4. **Every decision stays reversible** — avoid irreversible lock-in (e.g.
   Tauri vs. Electron, Expo vs. native both stay open).

## Tracked risks

| Risk | Mitigation |
| --- | --- |
| Agent protocols change (Claude/Codex) | target the ACP standard, avoid deep SDK integration |
| ToS risk from account pooling | isolation-first defaults, pooling is opt-in |
| Solo-dev maintenance load | keep scope tight — do not clone every Paseo feature |
