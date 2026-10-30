---
v: 1
milestone: "Per-task scratch dir and cleanup on archive/delete"
base: origin/develop
planRef: docs/plans/active/task-scratch.md
acs:
  - id: AC-S1
    text: "Nothing outside scratch/<taskId> is ever removed: a symlink inside (or at) the task dir pointing outside is deleted as a link and its target survives."
    covers: { tests: [TestRemove_OnlyTaskDir, TestRemove_SymlinkEscapeNotFollowed, TestRemove_SymlinkAtTaskDirRemovesLinkOnly], frames: [] }
  - id: AC-S2
    text: "Read-only files (Go module cache style) are removed; a running task's dir is never removed."
    covers: { tests: [TestRemove_ReadOnlyFiles, TestRemove_BusyRefused, TestSweep_SkipsBusyThenRetries], frames: [] }
  - id: AC-S3
    text: "Archive and delete of a task remove its scratch dir; the startup sweep removes archived/deleted/unknown dirs, keeps live ones and ignores non-numeric entries."
    covers: { tests: [TestArchive_RemovesScratch, TestDelete_RemovesScratch, TestSweep_RemovesClosedKeepsLive, TestSweep_IgnoresNonNumeric], frames: [] }
  - id: AC-S4
    text: "Every harness launches with TMPDIR in the task scratch dir; a pinned TMPDIR wins."
    covers: { tests: [TestLaunchEnv_ScratchTmpdir_Claude, TestLaunchEnv_ScratchTmpdir_Codex, TestLaunchEnv_ScratchTmpdir_ACP, TestLaunchEnv_ScratchTmpdir_GLM, TestLaunchEnv_PinnedTmpdirWins], frames: [] }
  - id: AC-S5
    text: "Shared caches are set only when enabled and never override an existing value."
    covers: { tests: [TestSharedCaches_On, TestSharedCaches_Off, TestSharedCaches_DoesNotOverrideExisting], frames: [] }
  - id: AC-S6
    text: "scratch.usage and scratch.clean are human-only."
    covers: { tests: [TestScratchRPC_RefusedForRunScoped, TestScratchRPC_RefusedForPlugin, TestScratchRPC_UsageAndClean], frames: [] }
