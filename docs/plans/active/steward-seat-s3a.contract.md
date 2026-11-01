---
v: 1
milestone: "Steward seat S3a: inbox review kind, steward.frame read, web review card, permission policy line, read-only Permission policy page (ADR-0047 section 5, 9)"
base: origin/develop
planRef: docs/plans/active/steward-seat.md
acs:
  - id: AC25
    text: "inbox.list/count derive kind review (key review:<reviewId>) from the task's latest non-stale finished review; it replaces that task's verdict row; a stale or open review drops out (the verdict row stays); summary is at most 160 chars and carries only verdict, PR number, head7 and failing gate ids; BLOCKED is high attention, ACCEPTED and CHANGES normal; steward.updated pokes inbox.changed (coalesced); no approve-all or merge method exists."
    covers: { tests: [TestInboxList_DerivesReviewKind, TestInboxList_ReviewReplacesVerdictRow, TestInboxList_StaleReviewDropsOut, TestInboxList_ReviewSummaryRedacted, TestInboxList_ReviewSummaryCarriesPRNumber, TestInboxChanged_CoalescedOnStewardUpdated, TestNoInboxApproveAllMethod], mutations: [M1] }
  - id: AC26
    text: "The permission card shows the matched rule id and why (or 'No rule matched') and the normalised heredoc-elided command instead of the raw command; it has no approve-all, no always-allow, no merge control; policy-allowed or denied requests are not inbox items; policy_allow and policy_deny resolutions have labels and an unknown resolution renders as its text."
    covers: { tests: ["permission card shows matched rule and peek", "permission card says no rule matched", "permission card has no always-allow or approve-all", TestInboxList_PolicyAllowedDeniedNotListed, "unknown permission resolution renders as text"], mutations: [M2] }
  - id: AC27
    text: "Web review card: verdict chip with icon and text, gates failing first, findings grouped by file, evidence frames inline from the read-only steward.frame (digest frames only, png, inside the frames dir, at most 4 MiB, human-only), copy findings, open PR, no merge/approve control; an unknown verdict or gate state renders as text; Settings > Permission policy is read-only (validity banner, rules table, recent audit, on-disk path, no control that writes); polite live regions."
    covers: { tests: ["inbox review row renders verdict chip with icon and text", "review card lists failing gates first", "review card shows frames and copy findings", "review card groups findings by file", "unknown review verdict renders as text", "review card has no merge or approve control", "stale review shows the stale note", "policy settings page is read-only", "policy settings page shows invalid banner", "steward.updated invalidates steward and inbox reads", "steward frame key is outside the steward prefix", TestStewardView_CarriesPRNumberAndFrames, TestStewardView_NoDigestHasEmptyFrames, TestStewardFrame_ServesDigestFrameOnly, TestStewardFrame_RefusesSymlinkEscapeAndOversize, TestStewardFrame_RefusedForRunScoped], mutations: [M3] }
