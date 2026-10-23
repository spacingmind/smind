# Git and pull-request state on tasks

Size: **XL (9)** — scope 2 (daemon + web, mobile payload only), PRs 2 (six implementation PRs with ordering), risk 2 (schema migration, a
long-lived poller with a rate budget and persisted ledger, git/forge subprocesses on untrusted repos), decisions 2 (ADR-0039 and ten open
questions), verification 1 (UI screenshots, live read-only `gh`; no real-forge writes). One Lead sequences the items; every PR is **non-stacked
off `develop`** (a PR that needs an earlier PR's code waits for it to merge). The poller and call-budget PRs (3, 4) get an independent
`reviewer` pass on the exact commit before the Human is asked to merge.

Spec for [ADR-0039](../../decisions/0039-git-pr-state.md) (read it first: schema, wire, budget, errors, security). Human decisions H1–H4, forge
neutrality and read-only-mobile-later are fixed. Status: **ADR accepted 2026-10-22 (O1–O10 as recommended); implementation starting with PR 2.**

## Goal

A task knows what it is based on and which pull request it belongs to, shows its git and PR state without being asked (dirty, ahead/behind,
conflicts, PR open/draft/merged, checks, review), and offers explicit, previewed actions (Create PR, Update PR) whose disabled state says why.
A new task can start from a branch, a PR or an issue. All of it comes from one status payload, one event topic and one rate-budgeted poller.

## Scope and PRs

Research items G1–G5. Out of scope here: G6 (diff-pane fixes, another Lead), G7 push/update-from-base, G8 merge/archive UI and Restore, G9 PR
panel, G10/G11 attachments follow-ups, G12 GitHub login (contract fixed in ADR §8, own PR/plan after this one), G13 approval defaults,
G14+ and anything under `mobile/`.

| PR | Content | Size | Peer | Live checks |
|---|---|---|---|---|
| 1 | **Docs**: ADR-0039 + this plan | S | — | none |
| 2 | **G1 base**: schema (`base_ref`, `base_sha`, `branch_owned`, `workspaces.default_base`), `CreateTask` writes the base per mode, startup backfill, reflog path and `develop` constant deleted, `workspace.defaultBase`/`setDefaultBase`, `task.setBase`, `CreatePR` uses the task's base and fixes the `smind/pr-task-<id>` name clash; Workspace settings "Default base branch" field | M | peer-glm (migration, `CreateTask`, step by step) + peer-sonnet (default-base detection, `CreatePR` rewrite) | real throwaway repos (`main`-only repo, local bare `origin`) |
| 3 | **G5 daemon**: hardened git runner, local status (dirty, ahead/behind, `merge-tree` conflicts, `merged`), `internal/gitstate` skeleton, `task.git`/`task.gitList`/`task.gitFocus`, topic `task.git`, `task.stats` removed, background `fetch` with budget; no forge | L | peer-sonnet (concurrency, budget, events) + peer-glm (git helpers, tests) | real git (≥ 2.38) repos, a hostile `core.fsmonitor` repo |
| 4 | **G2 daemon**: `forge.Provider` + `ghctx` extension (`OpenPRs`, `PR`, `CreatePR`, `Status`), PR identity columns, `task_pr_state` + `forge_budget`, the poller (single call per repo, cadence, backoff, adoption, ledger), `task.gitRefresh`, `pr` in `TaskGit`, typed `forge_*` errors | L | peer-sonnet | one read-only `gh pr list` against `spacingmind/smind` (timing, shape); no writes |
| 5 | **G2+G5 web**: `useTaskGit`/`useWorkspaceTaskGit` (ADR-0032), `task.gitFocus`, PR chip (header, composer), sidebar glyph + dot, hover-card token line, `use-task-stats` removed, composer chip reads `TaskGit.diff` | L | peer-glm (detailed briefs) + Lead review | light+dark screenshots, transition frames, isolated daemon |
| 6 | **G3**: `task.prPreview`/`createPr`/`updatePr`, `actions` map, error codes end to end, Create PR dialog, `<PrAction>`, Update/View PR | M | peer-sonnet (daemon) + peer-glm (dialog) | isolated daemon + local bare remote; `gh` stubbed |
| 7 | **G4**: `workspace.branches`, Start-from control in New Task, fork gate (`untrusted_source`, setup blocked, `task.retrySetup` clears), fail-closed on unknown | M | peer-glm | isolated daemon; fork PR via stubbed forge |

Order: 1 → 2 → 3 → (4 ‖ 5, disjoint write scopes: daemon forge vs web; 5's PR half is built against the frozen `pr` shape and verified live
after 4 merges) → 6 → 7. PR 6 mounts one component in the Diff header and rebases after the G6 Lead's PR merges. PR 7 needs 2 and 4 (it
calls `Provider.PR`).

