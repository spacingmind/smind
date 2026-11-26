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

- [x] plan PR (#675)
- [x] readiness package + tests
- [x] RPC + CLI
- [x] web panel + screenshots
- [x] docs

## Validation

- AC1-AC4: `internal/readiness` tests (all named scenarios pass, `-race`). Mutations, each run once and each failed its test: exec a Taskfile command -> `TestReadiness_NeverExecutesRepoCode`; `os.Stat` for `os.Lstat` -> `TestReadiness_SymlinkOutOfRepoNotFollowed`; read cap removed -> `TestReadiness_BoundedReads`; warn reported as pass -> `TestReadiness_EmptyRepo_AllWarnWithFix` (+3 more).
- AC5: `TestWorkspaceReadiness_RPC_Guards` (local, relay, run-scoped refused, unknown id, cap). Mutation: `workspace.readiness` added to `RunScopedMethods` -> that test fails. `workspace.readiness` is in the plugin never list (`TestPluginSession_CannotReachHumanOnlyMethods`). The `.env` listing is `workspace.TrackedEnvFiles` (`TestTrackedEnvFiles`).
- AC6: `TestWorkspaceReadiness_CLI_Table_And_JSON`; live against an isolated daemon (own `SMIND_HOME`, port 4931): table, `--json`, unknown id exits 1.
- AC7: `readiness-panel.test.tsx` (loading, findings, error, Re-check). Screenshots light+dark at 1280 and 390 plus 12-frame open and Re-check transition sheets, reviewed (`/tmp/b6/frames`). The review found the dialog had no max height, so a tall dialog was clipped at top and bottom; fixed with `max-h-[calc(100dvh-2rem)] overflow-y-auto` on the dialog. No new icon (reuses success/warning/info/refresh), so the icon drift snapshot is unchanged.
- AC8: public-docs paragraph; `task docs:cli` regenerated; docs drift tests pass.
- `task lint`, `tsc -b`, `go test -race` on readiness, wsapi, workspace, cmd/smind pass. Full vitest: 3898 pass; `open-in-editor.test.tsx` failed 3 tests once under load and passes alone (8/8).

Not done: mobile; any web mutation run; no live LLM calls were made.

## Dogfood pass (workspace 4 "smind (Mac)", 2026-10-31)

Ran `smind workspace readiness` on the smind repo itself: 7 pass, 2 warn, 1 info.

- `gitignore` warned (".gitignore does not ignore: .env"). Fixed in this repo: `.env`, `.env.*` ignored, `.env.example|sample|template` kept. Re-run against the branch (temporary `Check` call on the worktree path, `Options{}`): `gitignore` flips from warn to pass; with the old `.gitignore` restored it warns again (the mutation).
- `setup-script` still warns: it is a workspace setting (the record's setup script), not repo content. Human action, no repo change.

Follow-ups noticed in the check itself (not fixed here): `lint-command` accepts a `check-types` script as lint (type-checking is not linting); `lockfiles` only looks at the repo root, so nested `web/bun.lock` is not seen; `ignores` skips negations and nested-path globs.

## Checker follow-ups (FU1-FU5)

Fixes to the checker found by the dogfood pass above and by reading it against the smind repo (Go + `web/` bun + `mobile/` npm) and the landing repo (bun + turbo, root `workspaces: [apps/*, packages/config]`). Pass/warn/info semantics and message text stay as they are unless a bullet says otherwise.

### Acceptance criteria

- **FU1 lint is not typecheck.** A `check-types`/`typecheck`/`tsc` script (package.json, Taskfile or Makefile target) alone does not satisfy `lint-command`. Names that count: containing `lint`, `format`, `fmt`, `vet`, `eslint`, `prettier`, `biome`, `golangci`; a bare `check` counts only when its body names a lint tool. When a type-check-only script is the only candidate, the warn evidence says so ("type checking alone is not lint or format").
- **FU2 nested lockfiles.** A `package.json` one level into a candidate package dir (the root `workspaces` globs of the form `dir/*` or `dir`, plus `apps/*`, `packages/*`, and any top-level dir holding a `package.json`) counts when a lockfile sits in its own dir or at the repo root. Lock missing in both: warn `web/package.json has no lockfile`. The root manifest rule is unchanged (root lockfile required). A `go.mod` with no `require` does not need a `go.sum`.
- **FU3 workspace tests.** `test-command` also passes when a candidate package dir's `package.json` has a real `test` script (evidence `apps/web/package.json script test`). A root `test` script (including `turbo test`) keeps passing as before.
- **FU4 .gitignore matcher.** Last matching line wins; `!pattern` re-includes; `dir/**`, `**/dir`, leading `/`, trailing `/`, `*`, `?` handled. Checked names: `.env`, `node_modules`, build dirs (`dist build target out bin`).
- **FU5 other.** Whatever else the review finds, listed in Progress.

### Named tests (internal/readiness/readiness_followups_test.go, table-driven)

- `TestLint_TypecheckAloneIsNotLint` (package.json, Taskfile, Makefile rows; lint+typecheck passes; warn evidence note)
- `TestLockfiles_NestedPackages` (landing layout, smind layout, nested missing lock, root lock covers workspace, go.mod without require)
- `TestTestCommand_WorkspacePackages` (landing before/after #33 layouts, apps/web test script, no test anywhere warns, npm "no test specified" ignored)
- `TestIgnores_Gitignore` (negation, `.env*` + `!.env.example`, `.env` then `!.env`, `dist/**`, `/dist`, `dist/`, `**/node_modules`, comments, trailing spaces)
- Existing tests in readiness_test.go keep passing unchanged.

Mutations (one per fix, test must fail): FU1 put `check-types` back in the lint name match; FU2 skip the nested scan; FU3 skip workspace scripts; FU4 ignore `!` lines again.

### Follow-ups: Progress and Validation

- [x] FU1 lint vs typecheck, FU2 nested lockfiles, FU3 workspace test scripts, FU4 gitignore matcher, FU5 (found while running on the smind repo: `npm-stub/package.json` has no dependencies and must not need a lockfile; `go.mod` without `require` needs no `go.sum`).
- Tests: `TestLint_TypecheckAloneIsNotLint`, `TestLockfiles_NestedPackages`, `TestTestCommand_WorkspacePackages`, `TestIgnores_Gitignore`, `TestCheckGitignore_Layouts`; `-race` passes. One mutation per fix, each failed its test: typeName guard removed (FU1), nested scan disabled (FU2), workspaceTest disabled (FU3), `!` ignored (FU4), hasDeps disabled (FU5).
- Real check (library, read-only, before = origin/develop checker, after = this branch): smind `lockfiles` pass go.sum -> pass go.sum, desktop/, docs-site/, mobile/, web/bun.lock; landing without root test script: `test-command` warn -> pass `apps/web/package.json script test`; landing `lint-command` pass (check-types) -> warn with the typecheck note.
