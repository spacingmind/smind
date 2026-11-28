# P0 A2: the daemon runs git where an agent can edit the repo config

Status: active. **HIGH-RISK** (the daemon executes a program an agent can name). Size M. Go daemon only: no wire, store, permission-policy or UI change. ADR-0045 contract: `p0-git-diff-hardening.contract.md`, a validator on the exact head and two fresh reviewers.

## Threat

An agent writes to its task's worktree, and through it to the repository's shared git state: `.git/config` of the main repo, `.git/info/attributes`, and in-tree `.gitattributes`. git runs a program named in config for several operations. The daemon runs git in those repositories as itself, so a program an agent names can run with the daemon's rights, not the agent's sandbox. The classes, and which the daemon's calls can reach:

| Class | How an agent arms it | What runs it |
|---|---|---|
| **textconv** | `.gitattributes` `*.x diff=evil` + `diff.evil.textconv = <cmd>` | `git diff`, `git show`, `git log -p`, `git blame` |
| **external diff** | `diff.external = <cmd>` (or env `GIT_EXTERNAL_DIFF`) | `git diff`, `git show`, `git log -p` |
| **fsmonitor** | `core.fsmonitor = <cmd>` | `git status`, `git diff` (worktree), `git add`, `git ls-files` |
| **filter (clean/smudge/process)** | `.gitattributes` `*.x filter=evil` + `filter.evil.clean = <cmd>` | `git add`, `git status`/`git diff` of a stat-dirty file, `git commit -a`, checkout / `worktree add` (smudge) |
| **hooks** | `.git/hooks/*` or `core.hooksPath` | `git commit`, `worktree add` (post-checkout), `push` (pre-push) |
| **merge driver** | `merge.evil.driver = <cmd>` + attribute | `cherry-pick`, `merge`, `rebase` |
| **credential helper / askpass / ssh command** | `credential.helper`, `core.askpass`, `core.sshCommand` | `fetch`, `push` |
| **pager / editor** | `core.pager`, `core.editor`, `sequence.editor` | only on a terminal / interactive; the daemon's stdout is a pipe and it passes full messages |

Already in place before this PR: `core.fsmonitor=false` in `workspace.execGit` and `gitstate.DefaultRunner` (and in `taskrunner/gitrules_detail.go`, `wsapi/fleet_*`, `wsapi/forge_auth.go`); `--no-ext-diff --no-textconv` in `steward/git.go` and `learning/probe/arm/diff.go`. **Not** in place: the five `git diff` calls of `workspace/git.go`, `workspace/archive_branch.go:381`, `workspace/pr.go:171`, `workspace/sync.go:348`; no `fsmonitor` override on `validation/digest.go` `status`, `learning/probe/sandbox/tripwire.go` `status` (a status of an agent-edited probe sandbox), nor on the direct `exec.Command("git")` calls of `workspace/task_ref.go`; `GIT_EXTERNAL_DIFF` and `GIT_PAGER` are inherited from the daemon's own environment everywhere.

## Measured facts (git 2.x, this machine, a repo with `diff.external`, a `diff=evil` textconv driver and `core.fsmonitor` armed with marker scripts)

- `git diff` **patch output** runs `diff.external` (marker); with external diff off it runs the textconv driver (marker). `--no-ext-diff` stops the first, `--no-textconv` the second.
- `git diff --numstat`, `--shortstat`, `--stat`, `--name-only`, `--name-status` and `git show -s` run **neither**: those call sites are inert today. They still carry the flags (a future `-p` or format change would otherwise silently become exploitable) and have a regression-pin test, but **no test of an inert site can fail when its flag is removed**. The real exposure in the daemon is the two patch calls: `workspace/git.go` taskDiff and taskFileDiff (the diff a user reads in the UI).
- git suppresses the diff entirely when its stdout is `/dev/null`, so a naive probe of "does the driver run" under `> /dev/null` is meaningless; the tests capture the output.
- `core.fsmonitor` runs for `status` and worktree diffs.

