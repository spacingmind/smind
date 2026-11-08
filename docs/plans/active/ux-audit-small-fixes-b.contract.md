---
v: 1
milestone: "UX audit PR B: Deny with a reason, new-file content, demo card parity"
base: origin/develop
planRef: docs/plans/active/ux-audit-small-fixes.md
acs:
  - id: B1
    text: "run.respondPermission accepts an optional reason only with a reject option of the pending request; the text is trimmed and cleaned, at most 500 characters, valid UTF-8 without NUL. An allow with a reason, an unknown option or a bad text is refused and the request stays pending."
    covers: { tests: [TestDenyReason_RefusedWithAnAllowOptionOrOverTheCapAndRequestStaysPending, TestSanitizeDenyReason, TestSanitizeDenyReason_RefusesNUL, TestServer_RunRespondPermission_DenyReason], frames: [] }
  - id: B2
    text: "The reason reaches the model as the deny message when the provider runs through the Claude Code adapter; otherwise the Deny is sent as before and the result says reasonDelivered false."
    covers: { tests: [TestDenyReason_DeliveredToTheModelWhenTheProviderCanCarryIt, TestDenyReason_NotDeliveredWhereTheProviderCannotCarryIt], frames: [] }
  - id: B3
    text: "Callers that send no reason behave exactly as before: same options, same {} result, a second answer still refused as already resolved."
    covers: { tests: [TestDenyReason_EmptyReasonBehavesExactlyLikeRespondPermission, TestServer_RunRespondPermission_CrossConnection, TestTaskDenyWithoutReasonSendsNoReasonField], frames: [] }
  - id: B4
    text: "smind task deny takes --reason <text> or --reason=<text>; approve takes none; a missing or repeated value is a usage error that calls nothing."
    covers: { tests: [TestTaskDenyReasonFlag, TestTaskDenyReasonFlagNeedsAValueAndApproveHasNone], frames: [] }
  - id: B5
    text: "The web permission card has a labelled one-line field; Deny sends its text (capped at 500), Allow never sends it, and an old onRespond caller with three arguments is unchanged."
    covers: { tests: [deny_reason_is_sent_only_with_deny, deny_reason_is_capped_at_500, allow_never_sends_the_reason, old_onRespond_callers_unchanged], frames: [] }
  - id: B6
    text: "A write to a new path shows its content open under the title Content; an overwrite keeps the collapsed Diff."
    covers: { tests: [new_file_write_shows_content_open, overwrite_keeps_collapsed_diff], frames: [] }
  - id: B7
    text: "The demo permission card offers no Allow always."
    covers: { tests: [demo_permission_card_has_no_allow_always], frames: [] }
