# TypeScript to Go live relay interop in CI (E12): it already exists; close its trigger gap

Status: active (spec, 2026-11-15). Size: S (2): scope 1 (one workflow), PR 1, risk 1, decisions 1, verification 0. Code starts from a fresh `develop`.

## What already exists on `develop`

The requested "TS to Go live relay interop test in CI" is already built and required:

- `.github/workflows/mobile-check.yml` (required on develop) runs `npm run test:integration` in `mobile/`: eight node suites (`integration.node`, `.persistent`, `.devices`, `.password`, `.queue`, `.stop`, `.replay`, `.newtask`) that build and start `internal/relay/bridge/devharness`, a real Go relay, a real multi-device daemon hub with a device registry, Go desktop peers and a fake agent, and drive the real TypeScript `RelayConnection` against it (E2EE v2, pairing, approve/revoke, restarts, passwords, queueing, replay).
- `desktop-check.yml` and `desktop-windows.yml` do the same for the Rust client against the same harness (`tests/relay_harness_interop.rs`).
- The web UI has no relay client (hosted web is parked), so there is no third TypeScript client to add.

So a new interop suite would duplicate coverage. The gap is when it runs.

## The gap

`mobile-check.yml` decides `relevant` from a path filter: `^(mobile/|internal/relay/|\.github/workflows/mobile-check\.yml$)`. The TypeScript client talks to the daemon over the relay with the daemon's RPC surface (`internal/wsapi/`), but a PR that changes only `internal/wsapi/` (a renamed field, a changed method) skips the live interop run, so wire drift between the daemon and the mobile client is found after merge, or never. The same holds for the shared types the mobile client decodes (inbox, tasks, permissions, push seal).

## Acceptance criteria

1. The `relevant` filter also matches `internal/wsapi/`, `internal/inbox/` (served as RPC) and `internal/push/` (the sealed push format the mobile client opens), and the workflow's own comment says why each prefix is there.
2. A PR touching only one of those prefixes runs the integration suites (checked on a throwaway PR or by running the filter's `git diff | grep -qE` against a sample diff list); a docs-only PR still reports success immediately (the required check must never wait).
3. The filter regular expression lives in one place in the workflow (no copy in another job), so desktop-check's filter is read once and either left alone or aligned in the same PR with a stated reason.
4. The cost is stated: the integration job is about USD 0 (hosted runner) and its measured duration on the last green run is quoted in the PR; if a wsapi-only PR makes the median PR job longer than 10 minutes, the PR says so and proposes narrowing to `internal/wsapi/` plus `internal/push/`.
5. No product code changes.

## Named checks

- `mobile-check-filter`: a small shell test (`scripts/mobile-check-filter_test.sh`, POSIX, no network) that feeds sample changed-file lists to the regular expression extracted from the workflow and asserts: `mobile/x.ts` yes, `internal/relay/a.go` yes, `internal/wsapi/rpc.go` yes, `internal/inbox/inbox.go` yes, `internal/push/seal.go` yes, `docs/a.md` no, `web/packages/ui/a.tsx` no, `internal/store/a.go` no. Wired into the existing script-test job if there is one, otherwise run by hand and quoted.
- Evidence: the PR's own run shows the integration step executing when a wsapi file is in the diff.

## Decisions

- D1 (recommended): widen the filter; do not add a second interop suite.
- D2 (recommended): keep `internal/store`, `internal/runs` and the rest out: they reach the client only through `wsapi` and the push seal, which the filter now covers.
- Out of scope, paused with the wire-schema epic: a generated schema for the WebSocket API. When it lands, the interop job is where its drift check belongs.

## Progress

- [x] Inventory of what exists (this document).
- [ ] PR: filter, comment, shell test.

## Validation

(filled in per PR)
