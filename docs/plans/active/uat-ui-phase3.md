# UAT UI phase 3 — the Playwright regression suite in CI

Parent: [uat-ui.md](uat-ui.md) (phase 1 catalogue and harness, phase 2 fixes). This is the spec for the last phase: turn the journeys that found real bugs into a regression suite that runs in CI against a real daemon with a fake agent. Nothing in this PR changes code; it asks the Human for the decisions marked **Decision**.
Size: L (7) — scope 2 (new e2e package, CI job, a daemon fixture), PRs 2 (suite plus the CI job; ordering matters), risk 1 (a new CI job on every PR, flake risk), decisions 1 (new dev dependency, required-check policy), verification 1 (the suite is its own verification).

## What phase 1 and 2 left us

- A harness under `~/.smind/dogfood/uat/` (not in the repo): a built daemon with its own `SMIND_HOME` and port, a node ACP fake agent (`agent/uat-agent.cjs`: default, hang, slow, permission, error, long), three fake repos, a seed script, and about 55 journey scripts (`journeys/jNN.cjs`) driven by a small Playwright helper (`lib/capture.cjs`: a themed context, console/pageerror/failed-request log, a WebSocket injector for wsapi methods the isolated daemon lacks). 54 journeys × light/dark × 1280/390 ran in about 25-30 minutes locally, sequentially.
- Phase 2 fixed 20+ bugs, each with a vitest regression. vitest uses jsdom, which has **no layout, no focus rendering, no real WebSocket and no real daemon**: 12 of the fixes could only assert a class name ("structural"). Those are exactly what a real-browser suite must lock.
- Playwright is not a dependency of the repo today (the harness borrows an install under `/tmp/steward4/pw` and a cached Chromium). CI (`.github/workflows/ci.yml`, job `ci`: build, test, lint, version check; ~2-3 min; the merge queue runs only `ci`) has no browser job.

## Acceptance criteria

- AC1 A `web/e2e/` workspace package (`@smind/e2e`) with `@playwright/test`, a `playwright.config.ts`, fixtures and the specs below; `bun run --filter @smind/e2e test` runs it locally against a freshly built `smind` binary.
- AC2 A global setup starts one isolated daemon per worker group on a free port with a temp `SMIND_HOME`, fake agent only, seeded through wsapi; it refuses to run if `SMIND_HOME` is not under the OS temp dir or the port is not one it allocated (the phase 1 guard, in TypeScript). No model call and no provider secret: a spec run with no network must pass.
- AC3 Each spec carries its journey id (`J10`...) in the title and its project name encodes theme and width (`light-1280`, `dark-390`).
- AC4 A CI job `e2e` builds the binary and runs the PR subset (below) on chromium; the nightly job runs the full matrix. Failing runs upload screenshots, traces and the daemon log as an artifact.
- AC5 The PR subset finishes in under 8 minutes on a standard runner (budget below); the full matrix in under 30.
- AC6 The flake policy below is encoded in the config (retries, quarantine tag) and a weekly report lists the flake rate per spec.
- AC7 Every phase 2 fix that was only structurally tested in vitest has a real-browser spec that fails when the fix is reverted (one mutation per spec, recorded in the PR).

## Which journeys become specs

Rule: a journey becomes a spec when it (a) found a bug in phase 1, or (b) is a P0 core flow, and (c) can be made deterministic with the fake agent. Everything else stays a manual/exploratory journey in the harness.

PR subset (runs on every PR touching `web/`, `internal/wsapi`, `internal/server`, `internal/runs`; chromium; projects `light-1280` and `dark-390`): 

| spec | journeys | locks |
|---|---|---|
| smoke | J01 welcome, J04 add workspace, J06 create task, J07 send prompt and read the timeline | the main flow end to end |
| permissions | J10/J11 approve and deny, J12 inbox (Open task), the 5-minute expiry is not waited for (the daemon timeout is shortened by config in the fixture) | A-08 |
| queue-stop | J13 queue while busy, J14 cancel a queued prompt, J15 stop, J45 reconnect after daemon restart, J46 reload mid-run | restart/queue/stop data-loss class |
| tabs | J19 terminal opens ONE tab, J24 diff chip opens the Diff tab, J22/A-05 explorer order, J39 archive the selected task | A-02, A-03, B-01, A-05 |
| routing | J47 unknown task id shows Task not found, back/forward, J51 daemon unreachable shows Retry | B-02, B-03, token never printed in the banner |
| narrow | J48 390 layout sweep, J06 palette New task at 390, J04 folder picker at 390, J42 invite dialog, start-from labels, tap targets ≥ 36px, Jump to latest slot never overlaps (geometry assertions) | A-01, A-09, B-07, B-10, pill, A-13 |
| a11y | J49 keyboard-only main flow, focus ring on Permission mode and collapse toggle, `@axe-core/playwright` AA colour-contrast scan of sidebar, timeline, inbox, settings (the phase 2 token fix) | B-05, B-08 |
| tool-call | edit tool call renders an inline diff (A-10), the resume note is its own paragraph (A-12) | A-10, A-12 |

