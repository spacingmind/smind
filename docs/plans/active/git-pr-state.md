# Git and pull-request state on tasks

Size: **XL (9)** — scope 2 (daemon + web, mobile payload only), PRs 2 (six implementation PRs with ordering), risk 2 (schema migration, a
long-lived poller with a rate budget and persisted ledger, git/forge subprocesses on untrusted repos), decisions 2 (ADR-0039 and ten open
questions), verification 1 (UI screenshots, live read-only `gh`; no real-forge writes). One Lead sequences the items; every PR is **non-stacked
off `develop`** (a PR that needs an earlier PR's code waits for it to merge). The poller and call-budget PRs (3, 4) get an independent
`reviewer` pass on the exact commit before the Human is asked to merge.

Spec for [ADR-0039](../../decisions/0039-git-pr-state.md) (read it first: schema, wire, budget, errors, security). Human decisions H1–H4, forge
neutrality and read-only-mobile-later are fixed. Status: **ADR proposed 2026-10-22; nothing implemented until the Supervisor accepts.**

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

Coordination: ADR-0038's PR #459 (`internal/ghctx`) merges before PR 4 starts (DEPENDENCY_REQUEST). Nothing in `mobile/`.

## Acceptance criteria

### PR 1 — docs

- AC1. ADR-0039 exists (Proposed) with the schema, wire (RPCs, `TaskGit`, topic), poll budget and backoff, GitHub rate-limit handling, gh-auth
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

## Decisions

Taken from ADR-0039 (Proposed 2026-10-22).

| # | Decision | Status |
|---|---|---|
| H1/H2 | write the ADR; order G1 → G2+G5 → G3+G4 | Human, fixed |
| H3 | delete merged owned branches only; unmerged kept with a warning; Restore; UI in G8 | Human, fixed |
| H4 | paste-a-token over E2EE → `gh auth login --with-token` on stdin; device flow later; surface fixed in ADR §8, implemented as G12 | Human, fixed |
| Forge neutrality | `forge`/`pr`/`issue` vocabulary; provider interface, GitHub only in v1 | Human, fixed |
| Mobile | read-only, later (P2) | Human, fixed |
| D1 | `internal/forge` interface, `ghctx` (ADR-0038, #459) is the only `gh` client | proposed |
| D2 | `tasks.base_ref/base_sha/branch_owned/pr_*/untrusted_source`, `workspaces.default_base`, `task_pr_state`, `forge_budget` | proposed |
| D3 | one `TaskGit` payload, `task.git` read + `task.gitList` + one topic `task.git`; `task.stats` removed | proposed |
| D4 | server-computed `actions` with sentence reasons | proposed |
| D5 | interest-driven poller, one list call per repo, 20/120/300 s, 15 s floor, 240/h/host, backoff to 300 s, persisted ledger | proposed |
| D6 | typed `forge_*` + git/PR codes in `rpcError.code`, no argv | proposed |
| D7 | fork PR: untrusted flag blocks setup, fail closed, user clears via `task.retrySetup` | proposed |
| O1–O10 | see ADR "Open questions" (each with a recommendation) | pending the Supervisor |

## Progress

- 2026-10-22: research read (G1–G19 list, appendices A–C), ADR-0038/#459/0009/0028/0032/0033/0035 and the code seams re-checked, one live read-only
  `gh pr list` against `spacingmind/smind` to confirm the single-call-per-repo batching (4 PRs, ~1 s); ADR-0039 and this plan written; PR 1
  opened for review. Nothing else started.

## Validation

How each AC gets satisfied is recorded here per PR as it lands (command output, screenshots, call counts). Empty until implementation.
