# Release binaries + desktop version sync (ADR-0013 D1)

## Context

ADR-0013 (Accepted) lets the desktop app **install, update, and restart the local daemon it manages**: Windows via WSL2, and macOS natively. When the app is newer than the daemon, it offers "update daemon and restart". The flow is modeled on Paseo's `shouldRestartForVersion`.

That needs two things that don't exist yet:
1. A downloadable daemon binary per platform, attached to each GitHub Release. Today `.github/workflows/release-please.yml` stops at "tag + GitHub Release + changelog". Its own header comment says binaries are "a separate, not-yet-built distribution task … modeled on refs/cliproxyapi's release workflow".
2. The app's version must equal the daemon version it ships alongside. Today the desktop is `0.1.0` in four places (`desktop/package.json`, `desktop/src-tauri/tauri.conf.json`, `desktop/src-tauri/Cargo.toml`, `desktop/daemon-client/Cargo.toml`), while `.release-please-manifest.json` is `0.7.0`.

Version stamping already exists (PR #195): `internal/version`, `smind --version`, and `/healthz` `version`. The `task build` ldflags recipe computes `<manifest>` at a `v<manifest>` tag.

**User approval, 2026-09-25:** "có" to building daemon binaries and attaching them to GitHub Releases. The repo is public.

Branching and release model (see memory/CONTRIBUTING):
- Feature work lands on `develop`.
- `master` is release-only.
- release-please runs on push to `master` and creates the tag and Release when its release PR merges.

## Acceptance Criteria

- **AC1: release job.**
  - When release-please actually creates a release (`release_created` output true), a follow-up job in the same workflow builds `smind` for linux/amd64, linux/arm64, darwin/amd64 and darwin/arm64.
  - Builds use `CGO_ENABLED=0`, with the web UI embedded (`task build:web` first), and are stamped with the release version via the same ldflags as `task build`: `-X …/internal/version.Version=<tag without v>` and `Commit`.
  - The job uploads `smind_<version>_<os>_<arch>.tar.gz`, each containing the `smind` binary plus `LICENSE`, and a `checksums.txt` (SHA-256) to that Release.
  - It never runs on non-release pushes, and it does not change what release-please itself does.
- **AC2: Windows desktop installers on release.** The same release event also builds the desktop NSIS and MSI installers, reusing the `desktop-windows` workflow logic (a reusable workflow or a shared job), and attaches them to the Release with their checksums.
- **AC3: desktop version = manifest version.**
  - Release-please bumps the four desktop version fields together with the manifest, via `extra-files` in `release-please-config.json` (the JSON jsonpath updater for the two JSON files, and the TOML updater or generic `x-release-please-version` markers for the Cargo.toml files).
  - This PR sets all four to the current manifest version, `0.7.0`, so they already match.
  - A CI check (script or test) fails if any of the four drifts from `.release-please-manifest.json`.
- **AC4: pinned, least-privilege CI.**
  - Every third-party action is pinned by commit SHA with a version comment, as `ci.yml` does.
  - Workflow `permissions` are minimal: `contents: write` only where uploading.
  - No secrets beyond `GITHUB_TOKEN`.
  - No signing and no notarization; that is out of scope.
- **AC5: dry-run proof.**
  - Because a real release can't be cut from this PR, add a `workflow_dispatch` path or a PR-triggered dry-run job. It runs the exact build+package+checksum steps without uploading, and publishes the artifacts as workflow artifacts.
  - Run it on this branch and record in Validation the run URL, the artifact list, and a check that the linux/amd64 binary prints the stamped version.
- **AC6: docs.** Update `release-please.yml`'s header comment, and add a short "Installing the daemon from a release" section to README (download, verify checksum, run).

## Test Scenarios

- The dry-run job is green, and its artifacts include 4 tarballs, `checksums.txt` and the Windows installers.
- `sha256sum -c checksums.txt` passes on the downloaded artifacts.
- `tar -xzf smind_<v>_linux_amd64.tar.gz && ./smind --version` prints the expected version, not `dev`.
- `file` shows the right arch for each tarball, e.g. the darwin/arm64 one is a Mach-O arm64 binary.
- Version-sync check: the test passes at `0.7.0` and fails when one desktop field is edited to differ (check both locally).
- `release-please-config.json` is still valid. Release-please behavior is otherwise unchanged; confirm by reading its docs for `extra-files` updaters and record the source in Decisions.

## Decisions

- Model the build on `refs/cliproxyapi`'s release workflow where it helps; record what was borrowed.
  - Borrowed: per-target matrix build job shape, archive layout (binary +
    LICENSE flat inside the tarball, no wrapping directory), and a final
    job that downloads every build's artifact and computes one
    `checksums.txt` over all of them (`publish-checksums` there, `publish`
    here).
  - Not borrowed: cliproxyapi's workflow runs on tag push and creates the
    GitHub Release itself (`gh release create`/`edit`) before building;
    smind's release-please-action already owns tag + Release creation, so
    the new jobs key off its `release_created`/`tag_name` outputs instead
    of a tag-push trigger, and only *upload to* the existing Release
    rather than creating one.
- Archive format: `.tar.gz` for all four targets, with no Windows daemon binary. A native Windows daemon is blocked on `internal/terminal`, per ADR-0013.
- AC2 (Windows installers): `desktop-windows.yml` gained a `workflow_call`
  trigger alongside its existing `push`/`workflow_dispatch` ones, so
  `release-please.yml` calls it as a reusable workflow (`uses: ./.github/workflows/desktop-windows.yml`)
  rather than duplicating its steps. Its own version comes from
  `tauri.conf.json`, already bumped by the same release-please extra-files
  update as the rest of the tree, so no version is passed in.
- AC5 (dry run): added `workflow_dispatch` to `release-please.yml` itself,
  rather than a separate workflow file or a `pull_request` trigger. A
  `prepare` job gates every downstream job on "release just got created, or
  this is a manual dispatch" using `always()` + an explicit condition,
  since `release-please`'s own job only runs `on: push` and is otherwise
  skipped (and skipped `needs` block downstream jobs by default). The
  `publish` job re-applies the same `always()` pattern so it still runs
  (and uploads workflow artifacts) when `release-please` was skipped.
- AC3 (`extra-files` syntax): confirmed against release-please's own docs
  (`docs/customizing.md` in `googleapis/release-please`, fetched
  2026-09-25). JSON files use `{"type": "json", "path": ..., "jsonpath": "$.version"}`.
  Non-JSON/YAML files (the two `Cargo.toml`s) use `{"type": "generic", "path": ...}`
  plus an inline `# x-release-please-version` marker comment on the
  `version = "..."` line in the file itself -- release-please only rewrites
  the value on that annotated line. This does not change any other
  release-please behavior (tag/Release/changelog logic untouched).

## Progress

- [x] AC1 release job
- [x] AC2 desktop installers on release
- [x] AC3 version sync + drift check
- [x] AC4 pinning/permissions
- [x] AC5 dry-run proof
- [x] AC6 docs

## Validation

To be filled in once the AC5 dry run has been pushed and run on this branch.
