# macOS signing and notarization: prepared, gated on the Human's Apple account (DT1 item 5)

Status: active (2026-11-15). Size: M (7): scope 3 (scripts, workflow skeleton, ADR draft), PRs 1, risk 3 (handles signing credentials; a CI workflow that can see secrets), decisions 8 (all in the ADR draft, each with a recommendation), verification 3 (nothing can run against Apple). **HIGH-RISK** (secret handling, release path): two fresh-context reviews and a Supervisor second read.

## What this is, and what it is not

Everything that can be prepared without the Apple Developer account, and nothing that needs it. The account (USD 99 a year, a legal entity, the certificate and an API key) is the Human's money and identity call; **nothing in this PR has been run against Apple and no script will run without the Human's secrets**. It supersedes ADR-0023's "Apple notarization stays out of scope" with a Proposed ADR (0060), prepares the scripts, and adds a CI workflow that is disabled until the Human turns it on.

## Checked against `wave/desktop` (2026-11-15): what exists, what is dropped

- macOS builds are ad-hoc signed (`bundle.macOS.signingIdentity: "-"` in `tauri.macos.conf.json`); no entitlements, no hardened-runtime setting, no notarization, no Apple secret anywhere (ADR-0023 decision 2, "Apple notarization ... stay out of scope"). The Tauri updater signature (minisign, `TAURI_SIGNING_PRIVATE_KEY`) is a separate, existing mechanism and is unchanged.
- The app bundles the Go daemon as a Tauri `externalBin` sidecar at `Contents/MacOS/smind`: it is nested code that must be signed and notarized with the app.
- The release builds run in `desktop-macos.yml` (called by `release-please.yml`); the updater tarball (`*.app.tar.gz`) and its `.sig` are produced by the same `tauri build`, so a signature change after the build means regenerating and re-signing the tarball with the updater key.
- Nothing to drop: no signing script, ADR, entitlements file or signed workflow exists.

## Decisions (all in ADR-0060, Proposed: the Human decides each; recommendations there)

How the build signs: (A) Tauri-native (the bundler signs, notarizes and staples from `APPLE_*` environment variables during `tauri build`, one pass, the dmg layout and the updater tarball come out consistent) is recommended; (B) our own scripts sign the already-built app (full control, testable here, but the dmg layout and the updater tarball must be rebuilt and the tarball re-signed). The scripts below serve both: a **gate** (`verify.sh`, run after either path), a **credential check** (`sign-notarize.sh --check`) and the **fallback** (`sign-notarize.sh`). Other decisions in the ADR: enrol and which entity; notarization auth (App Store Connect API key, recommended, vs Apple ID with an app-specific password); certificate type (Developer ID Application only); entitlements start empty and grow only by a named failure; a protected GitHub Environment with a required reviewer, tags/branches limited; PR builds stay ad hoc; Windows Authenticode stays out of scope (own ADR); what existing ad-hoc installs lose on the first signed update (per-app OS grants such as notifications may be re-asked; not measured).

## Acceptance criteria

- AC1 (gate): every script refuses to run, with exit 3 naming only the missing variable(s), unless all six secrets are present with the right shape; it refuses before any signing tool is invoked and prints no value. `verify.sh` needs no secrets and changes nothing.
- AC2 (no leakage): no script uses `set -x`, echoes a secret, or writes one outside a 0600 file in a private 0700 temp dir that is removed on exit (also on failure and on a signal); the signing keychain is a temporary one and is deleted on exit; the notarization key goes to `notarytool` by file path, never by value.
- AC3 (signing order and flags): nested executables (the sidecar, frameworks) are signed before the app bundle; every `codesign` has `--options runtime` and `--timestamp`; never `--deep`; the committed entitlements files are passed (sidecar file for nested code, app file for the bundle); the app is verified with `--strict` after signing.
- AC4 (notarization): the app is zipped with `ditto`, submitted with `--wait`; anything but `Accepted` fails the script, fetches the log into the output dir, and nothing is stapled; stapling and `stapler validate` happen only after `Accepted`; with `--dmg` the dmg is created from the stapled app, signed, notarized and stapled; with `--updater-tarball` the stapled app is packed and the script says the tarball must be re-signed with the updater key.
- AC5 (verify gate): `verify.sh` fails (non-zero, one `FAIL` line per problem) for: an ad-hoc signature, a missing Developer ID authority, a missing hardened-runtime flag on the app or any nested executable, a Team ID that differs between the app and a nested executable, entitlements that differ from the committed files, a Gatekeeper rejection, a missing stapled ticket, and the same for a dmg.
- AC6 (workflow skeleton): `.github/workflows/desktop-macos-signed.yml` is `workflow_dispatch` only (never `pull_request`, so no secret reaches a fork), refuses to run unless the repository variable `MACOS_SIGNING_ENABLED` is `true` AND the six secrets exist (checked without echoing them), uses a protected Environment, has `contents: read` permissions, exposes each secret only to the one step that needs it and never interpolates a secret into a `run:` script, pins every action by SHA, and publishes nothing: it uploads the verified artifacts as workflow artifacts only. Release wiring (making `release-please.yml` call it) is NOT done here.
- AC7 (docs): `docs/desktop-macos-signing.md` is the Human's runbook: what to buy and create, the exact secret names and how to produce each value, how to turn the workflow on, what to run locally, how to roll back, and what to do when notarization rejects.
- AC8 (ADR): ADR-0060 (Proposed) lists context, the options, the eight decisions with recommendations, consequences, and the limits (nothing verified against Apple).

