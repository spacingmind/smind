# Desktop updater: `requireSignedVersion` (DT1 item 4)

Status: active (2026-11-15). Size: M (6): scope 2 (updater config + release scripts), PRs 1, risk 3 (changes what the updater trusts, and a mistake in the release flow can make every installed app refuse the next update), decisions 0, verification 2. **HIGH-RISK** (updater trust, release path): two fresh-context reviews and a Supervisor second read.

## What it is, and what happens today

The in-app updater (Tauri updater plugin 2.12, ADR-0023) asks `latest.json` on the release for `version`, a download `url` and a minisign `signature` per platform. **`latest.json` itself is not signed**; the signature covers the downloaded artifact and its *trusted comment* (`timestamp:…\tfile:…\tversion:X.Y.Z`, written by the Tauri CLI when the release is signed; covered by minisign's global signature).

The plugin has one check that closes the downgrade hole: after verifying the artifact signature it compares the version in the trusted comment with the `version` that `latest.json` announced and refuses a mismatch ("The update was signed for version A but the update endpoint announced version B"). Without it, anyone able to serve a crafted `latest.json` could pair an inflated `version` with the `url` and `signature` of an older, genuinely signed release: the app would install a real but outdated build (rollback to a version with known bugs).

That check only runs **when the signature carries a `version:` field**. With `plugins.updater.requireSignedVersion` off (today), a signature with no version comment skips the check entirely: an old signature made before the CLI recorded versions, or a re-signed one without it, is accepted whatever `latest.json` announced. `requireSignedVersion: true` makes a signature without a signed version an error (`MissingSignedVersion`), so the equality check can no longer be skipped.

Checked 2026-11-15 on `wave/desktop`:

- `desktop/src-tauri/tauri.conf.json` `plugins.updater` has `pubkey` and `endpoints`, no `requireSignedVersion` (the follow-up recorded in `completed/desktop-update-e2e.md` and `desktop-auto-update.md`).
- Every published release carries the signed version: the macOS and Windows updater signatures of all twelve releases v0.11.0 to v0.20.0 (36 files) have `version:<release version>` in their trusted comment (read from the public release assets).
- The existing hermetic test `artifact_signed_for_another_version_is_refused` covers a *wrong* signed version; nothing covers a *missing* one, nothing pins the flag, and `gen-latest-json.mjs` / the release verifier (DT1 item 1) never read the signed version.
- The flag is read by the *installed* app. Turning it on affects apps built from this change on, when they check for the update after theirs. Already-installed older apps keep their current behaviour.

## What changes in trust (and why it is only stricter)

The updater refuses more, never less: a signature without a signed version, which was accepted, is now refused. It cannot make the updater accept anything new. The cost is availability: if a future release were signed without the `version:` comment (a Tauri CLI change, a hand re-sign), apps built with the flag could not update to it at all (they fail closed with the plugin's clear error). The mitigation is in the release flow, before anything is published:

1. `desktop/scripts/gen-latest-json.mjs` reads each artifact signature, decodes it, and refuses to write `latest.json` unless its trusted comment carries a version equal to the manifest version.
2. `scripts/verify-release-artifacts.mjs` (the DT1 item 1 gate, run before and after `latest.json` is generated) gets a check `updater-signatures`: the three updater signatures each carry `version:<desktop version>`; the `latest-json` check stays as is.
3. Neither check verifies the signature cryptographically (the plugin does that at install; `sign-checksums.sh` does it for `checksums.txt`): they read the signed comment so a release that would be unusable is stopped.

## Decisions

- Enable `requireSignedVersion: true` in the committed `tauri.conf.json`. The hermetic updater tests start from that committed `plugins.updater` and override only key, endpoint and the insecure-transport flag, so they run with the flag on (as the real app does).
- No `allowDowngrades` (default false stays; pinned by a test): the plugin's "must be newer" rule and `is_offerable` (stable versions only) are unchanged.
- No re-signing of old releases: not needed (all carry the version) and the flag does not affect older installed apps.
- Both scripts read a signature exactly as the plugin does (found in review): the third line only, starting `trusted comment: ` with the space, split on tabs, fields not trimmed, the field starting `version:`; the verifier also strips a leading `v` before comparing, as the plugin's semver comparison does. Anything the plugin would not read a version from is refused here too. Parsing the signature in two scripts is deliberately small and duplicated, with a keep-in-sync comment in each (`desktop/scripts` and `scripts` are separate trees): base64-decode, take the line starting `trusted comment:`, split on tab, take the `version:` field.

## Acceptance criteria

- AC1: `tauri.conf.json` has `plugins.updater.requireSignedVersion: true`, `allowDowngrades` absent or false, and the key/endpoint pins unchanged.
- AC2: with the committed config, an update whose signature has **no** signed version is refused at download with the plugin's missing-version error, and nothing is installed.
- AC3: an update announced as version A whose signature is for version B is refused (existing test kept); an update with a matching signed version installs (existing tests kept).
- AC4: with the flag off, a versionless signature would be accepted (a test pins why the flag matters; it overrides the committed flag to false for that one test).
- AC5: `gen-latest-json.mjs` refuses to write `latest.json` when a platform signature has no signed version, a signed version different from the manifest version (also `v`-prefix tolerant), is not decodable, or has no trusted comment; every other behaviour is unchanged.
- AC6: `verify-release-artifacts.mjs` reports `updater-signatures` for a signature without the signed version, with a different one, or undecodable; the real v0.20.0 asset set passes.
- AC7: docs: the ADR-0023 amendment, the `desktop-auto-update.md` follow-up marked done (pointing here) and `docs/release-verification.md` (the new check, the fail-closed behaviour, what to do if a release is refused as unusable).

### Named tests

`desktop/src-tauri/src/updater.rs`: `updater_config_requires_signed_version` (AC1: the flag true, `allowDowngrades` not true, pins unchanged).
`desktop/src-tauri/tests/updater_e2e.rs`: `signature_without_a_signed_version_is_refused` (AC2), `artifact_signed_for_another_version_is_refused` (kept, AC3), `download_verifies_a_good_signature` (existing, kept: a matching signed version downloads), `without_the_flag_a_versionless_signature_is_accepted` (AC4).
`desktop/scripts/gen-latest-json.test.mjs`: `latest-json-refuses-a-signature-without-a-signed-version`, `latest-json-refuses-a-signed-version-that-differs`, `latest-json-accepts-v-prefixed-signed-version`, `latest-json-refuses-an-undecodable-signature`, `latest-json-refuses-a-signature-without-a-trusted-comment`, `latest-json-refuses-a-version-field-the-plugin-would-not-read`, `latest-json-reads-only-the-third-line`, `latest-json-cli-refuses-and-writes-nothing-for-a-versionless-signature` (plus the existing tests moved to real-shaped signatures).
`scripts/verify-release-artifacts.test.mjs`: `verify-updater-signature-without-version`, `verify-updater-signature-wrong-version`, `verify-updater-signature-undecodable`, `verify-updater-signature-version-field-the-plugin-would-not-read`, `verify-updater-signature-reads-only-the-third-line`, `verify-updater-signature-v-prefix-accepted` (plus the good set built with real-shaped signatures).

### Mutation list (each must fail the named test; results in Validation and the PR body)

flag removed from `tauri.conf.json` -> `updater_config_requires_signed_version` and `signature_without_a_signed_version_is_refused`; `allowDowngrades: true` added -> `updater_config_requires_signed_version`; the `gen-latest-json` signed-version comparison dropped -> `…differs`; the missing-version rejection dropped -> `…without-a-signed-version`; the verifier's `updater-signatures` version comparison dropped -> `verify-updater-signature-wrong-version`; the verifier's missing-version case dropped -> `verify-updater-signature-without-version`.

## Progress

- [x] spec
- [x] config flag + Rust tests
- [x] `gen-latest-json.mjs` check (logic: lead; fixtures and tests: peer-glm 641)
- [x] verifier check (logic: lead; fixtures and tests: peer-glm 641)
- [x] docs (ADR-0023 amendment, desktop-auto-update, release-verification)

## Validation

- Hermetic updater tests (`cargo test --offline`, macOS): `updater_config_requires_signed_version` and the 15 `updater_e2e` tests pass, including `signature_without_a_signed_version_is_refused` (refused with the plugin's "does not specify the version" error, nothing installed) and `without_the_flag_a_versionless_signature_is_accepted` (the same signature is accepted with the flag overridden off).
- `node --test scripts/*.test.mjs desktop/scripts/*.test.mjs`: 97 pass.
- Real data: all 36 updater signatures of v0.11.0 to v0.20.0 carry `version:<release>`; the v0.20.0 release asset set passes `verify-release-artifacts.mjs` including the new `updater-signatures` check (all 9 checks ok).
- Mutations (each on a committed file, the named test failed, restored): flag removed from `tauri.conf.json` (`updater_config_requires_signed_version`, `signature_without_a_signed_version_is_refused`); `allowDowngrades: true` added (`updater_config_requires_signed_version`); gen-latest-json signed-version comparison dropped (`latest-json-refuses-a-signed-version-that-differs`); its missing-version rejection dropped (`latest-json-refuses-a-signature-without-a-signed-version` and the CLI test); the verifier's version comparison dropped (`verify-updater-signature-wrong-version`); the verifier accepting a versionless signature (`verify-updater-signature-without-version`). A first attempt at the last mutation survived because the mutation itself returned a wrong version; redone as "accept the versionless case".
- Not verified: a real in-app update from a published release to the next with the flag on (needs the next release; two flag-off releases exist); the Windows updater path (the hermetic e2e is macOS and Windows in CI, run locally on macOS only); `cargo fmt --check` of the whole shell fails on `notify.rs`, pre-existing and not touched here (only the files this PR changed were formatted).
- Review (two fresh sub-agents): no blocker; fixed the three parsing differences from the plugin (trim, line choice, `v` prefix in the verifier), a wrong claim in an error message (the plugin refuses a mismatched signed version with or without the flag; the flag only governs a missing one), the spec's test name and AC7 wording; all 36 real signatures still parse with the stricter parser; 5 more tests.

### Second read of the PR

- Both scripts now split the signature on `\r?\n` (Rust `str::lines()` strips `\r`: a CRLF `.sig` was falsely refused) and strip every leading `v` (the plugin's `trim_start_matches('v')`); tests for CRLF and `vv1.2.3`, in both scripts.
- The verifier's `updater-signatures` check is now cryptographic: it verifies each `.sig` with the updater public key committed in `tauri.conf.json` over the artifact next to it (minisign prehashed `ED`: Ed25519 over BLAKE2b-512, plus the global signature over the signature and the trusted comment), because the file field cannot pin the architecture: on the real v0.20.0 signatures both macOS tarballs say `file:sMind.app.tar.gz` (the installer says its own name). Checked on the real v0.20.0 release: `checksums.txt` and the three real artifacts verify, and the x86_64 signature over the arm64 tarball is refused ("the signature does not match the artifact"). Tests: another artifact's `.sig`, a different key, an altered trusted comment, a missing public key; the real `checksums.txt` and its real signature verify, tampering does not.
- Real v0.20.0 fixtures are committed (`scripts/testdata/real-v0.20.0/`: the three updater `.sig`, `checksums.txt`, its signature, the public key) and read by both test files, so "all releases carry their version" is reproducible; `docs/release-verification.md` has the one-line re-check.
- Release workflow wiring is pinned (`scripts/release-verify-wiring.test.mjs`): the pre-upload verification exists, comes before the first upload, has no `if:` (so it also runs on dry runs), passes no flag that turns a check off other than the manifest one, and `updater-signatures` is among the checks; the second run, on `latest.json`, is before that upload and does not skip the manifest.
- ADR-0023 amendment states the inherent limit: a genuine older signed release that is still newer than the installed version can be re-served by a feed attacker (needs a signed manifest or a version floor); what we do instead is written there.