Nightly adds: the full J01-J54 catalogue in the four combos, Hosts/pairing (fleet stubbed through a WebSocket route), Automations off card (restart line per `managedBy`), long timeline scroll, and the visual-regression screenshots below.

Out of scope (stay manual): the desktop shell and the WKWebView PDF check (J56 needs a native window), anything needing `gh`/a forge, a real provider, push notifications, the mobile app.

## How the isolated daemon and fake agent are built in CI

1. `task build` already produces `smind` (web UI embedded). The e2e job reuses that build step and the artifact, then runs `bun install --frozen-lockfile` in `web/` and `bunx playwright install --with-deps chromium` (cached by the Playwright version in `~/.cache/ms-playwright`).
2. The fake agent moves into the repo as `web/e2e/fixtures/uat-agent.mjs` (the phase 1 `uat-agent.cjs`, scenario files unchanged: default, hang, slow, permission, error, long) and runs with the node already on the runner. The Go `internal/taskrunner/fakeagent` stays for Go tests; two agents are acceptable because they serve different harnesses (the node one needs no Go build and is copy-pastable by spec authors). **Decision for the Human**: keep both, or port the scenarios into the Go fakeagent and build it in CI (one agent, slightly slower startup).
3. The daemon fixture (`fixtures/daemon.ts`): temp dir, `config.yaml` with `server.port: <free>`, a custom provider `uat` pointing at the agent, `HOME` set to the temp dir, spawn `smind serve`, wait for `/healthz`, seed (workspaces from three temp git repos created with `GIT_CONFIG_GLOBAL=/dev/null`, tasks, a Lead tree, queued items, a permission request) over the websocket RPC, expose `{url, token, ids, restart(), stop()}`. One daemon per Playwright worker so specs never share state; mutating specs create their own task instead of consuming seeded ones.
4. The WebSocket injector (`routeWebSocket`) for fleet and automations pages is promoted to a typed fixture; it never replaces core flows.
5. Determinism: the fake agent has no clock-driven output except the `slow` scenario (fixed 300 ms), the daemon permission timeout is shortened by a test-only config value (or the `SMIND_TEST_*` knob that already exists for permission tests, to be checked), `Date` is not mocked (relative times are asserted by regex).

## Runtime budget

Measured locally: one journey × one combo about 14 s (browser launch, seed state, steps, two screenshots). Playwright reuses the browser and the daemon per worker, which should cut that to 4-6 s.
- PR subset: about 22 specs × 2 projects = 44 runs × 6 s ≈ 4.5 min of work, 2 workers on a 2-vCPU runner ≈ 2.5-3 min plus ~1.5 min setup (install, browser cache hit, build artifact download) — **target ≤ 6 min wall, hard cap 8**.
- Nightly full matrix: 55 journeys × 4 combos = 220 runs ≈ 22 min of work on 2 workers ≈ 12-15 min; cap 30.
- If the PR subset exceeds the cap: shard by project (`--shard=1/2`) before dropping specs.

## Flake policy

- No `waitForTimeout`; web-first assertions only (`expect(locator).toBeVisible()`, `toHaveText`), and every await on the daemon goes through a `waitForRpc` helper keyed on the request, not a sleep (lint rule in the e2e package).
- CI config: `retries: 1`, `trace: 'on-first-retry'`, `video: 'off'`, `screenshot: 'only-on-failure'`, `fullyParallel` across files, one daemon per worker. Locally `retries: 0`.
- A spec that passes only on retry is a **flake**: the run is green but the reporter writes it to a `flaky.json` artifact; more than 2 flakes in the last 20 runs of one spec quarantines it (tag `@flaky`, run in the nightly job only, non-blocking) and opens an issue assigned to the spec's owner. A quarantined spec must be fixed or deleted within 14 days.
- Timeouts: action 5 s, expect 7 s, test 45 s; the global job timeout 12 minutes (the job is killed, not retried).
- Visual regression is nightly only and never blocks a PR: `toHaveScreenshot` with `maxDiffPixelRatio` 0.01 on 6 frames (sidebar, timeline, inbox in light/dark at 1280), baselines stored as a CI artifact and regenerated by a manual workflow (no binary files in git: AGENTS.md rule on binaries).
- Weekly report (a scheduled workflow or a small script over the artifacts) prints flake rate and p95 duration per spec.

