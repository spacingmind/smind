# 0001: Go single binary with embedded web UI

## Status

Accepted

## Decision

Ship smind as a single Go binary. The daemon serves both the API and the
built web UI (embedded via `//go:embed`) from one process.

## Alternatives considered

- **Node daemon (like Paseo).** Familiar for web-heavy teams, but adds a
  runtime dependency and generally weaker throughput/concurrency for a
  routing-heavy workload.
- **Fork VS Code.** Would give a full editor for free, but pulls in a huge
  codebase and Electron/extension-host complexity that isn't needed for
  smind's scope.

## Rationale

The routing engine is the performance- and correctness-critical part of
smind — it needs to handle concurrent account/pool decisions efficiently and
deploy as a single artifact. Go fits that better than a Node daemon. Editor
needs are covered by the web UI plus CodeMirror, so forking VS Code isn't
necessary.
