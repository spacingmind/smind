# smind account CLI

## Acceptance Criteria

- A new `smind account` subcommand family talks to the locally running
  daemon over the same WebSocket API every other CLI subcommand already
  uses (`internal/wsclient`, auth token from `~/.spacingmind/token`):
  - `smind account add <provider> <label>` — registers an account.
    Credentials are read from stdin (the full credential blob — for
    OAuth-type accounts a JSON object with at least the refresh token;
    exact per-provider shape comes from what `internal/accounts`'
    refreshers already parse, don't invent a new one). Never accept a
    credential as a command-line argument (it would leak into shell
    history and `ps`).
  - `smind account ls` — lists accounts (id, provider, label,
    credential type, created/updated), same JSON output style as
    `smind workspace ls`.
- Backend: new `internal/wsapi` methods `account.list` and `account.add`,
  wired to `accounts.Registry` in `cmd/smind/serve.go` exactly like
  `workspace.*` is wired to `workspace.Manager` today. No new packages;
  `accounts.Registry` already owns the storage.
- `smind account` appears in `cmd/smind/main.go`'s usage text.
- Update `docs/ROADMAP.md`'s Phase 1 note: the "no way to add an
  account" gap is closed — replace the sentence that says adding
  `smind account add` is the next step with a short note that it exists
  (keep the honest note about not yet exercised against a real provider
  account if that's still true after your manual verification).

## Test Scenarios

- Go: `internal/wsapi` wire-level test for `account.list`/`account.add`
  following the established pattern in `internal/wsapi/wsapi_test.go`
  (real WS connection, real store).
- Go: CLI dispatch test for the new subcommand following whatever
  pattern `cmd/smind` tests already use (check for `*_test.go` in
  `cmd/smind/`; if none exists, follow the dispatch-through-`run()`
  pattern the code comments describe and test `run()` directly with a
  fake or local server — keep it minimal).
- Manual verification against a real daemon: start `smind serve`,
  `account add` a fake credential via stdin, confirm `account ls` shows
  it, confirm it lands in the real store. State plainly if you couldn't
  exercise a real provider credential.
- `go build ./...` / `gofmt -l` / `go vet ./...` / `go test -race ./...`
  clean. `task build` succeeds.

## Decisions

- Credentials over stdin, not argv — security requirement, matches how
  real CLIs avoid leaking secrets into `ps`/shell history.
- Reuse `accounts.Registry` as-is; the only new surface is
  `internal/wsapi` methods + CLI plumbing. Smallest coherent change.
- Out of scope: `account remove`, quota display, editing credentials,
  and the real-provider end-to-end failover dogfood (that's a follow-up
  once real credentials are at hand).

## Progress

- [x] `internal/wsapi`: `account.list`/`account.add` + tests
- [x] CLI: `smind account add` / `smind account ls` + usage text
- [x] ROADMAP update + verification (tests/build + real-daemon manual check)

## Validation

- `internal/wsapi` wire test (`TestServer_AccountAddListRoundTrip`) adds an
  OAuth-shaped credential through a real WebSocket connection and SQLite
  store, lists it, and verifies that neither response contains credential
  data.
- `cmd/smind` dispatch test (`TestRunAccountAddDispatchesCredentialFromStdin`)
  runs `run()` against a local WebSocket server, confirms the stdin-only API
  key reaches the registry, and confirms stdout omits it.
- Manual real-daemon check (2026-09-07): built `smind`, started `smind serve`
  with a temporary `SMIND_HOME`, piped a fake API key to
  `smind account add openai manual-test`, confirmed `smind account ls`, and
  queried the temporary SQLite `accounts` table. No real provider credential
  or real two-account failover was exercised.
- Validated: `go build ./...`, `gofmt -l .`, `go vet ./...`,
  `go test -race ./...`, `task lint`, `task test`, and `task build`.
