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

## Seeded-regression check

To prove a spec is load-bearing, reintroduce the bug in `web/packages/ui/src`, rebuild (`bun run --filter @smind/ui build && go build -o <bin> ./cmd/smind`), and run the spec against that binary: it must fail. The PR that added the suite records this for A-02, B-01, A-01 and the Jump-to-latest overlap.
