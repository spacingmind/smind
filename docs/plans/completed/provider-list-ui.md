# Dynamic provider list in the web UI

The taskrunner supports three providers — `claude-native`, `glm`, and
`codex-native` (`internal/taskrunner/provider.go`; the codex client was
merged in PR #40) — but the UI's prompt-form dropdown hardcodes only
two (`task-detail.tsx`'s `PROVIDERS = ["claude-native", "glm"]`), so
Codex is unusable from the web UI. The provider list should come from
the daemon, not be hardcoded in the client.

## Acceptance Criteria

- A new wsapi method `provider.list` (no params) returns the daemon's
  actually-supported providers as
  `{providers: [{id, label?}]}` (id = taskrunner Provider string,
  e.g. `codex-native`; label optional human name, e.g. "Codex" — derive
  from a single source of truth, do not duplicate the id→label mapping
  in the client). Sourced from `internal/taskrunner`'s provider
  registry/switch — wherever RunPrompt dispatches — so a provider added
  to the backend shows up without UI changes.
- `internal/wsapi` wire test for `provider.list` (existing real-WS
  patterns).
- Web UI: `task-detail.tsx`'s prompt form fetches `provider.list` once
  per connection (client change, like other one-shot fetches) and
  renders the dropdown from it; failure falls back to the current
  hardcoded two-provider list with no crash (and the error is surfaced
  somewhere non-fatal — decide where, likely just console + fallback).
- The stale hardcoded list shrinks to just the fallback.
- Update the sample/fixture data if any test relied on exactly two
  providers.

## Test Scenarios

- Go: `internal/wsapi` wire test — `provider.list` returns all three
  ids over a real WS connection; unknown-id result shape fails loudly
  (decode into the typed struct).
- Web: `task-detail` component test — dropdown options come from a
  mocked `provider.list` result (3 providers); when `provider.list`
  rejects, dropdown shows the fallback two and the form still submits.
- `bunx tsc -b` clean, `bun run test` passes, `task build` succeeds;
  `.gitkeep` restored if wiped; Go verify chain green
  (build/gofmt/vet/`go test -race ./...`).

## Decisions

- **Label mapping location**: `internal/taskrunner/provider.go` gained
  `SupportedProviders() []ProviderInfo` — the single source of truth
  listing every provider RunPrompt can dispatch to, in dropdown display
  order, with human labels ("Claude Code", "GLM", "Kimi", "Codex").
  `provider.list` (wsapi) serves it verbatim; the client maps
  `label ?? id` in the `<option>` and holds no id→label table. Note:
  the backend actually supports **four** providers (kimi was merged
  alongside codex), so provider.list returns all four — the wire/UI
  tests assert the full set.
- **Fallback semantics**: `PromptForm` keeps a `FALLBACK_PROVIDERS`
  const (the old two-provider list, ids only). It is set as initial
  state, `provider.list` replaces it on success (only if the response
  has ≥1 provider); on error the fallback stays and the error is logged
  via `console.error` — non-fatal, form fully usable.
- **Caching / fetch cadence**: fetched once per `WsClientLike` instance
  (an effect keyed on `client`), reset to fallback when the client
  changes — so a reconnect re-fetches, matching how run.list re-fetches
  per new client. No caching across connections.
- One combined commit instead of two (spec doc rides along).

## Progress

- [x] `provider.list` backend + wire test
- [x] UI dropdown from `provider.list` + fallback + tests
- [x] Verification (both sides)

## Validation

- `provider.list` returns `{providers: [{id, label}]}` sourced from
  `taskrunner.SupportedProviders()` — confirmed by
  `TestServer_ProviderList_RoundTrip` (real WS connection, decodes into
  the typed `providerListResult`, fails loudly on unknown ids/labels).
- Web: `task-detail.test.tsx` — "renders the provider dropdown from
  provider.list (3 providers, labels over ids)" asserts option
  labels/values; "falls back to the hardcoded two-provider list when
  provider.list rejects, and the form still submits" asserts the
  fallback options and a successful run.start after rejection.
- Go chain green: `go build ./...`, `gofmt -l .` (empty), `go vet ./...`,
  `go test -race ./...`, `task test`, `task lint` — all pass.
- Web: `bunx tsc -b` clean, `bun run test` (in `web/packages/ui`) —
  82 tests pass (including the two new ones); `task build` succeeds;
  `internal/server/dist/.gitkeep` restored after the vite build wiped
  it.
