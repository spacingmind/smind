# 0039: Git and pull-request state on tasks (stored base, PR identity, a `task.git` topic, previewed Create PR)

## Status

**Proposed — 2026-10-22.** Written by the git/PR Lead for the Supervisor's review. The Human decided H1–H4 on 2026-10-22 (recorded under
[Decisions made by the Human](#decisions-made-by-the-human)); everything else here is a recommendation until accepted. Plan:
[`docs/plans/active/git-pr-state.md`](../plans/active/git-pr-state.md). Scope: research items G1–G5 (P0), plus the policy and data
that P1 items G8 (archive) and G12 (GitHub login) need. Research behind it is a local-only study (`docs/research/local/git-github-ux*.md`,
not committed).

Builds on ADR-0006 (human-authored commits), ADR-0009 (lifecycle topics; this is the "own topic" it left open), ADR-0024 (worktrees and
archive), ADR-0028 (call-budget rules), ADR-0032 (TanStack Query + zustand), ADR-0033 and ADR-0035 (remote daemons, relay E2EE),
ADR-0038 (the `gh` client and the forge-neutral `pr`/`issue` vocabulary).

## Context

smind has the bones of a git workflow and almost no feedback loop (verified at `6b2bc582`, 2026-10-22; file:line evidence in the local
research appendix A):

- **The base is never stored.** `store.Task` holds `WorktreePath` and `Branch` only (`internal/store/types.go:89-111`). The diff, stats and
  PR all recover the fork point from the branch reflog (`internal/workspace/git.go:344-363`), which is wrong for existing-branch and PR tasks
  and breaks when the reflog expires. The PR base is the constant `"develop"` (`internal/workspace/pr.go:18`) and the UI never sends one
  (`diff-viewer-pane.tsx:316`), so Create PR fails on any `main` repo.
- **The PR is forgotten.** The URL is React state in the Diff pane (`:92`), cleared on remount. Nothing reads PR, CI, review or mergeability
  state. A second Create PR click is an error (`gh` says "already exists"), and a task started from a PR cannot update that PR.
- **Errors are raw.** The UI prints the whole `gh` argv in red.
- **No git status beyond a diff stat.** No dirty/ahead/behind/conflict signal, so actions cannot be enabled or disabled by state. The
  composer's diff chip goes stale after a run (`task-detail.tsx:144`).
- **The API is ahead of the UI.** `task.create` accepts `baseRef`, `branch` and `prNumber`; the New Task dialog sends none of them
  (`crud-dialogs.tsx:308-312`).
- **No event for any of it.** ADR-0009 §Consequences deliberately left git/PR state without a topic, so every surface polls or goes stale.
- **GitHub traffic is unbudgeted.** `runGH` and `runGit` run on `context.Background()` with no timeout except the `baseRef` fetch
  (`task_ref.go:15`); only provenance sets `GIT_TERMINAL_PROMPT=0` and `core.fsmonitor=false`.

Reference points (local research, appendices B and C): Paseo has the interaction model (a state-driven action ladder with sentence-long
disabled reasons, one poll cadence per repo, PR association without stored ids, an untrusted-fork gate) but makes blind commits and blind
PRs. ZCode has the visual language (quiet `+N −N` tokens, branch pill, warning banners). Codex and Claude Code show the approval posture
(read-only git is free; push, PR and force always ask). We port Paseo's interaction and ZCode's look, and we keep smind's previewed,
human-authored actions.

A live read-only check on 2026-10-22 shows the batching this ADR relies on works: one `gh pr list --repo spacingmind/smind --state open
--limit 100 --json number,url,title,state,isDraft,headRefName,headRefOid,baseRefName,isCrossRepository,headRepositoryOwner,mergeable,reviewDecision,statusCheckRollup`
returned state, review decision, mergeability and per-check results for every open PR in one call (~1 s, 5.5 KB for four PRs).

## Decisions made by the Human

Recorded as given (2026-10-22):

- **H1/H2.** Write this ADR, then implement in the order **G1 → G2 + G5 (one poller, one topic, one status payload) → G3 + G4**.
- **H3 (branch cleanup on archive).** Delete `smind/task-*` branches **only when merged**. Unmerged branches are kept, with a risk warning in
  the archive dialog. Archived tasks get **Restore**. The archive/merge UI is P1 item G8; this ADR defines the policy and the data it needs
  ([§9](#9-archive-and-branch-cleanup-policy-h3)); implementation comes in G8.
- **H4 (GitHub auth on a remote daemon).** The user **pastes a token, sent over the E2EE channel**, and the daemon runs `gh auth login
  --with-token`. The token never appears in logs, events, argv or errors; it goes on stdin. Device flow with a dedicated OAuth app comes
  later. P1 item G12. **This ADR defines the surface and the security rules ([§8](#8-github-auth-surface-h4)); the implementation is its own
  PR after G3/G4.** A device-flow ADR comes only if device flow is built.
- **Forge neutrality.** ADR-0038 is being amended to the forge-neutral kinds `pr`/`issue` plus a `forge` field. This ADR uses the same
  vocabulary: `forge: "github"` on the wire and in storage, and a provider interface with GitHub as the only v1 implementation.
- **Mobile git** is read-only and later (P2). Not in scope; the `task.git` payload is plain JSON with no web-only fields so mobile can
  consume it unchanged.

## Decision

### 1. Forge neutrality: one interface, one client

A small interface in a new `internal/forge` package:

```go
type Provider interface {
    Name() string                                   // "github"
    Status(ctx, host string) (AuthStatus, error)    // cli installed, host, login, scopes
    OpenPRs(ctx, dir string) ([]PR, error)          // one call: all open PRs of dir's repo (§4)
    PR(ctx, dir string, number int) (PR, error)     // one PR, any state
    CreatePR(ctx, dir string, in CreatePRInput) (PR, error)
    // Search / Get (issues, PR attachments) are ADR-0038's, same implementation.
}
```

`forge.PR`, `forge.AuthStatus` and `forge.Error` (with the codes in [§6](#6-error-codes)) are forge-neutral types. **The GitHub
implementation is ADR-0038's `internal/ghctx` client (PR #459), extended; there is no second `gh` client.** `ghctx.Client` gains
`OpenPRs`, `PR`, `CreatePR` and `Status` and keeps its injectable argv-only runner, its single-flight and its typed errors. `internal/forge`
holds only types and the interface; poller, preview and handlers depend on `forge.Provider`, never on `ghctx` or the string `"gh"`.

Wire and storage say `forge` ("github") and `pr`; copy says "pull request" and "Open on GitHub". A GitLab provider later adds a second
implementation and the word "merge request" in copy, and renames nothing on the wire. The remote is `origin` in v1 (as today:
`pr.go:79`, `task_ref.go:166`); the forge and repo are derived from `origin`'s URL by the provider, never typed by a client.

**Dependency:** this ADR's forge work starts after PR #459 merges (DEPENDENCY_REQUEST to the ADR-0038 Lead, see
[Coordination](#coordination-with-the-other-active-leads)).

### 2. Schema (G1, G2; breaking, no shims)

`internal/store/schema.sql` plus additive `addColumnIfMissing` migrations in `internal/store/migrate.go` (like `run_origin`):

**`tasks` gains**

| Column | Type | Meaning |
|---|---|---|
| `base_ref` | `TEXT` | The branch the task is based on and its PR targets, as a branch name (`develop`), not `origin/develop`. Compared as `origin/<base_ref>` after a fetch; if `origin` lacks it, the local branch. Empty = unknown. |
| `base_sha` | `TEXT` | The commit the task branch forked from (the diff base), 40 hex. Written once at creation. |
| `branch_owned` | `INTEGER NOT NULL DEFAULT 0` | 1 when `CreateTask` created the branch (default mode, `baseRef`, and the PR-start `smind/pr-<n>-<slug>` branch); 0 when the user's existing branch was checked out. Branch cleanup ([§9](#9-archive-and-branch-cleanup-policy-h3)) acts only on owned branches. A name test is not enough: a user branch can be named `smind/task-x`. |
| `pr_forge` | `TEXT` | `"github"` or empty. |
| `pr_repo` | `TEXT` | `owner/name` of the repository the PR lives in (the base repo). |
| `pr_number` | `INTEGER` | The task's own PR. Empty = none known. **Identity, not context**: set only by Create PR, by Start-from-PR, or by adoption ([§4](#4-the-poller-the-call-budget-and-github-rate-limits)). An ADR-0038 `pr` *attachment* is context and never sets this. |
| `pr_url` | `TEXT` | Validated `https` URL on the forge host. |
| `pr_head_ref` | `TEXT` | The branch Update PR pushes to: the task branch for a PR smind created, the PR's `headRefName` for a Start-from-PR task, `smind/pr-task-<id>` for the cherry-pick path. |
| `untrusted_source` | `INTEGER NOT NULL DEFAULT 0` | 1 for a task started from a fork PR ([§7](#7-start-from-branch-pr-or-issue-g4-and-the-fork-gate)). Only the user clears it. |

**`workspaces` gains** `default_base TEXT` (empty = auto-detect).

**New table `task_pr_state`** (the persisted poll ledger and last snapshot, like ADR-0028's `quota_poll_state`, so a restart neither forgets
what it knew nor bursts calls): `task_id INTEGER PRIMARY KEY`, `snapshot TEXT` (last `PR` JSON), `fetched_at`, `next_allowed_at`,
`consecutive_failures`, `last_error_code`, `forced_at` (JSON, newest 12 explicit-refresh timestamps). The per-host hourly call ledger is a
second tiny table `forge_budget(host, window_start, calls)`. Local git status (dirty, ahead/behind, conflicts) is cheap and derived; it is
**memory only**, never persisted.

**Default base detection** (`workspace.defaultBase`, written when `default_base` is empty; overridden by `workspace.setDefaultBase`):

1. `git symbolic-ref refs/remotes/origin/HEAD` → `origin/<x>` → `x`.
2. `develop` if `origin/develop` or local `develop` exists.
3. `main`, then `master`, by the same test.
4. The workspace checkout's current branch.

The forge's `defaultBranchRef` (`gh repo view`) is consulted only on an explicit "Detect" press, cached 1 h, never on a hot path. (The
research's order put the forge query second; a network call in the task-creation path is what this ADR avoids.)

**Per-task base, by start mode** (all written in `CreateTask`, before the worktree exists; failures leave no row):

| Mode | `base_ref` | `base_sha` |
|---|---|---|
| default (workspace HEAD) | workspace default base | `git merge-base HEAD origin/<base>` (after a bounded fetch); if none, `HEAD`'s sha |
| `baseRef` | that ref, remote prefix stripped | the resolved start-point sha |
| `branch` (existing) | workspace default base | `git merge-base <branch> origin/<base>`; empty if unrelated |
| `prNumber` | the PR's `baseRefName` (forge `PR`, best effort); else workspace default base | `git merge-base FETCH_HEAD origin/<base>` |

**Migration of existing tasks** (once, at startup, best effort, in a transaction per task): for a task with a worktree and an empty
`base_sha`, compute the reflog base one last time and write it; set `base_ref` to the workspace's detected default; set `branch_owned` to 1
when the branch name starts `smind/task-` or `smind/pr-` (the only heuristic use of the name, and only at migration). **The reflog code path
is then deleted** (`taskDiffBase` reads `base_sha`). A task whose base cannot be recovered gets `base_unknown` from diff/PR actions with a
sentence pointing at **`task.setBase {taskId, baseRef}`** (computes the merge-base and stores both fields; also the entry point for a later
"Change base").

Breaking, noted in the release notes (no installed base): the `develop` constant, `taskDiffBase`'s reflog path and `task.stats` are removed;
`store.Task` marshals new fields (clients ignore unknown keys).

### 3. Wire: RPCs, the `task.git` payload and one topic (G2 + G5)

One status payload per task, one read, one event. **Reads are cache-first and never call the forge.**

**Reads**

- `task.git {taskId}` → `TaskGit` (below).
- `task.gitList {workspaceId}` → `{items: TaskGit[]}`: the sidebar's one call per workspace. It **replaces `task.stats`** (removed; its
  `branch`/`filesChanged`/`insertions`/`deletions` become `TaskGit.diff`). A task that cannot compute a field omits that field; absent is
  never zero (the rule `taskStatsResult` already states).
- `task.gitFocus {taskId|null}` → `{}`: the connection declares which task it is looking at (one per connection, dropped on disconnect).
  It is the interest signal for the poller ([§4](#4-the-poller-the-call-budget-and-github-rate-limits)).
- `task.gitRefresh {taskId}` → `TaskGit`: an explicit refresh; the only read that may spend a forge call, under the explicit-refresh floor
  and budget. Refuses run-scoped (agent) callers.
- `workspace.defaultBase {workspaceId}` / `workspace.setDefaultBase {workspaceId, branch}`; `task.setBase {taskId, baseRef}`.

**`TaskGit`** (JSON, camelCase; every nested object optional; an absent object means "not known")

```json
{
  "taskId": 12, "forge": "github",
  "branch": { "name": "smind/task-x", "owned": true },
  "base":   { "ref": "develop", "sha": "9f3c…", "tip": "a1b2…", "known": true },
  "diff":   { "files": 3, "insertions": 22, "deletions": 2 },
  "worktree": { "dirty": true, "staged": 1, "unstaged": 2, "untracked": 1 },
  "commits":  { "aheadOfBase": 3, "behindBase": 5 },
  "origin":   { "upstream": "origin/smind/task-x", "published": true, "ahead": 1, "behind": 0 },
  "conflict": { "state": "none", "files": [], "against": "origin/develop", "checkedAt": "2026-10-22T09:00:00Z" },
  "merged":   { "state": "unmerged", "via": "" },
  "pr": {
    "number": 123, "repo": "spacingmind/smind", "url": "https://github.com/spacingmind/smind/pull/123",
    "title": "…", "state": "open", "draft": false,
    "headRef": "smind/task-x", "headSha": "a1b2…", "baseRef": "develop", "fromFork": false,
    "mergeable": "mergeable",
    "reviewDecision": "review_required",
    "checks": { "state": "pending", "passed": 5, "failed": 0, "pending": 1, "total": 6 },
    "fetchedAt": "2026-10-22T09:00:00Z", "stale": "", "refreshAvailableAt": ""
  },
  "actions": {
    "createPr": { "enabled": false, "code": "pr_exists",   "reason": "A pull request is already open for this branch." },
    "updatePr": { "enabled": true }
  },
  "untrusted": false,
  "computedAt": "2026-10-22T09:00:01Z"
}
```

Enumerations: `conflict.state` `none | conflicts | unknown`; `merged.state` `merged | unmerged | unknown`, `via` `ancestor | pr`; `pr.state`
`open | closed | merged`; `pr.mergeable` `mergeable | conflicting | unknown`; `pr.reviewDecision` `approved | changes_requested |
review_required | none`; `pr.checks.state` `success | failure | pending | none`; `pr.stale` `"" | "budget" | "backoff" | "rate_limited" |
"unauthenticated" | "unavailable"` (why the numbers are not fresh; the age is `fetchedAt`).

**`actions` is computed on the daemon**, a pure function of the payload (Paseo's `policy.ts`, ported to Go) and shipped with it, so web and
(later) mobile share one policy and one set of sentences instead of two ports that drift. Each entry is `{enabled, code?, reason?}`; when
disabled, `reason` is a sentence that says why and, where there is one, the next step ("Create PR isn't available yet because this branch
has no commits beyond `develop`. Commit your changes first."). This ADR defines `createPr` and `updatePr`; G7/G8 add `push`,
`updateFromBase`, `mergeLocal` and `archive` as new keys, not a wire change. Actions are advisory for UI state; the RPC re-checks the same
preconditions (a stale payload can never make an action run).

**Event.** New topic **`task.git`**, registered in `knownTopics`, payload `{taskId, git: TaskGit}`: the full snapshot, like ADR-0009's
lifecycle payloads. It is published **only when the payload changes**, comparing canonical JSON with `computedAt`, `pr.fetchedAt` and
`pr.refreshAvailableAt` excluded, and coalesced to at most one event per task per second. No `task.git` event on archive or delete
(`task.archived`/`task.deleted` already carry that). The poller recomputes and publishes on: run end (the existing signal), `task.commit`,
`task.stage`, `task.createPr`/`task.updatePr`, `task.setBase`, a forge poll result, a fetch result, and the focused-task local tick.

**Client (ADR-0032).** `useTaskGit(taskId)` and `useWorkspaceTaskGit(workspaceId)` are TanStack Query hooks with keys in
`lib/query-keys.ts`, `staleTime: Infinity`. `useDaemonEvents` is the only subscriber: a `task.git` event `setQueryData`s the task's entry and
patches the workspace list; a reconnect invalidates both (the existing resync). `useTaskGit` calls `task.gitFocus` for the task on screen.
`use-task-stats.ts`'s hand-rolled loading is deleted. Components never call `task.git` themselves.

### 4. The poller, the call budget and GitHub rate limits

One daemon component, `internal/gitstate`, owns every periodic git or forge call. It follows ADR-0028's hard rules (and the usage-endpoint
lesson): **daemon call budget, single-flight, cache-first reads, count-asserting tests.**

**Interest.** Nothing is polled for a task unless it is *watched*: at least one `/ws` connection is subscribed to `task.git`, the task is
unarchived and has a worktree. **Zero subscribers means zero forge calls and zero fetches.** A watched task is *focused* when any connection
set it with `task.gitFocus` or it has a run in progress.

**Cadence** (Paseo's, `workspace-git-service.ts`):

| Class | Interval |
|---|---|
| focused task, PR checks pending | **20 s** |
| focused task, otherwise | **120 s** |
| watched, not focused, PR open | **300 s** |
| PR merged or closed | none: one final fetch on the transition, then explicit refresh only; a head-SHA change from a push re-arms one fetch |
| no PR stored | no own cadence (adoption, below) |
| local status (dirty, ahead/behind, conflicts) of the focused task | 30 s tick, **local git only** (no network) |
| `git fetch origin <base>` per workspace | 180 s while any task of it is watched; floor 60 s |

**One forge call per repo per tick, not one per task.** The poller groups watched tasks by `(forge, repo)` and issues **one**
`Provider.OpenPRs` call (`gh pr list --state open --limit 100 --json …`, the field list above) that serves every task of that repo; the repo's
interval is the shortest interval among its tasks. A task whose stored PR is absent from the open list (it closed or merged) costs exactly
one `Provider.PR` call to learn the final state, then becomes terminal. Result: N tasks in one repo cost 1 call per tick however many
clients are connected.

**Hard rules** (all enforced inside `gitstate`, below the RPC layer, so no client, MCP tool or script can bypass them):

1. **Cache-first.** `task.git`, `task.gitList` and every event read the in-memory/persisted snapshot. They never spawn `gh`. Only the poller
   and `task.gitRefresh` do.
2. **Single-flight** per `(host, repo)` for list calls and per task for view calls: concurrent callers join the in-flight call and spend no
   budget.
3. **Floor and budget.** A per-repo floor of **15 s** between forge calls; a per-host budget of **240 forge calls per rolling hour** (the
   `forge_budget` row). An explicit `task.gitRefresh` bypasses the interval but not the floor, and has its own floor of **5 s** and **12 per
   hour per task**. A call over the floor or budget is **not queued and not retried**: the cache is served with `pr.stale = "budget"` and
   `pr.refreshAvailableAt` set, and the UI says so in a sentence. MCP and run-scoped callers cannot refresh at all.
4. **Backoff.** After a failure the task's next interval doubles from its base: 30 → 60 → 120 → **300 s cap**; a success resets it.
   `forge_rate_limited` (HTTP 403/429 or "rate limit" in `gh`'s error) sets `next_allowed_at` ≥ **10 min** (or the reset time when `gh`
   reports one) and `pr.stale = "rate_limited"`. `forge_unauthenticated` **suspends polling for the host** until `forge.status` sees it
   authenticated again (re-probed at most every 10 min, and immediately after `forge.setToken`); no repeated failing calls.
5. **Restart-safe.** `next_allowed_at`, the failure count and the hourly ledger are persisted; start-up polling is staggered with ±10 % jitter
   per task and never fires before `next_allowed_at`. A daemon restart loop cannot burst.
6. **Timeouts.** Every forge and network git call has a context deadline (forge 20 s, fetch 20 s, push 60 s, local git 10 s) and kills the
   process on expiry (`runGitOutputEnvCtx`'s `WaitDelay` pattern). `GIT_TERMINAL_PROMPT=0` and `GH_PROMPT_DISABLED=1` are always set.
7. **Count-asserting tests** use a fake runner that counts calls (see the plan's `TestPoller_*`). A change that adds a call per task per tick
   fails them.

**Adoption** (association without a stored id). The open-PR list also tells the poller which open PRs exist for tasks with no stored PR: a
task whose branch (`pr_head_ref` or `branch.name`) equals an open PR's `headRefName` in the same repo, with `isCrossRepository == false`, is
**adopted** (identity written to the task, `task.updated` published). It costs no extra call. Merged or closed PRs of tasks that never had an
identity are not discovered in v1 (Paseo's exact-head-SHA rule needs a call per task; [O6](#o6-find-merged-or-closed-prs-for-tasks-with-no-stored-pr)).

**Local status and the conflict token (G5).** All local git runs through a shared hardened runner: `-c core.fsmonitor=false`,
`--no-optional-locks` for status, `GIT_TERMINAL_PROMPT=0`, a deadline, argv only. This also fixes the known exposure that daemon
diff/stats/commit/archive calls honour an agent-editable `core.fsmonitor` (`orchestration-and-metering.md:1062`); the same runner is used by
the existing calls in the touched files.

- dirty/staged/unstaged/untracked: one `git status --porcelain=v2 -b`.
- `aheadOfBase`/`behindBase`: `git rev-list --left-right --count origin/<base>...<branch>` (local base if no `origin`).
- `origin.ahead/behind/published`: the same count against `origin/<branch>`; `published` is whether that ref exists.
- `diff`: the existing snapshot-index computation, but on `base_sha`.
- `conflict`: only when `behindBase > 0` and `aheadOfBase > 0` (otherwise it is `none` without running anything):
  `git merge-tree --write-tree --name-only --no-messages origin/<base> <branch>` (git ≥ 2.38; exit 1 = conflicts, the file list follows the
  tree id). It needs no checkout. Cached by the pair of tips, so it reruns only when either moves. Under git < 2.38 the state is `unknown`
  with `reason: "git_too_old"`. `merge-tree --write-tree` writes loose objects (no refs) into the repository; this is accepted and left to
  `git gc`.
- `merged`: `ancestor` when `branch` is an ancestor of `origin/<base>` (after the 180 s fetch), `pr` when the task's PR is `merged` and the
  branch tip equals `pr.headSha` (a squash or rebase merge makes the ancestor test false), else `unmerged`; `unknown` when neither side can
  be evaluated.

### 5. Create PR becomes a previewed action; Update PR (G3)

- `task.prPreview {taskId, baseBranch?}` → `{base:{ref,sha}, head:{ref, strategy:"direct"|"cherry-pick", branch}, commits:[{sha,subject}]
  (≤50), uncommitted:{staged,unstaged,untracked}, existingPr?, defaults:{title, body, draft:true}, forge:{name, repo, host}, blockers:[{code,
  reason}]}`. Cache-first for the PR half; local git for the rest.
- `task.createPr {taskId, baseBranch, title, body, draft}` → `{pr: PR}`. `title` ≤ 256 chars, `body` ≤ 64 KiB, no NUL; `baseBranch` empty =
  the task's `base_ref`. The body goes to `gh` through **stdin** (`--body-file -`), never argv. `--draft` per the dialog. On success the
  identity is stored, `task.git` is published and the chip appears; there is no transient "PR opened" paragraph.
- `task.updatePr {taskId}` → `{pr, pushed:{from,to}}`: fast-forward `git push origin <branch>:refs/heads/<pr_head_ref>` (never `--force`);
  refused with `pr_not_open`, `push_rejected` (non-fast-forward: "the branch on GitHub has changes this branch doesn't"), or
  `fork_pr_readonly`.
- **Idempotent.** `task.createPr` for a task that already has an open PR is `pr_exists {url, number}` and adopts it. It never calls `gh pr
  create` twice. This replaces "the only way to update is an error".
- **The default body is a deterministic template, not a model output** (ADR-0006 keeps the daemon from generating commit messages; the same
  line holds for PR text): a `## Summary` bullet list of the commits' subjects, then a footer `Opened from smind task #N.` The title defaults
  to the task title. Both are editable in the dialog; nothing is sent that the user has not seen. ([O7](#o7-pr-body-footer-attribution).)
- **Draft default: on**, remembered per workspace on the client.
- **Name clash fixed:** the cherry-pick path's throwaway branch is `smind/pr-task-<id>` (a PR-start task's branch is `smind/pr-<n>-<slug>`),
  and it is deleted after a failed cherry-pick. The cherry-pick path remains for *creating* a PR when the task's base is not an ancestor of
  `origin/<base>`; the dialog says "smind will open this from a rebased copy (`smind/pr-task-12`)". Update PR on such a PR is disabled with
  a reason in v1 ([O5](#o5-the-cherry-pick-create-path)).
- Preconditions are one function, shared by `actions`, `prPreview` and the RPCs, so the three cannot disagree: no worktree
  (`task_no_worktree`), base unknown (`base_unknown`), a run active in the task's worktree (`run_active`), no commits beyond the base
  (`no_commits`), PR exists, forge CLI missing/unauthenticated/no remote.

### 6. Error codes

Typed codes replace the raw `gh` argv dump. The code travels in `rpcError.code` (the existing `RPCCode()` mechanism, `wsapi.go:54-69`) and
the message is a fixed sentence per code. A scrubbed stderr tail (≤ 300 chars) may ride in `detail` for a "Details" disclosure; **argv is
never included**, and `detail` passes the token scrubber in [§10](#11-security).

| Code | Meaning (message gives the next step) |
|---|---|
| `forge_cli_missing` | `gh` is not on the daemon's PATH |
| `forge_unauthenticated` | not logged in on the daemon host (Settings → GitHub) |
| `forge_no_remote` | `origin` is not a supported forge remote |
| `forge_not_found` | repo, PR or issue not found / no access |
| `forge_rate_limited` | the forge refused; `retryAt` when known |
| `forge_timeout` | the call hit its deadline |
| `forge_error` | anything else, with `detail` |
| `task_no_worktree`, `base_unknown`, `base_not_found`, `run_active` | task state |
| `no_commits` | nothing beyond the base to open a PR for |
| `pr_exists {url, number}`, `pr_not_open`, `no_pr` | PR state |
| `push_rejected`, `push_auth`, `cherry_pick_conflict` | git outcome |
| `fork_pr_readonly` | Update PR on a fork PR (v1) |
| `invalid_title`, `invalid_body`, `invalid_branch` | input |
| `git_too_old` | needs git ≥ 2.38 for conflict detection (status only, never an RPC error) |
| `refresh_limited {retryAt}` | `task.gitRefresh` over its floor/budget |
| `insecure_channel`, `token_invalid`, `token_scopes` | G12, [§8](#8-github-auth-surface-h4) |

ADR-0038's `github_unavailable` and `github_not_found` are the coarse forms of `forge_cli_missing | forge_unauthenticated | forge_no_remote`
and `forge_not_found`. The recommendation ([O1](#o1-one-error-vocabulary-with-adr-0038)) is one `forge_*` vocabulary: #459 is not merged, so
the rename costs a find-and-replace now and a shim later.

### 7. Start from branch, PR or issue (G4) and the fork gate

`task.create` keeps `baseRef | branch | prNumber` (at most one). New: **`workspace.branches {workspaceId, query?, limit≤50}`** →
`{defaultBase, current, branches:[{name, remote:"local"|"origin", behind, ahead}]}` from `git for-each-ref` over local and `refs/remotes/origin/*`;
`behind`/`ahead` are against the default base, computed only for the returned page by one local `rev-list --left-right --count` each. No
network.

New Task dialog: **Start from** = *Workspace HEAD* (default; shows "Branch from `develop`") / *Branch…* / *Pull request…* / *Issue…*, ZCode's
segmented control with Paseo's picker (`new-workspace-picker-item.ts`). The PR and issue pickers are ADR-0038's `github.search` popover
(a pasted PR/issue URL or `#123` resolves one item). Choosing an **issue** sets the title from the issue and attaches it as an `issue`
attachment on the first prompt (client-side; no `issueNumber` daemon param). Choosing a **PR** sends `prNumber` and attaches the `pr`.

**Fork gate.** `CreateTask` with `prNumber` asks the provider for the PR (`Provider.PR`, one call, not polled): `isCrossRepository == true`
sets `untrusted_source`. **Fail closed:** if the provider cannot answer (CLI missing, unauthenticated), the task is created untrusted; only
the user can clear it. While `untrusted_source` is set:

- the **setup script does not run** and `setup_status` is `"blocked"` (the banner says "From fork owner/repo — setup is blocked until you
  run it"); the teardown script is skipped at archive;
- **`task.retrySetup` is the explicit "Run setup"** and clears the flag. `forge` calls for the task continue to work.

This is narrower than Paseo's gate and for a different reason: smind's setup and teardown scripts are stored daemon-side and written by humans
(ADR-0024), so a fork cannot inject a script. The remaining risk is that the *human-authored* script (`npm install`, `make`) executes the
fork's code. The banner also says the PR's own text (`AGENTS.md`, `CLAUDE.md`) reaches the agent as repository content. Update PR is
`fork_pr_readonly` in v1: pushing to a fork needs a push remote ([O4](#o4-fork-pr-update)).

### 8. GitHub auth surface (H4)

Auth is delegated to `gh` and `git`; smind stores no token itself (H4's `gh auth login --with-token` lets `gh` store it). Implemented as
the G12 PR after G3/G4; the contract is fixed here.

- **`forge.status {refresh?}`** → `{provider, cli:{installed, version}, hosts:[{host, authenticated, login, scopes[], protocol, source}]}`
  with `source` `keyring | file | env`. It runs `gh auth status` and is cached 60 s, single-flight, `refresh` under the explicit floor.
  The *client* learns auth only from this result; there is no way to read the token, and `scopes` are names, never values.
  `daemon.info` gains `forge: {provider, cli: bool}` (a `LookPath`, no process spawn; this is ADR-0038 §8's `github: bool`, made an object
  so it can grow). New topic **`forge.status`** (`{status}`), published after a status change, so a second tab updates.
- **`forge.setToken {host?, token}`** → `{status}`: the daemon runs `gh auth login --hostname <host> --with-token --git-protocol https` with
  the token **on stdin and nowhere else**, then re-reads status.
  - `host` defaults to the `origin` host and must match a hostname regex; the token is `[\x21-\x7e]{1,255}`.
  - Allowed only on a **secure channel**: loopback `/ws` (a local or ssh-tunnelled client), a relay-served E2EE session (ADR-0035), or
    `wss`. A plain `ws://` direct connection to a remote host is refused with `insecure_channel`. The transport tags each connection
    (`channel: loopback | tls | e2ee | plain`); this is the one new connection property.
  - Refused for run-scoped callers; MCP has no tool for it (agents must never set credentials).
  - Failure maps to `token_invalid` (gh rejects the token), `token_scopes` (a classic token without `repo`/`read:org`; fine-grained
    tokens are not scope-checked by `gh`), `forge_cli_missing`, `forge_timeout`.
  - **The token never appears** in logs, events, argv, env, `request_log`, `run_events`, error text, `detail`, or the `forge.status`
    result. Handlers on this surface log no params; the sentinel test ([§10](#11-security)) covers every sink.
- **`forge.logout {host}`** runs `gh auth logout --hostname <host>`; same channel rules.
- **Git over https** uses the login without editing the daemon user's global gitconfig: the daemon's push and fetch calls add
  `-c credential.helper= -c credential.helper='!gh auth git-credential'` per invocation, and only when the forge is authenticated and
  `origin` is https. ssh remotes are untouched. ([O9](#o9-git-credential-helper).)
- The Docker images gain `gh` (today `Dockerfile:37-42` has `git` and `openssh-client` only).
- **UI** (G12): Settings → GitHub card, and a callout on every `forge_cli_missing`/`forge_unauthenticated` error, with a "Paste a token"
  field (masked, `autocomplete=off`, cleared on submit and on unmount, never kept in zustand, never in `localStorage`); "Run `gh auth login`
  on the daemon host" for a local daemon. Device flow is out of this ADR.

### 9. Archive and branch-cleanup policy (H3)

Policy only; G8 implements the dialog and the RPCs.

- **Delete a branch on archive only when all hold:** `branch_owned = 1`, the task is **merged** (`TaskGit.merged.state == "merged"`, [§4](#4-the-poller-the-call-budget-and-github-rate-limits)),
  and the branch is not checked out anywhere else. Never a user-named existing branch, never a branch with commits not in the base or the
  PR. **Remote branches are never deleted by smind** (the forge's "delete branch on merge" does that).
- **Unmerged is kept**, with a risk warning in the archive dialog: uncommitted lines (added/deleted), commits ahead of `origin/<branch>`,
  commits ahead of base, and "this branch is not merged anywhere". The dialog offers an explicit "Delete branch anyway" only for owned
  branches, unchecked.
- **Restore** recreates the worktree from the kept branch (`git worktree add <path> <branch>`); it is disabled with a sentence ("The branch
  was deleted after it was merged") when the branch is gone (`tasks.branch_deleted_at`, added by G8, plus a local `show-ref` check).
- Data this ADR puts in place so G8 needs no schema work beyond `branch_deleted_at`: `branch_owned`, `base_ref`/`base_sha`, `merged`,
  `origin.ahead`, `commits.aheadOfBase`, `worktree.*`, `diff`, and the PR snapshot (`state`, `headSha`).
- The existing checkpoint commit (`smind: checkpoint before archive`) is unchanged; ignored files are still lost, and the dialog says so.

### 10. UI placement

References: ZCode for the look (`DESIGN.md` token discipline: diff colours are never borrowed from success/destructive; status never by
colour alone; `rounded-2xl` is reserved for floating panels; badges `text-ui-xs`), Paseo for the interaction. Every icon comes from
`lib/icons.ts` (the drift test); shared state is the ADR-0032 store; light and dark screenshots, transition frames included, are part of
each UI PR.

- **Task header** (next to the title): the **PR chip**: `#123 · Open · ✓ checks`, `· ✗ 2 failing`, `· Changes requested`, `· Draft`,
  `· Merged`, always text, colour only on failing/blocking states (Claude Code's PR dot palette: merged is the one non-red accent). Click
  = "Open on GitHub" in v1 (the PR panel is G9). Its hover card shows `smind/task-x → develop`, the commit counts and the tokens.
- **Sidebar row**: keeps `+N −M`; adds **one** glyph: PR state (open/draft/merged/closed) and a single dot for "unpublished" or "conflicts"
  (text only in the hover card), so the row stays quiet. Rows with nothing to say show nothing.
- **Hover card** (sidebar and header): the full token line, quiet and mono, ZCode's `GitStatusSection`: `+22 −2 · 3 commits · ↑1 · ↓5
  behind develop · ⚠ conflicts with develop`. Destructive tint on the conflicts token only.
- **Composer status chips**: the PR chip beside the diff-stat chip, plus a `⚠ conflicts` chip. The diff-stat chip now reads `TaskGit.diff`,
  which fixes its staleness at the source (events refresh it).
- **Diff header**: a single `<PrAction>` component renders **Create PR…** (opens the dialog), **Update PR** + **View PR** once a PR exists,
  and the disabled reason as a sentence in the tooltip and under the button on narrow widths.
- **Create PR dialog** (ZCode's commit dialog and push card): a branch row with `+N −M`, a **Base** combobox (the task's `base_ref`), **Title**,
  **Body** (borderless textarea), a **Draft** switch, a summary line "3 commits · 2 files uncommitted (not included)" with "Commit staged
  first", a warning banner for blockers, the typed error in one line with a "Details" disclosure, copyable.
- **New Task dialog**: the Start-from control above; the fork badge; "Branch from `develop`" for the default mode.
- **Settings → workspace**: *Default base branch* (shows the detected value, an override field, "Detect").
- **Mobile** (P2, not in scope): rows get the same glyph and chip from `task.gitList`; the payload has nothing web-only.

### 11. Security

| Concern | Mitigation |
|---|---|
| GitHub token | stdin only; never argv, env, log, event, `request_log`, `run_events`, error, `detail`; secure channel required ([§8](#8-github-auth-surface-h4)); a sentinel test greps every sink |
| Token-shaped text in `detail` | a scrubber redacts `gh[pousr]_…`, `github_pat_…`, `Authorization:` values and any `x-access-token:…@` URL userinfo before text leaves the daemon |
| Shell injection | argv only for `gh` and `git`, never a shell; refs through the existing `validateRefName` and `--end-of-options`; `owner/name` and hostnames regex-validated; the client never names the repo or remote |
| Untrusted PR text (title, body, branch, check names, reviewers) | treated as data: text-only rendering (React escaping, no `dangerouslySetInnerHTML`), titles clipped to 200 chars, never concatenated into a system prompt; PR content reaches an agent only through ADR-0038's delimited `pr` attachment, never through `task.git` |
| Hostile URL in a result | `pr.url` is accepted only as `https` on the forge host (or the origin's host); anything else is dropped before storage and before it becomes an `href` |
| Rate limits / DoS | the budget and floors in [§4](#4-the-poller-the-call-budget-and-github-rate-limits); explicit refresh capped; agents and MCP cannot refresh |
| Agent-driven side effects | `task.createPr`, `task.updatePr`, `task.gitRefresh`, `forge.setToken`, `forge.logout` refuse run-scoped callers (D9 of orchestration-parity, H6 of the research stays open) |
| Fork PR code | `untrusted_source` blocks the setup script and the user decides ([§7](#7-start-from-branch-pr-or-issue-g4-and-the-fork-gate)) |
| Repo-config execution | the shared hardened runner disables `core.fsmonitor`, sets `GIT_TERMINAL_PROMPT=0`, `--no-optional-locks`, deadlines; `merge-tree` runs without a checkout |
| Stale payload driving an action | every RPC re-checks the preconditions the `actions` map encodes |

## Alternatives considered

- **Keep the reflog heuristic and add only `base_ref`.** Leaves existing-branch and PR tasks with an arbitrary diff base and breaks when
  the reflog expires. A stored sha is cheaper than the heuristic and testable.
- **One `gh pr view` per task per tick.** Linear in tasks and clients; the single list call per repo is flat. Rejected on the usage-endpoint
  lesson.
- **Poll every task regardless of clients.** Spends budget on work nobody sees. Rejected: interest-driven, zero subscribers = zero calls.
- **Client-side action policy** (Paseo's `policy.ts`). Two copies (web, mobile) drift, and mobile would need its own sentences. Rejected for
  server-computed `actions`; the client may still derive purely visual state.
- **Several events** (`task.pr`, `task.status`-like, `task.conflict`). More topics, more races between them. Rejected for one snapshot topic.
- **Persist local status.** It changes with every keystroke and is recomputed in milliseconds; persisting it adds a stale-data bug class for
  no gain. Only the PR snapshot and the ledger persist.
- **Paseo's parallel `github_*` and `forge_*` schemas** with long-lived compat. Rejected; one vocabulary now (no installed base).
- **Detect PRs through the PR's head SHA only** (Paseo, for closed/merged). A call per task; deferred (O6).
- **Device flow for H4 now.** Needs smind's own OAuth app; the Human chose paste-a-token first.
- **Edit the daemon user's `~/.gitconfig` with `gh auth setup-git`.** Global, persistent, surprising; a per-invocation helper does the same
  for smind's own git calls.
- **Auto-delete branches on archive.** The Human decided: only when merged, owned, and with a warning otherwise.

## Consequences

- **Breaking, no shims** (no installed base): `task.stats` removed (`task.gitList`); the reflog base and the `develop` constant removed;
  new nullable columns and two small tables; `store.Task` JSON gains fields; `task.createPr` takes `{title, body, draft}`; `task.createPr`
  on an existing PR returns `pr_exists` instead of surfacing `gh`'s error. All noted in the release notes.
- ADR-0009's "needs its own topic" is answered by `task.git`; its "task.commit and task.createPr publish nothing" consequence no longer
  holds. A one-line note is added to ADR-0009 when this is accepted.
- ADR-0024 is amended by G8 (archive may delete merged owned branches; Restore). Nothing in this implementation changes archive behaviour.
- ADR-0038 is amended on its own track (forge-neutral kinds; error vocabulary, [O1](#o1-one-error-vocabulary-with-adr-0038)).
- The daemon gains a long-lived component with persisted state and a rate budget. It is the main risk and gets the count-asserting tests.
- Existing daemon git calls move to the hardened runner in the files the work touches. A repo with a hostile `core.fsmonitor` no longer
  runs it from these paths.
- `gh` becomes a documented runtime dependency for the PR features; without it everything local still works and the UI says so.
- Mobile can render the payload unchanged later; nothing in this ADR touches `mobile/`.

## Coordination with the other active Leads

- **G6 (diff-pane fixes):** owns `diff-viewer-pane`, review submit and the diff header layout. This work touches the Diff pane in **one**
  place: it replaces the Create PR button with a `<PrAction taskId>` component in its own file, mounted in the header slot. We rebase that
  one-line mount after G6 merges (G3 is last in the order). We do not touch `review-*`, the gutter/overflow CSS or submit routing. The
  composer's stale diff chip is ours (it reads `TaskGit.diff`); the G6 Lead should not fix it separately.
- **ADR-0038 (attachments):** we reuse `internal/ghctx` and add methods after #459 merges; we reuse `github.search` for the pickers. We ask
  the ADR-0038 Lead for (a) the forge-neutral kinds, (b) the one `forge_*` error vocabulary ([O1](#o1-one-error-vocabulary-with-adr-0038)),
  and (c) `daemon.info.forge` as an object (`github: bool` stays as a derived alias until their PR lands). An attached PR is context; a task's
  `pr_number` is identity, and the UI draws them differently (attachment pill vs status chip).
- **Mobile M5:** untouched. The `task.git` payload is documented so mobile can adopt it in P2.

## Open questions

Each has a recommendation; the Supervisor can accept all of them with one "ok theo đề xuất".

### O1. One error vocabulary with ADR-0038
ADR-0038/#459 has `github_unavailable` and `github_not_found`; this ADR needs finer, forge-neutral codes. **Recommendation:** one `forge_*`
set ([§6](#6-error-codes)); ADR-0038's two codes become aliases in its amendment, and #459's `ghctx` returns `forge.Error` before it merges.

### O2. Draft by default
**Recommendation:** draft **on**, remembered per workspace. A non-draft PR pings reviewers and starts CI the user may not want yet.

### O3. PR text in the sidebar row
**Recommendation:** glyph and one dot only; text lives in the hover card. A per-user "show PR/checks in rows" toggle (Paseo's display
toggles) later, not in v1.

### O4. Fork PR Update
v1 makes Update PR `fork_pr_readonly` for fork PRs (Paseo adds a `paseo-pr-N` push remote). **Recommendation:** keep it read-only until
someone asks; the push-remote design needs the fork's permission model.

### O5. The cherry-pick Create path
Needed when the task's base is not an ancestor of `origin/<base>` (the workspace checkout was ahead of base). **Recommendation:** keep it for
*create* only, with the dialog explaining it; Update PR disabled on such PRs. Revisit if G1's `Branch from` default (always `origin/<base>`)
makes this path rare.

### O6. Find merged or closed PRs for tasks with no stored PR
**Recommendation:** not in v1. Adoption covers open PRs for free; the closed/merged case needs a call per task (head-SHA match) and only
matters for tasks that predate this work. A manual "Link PR…" (number or URL) is the fallback if it matters.

### O7. PR body footer attribution
The default body ends `Opened from smind task #N.` (today's text is `… via task.createPr.`). **Recommendation:** keep a short footer,
editable in the dialog; a per-workspace setting to drop it can come with the settings card.

### O8. The poll set when no client is focused
Watched-but-not-focused PR tasks poll every 300 s. **Recommendation:** accept; it keeps the sidebar PR glyph truthful at one list call per
repo per 5 min.

### O9. Git credential helper
Per-invocation `-c credential.helper='!gh auth git-credential'` versus `gh auth setup-git`. **Recommendation:** per-invocation, only for
https origins and only when authenticated; revisit if agents' own `git push` should work from the token too (G13).

### O10. Is `statusCheckRollup` in the list call fast enough on big repos
Measured fast for `spacingmind/smind` (4 PRs, ~1 s). **Recommendation:** ship the single list call; if a Peer's measurement on a repo with
many open PRs shows timeouts, fall back to a list without the rollup plus one `pr view --json statusCheckRollup` per *focused* PR task, and
say so in the plan's Progress. The budget rules do not change.