Coordination: ADR-0038's PR #459 (`internal/ghctx`) lands as is before PR 4 starts; PR 4 owns moving `ghctx` onto `internal/forge` and the `forge_*` codes (the ADR-0038 Lead does not touch ghctx internals). Nothing in `mobile/`.

## Acceptance criteria

### PR 1 — docs

- AC1. ADR-0039 exists (Accepted) with the schema, wire (RPCs, `TaskGit`, topic), poll budget and backoff, GitHub rate-limit handling, gh-auth
  surface, error codes, UI placement with ZCode/Paseo references, security, and open questions each with a recommendation; it records H1–H4 as given.
- AC2. This plan has a size line, ACs, named tests and a non-stacked PR split; the Supervisor accepted the ADR before PR 2 started.

### G1 — stored base (PR 2)

- AC3. **Schema.** `tasks.base_ref`, `tasks.base_sha`, `tasks.branch_owned`, `workspaces.default_base` exist in `schema.sql` and as idempotent
  migrations; an existing database migrates without error and a second start changes nothing.
- AC4. **Per start mode** (ADR §2 table): default, `baseRef`, existing `branch` and `prNumber` tasks each store the right `base_ref` and `base_sha`
  and `branch_owned` (1 for created branches, 0 for an existing user branch). A failed creation leaves no row.
- AC5. **Backfill.** Pre-existing tasks with a worktree get `base_sha` once from the reflog and `base_ref` from the detected default; after that
  no code path reads the reflog. A task whose base cannot be recovered returns `base_unknown` from diff/PR actions; `task.setBase` fixes it.
- AC6. **Diff** (`task.diff`, `task.files`, `task.fileDiff`, stats) uses `base_sha`; expiring or deleting the branch reflog does not change it.
- AC7. **Default base.** Detection order `origin/HEAD` → `develop` → `main` → `master` → current branch; `workspace.setDefaultBase` validates the
  ref; an override wins; a repo with only `main` and no `develop` works end to end.
- AC8. **PR base.** `CreatePR` opens against the task's `base_ref` (or an explicit `baseBranch`), never a constant; the cherry-pick path's
  throwaway branch is `smind/pr-task-<id>` and is deleted after a failed cherry-pick, so a second attempt does not fail on "branch exists".
- AC9. **Settings UI.** Workspace settings show the detected default base, accept an override, and show the validation error as one sentence.

### G5 — local git status (PR 3)

- AC10. **`TaskGit`** (ADR §3) is returned by `task.git` and `task.gitList` with `worktree`, `commits`, `origin`, `conflict`, `merged`, `diff`,
  `base`, `branch`, `actions` (no `pr` yet). A field that cannot be computed is absent, never zero. `task.stats` no longer exists.
- AC11. **Conflicts.** `conflict.state` is `conflicts` with the file list when `git merge-tree --write-tree` reports them, `none` otherwise, and is
  not even run when the branch is not both ahead and behind; it is cached by the pair of tips and re-run only when one moves; with git < 2.38 it is
  `unknown` and nothing fails.
- AC12. **Merged.** `merged.state` is `merged` (`via` `ancestor`) for a branch contained in `origin/<base>`, `unmerged` otherwise, `unknown` when
  base is unreadable.
- AC13. **Hardening.** Every git call in `gitstate` and in the touched workspace files uses the shared runner (`core.fsmonitor=false`,
  `--no-optional-locks` for status, `GIT_TERMINAL_PROMPT=0`, deadlines, argv only). A repository whose `.git/config` sets an `fsmonitor` command
  that touches a marker file never runs it from these paths.
- AC14. **Event.** Topic `task.git` is in `knownTopics`; payload `{taskId, git}`; published only when the canonical payload changes
  (ignoring timestamps), coalesced to ≤ 1 per task per second; triggers: run end, `task.commit`, `task.stage`, `task.setBase`, fetch result,
  focused-task tick.
- AC15. **Interest and budget.** No subscriber on `task.git` → no fetch and no tick. `git fetch origin <base>` runs ≤ once per 180 s per workspace
  (floor 60 s), single-flight, with a 20 s deadline; a hung remote cannot block a read.
- AC16. **Disabled reasons.** `actions.createPr` / `updatePr` carry `{enabled, code, reason}`; each reason is a full sentence naming the next
  step; a table test pins every `(state → code → reason)`.

### G2 — PR identity, poller, forge (PR 4)

- AC17. **Forge interface.** `internal/forge` has the types and `Provider`; `ghctx.Client` implements it; poller, preview and handlers import
  `forge`, not `ghctx`; one runner, argv only; no second `gh` client.
- AC18. **Identity.** `pr_forge/pr_repo/pr_number/pr_url/pr_head_ref` are written by Create PR, Start-from-PR and adoption only; a `pr` attachment
  never sets them; `pr_url` is stored only if `https` on the forge host.