## Artifacts

On failure the job uploads `web/e2e/test-results/` (screenshots, traces, the per-worker `daemon.log`, console logs, network failures) as `e2e-failures-<run>` with 7-day retention, and prints the first failing spec's trace command in the job summary (`bunx playwright show-trace`). Passing runs upload nothing except the nightly timing report.

## Decisions for the Human (with my recommendation)

1. **New dev dependency**: `@playwright/test` (+ `@axe-core/playwright`) in `web/` and a browser download in CI. Recommendation: yes; they are dev-only and pinned, the browser is cached.
2. **Required-check policy**: recommendation — start `e2e` as a **non-required** check on PRs (path-filtered), keep it OFF the merge queue (the queue runs only `ci` to stay at ~2-3 min), make it required after 20 consecutive green runs with a flake rate under 2% and p95 under 6 min. The nightly job is never required; a red nightly opens an issue.
3. **Agent**: keep the node fake agent (recommended, zero Go build) or port to the Go fakeagent.
4. **Where the specs live**: `web/e2e/` as its own workspace package (recommended: keeps vitest and Playwright runners separate), or under `web/packages/ui/e2e/`.
5. **Runner OS**: ubuntu only (recommended). macOS/Windows webviews are covered by the existing desktop workflows and the manual J56 check.

### Recorded answers (Supervisor default 2026-11-01, Human may veto)

1. Pinned `@playwright/test` 1.49.1 (the cached Chromium revision) and `@axe-core/playwright`, with cached browsers in CI.
2. A non-required, path-filtered PR check that becomes required after 20 consecutive greens with flake < 2% and p95 < 6 min, plus a nightly full matrix never required (a red nightly opens an issue). The repo is public, so Actions minutes are free.
3. The node fake agent (`web/e2e/fixtures/uat-agent.cjs`).
4. `web/e2e/` as its own workspace package.
5. Ubuntu only.

## Test scenarios (named, for the suite itself)

- `fixture_refuses_foreign_home`, `fixture_refuses_reserved_port`: the guard.
- `fixture_seed_is_idempotent_per_worker`: two workers do not share ids or ports.
- `suite_runs_offline`: with the network disabled the PR subset passes (no model call, no provider secret read).
- `mutation_revert_fix_fails_spec` per row of the table (AC7): reverting the fix named in "locks" fails that spec.
- `flake_report_lists_retried_pass`: a spec forced to pass on retry shows up in `flaky.json`.

## Progress

- [x] Decisions 1-5 recorded (Supervisor default 2026-11-01, Human may veto).
- [x] PR A: `web/e2e/` package, daemon fixture, guard, agent, the PR subset, a local README (no CI change). 65 tests per run in light-1280 and dark-390, about 95 s wall with 2 workers under `nice`; 5 consecutive full runs green, 0 flakes (`flaky.json` empty), no daemon, browser or temp dir left behind.
- [ ] PR B: the `e2e` CI job (non-required), artifacts, nightly workflow, flake report.
- [ ] Mutation per spec recorded; budget measured on a real runner and written back here.

## Validation

PR A (the suite):
- 5 consecutive full local runs (2 workers, `nice -n 10`, this Mac under load): 65 passed, 5 skipped by design (narrow/desktop-only specs), 0 flaky, 94-98 s wall; after each run `pgrep` shows no smind-e2e daemon or Chromium and `$TMPDIR/smind-e2e-*` is empty.
- Seeded regressions (each rebuilt into a separate binary, the spec must go red, then restored): A-02 first terminal back to the indexed `Terminal 2` tab fails `J19/A-02` (both projects); B-01 the `task.archived` selection move disabled fails `J39/B-01` (both); A-01 the CRUD dialogs moved back inside the mobile Sheet fails both `A-01` palette specs at 390; the Jump-to-latest pill made `absolute` over the log fails the strengthened `Jump to latest pill` spec (4 of 4 runs) — the first version of that spec passed the same mutation and was rewritten to assert real overlap and layout stability. The unmutated control passes 12/12.
- Bugs and gaps the suite surfaced while being written: J39 failed once in five runs because the daemon refuses to archive a task whose run is still `running` while the UI already shows it finished (the spec now waits for the daemon's own run status); J49 found a keyboard-focusable scroll region with no visible ring (B-11, P3, tolerated and annotated); the daemon fixture re-created its temp home through provider probes that outlived it (fixed with a process-group kill).
- Not yet measured: runtime and flake rate on a real CI runner (PR B).
