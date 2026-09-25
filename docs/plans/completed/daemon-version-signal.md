# Daemon version signal

## Context

ADR-0013, in its version-skew sub-decision (user-approved 2026-09-25), needs the daemon to report its own version, so the desktop app can compare it against its own and offer "update daemon and restart". The flow is modeled on Paseo's `shouldRestartForVersion`.

Today smind has no version concept at all:
- `Taskfile.yml`'s `build` runs a plain `go build`, with no ldflags;
- there is no `--version`;
- `GET /healthz` (`internal/server/server.go`, `handleHealth`) returns only `{"status":"ok","service":"smind"}`.

The release version lives in `.release-please-manifest.json` (`{".": "0.7.0"}`), because release-please uses the "simple" release type.

The user approved a **small, additive** daemon change: no new endpoint, and no change to existing fields.

## Acceptance Criteria

- **AC1.** A new `internal/version` package exposes `Version` (a `var`) with default `"dev"`, set at build time with `-ldflags "-X github.com/spacingmind/smind/internal/version.Version=<v>"`. It may also expose a `Commit` var, set the same way, if cheap.
- **AC2.** `task build` stamps the version.
  - A clean tree at a release tag gets the manifest version, e.g. `0.7.0`.
  - Otherwise it gets `<manifest>-dev+<shortsha>`, with `.dirty` added if the tree is dirty.
  - Computing it needs only `git` and the manifest file; no new tool dependencies.
  - A plain `go build` without ldflags still works and reports `dev`.
- **AC3.** `smind --version` and `smind version` print `smind <version>` (plus the commit, if present) and exit 0. They do not need a running daemon.
- **AC4.** `GET /healthz` returns `{"status":"ok","service":"smind","version":"<v>"}`. The existing fields and status code are unchanged.
- **AC5.** No other API change. Specifically:
  - no new endpoint;
  - no wsapi change;
  - `/api/token` and `/ws` are untouched;
  - the embedded web UI is unaffected.
- **AC6.** `task test` and `task lint` are green.

## Test Scenarios

- A `/healthz` handler test asserting all three fields, including `version` = the injected value. Set `version.Version` in the test and restore it afterwards.
- A CLI test that `smind --version` / `smind version` output contains the version and exits 0 with no daemon running. Follow the existing CLI test patterns in `cmd/smind`.
- A unit test for the version-string computation, if it's done in Go. If it's a Taskfile shell snippet, verify it manually and record the outputs for a clean tree and a dirty tree in Validation.
- A regression check: the existing `/healthz` consumers still parse the response. These are `desktop/daemon-client` (after #194 lands) and the web UI's reconnect logic (`web/packages/ui/src/lib/reconnect.ts`). They only read `status`; confirm by reading the code.

## Decisions

- The version comes from the release-please manifest. It is the single source of truth, and it is what release tags use.
- `internal/version` is kept tiny and dependency-free, so the future desktop app and the relay binary can share it.

## Progress

- [ ] AC1 internal/version
- [ ] AC2 Taskfile stamping
- [ ] AC3 --version / version
- [ ] AC4 /healthz field
- [ ] AC5/AC6 regressions, test + lint

## Validation

To be filled in.