- AC19. **One call per repo.** N watched tasks of one repo cost exactly one `OpenPRs` call per tick regardless of how many clients are connected; a
  task whose PR left the open list costs one `PR` call, then is terminal.
- AC20. **Cadence.** Focused + pending checks → 20 s; focused otherwise → 120 s; watched-not-focused → 300 s; merged/closed → none; verified on an
  injected clock.
- AC21. **Budget.** 15 s per-repo floor; 240 calls/hour/host; explicit refresh floor 5 s and 12/hour/task; over any limit the cache is served,
  nothing is queued or retried, and `pr.stale = "budget"` with `refreshAvailableAt`.
- AC22. **Backoff and suspension.** Failures back off 30 → 60 → 120 → 300 s; `forge_rate_limited` waits ≥ 10 min; `forge_unauthenticated` suspends
  the host until `forge.status`/a token change reports authenticated; `pr.stale` says which.
- AC23. **Restart-safe.** The ledger (`next_allowed_at`, failures, hourly count) survives a daemon restart; start-up polls are jittered and never
  precede `next_allowed_at`; a restart loop makes no calls beyond what the ledger allows.
- AC24. **Cache-first reads.** `task.git`, `task.gitList` and `task.git` events never spawn `gh`; with no subscribers the poller makes no call.
- AC25. **Adoption.** An open PR whose `headRefName` equals the task branch in the same repo and is not cross-repository is adopted at no extra
  call; a fork PR or another repo's PR is not.
- AC26. **Mapping.** `statusCheckRollup` → `checks{state,passed,failed,pending,total}`, `reviewDecision`, `mergeable`, `isDraft`, `state` map per
  ADR §3 via a table test; unknown values become `unknown`/`none`, never a crash.
- AC27. **Agents cannot drive it.** `task.gitRefresh`, `task.createPr`, `task.updatePr` refuse run-scoped callers.

### G2 + G5 — web (PR 5)

- AC28. `useTaskGit` and `useWorkspaceTaskGit` use keys from `lib/query-keys.ts`, `staleTime: Infinity`; `useDaemonEvents` is the only place that
  handles `task.git` (`setQueryData`); reconnect invalidates; components make no `client.call("task.git")` of their own; `use-task-stats.ts` is gone.
- AC29. The **PR chip** shows in the task header and the composer chips (`#123 · Open · ✓ checks` / `· ✗ 2 failing` / `· Changes requested` /
  `· Draft` / `· Merged` / `· Closed`), always with text, colour only on failing/blocking; click opens the PR URL; a stale snapshot shows its age.
- AC30. The **sidebar row** keeps `+N −M` and adds at most one PR glyph and one dot (unpublished or conflicts); a row with nothing to say shows
  nothing; the **hover card** shows branch → base and the token line `+22 −2 · 3 commits · ↑1 · ↓5 behind develop · ⚠ conflicts with develop`
  (destructive tint on conflicts only), as quiet mono tokens.
- AC31. The composer's diff chip updates when a run ends and when a commit lands (no stale count), from `TaskGit.diff`.
- AC32. All icons come from `lib/icons.ts`; PR text is rendered as text; the chip handles a missing `pr`, a missing `checks` and an unknown state.
- AC33. **Visuals.** Light and dark screenshots of: header with each PR state, composer chips, sidebar rows (none / glyph / dot / both), hover card
  with conflicts, a stale-budget state, narrow width; **transition frames** (chip appearing after Create PR, hover card open/close, sidebar glyph
  change) captured on the real app and reviewed by the Lead; no mid-animation glitch.

### G3 — Create PR (PR 6)

- AC34. `task.prPreview` returns base, head strategy, commits (≤ 50), uncommitted counts, existing PR, deterministic default title/body,
  `draft: true`, blockers; the PR half is cache-first.
- AC35. `task.createPr {taskId, baseBranch, title, body, draft}` validates title (≤ 256, no NUL) and body (≤ 64 KiB), passes the body on **stdin**,
  uses `--draft` per the flag, stores the identity, publishes `task.git`; with an existing open PR it returns `pr_exists {url, number}` and adopts it
  without calling `gh pr create`.
- AC36. `task.updatePr` pushes fast-forward only to `pr_head_ref`, never `--force`; refuses `pr_not_open`, `push_rejected`, `fork_pr_readonly`.
- AC37. **Errors.** Every failure carries a typed `rpcError.code` from ADR §6 and a fixed sentence; no message or `detail` contains an argv, a token,
  or an env value; scrubbed stderr tail ≤ 300 chars only in `detail`.
- AC38. **Dialog.** Opens from **Create PR…** with base combobox, title, body, Draft switch (default on, remembered per workspace), the "N commits · M
  files uncommitted (not included)" line with "Commit staged first", blockers as a banner, the error in one line with Details; **Update PR** and
  **View PR** replace Create once a PR exists; the button's disabled state shows the `reason` sentence.
