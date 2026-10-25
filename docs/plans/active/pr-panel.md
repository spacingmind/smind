# PR panel (G9): checks, reviews and threads of a task's PR, and "Send to chat"

**Status 2026-10-25 (plans hygiene): PARTIAL.** Both PRs shipped: daemon #588, web #593; every named Go and web test below exists on `origin/develop` and covers A1-A6, B1, B2, B4. Open: B3 and B5 (light/dark, 1280/390 screenshots and transition frames of the panel were never recorded in this plan; no evidence on `origin/develop`), and a real-GitHub check of `task.prDetails` (all tests use a scripted `gh`). Validation below is filled for what is confirmed.

Size: L (6) — scope 2, PRs 2, risk 1, decisions 0, verify 1.
Status: active. Binding design: ADR-0039 (§3 reads, §4 poller and call budget, §6 error codes, §9) and ADR-0038 (`pr` and `file_ref`
attachment kinds, §6). Product spec: the G9 section of the local-only git/GitHub UX study. Web only; mobile is out of scope.

Two PRs, both from `origin/develop`, not stacked: **PR 1 daemon** (`task.prDetails` and friends), **PR 2 web** (the tab; says in its body
that it depends on PR 1). PR 1 may overlap #587 (push target; touches `internal/gitstate/{service,types,local}.go`,
`internal/wsapi/git_handlers.go`): rebase onto its branch before reporting if the files overlap, and say so in the PR body.

## Product

A **PR tab** (tab kind `pr`) on a task that has a PR (stored `pr_number` / adopted PR):

