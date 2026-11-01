# @smind/e2e

Playwright regression suite for the web UI (UAT phase 3, `docs/plans/active/uat-ui-phase3.md`). It drives the real web app served by a real `smind serve` with a fake agent: no model call, no provider secret, nothing under the real `~/.smind`.

## Run it

```sh
task build                                   # produces bin/smind with the web UI embedded
cd web/e2e
nice -n 10 bunx playwright test --workers=2   # default projects: light-1280 and dark-390
E2E_FULL=1 bunx playwright test               # adds dark-1280 and light-390
bunx playwright test specs/tabs.spec.ts -g J19
```

Environment:
- `SMIND_BIN`: the daemon binary (default `<repo>/bin/smind`).
- `PW_CHROMIUM_PATH`: use an already installed Chromium instead of the Playwright download (e.g. `~/Library/Caches/ms-playwright/chromium-1148/chrome-mac/Chromium.app/Contents/MacOS/Chromium`).
- `bunx playwright install chromium` once if you have no Chromium (pinned `@playwright/test` 1.49.1 expects revision 1148).

## How it is isolated

- One daemon per Playwright worker: a temp `SMIND_HOME` and `HOME` (marker file `.smind-e2e`), a free port, seeded over the websocket (workspaces alpha/beta/gamma, a finished task, a hanging run with queued prompts, a Lead with two Peers, ...). `fixtures/guard.ts` refuses a home outside the OS temp dir and the ports 4648/4870; `guard.spec.ts` covers it.
- The agent is `fixtures/uat-agent.cjs` (scenarios `default`, `hang`, `slow`, `permission`, `error`, `long`, read from a `scenario` file in the task worktree, written before the prompt). A spec that sends always asserts the provider chip is UAT, never Claude Code.
- `stop()` kills the daemon's process group and removes the temp home; an aborted run leaves no process or directory (`specs/daemon-lifecycle.spec.ts`).

## Rules for specs

- Title starts with the journey id (`J19`, `A-01`, ...). No `waitForTimeout` (`bun run lint` fails on it): web-first assertions and `waitForRpc`.
- Narrow-only specs skip the 1280 projects and the reverse.
- Mutating specs create their own task; never consume or archive the seeded ones.
- A spec that passes only on retry is reported in `test-results/flaky.json` (CI retries once).

## CI

- `.github/workflows/e2e.yml` (job `e2e`, non-required, not on the merge queue): on PRs touching `web/**` or the daemon paths it runs the PR subset (`light-1280`, `dark-390`) with `--grep-invert @flaky`, 12-minute cap. The nightly (03:17 UTC) and a manual dispatch with `full` run all four projects and the `@flaky` specs, 35-minute cap. A red nightly opens or comments on the `e2e-nightly` issue.
- Every run writes a job summary (totals, slowest 5 specs, flaky table, the first failing spec's `bunx playwright show-trace` command) via `scripts/flake-report.mjs --summary`. A failing run uploads `web/e2e/test-results/` as `e2e-failures-<run>` (7 days); every run uploads `results.json` and `flaky.json` as `e2e-timing-<run>` (30 days).
- `.github/workflows/e2e-weekly-report.yml` (Mondays 04:23 UTC) downloads the timing artifacts of the last 20 runs and runs `flake-report.mjs --weekly`: flake rate and p50/p95 per spec, plus `Quarantine candidates` (more than 2 flaky runs in 20). With at least one candidate it opens or updates one `e2e-flaky` issue.
- The report script has its own tests: `bun run test:scripts` (or `node --test web/e2e/scripts/*.test.mjs`).

### Quarantine

Put `@flaky` in a spec's title (`test('J19 tabs @flaky', ...)`). PRs run with `--grep-invert @flaky`, so it stops blocking them; the nightly passes no grep-invert and still runs it. Fix or delete it within 14 days; remove the tag when the weekly report no longer lists it.

## Seeded-regression check

To prove a spec is load-bearing, reintroduce the bug in `web/packages/ui/src`, rebuild (`bun run --filter @smind/ui build && go build -o <bin> ./cmd/smind`), and run the spec against that binary: it must fail. The PR that added the suite records this for A-02, B-01, A-01 and the Jump-to-latest overlap.

## Demo smoke (web demo mode)

`specs-demo/` is a separate suite for the web demo build (ADR-0051, `docs/plans/active/web-demo-mode.md`): the real UI against the scripted in-browser daemon in `web/packages/ui/src/demo/`. It needs no daemon, no Go and no network, so none of the isolation rules above apply: its `webServer` builds `web/packages/ui/dist-demo` (`build:demo`, which also runs the forbidden-string and size check) and serves it with `vite preview` on `127.0.0.1:4173`.

```sh
cd web/e2e
bunx playwright test -c playwright.demo.config.ts           # or: bun run test:demo
E2E_FULL=1 bunx playwright test -c playwright.demo.config.ts  # adds dark-1280 and light-390
DEMO_FRAMES_DIR=<dir> bunx playwright test -c playwright.demo.config.ts -g DM-12   # writes the review frames
```

Preview it by hand: `bun run --filter @smind/ui build:demo && bun run --filter @smind/ui preview:demo`, then open `http://127.0.0.1:4173/`.
