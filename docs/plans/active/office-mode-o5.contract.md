---
v: 1
milestone: "Office mode O5: deliverable cards, Open in app / Reveal / Copy path, web Download"
base: origin/develop
planRef: docs/plans/active/office-mode.md
acs:
  - id: AC23
    text: "Detection is deterministic: candidates are the paths of successful write-intent tool calls plus workspace-relative paths in the final assistant message (markdown links, matching inline code), normalised to worktree-relative; a shell command text is never parsed."
    covers: { tests: [candidates_from_write_tools_and_final_message, markdown_links_and_inline_code_in_reading_order, path_normalisation_table, shell_text_never_parsed], frames: [] }
  - id: AC24
    text: "Verification by file.stat: exists, regular, inside the worktree, allow-listed extension, not a mismatch, mtime at or after the run start; an older file, a missing file, a directory, ../x.pdf, ~/x.pdf, https://x.pdf and an absolute path outside the worktree make no card."
    covers: { tests: [verification_drops_old_missing_mismatch_dir, path_normalisation_table], frames: [] }
  - id: AC25
    text: "At most 6 cards per run (then + N more files linking to the Files tab); at most 20 candidates checked; one file.stat per candidate per run."
    covers: { tests: [caps_6_cards_20_candidates, caps_cards_and_summarises_the_rest, verification_drops_old_missing_mismatch_dir], frames: [] }
  - id: AC26
    text: "Card actions: Open (in-app view), Open in app and Reveal in folder on desktop with the local daemon only, Download on the web only, Copy path; nothing auto-opens a tab; cards do not depend on the interface mode; the No-preview panel gains the same actions."
    covers: { tests: [actions_per_platform_and_type, desktop_attached_to_a_remote_daemon_offers_neither_open_in_app_nor_reveal, no_auto_open, both_modes, availableFileActions, desktop_openable_list_matches_rust], frames: [] }
  - id: AC27
    text: "open_path_default(root, rel_path) refuses a path outside the canonical root, a symlink escaping it, a directory (including an .app bundle), an extension off the allow-list, a Windows UNC network path, and any call when the daemon is remote; it opens through the opener plugin path API (no shell, no argv string); error text carries no absolute path; the command is on the exact capability allow-list."
    covers: { tests: [open_path_default_refuses_outside_root, open_path_default_symlink_escape, open_path_default_directory_and_app_bundle, open_path_default_extension_not_allow_listed, open_path_default_unc_on_windows, open_path_default_remote_daemon, open_path_default_error_text_has_no_path, capability_allowlist_exact], frames: [] }
  - id: AC28
    text: "Web Download assembles chunks into a Blob with ifMtime, names the file by a sanitised basename (control characters and separators stripped, at most 120 characters), revokes the object URL, shows progress above 5 MiB and refuses more than 50 MiB."
    covers: { tests: [assembles_with_ifMtime, sanitises_filename, revokes_url, refuses_over_cap], frames: [] }
