# macOS signing and notarization: the runbook

This is for the person who owns the Apple Developer account. **Nothing here has been run against Apple**; the scripts and the workflow are prepared and tested only with fake tools. The decisions behind it are in `docs/decisions/0060-desktop-macos-signing-and-notarization.md` (Proposed): read it first.

## 1. What to get

1. Enrol in the Apple Developer Program (USD 99 a year). An organisation account shows the organisation's name to users; an individual shows a person's.
2. Create a **Developer ID Application** certificate (Certificates, Identifiers & Profiles). Export it with its private key from Keychain Access as a `.p12` with a password.
3. In App Store Connect, Users and Access, Integrations, create an **API key** with the Developer role (it is for notarization). Download the `.p8` once, and note its **Key ID** (10 characters) and the **Issuer ID** (a UUID).

## 2. The six secrets

Store them in the GitHub Environment `macos-signing` (Settings, Environments). Nowhere else. **Set the Environment up before the first run; these are requirements:** a manual run executes the workflow and the scripts of the branch it is started from, so without the policy anyone with write access could start a branch whose script prints the secrets.

- Deployment branches: **selected branches only**, the protected branch the workflow is released from (`develop`). Never "all branches". No tag patterns, unless a tag ruleset restricts who can create `v*` tags.
- Branch protection on that branch requires a pull-request review.
- Required reviewers: yourself, with **Prevent self-review** on. Before approving a run, check that the ref has not changed `.github/workflows/`, `scripts/macos-sign/` or `desktop/src-tauri/entitlements/`.

| Secret | Value |
|---|---|
| `SMIND_MACOS_CERT_P12_BASE64` | `base64 -i DeveloperID.p12 \| pbcopy` |
| `SMIND_MACOS_CERT_PASSWORD` | the `.p12` export password |
| `SMIND_MACOS_SIGN_IDENTITY` | the certificate's full name, for example `Developer ID Application: Example Ltd (ABCDE12345)` (`security find-identity -v -p codesigning`) |
| `SMIND_NOTARY_KEY_P8_BASE64` | `base64 -i AuthKey_XXXXXXXXXX.p8 \| pbcopy` |
| `SMIND_NOTARY_KEY_ID` | the 10-character Key ID |
| `SMIND_NOTARY_ISSUER` | the Issuer ID (UUID) |

Then set the repository variable `MACOS_SIGNING_ENABLED` to `true` (Settings, Secrets and variables, Variables, a repository variable: the gate job cannot see the Environment). Without it the `gate` job says so and the signing job does not run; with it but without a secret, the signing job stops at once and names the missing variable (it cannot reach Apple without all six). The signing job also runs only from `develop`.

## 3. Try it locally first

On a Mac, with the six values in your environment (a shell session, not a file in the repository):

```
scripts/macos-sign/sign-notarize.sh --check          # certificate imports, identity found; signs nothing
scripts/macos-sign/sign-notarize.sh --app path/to/sMind.app --out out --dmg out/sMind.dmg
scripts/macos-sign/verify.sh --app path/to/sMind.app --dmg out/sMind.dmg
```

The script creates a temporary keychain and deletes it when it exits; it never touches your login keychain. The `.p12` password is passed to `security import` on its command line (the tool has no other way), and the random temporary-keychain password goes to `security create-keychain`, `unlock-keychain` and `set-key-partition-list` the same way, so they are visible in `ps` to other users of that machine for a moment: do this on your own machine or a single-user runner. `base64 -d` needs macOS 13 or later.

## 4. Run it in CI

Actions, `desktop-macos-signed`, Run workflow, with a release tag such as `v0.21.0`. It downloads that release's ad-hoc macOS app, signs it (the Go sidecar first, then the app, hardened runtime, the committed entitlements), notarizes it and waits, staples it, runs `verify.sh`, and uploads the dmg, the updater tarball and Apple's log as **artifacts of the run (7 days)**. It publishes nothing.

The updater tarball it produces is **not signed with the updater key**: the old `.sig` no longer matches. Until the release workflow itself signs (decision 1 of the ADR), re-sign it by hand (`tauri signer sign`) and attach the dmg, tarball and `.sig` to the release.

## 5. When it fails

- **The gate says off:** the repository variable is not `true`.
- **"missing secret environment variable(s)":** the signing job names which of the six is absent or empty (the Environment, not the repository, must hold them).
- **"nested code this script does not sign":** the bundle has `Helpers`, `XPCServices`, `PlugIns` or `Library/LaunchServices`; the script refuses rather than leave them unsigned. Extend it (and its tests) first.
- **"signing identity is not in the imported certificate":** `SMIND_MACOS_SIGN_IDENTITY` does not match the `.p12`, or the certificate expired.
- **Notarization `Invalid`:** the script prints the status and writes Apple's log to `notarization-log-app.json` (also uploaded). Typical causes: an unsigned nested binary, a missing hardened runtime or timestamp, a disallowed entitlement. Fix the cause; do not weaken the verification. If the cause is a runtime exception the app really needs, add only that entitlement to `desktop/src-tauri/entitlements/` and record why in the ADR.
- **Roll back:** disable by setting `MACOS_SIGNING_ENABLED` to anything but `true`; releases keep working ad hoc. Revoke a leaked certificate in the Apple developer portal and the API key in App Store Connect, then replace the secrets.
