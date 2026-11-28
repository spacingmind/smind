# P0: daemon-initiated git runs stripped (hooks, filters, merge drivers, credential helpers)

Status: active. Size L. High-risk (daemon, a security boundary, every git spawn): validation contract, a validator on the exact head, two fresh reviewers, the Lead's own mutations on committed code. Follows `p0-git-diff-hardening.md` (A2, merged as #996), which closed the diff/textconv/fsmonitor classes and recorded the others as out of scope (its D4) with a follow-up: this is that follow-up. The Human decided (2026-11-28): git the daemon starts on its own runs with a stripped configuration; git the Human starts keeps the repository's hooks as today.

## Threat (what A2 left open, D4)

An agent writes to its task's worktree and, through it, to shared git state: `.git/config`, `.git/hooks/*`, `.git/info/attributes`, in-tree `.gitattributes`. git runs a program that this state names, as the daemon:

| Class | How an agent arms it | What runs it in daemon code |
|---|---|---|
| hooks | `.git/hooks/*` or `core.hooksPath` | the checkpoint `git commit`, `worktree add` (post-checkout) |
| filter (clean/smudge/process) | `.gitattributes`/`info/attributes` `filter=x` + `filter.x.clean` | `add -A` (checkpoint, diff snapshot), `status` of a stat-dirty file, `worktree add`, checkout, merge |
| merge driver | `merge.x.driver` + attribute | `merge`, `rebase`, `cherry-pick` |
| credential helper / askpass | `credential.helper`, `core.askPass` (a repo-local value) | `fetch`, `push` |
| ssh command | `core.sshCommand` | `fetch`, `push` over ssh |
| signing program | `commit.gpgsign=true` + `gpg.program` | `commit` |
| upload/receive pack program | `remote.<n>.uploadpack`, `remote.<n>.receivepack` | `fetch`, `push` (a local-path remote runs it locally) |

## What git allows and what it does not (measured, git 2.54 Apple, a lab repo under `~/.smind/dogfood/git-stripped/`, every program a marker-writing script)

| Class | Mechanism | Measured result |
|---|---|---|
| hooks | `-c core.hooksPath=/dev/null` | the pre-commit marker never appears (git looks for `/dev/null/pre-commit`, which cannot exist; an agent cannot plant a file in it, unlike a predictable temp directory) |
| filter | `-c filter.<name>.clean=` / `.smudge=` / `.process=` (an empty value) | no marker for `add`, `status`, `merge`; a configured process filter does not start. **There is no wildcard**: the names have to be listed first. |
| filter, by attributes | `--attr-source=<empty tree>` | removes in-tree `.gitattributes` but **not** `.git/info/attributes` (the marker still ran), and it also drops legitimate attributes (`text eol=crlf`, `binary`, `merge=union`), which makes the daemon's own diff and status see files as changed. **Rejected.** |
| merge driver | `-c merge.<name>.driver=` | the driver does not run; the merge reports a conflict instead of auto-resolving (conservative) |
| credential helper | `-c credential.helper=` (resets the list) | no helper runs (`git credential fill`) |
| ssh command | `-c core.sshCommand=ssh` | the repo's program does not run |
| signing | `-c commit.gpgsign=false -c tag.gpgsign=false` | `gpg.program` never runs |
| upload/receive pack | `-c remote.<n>.uploadpack=` with an empty value, the default name, an absolute path, or `GIT_CONFIG_COUNT`: **none overrides it** (the repository program still ran in all four); the command-line flag does: `git fetch --upload-pack=git-upload-pack` and `git push --receive-pack=git-receive-pack` run no repository program | **the flag, not config**: stripped mode inserts it right after `fetch` / `ls-remote` / `push`. `remote update` and `remote add -f` cannot take the flag and stay uncovered (the daemon calls neither). |
| config scope | `git config --show-scope --show-origin --null --get-regexp` | each value comes with its scope: `local` and `worktree` are the repository's (agent-editable); `system`, `global` and `command` are the person's. Apple's Xcode system file reports scope `unknown` with an absolute path outside the repository. |

**What stays open, stated plainly:**

