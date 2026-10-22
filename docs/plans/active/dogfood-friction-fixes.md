# Dogfood friction fixes (CLI-driven orchestration)

Size: **M (4)** — scope 1 (CLI + one daemon helper), PRs 2 (five small, non-stacked), risk 0, decisions 1 (two open questions, neither blocks), verification 0 (unit tests plus a fake-daemon CLI test).

Source: the 2026-10-22 dogfood log (Phase 2 DoD in `docs/ROADMAP.md`: orchestrate real work through smind instead of Paseo). The log is a local-only research note; this plan restates each item with its verified status.

## Goal

A Lead can drive Peers entirely from the CLI without babysitting the shell, reading kilobytes of ACP noise, or touching the main checkout: start a run and detach, pick the model and thinking level, fork a task from a fresh remote ref or an unmerged PR branch, and get short readable output.

## Verification of each candidate (against `origin/develop` 164a596b)

| # | Log item | Status in code | Action |
|---|---|---|---|
| 1a | `task send` has no `--model` | **Already shipped** (ADR-0025, `cmd/smind/task.go` `--model`, in the usage line since v0.11.0). The log entry was wrong or read an older binary. | none |
| 1b | no `--thinking` | `run.start` takes `thinkingLevel` (`off`/`standard`/`extended`, `internal/taskrunner/thinking.go`); the CLI never sends it. | **PR 1** |
| 1c | claude-native "Default" runs the CLI's default (Opus here) | `ClaudeModelDefault` sends no `--model` (`internal/taskrunner/runner.go:781`). The CLI cannot know what the CLI resolves to. Profiles already carry a `model` (ADR-0025). | **PR 1**: a stderr note when claude-native runs unpinned, and a MODEL column in `task runs`. A config-level per-provider default model is an open question (O1). |
| 2 | no `--detach` | `send` always calls `streamRun`. | **PR 1** |
| 3a | `[raw]` noise | `streamRun`/`printRunLogs` print every `raw` event (`renderRaw`). | **PR 2** |
| 3b | `--auto-accept` prints full permission blocks | `renderPermissionRequest` always prints the full block. | **PR 2** |
| 4 | new task base | `task.create` already accepts `baseRef`, `branch`, `prNumber` (`internal/wsapi/handlers.go:583`), and the MCP `task_new` exposes them. ADR-0039 G1 stores `base_ref`/`base_sha`, but §2 deliberately keeps the *fork point* of the default mode at the workspace checkout's HEAD (`default_base` only sets the PR target). The **CLI** `task new` exposes none of them. | **PR 3**: `--base`, `--branch`, `--pr` on `task new`. Changing the default fork point is O2. |
| 5 | long branch/worktree names | `slugify` (`internal/workspace/task.go:291`) has no length cap. | **PR 4** |
| 6 | no `node_modules` in task worktrees | The setup-script feature (ADR-0024, `smind workspace setup set`) already covers it. | **PR 5**: sample script and docs. |
| 7 | other | Log items 1–3 (desktop PATH, daemons) are owned by other Leads; 9 (`logs -f`) exists already; 14 (commit trailer) is a policy call. | none |

## Pull requests

All branch from `origin/develop`; none stack. PR 1 and PR 2 both edit `cmdTaskSend`'s flag parser; PR 2 starts after PR 1 merges, to avoid a conflict rather than stack on it.

| PR | Content | Files |
|---|---|---|
| 0 | this plan | `docs/plans/active/dogfood-friction-fixes.md` |
| 1 | `task send --detach`, `--thinking <off\|standard\|extended>`, claude-native unpinned-model note, `task runs` MODEL column | `cmd/smind/task.go` + tests |
| 2 | hide `raw` events unless `--verbose` (send, attach, logs); one line per permission decision with `--auto-accept` | `cmd/smind/task.go`, `permission_detail.go` + tests |
| 3 | `task new … [--base <ref>] [--branch <name>] [--pr <n>]` | `cmd/smind/task.go` + tests |
| 4 | cap the task slug | `internal/workspace/task.go` + test |
| 5 | sample task-worktree setup script + docs; close this plan | `scripts/`, `docs/` |

## Acceptance criteria

