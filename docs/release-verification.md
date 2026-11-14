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
