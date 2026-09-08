# Web UI: editor preview pane

## Acceptance Criteria

- The file explorer's editor view gains a preview mode: when the selected
  file is previewable (HTML, Markdown, SVG by default; extensible), a
  preview control lets the user switch between Edit and Preview without
  leaving the editor, following the file-explorer task's established
  pattern. With no previewable file selected, the control is absent (or
  clearly disabled) — never a broken render.
- **Markdown** renders as real formatted output (headings, lists, code
  blocks, links — [react-markdown](https://github.com/remarkjs/react-markdown)
  is the suggested library; GFM tables via `remark-gfm` if cheap). The
  preview reflects the editor's *current* buffer, not the last-saved
  file content.
- **SVG** renders as an actual image (inline `<img>` from a data/blob URL,
  or equivalent), not source text.
- **HTML** previews sandboxed: an iframe with `sandbox` (no
  `allow-scripts`, no `allow-same-origin`), content supplied via `srcDoc`
  or blob URL — never a live fetch of a daemon URL. Plain HTML preview
  renders fine without network access to the daemon.
- **Security requirement:** anything rendered as HTML must go through the
  sandboxed-iframe path (see above); react-markdown's output is React
  elements, not `innerHTML`, so Markdown is safe by construction. No
  `dangerouslySetInnerHTML` anywhere outside a sandboxed iframe. No
  execution of unsaved/injected scripts in the UI's origin.
- The pane is self-contained inside the file-explorer feature (it edits
  `file-explorer-pane.tsx` / adds new component files) and must not touch
  `App.tsx` or `task-detail.tsx`.
- Preview does not auto-save: switching to Preview renders the current
  buffer; unsaved changes are visible in the preview. Switching back to
  Edit preserves editor state (cursor/undo — the existing
  `CodeMirrorEditor` already keeps its EditorView across renders, so
  keep the editor mounted and just hide/show, don't unmount).

## Test Scenarios

- Frontend component tests (jsdom + Testing Library, following
  `file-explorer-pane.test.tsx`'s existing FakeWsClient pattern):
  - selecting a `.md` file and switching to Preview shows rendered
    markdown (e.g. an `h1` from `# Title`), not the raw source;
  - editing then previewing shows the *edited* buffer (unsaved changes
    visible in preview);
  - a `.svg` file preview shows an image element, not source text;
  - an `.html` file preview renders inside a sandboxed iframe
    (`sandbox` attr without scripts/same-origin, `srcDoc`/blob src),
    asserting the sandbox attribute value on the iframe;
  - a non-previewable file (e.g. `.go`, `.ts`) shows no preview control
    (or a disabled one) and no broken render;
  - switching Preview → Edit restores the editor with its content
    intact (a change made before previewing is still in the buffer).
- No new backend work expected — preview works purely off the buffer the
  editor already holds (`file.read`/editor state); if implementation
  reveals a real need for a daemon change, stop and record it in
  Decisions rather than expanding scope silently.
- `bunx tsc -b` clean, `bun run test` passes, `task build` succeeds;
  `internal/server/dist/.gitkeep` restored if the build removes it
  (known Vite `--emptyOutDir` behavior).

## Decisions

(To be filled by the implementer: react-markdown vs alternative, how the
 sandboxed iframe gets its content — `srcDoc` vs blob URL — and how the
 preview-control UI looks, plus anything discovered that changed scope.)

## Progress

- [ ] Preview-control UI (Edit/Preview switch, previewable-type detection)
- [ ] Markdown preview + tests
- [ ] SVG preview + tests
- [ ] Sandboxed HTML preview + tests
- [ ] Verification (typecheck/tests/build)

## Validation

(Filled in as each Acceptance Criterion is confirmed.)
