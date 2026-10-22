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
5. `node --test 'desktop/scripts/*.test.mjs'` runs in `task test`.

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
- `create_updater_artifacts_opt_in_only` — absent (or false) in the base and
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

**Status 2026-10-22 (plans hygiene): PARTIAL.** Shipped and in use; kept in `active/` because several acceptance criteria were only partly verified and nothing in the repo shows that a human closed them.

- Merged: #274 (`feat(desktop): signed auto-update via the Tauri 2 updater`), #333 (release notes rendered as markdown), #377 (hermetic end-to-end updater test; see `completed/desktop-update-e2e.md`).
- U6 release path is now exercised for real: the v0.13.1 release run (39379008532, `publish` job success) attached `latest.json` plus `.sig` files for both macOS arches (`smind-desktop-0.13.1-macos-{arm64,x86_64}.app.tar.gz.sig`) and the Windows NSIS installer (`sMind_0.13.1_x64-setup.exe.sig`). The earlier note "Not run on GitHub" is obsolete.
- **Remaining ACs (unverified):**
  - U3: the menu item was never clicked in a real app (`update_menu_location` logic only).
  - U4: only a mocked-bridge render, not a screenshot of the real window.
  - U7 daemon interplay: no code change, never verified live (`daemon-survives-app-update`).
  - Local end-to-end in a real window (`local-update-end-to-end`, `local-menu-check`, `local-bad-signature-rejected`, `local-later-dismiss`, `local-endpoint-down-launch-silent`).
  - The Windows NSIS updater path on a real Windows machine; the first real in-app update from one published release to the next (two releases with updater artifacts now exist, but no evidence it was done).
  - Follow-up from `desktop-update-e2e`: `requireSignedVersion` is not enabled in `tauri.conf.json`.

- [x] U1 config, keys, plugin (`ecaaccf`)
- [x] U2 Rust check/install (`05bd511`, review fixes `4cf553e`)
- [x] U3 menu (`b902554`)
- [x] U4 UI (`451e7bc`, `e5fe7df`)
- [x] U5 `latest.json` script (`af4528d`, version guard in `3a47b44`)
- [x] U6 CI/release (`c261ac6`, `c28a6dc`, `dca0856`, fixes `3a47b44`, token hygiene in the final commit)
- [~] U7 daemon interplay: no code change; not verified live (see Not verified)
- [~] Local end-to-end verification: signing/verification and UI states verified; the in-window update on this Mac was blocked by a locked session (see Not verified)

Work was split across three worktrees (Rust, CI, UI) and merged here. Review
found and fixed before merge: `publish` ran `actions/checkout` after
`download-artifact` (its `git clean` would have deleted `dist/`); duplicate
`*.app.tar.gz` globs; no check that asset names match the manifest version;
the updater plugin registered unconditionally (Linux would not compile);
`semver` cfg'd away from Linux; a failed install consumed the pending
update so the UI's Retry would have said "No update is pending"; a check
could swap the pending update mid-install; a reducer path where a menu check
could replace an in-flight download.

## Validation

Environment: this Mac (Apple Silicon, macOS 27). All runtime scenarios used
an isolated `HOME`/`SMIND_HOME`, an `.e2e` identifier, daemon port 4751 and a
local HTTP server on 8765 (`/tmp/smind-e2e`); the user's real app data and
port 4648 were never touched. The machine was under heavy load from other
agents (load average ~28) for most of the session.

### Suites

- `cargo test` in `desktop/src-tauri`: 46 passed (incl. every Rust scenario
  above); `cargo clippy --all-targets -- -D warnings` clean.
- `node --test 'desktop/scripts/*.test.mjs'`: 17 tests (13 latest.json, 4
  signing-mode) pass; part of `task test`.
- `task lint` clean, `task check:versions` passes, `cd web && bunx tsc -b
  packages/ui` clean, `actionlint` clean on all four workflows.
