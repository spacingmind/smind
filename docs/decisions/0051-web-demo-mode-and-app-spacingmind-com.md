# 0051: Web demo mode and app.spacingmind.com (demo only)

## Status

**Proposed — 2026-11-01** (implemented, unmerged, in PR #797). The *direction* is the Human's (2026-11-01): the landing page gets a **hybrid demo** — the hero stays a light
coded mock (landing PR #34, another Lead), plus a "Try it live" entry that opens the **real** smind web UI (`web/packages/ui`) in **demo
mode**, hosted at `app.spacingmind.com`, **demo only for now**. This ADR is the design the Human reviews; the demo is built as
*Proposed* in parallel (`docs/plans/active/web-demo-mode.md`), so a change requested in review is a change to unmerged code.

Open for the Human in this ADR: the **hosting option** ([section 8](#8-hosting)) and the **infra steps** ([section 9](#9-the-humans-infra-steps)).
Everything else is a Lead default, listed so it can be vetoed.

**Not decided here, and not built:** a real hosted web app that connects to users' daemons and an official relay at
`relay.spacingmind.com`. That is [ADR-0052](0052-hosted-web-app-and-official-relay.md) (Proposed, a draft for the Human). Nothing in this
ADR makes `app.spacingmind.com` able to reach a daemon, and [section 10](#10-security) forbids the demo build from containing a path that
could.

Inputs: ADR-0002 (bun workspaces for web), ADR-0005 (wsapi event subscription), ADR-0008 (structured run events), ADR-0013 (the bundled UI,
same-origin `CheckOrigin`), ADR-0019 (an agent never answers a permission), ADR-0031 D8/D11, ADR-0045 (inbox), `web/packages/ui/src/App.tsx`
(`connect` prop), `src/lib/ws-client.ts`, `src/test/fake-ws-client.ts`, `src/test/fake-socket.ts`.

## Context

The landing page needs a way for a visitor to *feel* smind in thirty seconds without installing a daemon, signing in or giving us an API
key. A coded mock of the hero is cheap but it is a second UI that drifts from the product. The real UI is the product; what a visitor needs is
a daemon that is not there.

What the UI needs from a daemon is already a clean seam:

- `App` takes `connect: () => Promise<WsClient>` (default `connectDaemon`) and uses it for the first connect **and** for every reconnect
  (`watchForReconnect`), `App.tsx:168-171`. Tests already pass a scripted `connect` that wraps a `FakeSocket` in a real `WsClient`
  (`App.test.tsx`).
- `WsClient` (`lib/ws-client.ts`) speaks one JSON envelope per message over a `SocketLike` (`send`, `close`, `addEventListener`). Requests,
  streaming events (`{id, event, params}`), terminal results, cancellation (`task.cancel`) and ADR-0005 notifications (`{event:{topic,seq,payload}}`)
  are all just envelopes on that socket.
- The UI calls about 170 distinct RPC methods (`workspace.list`, `task.get`, `run.start`, `run.respondPermission`, `task.diff`,
  `terminal.attach`, `inbox.list`, `usage.summary`, ...), most of them from settings pages a demo does not need.
- `FakeWsClient` (`test/fake-ws-client.ts`) is the existing test double for the *call* surface: it records calls and leaves them pending for a
  test to resolve. That is the right shape for a test and the wrong shape for a demo (nobody is there to resolve anything), but it shows the
  contract a demo daemon must honour: `call`, `callStream`, `onNotification`.

The landing site's own hosting pattern is known (`spacingmind-web` Worker, `apps/web/wrangler.jsonc`: `assets`, `routes` with
`custom_domain: true` for `spacingmind.com` and `www`). Paseo, the reference product, hosts its web app at `app.paseo.sh` as a static
bundle deployed by a workflow with a Cloudflare token (`refs/paseo/.github/workflows/deploy-app.yml`, Cloudflare Pages) — the same shape.

## Decision

### 1. Demo mode is a *daemon-shaped fake*, plugged in at the socket

A **`DemoSocket`** implements `SocketLike`. It receives the request envelopes `WsClient` sends and answers them like a daemon would, from a
**scenario** (section 2). `connectDemo()` is `async () => new WsClient(new DemoSocket(scenario))`. The real `WsClient` — id correlation,
streaming, cancel, notifications, `onClose` — runs **unmodified**; so do `App`'s connect state machine, the reconnect wrapper, the query
cache, `useDaemonEvents` and every component. `App.tsx` gains no demo branch for connecting: `main.tsx` passes `connect={connectDemo}`.

Why the socket and not `WsClientLike`: `App`'s prop type is the concrete `WsClient` class (it has private state, `onClose`, `onNotification`
and is what `reconnect.ts` returns), so an object that only has `call`/`callStream` does not fit without an adapter and a type cast; a
`SocketLike` fits with zero. It also keeps the wire shapes honest: the demo answers with the **same JSON the daemon sends**, so a UI change
that breaks the wire contract breaks the demo test, not just production.

`DemoSocket` is a small dispatcher:

```
request {id, method, params}
  -> rule = scenario.rules[method]                 (exactly one of the four kinds below)
  -> "read"      answer with a fixture (or fn(params, state)); respond {id, result}
  -> "scripted"  stream the named script's events as {id, event, params}, then the terminal {id, result}
  -> "local"     mutate the in-memory session state, push the ADR-0005 notification(s) the real daemon would, respond
  -> "refused"   respond {id, error:{code:"demo_read_only", message:"Read-only in the demo."}}
no rule: respond {id, error:{code:"demo_unsupported", message:"Not available in the demo."}} and console.warn in dev
```

State is **in memory, per page load**: a reload resets the demo, nothing is written beyond the UI's own preference keys (theme, sidebar, tabs),
no token or session is ever stored. A cancel (`task.cancel`) stops the matching script, like the daemon.

**Coverage guard (a test, not a convention):** a unit test statically collects every method string the UI passes to `call`/`callStream`
(a regex over `src/**` excluding tests) and fails if any is in neither the rule table nor `REFUSED_METHODS`, the explicit list of writes the demo refuses (by name, not guessed from a verb). A new RPC in the UI
cannot ship without someone deciding what the demo does with it.

### 2. Scenario format

A scenario is **typed TypeScript data** in `src/demo/scenario/` (typed against `lib/types.ts`, so a type change in the UI breaks the build of the
demo), not a JSON recording: recorded streams rot when wire shapes change, a typed module is checked by `tsc -b`, and it can be lazy-loaded as one
chunk. A recorded stream *can* be pasted in as a typed constant when that is faster to author; the contract is the type.

```ts
interface DemoScenario {
  version: 1;
  now: number;                                // epoch ms every timestamp is written relative to: Date.now() in the browser (the UI formats "2h ago" against the real clock), fixed in tests
  daemon: DaemonInfo;                         // daemon.info: version string "demo", capabilities, relay/auth off
  workspaces: Workspace[];                    // 3 (two with spaces)
  spaces: Space[];
  tasks: Task[];                              // ~6 across the workspaces, mixed statuses (active, done, needs-you, failed)
  chats: Chat[];
  runs: Record<RunId, RecordedRun>;           // finished runs: run.logs answers from here (the timeline of a done task)
  scripts: Record<ScriptId, Script>;          // live behaviour (below)
  diffs: Record<TaskId, TaskDiffFixture>;     // task.diff / task.fileDiff / task.git*: a few files, a real unified diff
  terminals: Record<TaskId, TerminalReplay>;  // canned terminal output
  inbox: InboxItem[];                         // inbox.list: a pending permission (links to the live run), a validator verdict, a failed run, a stalled-task notice
  usage: UsageFixture;                        // usage.summary, provider.usage, account.usage: believable numbers, one account at 62% of a 5h window
  // the dispatcher table above lives in code (rules.ts): every method the UI calls is in it (read / scripted / local) or in REFUSED_METHODS; nothing is guessed
}
type Step =
  | { at: number; event: RunLogEvent }                       // a run event: chunk, thinking, tool_call, usage, done, ...
  | { at: number; notify: { topic: string; payload: unknown } }  // a daemon notification (task/run lifecycle) so lists and the sidebar update live
  | { at: number; ask: PermissionRequest; allow: Step[]; deny: Step[] };  // pause the script until the visitor answers
type Script = { steps: Step[]; speed?: number };
```

`at` is milliseconds since the script started; playback uses an **injectable clock** (`setTimeout` by default, fake timers in unit tests), honours
`prefers-reduced-motion` (collapses delays to a few milliseconds), and is cancelled by `task.cancel`, `run.stop` or leaving the page.

**The shipped scenario** (named "tour": the nine tasks of the landing hero, three workspaces, a Lead with two Peers; see the plan's amendment) covers exactly what the Human listed, each reachable in two clicks from first paint:

| Moment | How it plays |
|---|---|
| **A run streaming tool calls** | The landing task ("Add rate limiting to the public API") opens on a run already in progress: thinking, assistant text chunks, then `tool_call` cards (Read, Edit, a Bash `go test`), a `usage` event. It starts when the page loads and the demo opens on it (`#/workspace/1/task/1/chat/1`), so the visitor sees motion without doing anything. |
| **A permission request** | The script reaches an `ask` step: `permission_request` with a `write`/`edit` detail and a diff preview (the real permission card). **Allow** → the script's `allow` branch finishes the edit, runs the tests, `done`. **Deny** → the `deny` branch has the agent acknowledge and propose a plan, `done`. `run.respondPermission` is the *only* thing that advances an `ask` (ADR-0019: no auto-answer). The decision is also reflected in the inbox row. |
| **A diff tab** | `task.diff`/`task.fileDiff` return a small, real-looking unified diff (the debug fix, the first task test; the live task gains `limiter.go` on Allow); the diff viewer renders it exactly as production. Allowing the permission adds the file the request was about. |
| **A terminal tab** | `terminal.create`/`terminal.list`/`terminal.attach` replay a canned session as base64 `data` events with recorded inter-frame delays (a `go test ./...` run, then a prompt). `terminal.write` is accepted and echoed locally; Enter answers one dim line, "demo: this terminal replays canned output; nothing runs here". `terminal.resize` is a no-op. |
| **The inbox** | `inbox.list` returns the fixtures; `inbox.seen`, `inbox.snooze`, `inbox.resolve` are `local` rules (the badge count in the sidebar updates through the same notification the daemon pushes). |
| **Usage** | `usage.summary`, `provider.usage`, `account.usage` return fixtures: a Pro-style account with a 5h and a weekly window, a month of spend by day and by model. Read-only. |
| **Sending a prompt** | `run.start`/`task.prompt` on any task makes the visitor's text the run's prompt (the timeline shows it as the user's message) and plays the **canned reply script** for that task (a short, honest answer that says it is a demo and points at the install command). The visitor's text is shown, never parsed, never sent anywhere. |

### 3. What is disabled or faked

| Surface | Behaviour in demo | Mechanism |
|---|---|---|
| Sending a prompt, new task, new chat | works, plays canned reply script; new tasks/chats exist in the session until reload | `local` + `scripted` rules |
| Permission Allow / Deny | works, two scripted branches | `scripted` `ask` step |
| Stop run, queue cancel | works on the live script | `local` |
| Terminal input | echoed, nothing executes | `terminal.write` is a `local` no-op + one dim line |
| Settings (accounts, providers, profiles, MCP, plugins, learning, fleet hosts, automations, keys, secrets, forge login) | **visible, read-only**: lists render fixtures, every write answers `demo_read_only` and the UI's existing error path shows "Read-only in the demo." | `refused` rules (the default for any write) |
| File read of binaries / PDF / office preview, file write, git commit/push/PR | refused (`demo_read_only`); text file read serves a small bundled file set for the Files tab | `refused` / `read` |
| Account login, OAuth, `/api/token`, password login, relay, fleet | not present: no login screen, the `LoginRequiredError` path is unreachable | the real `connectDaemon` and `fetchToken` are dead-code-eliminated (section 6) |
| Real data | none: every name, path, branch and number is synthetic | scenario review in the PR |

**Banner.** One component, `DemoBanner`, rendered by `main.tsx` above `<App>` in demo mode only: a full-width strip, `role="status"`, uses the
existing semantic tokens (no new colours, no visual fork of any component), text **"Demo — not connected to a daemon. Scripted data; nothing you
type leaves your browser."** with one link, "Install smind" (`https://spacingmind.com`). It is **not dismissible** and stays visible at 390 px (it wraps to
two lines at most; the layout below it is offset by its measured height through one CSS variable, not by editing the shell). The connection label the sidebar shows ("Connected to daemon") reads **"Demo (scripted)"** — the one conditional in
`App.tsx`, on a constant, for a string.

The UI is otherwise **used as is**. No demo-only component, no demo-only styling, no forked layout; the diff to existing `src/` files outside `src/demo/`
is limited to: `main.tsx` (pick `connect`, render the banner), the connection-label string, `lib/ws-client.ts`/`lib/daemon.ts` (a `__SMIND_DEMO__`
guard, section 6) and `vite.config.ts`.

### 4. Build target

A new Vite **mode**, not a new package: `vite build --mode demo` → **`web/packages/ui/dist-demo/`** (gitignored; never embedded in the Go binary,
never under `internal/server/dist`, never in the desktop bundle).

- `vite.config.ts` becomes `defineConfig(({ mode }) => ...)`: for `mode === "demo"` it sets `define: { __SMIND_DEMO__: "true" }`, `build.outDir: "dist-demo"`,
  drops the dev-server proxy, and adds a plugin that writes `_headers` (section 10) and injects the CSP `<meta>` and the sha256 of the theme bootstrap
  inline script. In every other mode `__SMIND_DEMO__` is `false`, so the production and desktop builds contain **none** of `src/demo/**` (a test asserts the string
  "Demo — not connected" is absent from the production dist — the guard in both directions).
- Scripts in `web/packages/ui/package.json`: `build:demo` (`tsc -b && vite build --mode demo`, with `scripts/check-demo-build.mjs` after it),
  `preview:demo` (`vite preview --mode demo --host 127.0.0.1 --port 4173 --strictPort`), `check:demo`. The PDF-viewer second build is skipped (no PDF preview in demo).
- `bun run --filter @smind/ui build:demo` is the whole story locally; no daemon, no Go toolchain, no network.

### 5. Entry from the landing page

The landing's "Try it live" is a plain link to `https://app.spacingmind.com/` (new tab). The demo opens on the live task when the URL names
nothing (`#/workspace/1/task/1/chat/1`); a deep link may pick another (`/#/workspace/2/task/4/chat/4` is a route the UI already handles; the scenario's ids are
stable, documented in the scenario file). No cross-origin messaging, no iframe (the demo
sets `frame-ancestors 'none'`, [section 10](#10-security)).

### 6. No daemon or WebSocket code is reachable in the demo build

A banner is a promise; the build makes it checkable.

- `WsClient.connect` (the only `new WebSocket(` in the app) and `connectDaemon`/`fetchToken`/`fetchAuthRequired` (every `fetch("/api/...")` of the
  connect flow) start with `if (__SMIND_DEMO__) throw new Error("demo build has no daemon")`; with `define` that is a constant `true`, so the bundler removes
  the rest of each function and the strings `/api/token`, `/ws?token=`, `/api/login`, `new WebSocket(` do not appear in `dist-demo`.
- Other `fetch(` callers in the UI (downloads, attachments, healthz, update checks) are reached only after a connect; they are listed once by
  `check-demo-build.mjs`, and `connect-src 'none'` in the CSP (section 10) makes any survivor fail closed in the browser, not silently succeed.
- `scripts/check-demo-build.mjs` (run by `build:demo` and by the non-required `demo-build.yml`) fails the build if `dist-demo` contains any of: `/api/token`, `/api/login`, `new WebSocket(`, a `ws://` or `wss://` URL,
  a `http(s)://` URL to `localhost`/`127.0.0.1`/`[::1]`, `relay.spacingmind`, `smind.bearer`, `grpc`, `/healthz`, `new EventSource(`, `sendBeacon(` (the list is the script's data, reviewed in the PR). The bare words `localhost` and
  `127.0.0.1` are not forbidden: the UI compares `window.location.hostname` with them and two settings forms show `http://localhost:8080` / `http://localhost:54545/callback` as placeholder text for flows the demo refuses; those two
  placeholders are the script's only allow-list. It also fails on a missing or weak CSP (`connect-src 'none'`, `default-src 'none'`, no unsafe script sources, the script hash), a missing `_headers`, a `pdf-viewer.html` (the PDF page from `public/`
  is removed from the demo output), the scripted daemon sitting in the entry chunk, and any budget overrun. `check-demo-build.mjs --production <dist>` asserts a normal build contains no demo code, by looking for the banner text, the scripted daemon, the tour's landing route and the banner's CSS variable (so a stray static `@/demo/*` import is caught even if it is not tree-shaken). The required `ci` job runs only the guard's own tests and the `--production` check; the demo build and its budgets run in `demo-build.yml`, path-filtered to `web/**`, so the size budget cannot fail an unrelated PR.

### 7. Bundle size budget and lazy loading

**Measured baseline (2026-11-01, `vite build` on `origin/develop` at `5e09caf1`):** the production UI is **one JS chunk**, `index-*.js` = 3,539,994 B raw,
**1,067,437 B gzip**, plus 166,378 B CSS (27,171 B gzip), five Geist font files (~76 kB) and a 9 kB `file-tab-router` chunk. There is no meaningful code
splitting today (xterm, CodeMirror, diff2html, react-markdown, pdf.js shell are all in the entry chunk).

**Measured on the implementation (PR #797):** `dist-demo` initial JS **1,071,795 B gzip** (+4,358 B over the baseline: the banner, the flag guards and the entry glue), initial CSS 27,353 B gzip, the scripted daemon (dispatcher, scenario, fixtures) **13,761 B gzip** in its own lazily imported chunk; the production build grows by 33 B. The budgets below stay as set, with large headroom.

Budget for `dist-demo` (the numbers live in `scripts/check-demo-build.mjs`, so changing one is a visible diff):

| Item | Budget | Why |
|---|---|---|
| Demo code (`src/demo/**`: dispatcher, scripts, scenario) | **≤ 60 KB gzip**, in its own lazily imported chunk(s) | the demo must not be heavier than the product it shows by more than a few percent |
| Initial JS (entry + modulepreloads) | **≤ the production entry chunk built in the same job + 32 KB gzip** (absolute fallback 1,150,000 B when run without a baseline); initial CSS + 8 KB | relative, so it goes red only when the demo itself grows: the measured demo overhead is 4.4 KB, 32 KB is about 7x that |
| Initial CSS | ≤ 35 KB gzip | baseline 27 KB |
| Any single demo asset (fixture, font subset) | ≤ 30 KB gzip | the scenario is data, not media |

**Lazy loading:** `main.tsx` in demo mode imports `@/demo/connect` with a dynamic `import()` started **before** `createRoot`, so the engine and the scenario
load in parallel with the first render and are code-split from the entry. The scenario's large fixtures (diffs, terminal replays, the usage month) are
separate dynamic imports resolved by the rule that first needs them. The **heavy product libraries stay as today** — splitting xterm, CodeMirror and
diff2html out of the entry chunk would help production and demo alike, but it is a change to the product UI that this ADR does not make; it is listed under
[consequences](#consequences) as the follow-up that would cut the demo's initial load by roughly half (to be measured, not promised). Fonts: the existing Latin
subsets only (the demo is English); the Vietnamese/Cyrillic subsets are served only if the browser asks (`unicode-range`, as in production).

### 8. Hosting

Three options; one recommendation.

| | (a) Cloudflare Worker static assets, `app.spacingmind.com` as `custom_domain` | (b) Served by the landing worker under `/demo` | (c) GitHub Pages |
|---|---|---|---|
| Origin | its own origin, `app.` | same origin as the marketing site | `<user>.github.io/smind/` or a CNAME |
| Isolation | separate origin: demo `localStorage` and a future real app are separate from the marketing site; strict CSP/headers possible without touching the landing | demo shares the landing's origin: the landing's scripts, cookies, analytics and CSP apply; a looser landing CSP weakens the demo's and vice versa | separate origin with a CNAME; headers **cannot** be set (no `_headers`, no CSP header, no `frame-ancestors`) |
| Matches the future | the same host `app.spacingmind.com` that ADR-0052 would use for a real app: the demo is the first thing deployed there, no URL changes later | the real app would need a different host, or the demo moves | needs a re-point when the real app arrives |
| Pattern | same as landing `wrangler.jsonc` (`assets`, `routes: [{pattern, custom_domain: true}]`), same Cloudflare account as the zone | depends on landing PR/repo, whose Lead owns it; couples deploy cadence | no Cloudflare dependency; DNS CNAME only |
| Cost | Workers static assets: free tier is ample for a demo (static asset requests are not billed/limited as Worker invocations; verify at deploy) | free | free |
| Deploy | a smind workflow builds `dist-demo` and runs `wrangler deploy` with a CF token secret | a landing-repo change to copy the bundle: cross-repo coupling | `actions/deploy-pages` (like `docs-pages.yml`) |

**Recommendation: (a).** It is the only option with a separate origin **and** real security headers **and** the right long-term hostname, and it
follows a pattern the same Cloudflare account already runs. (b) couples two repos and two CSPs for no gain. (c) is the simplest but cannot send the
CSP/`frame-ancestors` headers this ADR relies on (a `<meta>` CSP cannot express `frame-ancestors`), and moves hostnames later.

**Deploy (Human decision 2026-11-01: option (a)).** `web/packages/ui/wrangler.demo.jsonc` is a static-assets-only Worker, no script: `name: "smind-demo"`, `assets.directory: "./dist-demo"`,
`assets.not_found_handling: "single-page-application"`, `routes: [{ pattern: "app.spacingmind.com", custom_domain: true }]`, `workers_dev: false` and `preview_urls: false` (the demo is reachable only on its own
domain). `.github/workflows/demo-deploy.yml` builds and checks the demo and runs a pinned `wrangler@4.156.0 deploy`. It runs only when a release is cut: `release-please.yml` calls it (`workflow_call`, `ref` = the new tag) once the release is published, and `workflow_dispatch` takes a `ref` (default
`develop`) for a manual run. There is no `release: published` trigger and no deploy on a push to develop (a release published with the default `GITHUB_TOKEN` starts no workflows, so that trigger would never fire). Hardening: `permissions: contents: read`, actions pinned by SHA, a `demo-deploy` concurrency group that never cancels a running deploy, a 15-minute timeout, `persist-credentials: false`. The two
secrets are mapped only into the step that checks them and the step that deploys, never into the job, so the install and build steps (which run package scripts) cannot see them; if either is missing the run prints
a notice and ends green without building or deploying. The workflow runs `check-demo-build.mjs dist-demo` (forbidden strings, CSP and its script hash, lazy chunk, budgets) before the upload, so a bundle that fails the
guard is never published.

**Headers are served, verified locally.** Workers static assets honour a `_headers` file in the assets directory, and `_headers` is exactly what the build already writes into `dist-demo`. With `wrangler dev --local` (Cloudflare's own
`workerd` runtime, on 127.0.0.1, no token, no API call) every response carried the CSP including `frame-ancestors 'none'`, `X-Content-Type-Options`, `Referrer-Policy`, `Permissions-Policy` and `Cross-Origin-Opener-Policy`,
`/assets/*` carried `Cache-Control: public, max-age=31536000, immutable`, and an unknown path (the SPA fallback) carried the same headers; the page loaded with no console error and no request beyond its own origin. No header
config beyond `_headers` is needed, and there is no Worker script. Not verifiable without a deploy: the Cloudflare edge itself and the custom-domain certificate; the first manual run is the check (step 5 below).

### 9. The Human's infra steps

None of these is done by an agent; no agent asks for, prints or stores the token. The order matters.

1. **Review this ADR**, choose the hosting option (done: (a), 2026-11-01) and accept or veto the banner text and the "Install smind" link target.
2. **Create a Cloudflare API token** (Cloudflare dashboard → My Profile → API Tokens → Create Token → the **"Edit Cloudflare Workers"** template), with:
   - **Account Resources:** Include → the **Spacing Mind** account only;
   - **Zone Resources:** Include → Specific zone → **`spacingmind.com`** only;
   - leave the template's permissions as they are (Account: Workers Scripts: Edit, and the template's other Workers, KV, R2 and routes entries). If the first deploy fails while attaching
     `app.spacingmind.com` with a permission error, edit the token and add **Zone → DNS → Edit** for the same zone only (a `custom_domain` route creates the DNS record and the certificate);
   - optionally add a client IP filter for GitHub's runners and a TTL; do not use the Global API key.
3. **Add two repository secrets** to `spacingmind/smind` (Settings → Secrets and variables → Actions → New repository secret), by those exact names:
   - **`CLOUDFLARE_API_TOKEN`**: the token from step 2;
   - **`CLOUDFLARE_ACCOUNT_ID`**: the account id (dashboard → the account's Workers & Pages overview, right-hand side).
   They are plain repository secrets (the workflow uses no GitHub Environment). Until both exist, `demo-deploy.yml` skips with a notice and passes.
4. **DNS**: no manual record (wrangler creates `app` as a proxied record for the custom domain); the hostname must not already have a conflicting `CNAME`/`A`/`AAAA` record in the zone. Confirm there is none today.
5. **Deploys.** A release deploys the demo by itself (`deploy-demo` in `release-please.yml`, a leaf after `publish`: a failed deploy does not touch the release; rerun that job to retry). By hand, any ref:

   ```
   gh workflow run demo-deploy.yml -f ref=v0.18.0   # redeploy a release
   gh workflow run demo-deploy.yml -f ref=develop   # put develop's demo live between releases
   gh run watch   # or the Actions tab
   ```

   After the first deploy open `https://app.spacingmind.com/`, check the banner, run the permission Allow flow, and look at the response headers (a browser's network panel, or `curl -sI https://app.spacingmind.com/`) for the
   `content-security-policy` with `frame-ancestors 'none'`.
6. **Landing**: the landing's "Try it live" link is `https://app.spacingmind.com/`; it must not go live before step 5 is green.
7. Optional: a Cloudflare **Web Analytics** or no analytics at all. The demo ships none (section 10); adding any is a Human decision and a CSP change.

**Rollback:** run the workflow again with an older `ref`; or delete the `smind-demo` Worker (Workers & Pages → smind-demo → Settings → Delete), which also removes the custom domain.

### 10. Security

There is no daemon behind `app.spacingmind.com`: the page cannot read, write or run anything of anyone's, and the build is made to *prove* it.

- **No connect path.** Section 6: the demo bundle contains no WebSocket, no `/api/*` URLs, no token handling, no relay or gRPC client, no `localhost`/`127.0.0.1`
  literal, enforced by a build-time scan **and** by the CSP below, which would block any such request even if the scan missed it. There is no field in the demo to type an address into.
- **CSP** (header via `_headers` on the host; the same policy in a `<meta http-equiv>` in `index.html` for local preview and other hosts, minus `frame-ancestors`, which only works as a header):

  ```
  default-src 'none';
  script-src 'self' 'sha256-<hash of the inline theme bootstrap in index.html>';
  style-src 'self' 'unsafe-inline';
  img-src 'self' data: blob:;
  font-src 'self';
  connect-src 'none';
  worker-src 'self' blob:;
  manifest-src 'self';
  base-uri 'none'; form-action 'none'; object-src 'none'; frame-ancestors 'none';
  ```

  `connect-src 'none'` forbids `fetch`, XHR, `WebSocket`, `EventSource` and beacons to *any* origin, same-origin included: the demo needs none. `style-src 'unsafe-inline'`
  is required by xterm.js and Radix inline styles (production has the same need); `script-src` has **no** `'unsafe-inline'` and **no** `'unsafe-eval'`; the one inline script is allowed by hash,
  and the plugin computes it from `index.html` at build time (a test fails if they diverge, so editing the bootstrap without re-hashing cannot ship silently).
- **Other headers** (`_headers`): `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `Permissions-Policy: camera=(), microphone=(), geolocation=(), clipboard-read=()`,
  `Cross-Origin-Opener-Policy: same-origin`, `Strict-Transport-Security` (Cloudflare adds HSTS on request; the Human enables it on the zone), long-lived `Cache-Control: immutable` for hashed `assets/*`,
  and `Cache-Control: public, no-transform` for everything that is not a build asset (the HTML and the SPA fallback; the HTML then has no freshness lifetime and is revalidated by its ETag). Why `no-transform`: Cloudflare Web Analytics' automatic setup, enabled zone-wide on `spacingmind.com`, injects a beacon `<script>` (static.cloudflareinsights.com) into proxied HTML; this CSP blocks it (nothing leaks, but the console shows two errors) and the zone's ruleset cannot exclude the host. Cloudflare's Web Analytics documentation ("Sites proxied through Cloudflare", developers.cloudflare.com/web-analytics/get-started/) says: "If you have a `Cache-Control` header set to `public, no-transform`, Cloudflare proxy will not be able to modify the original payload of the website. Therefore, the Beacon script will not be automatically injected to your site". `/assets/*` detaches that header (`! Cache-Control`) and keeps its immutable caching; `check-demo-build.mjs` requires both. Verified locally with `wrangler dev --local`: `/`, an unknown path (the SPA fallback) and the `/index.html` redirect answer `public, no-transform`, the assets `public, max-age=31536000, immutable`. The effect on the zone's injection can only be confirmed on a deployed demo; the docs site is a separate Worker and keeps its analytics.
- **No third parties**: no analytics, no fonts CDN, no error reporter, no cookies. Visitor input never leaves the browser (the prompt box is local, and `connect-src 'none'` makes that a browser guarantee, not a promise).
- **No secrets in the bundle**: the scenario is synthetic; a test asserts it contains no `/Users/`, `C:\Users\`, `@gmail`, `sk-`, or the repo owner's real paths, and the PR review reads the scenario once as data.
- **Open redirect / link safety**: external links in the demo use the existing `external-link` helper (`rel="noopener noreferrer"`); the only external target in the banner is `https://spacingmind.com`.
- **Residual risk, stated:** the demo shares the `spacingmind.com` registrable domain; it sets no cookies and reads none, but a cookie scoped to `.spacingmind.com` by another subdomain would be sent to it
  and is ignored. `localStorage` is per-origin, so the landing's storage is not shared.

## Alternatives considered

- **Keep only the coded hero mock.** Cheapest, but a second UI to maintain; the Human chose to show the real one.
- **Record a real session (HAR/WS capture) and replay it.** Real wire shapes, but a recording rots with every wire change, cannot branch (Allow vs Deny), contains real paths unless scrubbed, and is not type-checked. Typed scenario data keeps
  `tsc` in the loop; a recording can still seed a scenario constant.
- **A `DemoClient` implementing only `WsClientLike`.** Smaller, but the app needs the concrete `WsClient` (reconnect, `onClose`, notifications) and a cast would hide wire drift. The socket seam costs ~one file and keeps every real layer under test.
- **MSW / a Service Worker faking `/api` and `/ws`.** Realistic, but a service worker is a persistent network interposer on the origin (the opposite of "no network path"), needs registration and cache lifecycle, and cannot be proven absent by a bundle scan.
- **A real daemon behind the demo (a sandbox per visitor).** Costs model calls and compute, needs abuse controls, and is precisely the hosted-product decision ADR-0052 asks the Human to make. Not now.
- **An iframe of the real app inside the landing.** Couples origins and CSPs, and the iframe's `frame-ancestors` would have to allow the landing; a plain link avoids it.
- **Hosting (b) and (c)**: see the table in section 8.
- **A new package (`web/packages/demo`).** Would fork imports of the UI's internals or duplicate the build; a Vite mode on the existing package uses the UI as-is, which is the Human's requirement ("no visual forks").

## Rationale

The demo should be the product, running against a daemon that is not there. The cheapest faithful place to put "not there" is under `WsClient`, where the interface is already a
one-method JSON socket and where a `connect` prop already exists for tests. That keeps `App.tsx` free of demo logic and gives the demo the same wire types as production. The
security story is mostly *absence* — nothing to connect to — made mechanical by a bundle scan and a CSP that forbids all network from the page. A separate origin on the zone's own
Cloudflare account matches how the site is already shipped and is the hostname a later real app would use, so the demo does not create a migration later.

## Consequences

- A new `src/demo/` tree (dispatcher, scenario, banner), a Vite mode, three package scripts, a Playwright smoke against the built demo, and a size/forbidden-string check. Production and desktop builds are unchanged (their bundles contain no demo code; a test says so).
- Every new RPC the UI adds needs a demo rule (the coverage test fails otherwise): a small, deliberate tax that also documents which calls are read-only.
- The scenario is a maintained asset; when a wire shape changes, `tsc -b` points at it.
- Initial JS is as large as production's (~1.07 MB gzip). **Follow-up, not decided here:** lazy-load xterm/CodeMirror/diff2html/pdf.js by tab in the product UI; it would roughly halve the demo's first load.
- `app.spacingmind.com` is reserved by this change for the demo. ADR-0052 must say what happens to it if a real app arrives (replace the demo, or move the demo to `/demo`).
- Until the Human completes section 9, the demo exists only as a local build (`bun run --filter @smind/ui preview:demo`).

## Addendum 2026-11-01: docs.spacingmind.com moves from GitHub Pages to Cloudflare, and both sites deploy on release

**Human decisions.** (1) `docs.spacingmind.com` leaves GitHub Pages and is served the same way as the demo: a static-assets-only Worker `smind-docs` (`docs-site/wrangler.docs.jsonc`), deployed by `.github/workflows/docs-deploy.yml` (formerly `docs-pages.yml`) with the same two secrets and the same skip-if-missing notice. (2) The demo and the docs are deployed **only when a release is cut**, plus manual dispatch, never on a push to develop, so production always matches the released version.

- **Cutover (done by the Human before this PR):** the old CNAME to `spacingmind.github.io` was deleted, Pages unpublished, and the Worker deployed by hand with the settings now in the config file (`html_handling: auto-trailing-slash`, `not_found_handling: 404-page`, `workers_dev: false`, `preview_urls: false`, `custom_domain` route). CI deploys replace that Worker in place. Research behind the runbook: Cloudflare's docs say a Custom Domain cannot be created on a hostname with an existing CNAME (delete the record first, then attach; Cloudflare then creates the record and an Advanced Certificate for the hostname); the docs describe no override. In wrangler 4.156.0 itself, a non-interactive (CI) deploy sends `override_existing_dns_record: true` and `override_existing_origin: true` to the custom-domain API, and an interactive one asks first. So a hostname that already has a conflicting record is replaced silently in CI: never point `custom_domain` at a hostname whose record must be kept. **Rollback:** re-create the CNAME `docs` → `spacingmind.github.io`, re-enable Pages (Settings → Pages → Source: GitHub Actions) and publish the build artifact from a branch that still has the Pages workflow (`git show a1c9fe1a:.github/workflows/docs-pages.yml` is the last one); or redeploy an older tag with `gh workflow run docs-deploy.yml -f ref=<tag>`.
- **What changed in the repo.** The Pages upload and deploy, the `pages`/`id-token` permissions and the `github-pages` environment are gone; the build, test and `check-dist` steps stay. `CNAME` and `.nojekyll` are no longer written and `check-dist` fails if they appear. `config.mts` generates `_headers` in the build.
- **Headers.** `nosniff`, `Referrer-Policy: strict-origin-when-cross-origin`, `X-Frame-Options: DENY`, a `Permissions-Policy`, `Cache-Control: public, max-age=31536000, immutable` for `/assets/*`, and a **strict CSP**: `default-src 'self'`; `script-src 'self'` plus the SHA-256 of every executable inline script of the built pages; `style-src 'self' 'unsafe-inline'` (VitePress and Vue set inline styles); `img-src 'self' data:`; `font-src`, `connect-src` and `manifest-src` `'self'`; `worker-src 'self' blob:`; `base-uri 'self'`; `form-action 'none'`; `object-src 'none'`; `frame-ancestors 'none'`. VitePress emits exactly three executable inline scripts, identical on all 26 built pages (the appearance bootstrap, the "mac" class, the page-hash map); the JSON-LD blocks are data. The hashes are computed from the built pages in `buildEnd`, and `check-dist` fails the build if any page has an inline script the policy does not list, or if `script-src` gains `'unsafe-inline'`/`'unsafe-eval'`. Checked in Chrome against `wrangler dev --local`: no CSP violation and no foreign request on the home page, the appearance switch (dark to light), local search (16 results for "permission"), client-side navigation and the 404 page. `interest-cohort` is left out of `Permissions-Policy` (Chrome logs a warning for the retired feature).
- **URLs.** The sitemap, the canonical links and the `llms.txt` links use `/foo` (no extension, no slash) and `/` for the home page. With `auto-trailing-slash` (the live setting) `/foo` is 200 with no redirect, `/foo/` and `/foo.html` redirect (307) to `/foo`, `/index.html` to `/`, an unknown path answers 404 with `404.html`, `/_headers`, `/CNAME` and `/.nojekyll` are not served, and `.md`, `llms.txt`, `llms-full.txt`, `robots.txt` and `sitemap.xml` are served with sensible content types. A crawl of the local server fetched all 25 sitemap URLs and the 58 internal links and assets they reference: all 200, no redirect, no loop. `drop-trailing-slash` behaves the same for these flat pages.
- **Release wiring.** `demo-deploy.yml` and `docs-deploy.yml` are reusable (`on: workflow_call`, a required `ref` input, the two secrets declared and passed explicitly, never `secrets: inherit`) and keep `workflow_dispatch` with a `ref` input. `release-please.yml` gains two jobs, `deploy-demo` and `deploy-docs`, `needs: [release-please, publish]`, guarded by the same `release_created == 'true'` and `publish.result == 'success'` as the other post-publish jobs, `permissions: contents: read`, `ref` = the new tag. They are leaves (nothing needs them), so a failed deploy never fails or rolls back the release. The `release: published` triggers were removed: release-please publishes with the default `GITHUB_TOKEN`, and events created with it start no workflows, so they never fired. The docs workflow decides "deploy or only build" from whether a `ref` input was given (a called workflow sees its caller's event name, so it cannot use that); a push to develop and a pull request on the docs paths still build, test and run `check-dist`.
- **By hand.** `gh workflow run docs-deploy.yml -f ref=v0.18.0` and `gh workflow run demo-deploy.yml -f ref=v0.18.0` (any tag, branch or commit; default `develop`). `docs-site/README.md` carries the docs commands.