1. **A race on config.** The names are read right before the call. A filter, merge driver or helper that an agent writes into `.git/config` *between* the scan and the git process starting is not neutralised. The window is milliseconds; the daemon cannot make the two steps atomic (git offers no "ignore repository config" switch). It is far narrower than today, and a static or committed hostile configuration is always caught.
2. **`core.askPass`, `core.gitProxy` and `remote.<n>.vcs`** are neutralised by the same scan where git honours an empty or default value, but no real-git test exercises them (they need a server). They are listed as applied, not proven.
3. **Alternates, `url.*.insteadOf` redirects, `core.alternateRefsCommand` and the like** are not covered.
4. A Human-initiated operation runs everything the repository names (that is the decision). An agent with a shell can call the same RPC on an unidentified local connection (ADR-0056, not built): a Human-mode commit then runs the repository's hook. Stated here because it bounds what "human-initiated" can mean.

## Decisions

- **D1 Two modes in `internal/gitsafe`.** The existing `Args` / `Env` stay as the *hardened baseline* (A2) and become the **human mode**: everything the repository names still runs, minus the three A2 classes. A new **stripped mode** is the **default for daemon-initiated git**: `StrippedArgs(ctx, dir, args...) ([]string, error)` and `StrippedEnv(base)`.
- **D2 What stripped mode adds** (on top of the baseline `-c core.fsmonitor=false`, `--no-ext-diff --no-textconv`, the baseline environment):
  - always: `-c core.hooksPath=/dev/null`, `-c commit.gpgsign=false`, `-c tag.gpgsign=false`;
  - from one `git config --show-scope --show-origin --null --get-regexp` read (5 s limit, the baseline hardening and environment, no program runs): for every `filter.<n>.{clean,smudge,process}`, `merge.<n>.driver`, `core.sshCommand`, `core.askPass`, `credential.helper` (and `credential.<url>.helper`) whose value is defined in a `local` or `worktree` scope: an override `-c key=<value>`. The value is the last **trusted** (`system`, `global`, `command`, or `unknown` with an absolute origin outside the repository) value of the same key if there is one (so a local entry cannot hide the person's own `git lfs install`), else the neutral value (empty for filters, merge drivers, askpass; `ssh` for the ssh command; the default program name for `gpg.*.program`; a reset for `credential.helper` followed by the trusted helpers, in order);
  - pack programs by **flag**: `--upload-pack=git-upload-pack` after `fetch` and `ls-remote`, `--receive-pack=git-receive-pack` after `push`, unless the call already names one (config overrides do not work for these, see the table);
  - a configured value that is **exactly** one of git-lfs's three command lines (`git-lfs clean -- %f`, `git-lfs smudge -- %f`, `git-lfs filter-process`) is trusted even in a local scope (`git lfs install --local`);
  - **fail closed**: if the read fails or times out, no git is run (an error), never a call without the overrides. Exit status 1 with no output is "no match", not a failure.
  - the scan is skipped for subcommands that only read refs or the config and can run none of the classes (`rev-parse`, `rev-list`, `for-each-ref`, `symbolic-ref`, `merge-base`, `config`, `remote`, `branch`, `tag`, `show-ref`, `log` without `-p`/`-g`... the table in code is an allow-list; every other subcommand is scanned, so an unknown one fails safe). The static overrides are always added.
- **D3 Which mode a call gets: by the trigger.** *Human-initiated* means a direct Human click or a CLI command the Human runs, reaching the daemon on a non-run-scoped connection: **Commit, Stage/Unstage, Push (incl. update push), Sync (update from base: fetch + merge/rebase), Create PR (the clean-branch push)**. Everything else is *daemon-initiated* (default, stripped): checkpoints, diff and status reads, task creation and archive cleanup (an agent or an automation can start them through MCP), probe and learning reads, the steward and validator reads, fleet reads. When a function has both kinds of callers (`CommitTask`: the Commit button and the runner's agent commit), it takes the initiator from the **context**; the default context is daemon.
- **D4 Plumbing.** `workspace.WithHumanInitiated(ctx)` marks a context; `execGit` (every `runGit*` helper) picks the mode from its context. The wsapi handlers of the six human RPCs mark the context only when `!rc.runScoped`. New Manager methods `CommitTaskAs`, `TaskStageAs` (the old names stay and mean daemon-initiated); `Push`, `UpdateFromBase`, `PushForPR`, `PushUpdate` already carry a context.
- **D5 The audit.** `internal/gitsafe/audit_stripped_test.go` extends the A2 audit: every non-test file that spawns git directly must have a row in the classification table below with a class, and a daemon-initiated or mixed file must call `gitsafe.StrippedArgs(`; a file marked human-initiated only may call `gitsafe.Args(`; every call of a human-mode variant (`WithHumanInitiated`, `execGitHuman`) must sit in a function the table names. A new spawn with no row, or a human variant in an unnamed function, fails the test. The audit logic is a function over a directory and the plan text so a test can feed it a synthetic new spawn.
- **D6 Out of scope (with the reason).** Fresh directories the daemon made itself (`plugins/source.go`, `learning/probe/arm/materialise.go`: `init`, `archive`, `cat-file` of the repository's objects), `diff --no-index` (`learning/probe/arm/diff.go`), developer tools, the user's own CLI command `cmd/smind/run_columns.go` (refs only). They run no repository-named program or are not the daemon.

## Classification of every git call site

Class: **daemon** (stripped), **human** (baseline, the repository's hooks and drivers keep working), **mixed** (the context decides; the human functions are named), **out** (D6, with the reason). Every file that spawns git directly is named; the audit test reads this table.

| Call site (file: functions) | Commands | Class | Why |
|---|---|---|---|
| `workspace/git.go`: `execGit` and the `runGit*` helpers | every workspace git call | **mixed** | stripped by default; `execGit` takes the mode from the context |
| `workspace/git.go`: `gitWorktreeAdd`, `gitWorktreeAddFrom`, `gitWorktreeRemove` | `worktree add/remove` | daemon | task creation and cleanup are reachable from MCP and automations |
| `workspace/git.go`: `gitWorktreeCheckpoint` | `status`, `add -A`, `commit` | daemon | the automatic checkpoint; the main exposure |
| `workspace/git.go`: `snapshotIndex`, `taskDiff`, `taskChangedFiles`, `taskFileDiff`, `taskDiffStat`, `stagedPaths`, `TrackedEnvFiles` | `add -A` into a throwaway index, `diff`, `status`, `ls-files` | daemon | reads for the UI, triggered by viewing |
| `workspace/git.go`: `taskStageFile` | `add -- path`, `restore --staged` | **mixed** (human: the Stage button) | a person's own staging keeps the clean filter (git-lfs pointers) |
| `workspace/git.go`: `gitTaskCommit` | `diff --cached --name-only`, `commit`, `rev-parse` | **mixed** (human: the Commit button; daemon: the runner's agent commit, `taskrunner/runner.go`) | the person's commit keeps the repository's pre-commit hook |
| `workspace/commit.go`: `CommitTask`, `CommitTaskAs`, `TaskStage`, `TaskStageAs` | the two above | mixed | the `...As` variants take the initiator from the context |
| `wsapi/handlers.go`: `handleTaskStage`, `handleTaskCommit`; `wsapi/sync_handlers.go`: `prHost.push`, `prHost.updateFromBase`; `wsapi/pr_handlers.go`: `prHost.create`, `prHost.update` | mark the context human-initiated (`humanContext`) | **human** | the six human RPCs; `task.stage` and `task.commit` are open to a run-scoped agent caller, which stays daemon-initiated (stripped); the other four already refuse a run-scoped caller |
| `workspace/sync.go`: `Push`, `pushRemote` | `push`, `config`, `remote` | **human** (push) / daemon (config and remote reads, run no program) | the Push button; a person's credentials and pre-push hook |
| `workspace/sync.go`: `UpdateFromBase` | `fetch`, `merge`, `rebase` | **human** | the Sync button: merge drivers and post-merge hooks keep running |
| `workspace/sync.go`: `forkPRRecorded`, `remoteURLOf`, `syncCount`, `unmergedFiles`, `worktreeOperation` | `config --get`, `rev-list`, `diff --name-only`, `rev-parse` | daemon | reads |
| `workspace/pr.go`: `PushForPR`, `pushCleanBranch`, `execGitWorktreeAdd`, `PushUpdate` | `branch -D`, `worktree add -b`, `show -s`, `cherry-pick`, `push` | **human** | the Create PR / update click; the clean-branch cherry-pick runs the person's merge driver |
| `workspace/pr.go`: `PRPreview`, `prGit`, `prRevParse`, `prIsAncestor`, `prCommits`, `prSubjects` | `diff --shortstat`, `rev-parse`, `merge-base`, `rev-list`, `log` | daemon | reads for the PR dialog |
| `workspace/archive_branch.go`: all (`deleteBranch`, `fetchBaseBounded`, `uncommittedStat`, `unpushedCommits`, `branchCheckedOutElsewhere`, `worktreeRegistered`, `addRestoredWorktree`) | `branch -D`, `fetch`, `diff --numstat`, `rev-list`, `worktree list/add` | daemon | archive and restore run on the daemon's schedule too; a credential-less best-effort fetch is accepted |
| `workspace/base_backfill.go`, `branches.go`, `default_base.go`, `provenance.go`, `search.go`: all | `log -g`, `for-each-ref`, `symbolic-ref`, `rev-list`, `merge-base`, `rev-parse`, `status`, `ls-files` | daemon | reads |
| `workspace/task.go`: `CreateTask`, `discardTask` | `config`, `worktree remove`, `branch -D` | daemon | reachable from MCP and automations |
| `workspace/task_ref.go`: `validateRefName`, `gitRefCommitExists`, `remoteConfigured`, `fetchRemoteBranchBestEffort`, `createTaskFromBranch` | `check-ref-format`, `rev-parse`, `remote get-url`, `fetch`, `worktree add` | daemon | task creation from a branch or PR is reachable from MCP; the fetch is best effort |
| `steward/git.go`: `gitCmd`; `steward/standards_git.go` | `diff`, `log`, `rev-list`, `merge-base`, `ls-tree`, `cat-file` | daemon | steward reads |
| `validation/digest.go`: `gitCmd`, direct exec | `status`, `rev-parse`, `merge-base` | daemon | the validator snapshot |
| `gitstate/runner.go`: `DefaultRunner` | `status`, refs, `config` | daemon | the status the UI polls |
| `learning/probe/sandbox/tripwire.go`: `git` | `status` | daemon | the probe tripwire reads an agent-edited sandbox |
| `taskrunner/gitrules_detail.go`: `stagedFiles` | `diff --cached --name-only` | daemon | the git-rules check |
| `wsapi/steward_collect.go`, `wsapi/steward_collect_twoaxis.go` | `rev-parse`, `ls-tree`, `grep -q` | daemon | steward collectors |
| `wsapi/fleet_read.go`, `wsapi/fleet_write.go`, `wsapi/forge_auth.go` | `remote get-url`, `fetch` | daemon | fleet and forge reads; the fleet fetch is daemon-started |
| `learning/probe/arm/diff.go` | `diff --no-index` | out | no repository config (`--no-index`), already flagged (D6) |
| `learning/probe/arm/materialise.go` | `cat-file`, `init`, `archive`, `rev-parse` | out | a fresh directory the daemon made; `archive` runs no driver (D6) |
| `plugins/source.go` | `fetch`, `checkout --detach`, `rev-parse` | out | a fresh plugin directory, no agent-edited config (D6) |
| `cmd/smind/run_columns.go` | `rev-list --count` | out | a CLI command the person runs; refs only (D6) |
| `qaframes/cmd/qaframes/main.go`, `relay/bridge/devharness/main.go` | developer tools | out | not the daemon |

## Acceptance criteria

- AC1 Stripped mode: a hook, a filter (clean, smudge, process), a merge driver, a credential helper, an ssh command, a signing program and an upload-pack program that write a marker do **not** run for any daemon-initiated operation (checkpoint, diff snapshot, status, worktree add, merge) in a poisoned repository.
- AC2 Human mode: the same programs **do** run for the operations the Human starts (commit hook, stage filter, push hook, sync merge driver).
- AC3 Only repository-controlled config is neutralised: a filter and a credential helper defined in the person's global config keep working in stripped mode, and a local entry with the same name cannot replace them; git-lfs's exact command lines are trusted.
- AC4 The scan fails closed (an error, no git), treats exit status 1 as no match, and is skipped for ref-only subcommands (an unknown subcommand is scanned).
- AC5 A repository with `text eol=crlf` attributes shows no spurious change in stripped mode (the reason `--attr-source` was rejected).
- AC6 The classification table covers every spawn; the audit fails for a new spawn without a row and for a human variant in an unnamed function.
- AC7 The human RPCs mark the context only for a non-run-scoped caller; the runner's agent commit is daemon-initiated.
- AC8 No UI, wire or mobile change; hygiene green.

## Test scenarios (named; Go function `Test_<name>`)

`internal/gitsafe`: `stripped_args_static_prefix`, `stripped_scan_parse_scope_and_origin`, `stripped_neutralises_local_filter_merge_driver_ssh_askpass_pack`, `stripped_keeps_global_filter_and_credential_helper`, `stripped_local_override_of_a_global_filter_is_reasserted`, `stripped_trusts_exact_lfs_command_lines`, `stripped_credential_helper_reset_keeps_trusted_helpers`, `stripped_scan_failure_fails_closed`, `stripped_scan_exit_one_is_no_match`, `stripped_skips_the_scan_for_ref_only_subcommands`, `stripped_scans_an_unknown_subcommand`; real git (`gitsafetest`, fixture arms every class): `real_git_daemon_mode_runs_no_armed_program`, `real_git_human_mode_runs_the_armed_programs`, `real_git_daemon_mode_keeps_a_global_filter`, `real_git_eol_attributes_do_not_show_spurious_changes`; audit: `audit_every_git_spawn_is_classified`, `audit_daemon_files_use_stripped_args`, `audit_human_variants_only_in_named_functions`, `audit_flags_a_new_unclassified_spawn`.

`internal/workspace`: `checkpoint_runs_no_hook_or_filter_in_a_poisoned_repo`, `diff_snapshot_runs_no_filter`, `worktree_add_runs_no_post_checkout_hook`, `human_commit_runs_the_pre_commit_hook`, `runner_agent_commit_runs_no_hook`, `human_stage_runs_the_clean_filter`, `human_push_runs_the_pre_push_hook`, `human_update_from_base_runs_the_merge_driver`.

`internal/wsapi`: `task_commit_marks_human_only_for_a_non_run_scoped_caller`.

## Mutations (each by the Lead on committed code; the validator and the reviewers repeat them)

- M1 drop `core.hooksPath` from the static prefix -> `real_git_daemon_mode_runs_no_armed_program`.
- M2 trust `local` scope (neutralise nothing) -> the same test and `stripped_neutralises_local_filter_merge_driver_ssh_askpass_pack`.
- M3 the human context is ignored (always stripped) -> `human_commit_runs_the_pre_commit_hook`.
- M4 `execGit` back to the baseline `Args` -> `checkpoint_runs_no_hook_or_filter_in_a_poisoned_repo`.
- M5 a scan error is swallowed (fail open) -> `stripped_scan_failure_fails_closed`.
- M6 a classification row removed from this table -> `audit_every_git_spawn_is_classified`.

## Staffing

GLM peers write the code from step briefs (one commit per step, at most two at a time): P1 the `gitsafe` core and tests; then P2 the workspace and wsapi plumbing and P3 the remaining choke points in parallel; the audit test and this plan by the Lead. Two fresh reviewers on the exact head, then the validator.

## Progress

- Spec and the mechanism measured first; the validation contract committed before code.
- Code by GLM peers from step briefs, one commit per step, every diff reviewed and cherry-picked by the Lead: P1 the `gitsafe` core and the armed-repo fixture and real-git tests, P2 the workspace and wsapi plumbing, P3 the other choke points and the audit. GLM stopped at its output-token cap twice on the first, large P1 prompt (nothing saved; not a plan limit), so the work was re-sent in small single-commit steps, still on GLM.
- A real-git test caught a wrong assumption of the spec: `-c remote.<n>.uploadpack=...` does not override the repository's program in any variant; the command-line flag does. The mechanism, the table above and D2 were corrected (the flag is inserted after `fetch`, `ls-remote`, `push`).
- Lead review corrected two allow-list entries (`remote` and `cat-file` can run programs) and added `gpg.program`.
- One existing test changed by decision: `cmd/smind` `TestMCPTools_TaskArchiveCheckpointFailureKeepsWorktree` made the archive checkpoint fail with a failing pre-commit hook; checkpoints now run with hooks off, so it fails the checkpoint with an `index.lock` in the worktree's git dir instead (the intent, a failed checkpoint keeps the worktree and the task, is unchanged). Edited by the Lead (one test, ten lines).
- Lead mutations on committed code, each fails its named test: M1 (no `core.hooksPath`), M2 (local scope trusted), M3 (the human context ignored), M4 (`execGit` always baseline), M5 (a scan error swallowed), M6 (a classification row removed).

## Validation

- Pending.

## Decisions for the Human

1. **LFS and the user's global setup keep working** (a global `git lfs install` filter, a global credential helper), because only repository-controlled (local and worktree scope) config is neutralised; a repository-local `git lfs install --local` is recognised by the exact command lines. Recommended as built. If you want every filter off regardless of scope, say so: it breaks git-lfs checkouts of new task worktrees.
2. **Daemon-initiated fetches lose a repository-local credential helper** (a global one is kept), so a best-effort fetch for a private https remote that relies on a repo-local helper stops authenticating. Pushes the person clicks are unaffected. Recommended: accept.
3. **"Human-initiated" cannot be proven on the socket** (ADR-0056): an agent that opens the daemon socket without identifying gets human mode and the repository's hook on a commit it asks for. Recommended: accept for now, decide ADR-0056 separately.
4. **Checkpoints stop signing** (`commit.gpgsign=false` for daemon commits). A person who signs every commit globally gets unsigned checkpoint commits (they were a pinentry hang risk). Recommended: accept.
