# Roadmap

## Status as of 2026-10-22

Audited against `develop` @ `5b90afae`, merged PRs, the plans in `docs/plans/` and the code itself. **done** = built and
tested; **partial** = built but a DoD item is missing or only partly verified; **missing** = not started; **unverified** is
called out where something is built but no evidence shows it was ever run for real (a real phone, a real Windows machine,
a live provider account). The per-phase sections below keep the original goals and checklists; where this table and an older
checklist line disagree, this table is newer.

| Phase | Item / DoD | Status | Evidence |
| --- | --- | --- | --- |
| **0 Scaffold** | Repo, binary, `/healthz`, bun workspace, Taskfile, CI | done | Phase 0 checklist below; DoD (binary runs, UI on `:4648`) superseded by the full UI |
| **1 Routing** | SQLite store, accounts registry + OAuth, utls transport | done | `internal/{store,accounts,transport}` |
| | Quota poller with real per-provider usage fetchers | done | #298, #302 (Anthropic and ChatGPT usage windows, call budget); older "Fetcher is a stub" text is obsolete |
| | Routing engine: affinity, failover, route around exhausted accounts | done | `internal/routing`, #304 |
| | Proxy `/v1/messages` and `/v1/chat/completions` | done | `internal/server/proxy.go`, request log #226, base-URL/model routing #253 |
| | **DoD:** Claude Code via `ANTHROPIC_BASE_URL` works across 2 accounts with real failover | partial | Built and tested with fakes; **unverified** with two live accounts (`completed/oauth-account-login.md` Validation; `account-usage-windows` live check still open) |
| **2 Workspaces / tasks / web UI** | Data model, workspaces with git worktrees, auth, ACP + claude-native + codex spawn, WS RPC, web UI (panes, tree, timeline, files, editor, diff, terminal, permissions, preview), CLI | done | Phase 2 checklist below; `completed/web-ui-*`, `completed/pane-split-tree*.md` |
| | **DoD (re-scoped 2026-09-11):** dogfood smind on smind, 5 real sessions | done | `completed/smind-dogfood.md`, closed 2026-09-16 |
| | **DoD (original goal):** a real feature built end to end from the smind UI **instead of Paseo**, i.e. smind replaces Paseo for orchestration | partial | The task/run/queue/MCP/tree primitives exist (#248, #289, #291, #246) but the supervisor → lead → peer workflow still runs through Paseo: `orchestration-and-metering` Step 3 (lead + 2 peers via smind only) is not done, `orchestration-roles` is parked, and this repo's own work is still orchestrated from Paseo worktrees |
| **3 Relay E2EE + mobile** | Relay server, self-hostable, multi-device | done | #177, #345, #346 |
| | E2EE handshake, QR pairing, reconnect grace, key rotation | done | ADR-0007; E2EE v2 with a fresh daemon ephemeral key in #431/#433/#434 (`completed/relay-e2ee-v2.md`) |
| | Mobile: pairing, workspace/task list, realtime timeline, follow-up, **permission approval** | done | #175, #176, #178; relay device identity #350; ZCode redesign in progress (`active/mobile-zcode-redesign.md`) |
| | Mobile: **push notifications** | **missing** | No `expo-notifications` anywhere in `mobile/`; no design pass for E2EE-safe APNs/FCM payloads |
| | Mobile: **assign new work** from the phone | partial | Mobile only calls `run.start`/`run.stop`/`run.respondPermission`/`chat.queue*` and list methods; there is no `task.create`/`workspace.create` in `mobile/src` |
| | Deploy relay at `relay.spacingmind.sh` (ADR text says `.com`) | **missing** | `homelab-cli.md` P14–P16 blocked on ADR-0031 decision 12 (hosting); no `deploy/relay` in the tree. Only a one-off live test through a Cloudflare Tunnel was recorded (#150) |
| | **DoD:** from a phone off-network: assign work, get notified, approve | partial | Approve works over the relay; notify is missing; assign is follow-up only. Everything mobile is **unverified on a real phone or simulator** (every mobile Validation says so) |
| **4 Desktop + polish** | Tauri shell: tray/menu bar, global shortcut, native notifications, deep links, window state | done | `completed/desktop-tauri-shell.md`, `completed/desktop-quick-wins.md`, `completed/desktop-native-feel.md` |
| | Bundled UI, connection picker (local / url / ssh / relay) | done | `completed/desktop-bundled-ui.md`, `completed/desktop-relay-transport.md`, #357 |
| | App-managed daemon: macOS bundled sidecar; Windows native `smind.exe` bundled in the installer (no longer WSL2-first) | done | #238, #243, #311, #334, `completed/windows-bundled-daemon.md` |
| | Settings UI | partial | Done: accounts, providers, profiles, connections, daemon, notifications, shortcuts, MCP servers, usage, learned skills, appearance. **Missing:** pools and per-workspace routing policies |
| | **Auto-update** | done in code; **unverified** in a real window | Signed updater #274, hermetic e2e #377, `latest.json` + `.sig` published by the v0.13.1 release. Real in-window update, the Windows installer hand-off and the first real update are unverified (`active/desktop-auto-update.md`). The older "deferred" line is obsolete |
| | Code signing and notarization (macOS Developer ID, Windows Authenticode) | **missing** | Deliberately deferred: ad-hoc signature, personal-use builds |
| | Windows parity | partial | Done: native daemon (`completed/windows-daemon-runtime-parity.md`, full suite green nightly on Windows Server 2025 #366), bundled daemon + installer e2e (#334, #374), release zips for amd64 and arm64. **Unverified:** Windows 10 and the Windows 11 client, Defender/SmartScreen, an ARM64 installer (the amd64 daemon runs emulated), the Windows QA re-test (`active/windows-qa-terminal-keys-live-runs.md`), a real logged-in `claude` turn on Windows |
| | macOS and Linux | partial | macOS DMGs per arch ship each release; its Dock close/reopen, badge and quarantine flows are unverified (`active/desktop-macos-app.md`). Linux is `.deb` only, with no updater, and its chrome is unverified |
| **5 Automation (demand-driven)** | Heartbeats (task self-evaluates and continues) | missing | Nothing in the tree |
| | Schedules (cron creates tasks) | missing | Nothing in the tree |
| | Cross-provider subagents | partial | MCP primitives, parent/child, depth and tree guards, queue (ADR-0017/0018/0019/0021, #246, #289, #291); dogfood Step 3 open |
| | GitHub integration (PR status, checkout PR as task) | partial | PR creation from the UI (#96); `gh` client and `forge.search` (#459); ADR-0039 git/PR state in progress (`active/git-pr-state.md`, #469, #471) |
| | Per-workspace audit log | missing | Nothing in the tree |
| | Quota prediction | partial | ADR-0028: usage windows fetched, routing around exhausted accounts, UI (#298-#317). Pace prediction (`runsOutAt`) is explicitly out of scope; live-account check unverified |
| *Not on the original roadmap* | Self-learning skills + probe-before-approve (ADR-0026/0037), homelab distribution (ADR-0031: install scripts, `smind service`, Docker, `smind update`), prompt attachments (ADR-0038, in progress), task hierarchy tiers | mostly done | `completed/self-learning-skills.md`, `active/learning-probe.md`, `active/homelab-cli.md`, `active/prompt-attachments.md`, `active/task-hierarchy-tiers.md` |

### Biggest gaps (2026-10-22)

1. **Smind does not yet orchestrate its own work (Phase 2's original DoD).** The lead/peer workflow still runs through
   Paseo. Needs `orchestration-and-metering` Step 3 on a non-smind project and a decision on whether `orchestration-roles`
   is ever built.
2. **Phase 3's DoD is not met.** No push notifications (`expo-notifications` is absent), no way to create a task from the
   phone, no deployed relay (blocked on the hosting decision), and nothing verified on a real device.
3. **Desktop distribution trust.** No code signing or notarization on any platform. The updater is built but its real
   in-window path is unverified.
4. **Windows and Linux are CI-verified only.** No evidence of a Windows 10 or Windows 11 client run, Defender/SmartScreen
   behaviour, or an ARM64 installer. Linux has no updater.
5. **Phase 1's DoD was never run with two live accounts**, and the usage-window fetchers (ADR-0028) have never been checked
   against real provider responses.
6. **Phase 5 is untouched** (heartbeats, cron, audit log), as intended while demand-driven, plus the Settings gap
   (pools and per-workspace routing policies).

Open Human decisions behind these: ADR-0031 decision 12 (hosted relay) and 27–32/36; keep or close `orchestration-roles`;
the `learning-probe` `probeCheck` (AC33); whether to re-test Windows QA (#284) or close that plan.

## Phase 0 — Scaffold (week 1)

Goal: repo lives, binary runs.

- [x] Init repo `spacingmind/smind`: go.mod, cmd/smind, internal/{config,server}
- [x] Health endpoint `GET /healthz` listen :4648, config load from `~/.smind/config.yaml`
- [x] Bun workspace `web/` + Vite React placeholder, embed into binary
- [x] Taskfile: `task build` (1 binary); `task dev` (hot reload both)
- [x] CI: GitHub Actions — go test + bun build + lint
- [x] npm reserve `smind` stub

Definition of done: `smind` binary runs, `localhost:4648` shows placeholder UI.

## Phase 1 — Core routing (weeks 2-5) — *the heart*

Goal: proxy Anthropic/OpenAI-compatible requests across multiple accounts.

- [x] SQLite: accounts, routing decisions, quota snapshots (`internal/store`)
- [x] Accounts registry: typed credential registry + OAuth refresh (`internal/accounts`, on top of `internal/store` — superseded the original `~/.smind/accounts/` file-store idea)
- [x] TLS fingerprinting (utls) for outbound provider requests (`internal/transport`, HTTP/1.1 only)
- [x] Quota poller: TTL-cached per-account usage (`internal/quota`) — Anthropic and ChatGPT usage-window fetchers, a daemon-enforced call budget and routing around exhausted accounts landed in ADR-0028 (#298, #302, #304); the original stubbed `Fetcher` is gone
- [x] Routing engine: session affinity (24h TTL), failover chain, routing policy v1 (`hard`/`pool` fill-first) — `internal/routing`
- [x] Proxy endpoints: Anthropic `/v1/messages`, OpenAI `/v1/chat/completions` — wires `internal/routing` + `internal/accounts` + `internal/transport` into `internal/server`. Also added real OAuth token refresh (`internal/accounts`) for Anthropic, OpenAI, Kimi, xAI, and Antigravity (Google Vertex/Gemini skipped — needs a service-account JWT-bearer flow, not a refresh-token flow, tracked as a separate future task)

Phase 1 is functionally complete but not yet exercised with a real provider account: `smind account add`/`smind account ls` (paste a credential blob) and, as of `docs/plans/completed/oauth-account-login.md`, `smind account login <provider> <label>` plus the accounts dialog's "Connect" button (real browser-based OAuth for `anthropic`/`openai`, the two providers `proxy.go` actually routes) now manage local accounts through the daemon, but real two-account failover still needs real provider credentials to validate end-to-end -- untested against a live Anthropic/OpenAI account in this environment (no vendor accounts, no browser available here; see that plan's Validation section).

Definition of done: Claude Code pointed at `ANTHROPIC_BASE_URL=localhost:4648` works across 2 accounts with real failover.

## Phase 2 — Workspaces + Tasks + Web UI (weeks 6-10)

Goal: replace Paseo as daily driver.

- [x] Data model: Workspace/Space/Task + workspace_accounts (`internal/store`)
- [x] Workspace CRUD + Task lifecycle with real git worktrees (`internal/workspace`) — `create`/`run`/`archive`, not yet wired to CLI/HTTP
- [x] Auth: simple bearer token (`internal/auth`), gating the Phase 1 proxy endpoints
- [x] Agent spawning: ACP client (`internal/acp`, proven against real GLM) + Claude Code native headless client (`internal/claudecode`, protocol verified against `refs/claude-agent-sdk-python`) + `internal/taskrunner` unifying both behind one `RunPrompt(taskID, provider, prompt, events)` call. Codex uses its own "app-server" JSON-RPC protocol (distinct from both), not yet implemented — tracked as a follow-up. Google Vertex/Gemini also deferred (needs a service-account JWT-bearer credential type, not a refresh-token flow). Two real concurrency bugs (a close/send race in `acp.Client.Prompt`, an unguarded final-event send in `taskrunner.RunPrompt`) were found and fixed during review, each with a `-race`-verified regression test.
- [x] Extract Claude Code client into its own Go module/repo: [spacingmind/claude-agent-sdk-go](https://github.com/spacingmind/claude-agent-sdk-go) (public, MIT) — no official Go SDK for Claude Code exists yet. `internal/taskrunner` now depends on it externally.
- [x] WebSocket RPC API (`internal/wsapi`, `GET /ws`) exposing workspace/space/task CRUD and streaming `task.prompt` — the transport the web UI (and later the terminal feature) will drive. REST+SSE was tried first and discarded in favor of RPC-over-WebSocket (bidirectional need); gRPC ruled out for this layer (no browser support) but planned for daemon↔relay in Phase 3.
- [x] Web UI: split panes + tabs; workspace/space/task tree sidebar; agent timeline (streaming chat); file explorer with git status; CodeMirror 6 editor; custom per-hunk diff viewer; xterm terminal (PTY in task cwd); permission prompts UI
- [x] Web UI: preview pane (markdown, svg, sandboxed html alongside the CodeMirror editor) — built in #51, e2e-verified in the crud-ui Playwright pass
- [x] `smind` CLI: `task new`, `ls`, `attach`, `send`, `logs`, `stop` — run registry + streaming CLI over `internal/wsapi`

**Phase 2 complete (2026-09-16).** The dogfood gate was reached: five real
sessions logged through smind's UI (`docs/plans/completed/smind-dogfood.md`),
including the closing one that routed a Perplexity Pro subscription through
smind's own proxy as a provider (via
[perplexity-proxy-go](https://github.com/spacingmind/perplexity-proxy-go)
and per-account base_url, #140/#142).

Codex spawning status (2026-09-11): live-verified up to the real turn.
`codex app-server` v0.149.1 speaks stdio JSON-RPC directly (bare spawn
works — invalidating provider-codex's earlier "daemon+proxy required"
finding); smind's client completed a real initialize + thread/start and
received a real `turn/completed` (status Failed) with the account's
usage-limit error surfaced verbatim. The only unexercised step is a
quota'd successful agent turn, blocked on the account's Codex limit
(resets Oct 7, 2026) — protocol-level work is done.

Definition of done (re-scoped by user, 2026-09-11 — was "a real
scopedocs feature built end-to-end from smind UI"): **dogfood smind on
smind** — use smind (not Paseo) as the daily driver for real work on
this repo, closing the loop it was built for. A scopedocs feature
remains a fine future stress test but is no longer the Phase 2 gate.

## Phase 3 — Relay E2EE + Mobile (weeks 11-14)

Goal: usable from a phone outside the home network.

Transport note: daemon ↔ relay is a real service boundary (the relay is
separately deployable), so gRPC is the planned protocol there — unlike
the daemon's own client-facing API (Phase 2), which uses RPC-over-WebSocket
specifically because its primary client is a browser (gRPC has no native
browser support without a grpc-web proxy layer). Not implemented yet;
noted here so Phase 3 design starts from this rather than re-litigating it.

- [x] Relay server (Go, dumb pipe), self-hostable as `smind relay`
      (gRPC wire contract + admission auth per ADR-0011 + forwarding with
      bounded reconnect buffer + multi-device fanout; TLS cert + workspace
      enrollment CLI; daemon-side client with fingerprint pinning and
      session-resume semantics — see `docs/plans/completed/relay-e2ee-mobile.md`)
- [x] E2EE handshake: X25519 + ChaCha20-Poly1305, QR pairing
      (daemon↔mobile ends incl. URL-fragment QR offer with relay
      fingerprint pin; verified end-to-end through a real in-process TLS
      relay in the integration tests)
- [x] Reconnect grace, correct key rotation (bounded 200-frame buffer,
      in-order flush, oldest-evicted past cap; transport reconnect
      resumes the same session so buffered frames decrypt — ADR-0007 (e)
      amendment — while rotation starts a fresh session)
- [ ] Mobile app (Expo + @expo/ui): pairing + workspace/task list, realtime
      agent timeline + follow-up, push notifications, mobile permission approval
      (push notifications: ADR-0040 Accepted, `docs/plans/active/mobile-push-notifications.md`)
  - [x] Pairing, workspace/task list, realtime timeline, follow-up
        prompts, mobile permission approval (PRs #175, #176, #178) --
        design-token/native-control polish on top (PRs #181, #183)
  - [ ] Push notifications -- needs its own design pass (E2EE data-
        minimization for what reaches APNs/FCM); not started (2026-10-22:
        still no `expo-notifications` in `mobile/`)
- [ ] Deploy relay at `relay.spacingmind.sh` (Cloudflare TLS) -- blocked
      on ADR-0031 decision 12 (hosting); see `homelab-cli` P14–P16

Definition of done: from a phone off-network — assign new work, get notified, approve an agent.

## Phase 4 — Tauri desktop + polish (weeks 15-17)

Status as of v0.10.0 (2026-10-11):

- [x] Tauri 2 shell with tray, global shortcut, native notifications that
      navigate to the task, deep links, window state, and an app menu
      (`desktop-tauri-shell`, `desktop-quick-wins`).
- [x] Bundled UI plus a loopback proxy and a scoped per-command IPC
      bridge, with a connection picker for local, URL and relay daemons
      (ADR-0013, `desktop-bundled-ui`, `desktop-relay-transport`).
- [x] An app-managed local daemon: a bundled sidecar with auto-start on
      macOS, and (since 2026-10-13, ADR-0030) a bundled native `smind.exe`
      on Windows instead of WSL2. It updates only when no runs are in
      flight (`desktop-managed-daemon`, `desktop-macos-app` M2/M5,
      `windows-bundled-daemon`).
- [x] Native feel in the ZCode style:
  - custom window chrome with no launch flash (D1);
  - no webview-isms (D2);
  - desktop shortcuts and a native menu (D3);
  - Reveal/Open in editor and a taskbar badge (D4) (`desktop-native-feel`).
- [x] macOS app: per-arch DMGs (unsigned, ad-hoc), Dock lifecycle and a
      Dock badge (`desktop-macos-app`). First shipped in v0.10.0.
- [x] Native-call abstraction (`web/packages/ui/src/lib/platform.ts`,
      gated on `isDesktop`).
- [~] Settings UI. Done: accounts, providers, profiles, connections,
      daemon, notifications, shortcuts, MCP servers. Not done: pools and
      per-workspace routing policies.
- [x] Auto-update (ADR-0023): signed Tauri 2 updater (#274) and a hermetic
      end-to-end test (#377); releases publish `latest.json` and `.sig`
      files. The real in-window update is unverified
      (`desktop-auto-update`).
- [ ] Code signing and notarization (macOS Developer ID, Windows
      Authenticode). Deferred: the builds are for personal use for now.
- [ ] Manual verification still open: Windows/Linux chrome and
      integrations, and the macOS Dock badge and notification click (see
      each plan's Validation). Windows has since gained a native daemon,
      a bundled-daemon installer and an installer e2e on `windows-latest`;
      real Windows 10/11-client runs are still unverified.

## Phase 5 — Automation (week 18+, demand-driven only)

- [ ] Heartbeats: task self-evaluates and continues
- [ ] Schedules: cron creates new tasks (e.g. daily triage)
- [~] Subagents: session spawns session, cross-provider. The primitives
      exist:
  - `smind mcp serve` with `task_new`, `task_send`, `task_wait` and
    `task_stop`, plus an auto-approve guard (ADR-0017/0019);
  - task parent/child with a depth guard;
  - a per-chat prompt queue with `whenBusy` that survives restarts
    (ADR-0021);
  - agent MCP servers (ADR-0018);
  - per-run usage (ADR-0020).

  The remaining piece is dogfooding: Step 3 of
  `orchestration-and-metering`.
- [ ] GitHub integration: PR status / checkout PR as a task
- [ ] Per-workspace audit log
- [~] Quota prediction. Per-run and proxy usage are recorded, and the
      quota fetchers, poller and routing-around-exhaustion exist
      (ADR-0028, #298–#317, live-account check open). Pace prediction
      (`runsOutAt`) is out of scope. Design (Accepted):
      [ADR-0028](decisions/0028-account-usage-windows.md).

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
