# 0040: Mobile push notifications — encrypted wake payloads, daemon-sent through Expo

## Status

**Proposed — 2026-10-22.** Not accepted: the Human decisions in [Open questions](#open-questions-for-the-human) (H1–H8) are open, and
H1 (privacy model, a Supervisor default the Human may veto), H2 (Apple Developer Program) and H3 (Expo/Firebase credentials) gate the
implementation PRs. Plan: [`docs/plans/active/mobile-push-notifications.md`](../plans/active/mobile-push-notifications.md).
Closes the "push notifications" gap in ROADMAP Phase 3. Builds on ADR-0031 D9 (device identity, revoke, multi-device),
ADR-0035 (relay E2EE v2), ADR-0036 (Expo Router, `smind://` scheme) and ADR-0009 (lifecycle events).
Research behind it is local-only (`docs/research/local/mobile-push-notifications.md`, not committed).

## Context

The phone is only useful as a remote if it can tell the user that something needs them while the app is backgrounded or killed.
Today the mobile app learns about `permission.pending` and `run.status` only over a live E2EE relay session
(`mobile/src/taskAttention.ts`), and the session dies when the OS suspends the app. Three kinds of event matter:

| Kind | Source on the daemon today |
|---|---|
| `permission` — a run needs approval | `permission.pending` (`internal/wsapi/server.go:377`) |
| `needs_continue` — a run stopped at a turn/token/budget cap | `run.status` with `Disposition == "needs-continue"` (`runs.DispositionFor`) |
| `done` / `error` — a run finished or failed | `run.status` `done` / `error` |

Constraints that shape the answer (all verified, see the local research notes):

1. **Privacy.** smind's relay is end-to-end encrypted (ADR-0035) so that the relay operator sees no task content. A push goes through
   Expo, then Apple or Google. A push that carries a task title or a permission summary would hand exactly that content to three
   more parties. Paseo does this (`refs/paseo/packages/server/src/server/push/push-service.ts` sends `title`/`body` in clear); smind must not.
2. **There is no hosted relay** (ADR-0031 D11/decision 12 are deferred by the Human). Users run their own relay or homelab daemon, so the
   design cannot depend on infrastructure only we operate, and a self-hoster should not need to hold a push credential for the default path.
3. **No paid Apple Developer Program for now.** Expo's own setup page: "A paid Apple Developer Account is required to generate
   credentials" (the APNs key); no workaround is documented. Android needs a Firebase project (the FCM V1 service-account key is uploaded
   to EAS), which has no per-message charge.
4. **Expo Go is out.** Remote push is not available in Expo Go from SDK 53 (v58 `expo-notifications` page and the push FAQ), and the
   redesign plan already records that Expo Go for SDK 58 is not available. Push needs a **development build** on both platforms.
5. **A woken process cannot open an authenticated relay session.** ADR-0031 P13c keeps the daemon password in JS memory only, never on
   disk, so a process started by a push has no password to present; a device session is also unique per device id (a second session for
   the same device id evicts the first); and the handshake (admission, E2EE v2, `device_hello` with a challenge) is several round trips inside
   a background budget the OS controls.