- AC39. `<PrAction>` is a single component mounted in the Diff header slot; PR 6 touches no other part of `diff-viewer-pane` and no `review-*` file.
- AC40. **Visuals** as AC33 for the dialog (open/close frames), every blocker banner, an error, Update/View state; light + dark, narrow.

### G4 — Start from (PR 7)

- AC41. `workspace.branches` lists local and `origin/*` branches with `ahead`/`behind` vs the default base for the page, no network.
- AC42. New Task dialog has Start from = Workspace HEAD / Branch… / Pull request… / Issue…; the PR/issue pickers reuse `github.search`; pasting a
  PR/issue URL or `#123` in the picker resolves one item; an issue sets the title and attaches an `issue` on the first prompt; a PR sends
  `prNumber`.
- AC43. **Fork gate.** A fork PR creates a task with `untrusted_source = 1`, `setup_status = "blocked"`, no setup run and no teardown at archive; a
  banner explains it; `task.retrySetup` runs setup and clears the flag; an unreachable forge at creation creates the task untrusted (fail closed);
  a same-repo PR is trusted and runs setup.
- AC44. **Visuals** as AC33: the dialog in each mode, the fork badge and banner, picker states (loading, empty, `forge_unauthenticated`); light +
  dark, narrow, transition frames.

### Cross-cutting

- AC45. `task test`, `task lint` and `cd web && bunx tsc -b packages/ui` pass on every PR; each PR lists verified and unverified items.
- AC46. Nothing in the implementation creates a PR, issue or comment on a real GitHub repo; live `gh` use is read-only against `spacingmind/smind`.
- AC47. Nothing under `mobile/` changes; `TaskGit` has no web-only field (a JSON-schema-style test pins the field list).

## Test scenarios

Names are the contract; Peers use these exact names.

**store / migration** (`internal/store`, `internal/workspace`): `TestMigrate_TaskBaseColumns_Idempotent`, `TestMigrate_BackfillsBaseSha_FromReflogOnce`,
`TestMigrate_BackfillBranchOwned_ByPrefixOnly`, `TestMigrate_UnrecoverableBase_LeavesEmpty`,
`TestCreateTask_StoresBase_DefaultMode`, `TestCreateTask_StoresBase_BaseRefMode`, `TestCreateTask_StoresBase_ExistingBranchMode`,
`TestCreateTask_StoresBase_PullRequestMode`, `TestCreateTask_BranchOwned_PerMode`, `TestCreateTask_FailureLeavesNoRow`,
`TestTaskDiff_UsesStoredBaseSha_ReflogExpired`, `TestTaskDiff_BaseUnknown_TypedError`, `TestNoReflogReaders` (grep-style guard),
`TestDefaultBase_DetectionOrder`, `TestDefaultBase_MainOnlyRepo`, `TestWorkspaceSetDefaultBase_ValidatesRef`,
`TestTaskSetBase_RecomputesSha`, `TestCreatePR_UsesTaskBase_NoDevelopConstant`, `TestCreatePR_CherryPickBranchName_NoClash`,
`TestCreatePR_CherryPickFailureDeletesBranch`.

**gitstate / local status** (`internal/gitstate`): `TestGitStatus_DirtyCounts`, `TestGitStatus_AheadBehindBase`,
`TestGitStatus_OriginUnpublished`, `TestGitStatus_OriginAheadBehind`, `TestGitStatus_MergeTreeConflict_Files`,
`TestGitStatus_MergeTreeSkippedWhenNotBothAheadAndBehind`, `TestGitStatus_MergeTreeCachedByTips`, `TestGitStatus_GitTooOld_Unknown`,
`TestGitStatus_MergedByAncestor`, `TestGitStatus_MergedByPRHeadSha`, `TestGitStatus_AbsentNotZero`,
`TestGitRunner_HostileFsmonitorNotExecuted`, `TestGitRunner_DeadlineKillsProcess`, `TestGitRunner_ArgvOnly`,
`TestFetch_FloorAndSingleFlight_CountAsserting`, `TestFetch_NoSubscribersNoCalls`, `TestFetch_HungRemoteDoesNotBlockRead`,
`TestTaskGitEvent_PublishedOnChangeOnly`, `TestTaskGitEvent_CoalescedPerSecond`, `TestTaskGitEvent_TriggersRunEndCommitStageSetBase`,
`TestTaskGitList_ReplacesTaskStats`, `TestActions_DisabledReasons_Table`, `TestTaskGit_FieldListPinned`.

