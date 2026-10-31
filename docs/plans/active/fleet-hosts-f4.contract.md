---
v: 1
milestone: "Fleet F4: the Lead/Peer tree across hosts, host chip in the UI (ADR-0048)"
base: origin/develop
planRef: docs/plans/active/fleet-hosts.md
acs:
  - id: AC-23
    text: "task.tree shows remote children under their local parent with remote, host alias, hostId, globalId and mirrored state; usage and live-count rollups include remote reported cost and state (nil stays nil); the tree budget counts remote spend; deleting every mirror loses nothing local; archiveFinished and stopTree never see a mirror."
    covers: { tests: [TestTaskTreeShowsRemoteChildrenAndRollups, TestTaskTreeRemoteNilCostStaysNil, TestTreeBudgetCountsRemoteSpend, TestMirrorRowsAreCacheOnly, TestTaskTreeHandlerRemoteOnlyWhenFleetOn] }
  - id: AC-24
    text: "Exactly one owner per task: a mirror row is updated only from the owner host's own answer (host id from the row, never the response); ids are alias:id for humans and {hostId,id} stored; an alias rename changes no stored wire id; no local method accepts a mirror."
    covers: { tests: [TestOwnerHostIsSingle, TestAliasRenameKeepsWireIds, TestRefresherUpdatesOnlyOwnerRowOnSuccess, TestRefresherErrorLeavesRowUntouched] }
  - id: AC-25
    text: "Web shows a host chip on remote nodes in the tree, inbox and task header; mobile shows the chip; light and dark, 1280 and 390, transition frames of the tree expanding with a remote child reviewed."
    covers: { tests: [TestTaskLineageRemoteNodeShowsHostChip, TestInboxRowShowsHostChip, TestRemoteTaskHeaderReadOnlyWithHostChip] }
tests:
  - { name: TestTaskTreeShowsRemoteChildrenAndRollups, run: "go test ./internal/tasktree -run '^TestTaskTreeShowsRemoteChildrenAndRollups$' -count=1", timeoutSec: 300 }
  - { name: TestTaskTreeRemoteNilCostStaysNil, run: "go test ./internal/tasktree -run '^TestTaskTreeRemoteNilCostStaysNil$' -count=1", timeoutSec: 300 }
  - { name: TestTreeBudgetCountsRemoteSpend, run: "go test ./internal/store ./internal/runs -run '^TestTreeBudgetCountsRemoteSpend$' -count=1", timeoutSec: 300 }
  - { name: TestMirrorRowsAreCacheOnly, run: "go test ./internal/wsapi -run '^TestMirrorRowsAreCacheOnly$' -count=1", timeoutSec: 300 }
  - { name: TestTaskTreeHandlerRemoteOnlyWhenFleetOn, run: "go test ./internal/wsapi -run '^TestTaskTreeHandlerRemoteOnlyWhenFleetOn$' -count=1", timeoutSec: 300 }
  - { name: TestOwnerHostIsSingle, run: "go test ./internal/fleet/mirror -run '^TestOwnerHostIsSingle$' -count=1", timeoutSec: 300 }
  - { name: TestAliasRenameKeepsWireIds, run: "go test ./internal/wsapi -run '^TestAliasRenameKeepsWireIds$' -count=1", timeoutSec: 300 }
  - { name: TestRefresherUpdatesOnlyOwnerRowOnSuccess, run: "go test ./internal/fleet/mirror -run '^TestRefresherUpdatesOnlyOwnerRowOnSuccess$' -count=1", timeoutSec: 300 }
  - { name: TestRefresherErrorLeavesRowUntouched, run: "go test ./internal/fleet/mirror -run '^TestRefresherErrorLeavesRowUntouched$' -count=1", timeoutSec: 300 }
  - { name: TestTaskLineageRemoteNodeShowsHostChip, run: "cd web && bun run --cwd packages/ui vitest run src/components/task-lineage.test.tsx -t TestTaskLineageRemoteNodeShowsHostChip", timeoutSec: 600 }
  - { name: TestInboxRowShowsHostChip, run: "cd web && bun run --cwd packages/ui vitest run src/components/inbox -t TestInboxRowShowsHostChip", timeoutSec: 600 }
  - { name: TestRemoteTaskHeaderReadOnlyWithHostChip, run: "cd web && bun run --cwd packages/ui vitest run src/components/app-header-task.test.tsx -t TestRemoteTaskHeaderReadOnlyWithHostChip", timeoutSec: 600 }
mutations:
  - { id: M1, file: internal/tasktree/tree.go, change: "do not add remote cost to the ancestors' subtree usage", expectFail: [TestTaskTreeShowsRemoteChildrenAndRollups] }
  - { id: M2, file: internal/store/task_budget.go, change: "drop the remote mirror term from TreeKnownCost", expectFail: [TestTreeBudgetCountsRemoteSpend] }
  - { id: M3, file: internal/fleet/mirror/refresh.go, change: "write the row of the host named in the response instead of the row's host id", expectFail: [TestRefresherUpdatesOnlyOwnerRowOnSuccess] }
  - { id: M4, file: internal/tasktree/tree.go, change: "estimate a nil remote cost as 0", expectFail: [TestTaskTreeRemoteNilCostStaysNil] }
  - { id: M5, file: internal/wsapi/task_tree.go, change: "pass remotes into buildTaskTree (shared with stopTree)", expectFail: [TestMirrorRowsAreCacheOnly] }
---

F4 tree across hosts: see the plan's "F4 design".
