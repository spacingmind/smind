# Architecture

Spacing Mind runs as a single Go daemon (`cmd/smind`, default `:4648`) that
serves the API and the embedded web UI from one binary — see
[decisions/0001-go-single-binary.md](decisions/0001-go-single-binary.md).

## Data model (planned)

```
Workspace -> Space -> Task -> Tabs
```

- **Workspace** — a top-level project/environment an agent operates in.
- **Space** — a grouping of related tasks within a workspace.
- **Task** — a unit of agent work; owns one or more tabs.
- **Tabs** — individual views/sessions (e.g. terminal, diff, chat) within a task.

## Routing engine (phase 1)

The routing engine maps agent traffic to provider accounts:

- **Accounts** — individual provider credentials (e.g. an Anthropic account).
- **Pools** — groups of accounts that can share load.
- **Hard isolation vs. pool policies** — some workspaces require a dedicated
  account with no sharing (hard isolation); others can draw from a pool under
  a routing policy (e.g. round-robin, least-loaded).

## Surfaces roadmap

1. **Web** — the initial surface, served by the Go daemon (`web/packages/ui`).
2. **Mobile** — Expo app, reusing the same API.
3. **Desktop** — Tauri app, reusing the same API and web UI bundle.