tests:
  - { name: candidates_from_write_tools_and_final_message, run: "cd web/packages/ui && bunx vitest run src/components/timeline/deliverables.test.ts -t candidates_from_write_tools_and_final_message", timeoutSec: 900 }
  - { name: markdown_links_and_inline_code_in_reading_order, run: "cd web/packages/ui && bunx vitest run src/components/timeline/deliverables.test.ts -t markdown_links_and_inline_code_in_reading_order", timeoutSec: 900 }
  - { name: path_normalisation_table, run: "cd web/packages/ui && bunx vitest run src/components/timeline/deliverables.test.ts -t path_normalisation_table", timeoutSec: 900 }
  - { name: shell_text_never_parsed, run: "cd web/packages/ui && bunx vitest run src/components/timeline/deliverables.test.ts -t shell_text_never_parsed", timeoutSec: 900 }
  - { name: caps_6_cards_20_candidates, run: "cd web/packages/ui && bunx vitest run src/components/timeline/deliverables.test.ts -t caps_6_cards_20_candidates", timeoutSec: 900 }
  - { name: verification_drops_old_missing_mismatch_dir, run: "cd web/packages/ui && bunx vitest run src/components/timeline/deliverable-card.test.tsx -t verification_drops_old_missing_mismatch_dir", timeoutSec: 900 }
  - { name: caps_cards_and_summarises_the_rest, run: "cd web/packages/ui && bunx vitest run src/components/timeline/deliverable-card.test.tsx -t caps_cards_and_summarises_the_rest", timeoutSec: 900 }
  - { name: actions_per_platform_and_type, run: "cd web/packages/ui && bunx vitest run src/components/timeline/deliverable-card.test.tsx -t actions_per_platform_and_type", timeoutSec: 900 }
  - { name: desktop_attached_to_a_remote_daemon_offers_neither_open_in_app_nor_reveal, run: "cd web/packages/ui && bunx vitest run src/components/timeline/deliverable-card.test.tsx -t desktop_attached_to_a_remote_daemon_offers_neither_open_in_app_nor_reveal", timeoutSec: 900 }
  - { name: no_auto_open, run: "cd web/packages/ui && bunx vitest run src/components/timeline/deliverable-card.test.tsx -t no_auto_open", timeoutSec: 900 }
  - { name: both_modes, run: "cd web/packages/ui && bunx vitest run src/components/timeline/deliverable-card.test.tsx -t both_modes", timeoutSec: 900 }
  - { name: availableFileActions, run: "cd web/packages/ui && bunx vitest run src/components/file-actions.test.tsx -t availableFileActions", timeoutSec: 900 }
  - { name: desktop_openable_list_matches_rust, run: "cd web/packages/ui && bunx vitest run src/components/file-actions.test.tsx -t desktop_openable_list_matches_rust", timeoutSec: 900 }
  - { name: assembles_with_ifMtime, run: "cd web/packages/ui && bunx vitest run src/lib/download.test.ts -t assembles_with_ifMtime", timeoutSec: 900 }
  - { name: sanitises_filename, run: "cd web/packages/ui && bunx vitest run src/lib/download.test.ts -t sanitises_filename", timeoutSec: 900 }
  - { name: refuses_over_cap, run: "cd web/packages/ui && bunx vitest run src/lib/download.test.ts -t refuses_over_cap", timeoutSec: 900 }
  - { name: revokes_url, run: "cd web/packages/ui && bunx vitest run src/lib/download.test.ts -t revokes_url", timeoutSec: 900 }
  - { name: open_path_default_refuses_outside_root, run: "cd desktop/src-tauri && cargo test --lib open_path::tests::open_path_default_refuses_outside_root -- --exact", timeoutSec: 900 }
  - { name: open_path_default_symlink_escape, run: "cd desktop/src-tauri && cargo test --lib open_path::tests::open_path_default_symlink_escape -- --exact", timeoutSec: 900 }
  - { name: open_path_default_directory_and_app_bundle, run: "cd desktop/src-tauri && cargo test --lib open_path::tests::open_path_default_directory_and_app_bundle -- --exact", timeoutSec: 900 }
  - { name: open_path_default_extension_not_allow_listed, run: "cd desktop/src-tauri && cargo test --lib open_path::tests::open_path_default_extension_not_allow_listed -- --exact", timeoutSec: 900 }
  - { name: open_path_default_unc_on_windows, run: "cd desktop/src-tauri && cargo test --lib open_path::tests::open_path_default_unc_on_windows -- --exact", timeoutSec: 900 }
  - { name: open_path_default_remote_daemon, run: "cd desktop/src-tauri && cargo test --lib open_path::tests::open_path_default_remote_daemon -- --exact", timeoutSec: 900 }
  - { name: open_path_default_error_text_has_no_path, run: "cd desktop/src-tauri && cargo test --lib open_path::tests::open_path_default_error_text_has_no_path -- --exact", timeoutSec: 900 }
  - { name: capability_allowlist_exact, run: "cd desktop/src-tauri && cargo test --lib capability_tests::capability_allowlist_exact -- --exact", timeoutSec: 900 }
