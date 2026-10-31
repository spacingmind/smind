---
v: 1
milestone: "Fleet follow-up: host audit view (AC14): smind host audit, fleet.host.audit, Activity row, prompt-free summaries"
base: origin/develop
planRef: docs/plans/active/fleet-hosts.md
acs:
  - id: AC14
    text: "fleet.host.audit is a human-only, read-only method over fleet_audit (direction, peer alias, method, decision, redacted summary, time; never a trace or request id): refused over the relay, to an agent, a plugin or a peer host; filtered by host; the limit defaults to 50 and is clamped to 200; smind host audit and an Activity row per host in Settings -> Hosts read it."
    covers: { tests: [TestFleetHostAudit_ViewShape, TestFleetHostAudit_LimitIsClamped, TestFleetHostAudit_HumanOnly, TestFleetAdminMethodsLocalOnlyNotRunScopedNotPlugin, TestHostAuditPrintsTableAndSendsParams, TestHostAuditDefaultsAndJSON, TestHostAuditEmptyAndErrors, TestHostAuditBadUsage, activity_closed_by_default_no_fetch, activity_lists_rows_for_that_host_only, activity_empty_error_and_refresh, activity_is_offered_for_approved_and_revoked_hosts_only], frames: [] }
  - id: AC14-no-prompt
    text: "No prompt text is ever stored in or served from the audit: the writer replaces the value of every free-text field (prompt, text, message, body, content, input) with its length at any depth before taking the 120-character prefix (the hash still covers the real body), and the view masks the same fields again for rows written before that, including a value cut off by the prefix and one with escaped quotes."
    covers: { tests: [TestFleetSummaryNeverHoldsPromptText, TestFleetSummaryDropsProvAndCutsOnRuneBoundary, TestFleetAuditBothEndsNoPromptBody, TestFleetHostAudit_NeverShowsPromptText, activity_shortens_the_hash_and_renders_text_not_markup], frames: [] }
tests:
  - { name: TestFleetHostAudit_ViewShape, run: "go test ./internal/wsapi -run '^TestFleetHostAudit_ViewShape$' -count=1", timeoutSec: 900 }
  - { name: TestFleetHostAudit_LimitIsClamped, run: "go test ./internal/wsapi -run '^TestFleetHostAudit_LimitIsClamped$' -count=1", timeoutSec: 900 }
  - { name: TestFleetHostAudit_HumanOnly, run: "go test ./internal/wsapi -run '^TestFleetHostAudit_HumanOnly$' -count=1", timeoutSec: 900 }
  - { name: TestFleetHostAudit_NeverShowsPromptText, run: "go test ./internal/wsapi -run '^TestFleetHostAudit_NeverShowsPromptText$' -count=1", timeoutSec: 900 }
  - { name: TestFleetAdminMethodsLocalOnlyNotRunScopedNotPlugin, run: "go test ./internal/wsapi -run '^TestFleetAdminMethodsLocalOnlyNotRunScopedNotPlugin$' -count=1", timeoutSec: 900 }
  - { name: TestFleetSummaryNeverHoldsPromptText, run: "go test ./internal/fleet/serve -run '^TestFleetSummaryNeverHoldsPromptText$' -count=1", timeoutSec: 900 }
  - { name: TestFleetSummaryDropsProvAndCutsOnRuneBoundary, run: "go test ./internal/fleet/serve -run '^TestFleetSummaryDropsProvAndCutsOnRuneBoundary$' -count=1", timeoutSec: 900 }
  - { name: TestFleetAuditBothEndsNoPromptBody, run: "go test ./internal/fleet/serve -run '^TestFleetAuditBothEndsNoPromptBody$' -count=1", timeoutSec: 900 }
  - { name: TestHostAuditPrintsTableAndSendsParams, run: "go test ./cmd/smind -run '^TestHostAuditPrintsTableAndSendsParams$' -count=1", timeoutSec: 900 }
  - { name: TestHostAuditDefaultsAndJSON, run: "go test ./cmd/smind -run '^TestHostAuditDefaultsAndJSON$' -count=1", timeoutSec: 900 }
  - { name: TestHostAuditEmptyAndErrors, run: "go test ./cmd/smind -run '^TestHostAuditEmptyAndErrors$' -count=1", timeoutSec: 900 }
  - { name: TestHostAuditBadUsage, run: "go test ./cmd/smind -run '^TestHostAuditBadUsage$' -count=1", timeoutSec: 900 }
  - { name: activity_closed_by_default_no_fetch, run: "cd web/packages/ui && bunx vitest run src/components/hosts/host-activity.test.tsx -t activity_closed_by_default_no_fetch", timeoutSec: 900 }
  - { name: activity_lists_rows_for_that_host_only, run: "cd web/packages/ui && bunx vitest run src/components/hosts/host-activity.test.tsx -t activity_lists_rows_for_that_host_only", timeoutSec: 900 }
  - { name: activity_empty_error_and_refresh, run: "cd web/packages/ui && bunx vitest run src/components/hosts/host-activity.test.tsx -t activity_empty_error_and_refresh", timeoutSec: 900 }
  - { name: activity_shortens_the_hash_and_renders_text_not_markup, run: "cd web/packages/ui && bunx vitest run src/components/hosts/host-activity.test.tsx -t activity_shortens_the_hash_and_renders_text_not_markup", timeoutSec: 900 }
  - { name: activity_is_offered_for_approved_and_revoked_hosts_only, run: "cd web/packages/ui && bunx vitest run src/components/hosts/host-activity.test.tsx -t activity_is_offered_for_approved_and_revoked_hosts_only", timeoutSec: 900 }