## Decisions


- **D1 One shared hardening, `internal/gitsafe`** (stdlib only, no daemon import): `Args(args ...string) []string` returns the call's arguments with the overrides every daemon-side call carries (`-c core.fsmonitor=false` first) and, for the diff family (`diff`, `show`, `log`, `whatchanged`), `--no-ext-diff --no-textconv` right after the subcommand when the call did not already carry them; `Env(base []string) []string` returns `base` (or `os.Environ()` when nil) without `GIT_EXTERNAL_DIFF` and with `GIT_PAGER=cat`, `GIT_TERMINAL_PROMPT=0`, `GIT_OPTIONAL_LOCKS=0`. It finds the subcommand by skipping git's own global options (`-c <k=v>`, `-C <dir>`, `--no-optional-locks`, `--git-dir=...`, any leading `-` option), so it is correct for the `git --no-optional-locks -C dir <verb>` shape steward and validation use.
- **D2 Choke points use it**, so every present and future call through them is covered: `workspace.execGit` (every `runGit*` helper goes through it), `steward.gitCmd`, `validation.gitCmd` and its direct exec, `gitstate.DefaultRunner`, `sandbox.git` (tripwire), the direct calls in `workspace/task_ref.go`.
- **D3 Per-call flags too, two independent layers** (belt and braces, and so the call sites read correctly): every `git diff` call of the daemon spells `--no-ext-diff --no-textconv` itself, and `gitsafe.Args` adds them if a call forgot. A mutation of ONE layer therefore passes the driver tests on purpose (the other layer still protects); the tests are killed by removing BOTH layers (see the mutations).
- **D4 Out of scope, with the reason (listed in the audit below):** filter drivers, hooks, merge drivers, credential helpers. They are features a user relies on (git-lfs installs filters and a pre-push hook; husky installs hooks; credential helpers are how a push authenticates), and no single flag neutralises an unknown-named filter. Fixing them needs a design decision (a hardened clone, or an allowlist of configured drivers), not a flag: **reported as a follow-up with a recommendation (run the daemon's own non-user-initiated git in a stripped configuration, and decide whether a user-initiated commit should run the repo's hooks)**. The checkpoint commit and the `add -A` of the diff snapshot are the reachable cases; they are named.

## Audit: every daemon-side git call site (non-test), with a verdict

Verdict: **safe** (cannot run a configured program, or only a read of refs), **fixed** (changed in this PR), **out of scope** (a class from D4, with the reason).

