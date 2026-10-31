---
v: 1
milestone: "Fleet F3a: callee write path and budgets (ADR-0048)"
base: origin/develop
planRef: docs/plans/active/fleet-hosts.md
acs:
  - id: AC-17
    text: "fleet.task.create needs op create and a workspace in workspacesWrite; provider and model are required; the request type has no permissionMode, profileId, env, cwd, attachments, mcp, grant or secret field; a provider the callee lacks fails provider_not_configured; the branch is fetched from the callee's own origin (branch_not_found otherwise); no path, diff or file upload exists."
    covers: { tests: [TestFleetCreateRequiresExplicitProviderModel, TestFleetCreateTypeHasNoModeEnvCwdFields, TestFleetCreateBranchFromOwnOrigin, TestFleetCreateProviderNotConfigured, TestFleetParamsHaveNoGrantOrSecretFields] }
  - id: AC-18
    text: "fleet.task.send and fleet.task.stop act only on tasks the calling peer created (not_owner otherwise); prompts are text only; idempotencyKey (24 h) makes a retry return the same task and run; a different key creates another."
    covers: { tests: [TestFleetSendOnlyOwnTasks, TestFleetIdempotencyKeyRetryReturnsSame] }
  - id: AC-19
    text: "Remote-created tasks carry origin_host and origin_trace, run with the remote source: never AutoAccept, never an auto-approving mode, and the ADR-0047 floor decides in every mode; a permission request goes to the callee's inbox and the caller sees only waiting_permission."
    covers: { tests: [TestFleetRemoteTaskUnderCalleeFloorAndMode, TestFleetRemotePermissionGoesToCalleeInbox] }
  - id: AC-20
    text: "Per-origin budget: the callee refuses with budget_exceeded{which} when remote-origin live agents reach maxLiveAgents or reported 24 h cost reaches maxCostUsd, counted per origin host across all trace ids; a run over maxWallSeconds is stopped by the callee and reported wall_limit; nil cost is never estimated."
    covers: { tests: [TestFleetBudgetPerOriginAcrossTraces, TestFleetWallLimitStopsRun] }
  - id: AC-D6
    text: "Accounts and secrets never cross hosts: a sentinel secret in the caller's env/config/relay secret appears in no callee-side row, event, argv or log."
    covers: { tests: [TestFleetSecretNeverLeavesHost] }
tests:
  - { name: TestFleetCreateRequiresExplicitProviderModel, run: "go test ./internal/fleet/serve -run '^TestFleetCreateRequiresExplicitProviderModel$' -count=1", timeoutSec: 300 }
  - { name: TestFleetCreateTypeHasNoModeEnvCwdFields, run: "go test ./internal/fleet/wire -run '^TestFleetCreateTypeHasNoModeEnvCwdFields$' -count=1", timeoutSec: 300 }
  - { name: TestFleetCreateBranchFromOwnOrigin, run: "go test ./internal/wsapi -run '^TestFleetCreateBranchFromOwnOrigin$' -count=1", timeoutSec: 300 }
  - { name: TestFleetCreateProviderNotConfigured, run: "go test ./internal/fleet/serve -run '^TestFleetCreateProviderNotConfigured$' -count=1", timeoutSec: 300 }
  - { name: TestFleetParamsHaveNoGrantOrSecretFields, run: "go test ./internal/wsapi -run '^TestFleetParamsHaveNoGrantOrSecretFields$' -count=1", timeoutSec: 300 }
  - { name: TestFleetSendOnlyOwnTasks, run: "go test ./internal/fleet/serve -run '^TestFleetSendOnlyOwnTasks$' -count=1", timeoutSec: 300 }
  - { name: TestFleetIdempotencyKeyRetryReturnsSame, run: "go test ./internal/fleet/serve -run '^TestFleetIdempotencyKeyRetryReturnsSame$' -count=1", timeoutSec: 300 }
  - { name: TestFleetRemoteTaskUnderCalleeFloorAndMode, run: "go test ./internal/runs -run '^TestFleetRemoteTaskUnderCalleeFloorAndMode$' -count=1", timeoutSec: 300 }
  - { name: TestFleetRemotePermissionGoesToCalleeInbox, run: "go test ./internal/wsapi -run '^TestFleetRemotePermissionGoesToCalleeInbox$' -count=1", timeoutSec: 300 }
  - { name: TestFleetBudgetPerOriginAcrossTraces, run: "go test ./internal/fleet/serve -run '^TestFleetBudgetPerOriginAcrossTraces$' -count=1", timeoutSec: 300 }
  - { name: TestFleetWallLimitStopsRun, run: "go test ./internal/fleet/serve -run '^TestFleetWallLimitStopsRun$' -count=1", timeoutSec: 300 }
  - { name: TestFleetSecretNeverLeavesHost, run: "go test ./internal/wsapi -run '^TestFleetSecretNeverLeavesHost$' -count=1", timeoutSec: 300 }
mutations:
  - { id: M1, file: internal/fleet/serve/budget.go, change: "count live agents per trace instead of per origin host", expectFail: [TestFleetBudgetPerOriginAcrossTraces] }
  - { id: M2, file: internal/fleet/wire/write.go, change: "add an Env field to TaskCreateParams", expectFail: [TestFleetCreateTypeHasNoModeEnvCwdFields, TestFleetParamsHaveNoGrantOrSecretFields] }
  - { id: M3, file: internal/fleet/serve/write.go, change: "skip the owner check on send", expectFail: [TestFleetSendOnlyOwnTasks] }
  - { id: M4, file: internal/runs/queue.go, change: "remote source keeps AutoAccept", expectFail: [TestFleetRemoteTaskUnderCalleeFloorAndMode] }
  - { id: M5, file: internal/fleet/serve/wall.go, change: "sweeper never stops a run", expectFail: [TestFleetWallLimitStopsRun] }
  - { id: M6, file: internal/fleet/serve/write.go, change: "idempotency lookup skipped", expectFail: [TestFleetIdempotencyKeyRetryReturnsSame] }
---

F3a callee write path: see the plan's "F3a design".
