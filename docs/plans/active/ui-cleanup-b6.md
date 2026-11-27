# UI cleanup B6: UI for RPCs that already exist

Status: active. Size M. Web only: no daemon, wire or `mobile/` change, no new RPC. Source: the CLI-only controls audit (relay device approval, doctor, push, automations pause for a remote client). Each claim was checked in the Go code; the verdicts are in D0.

## D0. What the code says (claims verified, two corrected)

| Claim | Verdict |
|---|---|
| `relay.device.list/approve/revoke/rename/window` exist, local-only | Confirmed (`internal/wsapi/relay_devices.go`). They are registered only when the daemon runs the relay bridge: without it the method does not exist. A relay session gets `forbidden` ("relay.device.* is not available over the relay"). |
| `relay-connection-status.tsx` prints `smind relay devices approve <id>` | Confirmed, but that text is for a **relay client** (Settings -> Connections of the phone or the desktop that is waiting for approval). It is not the daemon owner's view; it stays. The owner's device list is new, in Settings -> Daemon. |
| `doctor.sessions` is a read-only probe | Confirmed, and it returns **counts only**: `{runs, withRef, readable, tooLarge, gone, checked}`. It does not return crash reports or a role-pack line (those are CLI-side). The Diagnostics group shows exactly the counts; nothing is invented. Local human only (`requireLocalHuman`). |
| `push.status` gives status rows in Settings | **Not usable from the local web or desktop client.** `push.status` goes through `pushGuard`, which needs `rc.deviceID` (a paired relay device): the local `/ws` connection gets `forbidden` ("push.* needs a paired relay device"). It answers only for the calling device's own registration. So the Settings group cannot use it. What IS available with no new RPC: `relay.device.list` carries a `push` boolean per device (is it registered for push), and `push.test` (local-only, no params = every registered device, empty `results` when there is none). The Mobile push group is built on those two. A status row needing `push.status` over the local connection would need a new RPC: **reported, not built**. |
| Automations pause/resume for a relay client uses the existing per-automation RPCs | **Partly.** ADR-0044: a relay session may call `automation.disable` (a brake) and nothing else that mutates; `automation.enable` and `automation.resume` are human + local only (`automationHumanLocal`), by design ("a brake, never an accelerator"). The Automations page already has a per-automation switch and a Resume button, and already keeps Disable available when `canManage` is false. So: Pause (disable) works for a relay client today; Resume/Enable over relay is refused on purpose. **Not changed; the part that would loosen the guard is a security decision and is reported.** B6 adds tests for the existing behaviour, not UI. |

## Decisions

- **D1 Devices.** A "Paired devices" group in Settings -> Daemon (the Daemon section exists in the desktop build only: the web build has no device list, reported). Rows in a `SettingsList`: name (label, or "Unnamed device"), the short device id (first 8 characters of `device_id`; the public key `device_pub` is never rendered), a state chip (Pending, Approved, Revoked), "Connected now" when `connected`, last seen (relative, from `last_seen`), and "Mobile push on/off" from `push` when present. Actions: Approve (pending), Revoke (approved), Rename (any state), each Approve/Revoke behind a `ConfirmDialog` that names the device. A pairing-window control: "Open pairing window" (no params = the daemon default) and, while open, "Open until <time>, <n> device(s) left". `relay.device.window` returns the window; `relay.device.list` returns `window` too. Errors are `daemonSentence` of the daemon's message. No polling: one `relay.device.list` on mount, one after every mutation, and a Refresh button.
- **D2 Relay refusal.** When the desktop connection kind is `relay`, the group shows "Manage paired devices on the daemon's machine." and makes **no** `relay.device.*` call. On any other connection the list is requested once; a `forbidden` answer or a missing method (no relay bridge) becomes the same read-only copy or "Relay is not set up on this daemon." respectively, never a raw error.
- **D3 Diagnostics.** A "Diagnostics" group in Settings -> Daemon: a "Run checks" button (one `doctor.sessions` per click, a mutation, no polling, no query), then rows: Finished runs, With a provider session, Readable, Too large, Gone, Checked. Idle copy says what it checks. Running disables the button. An error is a sentence. A relay connection shows the same read-only copy as D2 and does not call.
- **D4 Mobile push.** A "Mobile push" group in Settings -> Notifications (web and desktop builds): one `relay.device.list` read gives the count of devices with a push registration; a "Send test notification" button calls `push.test` with no params. Results: no registered device -> "No device has push set up yet. Open sMind on your phone to register one." (not an error); per device sent / the daemon's code as a sentence (`not_registered`, `not_approved`, `send_failed`, `seal_failed`, or the provider's code, mapped to words), device shown by its short id or label. `push disabled on this daemon` (code `push_disabled`) is a sentence. No token, key or provider payload is ever shown (the wire returns none). A relay connection or a `forbidden` answer shows the read-only copy; no call is made from a known relay connection.
- **D5 Automations.** No UI change (see D0). Tests pin the relay behaviour.