- **AC1** `smind task send <id> <provider> <prompt> --detach` calls `run.start`, prints the run id on stdout (one line, `run <id> started` on stderr as today), makes no `run.attach` call, and exits 0. A queued result (`--when-busy queue`) behaves as today.
- **AC2** `task send --thinking off|standard|extended` sends `thinkingLevel` in `run.start`; any other value exits 2 with the allowed list; omitted sends no field (unchanged).
- **AC3** When the provider is `claude-native` and no `--model` was given, `task send` prints one stderr line saying the model is the Claude CLI's own default and how to pin one. Nothing is printed when `--model` is given or for other providers.
- **AC4** `task runs` has a MODEL column: the requested model; when unpinned, the model the agent reported using, in parentheses (this is how a user sees what "Default" resolved to); `-` when neither is known.
- **AC5** `raw` events are not printed by `task send`, `task attach` or `task logs` unless `--verbose` is passed; with `--verbose` the output is byte-identical to today.
- **AC6** With `--auto-accept`, a permission request that the daemon resolves automatically prints exactly one line (`[permission] auto-accepted <summary> -> <optionId>`), not the block. Without `--auto-accept`, output is unchanged. `task attach`/`logs` on a run started with auto-accept print the same one-liner (the decision is derived from the `permission_resolved` reason `auto_accept`, not from a flag on the viewer).
- **AC7** `task new <ws> <title> --base <ref>` sends `baseRef`; `--branch <name>` sends `branch`; `--pr <n>` sends `prNumber`. More than one of the three exits 2 with a message, before any dial. Existing `--space`/`--parent` still work, in any order with the new flags.
- **AC8** A task title longer than the cap yields a slug of at most 40 characters (cut on a word boundary where possible, no trailing hyphen) plus the existing `-<stamp>` suffix; short titles are unchanged; an all-punctuation title still yields `task`.
- **AC9** `scripts/` has a sample setup script that installs `web` and `mobile` dependencies from lockfiles when the directories exist, is idempotent, and passes `shellcheck`; the docs say how to install it with `smind workspace setup set`.

## Test scenarios

| Scenario | Covers | Kind |
|---|---|---|
| `TestTaskSendDetachPrintsRunIDAndSkipsAttach` | AC1 | CLI against a fake wsapi server that records methods |
| `TestTaskSendDetachQueuedUnchanged` | AC1 | same |
| `TestTaskSendThinkingFlag` (table: valid, invalid, omitted) | AC2 | same |
| `TestTaskSendUnpinnedClaudeNote` (claude-native unpinned, pinned, glm) | AC3 | same, captured stderr |
| `TestRunModelCell`, `TestTaskRunsShowsModel` | AC4 | pure function + CLI against the fake daemon |
| `TestRenderRawHiddenByDefault` / `TestRenderRawVerbose` | AC5 | `printRunLogs` and `streamRun` with a raw event |
| `TestPermissionAutoAcceptOneLine` / `TestPermissionManualUnchanged` | AC6 | event renderers |
| `TestTaskNewBaseBranchPR` (each flag, two flags, flag order) | AC7 | fake server |
| `TestSlugifyCapsLength` (long, exact, short, punctuation, multibyte) | AC8 | `internal/workspace` |
| `TestCreateTaskLongTitleBranchLength` | AC8 | real throwaway repo |
| `shellcheck scripts/task-setup.sh` + run twice in a temp dir with a stub `npm`/`bun` | AC9 | script test |

Checks per PR: `task test`, `task lint`.

## Decisions

- D1: `--detach` prints the run id on stdout, the status chatter stays on stderr, so `RUN=$(smind task send … --detach)` works.
- D2: `raw` is hidden by default in all three viewers, not only `send`: it is the same noise in `logs`. `--verbose` restores it. No wire change.
- D3: the auto-accept one-liner keys off the resolution reason, so it needs no new wire field and works for `attach`/`logs`.
- D4: the task slug is capped in `slugify`, so every entry point (UI, CLI, MCP) gets short names. Existing tasks keep their names.
- D5: `--branch` on `task new` means "check out an existing branch" (the daemon's meaning); it does not name the new branch.

## Open questions (do not block these PRs)

- **O1: default model for claude-native.** The CLI's own default is whatever the account has (Opus here). Options: (a) leave it and rely on `--model` or a profile (this plan); (b) a `providers.<id>.default_model` in `~/.smind/config.yaml`/workspace settings (config-schema change, and "Default" in the picker would show the resolved model); (c) make the daemon default claude-native to a named alias. (b) is the real fix but changes the config schema, so it needs a Human decision.
- **O2: default fork point.** ADR-0039 §2 forks the default mode from the checkout's HEAD. Forking from `origin/<default_base>` (after a fetch) would remove the stale-develop trap entirely but contradicts a decided ADR and surprises people with unpushed local commits. This plan leaves it and gives the CLI `--base origin/develop`.

## Progress

- 2026-10-22: code verified against develop; plan written.

## Validation

(filled per PR as each AC is confirmed)
