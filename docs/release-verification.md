# Release verification

Before a release uploads anything, `release-please.yml` (job `publish`) runs
`scripts/verify-release-artifacts.mjs` over the assembled `dist/` directory. It
reads only; it never changes `dist`. Every check runs and every failure is
listed, so one run shows the whole disagreement.

```
node scripts/verify-release-artifacts.mjs --dir dist --version X.Y.Z --tag vX.Y.Z \
  --repo owner/name [--desktop-version X.Y.Z] [--source-root .] [--skip-latest]
```

Exit 0: consistent. Exit 1: at least one `<check-id>: <message>` line. Exit 2: usage error.
`node scripts/verify-release-artifacts.mjs --help` prints the same table.

| Check id | What must agree |
|---|---|
| `source-versions` | `.release-please-manifest.json`, `desktop/package.json`, `tauri.conf.json` and both `Cargo.toml` files equal the desktop version |
| `tag` | the tag is `v<version>` |
| `daemon-archives` | exactly the six daemon archives for this version, no stray one |
| `desktop-assets` | the dmgs, updater tarballs and `.sig` files, the Windows installer and msi for the desktop version, no stray one |
| `checksums` | `checksums.txt` lists every release file once, with the right SHA-256 |
| `checksums-signature` | `checksums.txt.minisig` has the minisign shape and its signed comment names this file and version |
| `latest-json` | the updater manifest points at assets that exist, carries their signatures, and the desktop version |
| `sidecar-versions` | every daemon binary, and the daemon inside each macOS app tarball, embeds this version; the app's `Info.plist` has the desktop version |

On a dry run the daemon is stamped `<manifest>-dev+<sha>` while the desktop app
keeps the manifest version, which is why the workflow passes both versions.

Not checked: the inside of the `.dmg` and of the Windows installer (neither can be
opened portably in CI; the `.app.tar.gz` is the same bundle the dmg wraps), the
cryptographic validity of the signatures (`sign-checksums.sh` and `smind update`
do that), and the mobile app, which is versioned on its own for the app stores.
The sidecar version is read from the binary's embedded build settings; it proves
what the linker was told, and the checksums bind the bytes.

`latest.json` is verified a second time, right after it is generated and before it
is uploaded, so a manifest that disagrees with the assets is never published.

## CI must be green on the release commit

`release-please` and `ci.yml` both start on the same push to `develop`, independently.
So that a release cannot publish from a red, still-running or never-run commit, the
`ci-gate` job (`scripts/ci-green-gate.mjs`) asks the GitHub API whether the `ci` job
of the `CI` workflow succeeded for exactly the release commit, and `publish` needs it.
It only guards the upload: the builds run in parallel with it.

What it decides, in order:

| Situation | Result |
|---|---|
| The tag does not resolve to the commit under release (it moved) | refuse |
| The tag does not exist | refuse |
| No push run of `ci.yml` for that commit, after 10 minutes | refuse |
| Several runs for the commit (a re-run, a manual dispatch) | the newest run decides; an older green run never rescues a newer red one |
| The `ci` job is `queued` or `in_progress` | wait, up to 40 minutes, then refuse |
| The `ci` job is `failure`, `cancelled`, `timed_out`, `skipped` or anything but `success` | refuse |
| The run has no job named `ci` | refuse |
| `success` | pass |
| The GitHub API fails | retried twice, then refuse with exit 3 |

A run for any other commit is ignored. There is no override flag.

If the gate refuses, the tag and the (empty) GitHub Release that `release-please`
created already exist; no asset, `latest.json`, container image or master fast-forward
is published. To recover: fix the cause, then either re-run the failed jobs of the
release run (the release outputs are preserved, so the gate is evaluated again), or cut
a new release and delete the empty one.

Try it without publishing anything, with your own `gh` login:

```
node scripts/ci-green-gate.mjs --dry-run --repo owner/name --sha <40-hex commit>
```

It prints `WOULD PASS: ...` or `WOULD REFUSE: ...`, never waits and exits 0. A
`workflow_dispatch` run of `release-please.yml` runs the gate the same way.
