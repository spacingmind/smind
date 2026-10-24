# Archive preview finds squash-merged PRs (ADR-0039 O6, narrowed)

Size: S (3) — scope 1, PRs 1, risk 1, decisions 0, verify 0.

## Acceptance criteria

1. `task.archivePreview` and `task.archive {deleteBranch}` for a task with no stored PR identity and a branch not merged by ancestry make
   one bounded `PRsByHead` lookup and report `merged: {state: "merged", via: "pr"}` for a PR merged at exactly the branch tip.
2. `canDeleteBranch` is true for an owned, clean, so-merged branch; the identity is adopted (`pr_*` columns, `task.updated`).
3. A PR with another head sha, a fork PR, a PR for another head ref, or a closed-unmerged PR is never accepted.
4. An open PR for the branch is adopted.
5. No forge, no `gh`, unauthenticated, rate limited, any error: the preview still returns, merged as local git says, and the failure is
   booked like other forge failures (host suspension, task backoff).
6. A run-scoped caller makes no forge call. A task with a stored PR, or merged by ancestry, makes no call. `force` makes no call.
7. Inside the 5 s floor a second preview makes no second call; at most 12 lookups per task per hour; concurrent callers share one call.
8. No poller change, no new RPC, no schema change.

## Test scenarios

- gitstate: `TestPollerLookupByHead_AdoptsMergedPRAtTheBranchTip`, `_IgnoresWhatIsNotThisBranchsPR` (sha moved, fork, other head, closed),
  `_AdoptsAnOpenMatchAndPrefersItOverAMergedOne`, `_TaskWithAStoredPRCostsNoCall`, `_FloorAndHourlyCap`,
  `_FailuresDegradeAndBookTheHost`, `_SingleFlight`.
- ghctx: `TestGhctx_PRsByHead_ArgvAndMapping`, `_RejectsAnOptionLikeBranch`.
- wsapi (real git, local bare origin, squash merge, stub gh): `TestArchivePreviewLookup_SquashMergedBranchWithNoStoredPR`,
  `TestArchiveLookup_DeleteBranchSeesTheSquashMergedPR`, `_BranchAdvancedAfterTheMergeStaysUnmerged`, `_ForkAndOtherHeadAreIgnored`,
  `_OpenPRIsAdopted`, `_ForgeFailuresLeaveTodaysAnswer`, `_RunScopedCallerMakesNoForgeCall`,
  `_StoredPRAndAncestorMergeCostNoExtraCall`, `_SecondPreviewInTheFloorMakesNoSecondCall`.

## Decisions

- Lookup lives in the poller (`LookupByHead`) so it shares the ledger, floor, suspension and backoff; it reuses `ForcedAt` (the
  `task.gitRefresh` floor) rather than a second counter.
- The workspace manager hook (`SetPRLookup`, `LookupMergedPR`) skips the call when local git already says merged.
- For `task.archive` the lookup runs after the base fetch, so a branch merged by ancestry never costs a call.

## Progress

Implemented; see Validation.

## Validation

Recorded in the PR description (checks, mutations, live transcript).