mutations:
  - { id: M1, file: web/packages/ui/src/components/timeline/deliverables.ts, change: "drop the delete/move exclusion", expectFail: [candidates_from_write_tools_and_final_message] }
  - { id: M2, file: web/packages/ui/src/components/timeline/deliverables.ts, change: "isDeliverable ignores the run start (mtime check removed)", expectFail: [verification_drops_old_missing_mismatch_dir] }
  - { id: M3, file: web/packages/ui/src/components/timeline/deliverables.ts, change: "MAX_CARDS 6 becomes 7", expectFail: [caps_6_cards_20_candidates, caps_cards_and_summarises_the_rest] }
  - { id: M4, file: web/packages/ui/src/components/timeline/deliverables.ts, change: "a .. segment is skipped instead of refusing the path", expectFail: [path_normalisation_table] }
  - { id: M5, file: web/packages/ui/src/components/timeline/deliverables.ts, change: "an absolute path outside the worktree is accepted", expectFail: [path_normalisation_table] }
  - { id: M6, file: web/packages/ui/src/components/timeline/deliverable-card.tsx, change: "the first card opens itself when it appears", expectFail: [no_auto_open] }
  - { id: M7, file: web/packages/ui/src/components/file-actions.tsx, change: "Download is offered on the desktop too", expectFail: [actions_per_platform_and_type, availableFileActions] }
  - { id: M8, file: web/packages/ui/src/components/file-actions.tsx, change: "Open in app is offered without a local daemon", expectFail: [desktop_attached_to_a_remote_daemon_offers_neither_open_in_app_nor_reveal, availableFileActions] }
  - { id: M9, file: web/packages/ui/src/lib/download-file.ts, change: "the download name keeps the whole path", expectFail: [sanitises_filename] }
  - { id: M10, file: web/packages/ui/src/lib/download-file.ts, change: "the object URL is not revoked", expectFail: [revokes_url] }
  - { id: M11, file: web/packages/ui/src/lib/file-bytes.ts, change: "the default read cap is raised above 50 MiB", expectFail: [refuses_over_cap] }
  - { id: M12, file: web/packages/ui/src/components/file-actions.tsx, change: "html is added to the desktop opener list", expectFail: [desktop_openable_list_matches_rust] }
  - { id: R1, file: desktop/src-tauri/src/open_path.rs, change: "the inside-the-root check is removed", expectFail: [open_path_default_refuses_outside_root, open_path_default_symlink_escape] }
  - { id: R2, file: desktop/src-tauri/src/open_path.rs, change: "the regular-file check is removed", expectFail: [open_path_default_directory_and_app_bundle] }
  - { id: R3, file: desktop/src-tauri/src/open_path.rs, change: "every extension is allowed", expectFail: [open_path_default_extension_not_allow_listed] }
  - { id: R4, file: desktop/src-tauri/src/open_path.rs, change: "the Windows UNC refusal is disabled", expectFail: [open_path_default_unc_on_windows] }
  - { id: R5, file: desktop/src-tauri/src/open_path.rs, change: "a remote daemon is not refused", expectFail: [open_path_default_remote_daemon] }
  - { id: R6, file: desktop/src-tauri/src/open_path.rs, change: "an error message carries a path separator", expectFail: [open_path_default_error_text_has_no_path] }
  - { id: R7, file: desktop/src-tauri/capabilities/proxy.json, change: "allow-open-path-default is removed from the capability", expectFail: [capability_allowlist_exact] }
commands:
  - { id: C1, run: "cd web/packages/ui && bunx tsc -b", timeoutSec: 900 }
  - { id: C2, run: "cd web/packages/ui && bun run test", timeoutSec: 1800 }
  - { id: C3, run: "cd desktop/src-tauri && cargo test --lib", timeoutSec: 1800 }
limits: { budgetUsd: 3, maxMinutes: 60 }
---

# Office mode O5 validation contract

Web (`web/packages/ui`) plus one desktop command (`desktop/src-tauri/src/open_path.rs`). The Rust tests need the MSVC toolchain on Windows; the real-symlink row skips where the OS refuses to create a symlink, and `open_path_default_symlink_escape` also has a fake-probe row that always runs.
Frames are not validator frames: the real-app frames (light and dark, 1280 and 390, against an isolated daemon with scripted runs, plus the native desktop app for the desktop action set) are captured by the Lead with `playwright-core`; their folders are named in the PR body.
