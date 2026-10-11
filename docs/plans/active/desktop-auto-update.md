# Desktop auto-update (Tauri 2 updater)

Goal (user, 2026-10-11): the desktop app updates itself from GitHub
Releases, with signed updates, a prompt before anything is downloaded, and
no silent installs. Decisions and rationale: ADR-0023
(`docs/decisions/0023-desktop-auto-update.md`). Related:
`desktop-macos-app.md` (M2 bundled daemon, M4 CI, M5 update-when-idle).

No backend or daemon changes. Linux (deb only) has no updater.

## Interface contract (Rust <-> UI)

Both sides are built against this; a change needs the plan updated first.

- Command `app_update_check() -> AppUpdateCheck` (rejects with a
  human-readable string on failure). Serialized with serde
  `#[serde(tag = "kind", rename_all = "camelCase")]`:
  - `{ "kind": "unsupported" }` (Linux, or a build with no updater)
  - `{ "kind": "upToDate", "currentVersion": "0.10.0" }`
  - `{ "kind": "available", "version": "0.11.0", "currentVersion":
    "0.10.0", "notes": string | null, "date": string | null }`
  - A successful `available` check stores the plugin's `Update` in Rust
    state, replacing any earlier one. `notes` is capped at 4000 chars.
- Command `app_update_install() -> ()`: takes **no arguments**. Installs the
  update held by the last `available` check; rejects with
  `"No update is pending; check for updates again"` if none. While it runs
  it emits `app-update-progress` events: `{ "stage": "downloading",
  "downloaded": number, "total": number | null }`, then `{ "stage":
  "installing" }`. On macOS it then restarts the app (the call never
  resolves); on Windows the installer takes over. A failure rejects with a
  string and leaves the app running. A second concurrent call rejects with
  `"An update is already in progress"`.