**forge / poller** (`internal/forge`, `internal/ghctx`, `internal/gitstate`): `TestGhctx_ImplementsForgeProvider`,
`TestGhctx_OpenPRs_ArgvAndFields`, `TestGhctx_CreatePR_BodyOnStdin_DraftFlag`, `TestGhctx_ErrorMapping_Table`,
`TestPoller_OneCallPerRepoPerTick_CountAsserting`, `TestPoller_ManyClientsSameCalls`, `TestPoller_NoSubscribersNoCalls`,
`TestPoller_CadencePendingFocusedWatched`, `TestPoller_TerminalStateStopsPolling`, `TestPoller_PRLeftOpenList_OneViewCall`,
`TestPoller_FloorAndHourlyBudget_ServesCache`, `TestPoller_BackoffSequence`, `TestPoller_RateLimited_BackoffMinimum`,
`TestPoller_Unauthenticated_SuspendsHost`, `TestPoller_LedgerSurvivesRestart`, `TestPoller_StartupJitterRespectsNextAllowed`,
`TestPoller_AdoptsOpenPRByHeadRef`, `TestPoller_DoesNotAdoptForkPR`, `TestPoller_SingleFlightConcurrentRefresh`,
`TestRefresh_PerTaskFloorAndHourly`, `TestRefresh_RejectedForRunScoped`, `TestTaskGit_ReadNeverSpawnsForge`,
`TestPRMapping_ChecksReviewMergeable_Table`, `TestPRURL_Validation`, `TestPRIdentity_AttachmentDoesNotSet`.

**wsapi / create-update PR** (`internal/wsapi`, `internal/workspace`): `TestPrPreview_CommitsAndUncommitted`, `TestPrPreview_DefaultBodyDeterministic`,
`TestPrPreview_ExistingPrCacheFirst`, `TestCreatePr_Draft`, `TestCreatePr_TitleBodyValidation`, `TestCreatePr_PrExists_TypedAndAdopts`,
`TestCreatePr_NoCommits_Code`, `TestCreatePr_RunActive_Code`, `TestCreatePr_ForgeErrors_Codes`,
`TestCreatePr_ErrorsNeverContainArgvOrToken`, `TestCreatePr_RunScopedRefused`, `TestUpdatePr_FastForwardPush`, `TestUpdatePr_NeverForce`,
`TestUpdatePr_PushRejected`, `TestUpdatePr_PrNotOpen`, `TestUpdatePr_ForkPrReadOnly`, `TestUpdatePr_CherryPickPrDisabledReason`.

**start-from / fork gate** (`internal/workspace`, `internal/wsapi`): `TestWorkspaceBranches_LocalAndOrigin`, `TestWorkspaceBranches_AheadBehindPageOnly`,
`TestCreateTask_ForkPR_Untrusted_SetupBlocked`, `TestCreateTask_ForkPR_TeardownSkipped`, `TestCreateTask_SameRepoPR_RunsSetup`,
`TestCreateTask_PRForgeUnavailable_FailsClosed`, `TestRetrySetup_ClearsUntrusted`, `TestUntrusted_OnlyUserClears`.

**web** (`web/packages/ui`): `use-task-git.test.tsx` (`it keys from query-keys`, `it patches on task.git event`, `it invalidates on reconnect`,
`it calls task.gitFocus for the task on screen`), `pr-chip.test.tsx` (`it renders each state with text`, `it colours only failing/blocking`,
`it handles missing pr/checks/unknown state`, `it shows stale age`, `it renders PR title as text`), `git-tokens.test.tsx`
(`it renders the token line`, `it tints only conflicts`, `it shows nothing when there is nothing to say`),
`sidebar-git-glyph.test.tsx` (`it shows at most one glyph and one dot`), `composer-status-chips.test.tsx` (`it refreshes the diff chip after a run`),
`create-pr-dialog.test.tsx` (`it prefills from prPreview`, `it defaults Draft on and remembers per workspace`, `it shows uncommitted notice`,
`it disables with the reason sentence`, `it shows a typed error with details`, `it swaps to Update/View when a PR exists`),
`new-task-dialog.test.tsx` (`it has four start modes`, `it sends baseRef/branch/prNumber`, `it shows the fork badge`, `it handles forge_unauthenticated`),
`workspace-settings.test.tsx` (`it shows the detected default base`, `it validates an override`). No `client.call("task.git")` outside the hook
(`TestNoDirectTaskGitCalls`, a source-scan test).

**live** (isolated daemon: own `SMIND_HOME`, own port, never `~/.smind` or 4648; throwaway repos with a local bare `origin`; `gh` stubbed for all
writes): create a task on a `main`-only repo and open the PR preview; commit, publish, move `origin/main` and watch `↓N` and the conflict token;
fork-PR task via a stub; one read-only `gh pr list` against `spacingmind/smind` for shape and timing.

## Implementation notes

Filled in as work proceeds (deviations from the ADR text with the same behaviour).

**PR 3 (G5 daemon), decided 2026-10-22 with the Supervisor:**

