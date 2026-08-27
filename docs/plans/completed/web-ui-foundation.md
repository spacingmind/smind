# Web UI foundation: Tailwind + shadcn/ui + app shell

## Acceptance Criteria

- `web/packages/ui` uses Tailwind CSS v4 (`@tailwindcss/vite`) for styling,
  replacing the current placeholder's inline `style={{...}}`.
- shadcn/ui is set up (`components.json`, `@/*` path alias in both
  `vite.config.ts` and `tsconfig.json`, the standard `cn()` helper via
  `clsx`+`tailwind-merge`) with a small set of primitives actually used by
  the app shell built in this task (expect: button, sidebar, scroll-area,
  resizable, separator — add only what's used, not a speculative full set).
  Dark theme by default (matches the current placeholder's dark background),
  using shadcn's CSS-variable theming so switching/adjusting the theme later
  doesn't require touching component internals.
- A TypeScript WebSocket RPC client living in `web/packages/ui/src/lib/`
  mirrors `internal/wsclient`'s wire protocol exactly (same envelope shape:
  `{id, method, params, result, error, event}`; same semantics: one
  persistent connection, many concurrent in-flight requests correlated by
  id, a streaming call variant that invokes a callback per event before its
  terminal result, per-request cancel via `task.cancel` without closing the
  connection). See `internal/wsclient/wsclient.go` for the exact behavior
  to match — this is a second implementation of the same client contract,
  not a new design.
- The daemon serves its auth token to the same-origin page via a new `GET
  /api/token` endpoint (see Decisions for why this doesn't weaken the
  existing security posture) so the web UI can open `/ws?token=...` on
  load without the user pasting anything in manually.
- App shell: a collapsible sidebar listing real workspaces (`workspace.list`)
  and, per workspace, its tasks (`task.list`) — fetched from the actual
  running daemon over the new TS WS client, not mock/static data — plus an
  empty resizable main content area as a placeholder for task detail/panes.
  Explicitly deferred to later tasks: task detail view content, tabs/split
  panes content, agent timeline, file explorer, CodeMirror 6, diff viewer,
  xterm terminal, permission prompts UI.
- `task build`/`task dev:web` keep working; the production build still
  outputs to `internal/server/dist` unchanged (see ADR 0002).

## Test Scenarios

- `bun run build` (via `task build`) succeeds; the built CSS actually
  contains compiled Tailwind utility classes used by the app shell (not an
  empty/near-empty stylesheet — a real regression to check for with
  Tailwind v4's content-scanning if the config is wrong).
- TypeScript compiles with no errors across `web/packages/ui`.
- The WS client has real unit tests (Vitest — new to this package, add it)
  covering: request/response round-trip, a server error response surfacing
  as a rejected promise (not swallowed), a streaming call's event callback
  firing before its terminal response, and cancellation only cancelling its
  own request (not the whole connection) — run against a lightweight fake
  WebSocket server in the test, not a live daemon.
- `GET /api/token` returns the daemon's real current token; a real
  `internal/server` test proves it round-trips into a working `/ws`
  connection.
- Manual verification against the real built binary: start `smind serve`,
  create a workspace/task via the CLI (already built in the prior task),
  confirm the sidebar reflects them. No browser-automation tool is
  available in this environment — verify via the built asset output,
  curl-based checks of what the server returns, and reading the rendered
  behavior from code/logs; state plainly in the report if true
  interactive-browser verification wasn't possible, rather than implying it
  was done.
- No regression: `task build` still produces a working `bin/smind` serving
  `/` and `/healthz` as before; existing Go test suite (`task test`)
  unaffected.

## Decisions

- Tailwind CSS v4 + shadcn/ui, per explicit user direction: shadcn's
  components are copied into the repo (not an opaque npm dependency),
  giving full control for the IDE-like layout (panes, embedded terminal,
  diff viewer) this app needs later, at the cost of each new component
  needing its own `shadcn add` step instead of one bulk install.
- `GET /api/token`: the WS handshake already puts the token in a URL query
  param specifically because browser `WebSocket` can't set an
  `Authorization` header (see `internal/wsapi.Handler`'s existing doc
  comment) — the token already has to be readable by page JS one way or
  another. Serving it via a same-origin GET endpoint doesn't introduce a
  new trust boundary beyond what already exists: anyone who can reach the
  HTTP server at all can already hit any token-gated endpoint. This is
  explicitly scoped to smind's current single-user-localhost posture (the
  daemon isn't exposed remotely yet); revisit if/when remote access is
  ever added (see `docs/decisions/` for any future ADR on that).
- The WS client lives in `web/packages/ui`, not a new `web/packages/shared`
  (flagged as a "to follow" package in ADR 0002) — there's only one UI
  surface consuming it right now; move it if/when a second surface needs
  it.
- Everything beyond the app shell (task detail, panes, timeline, file
  explorer, CodeMirror, diff viewer, terminal, permissions UI) is
  explicitly out of scope for this task — each is substantial enough to be
  its own follow-up task against this same foundation.

## Progress

- [ ] Tailwind v4 + shadcn/ui scaffolding in `web/packages/ui`
- [ ] TypeScript WS RPC client + tests
- [ ] `GET /api/token` daemon endpoint + test
- [ ] App shell (sidebar with live workspace/task data + empty main pane)
- [ ] Verification (build/typecheck/tests + manual check against real binary)

## Validation

(Filled in as each Acceptance Criterion is confirmed — command run, test
name, or manual check.)
