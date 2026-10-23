# Push and Update from base (G7)

Size: L (7) — scope 2, PRs 2, risk 1, decisions 1, verify 1

Two human-only actions on a task's branch: **Push** ("Publish branch" when there is no upstream) and **Update from `<base>`**
(merge `origin/<base>` into the task branch; rebase only as an explicit secondary option). Binding design: ADR-0039 §3–§6, §8, §11.
Research: local-only `git-github-ux.md` G7/G5/G16, Paseo `policy.ts` (action ladder), ZCode `GitActionMenu` push card.

## Decisions (made by the Supervisor; the Human may veto)

- Merge, not rebase, is the default. Rebase is a checkbox-style secondary option in the confirm step, never preselected.
- **No force-push in v1**: no flag, no button, no RPC param. A rebase of already-published commits therefore leaves the branch needing a
  manual `--force-with-lease`; the card warns about it when the branch is published.
- A conflict **never auto-aborts**: the worktree is left in its MERGE/REBASE state, the abort command is named in the sentence, not run.
- Human-only: both RPCs refuse run-scoped callers (`refuseRunScoped`); no MCP tool exists for them.
- `worktree.operation` is **not** added to the wire; "mid-merge/rebase" is an `ActionCtx` input to the pure policy only.
- Mobile is out of scope (git stays read-only there).

## Wire contract (daemon ⇄ web; both Peers build against this)

Both RPCs: params JSON camelCase; refuse run-scoped callers with `forbidden`; one in flight per task (shares `prHost.begin`, busy →
`pr_busy`); `context.WithoutCancel` + a deadline; re-check the `actions` preconditions on a *fresh* snapshot (`svc.Fresh`); after success
publish `task.git` (recompute, same path Update PR uses). Remote is always `origin`; the client never names a remote, ref or refspec.

### `task.push {taskId}` → `{branch, remote, upstreamCreated, pushed}`

```json
{ "branch": "smind/task-x", "remote": "origin", "upstreamCreated": true, "pushed": 3 }
```

- Runs `git push [-u] origin refs/heads/<branch>:refs/heads/<branch>` through the same hardened path as `prPush` (`netGitArgs`, so https uses the
  `gh` credential helper per invocation; no `+`, no `--force*`). `-u` only when the branch has no upstream.
- `pushed` = commits on the branch not on the remote-tracking ref before the push (when no upstream: not on the base compare ref).
- Refused (RPC re-check of `actions.push`): `task_no_worktree`, `forge_no_remote`, `nothing_to_push`, `non_fast_forward` (origin moved).

### `task.updateFromBase {taskId, strategy?}` → `{result, commits}`

```json
{ "result": "merged", "commits": 5 }
```

- `strategy` `"merge"` (default when omitted/empty) or `"rebase"`; anything else → `invalid_strategy`.
- Steps: `git fetch origin <base>` (network deadline, creds as above) when `origin` exists; target = `origin/<base>` else the local base branch;
  count = `rev-list --count HEAD..target`; 0 → `{result:"up_to_date", commits:0}` (this is **advisory** in `actions`, so the RPC fetches and
  answers instead of refusing); else `git merge --no-edit <target>` or `git rebase <target>`. `result` `up_to_date | merged | rebased`;
  `commits` = base commits brought in.
- Refused (RPC re-check of `actions.updateFromBase`): `task_no_worktree`, `base_unknown`, `base_not_found`, `run_active`, `merge_in_progress`,
  `worktree_dirty`.
- **Conflict**: exit with unmerged paths → error code `conflict`, `data: {"files":["a.go","b/c.txt"], "operation":"merge"|"rebase",
  "base":"origin/develop", "filesTruncated":false}` (files sorted, ≤100, same list shape as `TaskGit.conflict.files`), message sentence (merge):
  "Merging `origin/develop` into this branch hit conflicts, and the worktree is now in the middle of the merge. Resolve the conflicts and
  commit, or ask the agent to; `git merge --abort` in the worktree undoes it." (rebase: "…rebasing onto…in the middle of the rebase…
  `git rebase --abort`…"). Nothing is aborted. A merge failing without unmerged paths is `forge_error`-style `git_error` with scrubbed detail.
- Worktree already has `MERGE_HEAD` or `rebase-merge`/`rebase-apply` → `merge_in_progress` `data:{operation}`; sentence tells the human to
  finish or abort it first.

### Error codes (new; `forge.PRError`, one fixed sentence each, scrubbed stderr tail ≤300 chars in `data.detail`, never argv)

| Code | When |
|---|---|
| `non_fast_forward` | push refused because origin has commits this branch lacks ("…Bring those changes in first (pull in the worktree, or ask the agent), then push again.") |
| `push_rejected` | any other rejection (hook, protected branch: `[remote rejected]`) — existing code, existing sentence |
| `forge_unauthenticated` | push auth failure on an https `origin` (sign in on the daemon host); ssh auth failure stays `push_auth` |
| `conflict` | above |
| `merge_in_progress` | above |
| `worktree_dirty` | updateFromBase with staged/unstaged/untracked changes |
| `nothing_to_push` | push with nothing ahead and an upstream that is current, or no commit beyond base when unpublished |
| `invalid_strategy` | bad `strategy` |
| existing | `task_no_worktree`, `base_unknown`, `base_not_found`, `run_active`, `forge_no_remote`, `forge_timeout`, `pr_busy` |

