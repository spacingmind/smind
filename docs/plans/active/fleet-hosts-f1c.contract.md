---
v: 1
milestone: "Fleet F1c: Hosts settings page, inbox host kinds, mobile list+approve (ADR-0048)"
base: origin/develop
planRef: docs/plans/active/fleet-hosts.md
acs:
  - id: AC-1
    text: "Inbox derives host_pair (a pending host awaiting this human's approval, high), host_down (a link down more than 5 minutes, normal) and host_refused (version skew or identity change, high) rows from injected sources; with no source (fleet off) nothing is derived and the wsapi inbox is unchanged; host rows can be snoozed or resolved like a stall."
    covers: { tests: [TestInboxHostPairDerived, TestInboxHostDownOnlyAfterFiveMinutes, TestInboxHostRefusedHigh, TestInboxHostKindsAbsentWithoutSource, TestInboxHostSourceOffWhenFleetDisabled] }
  - id: AC-2
    text: "Web Settings -> Hosts lists hosts with state, link state, alias, label and both directions; a daemon without fleet (method-not-found) shows a quiet 'Fleet is off' state and no actions."
    covers: { tests: [TestHostsPage_RendersStates, TestHostsPage_FleetOffState] }
  - id: AC-3
    text: "The pairing SAS is shown and is never auto-confirmed: Approve stays disabled until the human types the six digits and the call carries them; the grant must name at least one write workspace."
    covers: { tests: [TestHostsPage_SASNeverAutoConfirmed, TestHostsPage_GrantRequiresWriteWorkspace] }
  - id: AC-4
    text: "The reverse toggle's copy states the narrower default grant (read only, 0 live agents) before enabling; revoke and remove ask for confirmation."
    covers: { tests: [TestHostsPage_ReverseToggleShowsNarrowerGrant, TestHostsPage_RevokeConfirms] }
  - id: AC-5
    text: "Web inbox renders the three host kinds in their own groups with labels, icons and the Review action; host rows get snooze and resolve."
    covers: { tests: [TestInboxScreen_HostKinds] }
  - id: AC-6
    text: "Mobile lists hosts read-only and approves a pending host from its inbox row with the SAS typed; there is no pairing, invite, grant or reverse call on mobile."
    covers: { tests: [mobile-hosts] }
tests:
  - { name: TestInboxHostPairDerived, run: "go test ./internal/inbox -run '^TestInboxHostPairDerived$' -count=1", timeoutSec: 300 }
  - { name: TestInboxHostDownOnlyAfterFiveMinutes, run: "go test ./internal/inbox -run '^TestInboxHostDownOnlyAfterFiveMinutes$' -count=1", timeoutSec: 300 }
  - { name: TestInboxHostRefusedHigh, run: "go test ./internal/inbox -run '^TestInboxHostRefusedHigh$' -count=1", timeoutSec: 300 }
  - { name: TestInboxHostKindsAbsentWithoutSource, run: "go test ./internal/inbox -run '^TestInboxHostKindsAbsentWithoutSource$' -count=1", timeoutSec: 300 }
  - { name: TestInboxHostSourceOffWhenFleetDisabled, run: "go test ./internal/wsapi -run '^TestInboxHostSourceOffWhenFleetDisabled$' -count=1", timeoutSec: 600 }
  - { name: TestHostsPage_RendersStates, run: "cd web/packages/ui && NODE_OPTIONS=--no-experimental-webstorage bunx vitest run src/components/settings/hosts-section.test.tsx -t 'TestHostsPage_RendersStates'", timeoutSec: 600 }
  - { name: TestHostsPage_FleetOffState, run: "cd web/packages/ui && NODE_OPTIONS=--no-experimental-webstorage bunx vitest run src/components/settings/hosts-section.test.tsx -t 'TestHostsPage_FleetOffState'", timeoutSec: 600 }
  - { name: TestHostsPage_SASNeverAutoConfirmed, run: "cd web/packages/ui && NODE_OPTIONS=--no-experimental-webstorage bunx vitest run src/components/settings/hosts-section.test.tsx -t 'TestHostsPage_SASNeverAutoConfirmed'", timeoutSec: 600 }
  - { name: TestHostsPage_GrantRequiresWriteWorkspace, run: "cd web/packages/ui && NODE_OPTIONS=--no-experimental-webstorage bunx vitest run src/components/settings/hosts-section.test.tsx -t 'TestHostsPage_GrantRequiresWriteWorkspace'", timeoutSec: 600 }
  - { name: TestHostsPage_ReverseToggleShowsNarrowerGrant, run: "cd web/packages/ui && NODE_OPTIONS=--no-experimental-webstorage bunx vitest run src/components/settings/hosts-section.test.tsx -t 'TestHostsPage_ReverseToggleShowsNarrowerGrant'", timeoutSec: 600 }
  - { name: TestHostsPage_RevokeConfirms, run: "cd web/packages/ui && NODE_OPTIONS=--no-experimental-webstorage bunx vitest run src/components/settings/hosts-section.test.tsx -t 'TestHostsPage_RevokeConfirms'", timeoutSec: 600 }
  - { name: TestInboxScreen_HostKinds, run: "cd web/packages/ui && NODE_OPTIONS=--no-experimental-webstorage bunx vitest run src/components/inbox/inbox-screen.test.tsx -t 'TestInboxScreen_HostKinds'", timeoutSec: 600 }
  - { name: mobile-hosts, run: "cd mobile && npx vitest run src/__tests__/hostsList.test.ts src/__tests__/hostsApprove.test.ts src/__tests__/hostsSafety.test.ts src/__tests__/inboxView.test.ts", timeoutSec: 600 }
mutations:
  - { id: M1, file: internal/inbox/inbox.go, change: "host_down emitted without the 5 minute age check", expectFail: [TestInboxHostDownOnlyAfterFiveMinutes] }
  - { id: M2, file: internal/inbox/inbox.go, change: "host_refused attention set to normal", expectFail: [TestInboxHostRefusedHigh] }
  - { id: M3, file: web/packages/ui/src/components/hosts/approve-host-dialog.tsx, change: "Approve enabled without the typed SAS matching", expectFail: [TestHostsPage_SASNeverAutoConfirmed] }
  - { id: M4, file: web/packages/ui/src/components/hosts/approve-host-dialog.tsx, change: "canApprove drops the !grantNeedsWriteWorkspace(grant) term", expectFail: [TestHostsPage_GrantRequiresWriteWorkspace] }
  - { id: M5, file: web/packages/ui/src/lib/fleet-types.ts, change: "REVERSE_GRANT_COPY reworded without read only and 0 live agents", expectFail: [TestHostsPage_ReverseToggleShowsNarrowerGrant] }
  - { id: M6, file: mobile/src/hosts/api.ts, change: "approve sends no sas", expectFail: [mobile-hosts] }
commands:
  - { id: C1, run: "task lint", timeoutSec: 900 }
  - { id: C2, run: "task test", timeoutSec: 1500 }
limits: { budgetUsd: 2, maxMinutes: 45 }
writeScope:
  paths: []
---

# F1c validation contract

UI and a derived inbox read model only: no method here writes a grant, link or secret that the existing local-only admin methods
(F1b) do not already own. Judge the diff against AC-1..AC-6, run each named test, apply each mutation (one at a time, restore after)
and confirm the named test fails. Look for: any path where the SAS is confirmed without a human typing it, any place a host
secret/key/relay secret field is rendered, inbox derivation when fleet is off, mobile calls beyond list/approve.
