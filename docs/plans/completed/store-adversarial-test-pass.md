# Store adversarial test pass

## Decisions

- Keep the existing store API and schema shape unchanged.
- Treat declared account foreign keys as an intended invariant and enforce them at the SQLite connection level.
- Add connection-level busy timeout to make concurrent writes wait briefly instead of immediately returning `SQLITE_BUSY`.

## Progress

- Read `internal/store/*.go` and existing `internal/store/store_test.go`.
- Added tests for missing-row lookups, empty lists, missing account references, database reopen idempotence, and concurrent account creates.
- Fixed store opening so every SQLite connection enables foreign keys and a busy timeout via DSN pragmas.

## Validation

- `GOCACHE=/tmp/smind-go-cache go test ./internal/store/...`
- Full required validation was run after implementation; see the agent report for raw command output.