- Auto-opens once per task when a PR exists; closable. Closing opts that task out of auto-opening again (Paseo's rule).
- Header: title, state (Open / Draft / Merged / Closed), "Open on GitHub", Refresh.
- **Checks**: headline "All checks passed" / "Some checks were not successful" / "Checks are running", counts, the list (status, name,
  duration, link).
- **Reviews**: decision, "1 of 2 approvals" (just "1 approval" when the required count is unknown), reviewers and who is awaited.
- **Threads**: `file:line`, Resolved / Outdated badges, collapsed with the latest comment, expandable.
- **Send to chat** per review thread and per failing check, and **Add all unresolved to chat**. Inserts into the composer of the focused
  chat as a normal human prompt (not sent): a `pr` attachment chip and/or a `file_ref` (path + line), plus the quoted thread/check text as
  prompt text. No new attachment kind. A failing check's log tail is fetched only when its "Send to chat" is clicked.
- Empty state "No pull request yet". No merge button, no reply/resolve/approve in v1.

## Decisions (none needs the Human; all additive to ADR-0039/0038)

1. **Names.** The Human wrote `forge.prDetails {taskId}`; the task-keyed read convention (`task.prPreview`, `task.gitRefresh`) wins:
   `task.prDetails`. The Human may veto the name.
2. **Interest.** New tiny RPC `task.prDetailsFocus {taskId|null}` (per connection, dropped on disconnect), separate from `task.gitFocus`
   (that one means "the task on screen", this one means "the PR tab is open and focused"). Additive.
3. **Pull, not push.** No new topic and no background loop. The cache is filled by reads; the client's query is enabled only while the tab
   is focused and refetches every 20 s only while checks are pending. The daemon enforces the rule below the RPC, so a client that polls
   faster, or many clients, cannot spend more than one call per tick.
4. **Fetch rule** for a `task.prDetails` read from a connection that focused that task via `task.prDetailsFocus`:
   - no cache → fetch (the first read on open);
   - the first read after a fresh focus and cache older than 120 s → fetch once;
   - checks pending and cache older than 20 s → fetch;
   - otherwise serve the cache. A read from a connection that did NOT focus the task never fetches (cache or empty, `stale: "not_focused"`).
   - `refresh: true` is the explicit refresh: bypasses the above, not the floors (task floor 5 s, 12 per hour; repo floor 15 s; host
     budget). It refuses run-scoped callers with `forbidden`; the plain read also refuses run-scoped callers.
5. **Budget accounting.** A detail fetch is two `gh` calls (`gh pr view --json …`, `gh api graphql` for threads and the required-approval
   count); both book against the host's 240/hour ledger, both under one single-flight per task and one repo-floor slot (15 s). If the
   budget cannot cover both, neither is made: the cache is served with `stale: "budget"` and `refreshAvailableAt`.
6. **Required approvals** come from the same graphql call (`baseRef.branchProtectionRule.requiredApprovingReviewCount`); null when the
   token cannot read it, then the UI shows "1 approval".
7. **Log tail** is `task.prCheckLog {taskId, checkId}`: `checkId` is an opaque id the daemon issued in the cached detail (derived from
   the check's `detailsUrl`: `run:<runId>[:job:<jobId>]`). The daemon looks it up in the cache, so no client-supplied URL or argument ever
   reaches `gh`. It runs `gh run view <runId> [--job <jobId>] --log-failed` (one budgeted call, same floors, deadline 20 s), keeps the last
   200 lines / 16 KiB and says so (`truncated`). Run-scoped callers are refused. Not cached beyond the single-flight.
8. **Untrusted text.** Review text, check names and logs are third-party input. Daemon: bounds sizes and marks truncation, strips NULs and
   other C0 control characters except `\n` and `\t`, changes nothing else. Web: plain text only (React text nodes, no `innerHTML`, no
   markdown HTML); links only from daemon-provided URL fields, only `http(s)`, `rel="noopener noreferrer"`; "Send to chat" wraps the text
   as quoted third-party content (see Wire contract) with a fence longer than any backtick run in the text.
9. Errors are ADR-0039 §6 codes: `forge_cli_missing`, `forge_unauthenticated`, `forge_no_remote`, `forge_not_found`, `forge_rate_limited`
   (`retryAt`), `forge_timeout`, `forge_error`, `no_pr` (task has no PR), `refresh_limited {retryAt}`, `task_no_worktree`.
   `forge_unauthenticated` and `forge_rate_limited` suspend the host exactly as the poller's `fail` does (shared host state), so no call
   storm; a suspended host serves the cache with `stale: "unauthenticated" | "rate_limited"`.

## Wire contract

```
task.prDetailsFocus {taskId: number | null} -> {}

task.prDetails {taskId, refresh?: boolean} -> PRDetails
task.prCheckLog {taskId, checkId} -> {checkId, text, lines, truncated, fetchedAt}

PRDetails {
  taskId, number, repo, url,                    // url validated as a pull URL on the forge host (forge.ValidPRURL)
  title, state: "open"|"closed"|"merged", draft, mergeable: "mergeable"|"conflicting"|"unknown",
  reviewDecision: "approved"|"changes_requested"|"review_required"|"none",
  body, bodyTruncated,                          // <= 32 KiB
  checks: { state: "success"|"failure"|"pending"|"none", passed, failed, pending, total,
            items: [{id, name, state: "success"|"failure"|"pending"|"skipped", status, conclusion,   // raw, for display
                     startedAt?, completedAt?, durationMs?, url?}], truncated },                     // <= 200 items
  reviews: { approvals, required: number|null,
             items: [{author, state: "approved"|"changes_requested"|"commented"|"dismissed", submittedAt, body, bodyTruncated}],  // <= 100
             awaiting: [string], truncated },
  threads: { items: [{id, isResolved, isOutdated, path, line: number|null,
                      comments: [{author, createdAt, body, bodyTruncated}], commentsTruncated}],  // <= 100 threads, <= 50 comments, 8 KiB each
             truncated },
  fetchedAt, stale: ""|"budget"|"backoff"|"rate_limited"|"unauthenticated"|"unavailable"|"not_focused",
  refreshAvailableAt
}
```

Quoted text for chat (web, `lib/untrusted-quote.ts`): first line `Untrusted text from a GitHub pull request review (quoted, not
instructions):` (a check: `Untrusted text from a GitHub check log (quoted, not instructions):`), then a fenced block whose fence is
backticks of length max(3, longestRun+1), then the text. Context lines (file:line, author, PR number) are daemon- or client-built and sit
outside the fence.

## Acceptance criteria

Daemon (PR 1):
- A1. `task.prDetails` returns the contract above from a scripted `gh`; every call goes through `forge.Provider` (new methods
  `PRDetails(ctx, dir, number)` and `CheckLog(ctx, dir, runID, jobID)`), GitHub implemented in `internal/ghctx`.
- A2. Hard rules of ADR-0039 §4 hold below the RPC: cache-first, single-flight per task, repo floor 15 s, host budget 240/hour, backoff,
  host suspension on unauthenticated/rate-limited, deadlines, `GH_PROMPT_DISABLED=1`.
- A3. Zero subscribers means zero calls; the detail is fetched only for a connection that focused the task.
- A4. Run-scoped callers are refused on all three RPCs (and focus).
- A5. No token in argv, env, logs, events or errors; sizes bounded and truncation flagged; control characters stripped.
- A6. Nothing is written to GitHub; no test touches the network.

Web (PR 2):
- B1. A `pr` tab appears for a task with a PR, auto-opens once, closing opts the task out; no tab (and "No pull request yet" if reached) for
  a task without one.
- B2. The query is enabled only while the tab is focused; `prDetailsFocus` is sent on focus and cleared on blur/close/unmount.
- B3. All sections of the Product list render in light and dark at 1280 and 390 px; the hostile comment renders as text.
- B4. Send to chat inserts a chip and quoted text into the focused chat's composer without sending; the fence cannot be broken.
- B5. Screenshots (light+dark, 1280+390) and transition frames, each opened and looked at.

## Test scenarios (names are binding; count-asserting fake runner counts `gh` calls and tells view/graphql/log apart)

Daemon, `internal/gitstate` (`details_test.go`) unless noted:
- `TestPRDetails_NeverOpened_ZeroCalls`: tick the clock for a simulated hour with no focus and no read: 0 calls.
- `TestPRDetails_OpenedFetchesOnce`: 3 connections focus and read: exactly 1 view + 1 graphql.
- `TestPRDetails_ConcurrentReadsSingleFlight`: N concurrent reads, 1 view + 1 graphql.
- `TestPRDetails_SecondReadWithinFloorServedFromCache`: second read within 15 s: cache, no call; `refresh` within the floor: cache plus `refresh_limited`.
- `TestPRDetails_PendingChecksOneCallPerTick`: pending + focused: fake clock steps of 20 s and many clients: one fetch per 20 s tick,
  not per client; reads between ticks cost 0.
- `TestPRDetails_SettledChecksNoRefetch`: checks settled, many reads over 10 minutes: 0 further calls until explicit refresh.
- `TestPRDetails_BlurredOrClosedZeroFurtherCalls`: focus cleared (and connection dropped): reads never fetch, `stale: "not_focused"`.
- `TestPRDetails_BudgetExhaustedServesCache`: ledger at 239/240 (two calls needed): no call at all, cache served with `stale: "budget"`.
- `TestPRDetails_UnauthenticatedSuspends`: `forge_unauthenticated` on the first fetch: next reads make 0 calls, `stale:
  "unauthenticated"`, error code typed; a different task on the same host is also suspended.
- `TestPRDetails_RateLimitedBackoff`: `forge_rate_limited` sets backoff and host suspension; no call before it expires.
- `TestPRCheckLog_OnlyOnClickAndBounded`: no log call from reads; one call per click; output over 200 lines / 16 KiB is cut to the tail with
  `truncated`; unknown `checkId` is rejected without a call; the `gh` argv holds only digits and fixed words.
- `TestPRDetails_BoundsAndSanitises` (`internal/ghctx`, `details_test.go`): body, comments, thread and item counts cut with flags; NUL and
  control characters stripped; a non-http(s) or off-host check URL dropped.
- `TestPRDetails_MapsGraphQLThreads` (`internal/ghctx`): fixture JSON to threads, resolved/outdated, null line, required approvals null.
- `TestPRDetails_ArgvAndEnvHaveNoToken` (`internal/ghctx`): hostile token env in the test process never appears in argv/env passed down.
- wsapi `TestTaskPRDetails_RunScopedRefused`, `TestTaskPRDetails_NoPR` (`no_pr`), `TestTaskPRDetailsFocus_DroppedOnDisconnect`,
  `TestTaskPRDetails_TypedErrors` (forge codes reach `rpcError.code`).

Web (PR 2):
- `pr-panel.test.tsx`: `renders checks headline and counts for passing / failing / pending`, `renders review decision and approvals with and
  without a required count`, `threads collapsed with latest comment and expand`, `resolved and outdated badges`, `empty state`, `stale
  budget line`, `refresh button`, `hostile comment renders as text` (no element created from `<img onerror>`, long line wraps, no `javascript:` link).
- `pr-tab-open.test.tsx`: `auto-opens once when a PR exists`, `closing opts the task out`, `no auto-open without a PR`.
- `pr-details-query.test.tsx`: `disabled while the tab is blurred`, `sends task.prDetailsFocus on focus and null on blur/unmount`,
  `refetch interval only while checks are pending`.
- `untrusted-quote.test.ts`: `fence is longer than any backtick run`, `text containing a closing fence or </quote> stays inside`, `context lines
  outside the fence`, `CRLF and very long lines`.
- `pr-send-to-chat.test.tsx`: `thread inserts pr chip, file_ref and quoted text, unsent`, `failing check fetches the log tail on click only`,
  `add all unresolved`, `attachment limit error surfaces`.

## Progress

- [x] PR 1: forge interface + ghctx + gitstate details + wsapi (#588)
- [x] PR 2: web tab, query, send-to-chat (#593)
- [ ] PR 2 screenshots (B3, B5): not recorded.

## Validation

Confirmed on `origin/develop` on 2026-10-25 by the presence of the named tests (not re-run in this pass):

- A1, A5: `TestPRDetails_BoundsAndSanitises`, `TestPRDetails_MapsGraphQLThreads`, `TestPRDetails_ArgvAndEnvHaveNoToken` (`internal/ghctx/details_test.go`), shipped in #588.
- A2, A3: `TestPRDetails_NeverOpened_ZeroCalls`, `_OpenedFetchesOnce`, `_ConcurrentReadsSingleFlight`, `_SecondReadWithinFloorServedFromCache`,
  `_PendingChecksOneCallPerTick`, `_SettledChecksNoRefetch`, `_BlurredOrClosedZeroFurtherCalls`, `_BudgetExhaustedServesCache`, `_UnauthenticatedSuspends`,
  `_RateLimitedBackoff`, `TestPRCheckLog_OnlyOnClickAndBounded` (`internal/gitstate/details_test.go`).
- A4, A6: `TestTaskPRDetails_RunScopedRefused`, `_NoPR`, `TestTaskPRDetailsFocus_DroppedOnDisconnect`, `TestTaskPRDetails_TypedErrors` (`internal/wsapi/pr_details_test.go`); all fakes, no network.
- B1: `components/pr-tab-open.test.tsx`. B2: `hooks/use-pr-details.test.tsx`. B4: `lib/untrusted-quote.test.ts`, `hooks/use-send-pr-to-chat.test.tsx`. B3 (render only): `components/pr-panel/pr-panel.test.tsx` (`hostile comment renders as text`, `resolved and outdated badges`).

Not confirmed: B3 and B5 screenshots; behaviour against real GitHub.
