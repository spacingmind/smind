# AGENTS.md

Repository protocol entrypoint for agents working in this repo.

## Project overview

See [README.md](README.md) for what smind is and how to build/run it.

## Workspace map

- `cmd/` — Go binary entrypoints (`cmd/smind` is the daemon).
- `internal/config` — config loading (`~/.spacingmind/config.yaml`, `SMIND_HOME` override).
- `internal/server` — HTTP server: API routes + embedded web UI (`internal/server/dist`).
- `web/` — bun workspace root for the web UI (`web/packages/ui`, React + Vite).
- `docs/` — architecture notes, ADRs (`docs/decisions/`), and active/completed plans (`docs/plans/`).
- `refs/` — read-only reference clones of other projects for pattern lookup (see below).

## Workflow rules

**(a) Read-only questions.** Inspect the smallest relevant surface for the
question, then answer with evidence (file paths, line numbers, quoted code).
Don't guess at behavior you haven't read.

**(b) Bounded changes.** Make the smallest coherent change that satisfies the
request. Run `task test` and `task lint` before considering the change done.

**(c) Multi-session work.** Create `docs/plans/active/<slug>.md` with
sections for decisions, progress, and validation. Keep it updated as the work
proceeds. When the work is finished, move the file to `docs/plans/completed/`.

**(d) Material ambiguity.** If a choice materially affects architecture (data
model, routing behavior, public API shape, etc.) and isn't already decided in
`docs/decisions/`, STOP and present the choice to the user. Do not decide
architecture unilaterally.

## refs/ map

`refs/` holds read-only clones of other projects, used as pattern references
— not dependencies, not code to copy wholesale.

> Note: these clones are not present in this checkout yet. Once added, treat
> them as read-only reference material only.