- Event `app-update-check-requested` (no payload), emitted to the main
  window by the **Check for Updates…** menu item (which also shows the
  window first, for macOS's hidden-window state).
- Capability: `allow-app-update-check`, `allow-app-update-install` in
  `proxy.json`. No `updater:*` plugin permission is granted.

## Acceptance Criteria

### U1 — Config, keys, plugin

1. `tauri-plugin-updater` is a dependency only on macOS and Windows
   targets, registered in `lib.rs` on those targets only.
2. `tauri.conf.json` has `plugins.updater` with the real **public key**
   (from the key generated for this work) and **exactly one** endpoint,
   `https://github.com/spacingmind/smind/releases/latest/download/latest.json`.
   `dangerousInsecureTransportProtocol` is absent.
3. `bundle.createUpdaterArtifacts` is **not** in `tauri.conf.json` or the
   platform configs. It is `true` in a new
   `desktop/src-tauri/tauri.updater.conf.json`, merged only with
   `tauri build --config`. A plain `task desktop:mac` and `cargo test` need
   no private key.
4. The private key and its password never appear in the repo, logs or
   artifacts (`git grep` for the key's first bytes finds nothing).
5. The README documents: how updates work, that a first update needs the
   release to be installed by hand, that the key is a repo secret, and key
   rotation consequences.

### U2 — Rust check/install

1. `app_update_check` and `app_update_install` behave as in the contract.
2. The plugin's builder uses a version comparator: an update is offered only
   if its version is **strictly greater** than the running version **and**
   has no pre-release part. Equal, lower and pre-release versions are never
   offered.
3. The signature is verified by the plugin against the configured public
   key; there is no code path that installs without it. A bad signature
   surfaces as an error string from `app_update_install`.
4. The update is only ever installed from Rust state set by a check; the
   webview can't pass a URL or version.
5. `build.rs` lists both commands, `lib.rs` registers them, and
   `capabilities/proxy.json` grants exactly the two new `allow-*`
   permissions. `capability_allowlist_exact` is updated deliberately.
6. The launch check is started by the **UI** once (U4), not by Rust. Rust
   never downloads or installs without `app_update_install`.

### U3 — Menu

1. **Check for Updates…** is in the `smind` app menu on macOS (after
   About) and in the Help menu on Windows. It is absent on Linux.
2. Clicking it shows the main window and emits
   `app-update-check-requested`.

### U4 — UI

1. `platform.ts` gains `appUpdateCheck()`, `appUpdateInstall()`,
   `onAppUpdateProgress(cb)` and `onAppUpdateCheckRequested(cb)` on
   `DesktopApi`; the browser build's `unavailable()` rejects them like the
   other desktop methods.
2. On desktop startup the UI runs **one** background check. An error is
   swallowed (logged to the console only). `unsupported` and `upToDate` show
   nothing.
3. A menu-triggered check shows a toast on `upToDate` ("smind is up to
   date (vX)") and on error (the reason), and the prompt on `available`.
4. The prompt shows the new version, the current version and the notes
   (scrollable; "No release notes." when null), with **Update & restart**
   and **Later**. Nothing is downloaded or installed before Update is
   clicked. Later, Esc and the dialog's close button all dismiss it. A
   dismissed version isn't re-prompted by the launch check in the same
   session; a menu check always asks again.
5. After Update: the dialog shows download progress (determinate when
   `total` is known), then "Installing…". An install error returns the dialog
   to a recoverable state with the error shown inline plus a toast, and
   Update can be retried; Later still works. The dialog can't get stuck:
   dismissing during download only hides it.
6. Light and dark themes both look right (screenshots in Validation). The
   dialog reuses the app's existing dialog and toast components.
7. Renders and calls nothing in the browser build and when `unsupported`.

### U5 — `latest.json` generation

1. A script `desktop/scripts/gen-latest-json.mjs` (Node, no dependencies)
   exports a pure function `buildLatestJson({ version, tag, repo, notes,
   pubDate, files })` and has a CLI used by CI. The output matches Tauri's
   static updater format:
   `{ version, notes, pub_date, platforms: { "<target>": { signature, url } } }`.
2. Platforms: `darwin-aarch64`, `darwin-x86_64`, `windows-x86_64`. All three
   are **required**; a missing artifact or `.sig` fails with a clear error
   instead of emitting a partial manifest.
3. URLs are `https://github.com/<repo>/releases/download/<tag>/<asset>`
   with the asset name URL-encoded, and the right asset per arch (arm64
   never points at the x86_64 file).
4. `signature` is the `.sig` file's content, trimmed. `version` has no
   leading `v` and must be a stable semver (a pre-release version is
   rejected).
5. `node --test desktop/scripts/` runs in `task test`.

### U6 — CI/release

1. `desktop-macos.yml` and `desktop-windows.yml` accept an optional
   `TAURI_SIGNING_PRIVATE_KEY` secret and a `require-updater-signing`
   boolean input (default false). When the secret is non-empty they build
   with `--config src-tauri/tauri.updater.conf.json` (or the path
   equivalent) and stage the updater artifacts; when it is empty they build
   exactly as today. When `require-updater-signing` is true and the secret is
   empty, the job fails with a clear message.
2. The secret is passed only in the `env` of the `tauri build` step, and the
   "has key" decision is computed without echoing it.
3. macOS stages `smind-desktop-<version>-macos-<arch>.app.tar.gz` and its
   `.sig` (renamed per arch); Windows stages the NSIS `*-setup.exe` and
   `.exe.sig` next to the existing `.exe`/`.msi`. The flat artifact dir stays
   flat.
4. `release-please.yml` passes `require-updater-signing` = (release created)
   and `secrets:` explicitly, not `inherit`. `publish` extends its globs
   (checksums, artifact, release upload) to the new files, generates
   `latest.json` from the downloaded artifacts and uploads it to the Release
   **after** all other assets. The generated file's platform URLs are
   printed in the log; the private key is never referenced in `publish`.
5. A PR build (no secret) still passes all jobs. `actionlint` is clean.

### U7 — Daemon interplay (verification only)

1. No change to `daemon_manager.rs` or the M5 UI flow. After an app
   update the existing M5 behavior applies (verified manually).

## Test Scenarios

### Rust (`cargo test` in `desktop/src-tauri`)

- `version_newer_stable_only` — `0.10.0 -> 0.10.1`, `0.10.0 -> 0.11.0`,
  `0.10.0 -> 1.0.0` accepted; equal, lower, and `0.11.0-rc.1` / `0.11.0-dev+abc`
  with a pre-release part rejected; a non-semver remote version is rejected,
  not a panic. Pure function, no webview.
- `notes_are_capped` — notes over 4000 chars are cut (on a char boundary)
  and marked; `None` stays `None`; empty becomes `None`.
- `check_result_serializes_to_contract` — the three `AppUpdateCheck` variants
  serialize to exactly the JSON in the contract.
- `install_without_pending_is_error` — the pure take-pending helper returns
  the documented error when empty, and a second take after one succeeded is
  empty again.
- `install_rejects_concurrent_call` — the in-progress guard returns the
  documented error while held and releases on drop.
- `updater_supported_by_platform` — `true` for macOS and Windows, `false`
  for Linux (pure function of the target OS).
- `update_menu_location` — pure function: macOS -> app menu, Windows ->
  Help, Linux -> none.
- `updater_config_pins_key_and_endpoint` — reads `tauri.conf.json`: the
  pubkey is non-empty and decodes to a minisign public key (`untrusted
  comment:` header), `endpoints` is exactly the one https GitHub
  `releases/latest/download/latest.json` URL, `dangerousInsecureTransportProtocol`
  is absent.
- `createUpdaterArtifacts_opt_in_only` — absent (or false) in the base and
  both platform configs; `true` in `tauri.updater.conf.json`.
- `capability_allowlist_exact` (updated) and
  `capability_app_grants_match_build_rs_commands` (unchanged logic) include
  the two new commands, and still no `updater:*` and no wildcard.

### Script (`node --test desktop/scripts/`)

- `latest-json-all-platforms` — three platforms, correct keys, `version`,
  `notes`, `pub_date`.
- `latest-json-urls-per-arch` — arm64 -> the aarch64 asset, x86_64 -> the
  x86_64 asset; never swapped; tag and repo in the URL.
- `latest-json-signature-is-file-content-trimmed`.
- `latest-json-missing-sig-fails` / `latest-json-missing-platform-fails` —
  a clear error naming the platform/file; no output written.
- `latest-json-rejects-prerelease-version` — `0.11.0-rc.1` fails;
  `v0.11.0` normalizes to `0.11.0`.
- `latest-json-url-encodes-asset-names`.
- `latest-json-cli-writes-file` — the CLI over a temp dir of fixtures writes
  valid JSON and exits 0; exits non-zero on a missing file.

### Web (vitest: `lib/app-update.test.ts`, `components/desktop-update-dialog.test.tsx`)

- `update-launch-check-runs-once` — one `appUpdateCheck` on mount; a
  re-render and a StrictMode remount don't add calls.
- `update-launch-check-error-is-silent` — rejection: no toast, no dialog.
- `update-launch-check-up-to-date-silent`.
- `update-prompt-shows-version-and-notes` — new version, current version,
  notes text; `null` notes -> "No release notes.".
- `update-nothing-installed-before-confirm` — `appUpdateInstall` is not
  called until Update is clicked.
- `update-later-dismisses` — Later, Esc and the close button each close it
  with no install call; the launch check doesn't re-prompt that version in the
  same session.
- `update-manual-check-reprompts-after-later`.
- `update-manual-check-up-to-date-toast`.
- `update-manual-check-error-toast`.
- `update-install-shows-progress` — progress events render a percent when
  `total` is known and an indeterminate state when not; "Installing…" after.
- `update-install-error-recoverable` — a rejecting install shows the error
  inline and in a toast; Update retries and calls install a second time;
  Later still closes.
- `update-unsupported-renders-nothing` — `unsupported` and the non-desktop
  build: no dialog, no toast, no further calls.

### Build / CI checks

- `ci-pr-build-without-secret` — a PR build of both workflows (no secret)
  succeeds and uploads exactly today's artifacts.
- `ci-release-requires-secret` — with `require-updater-signing: true` and no
  secret, the build step fails with the documented message (checked by a local
  dry run of the guard script, since a real release can't be triggered).
- `ci-secret-scoped-to-build-step` — by reading the workflows: only the
  `tauri build` step's `env` references the secret; `publish` doesn't.
- `ci-latest-json-uploaded-last` — in the `publish` job, the `latest.json`
  upload is a separate command after the one that uploads the other assets.
- `actionlint` clean on all workflows.
- `signed-artifacts-local` — build with the local key and
  `--config tauri.updater.conf.json`: `smind.app.tar.gz` + `.sig` exist and
  `minisign -V`-equivalent verification (the plugin itself, below) accepts them.

### Manual on this Mac (record in Validation, with screenshots)

All with `HOME`, `SMIND_HOME` and the app-data dir isolated under `/tmp`, an
`.e2e` identifier, a non-default daemon port, and a local HTTP server
(`dangerousInsecureTransportProtocol` and the local endpoint via a build-time
`--config` override only):

- `local-update-end-to-end` — an older build (0.10.0) with the local
  endpoint, a `latest.json` serving a newer build (0.10.1): the prompt
  appears on launch (light and dark screenshots), nothing downloads until
  Update is clicked, download + signature check + restart land on 0.10.1.
- `local-menu-check` — **Check for Updates…** in the app menu triggers the
  check; up-to-date toast when the served version is the same.
- `local-bad-signature-rejected` — a tampered tarball and a signature from
  another key: both surface an error toast and the app stays on the old
  version and running.
- `local-later-dismiss` — Later / Esc, no download (server access log).
- `local-endpoint-down-launch-silent` — server not running: no UI noise at
  launch; a manual check shows the error toast.
- `daemon-survives-app-update` — a managed daemon keeps its PID across the
  app update; after the restart, an older managed daemon gets the M5
  treatment (banner or auto-update when idle).

Not testable before a second real release (stated in the PR): the genuine
GitHub `releases/latest/download/latest.json` path, and the release
workflow's `publish` job end to end.

## Decisions

- ADR-0023 records signing, endpoint, channel, UX and the daemon interplay
  (user-approved 2026-10-11).
- **Prompt in the web UI, Rust owns the plumbing** (ADR-0023.5). Two
  narrow commands instead of granting the plugin's JS API.
- **`createUpdaterArtifacts` is opt-in via a second config file**, not in
  the base config (ADR-0023.4).
- **The UI starts the launch check**, so the prompt can't be emitted before
  the UI is listening, and the browser/Linux builds do nothing.
- **Launch-check failures are silent (logged), menu-check failures toast.**
  Being offline at launch is normal and shouldn't produce a warning every
  time. Judgment call within "surface errors non-blockingly"; easy to flip.
- **Windows asset for the updater is the NSIS `.exe`**, not the `.msi`
  (the MSI is still published for manual install).
- **Test-only endpoint override is a build-time `--config`**, never a runtime
  env var, so the shipped binary has no way to be redirected to an
  unverified or http endpoint.

## Progress

- [ ] U1 config, keys, plugin
- [ ] U2 Rust check/install
- [ ] U3 menu
- [ ] U4 UI
- [ ] U5 `latest.json` script
- [ ] U6 CI/release
- [ ] U7 daemon interplay verified
- [ ] Local end-to-end verification

## Validation

(filled in as work lands)