## Acceptance criteria

- AC1 The device list renders every state (pending, approved, revoked), the connected flag, last seen and the push flag, and shows an empty state when there is none; no key bytes are rendered.
- AC2 Approve and Revoke each open a confirm that names the device and call exactly one `relay.device.approve` / `relay.device.revoke` with `{id}` after the confirm; nothing is sent before it; Cancel sends nothing.
- AC3 Rename sends `relay.device.rename` `{id, label}`; an empty label is refused client-side.
- AC4 The pairing window control sends `relay.device.window` and shows the remaining slots and the expiry.
- AC5 Over a relay connection (and on `forbidden`) the group is read-only with the "daemon's machine" copy, shows no Approve/Revoke/Rename/Open buttons, and a known relay connection makes no call.
- AC6 Doctor: one `doctor.sessions` per click, rows for the six counts, an error sentence, no polling; running disables the button.
- AC7 Push: the group reads one list, shows "no device" without error, sends one `push.test` per click, maps result codes to sentences, shows no secrets, and is read-only over relay.
- AC8 A relay client keeps Disable on the Automations page and gets a refusal sentence (not a crash) for Enable/Resume.
- AC9 Mobile push on the real e2e daemon (no relay bridge) is honest: no error banner, a clear "no device" state.
- AC12 No daemon, wire or mobile change; hygiene green.

## Test scenarios (named)

Unit (`web/packages/ui`):
- `relay-devices-group.test.tsx`: `relay_devices_list_renders_all_states`, `relay_devices_empty_state`, `relay_devices_never_render_key_bytes`, `relay_devices_approve_confirms_then_calls`, `relay_devices_approve_cancel_sends_nothing`, `relay_devices_revoke_confirms_then_calls`, `relay_devices_rename_sends_label`, `relay_devices_rename_empty_label_refused`, `relay_devices_pairing_window_opens_and_shows_remaining`, `relay_devices_relay_connection_is_read_only_and_makes_no_call`, `relay_devices_forbidden_answer_becomes_read_only_copy`, `relay_devices_no_bridge_says_relay_not_set_up`, `relay_devices_error_is_a_sentence`.
- `use-relay-devices.test.tsx`: `relay_devices_one_list_call_on_mount_and_after_a_change`.
- `diagnostics-group.test.tsx`: `doctor_runs_once_per_click`, `doctor_shows_the_six_counts`, `doctor_error_is_a_sentence`, `doctor_does_not_poll`, `doctor_button_disabled_while_running`, `doctor_relay_connection_is_read_only_and_makes_no_call`.
- `mobile-push-group.test.tsx`: `push_group_counts_registered_devices`, `push_test_with_no_device_is_not_an_error`, `push_test_results_map_codes_to_sentences`, `push_test_shows_no_secrets`, `push_test_one_call_per_click`, `push_group_relay_connection_is_read_only_and_makes_no_call`, `push_disabled_is_a_sentence`.
- `automations-page.test.tsx`: `automation_relay_client_can_disable_but_enable_and_resume_are_refused_with_a_sentence`.

E2E (`web/e2e/specs/ui-cleanup-b6.spec.ts`, the web harness): the Daemon section (devices, diagnostics) exists in the desktop build only, so the web e2e cannot reach it; those two groups are covered by the unit tests and by REAL-WINDOW frames of the desktop audit build. The web e2e covers what the web build has: `UC6-01` Mobile push on the real e2e daemon with no relay bridge and no device (the group says so and "Send test notification" is honest, not an error), `UC6-02` Mobile push with a stubbed `relay.device.list` (one push-registered device) and a stubbed `push.test` result (labelled stub, `page.routeWebSocket`), `UC6-03` Mobile push over a connection that answers `forbidden` (stub): read-only copy, no button, `UC6-04` the Automations page with a stub that answers `forbidden` to `automation.enable` / `automation.resume` and accepts `automation.disable` (a relay client): Disable works, Enable shows the refusal as a sentence.

Mutations on committed code (the validator re-runs them): M1 approve sent without the confirm -> `relay_devices_approve_confirms_then_calls`; M2 the read-only view renders Approve -> `relay_devices_relay_connection_is_read_only_and_makes_no_call`; M3 doctor polls -> `doctor_does_not_poll`; M4 a key byte rendered -> `relay_devices_never_render_key_bytes`.

## Staffing

GLM peers write every line of code from step-by-step briefs, one commit per step, at most two at a time. Lead: this spec and contract, review, mutations, checks, frames, PR body.

## Progress

- Spec written (this commit). Code: not started.

## Validation

- Pending.