tests:
  - { name: TestInboxList_DerivesReviewKind, run: "go test ./internal/inbox -run '^TestInboxList_DerivesReviewKind$' -count=1 -race", timeoutSec: 600 }
  - { name: TestInboxList_ReviewReplacesVerdictRow, run: "go test ./internal/inbox -run '^TestInboxList_ReviewReplacesVerdictRow$' -count=1 -race", timeoutSec: 600 }
  - { name: TestInboxList_StaleReviewDropsOut, run: "go test ./internal/inbox -run '^TestInboxList_StaleReviewDropsOut$' -count=1 -race", timeoutSec: 600 }
  - { name: TestInboxList_ReviewSummaryRedacted, run: "go test ./internal/inbox -run '^TestInboxList_ReviewSummaryRedacted$' -count=1 -race", timeoutSec: 600 }
  - { name: TestInboxList_ReviewSummaryCarriesPRNumber, run: "go test ./internal/inbox -run '^TestInboxList_ReviewSummaryCarriesPRNumber$' -count=1 -race", timeoutSec: 600 }
  - { name: TestInboxList_PolicyAllowedDeniedNotListed, run: "go test ./internal/inbox -run '^TestInboxList_PolicyAllowedDeniedNotListed$' -count=1 -race", timeoutSec: 600 }
  - { name: TestInboxChanged_CoalescedOnStewardUpdated, run: "go test ./internal/wsapi -run '^TestInboxChanged_CoalescedOnStewardUpdated$' -count=1 -race", timeoutSec: 600 }
  - { name: TestNoInboxApproveAllMethod, run: "go test ./internal/wsapi -run '^TestNoInboxApproveAllMethod$' -count=1 -race", timeoutSec: 600 }
  - { name: TestStewardView_CarriesPRNumberAndFrames, run: "go test ./internal/steward -run '^TestStewardView_CarriesPRNumberAndFrames$' -count=1 -race", timeoutSec: 600 }
  - { name: TestStewardView_NoDigestHasEmptyFrames, run: "go test ./internal/steward -run '^TestStewardView_NoDigestHasEmptyFrames$' -count=1 -race", timeoutSec: 600 }
  - { name: TestStewardFrame_ServesDigestFrameOnly, run: "go test ./internal/wsapi -run '^TestStewardFrame_ServesDigestFrameOnly$' -count=1 -race", timeoutSec: 600 }
  - { name: TestStewardFrame_RefusesSymlinkEscapeAndOversize, run: "go test ./internal/wsapi -run '^TestStewardFrame_RefusesSymlinkEscapeAndOversize$' -count=1 -race", timeoutSec: 600 }
  - { name: TestStewardFrame_RefusedForRunScoped, run: "go test ./internal/wsapi -run '^TestStewardFrame_RefusedForRunScoped$' -count=1 -race", timeoutSec: 600 }
  - { name: "inbox review row renders verdict chip with icon and text", run: "cd web/packages/ui && bunx vitest run src/components/inbox/steward-review-card.test.tsx -t 'inbox review row renders verdict chip with icon and text'", timeoutSec: 900 }
  - { name: "review card lists failing gates first", run: "cd web/packages/ui && bunx vitest run src/components/inbox/steward-review-card.test.tsx -t 'review card lists failing gates first'", timeoutSec: 900 }
  - { name: "review card shows frames and copy findings", run: "cd web/packages/ui && bunx vitest run src/components/inbox/steward-review-card.test.tsx -t 'review card shows frames and copy findings'", timeoutSec: 900 }
  - { name: "review card groups findings by file", run: "cd web/packages/ui && bunx vitest run src/components/inbox/steward-review-card.test.tsx -t 'review card groups findings by file'", timeoutSec: 900 }
  - { name: "unknown review verdict renders as text", run: "cd web/packages/ui && bunx vitest run src/components/inbox/steward-review-card.test.tsx -t 'unknown review verdict renders as text'", timeoutSec: 900 }
  - { name: "review card has no merge or approve control", run: "cd web/packages/ui && bunx vitest run src/components/inbox/steward-review-card.test.tsx -t 'review card has no merge or approve control'", timeoutSec: 900 }
  - { name: "stale review shows the stale note", run: "cd web/packages/ui && bunx vitest run src/components/inbox/steward-review-card.test.tsx -t 'stale review shows the stale note'", timeoutSec: 900 }
  - { name: "permission card shows matched rule and peek", run: "cd web/packages/ui && bunx vitest run src/components/permission/permission-detail.test.tsx -t 'permission card shows matched rule and peek'", timeoutSec: 900 }
  - { name: "permission card says no rule matched", run: "cd web/packages/ui && bunx vitest run src/components/permission/permission-detail.test.tsx -t 'permission card says no rule matched'", timeoutSec: 900 }
  - { name: "permission card has no always-allow or approve-all", run: "cd web/packages/ui && bunx vitest run src/components/permission/permission-card.test.tsx -t 'permission card has no always-allow or approve-all'", timeoutSec: 900 }
  - { name: "policy settings page is read-only", run: "cd web/packages/ui && bunx vitest run src/components/settings/policy-section.test.tsx -t 'policy settings page is read-only'", timeoutSec: 900 }
  - { name: "policy settings page shows invalid banner", run: "cd web/packages/ui && bunx vitest run src/components/settings/policy-section.test.tsx -t 'policy settings page shows invalid banner'", timeoutSec: 900 }
  - { name: "unknown permission resolution renders as text", run: "cd web/packages/ui && bunx vitest run src/components/timeline/permission-reason.test.ts -t 'unknown permission resolution renders as text'", timeoutSec: 900 }
  - { name: "steward.updated invalidates steward and inbox reads", run: "cd web/packages/ui && bunx vitest run src/lib/query/event-bridge.test.ts -t 'steward.updated invalidates steward and inbox reads'", timeoutSec: 900 }
  - { name: "steward frame key is outside the steward prefix", run: "cd web/packages/ui && bunx vitest run src/lib/query-keys.test.ts -t 'steward frame key is outside the steward prefix'", timeoutSec: 900 }
mutations:
  - { id: M1, file: "internal/inbox/inbox.go", change: "keep the verdict row when a review exists (skip the reviewTasks check)", expectFail: [TestInboxList_ReviewReplacesVerdictRow] }
  - { id: M2, file: "web/packages/ui/src/components/permission/permission-detail.tsx", change: "render an 'Always allow' button in PolicyLine", expectFail: ["permission card has no always-allow or approve-all"] }
  - { id: M3, file: "internal/wsapi/steward.go", change: "framePNG skips the inside-the-frames-dir check", expectFail: [TestStewardFrame_RefusesSymlinkEscapeAndOversize] }
commands:
  - { id: C1, run: "task lint", timeoutSec: 900 }
  - { id: C2, run: "task test", timeoutSec: 1800 }
  - { id: C3, run: "cd web/packages/ui && bunx tsc --noEmit -p .", timeoutSec: 900 }
  - { id: C4, run: "GOOS=windows go vet ./internal/inbox/ ./internal/steward/ ./internal/wsapi/", timeoutSec: 600 }
writeScope:
  paths: []
limits: { budgetUsd: 2, maxMinutes: 60 }
---

# S3a validation contract

Judge the branch head against these ACs, ADR-0047 section 5 and 9 and `docs/plans/active/steward-seat.md` S3. Attack targets: (1) any inbox method or UI control that approves, accepts, merges, answers a permission in bulk or sets an always-allow; (2) a stale, open or archived-task review reaching the inbox, or a review row AND a verdict row for one task; (3) seat or finding text reaching an inbox summary (summary must be verdict, PR, head7, gate ids only) or an absolute scratch path reaching the wire (steward.get frames carry base names only); (4) steward.frame reading outside the digest's frames dir (traversal, symlink, non-png, oversize, caller-chosen path) or being callable by a run-scoped agent; (5) the Permission policy page offering any write; (6) a crash on an unknown verdict, gate state, rule action or permission resolution. Frames are captured by the Lead against an isolated daemon with steward and policy methods stubbed; no live model call, no real home.
