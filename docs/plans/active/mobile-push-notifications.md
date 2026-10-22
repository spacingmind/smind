# Mobile push notifications

Size: **XL (10)** by the protocol table — scope 2 (daemon + mobile + native config), PRs 2 (seven, with ordering), risk 2 (secrets, crypto, a
new outbound network path), decisions 2 (ADR-0040, eight open questions), verification 2 (a real Android device, FCM through Expo).
Recommendation: run it as **two L items with disjoint write scopes and a frozen contract** (daemon: PRs 2–4; mobile: PRs 5–7), one Peer each, because the
wire and the crypto vector are fixed in ADR-0040 and the vector is the only thing the mobile side needs from the daemon side.
Every PR is **non-stacked off `develop`** (a PR that needs an earlier PR's code waits for it to merge). iOS (PR 8) is blocked on a Human decision.

Spec for [ADR-0040](../../decisions/0040-mobile-push-notifications.md) (read it first: privacy model, wire, registration lifecycle, event rules, platform limits).
Status: **ADR-0040 Accepted 2026-10-22 (H1–H8 as recommended; no credentials until the Human supplies them, so build against fakes).** Implementation starts with the daemon PRs; Peers run as smind tasks (dogfood).

## Goal

The phone gets a notification, with the real text, when a run needs permission approval, hits the turn cap and needs Continue, or finishes/errors,
even with the app backgrounded or swiped away. Tapping it opens that task. Expo, Google and Apple never see a task title, prompt, file name or summary.

## Scope and PRs

In scope: Android end to end (Expo-brokered FCM, development build), the platform-neutral daemon and TypeScript pieces, the `smind push test` command,
operator docs, a native-verification checklist. Out of scope: iOS remote push and the Notification Service Extension (PR 8, blocked on H2), the
`fcm`/`apns`/`unifiedpush`/`relay` senders, retracting a resolved permission's notification, a per-kind settings screen beyond the single opt-in, desktop (Tauri) notifications,
and anything in the M6 create-task, prompt-attachments and git/PR-state work (other Leads: do not touch `mobile/app/new-task.tsx`, `mobile/src/newTask*`, `internal/attach`, `internal/forge`, `internal/gitstate`).

| PR | Content | Size | Peer | Depends on |
|---|---|---|---|---|
| 1 | **Docs**: ADR-0040 + this plan + a pointer in ROADMAP Phase 3 | S | — | — |
| 2 | **Daemon core, no wiring**: `internal/push` — `Seal`/`Open` (ChaCha20-Poly1305, AAD `smind-push-v1‖device_id`, padding), the shared vector `internal/push/testdata/push_seal_vector.json` (`-update`), `Registry` (file store, 0600, atomic, token takeover, kinds), `Sender` interface + `expo` sender (batch 100, retry/backoff, ticket handling, injectable base URL), message builder (Android data-only / iOS generic alert + `mutableContent`) | M | peer-sonnet (crypto, error handling) + peer-glm (registry, tests) | PR 1 merged |
| 3 | **Daemon wiring**: device id on `wsapi` conns (`ServeDevice`), `push.register/unregister/status`, event consumer on the bus (rules, dedupe, rate limit, approved-at-send check), config `push.*`, hook in the hub | M | peer-sonnet (concurrency, wsapi) + peer-glm (config, rules table tests) | PR 2 merged |
| 4 | **Operator surface**: `push.test` (local only) + `smind push test [device]`, `smind relay devices ls` shows a push column, `docs/homelab.md` section (what leaves the daemon, config), logging audit test | S | peer-glm | PR 3 merged |
| 5 | **Mobile logic, no native deps**: `mobile/src/push/` — `open.ts` (vector-tested trial decrypt, padding strip, 24 h age check), `keys.ts` (secure-store push key per connection, deleted with the connection), `register.ts` (opt-in flow against `push.*`), `target.ts` (`resolveNotificationTarget`, pending-target store released by `routeGate`), `foreground.ts` (suppression rule); all vitest | M | peer-glm (detailed brief) | PR 2 merged (the vector) **and** PR #480 merged (shared `mobile/package.json`, `app/_layout.tsx`) |
| 6 | **Mobile native wiring (dev build)**: `expo install expo-notifications expo-task-manager expo-device`, config plugin and `app.config` (`android.package`, `extra.eas.projectId`, `googleServicesFile` from env), custom entry that registers the background task before `expo-router/entry`, Android channel `attention`, Settings → Notifications row, tap/cold-start routing, web no-op | L | peer-sonnet (headless task, entry, native config) + peer-glm (settings row) | PR 5 merged, H3/H5 answered |
| 7 | **Verification + docs**: run the native checklist (AC24) on a real Android device with the Human's Firebase/Expo project, record results, mobile docs ("what works without the Apple program"), close or update this plan | S | Lead + Human | PRs 3, 4, 6 merged |
| 8 | **iOS (blocked on H2)**: Notification Service Extension target (config plugin), CryptoKit decrypt, keychain access group, APNs credential via EAS, the iOS half of AC24 | L | peer-sonnet | H2 decided, PR 7 |

Order: 1 → 2 → (3 → 4) ‖ (5 → 6, after #480) → 7 → 8. The daemon track and the mobile logic only meet at the vector file and the frozen wire in ADR-0040 §3.
Each row's brief is the table row plus its ACs and scenarios below, copied into a Peer brief per `docs/WORKSPACE_PROTOCOL.md` (exact files, signatures, test names, commands).

Write scopes: PRs 2–4 own `internal/push/`, `internal/wsapi/push*.go`, `internal/config` (the `push` block only), `cmd/smind` (the `push` subcommand), `internal/relay/bridge` (passing the device id) and `docs/homelab.md`; PRs 5–7 own `mobile/src/push/`, `mobile/index.ts` (new entry), and the push lines of `mobile/app.json`/`mobile/package.json`/`mobile/app/_layout.tsx`/settings.

## Acceptance criteria

Daemon

1. `push.Seal` output for the fixed seeds equals `internal/push/testdata/push_seal_vector.json` byte for byte; `Open` returns the plaintext; the plaintext is padded to a multiple of 256 bytes and ≤ 1 KiB.
2. `Open` fails (typed error, no partial plaintext) for: another device id (AAD), a flipped ciphertext bit, a flipped nonce bit, an unsupported `v`, a truncated blob.
3. `push.register` over a relay-served approved device stores `{token, provider, platform, key, kinds}` under **that session's device id**; the id cannot be set by params; a local `/ws` connection gets `forbidden`.
4. `push.register` validates: unknown provider → `unsupported_provider`; a token that is not `ExponentPushToken[…]`/`ExpoPushToken[…]` → `invalid_token`; a key that is not 32 bytes → `invalid_key`; unknown kinds → `invalid_kinds`; daemon `push.enabled: false` → `push_disabled` with no outbound call ever.
5. A token registered under device B is removed from device A (token takeover); `push.unregister` removes the caller's registration; `push.status` never returns the token or the key.
6. Revoking a device stops pushes to it even if the registry file is edited offline (the sender checks `approved` at send time); a pending device cannot register. (`relay host rotate` is relay-side and does not touch daemon registrations.)
7. `push-registrations.json` is mode 0600, written atomically, survives a restart, and a corrupt or unknown-version file is an error, not a silent reset.
8. Event rules (ADR-0040 §4): `permission.pending` → one `permission` push per requestId per device; `run.status` done at `needs-continue` → `needs_continue` (and no `done`); done/error for root tasks only; `stopped`, system tasks and `Purpose` runs → nothing; a device whose `kinds` excludes the kind gets nothing.
9. Rate limit: 7 `done` events in a minute for one device send 6; `permission` and `needs_continue` are never dropped.
10. Multi-device: three registered devices get three differently sealed payloads in one batch; a device with a bad key does not stop the others.
11. **Content-free on the wire:** the HTTP body sent to the Expo endpoint contains, besides the ciphertext, only `to`, `priority`, `ttl`, `channelId` and (iOS) a fixed generic `title`/`body` with `mutableContent`; it contains none of the task title, summary, task id, run id or kind in clear.
12. Expo sender: ≤ 100 messages per request; a ticket error `DeviceNotRegistered` removes that registration; `InvalidCredentials`/`MismatchSenderId` keep it and log a non-sensitive error; 429/5xx/network errors retry at most 3 times with backoff then drop; a 400 is not retried; the optional access token is sent as a bearer header and never logged.
13. Logs, errors and events contain no token, key, title or body (a test greps captured log output after a full send).
14. `smind push test` sends a `kind: "test"` push to one or all registered devices and reports per-device results, over the local `/ws` only; it never calls a model.
15. No test, CI job or default config makes a call to `exp.host`; the base URL is injectable and tests use `httptest`.

Mobile

16. The TypeScript `open` reproduces the Go vector's plaintext; trial decryption picks the right key out of several; a blob no stored key opens yields the **generic** notification (never an error, never raw data); a payload older than 24 h is dropped.
17. Registration is opt-in (nothing is registered or prompted at launch); enabling requests the OS permission, creates the `attention` channel **before** the token/permission request on Android, registers token + a new key over `push.register`; a token rotation re-registers; disconnecting/`resetIdentity` deletes the connection's key and unregisters.
18. The background task is defined at module scope in the entry file; for a data-only message it decrypts and shows a local notification with the real title/body, identifier = event id, so a duplicate replaces rather than stacks; if decryption fails it still shows the generic notification.
19. Tap routing: `resolveNotificationTarget` accepts `{taskId, chatId?}` with a positive integer `taskId` and ignores anything else; a cold-start tap is held until `routeGate` is `app` (including after the daemon-password prompt) and navigated exactly once to `/task/<id>`; `clearLastNotificationResponseAsync` runs after handling.
20. Foreground: no banner for the task whose detail screen is open on a live connection; other tasks still notify.
21. The web build (Playwright smoke target) is unaffected: the push module is a no-op on `Platform.OS === "web"`; `npx tsc --noEmit`, `npm test` and `npm run smoke:web` pass.
22. No new dependency enters Expo Go-only flows: Expo Go still boots the app (push is feature-detected and shows "needs a development build" in Settings).
23. `grep` of the mobile sources finds no use of `getDevicePushTokenAsync`, no `content-available`/`contentAvailable`, and no push token in logs.

Verification (real device; manual, recorded under Validation)

24. **Native checklist on a real Android device with a development build** (handed to the mobile Lead to merge into their native-verification checklist): (a) the permission prompt appears on Android 13+ only after the channel exists; (b) a token is obtained and `push.register` succeeds against a real daemon through a real relay; (c) `smind push test` shows a notification with the test text with the app in the **foreground** (suppressed or shown per AC20), **backgrounded**, **swiped from recents**, and after `adb shell dumpsys deviceidle force-idle` (Doze); (d) a force-stopped app shows nothing until opened — expected, documented; (e) a real run needing permission, a run hitting the turn cap, and a finished and an errored run each notify with the right text; (f) a cold-start tap opens the task, and with a **daemon password** shows the password prompt first, then the task; (g) revoke → no further pushes; re-pair → none until re-registered; (h) two phones on one daemon both notify; (i) the decrypted text never appears in the daemon's outgoing request (check the daemon debug log of byte counts, not content); (j) background task reads the push key from secure-store in the headless context (an unproven native fact, ADR-0040 Consequences).
25. Everything above is done with the Human's own Firebase/Expo project; no agent creates an account or handles the FCM service-account key.

## Test scenarios

Go (`internal/push`, `internal/wsapi`, `internal/relay/bridge`, `cmd/smind`)

- `TestSealOpenVector` — AC1; `TestSealPadsToBucket` — padded length is a multiple of 256, ≤ 1 KiB; `TestOpenRejects{WrongDeviceID,FlippedCiphertext,FlippedNonce,BadVersion,Truncated}` — AC2.
- `TestRegistryPersistsAtomically0600`, `TestRegistryCorruptFileIsError`, `TestRegistryTokenTakeover`, `TestRegistryRemoveOnUnregister` — AC5, AC7.
- `TestPushRegisterBindsToSessionDeviceID` (a params `deviceId` is ignored), `TestPushRegisterForbiddenOnLocalWS`, `TestPushRegisterValidation` (table: provider, token shape, key length, kinds), `TestPushRegisterDisabled` (no sender call), `TestPushStatusRedactsSecrets` — AC3–AC5.
- `TestRevokeStopsPushes`, `TestRevokedByFileEditStopsPushes` (approved-at-send check), `TestPendingDeviceCannotRegister` — AC6.
- `TestPushRules` (table: permission, needs-continue, done root, done child, error root, stopped, system task, `Purpose` run, kinds filter) — AC8; `TestPermissionDedupePerRequest`; `TestDoneErrorRateLimit` + `TestPermissionNeverRateLimited` — AC9.
- `TestMultiDeviceSealedSeparately` (three payloads differ; one bad registration is skipped) — AC10.
- `TestExpoRequestHasNoPlaintext` (marshals a real request for a known title/summary/task id and asserts none occurs in the body; Android body has no `title`/`body`, iOS body has the fixed generic text) — AC11.
- `TestExpoBatching100`, `TestExpoTicketDeviceNotRegisteredRemoves`, `TestExpoInvalidCredentialsKeepsRegistration`, `TestExpoRetryBackoffThenDrop` (fake clock), `TestExpo400NotRetried`, `TestExpoAccessTokenHeaderNotLogged` — AC12.
- `TestPushLogsAreSecretFree` — AC13; `TestPushNeverCallsNetworkByDefault` (no sender configured → zero outbound) — AC15.
- `TestPushTestCommand` (local `/ws`, fake sender; relay-served connection gets `forbidden`) — AC14.

Mobile (`mobile/src/push/__tests__`, vitest)

- `open.vector.test.ts` (reads the Go vector) — AC16; `open.trialDecrypt.test.ts` (three keys, middle one matches); `open.fallback.test.ts` (generic notification on unknown key/corrupt blob); `open.age.test.ts` (>24 h dropped).
- `register.test.ts` (opt-in only, channel before permission, rotation re-registers, reset deletes the key and unregisters), `keys.test.ts` (per-connection key, 32 bytes, deleted with the connection) — AC17.
- `target.test.ts` (valid, missing, negative, float, string ids), `pendingTarget.test.ts` (held until gate `app`, released once, cleared) — AC19; `routeGate` cases with a held target and the password state.
- `foreground.test.ts` (open task suppressed, other task shown) — AC20.
- `noPushOnWeb.test.ts`, `noForbiddenApis.test.ts` (static grep for AC23) — AC21, AC23; the existing web smoke run unchanged — AC21.
- Task-definition test: importing the entry module registers the task name exactly once (a unit test with a mocked `expo-task-manager`) — AC18.

Manual: AC24's checklist items (a)–(j), one line each in Validation with device, OS version, build id and result.

## Decisions

- ADR-0040 (Accepted): encrypted push payload + generic fallback; daemon sends through Expo behind a `Sender` seam; registration over the E2EE channel keyed by device id; Android first.
- Human, 2026-10-22: H1–H8 accepted as recommended (H2: no Apple program, iOS blocked; H3: Human creates Expo/Firebase and uploads the FCM key, agents hold nothing).
- Coordination: PRs 5–6 wait for PR #480 (M6) to merge because both edit `mobile/package.json` and `mobile/app/_layout.tsx`; the native checklist is the mobile Lead's document, this plan only supplies its push section (AC24).
- Orchestration note: the PR table is written so each row can be handed to a smind-orchestrated agent with a short brief (files, signatures, test names are in the scenarios above).

## Progress

- 2026-10-22: research (Expo SDK 58 notifications and Push API docs, FCM/APNs limits, Paseo's push code); ADR-0040 and this plan written, docs PR opened. **Stopped for Supervisor acceptance.** No code, no live calls, no accounts.
- 2026-10-22: ADR-0040 Accepted (#481, H1–H8 as recommended). EAS link and app identifiers merged (#485, not in the original PR table: `owner`, `projectId`, `com.spacingmind.smind`, `eas.json`). #480 (M6) merged, so mobile PRs 5–6 are unblocked once PR 2 lands.
- 2026-10-22: **PR 2 opened as #487** (`internal/push`: seal/open + vector, registry, Expo sender; not wired). Orchestrated through smind (dogfood daemon, GLM peer, four steps); friction is logged in the local dogfood notes.
- Correction found while briefing PR 3: `smind relay host rotate` is relay-side and does not clear daemon registrations (AC6 and the ADR no longer claim it); revoke and the approved-at-send check are the controls.
- PR 3 is split into five Peer steps (one commit each): (a) device id on wsapi conns via an optional `DeviceServer` interface, (b) `push.register/unregister/status`, (c) notifier core (`internal/push/notifier.go`: rules, titles, dedupe, rate limit, per-device sealing, `DeviceNotRegistered` removal; pure and injectable), (d) async queue + bus tap in wsapi + `API.SetPush`, (e) `push` config block + `cmd/smind/serve.go` wiring + hub passes the device id.

## Validation

Not started. Each acceptance criterion gets its confirming test or manual check here when the work lands.