### Named tests (`scripts/macos-sign.test.mjs`, fake `security`/`codesign`/`xcrun`/`ditto`/`plutil`/`hdiutil`/`spctl`/`uname`/`tar` on `PATH`, `node --test`)

`sign-refuses-without-each-secret` (table over all six), `sign-refuses-a-bad-identity-shape`, `sign-refuses-bad-key-id-and-issuer`, `sign-refusal-names-variables-never-values`, `sign-refuses-before-any-tool-runs`, `sign-refuses-off-macos`, `sign-check-mode-signs-nothing`, `sign-no-secret-in-output`, `sign-cleans-up-keychain-and-temp-dir-on-success`, `sign-cleans-up-on-notarization-failure`, `sign-nested-code-before-app`, `sign-every-codesign-has-runtime-and-timestamp`, `sign-never-deep`, `sign-passes-the-entitlements`, `sign-notarization-not-accepted-fails-and-fetches-log`, `sign-staples-only-after-accepted`, `sign-key-by-file-path`, `sign-dmg-is-signed-notarized-stapled`, `sign-updater-tarball-warns-to-resign`, `sign-rejects-app-that-is-a-link`, `verify-passes-a-good-app`, `verify-fails-adhoc`, `verify-fails-without-developer-id`, `verify-fails-without-runtime`, `verify-fails-nested-without-runtime`, `verify-fails-team-mismatch`, `verify-fails-entitlements-mismatch`, `verify-fails-gatekeeper-rejection`, `verify-fails-without-staple`, `verify-dmg-checks`, `scripts-never-enable-xtrace` (static), `entitlements-files-are-valid-plists` (static, with `plutil` when present), `workflow-is-dispatch-only-and-gated` (static), `workflow-secrets-only-in-env-never-in-run` (static), `workflow-actions-are-pinned` (static).

### Mutation list (each must fail the named test)

secret gate removed -> `sign-refuses-without-each-secret`; gate moved after the keychain step -> `sign-refuses-before-any-tool-runs`; a value echoed in the refusal -> `sign-refusal-names-variables-never-values`; `--deep` added -> `sign-never-deep`; `--options runtime` dropped from one `codesign` -> `sign-every-codesign-has-runtime-and-timestamp`; sidecar signed after the app -> `sign-nested-code-before-app`; `--wait` dropped / status check removed -> `sign-notarization-not-accepted-fails-and-fetches-log`; staple before notarization -> `sign-staples-only-after-accepted`; keychain deletion removed -> `sign-cleans-up-keychain-and-temp-dir-on-success`; `set -x` added -> `scripts-never-enable-xtrace`; an adhoc check removed from `verify.sh` -> `verify-fails-adhoc`; the team comparison removed -> `verify-fails-team-mismatch`; the workflow gets a `pull_request` trigger -> `workflow-is-dispatch-only-and-gated`; a secret interpolated into a `run:` -> `workflow-secrets-only-in-env-never-in-run`; an unpinned action -> `workflow-actions-are-pinned`.

## Progress

- [x] spec
- [ ] scripts, entitlements
- [ ] tests
- [ ] workflow skeleton
- [ ] ADR-0060 draft, runbook

## Validation

(filled as the PR lands)
