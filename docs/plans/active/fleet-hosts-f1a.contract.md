---
v: 1
milestone: "Fleet F1a: host identity, registry, store tables, fleet config (ADR-0048)"
base: origin/develop
planRef: docs/plans/active/fleet-hosts.md
acs:
  - id: AC-1
    text: "Fleet off (default/absent): no host-key file created, no fleet table row written, config default Enabled=false with no env override, fleet.LoadIdentity returns ErrOff without touching the home dir; existing task/run rows keep empty origin columns."
    covers: { tests: [TestFleetOffIsByteForByte, TestFleet_DisabledByDefaultAndDefaults, TestFleet_EnvCannotEnable, TestFleetMigration_ExistingRowsKeepEmptyOrigin] }
  - id: AC-2
    text: "Host key is created lazily with mode 0600 through securefile; hostId equals the D35 DeviceID derivation on the shared vector; a corrupt or world-readable key file is refused and never regenerated or chmodded."
    covers: { tests: [TestHostIDMatchesDeviceIDVector, TestHostKeyFileModeAndCorruption, TestHostKeyConcurrentCreateOneIdentity] }
  - id: AC-3
    text: "All fleet tables and origin columns exist after migration (idempotent): fleet_hosts, fleet_links, fleet_audit, fleet_watches, fleet_idem, fleet_remote_tasks, tasks.origin_host, tasks.origin_trace, runs.origin_host; no mesh/cluster table; pair creation is atomic."
    covers: { tests: [TestFleetMigration_TablesAndColumns, TestFleetPair_CreateGetListDelete, TestFleetPair_AliasAndHostConflicts, TestFleetPair_RollsBackOnBadLink, TestFleetPair_RollsBackOnMidInsertFailure, TestFleetPair_RejectsLinkForOtherHost, TestFleetHost_NoSchemaForMesh] }
  - id: AC-4
    text: "Audit, watch, idempotency and mirror queries: summary truncation, only-old sweep, watch survives host removal, mirror+watch one transaction, nil cost stays nil, second idempotency put returns the first."
    covers: { tests: [TestFleetAudit_TruncatesSummary, TestFleetAudit_SweepOnlyOld, TestFleetWatch_MirrorAndWatchOneTransaction, TestFleetWatch_SurvivesHostDelete, TestFleetRemoteTask_NilCostStaysNil, TestFleetIdem_SecondPutReturnsFirst] }
  - id: AC-5
    text: "Grant validation is field-named and fail-closed; ForApproval requires at least one write workspace when create/send/stop is granted; S5 default forward and reverse grants equal the ADR table (reverse = read only, 0 agents, 0 wall, $0)."
    covers: { tests: [TestGrantValidate_Table, TestGrantValidateForApproval_RequiresWriteWorkspace, TestDefaultForwardGrant_S5, TestDefaultReverseGrant_S5, TestAliasValidation] }
  - id: AC-6
    text: "One pairing makes exactly two directed links: initiator outbound enabled, initiator inbound disabled; responder inbound enabled only after human approval with a valid grant; reverse is enabled only by EnableReverse with the narrow default; Authorize refuses a disabled or unapproved direction, an op outside the grant and a workspace outside scope."
    covers: { tests: [TestRegistryPairingDirectedLinksReverseOff, TestRegistryResponderAcceptApprove, TestRegistryEnableDisableReverse, TestRegistryAuthorize, TestRegistryMaxHosts] }
  - id: AC-7
    text: "fleet.* config validation names the field and bad value; limits default to the ADR section 12 numbers."
    covers: { tests: [TestFleet_ValidationNamesField, TestFleet_EnabledReadFromYAML] }