tests:
  - { name: TestDenyReason_RefusedWithAnAllowOptionOrOverTheCapAndRequestStaysPending, run: "go test ./internal/runs -run TestDenyReason_RefusedWithAnAllowOptionOrOverTheCapAndRequestStaysPending", timeoutSec: 900 }
  - { name: TestSanitizeDenyReason, run: "go test ./internal/runs -run '^TestSanitizeDenyReason$'", timeoutSec: 900 }
  - { name: TestSanitizeDenyReason_RefusesNUL, run: "go test ./internal/runs -run TestSanitizeDenyReason_RefusesNUL", timeoutSec: 900 }
  - { name: TestServer_RunRespondPermission_DenyReason, run: "go test ./internal/wsapi -run TestServer_RunRespondPermission_DenyReason", timeoutSec: 900 }
  - { name: TestServer_RunRespondPermission_CrossConnection, run: "go test ./internal/wsapi -run TestServer_RunRespondPermission_CrossConnection", timeoutSec: 900 }
  - { name: TestDenyReason_DeliveredToTheModelWhenTheProviderCanCarryIt, run: "go test ./internal/runs -run TestDenyReason_DeliveredToTheModelWhenTheProviderCanCarryIt", timeoutSec: 900 }
  - { name: TestDenyReason_NotDeliveredWhereTheProviderCannotCarryIt, run: "go test ./internal/runs -run TestDenyReason_NotDeliveredWhereTheProviderCannotCarryIt", timeoutSec: 900 }
  - { name: TestDenyReason_EmptyReasonBehavesExactlyLikeRespondPermission, run: "go test ./internal/runs -run TestDenyReason_EmptyReasonBehavesExactlyLikeRespondPermission", timeoutSec: 900 }
  - { name: TestTaskDenyWithoutReasonSendsNoReasonField, run: "go test ./cmd/smind -run TestTaskDenyWithoutReasonSendsNoReasonField", timeoutSec: 900 }
  - { name: TestTaskDenyReasonFlag, run: "go test ./cmd/smind -run '^TestTaskDenyReasonFlag$'", timeoutSec: 900 }
  - { name: TestTaskDenyReasonFlagNeedsAValueAndApproveHasNone, run: "go test ./cmd/smind -run TestTaskDenyReasonFlagNeedsAValueAndApproveHasNone", timeoutSec: 900 }
  - { name: deny_reason_is_sent_only_with_deny, run: "cd web/packages/ui && bunx vitest run src/components/permission/permission-card.test.tsx -t deny_reason_is_sent_only_with_deny", timeoutSec: 900 }
  - { name: deny_reason_is_capped_at_500, run: "cd web/packages/ui && bunx vitest run src/components/permission/permission-card.test.tsx -t deny_reason_is_capped_at_500", timeoutSec: 900 }
  - { name: allow_never_sends_the_reason, run: "cd web/packages/ui && bunx vitest run src/components/permission/permission-card.test.tsx -t allow_never_sends_the_reason", timeoutSec: 900 }
  - { name: old_onRespond_callers_unchanged, run: "cd web/packages/ui && bunx vitest run src/components/permission/permission-card.test.tsx -t old_onRespond_callers_unchanged", timeoutSec: 900 }
  - { name: new_file_write_shows_content_open, run: "cd web/packages/ui && bunx vitest run src/components/permission/permission-detail.test.tsx -t new_file_write_shows_content_open", timeoutSec: 900 }
  - { name: overwrite_keeps_collapsed_diff, run: "cd web/packages/ui && bunx vitest run src/components/permission/permission-detail.test.tsx -t overwrite_keeps_collapsed_diff", timeoutSec: 900 }
  - { name: demo_permission_card_has_no_allow_always, run: "cd web/packages/ui && bunx vitest run src/demo -t demo_permission_card_has_no_allow_always", timeoutSec: 900 }
mutations:
  - { id: D1, file: internal/runs/registry.go, change: "the reject-option check in RespondPermissionWithReason is removed", expectFail: [TestDenyReason_RefusedWithAnAllowOptionOrOverTheCapAndRequestStaysPending] }
  - { id: D2, file: internal/runs/registry.go, change: "the 500-rune cap check in SanitizeDenyReason is removed", expectFail: [TestDenyReason_RefusedWithAnAllowOptionOrOverTheCapAndRequestStaysPending] }
  - { id: D3, file: internal/runs/registry.go, change: "RespondPermissionWithReason reports delivered whenever a reason was sent", expectFail: [TestDenyReason_NotDeliveredWhereTheProviderCannotCarryIt] }
  - { id: D4, file: web/packages/ui/src/components/permission/options-card.tsx, change: "an allow click also sends the typed reason", expectFail: [allow_never_sends_the_reason] }
commands:
  - { id: C1, run: "go vet ./internal/runs ./internal/wsapi ./cmd/...", timeoutSec: 900 }
  - { id: C2, run: "cd web/packages/ui && bunx tsc -b", timeoutSec: 900 }
  - { id: C3, run: "cd web/packages/ui && bun run test", timeoutSec: 1800 }
limits: { budgetUsd: 3, maxMinutes: 60 }
---

# UX audit PR B validation contract

The deny reason is the human's own text, delivered to the model only as the deny message of a tool use the human already refused. It never widens a permission, never selects an option, and an allow cannot carry one. Not stored in run history.
Frames are not validator frames: the real-app frames (light and dark, 1280 and 390, against an isolated daemon) are captured by the Lead; their folder is named in the PR body.