| Call site | Command | Verdict |
|---|---|---|
| `workspace/git.go` taskDiff (:268) | `git diff --no-color --cached <base>` | **fixed** (textconv/ext-diff) |
| `workspace/git.go` name-status (:290) | `git diff --name-status --cached <base>` | **fixed** |
| `workspace/git.go` file diff (:337) | `git diff --no-color --cached <base> -- <path>` | **fixed** |
| `workspace/git.go` staged names (:390) | `git diff --cached --name-only` | **fixed** (flags spelled; name-only runs neither) |
| `workspace/git.go` shortstat (:442) | `git diff --shortstat --cached <base>` | **fixed** |
| `workspace/archive_branch.go:381` | `git diff --cached --numstat HEAD` | **fixed** |
| `workspace/pr.go:171` | `git diff --shortstat <forkPoint> <branch>` | **fixed** |
| `workspace/sync.go:348` | `git diff --name-only -z --diff-filter=U` | **fixed** (flags spelled) |
| `workspace/pr.go:299, :420` | `git show -s --format=...` | **fixed** by the helper (show carries `--no-ext-diff --no-textconv`; `-s` prints no diff) |
| `workspace/base_backfill.go:57` | `git log -g --format=%H` | **fixed** by the helper (no patch, flags are inert) |
| `workspace/git.go:76, :364` and `provenance.go:26`, `validation/digest.go:37`, `sandbox/tripwire.go:146` | `git status --porcelain` | fsmonitor already off in `execGit` / `gitstate`; **fixed** where it was missing (`validation/digest.go`, `sandbox/tripwire.go`); a configured **filter** can still run on a stat-dirty file: **out of scope (D4: filter)** |
| `workspace/git.go:83` `add -A` (checkpoint), `:222` `add -A` into a throwaway index, `:351` `add -- <path>` | `git add` | fsmonitor off; clean **filter** reachable: **out of scope (D4: filter)** |
| `workspace/git.go:86` checkpoint commit, `:404` user commit | `git commit` | **hooks** reachable: **out of scope (D4: hooks)**; the checkpoint is automatic, the user commit is a user action |
| `workspace/git.go:45, :54`, `task.go`, `task_ref.go:167,171`, `archive_branch.go:605,607` | `git worktree add` (post-checkout hook, smudge) | **out of scope (D4)** |
| `workspace/pr.go:304` | `git cherry-pick` (merge driver) | **out of scope (D4: merge driver)**; it runs in a throwaway worktree the daemon made |
| `workspace/sync.go:328` | `git merge` / `git rebase` (the Sync action: merge driver, post-merge / post-rebase hooks) | **out of scope (D4: merge driver, hooks)**; user-initiated; the fixed classes are off (it goes through `execGit`) |
| `workspace/pr.go:294`, `:318-321` (`execGitWorktreeAdd`) | `git worktree add -b` for the PR preview (post-checkout hook, smudge) | **out of scope (D4)**; a throwaway worktree the daemon made; through `execGit` |
| `workspace/pr.go:206, :263`, `sync.go:290`, `archive_branch.go:344`, `task_ref.go:129,138`, `wsapi/fleet_write.go:110` | `git fetch` / `git push` (credential helper, ssh command, pre-push hook) | **out of scope (D4: credentials)**: that is how the user's own login works |
| `workspace/git.go:193`, `archive_branch.go:405,417,441`, `branches.go`, `default_base.go`, `sync.go:96,159,191,204,367,396,405`, `pr.go:377,391,404`, `task.go:182,191,230,296`, `git.go:353` `restore --staged` | `rev-parse`, `rev-list`, `for-each-ref`, `symbolic-ref`, `merge-base`, `config --get`, `remote`, `worktree list/remove`, `branch -D`, `ls-files` | **safe** (refs and config reads; `ls-files` honours fsmonitor, off in `execGit`) |
| `steward/git.go` (`diff --numstat`, `diff -U0`, `log`, `rev-list`, `merge-base`) and `steward/standards_git.go` (`diff`, `ls-tree`, `cat-file`) | read-only | **safe / fixed**: the two diffs already carry the flags; `gitCmd` now carries the shared hardening |
| `gitstate/runner.go` (`DefaultRunner`) | status / refs / config for the UI | fsmonitor off; **fixed**: environment scrubbed by `gitsafe.Env`; filters: **out of scope (D4)** |
| `taskrunner/gitrules_detail.go:65` | `git diff --cached --name-only` | **fixed** (flags spelled, review round 1) |
| `validation/digest.go:33,155` `rev-parse`; `:121` `merge-base --is-ancestor` | refs | **safe** |
| `wsapi/steward_collect.go:157`, `wsapi/steward_collect_twoaxis.go:81,100` | `rev-parse`, `ls-tree`, `grep -q` (reads a commit's blobs, no textconv) | **safe** |
| `wsapi/fleet_read.go:99`, `wsapi/forge_auth.go:252` | `remote get-url` | **safe** |
| `learning/probe/arm/diff.go:68` | `git diff --no-index --no-textconv --no-ext-diff` | **safe** (already flagged; pinned by a test here) |
| `learning/probe/arm/materialise.go` | `cat-file`, `init`, `rev-parse`, `archive` of the real repo | **safe** (`archive` runs no driver; `export-subst` is a format expansion) |
| `plugins/source.go:208-214` | `fetch`, `checkout --detach`, `rev-parse` in a fresh plugin work dir | **safe** (a fresh directory: no repository config of an agent) |
| `cmd/smind/run_columns.go:101` | `git rev-list --count` | **safe** (CLI the user runs; refs only) |
| `qaframes/cmd/qaframes/main.go`, `relay/bridge/devharness/main.go` | developer tools, not the daemon | **out of scope** |

## Acceptance criteria

- AC1 `gitsafe.Args` puts `-c core.fsmonitor=false` first, inserts `--no-ext-diff --no-textconv` after `diff`, `show`, `log`, `whatchanged` (also when git's global options come first), never duplicates them, and leaves every other subcommand and every argument after `--` alone.
- AC2 `gitsafe.Env` drops `GIT_EXTERNAL_DIFF`, sets `GIT_PAGER=cat`, `GIT_TERMINAL_PROMPT=0`, `GIT_OPTIONAL_LOCKS=0`, keeps the rest in order, and never mutates its input.
- AC3 For each of the eight `git diff` call sites listed as **fixed**, in a real repository whose `.gitattributes` names a textconv driver and whose config sets `diff.external`, the call produces the same diff output as in a plain repository and creates **no marker file** (the driver and the external diff did not run).
- AC4 `status` (fsmonitor): the `validation` snapshot and the `sandbox` tripwire do not run a `core.fsmonitor` program (no marker).
- AC5 Every choke point listed in D2 goes through `gitsafe` (a source-level test fails when a listed file builds a git command without it).
- AC6 The audit table above covers every non-test call site (the test checks at FILE granularity: a spawn added to an already-listed file needs its row by review; the table, not the test, is the audit artefact) (a test greps for `exec.Command*("git"` and the helper names and fails when a file is not named in the plan).
- AC7 `task test`, `task lint`, `go test ./internal/hygiene/`, `sh scripts/verify.sh`.

## Test scenarios (named)

- `internal/gitsafe/gitsafe_test.go`: `gitsafe_args_orders_overrides_and_diff_flags`, `gitsafe_args_finds_the_subcommand_behind_global_options`, `gitsafe_args_does_not_duplicate_or_touch_other_verbs`, `gitsafe_args_leaves_pathspecs_after_double_dash`, `gitsafe_env_drops_external_diff_and_sets_the_safe_defaults`, `gitsafe_env_does_not_mutate_its_input`.
- `internal/gitsafe/driverrepo_test.go` (a shared `NewDriverRepo(t)` test helper: a git repo with a `.txt` file, `.gitattributes` `*.txt diff=evil`, `diff.evil.textconv` and `diff.external` both writing a marker file, and `core.fsmonitor` writing a marker) used by the tests below.
- `internal/workspace/git_driver_test.go`: `taskdiff_does_not_run_textconv_or_external_diff`, `task_name_status_does_not_run_drivers`, `task_file_diff_does_not_run_drivers`, `staged_names_do_not_run_drivers`, `task_shortstat_does_not_run_drivers`, `archive_numstat_does_not_run_drivers`, `pr_shortstat_does_not_run_drivers`, `sync_conflict_names_do_not_run_drivers`, `workspace_status_does_not_run_fsmonitor`.
- `internal/steward/git_driver_test.go`: `steward_collect_does_not_run_drivers`. `internal/validation/digest_driver_test.go`: `validation_snapshot_does_not_run_fsmonitor`. `internal/learning/probe/sandbox/tripwire_driver_test.go`: `tripwire_snapshot_does_not_run_fsmonitor`. `internal/learning/probe/arm/diff_driver_test.go`: `arm_diff_does_not_run_drivers`. `internal/gitstate/driver_test.go`: `gitstate_runner_does_not_run_drivers_or_fsmonitor`.
- `internal/gitsafe/audit_test.go`: `audit_every_git_call_site_is_in_the_plan`, `audit_choke_points_use_gitsafe`.
- Each driver test also asserts the diff text equals what a plain repository gives (a binary file stays "Binary files differ", a text file keeps its hunks).

## Mutations (each by the Lead on committed code, then by the validator and the reviewers)

- **M1** drop the `--no-ext-diff` insertion from `gitsafe.Args` -> `Test_gitsafe_args_orders_overrides_and_diff_flags` fails (the driver tests still pass: the call sites spell the flag: layer two).
- **M2** drop the `--no-textconv` insertion from `gitsafe.Args` -> same.
- **M3** drop the `-c core.fsmonitor=false` prefix from `gitsafe.Args` -> `Test_gitsafe_args_orders_overrides_and_diff_flags`, `Test_workspace_status_does_not_run_fsmonitor`, `Test_validation_snapshot_does_not_run_fsmonitor`, `Test_tripwire_snapshot_does_not_run_fsmonitor` fail.
- **M4** `gitsafe.Env` keeps `GIT_EXTERNAL_DIFF` -> `Test_gitsafe_env_drops_external_diff_and_sets_the_safe_defaults` fails.
- **M5** `validation.gitCmd` bypasses `gitsafe` -> `Test_validation_snapshot_does_not_run_fsmonitor` and `Test_audit_choke_points_use_gitsafe` fail.
- **M6** remove BOTH layers at the exploitable sites (the insertion in `gitsafe.Args` and the spelled flags at the `taskDiff` and `taskFileDiff` call sites) -> `Test_taskdiff_does_not_run_textconv_or_external_diff` and `Test_task_file_diff_does_not_run_drivers` fail (measured: the external-diff marker appears).

## Staffing

GLM peers from step-by-step briefs, one commit per step, at most two at a time; two **fresh-context verifier peers** (not the authors) review the exact head before this is reported; the validator judges the head against the contract.

## Review round 1 (two fresh GLM reviewers on `854fc0748`)

- Reviewer 1 (attack): no way to make the daemon run a textconv / external diff / fsmonitor marker through the fixed paths (44 argument shapes fuzzed; `.git/info/attributes`, `extensions.worktreeConfig`, `include.path`, `GIT_CONFIG_COUNT`, `GIT_EXTERNAL_DIFF`, `GIT_DIFF_OPTS`, pager, aliases all blocked or inert). Findings, all fixed in the next commit: P2 `taskrunner/gitrules_detail.go` claimed spelled flags but relied on `gitsafe.Args` alone (now spelled); P3 `Args` looked for `-c core.fsmonitor=false` past a bare `--`, so a pathspec pair suppressed the prefix (now only before `--`, with a test); P3 `blame` / `reflog -p` run textconv and are not in the diff family (the daemon calls neither; stated in the code comment); P3 `whatchanged` is refused by current git (harmless).
- Reviewer 2 (regressions and audit): no regression (argument order, env, error text, Windows build + vet all clean; all six package groups green). Two audit rows were missing (`sync.go:328` merge/rebase, `pr.go:294` worktree add; both out of scope D4): added. P3: the audit test is file-granular: documented in AC6.

## Progress

- Spec and contract first (audit of every spawn site done by the Lead before any code). Code by GLM peers from step briefs, one commit per step: G1 (`internal/gitsafe`, `gitsafetest`), G2 (`internal/workspace`), G3 (steward, validation, tripwire, gitstate, arm, taskrunner, wsapi), G4 (the two source-audit tests). Each reviewed and cherry-picked by the Lead.
- Found while building: git runs `diff.external` and textconv only for PATCH output; the stat/name sites are inert (documented above); the real exposure is `taskDiff` and `taskFileDiff`. `validation` and the probe `tripwire` ran `git status` with no fsmonitor override at all (an agent-edited probe sandbox is exactly the repository the tripwire reads).

## Validation

- Mutations M3, M5, M6 and the gitstate / tripwire bypasses were run by the Lead on committed code; each fails its named test (see the PR body for the exact output).
- `sh scripts/verify.sh` PASS; the validator's verdict and the two reviewers' findings are in the PR body.
