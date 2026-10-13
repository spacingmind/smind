# sMind as the product display name

Size: M (4) — scope 2, PRs 1, risk 1 (installer/updater), decisions 0, verify 1 (UI screenshots + macOS bundle).

## Goal

The product's *display* name is "sMind" (the landing site already uses it). Identifiers
stay `smind`: the command/binary, Go module, npm/cargo packages, `~/.smind`, `SMIND_*`,
URLs/repo, release asset names (`smind_<v>_…`, `smind-desktop-…`), RPC/event names,
localStorage keys, bundle identifier `dev.spacingmind.desktop`, deep-link scheme, test ids.

## Decision: `productName` → `sMind` (with the MSI upgrade code pinned)

Evidence (tauri-plugin-updater 2.12.0 in the cargo registry, tauri-bundler `dev` sources):

- **macOS updater** extracts the `.app.tar.gz`, drops the archive's top-level dir
  (`.iter().skip(1)`) and moves the contents over the *currently running* `.app`
  path. An existing `/Applications/smind.app` is updated in place whatever the new bundle
  is called; new installs get `sMind.app`. Main executable is the Cargo name
  (`smind-desktop`) and the sidecar is `Contents/MacOS/smind`; neither depends on `productName`.
- **App data/logs/keychain/TCC** are keyed by the bundle identifier, unchanged.
- **Windows NSIS** (the updater asset, `*-setup.exe`): install dir `$LOCALAPPDATA\${PRODUCTNAME}`,
  `Uninstall\${PRODUCTNAME}` and `Software\<manufacturer>\${PRODUCTNAME}` registry keys and
  `$DESKTOP\${PRODUCTNAME}.lnk` are all case-insensitive on Windows, so an existing
  `smind` install is upgraded in place.
- **Windows MSI**: the WiX `UpgradeCode` defaults to `uuid5(DNS, "<productName>.exe.app.x64")`,
  which is case-sensitive. Changing `productName` would silently create a second product. We pin
  `bundle.windows.wix.upgradeCode` to the old value `22b7a470-ef38-5084-842b-a2b04d57475b`
  (= uuid5(DNS, "smind.exe.app.x64")).
- Artifact names: the bundler emits `sMind.app`, `sMind_<v>_<arch>.dmg`, `sMind.app.tar.gz`,
  `sMind_<v>_x64-setup.exe`; the workflows rename the macOS ones to `smind-desktop-<v>-…`
  and `gen-latest-json` matches `-setup.exe` by suffix, so published asset names keep their shape
  (the Windows installer/MSI file name gains the capital M).

## Acceptance criteria

1. User-visible UI text (wordmark, titles, menus, dialogs, toasts, notifications, tray/dock,
   offline page, `<title>`, manifest) says "sMind"; commands and code spans still say `smind`.
2. README/docs prose and CLI help prose say "sMind" where they name the product; commands stay `smind`.
3. `tauri.conf.json` `productName` = `sMind`, WiX upgrade code pinned, every workflow path/glob/Taskfile
   target/test that referenced `smind.app` updated; `task desktop:mac` produces `sMind.app` + DMG.
4. No identifier from the "stays" list above changed (diff grep proves it).
5. Light + dark screenshots of sidebar top strip, collapsed rail, Settings, update dialog, offline page.

## Test scenarios

- web: wordmark/title/update-dialog/update-badge/notification tests assert "sMind".
- desktop: `cargo test` in `desktop/src-tauri`; `node --test desktop/scripts/*.test.mjs`.
- `task test`, `task lint`, web typecheck/tests.
- `task desktop:mac` → `bundle/macos/sMind.app`, `Contents/MacOS/{smind-desktop,smind}` present,
  Info.plist `CFBundleName`=sMind, URL scheme `smind` intact.

## Progress

(see commits)

## Validation

(filled in at the end)
