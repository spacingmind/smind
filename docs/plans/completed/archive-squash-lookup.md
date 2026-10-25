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

Done. Merged in #599 (`feat(archive): look up a squash-merged PR on demand ...`). Plan closed in the 2026-10-25 plans hygiene pass.

## Validation

Recorded in the PR #599 description (checks, mutations, live transcript). AC to test map, confirmed present on `origin/develop` on 2026-10-25:

- AC1, AC2: `TestPollerLookupByHead_AdoptsMergedPRAtTheBranchTip`, `TestArchivePreviewLookup_SquashMergedBranchWithNoStoredPR`,
  `TestArchiveLookup_DeleteBranchSeesTheSquashMergedPR`.
- AC3: `TestPollerLookupByHead_IgnoresWhatIsNotThisBranchsPR`, `TestArchivePreviewLookup_ForkAndOtherHeadAreIgnored`,
  `TestArchivePreviewLookup_BranchAdvancedAfterTheMergeStaysUnmerged`.
- AC4: `TestPollerLookupByHead_AdoptsAnOpenMatchAndPrefersItOverAMergedOne`, `TestArchivePreviewLookup_OpenPRIsAdopted`.
- AC5: `TestPollerLookupByHead_FailuresDegradeAndBookTheHost`, `TestArchivePreviewLookup_ForgeFailuresLeaveTodaysAnswer`.
- AC6: `TestArchivePreviewLookup_RunScopedCallerMakesNoForgeCall`, `TestPollerLookupByHead_TaskWithAStoredPRCostsNoCall`,
  `TestArchivePreviewLookup_StoredPRAndAncestorMergeCostNoExtraCall`.
- AC7: `TestPollerLookupByHead_FloorAndHourlyCap`, `TestPollerLookupByHead_SingleFlight`,
  `TestArchivePreviewLookup_SecondPreviewInTheFloorMakesNoSecondCall`.
- AC8: by construction (no poller, RPC or schema change in #599); `TestGhctx_PRsByHead_ArgvAndMapping` and `_RejectsAnOptionLikeBranch` cover the new `ghctx.PRsByHead`.