tests:
  - { name: TestFleetOffIsByteForByte, run: "go test ./internal/fleet -run '^TestFleetOffIsByteForByte$' -count=1", timeoutSec: 300 }
  - { name: TestFleet_DisabledByDefaultAndDefaults, run: "go test ./internal/config -run '^TestFleet_DisabledByDefaultAndDefaults$' -count=1", timeoutSec: 300 }
  - { name: TestFleet_EnvCannotEnable, run: "go test ./internal/config -run '^TestFleet_EnvCannotEnable$' -count=1", timeoutSec: 300 }
  - { name: TestFleetMigration_ExistingRowsKeepEmptyOrigin, run: "go test ./internal/store -run '^TestFleetMigration_ExistingRowsKeepEmptyOrigin$' -count=1", timeoutSec: 300 }
  - { name: TestHostIDMatchesDeviceIDVector, run: "go test ./internal/fleet/identity -run '^TestHostIDMatchesDeviceIDVector$' -count=1", timeoutSec: 300 }
  - { name: TestHostKeyFileModeAndCorruption, run: "go test ./internal/fleet/identity -run '^TestHostKeyFileModeAndCorruption$' -count=1", timeoutSec: 300 }
  - { name: TestHostKeyConcurrentCreateOneIdentity, run: "go test ./internal/fleet/identity -run '^TestHostKeyConcurrentCreateOneIdentity$' -count=1", timeoutSec: 300 }
  - { name: TestFleetMigration_TablesAndColumns, run: "go test ./internal/store -run '^TestFleetMigration_TablesAndColumns$' -count=1", timeoutSec: 300 }
  - { name: TestFleetPair_CreateGetListDelete, run: "go test ./internal/store -run '^TestFleetPair_CreateGetListDelete$' -count=1", timeoutSec: 300 }
  - { name: TestFleetPair_AliasAndHostConflicts, run: "go test ./internal/store -run '^TestFleetPair_AliasAndHostConflicts$' -count=1", timeoutSec: 300 }
  - { name: TestFleetPair_RollsBackOnBadLink, run: "go test ./internal/store -run '^TestFleetPair_RollsBackOnBadLink$' -count=1", timeoutSec: 300 }
  - { name: TestFleetPair_RollsBackOnMidInsertFailure, run: "go test ./internal/store -run '^TestFleetPair_RollsBackOnMidInsertFailure$' -count=1", timeoutSec: 300 }
  - { name: TestFleetHost_NoSchemaForMesh, run: "go test ./internal/store -run '^TestFleetHost_NoSchemaForMesh$' -count=1", timeoutSec: 300 }
  - { name: TestFleetAudit_TruncatesSummary, run: "go test ./internal/store -run '^TestFleetAudit_TruncatesSummary$' -count=1", timeoutSec: 300 }
  - { name: TestFleetAudit_SweepOnlyOld, run: "go test ./internal/store -run '^TestFleetAudit_SweepOnlyOld$' -count=1", timeoutSec: 300 }
  - { name: TestFleetWatch_MirrorAndWatchOneTransaction, run: "go test ./internal/store -run '^TestFleetWatch_MirrorAndWatchOneTransaction$' -count=1", timeoutSec: 300 }
  - { name: TestFleetWatch_SurvivesHostDelete, run: "go test ./internal/store -run '^TestFleetWatch_SurvivesHostDelete$' -count=1", timeoutSec: 300 }
  - { name: TestFleetRemoteTask_NilCostStaysNil, run: "go test ./internal/store -run '^TestFleetRemoteTask_NilCostStaysNil$' -count=1", timeoutSec: 300 }
  - { name: TestFleetIdem_SecondPutReturnsFirst, run: "go test ./internal/store -run '^TestFleetIdem_SecondPutReturnsFirst$' -count=1", timeoutSec: 300 }
  - { name: TestGrantValidate_Table, run: "go test ./internal/fleet/registry -run '^TestGrantValidate_Table$' -count=1", timeoutSec: 300 }
  - { name: TestGrantValidateForApproval_RequiresWriteWorkspace, run: "go test ./internal/fleet/registry -run '^TestGrantValidateForApproval_RequiresWriteWorkspace$' -count=1", timeoutSec: 300 }
  - { name: TestDefaultForwardGrant_S5, run: "go test ./internal/fleet/registry -run '^TestDefaultForwardGrant_S5$' -count=1", timeoutSec: 300 }
  - { name: TestDefaultReverseGrant_S5, run: "go test ./internal/fleet/registry -run '^TestDefaultReverseGrant_S5$' -count=1", timeoutSec: 300 }
  - { name: TestAliasValidation, run: "go test ./internal/fleet/registry -run '^TestAliasValidation$' -count=1", timeoutSec: 300 }
  - { name: TestRegistryPairingDirectedLinksReverseOff, run: "go test ./internal/fleet/registry -run '^TestRegistryPairingDirectedLinksReverseOff$' -count=1", timeoutSec: 300 }
  - { name: TestRegistryResponderAcceptApprove, run: "go test ./internal/fleet/registry -run '^TestRegistryResponderAcceptApprove$' -count=1", timeoutSec: 300 }
  - { name: TestRegistryEnableDisableReverse, run: "go test ./internal/fleet/registry -run '^TestRegistryEnableDisableReverse$' -count=1", timeoutSec: 300 }
  - { name: TestRegistryAuthorize, run: "go test ./internal/fleet/registry -run '^TestRegistryAuthorize$' -count=1", timeoutSec: 300 }
  - { name: TestRegistryMaxHosts, run: "go test ./internal/fleet/registry -run '^TestRegistryMaxHosts$' -count=1", timeoutSec: 300 }
  - { name: TestFleet_ValidationNamesField, run: "go test ./internal/config -run '^TestFleet_ValidationNamesField$' -count=1", timeoutSec: 300 }
  - { name: TestFleet_EnabledReadFromYAML, run: "go test ./internal/config -run '^TestFleet_EnabledReadFromYAML$' -count=1", timeoutSec: 300 }
