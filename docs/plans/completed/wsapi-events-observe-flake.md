# wsapi: `TestEventsObserve_OnlyAllowlistedTopics` flake (diagnosis and fix)

Size S. Test-only fix; the diagnosis shows no product bug.

## Diagnosis

- Failure: `only the subscribed allowlisted topic is delivered: 2 events` (CI on several PRs; reproduced locally with `go test ./internal/wsapi -race -count=15 -run TestEventsObserve`, about 2 of 15 runs, never without `-race`).
- The two events (logged while debugging): `plugin.updated {"id":"obs","globalEnabled":true}` with seq 1, then `task.deleted` with seq 2.
- Cause: the test's first loop subscribed the plugin to EVERY allowlisted topic (each allowlisted `events.subscribe` succeeds and stays), including `plugin.updated`. A plugin's process status changes are reported through the host's coalescing notifier, which runs `pluginEvents.changed` on its own goroutine, so the `plugin.updated` for "process reached running" can be published after the test has already subscribed. `waitRunning` only polls the status, not the notice. Under `-race` the goroutine is slow enough to land after the subscribe. That event is legitimate for a subscriber of `plugin.updated`: the assertion was wrong, not the daemon.
- The `crash: recovered a panic in wsapi.pumpEvents` line in the same CI log is NOT a daemon panic: `TestCrashWSPumpPanicClosesConnection` panics the pump on purpose (a transport that panics on `"event"` frames) and prints exactly that line. It is a different test with no relation to the failure.

## Fix

Check delivery first, with the plugin subscribed to the one topic under test (`task.deleted`), and run the refusal loop afterwards. Regression: the status notice is injected by hand (`pluginEvents.changed`) after the subscribe, so the late notice is no longer timing: it must not arrive. A second test pins the premise (a subscriber of `plugin.updated` does receive the notice).

Scope note: the diagnosis is the observed second event (`plugin.updated`); it is not an exhaustive audit of every async publisher. With the plugin subscribed to `task.deleted` alone, only a second `task.deleted` could break the assertion, and nothing in the test env publishes one. The refusal half keeps its original strength: after the loop and the mixed request, forbidden topics are published and must never arrive (a marker event orders the check).

## Validation

See the PR body.