mutations:
  - { id: G1, file: internal/fleet/serve/audit.go, change: "no free-text field is masked in the stored summary", expectFail: [TestFleetSummaryNeverHoldsPromptText] }
  - { id: G2, file: internal/wsapi/fleet_host_audit.go, change: "the view returns the stored summary unmasked", expectFail: [TestFleetHostAudit_NeverShowsPromptText] }
  - { id: G3, file: internal/wsapi/fleet_host.go, change: "fleet.host.audit is registered without the local-only wrapper", expectFail: [TestFleetHostAudit_HumanOnly] }
  - { id: G4, file: internal/wsapi/fleet_host_audit.go, change: "the view masker stops at an escaped quote", expectFail: [TestFleetHostAudit_NeverShowsPromptText] }
  - { id: G5, file: internal/wsapi/fleet_host_audit.go, change: "the 200-row clamp is removed", expectFail: [TestFleetHostAudit_LimitIsClamped] }
  - { id: G6, file: internal/wsapi/fleet_host_audit.go, change: "the peer column shows the host id, not the alias", expectFail: [TestFleetHostAudit_ViewShape] }
  - { id: G7, file: internal/wsapi/fleet_host_audit.go, change: "the host filter is ignored", expectFail: [TestFleetHostAudit_ViewShape] }
  - { id: G8, file: cmd/smind/host_audit.go, change: "--host is not sent", expectFail: [TestHostAuditPrintsTableAndSendsParams] }
  - { id: G9, file: cmd/smind/host_audit.go, change: "the hash is printed in full", expectFail: [TestHostAuditPrintsTableAndSendsParams] }
  - { id: W1, file: web/packages/ui/src/hooks/use-fleet-hosts.ts, change: "the audit query runs while the row is closed", expectFail: [activity_closed_by_default_no_fetch] }
  - { id: W2, file: web/packages/ui/src/hooks/use-fleet-hosts.ts, change: "the host id is not sent", expectFail: [activity_lists_rows_for_that_host_only] }
  - { id: W3, file: web/packages/ui/src/components/hosts/host-row.tsx, change: "Activity is offered for every state", expectFail: [activity_is_offered_for_approved_and_revoked_hosts_only] }
  - { id: W4, file: web/packages/ui/src/components/hosts/host-activity.tsx, change: "the hash is shown in full", expectFail: [activity_shortens_the_hash_and_renders_text_not_markup] }
commands:
  - { id: C1, run: "go vet ./cmd/smind ./internal/wsapi ./internal/fleet/...", timeoutSec: 900 }
  - { id: C2, run: "go test -p 4 ./cmd/smind ./internal/wsapi ./internal/store ./internal/fleet/...", timeoutSec: 1800 }
  - { id: C3, run: "cd web/packages/ui && bunx tsc -b", timeoutSec: 900 }
  - { id: C4, run: "cd web/packages/ui && bun run test", timeoutSec: 1800 }
limits: { budgetUsd: 3, maxMinutes: 60 }
---

# Fleet host audit view validation contract

`fleet.host.audit` is an admin method like the other `fleet.host.*` ones: local-only (the relay is refused), not run-scoped, not a plugin capability, and not in any host kind's allowed set. It is registered only when the fleet has a store.
The summary change is on the writer (`serve.Summary`): the hash covers the real request body, only the stored 120-character prefix has its free-text fields reduced to their length. `title` is not masked: it is a label shown everywhere in the UI.
Frames are not validator frames: the real-app frames (light and dark, 1280 and 390, an isolated daemon with seeded hosts and audit rows) are captured by the Lead with `playwright-core`; their folder is named in the PR body.