mutations:
  - { id: M1, file: internal/fleet/identity/identity.go, change: "truncate the hostId to 25 characters instead of 26", expectFail: [TestHostIDMatchesDeviceIDVector] }
  - { id: M2, file: internal/fleet/identity/identity.go, change: "LoadOrCreate regenerates the key when Load returns ErrCorrupt", expectFail: [TestHostKeyFileModeAndCorruption] }
  - { id: M3, file: internal/fleet/identity/identity.go, change: "Load skips the securefile.IsPrivate check", expectFail: [TestHostKeyFileModeAndCorruption] }
  - { id: M4, file: internal/fleet/registry/grant.go, change: "DefaultReverseGrant also grants op create", expectFail: [TestDefaultReverseGrant_S5] }
  - { id: M5, file: internal/fleet/registry/registry.go, change: "initiator's inbound link is created enabled", expectFail: [TestRegistryPairingDirectedLinksReverseOff] }
  - { id: M6, file: internal/fleet/registry/registry.go, change: "Authorize ignores the inbound enabled flag", expectFail: [TestRegistryAuthorize] }
  - { id: M7, file: internal/fleet/fleet.go, change: "LoadIdentity ignores cfg.Enabled and creates the key", expectFail: [TestFleetOffIsByteForByte] }
  - { id: M8, file: internal/store/fleet.go, change: "CreateFleetPair commits the transaction when a link insert fails instead of rolling back", expectFail: [TestFleetPair_RollsBackOnMidInsertFailure] }
  - { id: M9, file: internal/store/fleet_audit.go, change: "AppendFleetAudit stops truncating the summary", expectFail: [TestFleetAudit_TruncatesSummary] }
  - { id: M10, file: internal/config/fleet.go, change: "drop the maxHosts range check", expectFail: [TestFleet_ValidationNamesField] }
commands:
  - { id: C1, run: "task lint", timeoutSec: 900 }
  - { id: C2, run: "task test", timeoutSec: 1500 }
  - { id: C3, run: "GOOS=windows go vet ./internal/fleet/... ./internal/store/ ./internal/config/", timeoutSec: 600 }
writeScope:
  paths: []
limits: { budgetUsd: 2, maxMinutes: 60 }
---

# F1a validation contract

Judge the branch head against these ACs and `docs/decisions/0048-fleet-connected-hosts.md` (sections 1, 12) and `docs/plans/active/fleet-hosts.md` F1a. Attack targets: (1) any code path that creates `host-key` or touches a fleet table while `fleet.enabled` is false; (2) a key file that is regenerated, chmodded or silently accepted when corrupt or world-readable; (3) a default grant wider than the ADR table, or a reverse direction that is enabled by pairing; (4) Authorize returning nil for an unapproved host, disabled inbound, unlisted op or workspace; (5) an existing store struct or query changed (additive only); (6) a secret, key or password stored in a field returned by a list query; (7) a Windows-build break. No network code, CLI or UI is in this PR (expected). No real home: tests use temp dirs.
