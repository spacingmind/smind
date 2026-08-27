# 0002: Bun workspaces for web

## Status

Accepted

## Decision

Web/TypeScript code lives in a `web/` bun workspace root, with individual
packages under `web/packages/` (starting with `packages/ui`; `packages/shared`
to follow as more surfaces are added). The Go module stays at the repo root.
`Taskfile.yml` is the glue between the two — it drives `bun` for the web build
and `go` for the daemon build.

## Rationale

Keeping the Go module and the bun workspace as separate, top-level trees
avoids mixing Go and JS tooling conventions in one root, while `Taskfile.yml`
gives a single entrypoint (`task build`, `task dev:web`, etc.) so contributors
don't need to know which tool to reach for.
