# 0060: Desktop macOS code signing and notarization with a Developer ID

## Status

**Proposed — 2026-11-15. The Human decided on 2026-11-15 NOT to enrol in the Apple Developer Program for now: this ADR stays Proposed, decisions 1 to 9 below are recorded as pending the Human's account decision, the workflow stays off (default-off, no secrets exist), and the macOS app stays ad-hoc signed and un-notarized (a Gatekeeper prompt on first run, as today; the runbook says so). The scripts and the workflow skeleton ship so that enabling signing later is a settings change, not a project.** Supersedes the part of ADR-0023 decision 2 that says "Apple notarization ... stay out of scope"; the rest of ADR-0023 (the Tauri updater and its minisign signatures) stands unchanged. Everything that can be prepared without an Apple Developer account is prepared (scripts, entitlements, a disabled workflow, a runbook: `docs/plans/active/desktop-notarization-prep.md`) and **none of it has been run against Apple**. The account is the Human's money and identity decision, so every choice under [Decisions for the Human](#decisions-for-the-human) carries a recommendation and nothing is switched on before the Human answers.

## Context

The macOS app is ad-hoc signed (`bundle.macOS.signingIdentity: "-"`), with no hardened runtime, no entitlements and no notarization. An ad-hoc app downloaded from the internet is quarantined by Gatekeeper: macOS refuses to open it with "cannot be opened because the developer cannot be verified" until the user right-clicks Open or removes the quarantine attribute. That is a poor first run for a desktop app and an unwelcome instruction ("run `xattr -d`") for a tool that asks for the user's shell and credentials. Notarization (Apple scans the signed binary and issues a ticket that is stapled to it) removes the prompt, and the hardened runtime that notarization requires narrows what a compromised process can do.

The app is more than one binary: the Tauri shell plus the bundled Go daemon (`Contents/MacOS/smind`, a Tauri `externalBin` sidecar), and the dmg and the updater tarball (`*.app.tar.gz`) that wrap the same bundle. Facts that shape the design:

| Fact | Where |
|---|---|
| Nested code (the sidecar, any framework) must be signed before the bundle, never with `--deep`; every executable needs `--options runtime` and a secure `--timestamp` to be notarized | Apple's notarization requirements |
| The updater tarball and its minisign `.sig` are made by the same `tauri build` that makes the dmg; changing the app afterwards means regenerating the tarball and re-signing it with the updater key | `desktop-macos.yml`, `tauri.updater.conf.json` |
| The updater verifies its own minisign signature (and, since the `requireSignedVersion` change, the signed version); it does not look at the Apple signature | ADR-0023, `desktop-require-signed-version.md` |
| Notarization needs an Apple Developer Program membership (USD 99 a year, an individual or an organisation), a "Developer ID Application" certificate and either an App Store Connect API key or an Apple ID with an app-specific password | Apple |
| Pull-request and manual builds have no secrets and stay ad hoc (ADR-0023 decision 4) | `desktop-macos.yml` |

## Options considered

- **A. Tauri-native.** Set `APPLE_*` environment variables in the release build; the bundler signs the app and the sidecar, notarizes and staples, then packs the dmg and the updater tarball from the signed app. One pass, the dmg layout and the tarball come out consistent. Unverified here: how `APPLE_SIGNING_IDENTITY` interacts with the committed `signingIdentity: "-"` (an overlay config with `signingIdentity: null`, which a JSON merge removes, is the planned way), and the bundler's own entitlements and keychain handling.
- **B. Our scripts sign the built app.** `scripts/macos-sign/sign-notarize.sh` signs inside-out, notarizes, staples, optionally rebuilds the dmg and the tarball. Full control, testable here with fake tools, a documented fallback; costs: the dmg window layout is not reproduced and the tarball must be re-signed with the updater key afterwards.
- **C. Do nothing.** Keep ad hoc; document the Gatekeeper workaround. Free, and the status quo.

The scripts serve both A and B: `verify.sh` is the read-only gate that runs after either path (strict signature, Developer ID not ad hoc, hardened runtime on every executable, one Team ID, entitlements equal to the committed files, Gatekeeper, stapled tickets), and `sign-notarize.sh --check` is the credential check.

## What is prepared (this PR), and its security rules

- `scripts/macos-sign/{lib.sh,sign-notarize.sh,verify.sh}`: refuse to run, exit 3 naming the missing variables only, unless all six `SMIND_*` secrets are present with the right shape; keep secrets in env vars and 0600 files in a 0700 temp dir removed on exit; use a temporary keychain deleted on exit; pass the notarization key by file path; never `set -x`, never `--deep`; fetch Apple's log on a rejection and staple only after `Accepted`. Tested with fake tools (`scripts/macos-sign.test.mjs`).
- `desktop/src-tauri/entitlements/{app,sidecar}.entitlements`: empty to start (see decision 5).
- `.github/workflows/desktop-macos-signed.yml`: a **disabled skeleton**: `workflow_dispatch` only (so no secret reaches a pull request or a fork), `contents: read`, off unless the repository variable `MACOS_SIGNING_ENABLED` is `true` (a gate job without the Environment cannot see Environment secrets, so the existence of the six secrets is checked inside the signing job by `sign-notarize.sh`, which refuses naming any missing one), develop only, a protected Environment `macos-signing`, the secrets exposed to one step as `env:` only, every action pinned, artifacts of the run only (nothing published). It signs the already-published app of a tag. Joining the release workflow is not done: it depends on decision 1.
- `docs/desktop-macos-signing.md`: the Human's runbook.

## Decisions for the Human

All nine are **pending the Human's account decision** (2026-11-15: not enrolling for now). The recommendations stand for the day the account exists; nothing below is acted on before then.

| # | Decision | Recommendation |
|---|---|---|
| 1 | How the release build signs: A (Tauri-native), B (our scripts), or C (stay ad hoc) | **A for the release build, with `verify.sh` as the gate after it and B kept as the documented fallback.** A keeps the dmg and updater artifacts consistent in one pass; B is what we can test today. Decide after the first dry run on a real account shows whether A's identity override works. |
| 2 | Enrol in the Apple Developer Program, and as whom (an individual or an organisation) | **An organisation, if one exists** (the certificate's name is what users see in Gatekeeper dialogs); otherwise an individual. USD 99 a year; the Human's call. |
| 3 | Notarization credentials | **An App Store Connect API key** (revocable, no personal Apple ID or 2FA in CI) rather than an Apple ID with an app-specific password. The scripts and the workflow use the API key. |
| 4 | Certificate type | **"Developer ID Application" only** (apps distributed outside the App Store). No installer certificate: we ship a dmg and a tarball, not a pkg. |
| 5 | Entitlements | **Start empty** for the app and the sidecar (not sandboxed, no third-party dylibs, static Go binary). Add an exception (`allow-jit`, `allow-unsigned-executable-memory`, `disable-library-validation`) only when a named failure under the hardened runtime requires it, and record the reason here. |
| 6 | Where the secrets live | **A protected GitHub Environment `macos-signing`**, the six secrets stored in it and nowhere else, the repository variable `MACOS_SIGNING_ENABLED` as the master switch. The Environment settings are **requirements, not suggestions**, because a `workflow_dispatch` runs the workflow file and `scripts/macos-sign/*.sh` of the dispatched ref: anyone with write access could push a branch whose script prints the secrets and dispatch it. So: (a) deployment branches limited to the protected branch(es) the workflow is released from (`develop`), never "all branches" and no tag patterns unless a tag ruleset restricts who can create `v*` tags; (b) branch protection on those branches requiring a pull-request review; (c) a required reviewer, with "Prevent self-review" on, who before approving checks that the dispatched ref has not changed `.github/workflows/`, `scripts/macos-sign/` or `desktop/src-tauri/entitlements/`. The workflow also refuses any ref but `develop`, but a branch can edit that condition: only the Environment policy is enforceable. |
| 9 | Path-level control on `develop` and what the signing job will sign | **(a) What it signs is proven, not trusted.** Anyone with write access can create a release with an asset of the right name, so the signing job first checks that the release is published, stable (`vX.Y.Z`, not a draft or prerelease), its tag commit is on `develop`, and that the app tarball is listed in the release's `checksums.txt` whose minisign signature verifies with the updater public key committed in `tauri.conf.json` and names `file:checksums.txt` and the tag's version (`scripts/macos-sign/check-release.sh`, `provenance.mjs`), all in steps without any secret, before the tarball is unpacked. **(b) Who may change the signing paths.** `.github/CODEOWNERS` lists the workflows, `scripts/macos-sign/`, `desktop/src-tauri/entitlements/` and the signing tests; "Require review from Code Owners" is switched on for `develop`. **(c) What the Environment reviewer compares.** The dispatched commit against the LAST APPROVED signing commit (every run records its commit in the run summary; the baseline for the first run is the merge commit of this change): `git diff <last approved> <dispatched> -- .github/workflows scripts/macos-sign desktop/src-tauri/entitlements`; any difference is read before approving. **(d) Nothing else may use the Environment or the secrets**: a test greps every other workflow for `macos-signing` and the secret names. The repository settings (branch policy, code-owner reviews, required reviewer with self-review off, never auto-approve) are the Human's; the runbook has the checklist. |
| 7 | Scope | **Release builds only.** Pull-request and manual builds stay ad hoc (no secrets, as today). The first notarized release should be a patch release the Human installs by hand and updates in place before it is announced. |
| 8 | Windows | **Out of scope here**: Authenticode signing is a separate decision with its own cost and certificate; it needs its own ADR. |

## Consequences

- Users stop seeing the Gatekeeper "developer cannot be verified" block; the hardened runtime narrows what a compromised app or daemon can do.
- The first signed update replaces an ad-hoc app by a Developer ID one. Per-app operating-system grants keyed to the code signature (notifications, keychain access groups, TCC permissions) may be asked again. **Not measured**; the first real update will show it.
- Release time grows by the notarization wait (minutes, occasionally longer); a rejection stops the release at the build step, never after anything is published.
- The Apple credentials become the most sensitive secrets in the repository (a leaked Developer ID certificate lets anyone sign software as the project): hence the protected Environment, the temporary keychain and the tests that pin the handling.
- The updater key and the Apple signature are independent: losing or revoking one does not affect the other, and a revoked Developer ID certificate does not stop already-installed updaters.

## What we are NOT doing

No Apple account is created, no certificate is requested, no workflow is enabled, no release is signed; the entitlements are not tuned against a real notarization; the release workflow is not changed; nothing is verified against Apple's services.

## Limits (what this preparation cannot show)

Whether Apple accepts the bundle with empty entitlements, whether the sidecar notarizes as nested code, whether Tauri's own identity override works (option A), how the first update behaves for an ad-hoc to Developer ID change, and how long notarization takes: all need the real account and a real run. The scripts' behaviour is tested only against fake `codesign`/`security`/`xcrun`.