tests:
  - { name: TestRemove_OnlyTaskDir, run: "go test ./internal/scratch -run '^TestRemove_OnlyTaskDir$' -count=1", timeoutSec: 120 }
  - { name: TestRemove_SymlinkEscapeNotFollowed, run: "go test ./internal/scratch -run '^TestRemove_SymlinkEscapeNotFollowed$' -count=1", timeoutSec: 120 }
  - { name: TestRemove_SymlinkAtTaskDirRemovesLinkOnly, run: "go test ./internal/scratch -run '^TestRemove_SymlinkAtTaskDirRemovesLinkOnly$' -count=1", timeoutSec: 120 }
  - { name: TestRemove_ReadOnlyFiles, run: "go test ./internal/scratch -run '^TestRemove_ReadOnlyFiles$' -count=1", timeoutSec: 120 }
  - { name: TestRemove_BusyRefused, run: "go test ./internal/scratch -run '^TestRemove_BusyRefused$' -count=1", timeoutSec: 120 }
  - { name: TestSweep_SkipsBusyThenRetries, run: "go test ./internal/scratch -run '^TestSweep_SkipsBusyThenRetries$' -count=1", timeoutSec: 120 }
  - { name: TestSweep_RemovesClosedKeepsLive, run: "go test ./internal/scratch -run '^TestSweep_RemovesClosedKeepsLive$' -count=1", timeoutSec: 120 }
  - { name: TestSweep_IgnoresNonNumeric, run: "go test ./internal/scratch -run '^TestSweep_IgnoresNonNumeric$' -count=1", timeoutSec: 120 }
  - { name: TestArchive_RemovesScratch, run: "go test ./internal/workspace -run '^TestArchive_RemovesScratch$' -count=1", timeoutSec: 300 }
  - { name: TestDelete_RemovesScratch, run: "go test ./internal/workspace -run '^TestDelete_RemovesScratch$' -count=1", timeoutSec: 300 }
  - { name: TestLaunchEnv_ScratchTmpdir_Claude, run: "go test ./internal/taskrunner -run '^TestLaunchEnv_ScratchTmpdir_Claude$' -count=1", timeoutSec: 300 }
  - { name: TestLaunchEnv_ScratchTmpdir_Codex, run: "go test ./internal/taskrunner -run '^TestLaunchEnv_ScratchTmpdir_Codex$' -count=1", timeoutSec: 300 }
  - { name: TestLaunchEnv_ScratchTmpdir_ACP, run: "go test ./internal/taskrunner -run '^TestLaunchEnv_ScratchTmpdir_ACP$' -count=1", timeoutSec: 300 }
  - { name: TestLaunchEnv_ScratchTmpdir_GLM, run: "go test ./internal/taskrunner -run '^TestLaunchEnv_ScratchTmpdir_GLM$' -count=1", timeoutSec: 300 }
  - { name: TestLaunchEnv_PinnedTmpdirWins, run: "go test ./internal/taskrunner -run '^TestLaunchEnv_PinnedTmpdirWins$' -count=1", timeoutSec: 300 }
  - { name: TestSharedCaches_On, run: "go test ./internal/taskrunner -run '^TestSharedCaches_On$' -count=1", timeoutSec: 300 }
  - { name: TestSharedCaches_Off, run: "go test ./internal/taskrunner -run '^TestSharedCaches_Off$' -count=1", timeoutSec: 300 }
  - { name: TestSharedCaches_DoesNotOverrideExisting, run: "go test ./internal/taskrunner -run '^TestSharedCaches_DoesNotOverrideExisting$' -count=1", timeoutSec: 300 }
  - { name: TestScratchRPC_RefusedForRunScoped, run: "go test ./internal/wsapi -run '^TestScratchRPC_RefusedForRunScoped$' -count=1", timeoutSec: 300 }
  - { name: TestScratchRPC_RefusedForPlugin, run: "go test ./internal/wsapi -run '^TestScratchRPC_RefusedForPlugin$' -count=1", timeoutSec: 300 }
  - { name: TestScratchRPC_UsageAndClean, run: "go test ./internal/wsapi -run '^TestScratchRPC_UsageAndClean$' -count=1", timeoutSec: 300 }
mutations:
  - { id: M1, file: internal/scratch/scratch.go, change: "remove with os.RemoveAll(filepath.EvalSymlinks(dir)) / follow symlinks instead of removing the link", expectFail: [TestRemove_SymlinkEscapeNotFollowed, TestRemove_SymlinkAtTaskDirRemovesLinkOnly] }
  - { id: M2, file: internal/scratch/scratch.go, change: "skip the chmod step before removal", expectFail: [TestRemove_ReadOnlyFiles] }
  - { id: M3, file: internal/scratch/scratch.go, change: "ignore the busy hook in Remove", expectFail: [TestRemove_BusyRefused] }
  - { id: M4, file: internal/scratch/scratch.go, change: "Sweep also removes non-numeric entries or live tasks", expectFail: [TestSweep_IgnoresNonNumeric, TestSweep_RemovesClosedKeepsLive] }
  - { id: M5, file: internal/wsapi/scratch.go, change: "drop the runScoped refusal in scratch.clean", expectFail: [TestScratchRPC_RefusedForRunScoped] }
  - { id: M6, file: internal/taskrunner/scratch_env.go, change: "append the scratch TMPDIR after the provider env instead of before", expectFail: [TestLaunchEnv_PinnedTmpdirWins] }
commands:
  - { id: C1, run: "task lint", timeoutSec: 900 }
  - { id: C2, run: "task test", timeoutSec: 1500 }
limits: { budgetUsd: 2, maxMinutes: 40 }
---

# Scratch cleanup validation contract

Isolated temp dirs only; no model call. Key assertion: nothing outside `scratch/<taskId>` is ever removed.
Spec: `docs/plans/active/task-scratch.md`.
