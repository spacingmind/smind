---
v: 1
milestone: "Office mode O4: sandboxed PDF preview in the file tab (pdf.js, file.stat / file.readRange)"
base: origin/develop
planRef: docs/plans/active/office-mode.md
acs:
  - id: AC17
    text: "A .pdf opened from the Files tab or a file link opens the existing file tab (taskId:file:path, no new TabKind) and shows the rendered PDF with page N / M, zoom, fit, keyboard zoom and paging and a reload button; text files still open in the editor without a stat; other binaries show the No preview panel; a non-PDF named .pdf (mismatch) never reaches the viewer."
    covers: { tests: [pdf_opens_in_file_tab_no_new_tabkind, text_files_still_open_in_the_editor_without_a_stat, mismatch_never_reaches_viewer, binary_shows_no_preview_panel], frames: [] }
  - id: AC18
    text: "The viewer is an opaque-origin iframe with sandbox exactly allow-scripts; its document is served with the CSP header (default-src 'none'; script-src 'self'; connect-src 'none'; frame-ancestors 'self'), and carries the same policy as a meta tag."
    covers: { tests: [iframe_sandbox_attribute_is_exactly_allow_scripts, loads_the_separate_viewer_document_and_no_data_or_blob_src, TestPDFViewerCSPHeader], frames: [] }
  - id: AC19
    text: "getDocument options are hardened (isEvalSupported false, enableScripting false, enableXfa false, data only, canvas and image caps), the annotation layer is off, the page and canvas caps are the ADR numbers, and a PDF carrying a JavaScript action, an external link and a Launch action opens, reads and renders without any fetch, socket, window or dialog."
    covers: { tests: [getDocument_options_are_hardened, annotation_layer_is_off_on_every_render, caps_are_the_adr_numbers, canvas_scale_never_exceeds_16_megapixels, is_actually_hostile, opens_and_reads_without_executing_or_fetching_anything, the_viewer_never_loads_pdfjs_scripting_or_a_url_input], frames: [] }
  - id: AC20
    text: "The host accepts exactly four typed viewer messages, only from its own iframe window, and ignores everything else; the viewer is given bytes only."
    covers: { tests: [accepts_exactly_the_four_viewer_shapes, ignores_malformed_and_foreign_messages, drops_extra_fields_it_does_not_trust, only_the_hosts_own_frame_is_listened_to, host_messages_carry_bytes_and_view_commands_only, messages_from_other_windows_or_with_bad_shapes_change_nothing], frames: [] }
  - id: AC21
    text: "The preview is lazy: the pane is a React.lazy chunk and the viewer is a separate page fetched only when a PDF opens; pdf.js is pinned exactly at 6.4.299 (fix for CVE-2024-4367 landed in 4.2.67) with its attribution in NOTICE."
    covers: { tests: [pdfjs_is_pinned_exactly_at_a_fixed_version, entry_code_never_imports_pdfjs_or_the_pane_statically, the_viewer_is_built_separately_not_into_the_app_entry], frames: [] }
  - id: AC22
    text: "Oversize, password-protected, corrupt, truncated, timed-out and changed-while-reading files each show a distinct calm panel, tear the iframe down, and leave the app usable; a conflict offers Reload."
    covers: { tests: [states_oversize_password_corrupt_conflict, a_parse_that_never_finishes_times_out_and_tears_the_viewer_down, assembles_sequential_chunks_pinned_to_ifMtime, refuses_over_cap_before_asking_for_a_byte, maps_conflict_and_too_large_codes_and_short_files], frames: [] }
