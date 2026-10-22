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
| 0 | this plan | `docs/plans/completed/dogfood-friction-fixes.md` |
| 1 | `task send --detach`, `--thinking <off\|standard\|extended>`, claude-native unpinned-model note, `task runs` MODEL column | `cmd/smind/task.go` + tests |
| 2 | hide `raw` events unless `--verbose` (send, attach, logs); one line per permission decision with `--auto-accept` | `cmd/smind/task.go`, `permission_detail.go` + tests |
| 3 | `task new … [--base <ref>] [--branch <name>] [--pr <n>]` | `cmd/smind/task.go` + tests |
| 4 | cap the task slug | `internal/workspace/task.go` + test |
| 5 | sample task-worktree setup script + docs (merged, #489) | `scripts/`, `docs/` |
| 6 | per-provider `defaultModel` in config, applied at run start, shown in `provider.list`, web picker and `task runs` (O1) | `internal/config`, `internal/taskrunner`, `internal/runs`, `cmd/smind`, `web/` |

## Acceptance criteria

- **AC1** `smind task send <id> <provider> <prompt> --detach` calls `run.start`, prints the run id on stdout (one line, `run <id> started` on stderr as today), makes no `run.attach` call, and exits 0. A queued result (`--when-busy queue`) behaves as today.
- **AC2** `task send --thinking off|standard|extended` sends `thinkingLevel` in `run.start`; any other value exits 2 with the allowed list; omitted sends no field (unchanged).
- **AC3** When the provider is `claude-native` and no `--model` was given, `task send` prints one stderr line saying the model is the Claude CLI's own default and how to pin one. Nothing is printed when `--model` is given or for other providers.
- **AC4** `task runs` has a MODEL column: the requested model; when unpinned, the model the agent reported using, in parentheses (this is how a user sees what "Default" resolved to); `-` when neither is known.
- **AC5** `raw` events are not printed by `task send`, `task attach` or `task logs` unless `--verbose` is passed; with `--verbose` the output is byte-identical to today.
- **AC6** With `--auto-accept`, a permission request that the daemon resolves automatically prints exactly one line (`[permission] auto-accepted <summary> -> <optionId>`), not the block. Without `--auto-accept`, output is unchanged. `task attach`/`logs` on a run started with auto-accept print the same one-liner (the decision is derived from the `permission_resolved` reason `auto_accept`, not from a flag on the viewer).
- **AC7** `task new <ws> <title> --base <ref>` sends `baseRef`; `--branch <name>` sends `branch`; `--pr <n>` sends `prNumber`. More than one of the three exits 2 with a message, before any dial. Existing `--space`/`--parent` still work, in any order with the new flags.
- **AC8** A task title longer than the cap yields a slug of at most 40 characters (cut on a word boundary where possible, no trailing hyphen) plus the existing `-<stamp>` suffix; short titles are unchanged; an all-punctuation title still yields `task`.
- **AC10** `providers.<id>.defaultModel` loads from `config.yaml`, can be set, read and unset with `smind config set/get/unset` (comments and unrelated keys kept), and is rejected at load/set for an unknown provider id or a malformed model string. With nothing configured every provider behaves exactly as before.
- **AC11** A run that names no model resolves to the provider's configured default and records it as its requested model; an explicit model wins over it; with no default configured the run's model stays empty.
- **AC12** `provider.list` reports `defaultModel` for a provider that has one; web's model picker shows the resolved model on the Default row and in the trigger.
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
| `TestProviderDefaultModelLoad`, `...AbsentIsToday`, `...LoadRejects`, `...SetGetUnset`, `...SetValidates` | AC10 | `internal/config` |
| `TestDefaultModelProvidersMatchTaskrunner` | AC10 | keeps the provider list in step with taskrunner |
| `TestRunnerDefaultModel` | AC11, AC12 | `internal/taskrunner` (also checks `ProviderCatalog().DefaultModel`) |
| `TestStart_ConfiguredDefaultModel` | AC11 | `internal/runs` with the fake ACP agent |
| `configured default model` suite in `model-browser.test.ts` | AC12 | vitest |
| `shellcheck scripts/task-setup.sh` + run twice in a temp dir with a stub `npm`/`bun` | AC9 | script test |

Checks per PR: `task test`, `task lint`.

## Decisions

- D1: `--detach` prints the run id on stdout, the status chatter stays on stderr, so `RUN=$(smind task send … --detach)` works.
- D2: `raw` is hidden by default in all three viewers, not only `send`: it is the same noise in `logs`. `--verbose` restores it. No wire change.
- D3: the auto-accept one-liner keys off the resolution reason, so it needs no new wire field and works for `attach`/`logs`.
- D4: the task slug is capped in `slugify`, so every entry point (UI, CLI, MCP) gets short names. Existing tasks keep their names.
- D5: `--branch` on `task new` means "check out an existing branch" (the daemon's meaning); it does not name the new branch.

## Open questions: resolved

Both were answered by the Supervisor on 2026-10-22 ("Supervisor default 2026-10-22, Human may veto").

- **O1: default model for claude-native. Yes: an optional per-provider default model in config.** `providers.<id>.defaultModel` in `config.yaml` (id: `claude-native`, `codex-native`, `glm`, `kimi`), settable with `smind config set providers.claude-native.defaultModel sonnet`. Additive and optional: absent means today's behaviour. Precedence for a run's model: the run's own `model` (CLI `--model`, web picker, MCP `task_send`), then a profile's `model`, then this default, then the provider's own default. It is applied in one place, `runs.Registry.Start`, so CLI, web, MCP, queued prompts and learning runs all get it; the run row records it as the requested model, so `task runs` shows what was actually asked for. For claude-native the literal `--model default` still means "the CLI's own default" and bypasses it. Like every `smind config` change it applies when the daemon restarts. `provider.list` reports it as `defaultModel` (additive wire field), and web's model picker shows "Currently <label>" on the Default row and uses the resolved label for the picker trigger. No ADR: nothing here changes an existing contract. Validation: unknown provider ids and malformed model strings are errors at config load (same syntax rule as ADR-0025), so a typo is never a silent no-op. Not done: a per-workspace default and a Settings UI for it.
- **O2: default fork point. No.** ADR-0039 §2 stands. `task new --base origin/develop` covers the stale-checkout case, and the README's CLI section recommends it for agents.

## claude-native permission wall (found while dogfooding, 2026-10-22)

**Symptom.** A claude-native run in `acceptEdits` (or `default`) mode could not run any command that needs approval: the agent got "This command requires approval" within seconds, and `smind task permissions` showed nothing pending, so `task approve` had nothing to approve. A claude-native Peer under smind could edit files but not run `go test` or `git commit`.

**Root cause (reproduced with the raw CLI and on the dogfood daemon).** Claude Code 2.1.303 only forwards a permission prompt to its host as a `can_use_tool` control request when launched with `--permission-prompt-tool stdio`. Without it, anything that would prompt is denied by the CLI itself ("nobody answers"; `--permission-prompts` defaults to `host`, but there is no host unless the flag is given). smind never passed the flag (`claudecode.WithPermissionPolicy` alone does not add it), so the daemon never saw a request: no `permission_request` event, nothing pending, in the CLI, the web card or mobile. Probe with `claude -p --input-format stream-json ... --permission-mode acceptEdits`: without the flag the Bash tool result is the denial text; with `--permission-prompt-tool stdio` a `control_request` with `subtype: can_use_tool` arrives. The September analysis (`docs/plans/completed/claude-native-permission-cancellation.md`) saw requests reach the daemon, so older CLIs forwarded them without the flag; the CLI's behaviour changed under us. Paseo (TypeScript SDK) passes the same flag whenever it has a permission callback.

**Fix.** Pass `--permission-prompt-tool stdio` whenever a permission policy is installed (`internal/taskrunner/runner.go`), and pass a Bash request's command as permission detail (as the ACP adapter does) so an approver sees what they allow. Verified on an isolated daemon with a real Haiku run: the request becomes pending, `task approve` lets the command run, `task deny` reaches the agent as "denied by human reviewer".

**`--auto-accept` on claude-native: refused by design, left as is.** ADR-0019 decision 3: Claude Code and Codex have provider-native modes, and `autoAccept` (approve every prompt) exists only for ACP agents, which have no bypass mode of their own. For claude-native the equivalent is `--mode bypassPermissions` (or `auto`, the classifier). Making `--auto-accept` work there would mean auto-approving every `can_use_tool` request, which is the same authority as bypass with a second spelling, and it would reverse ADR-0019's split; I judged that a decision for the Human, not a bug fix, and recommend against it. What changed: the refusal now names the provider's own auto-approving modes (`--mode bypassPermissions`), instead of dead-ending.

- **AC13** A claude-native run in a prompting mode raises its Bash permission as a pending request (command visible in the detail) that `smind task approve` / `deny` resolves, and the answer reaches the agent.
- **AC14** The claude-native CLI argv carries `--permission-prompt-tool stdio` (test `TestRunner_RunPrompt_ClaudeNative_PermissionModeFlags`); a Bash request reaches a detail-aware decider with its command (`TestClaudeDeciderAdapter_BashCarriesCommandDetail`).
- **AC15** `autoAccept` on a provider without it fails with an error that names that provider's auto-approving modes (`TestValidatePermissionSettings_AutoAcceptHintsAtTheProvidersOwnModes`).

## Progress

- 2026-10-22: code verified against develop; plan written (#486).
- 2026-10-22: shipped: #489 setup script, #492 slug cap, #494 `task new` start options, #495 `--detach`/`--thinking`/MODEL, #496 `defaultModel`, #500 quiet output, #502 claude-native permission prompts (`--permission-prompt-tool stdio`). Peers ran as smind tasks (GLM until its 5-hour 429, then claude-native Sonnet 5.5); the claude-native Peers could not run commands before #502, so the Lead ran the checks and finished what they had not (see the dogfood log).
- 2026-10-22: re-verified on the rebuilt dogfood daemon (develop e63f2a23, "dev" build) with the matching CLI. Closed.

## Validation

"Unit" = a named test in the PR; "live" = run by hand on the dogfood daemon (:4870, develop e63f2a23) on 2026-10-22; "isolated" = a daemon built from the PR branch with its own home and port. Each PR also ran `go vet`, gofmt and the full `go test` for the touched packages locally; the full `task test`/`task lint` ran in CI.

| AC | Result | Evidence |
|---|---|---|
| AC1 `--detach` | confirmed | unit `TestTaskSendDetachPrintsRunIDAndSkipsAttach` (the command returns while the run is still `running`); live: returned in 0 s with only the run id on stdout, `run <id> started` on stderr |
| AC2 `--thinking` | confirmed | unit `TestTaskSendThinkingFlag` (invalid value exits 2 before dialing; the run row carries the level); live: `--thinking off` accepted |
| AC3 unpinned note | unit only | `TestUnpinnedModelNote`, `TestUnpinnedModelNote_ConfiguredDefault`; not run live because an unpinned claude-native send would start the CLI's default model (possibly Opus) |
| AC4 MODEL column | confirmed | unit `TestRunModelCell`, `TestTaskRunsShowsModel`; live: `task runs` shows `haiku` |
| AC5 raw hidden | confirmed | unit `TestRunViewer_RawHiddenByDefault/Verbose`, `TestPrintRunLogs_...`; live: `task logs` on a stored GLM run hides the several-KB `available_commands_update` and `--verbose` shows it |
| AC6 one-line auto-accept | confirmed by test, not live | unit `TestRunViewer_*`, `TestPrintRunLogs_AutoAcceptCollapsesAndHidesRaw`, and `TestTaskSend_AutoAcceptPrintsOnePermissionLine` (real daemon + the fake ACP agent: `send`, `logs`, `attach` each print exactly one permission line); the live GLM check could not run because GLM was inside its 5-hour 429 window |
| AC7 `task new --base/--branch/--pr` | confirmed (`--base`, `--branch` by test) | unit `TestTaskNewBase`, `...ExistingBranch`, `...StartFlagOrder`, `...StartFlagRejection`; live: `--base origin/develop` gave a worktree whose HEAD equals `origin/develop`. `--pr` was not run (needs a GitHub remote) |
| AC8 slug cap | confirmed | unit `TestSlugifyCapsLength`, `TestCreateTaskLongTitleBranchLength`; live: a 100-character title produced `smind/task-base-probe-a-deliberately-very-long-task-<stamp>` (84-character worktree path) |
| AC9 setup script | confirmed locally | `scripts/task_setup_test.sh` (stub bun/npm: both installs, idempotent, missing lockfile, missing tool) and shellcheck; not installed on a real workspace |
| AC10 config key | confirmed | unit `TestProviderDefaultModel*` (load, set/get/unset keeping comments, rejects, absent = today), `TestDefaultModelProvidersMatchTaskrunner` |
| AC11 default applied at run start | confirmed | unit `TestStart_ConfiguredDefaultModel` (default applied and recorded, explicit wins, none stays empty), `TestRunnerDefaultModel`. Not run with a live Claude login |
| AC12 `provider.list` + picker | confirmed | unit `configured default model` suite; isolated: Playwright light and dark against a daemon with `providers.claude-native.defaultModel: sonnet` showed the trigger "Sonnet (latest)" and the Default row "Currently Sonnet (latest)" |
| AC13 claude-native approve/deny | confirmed | isolated and live (:4870): the Bash request is pending with `command: gofmt -l .`; `task approve` runs it, `task deny` reaches the agent as "denied by human reviewer" |
| AC14 argv and detail | confirmed | unit `TestRunner_RunPrompt_ClaudeNative_PermissionModeFlags`, `TestClaudeDeciderAdapter_BashCarriesCommandDetail` |
| AC15 auto-accept refusal names the modes | confirmed | unit `TestValidatePermissionSettings_AutoAcceptHintsAtTheProvidersOwnModes` |

Also confirmed live: `--mode auto` runs a claude-native Bash command unattended with nothing pending (README documents it, including that the classifier is not a sandbox: it let `git push --force origin main` through in a throwaway repo with no remote).

**Not verified, carried forward:** the one-line auto-accept against a real GLM run (re-check once GLM's quota resets); `task new --pr` against GitHub; a real claude-native run with a configured `defaultModel`; the web and mobile permission cards against a claude-native run; older Claude CLI versions with `--permission-prompt-tool stdio`. Follow-up work (CLI batch 2: `task wait`, run error surfacing, richer `task runs`, chat/provider binding and setup-busy messages) is planned in `docs/plans/active/dogfood-cli-batch-2.md`.
