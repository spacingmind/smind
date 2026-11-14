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

## Item 1 decisions

- Node, `node:test`, no dependencies, like `desktop/scripts/*.mjs`; `tar` and `unzip` are invoked through `execFile` (never a shell) with fixed arguments, and member names are matched against a fixed path, never extracted to disk by name from the archive.
- The sidecar version is read from the Go binary's embedded `-ldflags` build setting, because it works for every target without executing a foreign binary. Limit: it proves what the linker was told, not that a different binary was swapped in; the checksums bind the bytes.
- Stays a verifier, not a generator: it never writes into `dist`.

## Item 2: CI green on the release commit (gate)

### How a release is chained today (checked 2026-11-15)

`release-please.yml` and `ci.yml` both trigger on `push` to `develop`, independently and in parallel. `release-please` creates the tag and the GitHub Release (already public, empty); `build-*` jobs then build; `publish` uploads the assets and `latest.json`. Nothing in `publish` looks at CI, so a release can ship from a commit whose CI is red, still running, or never ran. Branch protection makes `ci` required for merging PRs, not for what happens after the push.

### Decisions

- A script, `scripts/ci-green-gate.mjs`, asks the GitHub API (through `gh api`, so tests use a fake `gh` on `PATH`) whether the `ci` job of the `CI` workflow (`ci.yml`) succeeded for exactly the release commit. The `ci` job is the repository's required check; the whole run is not used because `windows-terminal` in the same workflow is deliberately not required.
- "Exactly": the run's `head_sha` must equal the full 40-hex commit under release, found with `event=push` on `--branch` (default `develop`: `master` also gets a push run for the same commit when it is fast-forwarded to the tag, and a re-run must not judge that one); runs for any other sha or branch are ignored. The tag must resolve (lightweight or annotated) to that same commit, else the tag moved after the workflow started and the gate refuses.
- Several runs for the sha (a re-run keeps its run id and the jobs endpoint reports the latest attempt; a manual dispatch is a new run): the newest run by `created_at` decides, success or not. An older green run never masks a newer red one.
- States: `ci` job `success` passes. `failure`, `cancelled`, `timed_out`, `skipped`, `neutral`, `action_required` refuse. `queued`/`in_progress`: poll until it ends or `--timeout` (default 40 min) passes, then refuse. No run yet: poll for `--grace` (default 10 min) in case the run is being created, then refuse. No `ci` job in the run: refuse. API errors are retried 3 times, then refuse with exit 3. There is no override flag: the way past a refusal is to fix CI and re-run the failed jobs of the release run (outputs are preserved), or cut a new release.
- New job `ci-gate` in `release-please.yml`, after `release-please`, only on a real release (`release_created == 'true'`) and, in `--dry-run`, on `workflow_dispatch`. `publish` needs it and requires `result == 'success'` on a real release (the dry run, which has no real release, ignores the gate's verdict but the gate still runs and reports). Builds run in parallel with the gate: a red CI wastes a build but never an upload.
- `--dry-run`: prints the verdict and `WOULD REFUSE: <reason>` or `WOULD PASS`, never waits, exits 0 for every verdict (usage errors exit 2 and a persistent API error exit 3, which are not verdicts). It is how the gate is exercised locally (`node scripts/ci-green-gate.mjs --dry-run --repo owner/name --sha <sha>` with your own `gh` login) and from a branch dispatch.
- The token is only read by `gh` from `GH_TOKEN` in the environment; the script never reads, prints or passes it on a command line. Job permissions: `contents: read`, `actions: read`.
- What it cannot do: the GitHub Release and tag created by `release-please` before any asset is built stay public and empty if the gate refuses (the gate guards the upload, not the tag). Recovery steps are in `docs/release-verification.md`.

### Acceptance criteria

- AC1: a release proceeds to upload only when the `ci` job of the newest `ci.yml` push run for exactly the release sha succeeded.
- AC2: a failed, cancelled, skipped or timed-out `ci` job, a run for a different sha only, no run after the grace, a still-running run after the timeout, a missing `ci` job, a moved tag, an unresolvable tag and a persistent API error each refuse (exit 1, or 3 for the API error) with the reason named.
- AC3: a still-running run that turns green within the timeout passes; a re-run that turns a failed run green passes; an older green run does not rescue a newer red one.
- AC4: `--dry-run` never exits non-zero for a verdict (only usage 2 / API error 3), never waits, and prints `WOULD PASS` / `WOULD REFUSE: <reason>`.
- AC5: usage errors exit 2 (missing `--repo`/`--sha`, a sha that is not 40 hex).
- AC6: the token never appears in output.
- AC7: in `release-please.yml`, `publish` needs `ci-gate` and requires its success on a real release; `ci-gate` runs only on a real release or a dispatch and passes the commit and the tag. Pinned by a static test.
- AC8: the failure modes and recovery are documented.

### Named tests (`scripts/ci-green-gate.test.mjs`, fake `gh` fixture)

`gate-green-passes`, `gate-failed-run-refuses`, `gate-cancelled-refuses`, `gate-skipped-job-refuses`, `gate-missing-run-refuses-after-grace`, `gate-in-progress-then-green-passes`, `gate-in-progress-timeout-refuses`, `gate-rerun-after-failure-passes`, `gate-newest-run-wins-failure`, `gate-other-sha-run-ignored`, `gate-job-missing-refuses`, `gate-tag-moved-refuses`, `gate-annotated-tag-resolves-and-passes`, `gate-tag-not-found-refuses`, `gate-api-error-retries-then-passes`, `gate-api-error-persistent-refuses`, `gate-bad-sha-usage-error`, `gate-missing-args-usage-error`, `gate-dry-run-never-fails`, `gate-dry-run-does-not-wait`, `gate-token-never-printed`; added in review: `gate-path-shaped-input-usage-error`, `gate-other-branch-run-ignored`, `gate-equal-timestamps-higher-run-id-wins`, `gate-unparsable-created-at-never-wins`, `gate-404-in-tag-name-is-not-not-found`. The wiring tests (`gate-workflow-wiring`, `gate-ci-workflow-job-name`, `gate-wiring-no-escape-hatches`, `gate-wiring-invocation-shape`, `gate-wiring-permissions-exact`, `gate-wiring-publish-needs-superset`, `gate-wiring-release-uploads-only-in-publish`) live in `scripts/ci-green-gate-wiring.test.mjs`.

Mutations (each must fail the named test): newest-run rule replaced by "any run succeeded" -> `gate-newest-run-wins-failure`; in-progress treated as pass -> `gate-in-progress-timeout-refuses`; sha filter dropped -> `gate-other-sha-run-ignored`; tag check skipped -> `gate-tag-moved-refuses`; `--dry-run` exit code follows the verdict -> `gate-dry-run-never-fails`; `ci-gate` removed from `publish.needs` -> `gate-workflow-wiring`.

## Progress

- [x] Item 1 verifier (script + 42 tests by peer-glm, CI wiring, `docs/release-verification.md`)
- [x] Item 2 CI-green gate (script + tests by peer-glm, wiring, wiring test, docs, input hardening by the lead)
- [ ] Item 3 crash capture
- [ ] Item 4 `requireSignedVersion`
- [ ] Item 5 notarization prep

## Validation

### Item 1

- `node --test scripts/verify-release-artifacts.test.mjs`: 42 pass (every test in the named list; a CLI exit-code test is added).
- `actionlint .github/workflows/release-please.yml`: clean.
- Mutations (each applied to the committed script, the named test failed, then restored): digest comparison off -> `verify-checksum-wrong-digest`; duplicate check off -> `verify-checksum-duplicate-entry`; prefix accepted in the sidecar match -> `verify-sidecar-prefix-version-not-accepted`; signature-equals-`.sig` comparison off -> `verify-latest-signature-mismatch`; `file:` comment check off -> `verify-minisig-wrong-file`.
- Real Go binaries built with the release ldflags (linux/darwin host build and a windows/amd64 cross build) were confirmed to contain `internal/version.Version=<v>` in their build settings, the basis of AC7.
- Run against the real published v0.20.0 assets (downloaded to a scratch dir, deleted afterwards): all 8 checks `ok`, so the naming and layout rules match what the pipeline produces today.
- Not verified: a real release run (the workflow step runs only on `develop` pushes and dry-run dispatches; no dispatch was made from this branch).

### Item 2

- `node --test scripts/ci-green-gate.test.mjs scripts/ci-green-gate-wiring.test.mjs`: the named tests plus the review additions, all pass (see the run in the PR).
- Mutations on the committed script, each failed the named tests: newest-run rule inverted -> `gate-rerun-after-failure-passes`, `gate-newest-run-wins-failure`; in-progress treated as done -> `gate-in-progress-*`, `gate-dry-run-does-not-wait`; sha filter dropped -> `gate-other-sha-run-ignored`; tag check skipped -> `gate-tag-moved-refuses`; dry-run exit follows the verdict -> `gate-dry-run-never-fails`; `ci-gate` removed from `publish.needs` -> `gate-workflow-wiring`.
- `actionlint .github/workflows/release-please.yml`: clean.
- Real GitHub, `--dry-run` with a read-only `gh` login (nothing published): a develop commit with a green push run -> `WOULD PASS`; a sha with no run -> `WOULD REFUSE: no ci.yml push run found`; the v0.20.0 tag against a different sha -> `WOULD REFUSE: tag ... points at commit ..., expected ...`.
- Second read added structure pins on the workflow; each mutation fails a named test: `|| true` on the gate command, `= "true"` flipped to `!=`, `continue-on-error: true` on `ci-gate`, `|| always()` in publish's `if`, `ci-gate` dropped from `publish.needs`, `actions: write` added, a `gh release upload` in another job.
- Found in review of the peer's script and fixed: `--tag`/`--workflow`/`--job` went unvalidated into an API path (read-only GET, but a `?`/`/` could have queried another endpoint); now rejected with exit 2 before any call.
- Not verified: a real release run or a `workflow_dispatch` dry run of `release-please.yml` (the two jobs are read by actionlint and a static test, not executed); the wait paths against a really running CI.
