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

- [x] AC1 internal/version (8fd165b)
- [x] AC2 Taskfile stamping (0aae715)
- [x] AC3 --version / version (e373b7b)
- [x] AC4 /healthz field (8fd165b)
- [x] AC5/AC6 regressions, test + lint

## Validation

Verified 2026-09-25 by the coordinator on a rebased branch (on develop at 2c9a511).

- **AC1:** `internal/version` exposes `Version` (default `"dev"`) and `Commit`. Both are set with `-X`, and the package has no imports.
- **AC2:** `task build` on a clean tree printed `building smind 0.7.0-dev+cbd86bd (cbd86bd)`. After touching a tracked file it printed `0.7.0-dev+cbd86bd.dirty`. The release-tag branch (`v<manifest>` exact match) gives the bare manifest version, per the Taskfile logic. That path wasn't exercised because there is no local release tag at HEAD. A plain `go build` / `go test` still reports `dev`.
- **AC3:** `./bin/smind --version` and `./bin/smind version` both printed `smind 0.7.0-dev+cbd86bd.dirty (cbd86bd)` with no daemon running. There is also a CLI test in `cmd/smind`.
- **AC4:** a live daemon on a temp `SMIND_HOME` and port 4702 answered `GET /healthz` with `{"service":"smind","status":"ok","version":"0.7.0-dev+cbd86bd.dirty"}`. A handler test asserts all three fields.
- **AC5:**
  - The diff touches only `internal/version`, `internal/server/server.go` (one field), `cmd/smind` (the version subcommand) and `Taskfile.yml`. There is no new endpoint and no change to wsapi, `/api/token`, `/ws` or `web/`.
  - The existing `/healthz` consumers only read `status`: the web UI reconnect probe and `desktop/daemon-client`.
- **AC6:**
  - `task lint` is green.
  - `task test` is green except `internal/relay/client` `TestIntegrationMobileDisconnectReconnectDeliversBufferedFrames`. That test failed once and passed 2 of 3 isolated reruns, and this change doesn't touch relay code, so it is a pre-existing timing flake, in the same family as the known CI wsapi flakes.
