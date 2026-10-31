---
v: 1
milestone: "Fleet F3b: caller watches and MCP write tools (ADR-0048)"
base: origin/develop
planRef: docs/plans/active/fleet-hosts.md
acs:
  - id: AC-21
    text: "Durable finish notice: mirror and watch row in one commit; after a caller restart the watcher resumes and a run finished meanwhile is delivered exactly once, remote text untrusted <= 1 KiB; 3 failed observations = status unknown never finished (connecting not counted); removed host = host unavailable; unsent prompt = no watch; local write failure is retry-safe by idempotencyKey."
    covers: { tests: [TestFleetCallerRestartStillGetsFinishNotification, TestFleetWatchObservationBackoffAndConnectingNotCounted, TestFleetWatchThreeFailuresStatusUnknownNeverFinished, TestFleetWatchHostRemovedUnavailable, TestFleetWatchFinishDeliveredOnce, TestFleetWatchUnsentPromptNoWatch, TestFleetLocalWriteFailureReportsAndRetrySafe, TestEnqueueChildEventClosingWatchOnce] }
  - id: AC-22
    text: "MCP task_new/task_send/task_stop/task_wait accept host; results carry host and globalId; a Peer seat and a remote-origin task get no fleet tools and a fleet call from them is refused locally with hop_limit."
    covers: { tests: [TestFleetPeerSeatGetsNoFleetTools, TestRemoteWriteRefusedForPeerSeatAndRemoteOrigin, TestMCPHostTaskNewRequiresProviderModelPrompt, TestMCPRemoteWriteTypedErrorsCodeFirst] }
  - id: AC-1
    text: "With fleet off, remote.write is not registered; the wsapi method map golden for fleet off is unchanged."
    covers: { tests: [TestWsapiMethodMapGoldenFleetOff, TestRemoteWriteAbsentWhenFleetOff] }
  - id: AC-9
    text: "remote.write is local only: refused over the relay and for a host principal."
    covers: { tests: [TestRemoteWriteNotOverRelayNotHostPrincipal] }
tests:
  - { name: TestFleetCallerRestartStillGetsFinishNotification, run: "go test ./internal/fleet/watch -run '^TestFleetCallerRestartStillGetsFinishNotification$' -count=1", timeoutSec: 300 }
  - { name: TestFleetWatchObservationBackoffAndConnectingNotCounted, run: "go test ./internal/fleet/watch -run '^TestFleetWatchObservationBackoffAndConnectingNotCounted$' -count=1", timeoutSec: 300 }
  - { name: TestFleetWatchThreeFailuresStatusUnknownNeverFinished, run: "go test ./internal/fleet/watch -run '^TestFleetWatchThreeFailuresStatusUnknownNeverFinished$' -count=1", timeoutSec: 300 }
  - { name: TestFleetWatchHostRemovedUnavailable, run: "go test ./internal/fleet/watch -run '^TestFleetWatchHostRemovedUnavailable$' -count=1", timeoutSec: 300 }
  - { name: TestFleetWatchFinishDeliveredOnce, run: "go test ./internal/fleet/watch -run '^TestFleetWatchFinishDeliveredOnce$' -count=1", timeoutSec: 300 }
  - { name: TestEnqueueChildEventClosingWatchOnce, run: "go test ./internal/store -run '^TestEnqueueChildEventClosingWatchOnce$' -count=1", timeoutSec: 300 }
  - { name: TestFleetWatchUnsentPromptNoWatch, run: "go test ./internal/wsapi -run '^TestFleetWatchUnsentPromptNoWatch$' -count=1", timeoutSec: 300 }
  - { name: TestFleetLocalWriteFailureReportsAndRetrySafe, run: "go test ./internal/wsapi -run '^TestFleetLocalWriteFailureReportsAndRetrySafe$' -count=1", timeoutSec: 300 }
  - { name: TestRemoteWriteRefusedForPeerSeatAndRemoteOrigin, run: "go test ./internal/wsapi -run '^TestRemoteWriteRefusedForPeerSeatAndRemoteOrigin$' -count=1", timeoutSec: 300 }
  - { name: TestRemoteWriteAbsentWhenFleetOff, run: "go test ./internal/wsapi -run '^TestRemoteWriteAbsentWhenFleetOff$' -count=1", timeoutSec: 300 }
  - { name: TestRemoteWriteNotOverRelayNotHostPrincipal, run: "go test ./internal/wsapi -run '^TestRemoteWriteNotOverRelayNotHostPrincipal$' -count=1", timeoutSec: 300 }
  - { name: TestWsapiMethodMapGoldenFleetOff, run: "go test ./internal/wsapi -run '^TestWsapiMethodMapGoldenFleetOff$' -count=1", timeoutSec: 300 }
  - { name: TestFleetPeerSeatGetsNoFleetTools, run: "go test ./cmd/smind -run '^TestFleetPeerSeatGetsNoFleetTools$' -count=1", timeoutSec: 300 }
  - { name: TestMCPHostTaskNewRequiresProviderModelPrompt, run: "go test ./cmd/smind -run '^TestMCPHostTaskNewRequiresProviderModelPrompt$' -count=1", timeoutSec: 300 }
  - { name: TestMCPRemoteWriteTypedErrorsCodeFirst, run: "go test ./cmd/smind -run '^TestMCPRemoteWriteTypedErrorsCodeFirst$' -count=1", timeoutSec: 300 }
mutations:
  - { id: M1, file: internal/fleet/watch/watch.go, change: "Stop() closes every open watch row", expectFail: [TestFleetCallerRestartStillGetsFinishNotification] }
  - { id: M2, file: internal/fleet/watch/watch.go, change: "failure limit 3 becomes 4 (or the unknown notice says finished)", expectFail: [TestFleetWatchThreeFailuresStatusUnknownNeverFinished] }
  - { id: M3, file: internal/fleet/watch/watch.go, change: "a connecting attempt counts as a failure", expectFail: [TestFleetWatchObservationBackoffAndConnectingNotCounted] }
  - { id: M4, file: internal/store/fleet_notify.go, change: "drop the open-only claim (UPDATE without state='open' check)", expectFail: [TestEnqueueChildEventClosingWatchOnce, TestFleetWatchFinishDeliveredOnce] }
  - { id: M5, file: internal/wsapi/fleet_remote_write.go, change: "an unsent create still writes the watch", expectFail: [TestFleetWatchUnsentPromptNoWatch] }
  - { id: M6, file: internal/wsapi/fleet_remote_write.go, change: "drop the seat check", expectFail: [TestRemoteWriteRefusedForPeerSeatAndRemoteOrigin] }
---

F3b caller write path: see the plan's "F3b design".