- `task.stats` is **not** removed in PR 3: the web sidebar still calls it. PR 5 deletes the web hook and the daemon RPC together, so AC10's "`task.stats` no longer exists" is satisfied there.
- `TaskGit.commits` gains an additive `sinceFork` (commits past `base_sha`): the Create-PR precondition is "at least one commit past the fork point", which `aheadOfBase` (against `origin/<base>`) cannot answer.
- `actions.createPr`'s `run_active` blocker is an input flag of `gitstate.Actions`; PR 3 passes false and PR 6 supplies it from the run registry.
- `internal/gitstate` does not import `internal/workspace`; it receives what it needs through `Input` and the `Source` interface that `wsapi` adapts from `*workspace.Manager`.
- `ComputeConflict`/`ComputeMerged` (not `Conflict`/`Merged`, which are the payload types). `ComputeMerged` takes `sinceFork` so an empty branch is not reported as merged. The conflict file list is capped at 100.
- The service is built inside `wsapi.New` (it needs the bus and the focus registry); `serve.go` starts and stops it. Cold `task.gitList` computes with 4 workers.
- Interest is connection-level: any `task.git` subscriber turns the loop on; focus comes from `task.gitFocus`. A fetch runs for each workspace with a cached task, at most every 180 s (floor 60 s), only while someone subscribes.

**PR 4 (forge poller + PR identity), decided 2026-10-22:**

- `forge.Provider` gains `Repo`, `OpenPRs`, `PR`. `CreatePR` moves to PR 6 (its only caller) and `Status` to G12: the poller needs no `Status` (an unauthenticated result suspends the host for 10 minutes and the next attempt is the re-probe). The `ghctx.Snapshot/Item/SearchResult` aliases are deleted (ADR-0038 PR 5b has merged).
- Polling groups by **workspace** (all its tasks share `origin`); the repo is resolved once per workspace via `Provider.Repo` (cached 1 h, counted against the budget). Workspaces are polled one after another within a tick, so a failing host is known before the next workspace calls.
- A rate limit pauses the **whole host** (GitHub limits are per token), in memory; an unauthenticated result suspends the host 10 minutes. Per-task backoff 30/60/120/300 s and the explicit-refresh ledger are persisted.
- `task.gitRefresh`: joins an in-flight poll for free, honours the task's `next_allowed_at` (it cannot bypass a backoff or rate limit), stamps the 12/hour ledger only when a call is actually allowed, returns the cached snapshot with `stale: "budget"` when blocked, and `refresh_limited {retryAt}` when over its own floor/cap. Run-scoped (agent) callers are refused.
- `actions.createPr` reports `pr_exists` before `base_unknown`, `run_active` and `no_commits`; `actions.updatePr` is `fork_pr_readonly` for a fork PR.
- A closed PR re-arms one fetch only when the local branch tip changes after the first observation and differs from `pr.headSha`.

