# Workspace agent-readiness check (B6)

Status: active (2026-10-27). Human pre-approved the checklist (A-D); no ADR needed (one read-only RPC, no new data model).

Size: M (4) — scope 2 (daemon + CLI + web), PRs 1, risk 1 (reads repo files that an agent may have edited), decisions 0, verification 1 (UI screenshots).

## What it is

A static, offline check of a workspace's repo: "would an agent be productive here?". It lists findings (pass or warn) with evidence and a one-line, non-mutating fix hint. It reads text files only. It never runs repo code, never touches the network, never writes, never calls a model.

## Acceptance Criteria

- AC1 `internal/readiness.Check(ctx, repoDir, Options)` returns findings `{id, status pass|warn|info, title, evidence, fix}` in a fixed order. Checks: `agent-instructions` (AGENTS.md or CLAUDE.md, at least 200 bytes), `test-command`, `lint-command`, `setup-script` (from `Options.HasSetupScript`, the workspace record), `gitignore` (exists; covers `.env`, `node_modules` when a JS ecosystem is present, and a build dir such as `dist`/`build`/`target`), `tracked-env` (warn when a tracked `.env*` file exists; `info` "skipped" when `Options.TrackedEnv` is nil or fails), `ci-config`, `docs` (a `docs/` dir or README), `lockfiles` (per detected ecosystem), `conventions` (CONTRIBUTING, ADR or plan folder; `info` only, never warn).
- AC2 Fix text is generic prose, never a command to run.
- AC3 Reads are bounded: per-file cap 64 KiB (extra bytes ignored), directory listings capped at 500 entries, no recursion beyond named paths; a symlink is never followed (an `Lstat` on every path; a symlinked file counts as absent with evidence "symlink, not followed"); nothing outside `repoDir` is opened.
- AC4 Repo code is never executed: scripts in a Taskfile/Makefile/package.json are only text-matched for existence (a `test`/`lint` target or script name).
- AC5 RPC `workspace.readiness {workspaceId}` is read-only, same guard as `workspace.get`: local and relay (a paired device already reads the workspace and its scripts), **not** run-scoped (agents are refused: `RunScopedMethods` stays as is), not a plugin method (added to the never list). Result is `{workspaceId, findings[], checkedAt}`, at most 32 findings, strings capped at 300 bytes. Unknown workspace → error.
- AC6 CLI `smind workspace readiness [id] [--json]`: all workspaces when omitted; plain table (status, title, evidence, fix line); exit 0 whatever the findings, 1 only for an unknown workspace or a daemon error.
- AC7 Web: an "Agent readiness" section in the workspace settings dialog (`workspace-scripts-dialog`), pass/warn icons from `lib/icons.ts` (drift snapshot updated), one-line fix text, a Re-check button, loading and error states; light+dark screenshots at 1280 and 390 reviewed. Mobile: not done.
- AC8 Docs: a paragraph in `public-docs/workspaces-and-tasks.md`; `public-docs/cli-reference.md` regenerated with `task docs:cli`.

## Test scenarios

Go `internal/readiness`:
- `TestReadiness_PassingRepoFixture`: a complete fixture repo has no warn.
- `TestReadiness_EmptyRepo_AllWarnWithFix`: an empty dir warns on every warnable check, each with a non-empty fix and no mutating command.
- `TestReadiness_NeverExecutesRepoCode`: hostile Taskfile/Makefile/package.json/.envrc touching a canary file; the canary is absent after Check.
- `TestReadiness_SymlinkOutOfRepoNotFollowed`: `AGENTS.md` and `.gitignore` symlinks to a file outside the repo count as absent; the outside content never appears in a finding.
- `TestReadiness_BoundedReads`: a 50 MiB package.json and a directory of 20k entries finish quickly, with bounded allocation, no panic.
- `TestReadiness_TrackedDotEnvWarns`: a stub `TrackedEnv` returning `.env` warns; returning an error gives `info` "skipped"; `.env.example` does not warn.
- `TestReadiness_WarnNotPass`: each check, given the failing fixture, reports `warn` (guards a mutation that flips status).

Go `internal/wsapi`: `TestWorkspaceReadiness_RPC_Guards` (local ok; relay ok; run-scoped refused; unknown id errors; result capped).
Go `cmd/smind`: `TestWorkspaceReadiness_CLI_Table_And_JSON`.
Web: `readiness-panel.test.tsx` (loading, findings, error, Re-check re-calls the RPC).

## Mutations (each must fail its test)

1. Execute a script (add an `exec` of the test script) → canary test fails.
2. Follow symlinks (`Stat` instead of `Lstat`) → symlink test fails.
3. Remove the read cap → bounded-reads test fails.
4. Report a warn as pass → the empty-repo test fails.
5. Drop the RPC guard (add the method to the run-scoped list) → guards test fails.

## Decisions

- **Relay and agents.** `workspace.get` has no relay guard and is refused to run-scoped agents; the readiness result holds only repo-relative paths and fixed text, so it follows `workspace.get`. Options considered: also allow agents (rejected: not needed, widens the allowlist); local only (rejected: the mobile/web-over-relay user reads the same workspace already). Most conservative that works: mirror `workspace.get`.
- **`.env` tracked check.** Uses `git ls-files` through the daemon's hardened git runner (`core.fsmonitor=false`, no prompts) with pathspecs restricted to `.env*`; injected as a func so the package stays git-free and testable. Skipped as `info` on any error.
- **Test/lint discovery** is by name only (a target `test`/`lint` in a Taskfile or Makefile, a `test`/`lint` script in package.json, `go.mod`, `pyproject.toml` with pytest/ruff, `Cargo.toml`); no parsing beyond line matching and `encoding/json` on a capped read.
- **Where the check runs:** the workspace path itself (the user's repo), not a task worktree.
- **No new data model, no persistence, no event topic.** Each call recomputes.

## Progress

- [ ] plan PR
- [ ] readiness package + tests
- [ ] RPC + CLI
- [ ] web panel + screenshots
- [ ] docs

## Validation

(filled in when done)
