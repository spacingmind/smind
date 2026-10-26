# Roadmap

## Status as of 2026-10-27

Audited against `develop` @ `91ae5ca0` (release v0.15.0, tag dated 2026-10-26; v0.14.0 was cut 2026-10-22, #446), merged PRs, the plans in `docs/plans/` and the code itself. **done** = built and
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
| | **DoD:** Claude Code via `ANTHROPIC_BASE_URL` works across 2 accounts with real failover | partial | Built and tested with fakes; **unverified** with two live accounts (`completed/oauth-account-login.md` Validation; `active/account-usage-windows.md` live check still open, needs the Human's go-ahead) |
| **2 Workspaces / tasks / web UI** | Data model, workspaces with git worktrees, auth, ACP + claude-native + codex spawn, WS RPC, web UI (panes, tree, timeline, files, editor, diff, terminal, permissions, preview), CLI | done | Phase 2 checklist below; `completed/web-ui-*`, `completed/pane-split-tree*.md` |
| | **DoD (re-scoped 2026-09-11):** dogfood smind on smind, 5 real sessions | done | `completed/smind-dogfood.md`, closed 2026-09-16 |
| | **DoD (original goal):** a real feature built end to end from the smind UI **instead of Paseo**, i.e. smind replaces Paseo for orchestration | partial | ADR-0041 Accepted 2026-10-23 and built (`completed/in-smind-orchestration.md`, closed 2026-10-23): the seat contract and role pack (#561, #564-#567), parent notifier (#552), rate-limit resilience and failover (#541, #549, #555, #557), and a Lead → Peers run built entirely inside smind (#570). Then the supervisor seats were folded: Lead is the single loop and a Peer returns a json handoff (#617, `active/single-loop/PLAN.md`; its after-check and the `reviewer` decision are open). **Open:** a Steward outside smind (a Paseo agent) still briefs the Leads and reads PRs, `orchestration-and-metering` Step 3 on a non-smind project is not done, and `orchestration-roles` R5/R6/R8 stay parked by design |
| **3 Relay E2EE + mobile** | Relay server, self-hostable, multi-device | done | #177, #345, #346 |
| | E2EE handshake, QR pairing, reconnect grace, key rotation | done | ADR-0007; E2EE v2 with a fresh daemon ephemeral key in #431/#433/#434 (`completed/relay-e2ee-v2.md`) |
| | Mobile: pairing, workspace/task list, realtime timeline, follow-up, **permission approval** | done | #175, #176, #178; relay device identity #350; ZCode redesign closed (`completed/mobile-zcode-redesign.md`, #467); read-only task tree (#594) |
| | Mobile: **push notifications** | partial | ADR-0040 Accepted 2026-10-22; PRs 1-6 merged (#481, #487, #499, #520, #493, #517, #521): E2EE-sealed payloads, `push.*` RPCs, `smind push test`, Android-first native wiring. AC1-AC23 and AC25 covered by tests (`active/mobile-push-notifications.md`). **Open:** AC24, the native checklist on a real Android device (never run; needs the Human's Firebase/Expo project), and iOS (PR 8, blocked on Human decision H2: no Apple Developer Program) |
| | Mobile: **assign new work** from the phone | done in code; **unverified** on a device | New-task form with `whenBusy: queue` (#480, `completed/mobile-create-task.md`, status completed 2026-10-22, native verification the one open item); prompt attachments on mobile (#581, `active/mobile-prompt-attachments.md`, device run open) |
| | Deploy relay at `relay.spacingmind.sh` (ADR text says `.com`) | **missing** | `homelab-cli.md` P14–P16 blocked on ADR-0031 decision 12 (hosting); no `deploy/` directory in the tree. Only a one-off live test through a Cloudflare Tunnel was recorded (#150) |
| | **DoD:** from a phone off-network: assign work, get notified, approve | partial | All three exist in code (assign #480, notify #517 Android-first, approve #178), but notify has never run on a real device or with iOS, and everything mobile is **unverified on a real phone** (every mobile Validation says so); no deployed relay |
| **4 Desktop + polish** | Tauri shell: tray/menu bar, global shortcut, native notifications, deep links, window state | done | `completed/desktop-tauri-shell.md`, `completed/desktop-quick-wins.md`, `completed/desktop-native-feel.md`; later fixes #589-#591, #601 |
| | Bundled UI, connection picker (local / url / ssh / relay) | done | `completed/desktop-bundled-ui.md`, `completed/desktop-relay-transport.md`, #357 |
| | App-managed daemon: macOS bundled sidecar; Windows native `smind.exe` bundled in the installer (no longer WSL2-first) | done | #238, #243, #311, #334, `completed/windows-bundled-daemon.md` |
| | Settings UI | partial | Done: accounts, providers (with provider instances and per-provider launch env, #628, #603), profiles, connections, daemon, notifications, shortcuts, MCP servers, usage, learned skills, GitHub token (#548), plugins (#635), appearance. **Missing:** pools and per-workspace routing policies |
| | **Auto-update** | done in code; **unverified** in a real window | Signed updater #274, hermetic e2e #377, `latest.json` + `.sig` published by releases. Real in-window update, the Windows installer hand-off and the first real update are unverified (`active/desktop-auto-update.md`) |
| | Code signing and notarization (macOS Developer ID, Windows Authenticode) | **missing** | Deliberately deferred: ad-hoc signature, personal-use builds (`install.sh --desktop` for the un-notarized app, #478) |
| | Windows parity | partial | Done: native daemon (`completed/windows-daemon-runtime-parity.md`, full suite green nightly on Windows Server 2025 #366), bundled daemon + installer e2e (#334, #374), release zips for amd64 and arm64. **Unverified:** Windows 10 and the Windows 11 client, Defender/SmartScreen, an ARM64 installer, the Windows QA re-test by the Human (`active/windows-qa-terminal-keys-live-runs.md`), a real logged-in `claude` turn on Windows |
| | macOS and Linux | partial | macOS DMGs per arch ship each release; its Dock close/reopen, badge and quarantine flows are unverified (`active/desktop-macos-app.md`). Linux is `.deb` only, with no updater, and its chrome is unverified |
| **5 Automation (demand-driven)** | Heartbeats (task self-evaluates and continues) | missing | No scheduler or heartbeat code in `internal/` (checked 2026-10-27); ADR-0041 notifier (#552) wakes a parent on a child event, which is not a heartbeat |
| | Schedules (cron creates tasks) | missing | Nothing in `internal/` or `cmd/` |
| | Cross-provider subagents | done | MCP primitives, parent/child, depth and tree guards, queue (ADR-0017/0018/0019/0021, #246, #289, #291), and the in-smind seat workflow (ADR-0041, `completed/in-smind-orchestration.md`); child tasks branch from the parent (G18, #610, #611); opt-in tree spend cap (#602, #604, `active/tree-budget.md`); stalled-tree alert (#605, `completed/stalled-tree-alert.md`). `orchestration-and-metering` Step 3 on a non-smind project is still open |
| | GitHub integration (PR status, checkout PR as task) | done | ADR-0039 (`completed/git-pr-state.md`, G8 archive cleanup #544/#545, G12 token login #547/#548): stored task base (#469), forge interface (#471), `task.git` (#482), PR poller (#503), chips (#497), Start from branch/PR/issue (#537), Create/Update PR (#538); since 2026-10-22: Push and Update from base G7 (#582, #583, #585), PR panel G9 (#588, #593; `active/pr-panel.md`), agent git approvals G13 (#595, #596; `active/agent-git-approvals.md`), stacked child tasks G18 (#610, #611), Draft commit message with agent G17 (#606, `completed/ai-commit-draft.md`), `smind task archive` (#586), squash-merge lookup (#599). Real-GitHub success paths (a valid token, a real `gh pr create`, `task.prDetails`) are unverified by decision. **Open:** G13 on mobile (AC5) and decision D1 (commit allowed in acceptEdits/auto); GitLab |
| | Per-workspace audit log | missing | Nothing in the tree |
| | Quota prediction | partial | ADR-0028: usage windows fetched, routing around exhausted accounts, UI (#298-#317); inferred reset marker (#600). Pace prediction (`runsOutAt`) is explicitly out of scope; live-account check unverified |
| *Not on the original roadmap* | Self-learning skills + probe-before-approve (ADR-0026/0037; probe #450, #452, #448, `active/learning-probe.md`), homelab distribution (ADR-0031: install scripts, `smind service`, Docker, `smind update`; `active/homelab-cli.md`), prompt attachments (ADR-0038: daemon #463, images #473, web #465, `@` mention and picker #476, mobile #581; `active/prompt-attachments.md`, AC36 mobile, AC37, AC38 live checks open), task hierarchy tiers (`active/task-hierarchy-tiers.md`), plugin system (ADR-0042 Accepted 2026-10-25: Wave 0 provider registry and instances #627, #628; Wave A PA.1-PA.5 #630, #631, #634, #635, #636; **Wave B-D not started**, they wait for a Human decision; `active/plugins.md`), public user docs (`public-docs/`, #621-#625; `active/public-docs.md`) | mostly done | the plans named in the cell; ADR-0043 (opt-in AI commit message from the daemon) was Rejected 2026-10-25 |

### Biggest gaps (2026-10-27)

1. **Phase 3's DoD is still not met, but the gap moved from "missing" to "unverified".** Assign (#480) and notify (#517)
   now exist; push has never run on a real Android device (AC24), iOS push is blocked on Human decision H2 (no Apple
   program), and there is still no deployed relay (ADR-0031 decision 12). Nothing mobile is verified on a real device.
2. **Smind orchestrates its own work, with a human-run Steward outside it.** ADR-0041 and the single-loop fold (#617)
   landed; what is open is `orchestration-and-metering` Step 3 on a non-smind project, the single-loop after-check and the
   `reviewer` seat decision (`active/single-loop/PLAN.md`), and the parked `orchestration-roles` R5/R6/R8.
3. **Real-account and real-device checks never ran.** Phase 1's two-account failover and the ADR-0028 usage fetchers
   (`active/account-usage-windows.md`, needs the Human's go-ahead), real GitHub paths for `task.prDetails` and PR creation
   (`active/pr-panel.md`), a live-model run crossing a tiny tree budget (AC13, `active/tree-budget.md`), the attachment live
   checks AC37/AC38 (`active/prompt-attachments.md`), and device runs for mobile attachments
   (`active/mobile-prompt-attachments.md`).
4. **Desktop distribution trust.** No code signing or notarization on any platform. The updater is built but its real
   in-window path is unverified (`active/desktop-auto-update.md`).
5. **Windows and Linux are CI-verified only.** No evidence of a Windows 10 or Windows 11 client run, Defender/SmartScreen
   behaviour, or an ARM64 installer; the Windows QA re-test (#284) has no record. Linux has no updater.
6. **Plugins Waves B-D and public-docs polish.** Waves B-D (`active/plugins.md`: PB.1-PB.4, PC.1-PC.3, PD.1, DOC.1) wait for
   the Human; the public docs exist (#621-#625) with `plugins.md` still a stub.
7. **Phase 5 is untouched** (heartbeats, cron, audit log), as intended while demand-driven, plus the Settings gap
   (pools and per-workspace routing policies).

Open Human decisions behind these: ADR-0031 decision 12 (hosted relay) and 27–32/36; iOS push H2 (Apple program) and a
Firebase/Expo project for AC24; the live probe go-ahead for account usage; Plugins Waves B-D; agent git approvals D1;
the `reviewer` seat and `orchestration-roles` (keep or close); the `learning-probe` `probeCheck` (AC33); whether to
re-test Windows QA (#284) or close that plan.

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
  - [x] New task from the phone (#480)
  - [~] Push notifications -- designed (ADR-0040) and built Android-first
        (#481, #487, #499, #520, #493, #517); AC24 on a real device and iOS
        (blocked on Human decision H2) are open (2026-10-27)
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
- [x] Subagents: session spawns session, cross-provider. The primitives
      exist:
  - `smind mcp serve` with `task_new`, `task_send`, `task_wait` and
    `task_stop`, plus an auto-approve guard (ADR-0017/0019);
  - task parent/child with a depth guard;
  - a per-chat prompt queue with `whenBusy` that survives restarts
    (ADR-0021);
  - agent MCP servers (ADR-0018);
  - per-run usage (ADR-0020).

  Since 2026-10-23 the in-smind seat workflow (ADR-0041, #561-#571,
  #617) runs Lead and Peers inside smind. Still open: Step 3 of
  `orchestration-and-metering` on a non-smind project.
- [x] GitHub integration: PR status / checkout PR as a task. Shipped as
      ADR-0039 (`completed/git-pr-state.md`, `completed/archive-merged-branches.md`,
      `completed/github-token-login.md`); push/update-from-base, the PR panel,
      agent git approvals and GitLab were separate work: push/update-from-base
      (G7, #582, #583), the PR panel (G9, #588, #593) and agent git approvals
      (G13, #595, #596) have since merged; GitLab is unstarted.
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