tests:
  - { name: pdf_opens_in_file_tab_no_new_tabkind, run: "cd web/packages/ui && bunx vitest run src/components/pdf-preview/pdf-preview.test.tsx -t pdf_opens_in_file_tab_no_new_tabkind", timeoutSec: 600 }
  - { name: text_files_still_open_in_the_editor_without_a_stat, run: "cd web/packages/ui && bunx vitest run src/components/pdf-preview/pdf-preview.test.tsx -t text_files_still_open_in_the_editor_without_a_stat", timeoutSec: 600 }
  - { name: mismatch_never_reaches_viewer, run: "cd web/packages/ui && bunx vitest run src/components/pdf-preview/pdf-preview.test.tsx -t mismatch_never_reaches_viewer", timeoutSec: 600 }
  - { name: binary_shows_no_preview_panel, run: "cd web/packages/ui && bunx vitest run src/components/pdf-preview/pdf-preview.test.tsx -t binary_shows_no_preview_panel", timeoutSec: 600 }
  - { name: states_oversize_password_corrupt_conflict, run: "cd web/packages/ui && bunx vitest run src/components/pdf-preview/pdf-preview.test.tsx -t states_oversize_password_corrupt_conflict", timeoutSec: 600 }
  - { name: a_parse_that_never_finishes_times_out_and_tears_the_viewer_down, run: "cd web/packages/ui && bunx vitest run src/components/pdf-preview/pdf-preview.test.tsx -t a_parse_that_never_finishes_times_out_and_tears_the_viewer_down", timeoutSec: 600 }
  - { name: messages_from_other_windows_or_with_bad_shapes_change_nothing, run: "cd web/packages/ui && bunx vitest run src/components/pdf-preview/pdf-preview.test.tsx -t messages_from_other_windows_or_with_bad_shapes_change_nothing", timeoutSec: 600 }
  - { name: iframe_sandbox_attribute_is_exactly_allow_scripts, run: "cd web/packages/ui && bunx vitest run src/components/pdf-preview/pdf-sandbox.test.tsx -t iframe_sandbox_attribute_is_exactly_allow_scripts", timeoutSec: 600 }
  - { name: loads_the_separate_viewer_document_and_no_data_or_blob_src, run: "cd web/packages/ui && bunx vitest run src/components/pdf-preview/pdf-sandbox.test.tsx -t loads_the_separate_viewer_document_and_no_data_or_blob_src", timeoutSec: 600 }
  - { name: getDocument_options_are_hardened, run: "cd web/packages/ui && bunx vitest run src/components/pdf-preview/pdf-viewer-options.test.ts -t getDocument_options_are_hardened", timeoutSec: 600 }
  - { name: annotation_layer_is_off_on_every_render, run: "cd web/packages/ui && bunx vitest run src/components/pdf-preview/pdf-viewer-options.test.ts -t annotation_layer_is_off_on_every_render", timeoutSec: 600 }
  - { name: caps_are_the_adr_numbers, run: "cd web/packages/ui && bunx vitest run src/components/pdf-preview/pdf-viewer-options.test.ts -t caps_are_the_adr_numbers", timeoutSec: 600 }
  - { name: canvas_scale_never_exceeds_16_megapixels, run: "cd web/packages/ui && bunx vitest run src/components/pdf-preview/pdf-viewer-options.test.ts -t canvas_scale_never_exceeds_16_megapixels", timeoutSec: 600 }
  - { name: is_actually_hostile, run: "cd web/packages/ui && bunx vitest run src/components/pdf-preview/pdf-hostile.test.ts -t is_actually_hostile", timeoutSec: 600 }
  - { name: opens_and_reads_without_executing_or_fetching_anything, run: "cd web/packages/ui && bunx vitest run src/components/pdf-preview/pdf-hostile.test.ts -t opens_and_reads_without_executing_or_fetching_anything", timeoutSec: 600 }
  - { name: the_viewer_never_loads_pdfjs_scripting_or_a_url_input, run: "cd web/packages/ui && bunx vitest run src/components/pdf-preview/pdf-hostile.test.ts -t the_viewer_never_loads_pdfjs_scripting_or_a_url_input", timeoutSec: 600 }
  - { name: accepts_exactly_the_four_viewer_shapes, run: "cd web/packages/ui && bunx vitest run src/components/pdf-preview/pdf-host-protocol.test.ts -t accepts_exactly_the_four_viewer_shapes", timeoutSec: 600 }
  - { name: ignores_malformed_and_foreign_messages, run: "cd web/packages/ui && bunx vitest run src/components/pdf-preview/pdf-host-protocol.test.ts -t ignores_malformed_and_foreign_messages", timeoutSec: 600 }
  - { name: drops_extra_fields_it_does_not_trust, run: "cd web/packages/ui && bunx vitest run src/components/pdf-preview/pdf-host-protocol.test.ts -t drops_extra_fields_it_does_not_trust", timeoutSec: 600 }
  - { name: only_the_hosts_own_frame_is_listened_to, run: "cd web/packages/ui && bunx vitest run src/components/pdf-preview/pdf-host-protocol.test.ts -t only_the_hosts_own_frame_is_listened_to", timeoutSec: 600 }
  - { name: host_messages_carry_bytes_and_view_commands_only, run: "cd web/packages/ui && bunx vitest run src/components/pdf-preview/pdf-host-protocol.test.ts -t host_messages_carry_bytes_and_view_commands_only", timeoutSec: 600 }
  - { name: assembles_sequential_chunks_pinned_to_ifMtime, run: "cd web/packages/ui && bunx vitest run src/lib/file-bytes.test.ts -t assembles_sequential_chunks_pinned_to_ifMtime", timeoutSec: 600 }
  - { name: refuses_over_cap_before_asking_for_a_byte, run: "cd web/packages/ui && bunx vitest run src/lib/file-bytes.test.ts -t refuses_over_cap_before_asking_for_a_byte", timeoutSec: 600 }
  - { name: maps_conflict_and_too_large_codes_and_short_files, run: "cd web/packages/ui && bunx vitest run src/lib/file-bytes.test.ts -t maps_conflict_and_too_large_codes_and_short_files", timeoutSec: 600 }
  - { name: pdfjs_is_pinned_exactly_at_a_fixed_version, run: "cd web/packages/ui && bunx vitest run src/components/pdf-preview/pdf-lazy-and-pin.test.ts -t pdfjs_is_pinned_exactly_at_a_fixed_version", timeoutSec: 600 }
  - { name: entry_code_never_imports_pdfjs_or_the_pane_statically, run: "cd web/packages/ui && bunx vitest run src/components/pdf-preview/pdf-lazy-and-pin.test.ts -t entry_code_never_imports_pdfjs_or_the_pane_statically", timeoutSec: 600 }
  - { name: the_viewer_is_built_separately_not_into_the_app_entry, run: "cd web/packages/ui && bunx vitest run src/components/pdf-preview/pdf-lazy-and-pin.test.ts -t the_viewer_is_built_separately_not_into_the_app_entry", timeoutSec: 600 }
  - { name: TestPDFViewerCSPHeader, run: "go test ./internal/server -run '^TestPDFViewerCSPHeader$' -count=1", timeoutSec: 300 }
