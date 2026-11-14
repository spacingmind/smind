# Desktop release hygiene without money (DT1)

Status: active (2026-11-15). Size: L (5 items, one PR each, no new paid service). Integration branch: `wave/desktop`.

## Why

A release is one tag that fans out into many artifacts (daemon archives for six targets, two macOS dmgs and updater tarballs, a Windows installer and msi, `checksums.txt` + signature, `latest.json`, a container image). Today nothing checks, as one set, that they all describe the same version, and `publish` does not look at whether CI passed on the commit being released. The crash story is "nothing is recorded". This plan closes those without paying for anything.

## Checked against `wave/desktop` (2026-11-15): what already exists, what is dropped

| Item | Exists today | Decision |
|---|---|---|
| Version sync | `scripts/check-version-sync.sh` (4 desktop fields vs the manifest), run by CI | kept as is; the verifier re-checks the same fields against the *tag* and adds everything below |
| Manifest sanity | `desktop/scripts/gen-latest-json.mjs` (3 platforms, `.sig` present, version appears in asset names) | kept; the verifier re-reads the generated file independently |
| Checksums signature | `scripts/sign-checksums.sh` verifies the signature with `minisign` | kept; the verifier only checks the file's shape and signed version comment |
| Bundle check (macOS) | `desktop-macos.yml` "Verify bundle" (Info.plist version, native daemon `--version`) | kept; the verifier repeats it on the *published* tarball, cross-platform |
| CI-green gate | none: `publish` needs only the build jobs | item 2 |
| Crash capture | none (no panic hook, no `recover()` at handler/run boundaries, no crash dir) | item 3 |
| `requireSignedVersion` | not enabled; recorded as a follow-up in `completed/desktop-update-e2e.md` | item 4 |
| Notarization | ADR-0023 says out of scope; nothing prepared | item 5 (scripts + ADR draft, nothing run) |
| Mobile version | `mobile/package.json` is `1.0.0`, versioned independently for the app stores, not by release-please | dropped from the verifier: it is not part of the release set (stated in the verifier's help and the docs) |

## Items (each its own PR; the spec for items 2-5 is refined in that item's PR)

1. Release-artifact consistency verifier (`scripts/verify-release-artifacts.mjs`, CI step in `release-please.yml` `publish`).
2. "CI green on the release tree" gate.
3. Crash capture (desktop panic hook, daemon recover, `smind doctor`).
4. `requireSignedVersion` downgrade protection.
5. Notarization scripts, gated on secrets, plus an ADR draft.

## Item 1: acceptance criteria

The verifier takes the assembled `dist/` directory of a release and fails, listing every problem (not only the first), when any artifact set disagrees. `node scripts/verify-release-artifacts.mjs --dir dist --version X.Y.Z --tag vX.Y.Z --repo owner/name [--desktop-version X.Y.Z] [--source-root .] [--skip-latest]`. Exit 0 and one `ok` line per check when consistent; exit 1 with `check-id: message (expected …, found …)` lines otherwise; exit 2 for a usage error. Under `GITHUB_ACTIONS` each failure is also an `::error::` annotation.

- AC1 (`source-versions`): `.release-please-manifest.json`, `desktop/package.json`, `desktop/src-tauri/tauri.conf.json`, and both `Cargo.toml` files all equal the desktop version (`--desktop-version`, default `--version`). `--tag` must equal `v<version>` (`tag`).
- AC2 (`daemon-archives`): exactly the six daemon archives `smind_<version>_{linux,darwin}_{amd64,arm64}.tar.gz` and `smind_<version>_windows_{amd64,arm64}.zip` exist; no other `smind_*` archive of a different version is present.
- AC3 (`desktop-assets`): `smind-desktop-<dv>-macos-{arm64,x86_64}.dmg`, `…-macos-{arm64,x86_64}.app.tar.gz` and its `.sig`, `sMind_<dv>_x64-setup.exe` and its `.sig`, `sMind_<dv>_x64_en-US.msi` exist; no `smind-desktop-*` / `sMind_*` file carries another version.
- AC4 (`checksums`): `checksums.txt` lists every file matching the publish glob (`*.tar.gz *.zip *.exe *.msi *.dmg *.sig`) exactly once, each digest equals the SHA-256 of the file in `dist`, no entry names a missing file, lines are `<64 hex>␠␠<name>`.
- AC5 (`checksums-signature`): `checksums.txt.minisig` exists, has the four-line minisign shape, and its trusted comment names `file:checksums.txt` and `version:<version>` (the cryptographic check stays in `sign-checksums.sh` and `smind update`).
- AC6 (`latest-json`, skipped with `--skip-latest`): `latest.json` parses; `version` equals the desktop version; platforms are exactly `darwin-aarch64`, `darwin-x86_64`, `windows-x86_64`; each `url` equals `https://github.com/<repo>/releases/download/<tag>/<percent-encoded asset>` and the asset is in `dist`; each `signature` is non-empty and equals the trimmed content of that asset's `.sig`; `pub_date` is a valid timestamp.
- AC7 (`sidecar-versions`): every daemon archive's binary (`smind` / `smind.exe`) carries `internal/version.Version=<version>` in its embedded build settings (readable from any target without running it); each macOS `.app.tar.gz` has `Info.plist` `CFBundleShortVersionString` equal to the desktop version and a `Contents/MacOS/smind` sidecar with the same embedded version as the daemon archives. Not checked, and said so in the help text: the dmg internals and the Windows installer payload (neither can be opened portably in CI; the app tarball is the same bundle the dmg wraps).
- AC8 (CI wiring): `release-please.yml` `publish` runs the verifier before the first upload (without `latest.json`) and again with `latest.json` generated into `dist` before that file is uploaded, so a bad manifest is never published. On a dry run it runs with the dev version and the manifest version as `--desktop-version`.
- AC9 (docs): a short section in the release docs lists the checks and their ids.

### Named tests (`scripts/verify-release-artifacts.test.mjs`, `node --test`)

Fixtures are built into a temp dir by a helper (real `tar`/`zip`, tiny files); every test below starts from the good set.

- `verify-good-set-passes`
- `verify-source-version-mismatch-<each of the 5 files>` (table)
- `verify-tag-mismatch`
- `verify-missing-daemon-archive`, `verify-stray-daemon-archive-other-version`
- `verify-missing-desktop-asset-<each>` (table), `verify-desktop-asset-wrong-version`
- `verify-checksum-wrong-digest`, `verify-checksum-missing-entry`, `verify-checksum-extra-entry`, `verify-checksum-duplicate-entry`, `verify-checksum-bad-line`
- `verify-minisig-missing`, `verify-minisig-wrong-version`, `verify-minisig-wrong-file`
- `verify-latest-wrong-version`, `verify-latest-missing-platform`, `verify-latest-wrong-url`, `verify-latest-asset-not-in-dist`, `verify-latest-signature-mismatch`, `verify-latest-empty-signature`, `verify-latest-bad-pub-date`, `verify-skip-latest-ignores-bad-manifest`
- `verify-sidecar-version-mismatch-daemon`, `verify-sidecar-version-mismatch-app`, `verify-plist-version-mismatch`, `verify-sidecar-prefix-version-not-accepted` (`0.20.0` must not match `0.20.01`)
- `verify-reports-every-failure` (two broken things, both reported), `verify-dry-run-dev-version-split` (`--version 0.20.0-dev+abc --desktop-version 0.20.0`)

Mutation table (each must make the named test fail; recorded in Validation): comparing the digest case-insensitively off, dropping the duplicate check, accepting a version prefix in the sidecar match, skipping the signature-equals-`.sig` comparison, dropping the `file:` comment check.

## Decisions

- Node, `node:test`, no dependencies, like `desktop/scripts/*.mjs`; `tar` and `unzip` are invoked through `execFile` (never a shell) with fixed arguments, and member names are matched against a fixed path, never extracted to disk by name from the archive.
- The sidecar version is read from the Go binary's embedded `-ldflags` build setting, because it works for every target without executing a foreign binary. Limit: it proves what the linker was told, not that a different binary was swapped in; the checksums bind the bytes.
- Stays a verifier, not a generator: it never writes into `dist`.

## Progress

- [ ] Item 1 verifier
- [ ] Item 2 CI-green gate
- [ ] Item 3 crash capture
- [ ] Item 4 `requireSignedVersion`
- [ ] Item 5 notarization prep

## Validation

(filled per item as each PR lands)
