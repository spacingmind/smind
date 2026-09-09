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

(To be filled: label mapping location, fallback semantics, caching.)

## Progress

- [ ] `provider.list` backend + wire test
- [ ] UI dropdown from `provider.list` + fallback + tests
- [ ] Verification (both sides)

## Validation

(Filled in as each Acceptance Criterion is confirmed.)
