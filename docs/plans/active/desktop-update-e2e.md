# Desktop update — hermetic end-to-end test

Follow-up to `desktop-auto-update.md` ("Not verified": the in-window update was
a manual QA item that a locked session blocked). Goal: everything between the
update feed and the installed bundle runs in `cargo test` on macOS and Windows
CI, with a throwaway signing key, so the manual list shrinks to the window itself.

## Design

- `desktop/src-tauri/src/updater.rs`: the two commands are thin wrappers over
  `check_with(builder, current_version, state)` and
  `install_pending(state, emit)`; the tests call the same cores the commands call.
- `desktop/src-tauri/tests/updater_e2e.rs` (integration test, macOS/Windows; an integration target so build.rs can give the Windows test exe the Common Controls v6 manifest dependency via `rustc-link-arg-tests`, hence `updater` is a `#[doc(hidden)] pub mod`): a mock-runtime
  Tauri app with the **real updater plugin registered the way `lib.rs` does**
  (`updater_plugin::<R>()`, same version comparator), configured with a pubkey and
  endpoint from the test (`plugins.updater` in the mock context, plus
  `dangerousInsecureTransportProtocol` — test config only, never the committed one),
  a real HTTP server on 127.0.0.1 serving `latest.json` + the artifact with an access
  log, and a minisign key pair generated per test (`minisign` crate, dev-dependency).
- macOS: the install runs for real onto a fake `smind.app` in a temp dir
  (`UpdaterBuilder::executable_path`); only `app.restart()` is not run.
  Windows: the install would launch the NSIS installer and exit, so the Windows
  run covers check + download + signature verification via `Update::download`.
- Contract tie to the UI: the `available` result JSON from the real check is
  asserted equal to `web/packages/ui/src/lib/__fixtures__/app-update-available.json`,
  and a vitest renders the real `DesktopUpdateDialog` from that same fixture.

## Acceptance criteria

1. A check against the local feed returns `available` for a newer stable version
   and nothing is downloaded (access log is only `GET /latest.json`).
2. Same/older/pre-release feed versions are `upToDate`; a dead endpoint is an error.
3. Install downloads, verifies the signature, replaces the bundle (macOS) and emits
   `downloading…` then `installing` progress.
4. A tampered artifact, a signature from a different key, an artifact signed for a
   different version (replayed older release) and an undecodable signature are all
   refused with the old bundle intact, the update still pending (retryable) and
   no partial install.
5. The release notes dialog renders the notes/version from the Rust-produced JSON.
6. Runs in `desktop-check` (macOS + Windows `cargo test`) without any secret.

## Test scenarios

Rust (`desktop/src-tauri/tests/updater_e2e.rs`, `cargo test --test updater_e2e`):
`check_reports_available_and_downloads_nothing`,
`available_result_matches_the_ui_fixture`,
`same_version_is_up_to_date`, `older_version_is_not_offered`,
`prerelease_version_is_not_offered`, `dead_endpoint_is_an_error`,
`download_verifies_a_good_signature`, `tampered_artifact_is_refused`,
`signature_from_another_key_is_refused`, `artifact_signed_for_another_version_is_refused`,
`undecodable_signature_is_refused`, macOS only: `install_replaces_the_bundle_and_reports_progress`,
`refused_install_leaves_the_bundle_and_keeps_the_update_pending`.

Web (`bunx vitest run src/components/desktop-update-dialog.test.tsx`):
`update dialog renders the Rust-produced available result`.

## Decisions

- Cores split rather than a test-only hook in the commands: no production
  backdoor, the commands stay one-line wrappers.
- Fixture shared between Rust and vitest rather than duplicating the contract.
- The tests start from the committed `plugins.updater` in `tauri.conf.json` and
  override only pubkey, endpoint and the insecure-transport flag.
- Found by the e2e/review and fixed here: the plugin announces its download
  finished (our `installing` event) *before* it verifies the signature, so a
  tampered download showed "Installing…" before the error. `install_pending`
  now downloads, then emits `installing`, then installs. Not a security issue
  (nothing was installed), a truthfulness fix, pinned by
  `refused_install_leaves_the_bundle_and_keeps_the_update_pending`.
- Follow-up, not done here (config/policy change): enable
  `requireSignedVersion` in `tauri.conf.json` (the live v0.12.0 signatures carry
  `version:` in the trusted comment) with a `gen-latest-json.mjs` check that the
  signed version equals the manifest version; today a signature without a
  version comment is accepted, so a rollback pairing of a new version number with
  an older signed artifact is only refused when the older signature carries one.

## Progress

- [x] Cores split + harness (feed, signer, mock app) — `7fb864d5`, `e187eb40`
- [x] Rust scenarios (peer-glm, reviewed; `978b06a9`..`70a034fe`)
- [x] UI fixture test (peer-glm, `f11377c3`)
- [x] Independent review (read-only): production plugin config as the test base, guard held across the relaunch, `installing` only after verification, tokio `rt-multi-thread` explicit
- [x] Docs: `desktop-auto-update.md` Not verified updated
- [x] CI evidence (macOS + Windows `cargo test`, run 39105531439 on `aabe92d5`)

## Validation

Commands: `cd desktop/src-tauri && cargo test --test updater_e2e` (13 pass on macOS locally
and in CI; 11 on Windows CI, the two install tests are macOS-only), `cargo test` (115 lib
tests + the e2e), `cd web/packages/ui && bunx vitest run src/components/desktop-update-dialog.test.tsx`
(16 pass), `tsc -b packages/ui` clean.

- AC1 `check_reports_available_and_downloads_nothing` (log == `GET /latest.json`).
- AC2 `same_version_is_up_to_date`, `older_version_is_not_offered`,
  `prerelease_version_is_not_offered` (the one that proves the production comparator is wired in),
  `dead_endpoint_is_an_error`.
- AC3 `install_replaces_the_bundle_and_reports_progress` (macOS: real extraction onto a fake
  `smind.app`, `downloading` then `installing`, log `latest.json` then the artifact),
  `download_verifies_a_good_signature` (control, also Windows).
- AC4 `tampered_artifact_is_refused`, `signature_from_another_key_is_refused`,
  `artifact_signed_for_another_version_is_refused`, `undecodable_signature_is_refused`,
  macOS `refused_install_leaves_the_bundle_and_keeps_the_update_pending` (Info.plist still `old`,
  update still pending, no `installing` event). The tampered byte is the gzip trailer so an
  unverified tarball would have extracted cleanly: the bundle assertion is real.
- AC5 `available_result_matches_the_ui_fixture` (Rust JSON == fixture) +
  `update dialog renders the Rust-produced available result` (mutation: fixture version 9.9.9
  makes the vitest fail).
- AC6 `desktop-check` on macOS + Windows, no secret; the key is generated per test.
- Independent read-only review: no blockers; changed because of it: tests start from the committed
  `plugins.updater`, the install guard is held across the relaunch, `installing` is announced only
  after verification, tokio `rt-multi-thread` explicit.
- Windows needed the e2e to be an integration test: tauri-build embeds the Common Controls v6
  manifest in the bin only, so a test exe reaching muda's `TaskDialogIndirect` import failed with
  `STATUS_ENTRYPOINT_NOT_FOUND`; `build.rs` now emits `rustc-link-arg-tests` for MSVC
  (`rustc-link-arg-tests` does not reach a lib's unit tests).

## Still manual

The webview window itself (prompt screenshots, menu click, Later/Esc in the real
window), `app.restart()` + the relaunched version, the Windows installer
hand-off, `daemon-survives-app-update`, the real GitHub `latest.json` redirect and
the `publish` job.
