---
v: 1
milestone: "Office mode O3: binary-safe file.stat / file.readRange, daemon.info.files limits"
base: origin/develop
planRef: docs/plans/active/office-mode.md
acs:
  - id: AC12
    text: "file.stat returns size, mtime, kind, mime, ext, mismatch, previewable from the first bytes (not the extension); a .pdf that is text or a .txt that is a PDF is mismatch and not previewable; a directory, FIFO or device is invalid_params; a missing file is not_found."
    covers: { tests: [TestFileStat_SniffsContentNotExtension, TestFileStat_RejectsNonRegular, TestFileStat_RejectsFIFO, TestFileStatAndReadRange_OverTheWire], frames: [] }
  - id: AC13
    text: "file.readRange returns base64 bytes [offset, offset+n) with eof and mtime, n clamped to the transport chunk ceiling; a 3 MiB random file reassembles byte-identically with eof exactly once; an offset past EOF is length 0 and eof; negative values are invalid_params."
    covers: { tests: [TestFileReadRange_RoundTrip, TestFileReadRange_ClampsAndBounds, TestFileReadRange_ChunkCeilingPerTransport, TestFileStatAndReadRange_OverTheWire], frames: [] }
  - id: AC14
    text: "../ escapes, absolute paths outside the worktree and a symlink pointing outside are refused; the file is opened once and checked on the open descriptor (regular, size <= maxReadBytes else too_large); a grown file never yields an oversized read; an ifMtime mismatch is conflict."
    covers: { tests: [TestFileReadRange_ContainmentAndTOCTOU, TestFileReadRange_IfMtimeConflict, TestFileReadRange_ErrorsNameNoContentOrAbsolutePath, TestFileReadRange_WindowsPathForms], frames: [] }
  - id: AC15
    text: "daemon.info.files = {maxReadBytes 50 MiB, maxChunkBytes 4 MiB direct / 512 KiB relay, previewKinds [pdf]}; the largest relay response fits one relay message."
    covers: { tests: [TestDaemonInfoFilesCapability, TestFileRangeRelayChunkFitsFrame, TestDaemonInfoReturnsVersionCommitAndPlatform], frames: [] }
  - id: AC16
    text: "Both methods are in the handler map, on the plugin never-list and in no plugin capability group; neither logs params or bytes and no error text carries file content or an absolute path."
    covers: { tests: [TestFileMethods_NotPluginReachableAndNoContentInLogs, TestWsapiMethodMapGoldenFleetOff, TestPluginSession_CapabilityTable_EveryMethodExists], frames: [] }
