# Desktop app (Tauri shell)

The thin desktop client: a Rust shell (`src-tauri/`) around the bundled web UI, with the daemon as a sidecar on macOS and Windows. Build commands live in the repository `Taskfile.yml` (`task desktop:mac` for the unsigned macOS app) and `scripts/desktop-audit-build.sh` builds an isolated copy for a real-window audit.

## Generated files

`src-tauri/gen/schemas/*.json` (the capability schemas the editor uses for autocompletion of `capabilities/*.json`, the `$schema` hint at the top of those files) are written by `tauri-build` on every build and are not tracked: `src-tauri/.gitignore` ignores them. They do not exist in a fresh checkout; any `cargo build`, `cargo check`, `cargo test` or `tauri build` in `src-tauri/` creates them, after which the editor hint resolves. Nothing in the build, the tests or CI reads them, so never commit them or fix a diff in them.
