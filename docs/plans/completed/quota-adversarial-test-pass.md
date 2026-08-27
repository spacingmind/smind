# Quota adversarial test pass

## Decisions

- Keep the public `internal/quota` API unchanged.
- Do not add store query/pruning methods during this pass; report store-surface concerns for orchestration.

## Progress

- Read `AGENTS.md`, existing quota implementation/tests, and quota snapshot store methods/schema.
- Identified quota test gaps around corrupt cached data, zero/negative TTLs, zero-value usage, latest snapshot selection, and concurrent expiry.

## Validation

- `go test -race ./internal/quota/...` — pass
- `task build` — pass
- `task test` — pass
- `task lint` — pass

Note: the codex agent running this pass hit a provider usage-limit error after
writing these tests but before committing/finalizing this doc. The
orchestrating session verified the tests independently (all green, see
above) and committed them as-is.