- `task test` (web): 1503 tests; the only failures were `App.test.tsx` /
  `App.responsive.test.tsx` timeouts (5 s) under the load above, a different
  set each run (4 failed, then 3); both files pass in isolation (70/70).
  Unrelated to this change (they don't touch the updater), but a re-run on
  a quiet machine/CI is the real confirmation.

### Acceptance criteria -> evidence

U1. Config: `updater_config_pins_key_and_endpoint`,
`create_updater_artifacts_opt_in_only`. A keyless build with the opt-in config
fails exactly as designed (observed: "A public key has been found, but no
private key"), a build without it needs no key (`task desktop:mac` unchanged).
Private key: generated with `tauri signer generate` to
`~/.tauri/smind-updater.key` (outside the repo, mode 600, no password); the
build log was grepped for the key's first bytes: 0 hits; the repo contains only
the public key. README "Updating" added. **Done.**

U2. All Rust scenarios pass (`version_newer_stable_only`, `notes_are_capped`,
`check_result_serializes_to_contract`, `install_without_pending_is_error`,
`failed_install_keeps_update_pending`, `install_rejects_concurrent_call`,
`check_rejected_while_install_in_progress`, `updater_supported_by_platform`,
capability tests). Signature enforcement (independent of the app): a scratch
crate using the same `minisign-verify` crate as the plugin accepted
`smind.app.tar.gz` + `.sig` built with the local key against the public key
from `tauri.conf.json` (`signed-artifacts-local`), and rejected both a
1-byte-tampered tarball ("signature verification failed") and a signature
made with a different key ("created with a different key"). The in-app
install path was **not** run (see below). **Done for logic; install not
exercised live.**

U3. `update_menu_location` (pure). The menu is built with it; the item was
**not** clicked (see below). **Partly verified.**

U4. 12 web scenarios + reducer tests pass. Visual check: the real
`DesktopUpdateDialog` was rendered in headless Chrome against a mocked Tauri
bridge in light, dark, downloading (62%) and error states (screenshots in
`/tmp/smind-e2e/dialog-*.png`, not committed): themes correct, default
focus on **Later**, notes scrollable. Not a screenshot of the real app
window. **Done, with that caveat.**

U5. 13 script tests. A real `gen-latest-json.mjs` run over the locally built,
signed artifact (renamed per arch) produced a valid manifest: three platforms,
per-arch URLs, 424-char signatures. **Done.**

U6. Read-through against ADR-0023: secret only in the signing-decision and
signed-build steps' env, never echoed; `publish` has no secret; PR/fork/manual
path unchanged (unsigned build); real release with an empty secret fails
(`signing-mode-fails-when-required-and-absent`); `latest.json` uploaded in its
own last step; `*.sig` added to checksums/artifact/release globs.
`actionlint` clean. **Not run on GitHub** (no release/dry run was started).

### Not verified, needs a human or the next release

**Update (desktop-update-e2e, `desktop-update-e2e.md`):** the feed -> check ->
download -> signature check -> install path is now covered hermetically in
`cargo test` (macOS: a real install onto a fake `smind.app`; Windows: check +
download + verify), including a tampered artifact, a signature from another key,
an older release replayed under a newer announcement and an undecodable signature
(all refused, bundle intact, update still pending), and the dialog renders the
Rust-produced result. What stays below as manual: the real window (screenshots,
menu click, Later/Esc, launch check), `app.restart()` and the relaunched version,
the Windows installer hand-off, `daemon-survives-app-update`, the real release path.

- **The in-window end-to-end on a real app** (`local-update-end-to-end`,
  `local-menu-check`, `local-bad-signature-rejected`, `local-later-dismiss`,
  `local-endpoint-down-launch-silent`, `daemon-survives-app-update`, and the
  real light/dark window screenshots). The session was locked
  (`CGSSessionScreenIsLocked = true`) for the whole verification window, so
  the webview never ran the launch check (the local server's access log stayed
  empty) and `screencapture` returned the login screen. I did not try to get
  past the lock. The built artifacts and a ready harness are in `/tmp/smind-e2e`
  (`old/smind.app` 0.10.0 with a local endpoint, `new/smind.app.tar.gz` +
  `.sig` for 0.10.1, `serve/latest.json`, `build-old.sh`, `build-new.sh`), but
  `/tmp` is ephemeral; to redo: run the two build scripts, serve `serve/` on
  8765 (`python3 -I -m http.server 8765 --bind 127.0.0.1 --directory
  .../serve`), put `server:\n  port: 4751` in `$SMIND_HOME/config.yaml`, launch
  `old/smind.app/Contents/MacOS/smind-desktop` with `HOME`, `SMIND_HOME`,
  `SMIND_DAEMON_URL=http://127.0.0.1:4751`, and expect the prompt on launch.
- **Windows** (the NSIS updater path, the signed Windows build): not built or
  run; cfg-gated code was only read, not compiled, for Windows/Linux.
- **The real release path**: GitHub's `releases/latest/download/latest.json`
  redirect, the `publish` job end to end, and the first real update, which
  needs the repo secret set and two releases.
- After an app update the per-app macOS permission grants (notifications)
  may need re-confirming, because the ad-hoc signature changes. Not measured.