6. **Silent pushes are not a reliable wake.** iOS does not launch a force-quit app for `content-available` pushes, throttles them ("two or
   three per hour"), and never guarantees delivery. On Android, a force-stopped app receives no FCM message at all; high priority only helps with Doze.

## Decision

### 1. Privacy model: the push carries an encrypted notification, not content and not a bare wake

**Recommendation (a Supervisor default, Human may veto — H1).** The daemon seals the notification text with a key that only the daemon and
the target phone hold, and sends the ciphertext as the push `data`. Expo, Apple and Google see an opaque, padded blob, a device token and timing.
The phone decrypts it **locally** and shows a local notification with the real text. If it cannot decrypt (no key, wrong key, corrupt), it shows
a fixed generic notification ("smind needs your attention") and opens the app on tap: the fallback is content-free by construction.

- **Per-device push key.** The phone generates a random 32-byte key `K_push` per paired connection and sends it to the daemon over the
  **E2EE channel** together with its push token (§3). The relay never sees the token or the key.
- **Seal:** ChaCha20-Poly1305 (IETF, 12-byte random nonce; the same primitive as the relay channel, so `golang.org/x/crypto`, `@noble/ciphers`
  and CryptoKit's `ChaChaPoly` all interoperate), AAD = `"smind-push-v1" ‖ device_id`. Binding the device id means a ciphertext cannot be
  replayed to a different device's key; an explicit negative vector checks it. Random 96-bit nonces under a per-device key are safe at this volume (pushes are rare; the daemon
  refuses to seal past 2^32 messages per key, and a phone that re-registers with a fresh key resets the count).
- **Plaintext** (JSON, then padded to a multiple of 256 bytes after a `u16BE` length so the provider cannot read title length):
  `{"id":"<event id>","kind":"permission|needs_continue|done|error|test","taskId":12,"runId":"…","chatId":3,"title":"≤80 chars","body":"≤160 chars","at":<unix>}`.
  `id` is the dedupe key (the notification identifier on the phone), `at` lets the phone drop a push older than 24 h. The padded plaintext is ≤ 1 KiB so the
  whole payload stays far below the 4 KiB provider limit.
- **Wire `data`:** `{"v":1,"n":"<b64url nonce>","c":"<b64url ciphertext‖tag>"}`. Nothing else is in the clear: no kind, no task id, no title.
  What the providers can still learn: that this device got a push, when, and (within a 256-byte bucket) how large it was; Expo also learns the
  daemon's public IP and the app's project id. This is stated in the user docs.
- **Android:** a **data-only** message (no `title`/`body`), `priority: high`, `channelId: "attention"`, `ttl: 86400`. The app's
  background task (`Notifications.registerTaskAsync`, defined at module scope in the entry file) decrypts and calls
  `scheduleNotificationAsync(…, trigger: null)`. It always shows *something* (real text or the generic text): a high-priority data message that
  produces no visible notification risks being deprioritized by FCM.
- **iOS (deferred, needs the paid program — H2):** an alert push with a **fixed generic** `title`/`body`, `mutableContent: true` and the same
  `data`; a Notification Service Extension (native Swift, CryptoKit) decrypts and rewrites the alert before display, with the shared key in a keychain
  access group. If the extension fails or times out, iOS shows the generic alert: a guaranteed, content-free fallback and no dependence on
  background wake. No `content-available` push is used anywhere.

Why not the other shape (**wake signal, then fetch over the relay**, the Supervisor's first alternative): it fails constraint 5 outright for a
password-protected daemon and is fragile for the rest (single session per device id, background-time budget, iOS force-quit), and the
encrypted payload gives the same privacy with no network round trip on the device. The wake-and-fetch idea survives only as the *generic fallback*
above, where the fetch is simply the user opening the app.

### 2. Who sends: the daemon, through a `Sender` seam, Expo first

The daemon is the only party that knows the plaintext and the per-device key, so it seals; it also delivers:

- **v1 `Sender = expo`:** HTTPS `POST https://exp.host/--/api/v2/push/send`, batches of ≤ 100, no credential on the daemon by default
  (the API needs none; Expo notes "enhanced push security" as an opt-in that requires a bearer token, supported through the optional
  `push.expoAccessToken` / `SMIND_EXPO_ACCESS_TOKEN`, never logged). The credentials that make delivery work (FCM service account, later an APNs key) belong to
  whoever **built the app** and live in EAS, not on the daemon: a self-hoster who installs a published build, or the Human's own
  build, needs no secret to receive pushes. Errors: `DeviceNotRegistered` in a ticket removes that registration; `InvalidCredentials`/`MismatchSenderId`
  are logged as an operator problem and the registration is kept (Paseo drops the token on `InvalidCredentials`; that discards good registrations because of a build-side mistake);
  network errors, 429 and 5xx retry with exponential backoff at most 3 times within about a minute, then the push is dropped (a stale "needs approval" push is
  worse than none); 4xx payload errors are not retried. One receipt check, best effort, about 15 minutes after a send.
- **The seam:** `push.Sender` (`Send(ctx, []Message) ([]Result, error)`), selected by the registration's `provider` field. Later senders are
  additive and need no wire change: `fcm` (the daemon holds the user's Firebase service account and uses native tokens from `getDevicePushTokenAsync`: the
  fully self-sovereign path for people who build their own app), `apns`, `unifiedpush` (ntfy and friends, no Google), and `relay` (the relay
  forwards sealed blobs and holds the provider credential, which makes sense only once a hosted relay exists and ADR-0031 decision 12 is decided; the relay still
  could not read the content).
- **Why not the relay now:** the relay is a dumb pipe with no knowledge of events; making it a push gateway would give a relay operator a
  credential and a new job, would not work for a daemon without a relay, and there is no hosted relay to host it. The seam keeps the door open (H4).

### 3. Registration over the E2EE channel, keyed by device id

New wsapi methods, served **only to a relay-served approved device** (after `device_hello`; the local token `/ws` gets `forbidden`). The device id
comes from the session (`session.deviceID`, bound to the device key), never from params, so a device can only register for itself.
`wsapi` conns gain the device id (today they carry only `local bool`, `internal/wsapi/conn.go:34-43`); the hub passes it in.

| Method | Params | Result |
|---|---|---|
| `push.register` | `{"v":1,"provider":"expo","token":"ExponentPushToken[…]","platform":"android"\|"ios","key":"<b64 32 bytes>","kinds":["permission","needs_continue","done","error"]}` | `{"v":1,"registered":true,"kinds":[…]}` |
| `push.unregister` | `{}` | `{"v":1,"registered":false}` |
| `push.status` | `{}` | `{"v":1,"enabled":bool,"registered":bool,"provider"?:…,"kinds"?:[…]}` |
| `push.test` | `{"deviceId"?:"…"}` — local `/ws` only | `{"results":[{"deviceId","sent":bool,"error"?:code}]}` for `smind push test` |

Error codes: `forbidden`, `push_disabled` (daemon kill switch), `unsupported_provider`, `invalid_token`, `invalid_key`, `invalid_kinds`.
Storage: `$SMIND_HOME/push-registrations.json`, mode 0600, atomic writes, same posture as `relay-devices.json` (a corrupt or unknown-version
file is an error, never a silent reset); it holds the token and `K_push`, so it is as sensitive as the daemon's static key file. Registration is
**opt-in on the phone** (a button in settings, in the context of the OS permission prompt), never automatic at launch.

Lifecycle:

- **Revoke.** The registration is deleted when the device is revoked, and, as defence in depth (revocation by editing the registry file with no daemon
  running), the sender checks that the device is still `approved` in the registry at send time. A revoked phone keeps `K_push` but the daemon sends nothing
  to it. `smind relay host rotate` (the hard reset) clears all registrations.
- **Re-pair.** A re-paired phone has a new device key, hence a new device id, hence no registration until it registers again; the app deletes its push key
  with the connection (`resetIdentity`). A token belongs to at most one device id per daemon: registering a token under a new id removes it from any other id, so an
  old, revoked or abandoned id can never share a live phone's token.
- **Multi-device (D9).** One registration per device id, at most `relay.maxDevices`. Each event is sealed once **per device** with that device's key and sent
  in one batch. Pending devices cannot register (they are not served). A phone paired to two daemons holds two keys and two registrations (the token is per
  install, the key per connection); an arriving push is opened by trying the few stored keys (the AEAD tag decides), so no key id appears in clear.
- **Token rotation.** `addPushTokenListener` re-registers the new token (same key). A registration is not leased: it lasts until unregister, revoke, `DeviceNotRegistered` or rotate.

### 4. Which events notify, and how often

The daemon subscribes to its own event bus in-process (no new topic, no wire change to existing clients), after the same system-task / `Purpose` filtering
the bus notifier already applies:

| Event | Push `kind` | Notes |
|---|---|---|
| `permission.pending` | `permission` | any task; one push per `requestId` per device; body is the truncated summary |
| `run.status` done with `Disposition == "needs-continue"` | `needs_continue` | "Reached the turn cap — tap to Continue"; replaces `done` for that run |
| `run.status` done | `done` | **root tasks only** (no parent); child-task completions are not pushed |
| `run.status` error | `error` | root tasks only |
| `run.status` stopped | — | the user stopped it |

(Whether child-task `done`/`error` should also push is H6.) Per-device filters come from the registration's `kinds`. Rate limit: `permission` and
`needs_continue` are never dropped; `done`/`error` are limited to 6 a minute per device, extras are dropped (they stay visible in the task list). A
resolved permission does not retract the push in v1 (tapping a stale one opens the task, which shows the true state); an Android-only "clear" message is a follow-up.

### 5. Tap opens the right task

The decrypted plaintext puts `taskId` (and `chatId`) into the **local** notification's `data`; the tap route is `/task/<id>` (ADR-0036's route, `title` optional).
A pure `resolveNotificationTarget(data)` validates it; the target is held in a small store until `routeGate` reports the `app` group (a cold start tap arrives
before the session exists, possibly before the password prompt is answered), then navigated once and `clearLastNotificationResponseAsync` is called. A tap whose
key belonged to a connection that is not the active one opens the app normally in v1. A foreground notification for the task screen that is already open is suppressed.

### 6. What works for free, and what does not (explicit v1 scope)

| | Free (no Apple program) | Needs paid |
|---|---|---|
| Android dev build or APK, Expo-brokered push, encrypted payload | **Yes.** Firebase project (no per-message charge), an Expo account for the EAS credential upload, `google-services.json` | — |
| iOS remote push (Expo, or APNs directly) | No: needs an APNs key | **Apple Developer Program** (H2) |
| iOS Notification Service Extension | Not testable without push; also needs a keychain access group (whether a personal team can enable it is unverified) | with the program |
| iOS dev build for everything *except* push | Yes (7-day profiles, 3 devices) | — |

**v1 = Android first, daemon side platform-agnostic, iOS later.** The daemon, the wire, the registration and the mobile TypeScript (decryption, routing) are
platform-neutral and land now; only the iOS extension and the APNs credential wait for H2. On iOS without the program, the app still works and shows live attention
in the foreground; it just gets no remote notifications. This is stated in the mobile docs.

### 7. Development build, not Expo Go

`expo-notifications` and `expo-task-manager` (plus `expo-device` for the physical-device check) are native modules in a **development build** (`expo run:android`
or EAS Build); Expo Go cannot receive remote push. The Android build needs `android.package`, `extra.eas.projectId` and `android.googleServicesFile`
(from an environment value so a Firebase project is not baked into the open-source repo; H3, H5). The background task must be registered where the headless
JS context loads it, so the entry becomes a one-line custom file that imports the task module and then `expo-router/entry` (a small amendment to ADR-0036's
`main: "expo-router/entry"`). The mobile native-verification checklist (owned by the mobile Lead) gets a push section; the plan lists the items (AC24).

### 8. Operator surface and failure handling

Config: `push.enabled` (default true: registration is already an explicit opt-in on the phone), `push.expoAccessToken` (optional). `smind push test [device]` sends a
`kind: "test"` push with no LLM involved, which is also how native verification is done without spending agent budget. Logs never contain tokens, keys, titles or
bodies (only counts, kinds, device-id prefixes, HTTP status). No call to Expo happens in tests: the sender takes an injectable base URL and the unit tests use `httptest`.
The user docs (`docs/homelab.md`) state what leaves the daemon: the daemon's IP and an opaque blob to `exp.host`, nothing else.

## Alternatives considered

- **Plaintext push (what Paseo does).** Simplest and what users expect (the notification text works even when decryption would fail), but it gives Expo, Apple and Google
  every task title and permission summary and defeats the point of an E2EE relay. Rejected (H1 is the veto point).
- **Content-free wake, then fetch over the E2EE relay.** Maximal privacy and simple on the wire, but constraint 5 kills it: no password, evicts the foreground session,
  needs a full handshake in a background budget, and iOS silent push is throttled and absent after a force quit. Kept only as the generic fallback.
- **Always-generic visible push ("smind needs you"), no decryption.** Perfectly reliable and private, and it is the fallback of the chosen design; as the *only* mode it
  makes every notification say nothing useful ("which task? approve what?"). Rejected as the only mode.
- **iOS silent push (`content-available`) to drive a local notification.** Not delivered after a force quit, throttled, not guaranteed. Rejected; the iOS design uses an alert push + extension.
- **Polling: iOS `BGAppRefreshTask` / Android WorkManager.** System-scheduled (no tighter than ~15 min on Android, discretionary on iOS), unrelated to the event. Rejected. An Android
  foreground service holding the relay session open would need a persistent notification and the password problem again.
- **Daemon sends straight to FCM/APNs.** No third party except Google/Apple, but only possible for people who build their own app with their own Firebase project, and the daemon then
  holds a service-account secret. Kept as a later `Sender` (`fcm`, `apns`), not v1.
- **The relay sends.** See §2; deferred with ADR-0031 decision 12.
- **A Noise/libsignal-style ratcheting push channel.** Overkill: pushes are one-way, rare, and the key is exchanged over an already E2EE channel; random nonces and a per-device key suffice.

## Open questions for the Human

Each has a recommendation; none blocks writing the plan. H1–H3 gate the implementation PRs.

- **H1. Privacy model.** *Recommended: encrypted payload with a generic fallback (§1).* Alternatives: always-generic (more private, less useful), plaintext (Paseo-style, rejected above).
- **H2. Apple Developer Program (USD 99 a year, an external cost).** *Recommended: not now.* Ship Android first; iOS push (APNs key, Notification Service Extension) waits. Without it nothing about iOS push can be tested or shipped.
- **H3. Expo account, Firebase project, EAS credentials (external accounts and secrets).** *Recommended:* the Human creates one Expo organization (for example `spacingmind`) and one Firebase project,
  uploads the FCM V1 service-account key to EAS themselves (never in the repo, never given to an agent), and gives the Lead the `projectId` and `google-services.json` as environment values. Agents never create accounts or hold the key.
- **H4. Sender.** *Recommended: daemon → Expo Push API (§2), accepting that Expo sees the daemon's IP, the token and an opaque blob.* The relay-as-gateway and direct-FCM senders are later additions behind the same seam.
- **H5. Application identifiers.** *Recommended:* `com.spacingmind.smind` for `android.package` and (later) `ios.bundleIdentifier`. They are effectively permanent once a store listing or an FCM registration exists.
- **H6. Which events notify by default.** *Recommended:* permission, needs-continue, and done/error for root tasks only (children are noisy in orchestrated trees). Alternative: push every task's done/error.
- **H7. Daemon default.** *Recommended:* `push.enabled: true` (registration is an explicit phone-side opt-in; document the outbound host). Alternative: default false and require `smind config set push.enabled true`.
- **H8. Hosted relay as a push gateway.** *Recommended: not until ADR-0031 decision 12 is decided;* revisit then (the `Sender` seam already allows it).

## Consequences

- A user who enables notifications gets a real title and body on the lock screen on Android while no third party can read them; the cost is one new wire pair, a registry file, an event
  consumer on the daemon and native modules in the app.
- Delivery stays best effort: a force-stopped Android app, an OEM battery killer, a user without Play services, or Doze under standby buckets can delay or lose a push. The app always
  reconciles when opened (the task list is the truth); the docs say so.
- iOS remote push is unavailable until H2 is decided; the design for it is fixed so the daemon needs no change.
- New secrets on disk: tokens and `K_push` in `push-registrations.json` (0600). Stealing it lets an attacker forge pushes to that phone (not read the relay) and read pushes captured earlier by a
  provider; revoke and re-register rotate it.
- ADR-0036's entry point changes by one file (see §7). `wsapi` conns learn the device id. Nothing in the relay, proto, or E2EE core changes.
- Verification needs a physical device or an emulator with Play services, a dev build, and the Human's Firebase/Expo setup: it cannot be CI-verified end to end.
- **Unproven until a device run (checklist AC24 of the plan):** that Expo's push service delivers a message with no `title`/`body` to Android as a data message that wakes the
  `registerTaskAsync` task in a killed app, and that `expo-secure-store` can be read inside that headless JS context. If either fails, the fallback is an Android notification message with
  the generic text (still content-free); the encryption design and the wire do not change.