### `actions` entries (added to `gitstate.Actions`, `{enabled, code?, reason?}`)

`push` — first match wins: no branch → `task_no_worktree`; `NoOrigin` → `forge_no_remote`; `Origin` nil (unknown) → disabled `status_unknown`,
reason "Checking this branch's remote state…"; published and `Origin.Behind>0` → `non_fast_forward` ("Push isn't available because origin has
newer changes on this branch. Bring those changes in first (pull in the worktree, or ask the agent)."); published and `Origin.Ahead==0` →
`nothing_to_push` "Nothing to push."; unpublished and `Commits.SinceFork==0` → `nothing_to_push`; else enabled. A local-path origin does NOT
block push (only Create PR).
`updateFromBase` — no branch → `task_no_worktree`; base unknown → `base_unknown`; no `Base.Tip` → `base_not_found`; run active → `run_active`;
mid merge/rebase (`ActionCtx.MidOperation != ""`) → `merge_in_progress`; dirty → `worktree_dirty`; `Commits.BehindBase==0` → `up_to_date`
"Already up to date with `<base>`."; else enabled. Reasons are sentences naming the next step.
`ActionCtx` gains `MidOperation string` (`""|"merge"|"rebase"`), filled by the service from `git rev-parse --git-path MERGE_HEAD|rebase-merge|rebase-apply`.

### Web contract (`web/packages/ui`)

Types in `lib/types.ts`: `TaskPushResult`, `TaskUpdateFromBaseParams {taskId; strategy?: "merge"|"rebase"}`, `TaskUpdateFromBaseResult`,
`TaskGitActions` gains `push?`, `updateFromBase?`; conflict error data `{files; operation; base; filesTruncated}`. Hooks `usePushTask`,
`useUpdateFromBase` in `hooks/use-pr-actions.ts`' neighbour (TanStack `useMutation`; `task.git` events refresh state). One dialog
`components/git/sync-dialog.tsx` (mode `push|update`) with card states idle / in flight / success / error / conflict; items in the task header
"…" menu and `<PrAction>`-style buttons in the Diff header, gated by `actions`.

## Acceptance criteria

1. Publish a branch with no upstream sets the upstream (`upstreamCreated:true`); a later push says `false`.
2. Push with commits ahead pushes exactly them (`pushed` = n); the bare remote has the commits.
3. Nothing to push is refused by `actions.push` and by the RPC (`nothing_to_push`).
4. Origin moved → typed `non_fast_forward`; no force flag exists anywhere in the code path.
5. Update from base, clean: merge commit (or fast-forward) lands, `{merged, n}`; already current → `up_to_date`.
6. Update with conflict leaves `MERGE_HEAD`/the rebase dir, returns `conflict` with the paths and aborts nothing; second call → `merge_in_progress`.
7. `strategy:"rebase"` rebases; the default is merge.
8. `run_active`, dirty worktree, no worktree, unknown base are refused by actions and RPC with the same code.
9. Agent (run-scoped) callers are refused for both RPCs; no MCP tool exists for either.
10. No token/credential/argv in errors, `detail`, logs or events; assert on the recorded command line.
11. `actions.push` / `actions.updateFromBase` correct per state (table test).
12. After success a `task.git` event is published.
13. Web: header menu + Diff header gating with the reason shown; card renders idle / in flight / success / rejected / conflict; Publish vs Push labels;
    rebase option present and not default; "Ask the agent to resolve" sends a human prompt (whenBusy queue, the chat's provider/model/profile).
14. Light + dark screenshots at 1280 and 390 px incl. transition frames, reviewed by the Lead.

## Test scenarios (names)

Go (`internal/gitstate`, `internal/workspace`, `internal/wsapi`; throwaway repos with a LOCAL BARE REMOTE, no network):
`TestActionsPush` (table), `TestActionsUpdateFromBase` (table), `TestPushPublishesBranch`, `TestPushAhead`, `TestPushNothingToPushRefused`,
`TestPushNonFastForward`, `TestUpdateFromBaseMerge`, `TestUpdateFromBaseUpToDate`, `TestUpdateFromBaseConflictLeavesMergeState`,
`TestUpdateFromBaseMergeInProgress`, `TestUpdateFromBaseRebase`, `TestUpdateFromBaseDefaultsToMerge`, `TestUpdateFromBaseRefusals` (run_active,
dirty), `TestSyncRPCsRefuseRunScoped`, `TestNoMCPToolForPushOrUpdate`, `TestSyncNoSecretsInArgvOrErrors`, `TestSyncPublishesTaskGit`.
Web: `sync-dialog.test.tsx` (`renders idle push card`, `publish label without upstream`, `in flight`, `success line`, `rejected inline with details`,
`conflict state lists files and asks the agent`, `rebase option not default`), `sync-menu.test.tsx` (gating, disabled reason).

## Route

Peer 1 (daemon, `peer-sonnet`) and Peer 2 (web, `peer-sonnet`, screenshots) in parallel against this contract; PR 1 daemon, PR 2 web (depends on 1;
merge order daemon first); both branch from `origin/develop`, no stacking.

## Progress

- [ ] spec committed
- [ ] Peer 1 daemon
- [ ] Peer 2 web
- [ ] review / ACCEPT / REJECT
- [ ] PR 1, PR 2, live smoke, screenshots

## Validation

(filled at the end)
