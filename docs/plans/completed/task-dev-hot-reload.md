# task dev — single-command hot reload

## Acceptance Criteria

- `task dev` (Taskfile.yml) starts both halves of the dev loop with
  hot reload and stays in the foreground until interrupted:
  - the Go daemon with auto-reload (today's `task dev:go` comment says
    "requires air" but the task doesn't actually invoke air — pick one:
    either actually wire [air](https://github.com/air-verse/air) with a
    committed `.air.toml`, or use a small Go-based watcher; justify the
    choice in Decisions),
  - the Vite dev server for the web UI (`task dev:web` — already
    exists, proxies API to :4648),
  - both under one Ctrl+C (one Ctrl+C kills both, no orphaned
    processes).
- The existing `task dev:go` / `task dev:web` tasks keep working
  standalone for people who only want one half.
- Update `docs/ROADMAP.md` Phase 0: tick the `task dev` TODO.

## Test Scenarios

- Manual: run `task dev`, edit a `.go` file, confirm the daemon
  rebuilds and restarts; edit a file under `web/packages/ui/src`,
  confirm Vite hot-reloads; Ctrl+C once, confirm both processes exit
  (`ps` check, no orphans) — same honesty standard as prior tasks,
  record what was actually verified in Validation.
- No new Go/web unit tests required — this is build tooling; the
  existing suites must still pass (`go build ./...`, `go test ./...`,
  `bun run test`).

## Decisions

- Use Air v1.67.4 via `go run`, configured in the committed `.air.toml`.
  This keeps the reload tool version reproducible without adding a binary to
  the repository or requiring contributors to install it globally.
- `task dev` starts `task dev:go` and `task dev:web` in separate sessions via
  `setsid`. Its interrupt/exit trap terminates both process groups and waits
  for them, so Air/Vite and the daemon they manage do not survive Ctrl+C.

## Progress

- [x] `task dev` composing daemon + web dev servers, one Ctrl+C
- [x] ROADMAP tick + manual verification recorded

## Validation

- `go build ./...` and `go test ./...`: passed.
- `bun run test` from `web/`: passed (46 tests). The workspace root now
  forwards that command to the UI package test script.
- `task lint` and `task test`: passed.
- Manual, 2026-09-07: started `task dev` with an isolated `SMIND_HOME`
  configuration on port 14648 because another worktree already owned the
  default daemon port. Both the daemon health endpoint and Vite on :5173
  became available. A temporary whitespace-only Go edit caused Air to build
  and restart the daemon (the daemon PID changed); a temporary `App.tsx`
  comment produced Vite's `hmr update /src/App.tsx` log. Both edits were
  reverted. One Ctrl+C stopped Air, its daemon, Vite, and their Task
  wrappers; a subsequent `ps`/`ss` check found no matching process and no
  listener on :14648 or :5173.