tests:
  - { name: TestFileStat_SniffsContentNotExtension, run: "go test ./internal/files -run '^TestFileStat_SniffsContentNotExtension$' -count=1", timeoutSec: 300 }
  - { name: TestFileStat_RejectsNonRegular, run: "go test ./internal/files -run '^TestFileStat_RejectsNonRegular$' -count=1", timeoutSec: 300 }
  - { name: TestFileStat_RejectsFIFO, run: "go test ./internal/files -run '^TestFileStat_RejectsFIFO$' -count=1", timeoutSec: 300 }
  - { name: TestFileReadRange_RoundTrip, run: "go test ./internal/files -run '^TestFileReadRange_RoundTrip$' -count=1", timeoutSec: 300 }
  - { name: TestFileReadRange_ClampsAndBounds, run: "go test ./internal/files -run '^TestFileReadRange_ClampsAndBounds$' -count=1", timeoutSec: 300 }
  - { name: TestFileReadRange_ContainmentAndTOCTOU, run: "go test ./internal/files -run '^TestFileReadRange_ContainmentAndTOCTOU$' -count=1", timeoutSec: 300 }
  - { name: TestFileReadRange_IfMtimeConflict, run: "go test ./internal/files -run '^TestFileReadRange_IfMtimeConflict$' -count=1", timeoutSec: 300 }
  - { name: TestFileReadRange_ErrorsNameNoContentOrAbsolutePath, run: "go test ./internal/files -run '^TestFileReadRange_ErrorsNameNoContentOrAbsolutePath$' -count=1", timeoutSec: 300 }
  - { name: TestFileReadRange_WindowsPathForms, run: "go test ./internal/files -run '^TestFileReadRange_WindowsPathForms$' -count=1", timeoutSec: 300 }
  - { name: TestFileStatAndReadRange_OverTheWire, run: "go test ./internal/wsapi -run '^TestFileStatAndReadRange_OverTheWire$' -count=1", timeoutSec: 300 }
  - { name: TestFileReadRange_ChunkCeilingPerTransport, run: "go test ./internal/wsapi -run '^TestFileReadRange_ChunkCeilingPerTransport$' -count=1", timeoutSec: 300 }
  - { name: TestDaemonInfoFilesCapability, run: "go test ./internal/wsapi -run '^TestDaemonInfoFilesCapability$' -count=1", timeoutSec: 300 }
  - { name: TestDaemonInfoReturnsVersionCommitAndPlatform, run: "go test ./internal/wsapi -run '^TestDaemonInfoReturnsVersionCommitAndPlatform$' -count=1", timeoutSec: 300 }
  - { name: TestFileRangeRelayChunkFitsFrame, run: "go test ./internal/wsapi -run '^TestFileRangeRelayChunkFitsFrame$' -count=1", timeoutSec: 300 }
  - { name: TestFileMethods_NotPluginReachableAndNoContentInLogs, run: "go test ./internal/wsapi -run '^TestFileMethods_NotPluginReachableAndNoContentInLogs$' -count=1", timeoutSec: 300 }
  - { name: TestWsapiMethodMapGoldenFleetOff, run: "go test ./internal/wsapi -run '^TestWsapiMethodMapGoldenFleetOff$' -count=1", timeoutSec: 300 }
  - { name: TestPluginSession_CapabilityTable_EveryMethodExists, run: "go test ./internal/wsapi -run '^TestPluginSession_CapabilityTable_EveryMethodExists$' -count=1", timeoutSec: 300 }
mutations:
  - { id: M1, file: internal/files/binary.go, change: "classify by extension instead of content (return KindPDF for .pdf)", expectFail: [TestFileStat_SniffsContentNotExtension] }
  - { id: M2, file: internal/files/binary.go, change: "drop the size > MaxReadBytes check", expectFail: [TestFileReadRange_ContainmentAndTOCTOU] }
  - { id: M3, file: internal/files/binary.go, change: "do not clamp n to MaxChunkBytes", expectFail: [TestFileReadRange_ClampsAndBounds, TestFileReadRange_ChunkCeilingPerTransport] }
  - { id: M4, file: internal/files/binary.go, change: "ignore ifMtime", expectFail: [TestFileReadRange_IfMtimeConflict] }
  - { id: M5, file: internal/files/binary.go, change: "make refuseUnsafeForm a no-op", expectFail: [TestFileReadRange_WindowsPathForms] }
  - { id: M6, file: internal/wsapi/files_binary.go, change: "fileChunkLimit always returns the direct ceiling", expectFail: [TestDaemonInfoFilesCapability, TestFileReadRange_ChunkCeilingPerTransport, TestFileRangeRelayChunkFitsFrame] }
commands:
  - { id: C1, run: "go vet ./internal/files ./internal/wsapi", timeoutSec: 600 }
  - { id: C2, run: "go test ./internal/files ./internal/wsapi -count=1", timeoutSec: 1800 }
limits: { budgetUsd: 2, maxMinutes: 30 }
---

# Office mode O3 validation contract

Daemon-only: `internal/files/binary.go`, `internal/wsapi/files_binary.go`, two handler-map entries, `daemon.info.files`, and the method golden. No web, desktop or `internal/server` change.
`TestFileStat_RejectsFIFO` runs on non-Windows builds only; on Windows the FIFO row is covered by `TestFileReadRange_WindowsPathForms` (reserved device names). The symlink subtests skip where the OS refuses symlink creation without privilege (a Windows host without Developer Mode): the validator should state which ran.