mutations:
  - { id: M1, file: web/packages/ui/src/components/pdf-preview/pdf-viewer-frame.tsx, change: "PDF_VIEWER_SANDBOX = \"allow-scripts allow-same-origin\"", expectFail: [iframe_sandbox_attribute_is_exactly_allow_scripts] }
  - { id: M2, file: web/packages/ui/src/components/pdf-preview/viewer-options.ts, change: "enableScripting: true", expectFail: [getDocument_options_are_hardened] }
  - { id: M3, file: web/packages/ui/src/components/pdf-preview/viewer-options.ts, change: "RENDER_OPTIONS annotationMode 1 (ENABLE)", expectFail: [annotation_layer_is_off_on_every_render] }
  - { id: M4, file: web/packages/ui/src/components/pdf-preview/host-protocol.ts, change: "acceptViewerEvent ignores event.source", expectFail: [only_the_hosts_own_frame_is_listened_to, messages_from_other_windows_or_with_bad_shapes_change_nothing] }
  - { id: M5, file: web/packages/ui/src/components/file-editor-pane.tsx, change: "route a mismatched PDF to the viewer (drop the stat.mismatch branch)", expectFail: [mismatch_never_reaches_viewer] }
  - { id: M6, file: web/packages/ui/src/components/pdf-preview/pdf-preview-pane.tsx, change: "drop the stat.size > maxBytes oversize check", expectFail: [states_oversize_password_corrupt_conflict] }
  - { id: M7, file: web/packages/ui/src/lib/file-bytes.ts, change: "do not send ifMtime with each chunk", expectFail: [assembles_sequential_chunks_pinned_to_ifMtime] }
  - { id: M8, file: web/packages/ui/src/components/pdf-preview/pdf-preview-pane.tsx, change: "never start the parse timeout", expectFail: [a_parse_that_never_finishes_times_out_and_tears_the_viewer_down] }
  - { id: M9, file: internal/server/web.go, change: "connect-src 'none' becomes connect-src *", expectFail: [TestPDFViewerCSPHeader] }
  - { id: M10, file: web/packages/ui/src/components/pdf-preview/host-protocol.ts, change: "parseViewerMessage accepts any error reason string", expectFail: [ignores_malformed_and_foreign_messages] }
  - { id: M11, file: web/packages/ui/package.json, change: "pdfjs-dist \"6.4.299\" becomes \"^6.4.299\"", expectFail: [pdfjs_is_pinned_exactly_at_a_fixed_version] }
  - { id: M12, file: web/packages/ui/src/components/file-editor-pane.tsx, change: "add a static import of pdfjs-dist", expectFail: [entry_code_never_imports_pdfjs_or_the_pane_statically] }
commands:
  - { id: C1, run: "cd web/packages/ui && bunx tsc -b", timeoutSec: 900 }
  - { id: C2, run: "cd web/packages/ui && bun run test", timeoutSec: 1800 }
  - { id: C3, run: "cd web/packages/ui && bun run build", timeoutSec: 900 }
  - { id: C4, run: "go test ./internal/server -count=1", timeoutSec: 900 }
limits: { budgetUsd: 3, maxMinutes: 45 }
---

# Office mode O4 validation contract

Web (`web/packages/ui`) plus one daemon header (`internal/server/web.go`). Frames are not validator frames: the real-app frames (light and dark, 1280 and 390, against an isolated daemon, with the PDF-with-JavaScript network log) are captured by the Lead with `playwright-core` and Edge, and their folder is named in the PR body. The viewer chunk fetch (AC21) is checked by the build output and the Playwright request log, not by a unit test.