**PR 7 (G4 daemon, step 7-A), decided 2026-10-23:**
- `task.create` with `prNumber` asks `Provider.PR` once (workspace `SetForge(func() forge.Provider)`, the same ghctx client `forge.search` uses, looked up per call; nil = unavailable). Fork PR (`isCrossRepository`) or any provider failure → `tasks.untrusted_source = 1` (fail closed, no PR identity, base = workspace default). A same-repo PR is trusted, stores identity (`SetTaskPR` fields written in the same insert; head ref = the PR's `headRefName`) and `base_ref` = the PR's `baseRefName` (validated like a user ref; one bounded best-effort `git fetch origin <base>` when the checkout has no `origin/<base>`, so the merge-base `base_sha` resolves).
- Untrusted + workspace has a setup script → `setup_status = "blocked"` (new, additive), the script does not run, and `runTeardown` skips (appends "teardown skipped…" to `setup_log`). Without a setup script nothing is blocked (`setup_status` stays empty) and `task.retrySetup` just clears the flag. Only `BeginTaskSetup` (the user's `task.retrySetup`) clears the flag, in the same atomic UPDATE; a run-scoped caller of `task.retrySetup` gets `forbidden`. Agents on a blocked task can still run (only setup/teardown are gated, ADR §7).
- `task.retrySetup` also invalidates `task.git` so `TaskGit.untrusted` follows the row.
- `workspace.branches {workspaceId, query?, limit}`: local + `origin/*`, `origin/HEAD` hidden, local wins duplicates, newest commit first (name tie-break), query = case-insensitive substring, default 30 / max 50. `ahead`/`behind` come from one `rev-list --left-right --count <origin/base or base>...<ref>` per returned branch only, and are **omitted** (not zero) when the default base cannot be resolved locally. No network.
- `forge.Item.fromFork` from `isCrossRepository` in the PR search list only. A one-item resolve (pasted PR URL/`#123`) goes through `gh pr view` and does not carry it; creation still gates on `Provider.PR`, so the fork flag is correct at creation either way.

**PR 7 (web), decided 2026-10-23:** Branch… has a toggle after picking: "New branch from it" (default, sends `baseRef`) or "Check it out" (sends `branch`; git refuses if it is checked out elsewhere); an origin-only branch sends `origin/<name>` as `baseRef` and the short name as `branch`. An issue carries into the new task as composer draft text (`Resolve #<n>: <title>` + url), not a real `issue` attachment, because the attachment drafts are keyed by chat id, which does not exist at creation. A picked PR is not attached to the first prompt (the task already carries the PR identity). The untrusted banner and Run setup key on `UntrustedSource`, not on `SetupStatus`, because a workspace without a setup script has no `blocked` status. `forge.search` accepts `workspaceId` or `taskId` (exactly one).

## Decisions

Taken from ADR-0039 (Accepted 2026-10-22).

| # | Decision | Status |
|---|---|---|
| H1/H2 | write the ADR; order G1 → G2+G5 → G3+G4 | Human, fixed |
| H3 | delete merged owned branches only; unmerged kept with a warning; Restore; UI in G8 | Human, fixed |
| H4 | paste-a-token over E2EE → `gh auth login --with-token` on stdin; device flow later; surface fixed in ADR §8, implemented as G12 | Human, fixed |
| Forge neutrality | `forge`/`pr`/`issue` vocabulary; provider interface, GitHub only in v1 | Human, fixed |
| Mobile | read-only, later (P2) | Human, fixed |
| D1 | `internal/forge` interface, `ghctx` (ADR-0038, #459) is the only `gh` client | accepted |
| D2 | `tasks.base_ref/base_sha/branch_owned/pr_*/untrusted_source`, `workspaces.default_base`, `task_pr_state`, `forge_budget` | accepted |
| D3 | one `TaskGit` payload, `task.git` read + `task.gitList` + one topic `task.git`; `task.stats` removed | accepted |
| D4 | server-computed `actions` with sentence reasons | accepted |
| D5 | interest-driven poller, one list call per repo, 20/120/300 s, 15 s floor, 240/h/host, backoff to 300 s, persisted ledger | accepted |
| D6 | typed `forge_*` + git/PR codes in `rpcError.code`, no argv | accepted |
| D7 | fork PR: untrusted flag blocks setup, fail closed, user clears via `task.retrySetup` | accepted |
| O1–O10 | see ADR "Open questions" (each with a recommendation) | accepted as recommended |

## Progress

- 2026-10-22: G1 merged (#469), forge interface merged (#471). PR 3 (G5 daemon) implemented: `internal/gitstate` (hardened runner, local status, merge-tree conflict token, merged verdict, actions policy, service with coalesced `task.git` events and the interest-gated fetch loop), `task.git`/`task.gitList`/`task.gitFocus`, invalidation on run end / commit / stage / setBase.
- 2026-10-22: research read (G1–G19 list, appendices A–C), ADR-0038/#459/0009/0028/0032/0033/0035 and the code seams re-checked, one live read-only
  `gh pr list` against `spacingmind/smind` to confirm the single-call-per-repo batching (4 PRs, ~1 s); ADR-0039 and this plan written; PR 1
  opened for review. Nothing else started.

## Validation

How each AC gets satisfied is recorded here per PR as it lands (command output, screenshots, call counts).

**PR 2 (G1, #469):** AC3–AC9 by the named tests under "store / migration", live on an isolated daemon (`main`-only repo: `base_ref=main`, fork-commit `base_sha` also with an unpushed checkout commit) and screenshots of the settings dialog.

**PR 7 (G4 daemon, 7-A):** AC41 and the daemon half of AC43 by `TestWorkspaceBranches_LocalAndOrigin`, `TestWorkspaceBranches_AheadBehindVsDefaultBase`, `TestWorkspaceBranches_AheadBehindPageOnly` (counts `rev-list` calls: one per returned branch), `TestWorkspaceBranches_RPC`, `TestCreateTask_ForkPR_Untrusted_SetupBlocked`, `TestCreateTask_ForkPR_TeardownSkipped`, `TestCreateTask_SameRepoPR_RunsSetupAndStoresIdentity`, `TestCreateTask_PRForgeUnavailable_FailsClosed` (no provider / nil / unauthenticated / cli missing), `TestCreateTask_PR_BaseRefFromPR`, `TestRetrySetup_ClearsUntrusted`, `TestUntrusted_OnlyUserClears`, `TestTaskGit_UntrustedFlag`, `TestTaskCreate_ForkPR_Gate_EndToEnd` (over the WebSocket, ghctx stubbed, run-scoped refusal), and `TestMigrate_TaskUntrustedSource_Idempotent`. Web half (AC42, AC44) is step 7-B.

**PR 7 (G4 web, 7-B), AC42 and AC44:** `new-task-dialog.test.tsx` (four start modes, sends `baseRef`/`branch`/`prNumber`, fork badge, `forge_unauthenticated` as one sentence, title from a picked issue), `untrusted-banner.test.tsx`, the blocked setup badge and strip. Live (isolated daemon on port 4851, throwaway repo with a bare origin and fake PR head refs, a scripted fake `gh` on PATH, a workspace setup script that writes a marker file; nothing touched GitHub): `workspace.branches` listed local and origin branches with `+N −M`; `forge.search` by `workspaceId` returned a fork PR with `fromFork: true`; creating a task from the fork PR gave `UntrustedSource: true`, `SetupStatus: blocked` and **no marker file**, while a same-repo PR task ran setup; in the UI the fork task showed the **Setup blocked** badge and the banner, **Run setup** ran the script (marker file written), cleared the flag and removed the banner. Screenshots light and dark: dialog in all four modes (branch list with divergence and local/origin tags, PR list with the fork badge, issue list), the fork banner, and Run-setup frames at 0/60/200/1500 ms (the banner is replaced by the finished strip in one step, no intermediate state). Review fixes found there: the centred dialog jumped between modes (it is now top-anchored; the dialog top measured 96 px in all four modes), and a blocked task's strip said "Setup finished" (now "Setup has not run").

**PR 4 (forge poller):** AC17–AC27 by the 20 named `TestPoller_*`/`TestRefresh_*`/`TestTaskGit_*`/`TestPRIdentity_*` tests with a counting fake provider and an injected clock (poller tests also run 15× under `-race`). Live (isolated daemon, port 4841, a clone of spacingmind/smind, `gh` wrapped on PATH to log every call, read-only GitHub calls only, no write of any kind): no subscriber, 60 s → **0 gh calls**; subscribed + focused on a task whose branch is the head of open PR #498 → `repo view` once, then exactly **one `pr list` per tick** (20 s pending cadence + 5 s beat), the PR was **adopted** (`task.updated` with `PRNumber 498`) and `TaskGit.pr` showed the real state, review decision, mergeability and checks (`pending 3 passed / 1 pending`), `createPr` disabled `pr_exists`, `updatePr` enabled; restarted with `GH_TOKEN=<bogus>` → exactly **one failed `repo view`**, then the host was suspended (`pr.stale: "unauthenticated"`, `refreshAvailableAt` +10 min), no further calls in 70 s, and the last known snapshot stayed visible; no token text in the daemon log.

**PR 3 (G5 daemon):** AC10–AC16 by the `TestGitStatus_*`, `TestGitRunner_*`, `TestFetch_*`, `TestTaskGitEvent_*`, `TestActions_*`, `TestTaskGit_*` tests. AC10's `task.stats` removal moves to PR 5. Live (isolated daemon on port 4821, throwaway repo with a bare origin, `git` wrapped on PATH to log every call): with **no `task.git` subscriber, 70 s of idle plus `task.git`/`task.gitList` reads made 12 local git calls and 0 `fetch`**; after subscribing and focusing, the first tick ran exactly one `git fetch --no-tags origin +refs/heads/main:refs/remotes/origin/main`, the payload then showed `behindBase 1 / aheadOfBase 1 / sinceFork 1`, `conflict: conflicts [shared.txt] against origin/main`, `origin.published false`, dirty counts, and the next focus tick published one `task.git` event (untracked 1 → 2).

**PR 5 (web):** AC28–AC33 by the named web tests (`use-task-git`, `pr-chip*`, `git-tokens`, `sidebar-git-glyph`, header/composer/sidebar tests) and screenshots on the real app (isolated daemon, throwaway repo: a conflicting+dirty+unpublished task, a quiet task, a published task) in light and dark: sidebar rows (nothing / glyph / dot / both), header chip and both hover cards, composer chips, every PR state (failing, success, pending, draft, changes requested, merged, closed, stale), a 520 px width, and the chip-appears event frames. The PR objects were **injected into the WebSocket traffic** (Playwright `routeWebSocket`) because the poller is PR 4; the daemon-side `task.git`/`task.gitList` data was real. The real PR half is verified live after PR 4 merges.

**PR 5 implementation notes (web + `task.stats` removal):** `useWorkspaceTaskGit`/`useTaskGit`/`useTaskGitFocus` (one shared focus stack, ADR-0032), `task.git` event bridge, `PrChip`/`GitTokens`/`SidebarGitGlyph`/`GitBranchLine`, mounted in the sidebar row + hover card, header + hover card and composer chips. `task.stats` and `use-task-stats` are gone (AC10). The composer takes `git?: TaskGit` instead of `diffStat`; `task-detail.tsx` no longer runs its own `useTaskDiff` (only the chip used it; the Diff pane keeps its own query). A stale PR snapshot shows its age in the chip label itself (not only the tooltip). The 5-C GLM Peer hit the provider's 5-hour limit after its first file; the step was finished by peer-sonnet.
