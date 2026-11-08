# AGENTS.md

Repository protocol entrypoint for agents working in this repo.

## Project overview

See [README.md](README.md) for what smind is and how to build/run it.

## Workspace map

- `cmd/` — Go binary entrypoints (`cmd/smind` is the daemon).
- `internal/config` — config loading (`~/.smind/config.yaml`, `SMIND_HOME` override).
- `internal/server` — HTTP server: API routes + embedded web UI (`internal/server/dist`).
- `web/` — bun workspace root for the web UI (`web/packages/ui`, React + Vite).
- `docs/` — architecture notes, ADRs (`docs/decisions/`), and active/completed plans (`docs/plans/`).
  New research notes (competitor/reference studies, pplx findings) go in
  `docs/research/local/` — gitignored, never committed; plans may cite
  them as local-only.
- `refs/` — read-only reference clones of other projects for pattern lookup (see below).

- `.agents/skills/` (symlinked into `.claude/skills/`, pinned in `skills-lock.json`) — agent skills: frontend (`web-design-guidelines`, `vercel-composition-patterns`, `vercel-react-best-practices`, `frontend-design`), mobile (`expo-*`, `sleek-design-mobile-apps`, `vercel-react-native-skills`), Go (`golang-*`), process (`plan`, `adr`, `verify`, `diagnosing-bugs`, `writing-for-agents`, `retro`, `wizard`, `code-review`, `single-agent-loop` — audit/fold multi-agent setups, notes in `docs/plans/active/single-loop/`). Agents that don't auto-load skills should read the relevant `SKILL.md` before UI work.

## Workflow rules

**(a) Read-only questions.** Inspect the smallest relevant surface for the
question, then answer with evidence (file paths, line numbers, quoted code).
Don't guess at behavior you haven't read.

**(b) Bounded changes.** Make the smallest coherent change that satisfies the
request. Run `task test` and `task lint` before considering the change done.

**(c) Multi-session work — spec-driven.** Before implementation starts,
create `docs/plans/active/<slug>.md` with concrete acceptance criteria and
named test scenarios (the spec), plus sections for decisions, progress, and
validation (how the spec got satisfied). See the `plan` skill. Keep it
updated as work proceeds. When finished — every acceptance criterion
confirmed in Validation — move the file to `docs/plans/completed/`.

**(d) Material ambiguity.** If a choice materially affects architecture (data
model, routing behavior, public API shape, etc.) and isn't already decided in
`docs/decisions/`, STOP and present the choice to the user. Do not decide
architecture unilaterally.

**(e) Multi-agent work.** When work is delegated (Human/Steward → Lead →
optional Peers), follow [docs/WORKSPACE_PROTOCOL.md](docs/WORKSPACE_PROTOCOL.md):
complexity-point sizing, one owner per write scope, the brief and the Peer's
json handoff block (`status` done/blocked/failed), explicit accept/reject, and
process safety (isolated sandboxes; kill only the PIDs you started).

## Vendored process skills

`diagnosing-bugs`, `writing-for-agents`, `retro`, `wizard` and `code-review`
are vendored unmodified (MIT, see `NOTICE`); smind-specific meaning is mapped
here, never edited into the skill files:

- "issue tracker" means GitHub PRs and issues, with `docs/plans/active/<slug>.md`
  as the spec source (what `code-review` calls the originating issue/spec).
- `GLOSSARY.md` is optional; when it is absent, skip it.
- `code-review`'s fixed point defaults to `origin/develop`.
- The vendor's repo-setup skill is not used; this section replaces it.

## refs/

`refs/` holds local, read-only reference clones; see `refs/README.local.md` on your machine.

Day-to-day style (Go idioms, React/TS conventions) is enforced by linters
(`go vet`/`gofmt`, and ESLint/Prettier once added to `web/`), not by reading
refs/. Reach for refs/ when stuck on a specific hard pattern (routing,
worktree lifecycle, ACP wiring), or when a dependency's own docs are the
better source of truth.
