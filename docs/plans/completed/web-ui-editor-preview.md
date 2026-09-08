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

- **Library:** `react-markdown@10` + `remark-gfm@4` (the spec's suggested
  pairing) instead of an alternative (marked/markdown-it + innerHTML or a
  rehype pipeline). react-markdown emits React elements — safe by
  construction, no `dangerouslySetInnerHTML` — and GFM tables come free.
- **iframe content via `srcDoc`**, not a blob URL: srcDoc renders straight
  from the editor's current buffer with no network/daemon involvement and
  no object-URL lifetime to manage; a blob adds indirection and nothing
  else. `<meta charset="utf-8">` is prepended when the buffer has no
  charset meta, since srcDoc documents default to windows-1252 and garble
  non-ASCII.
- **`sandbox=""`** — zero permissions, the strictest form. Listing nothing
  can never accidentally include `allow-scripts` or `allow-same-origin`.
- **SVG via `data:image/svg+xml,...` URL on an `<img>`** rather than an
  inline `<svg>` element or `dangerouslySetInnerHTML`: image-mode SVG
  cannot run scripts and isolates the document's styles.
- **UI:** a two-segment Edit/Preview toggle next to Save, rendered only
  for previewable files (absent — not disabled — otherwise, per the AC).
  `aria-pressed` marks the active segment.
- **Previewable detection:** `previewKind(path)` in the new
  `file-preview.tsx` maps extensions (.html/.htm/.md/.markdown/.mdx/.svg,
  case-insensitive) to a `PreviewKind`; extensible by adding a map entry.
- **Editor lifecycle:** CodeMirror stays mounted in preview mode, hidden
  via a `hidden` Tailwind class on its wrapper (never unmounted), so
  cursor/selection/undo survive the round-trip. Mode is component state;
  `previewing = kind !== null && mode === "preview"` so a stale "preview"
  can't blank the editor after switching to a non-previewable file.
- **Scope:** no backend changes needed — everything renders off the
  buffer the hook already holds. No scope changes discovered.

## Progress

- [x] Preview-control UI (Edit/Preview switch, previewable-type detection)
- [x] Markdown preview + tests
- [x] SVG preview + tests
- [x] Sandboxed HTML preview + tests
- [x] Verification (typecheck/tests/build)

## Validation

All criteria confirmed on branch `web-ui-editor-preview`:

- **Preview mode + control:** `.md`/`.svg`/`.html` selections show the
  Edit/Preview toggle; `.go` shows none and the editor renders normally
  (test: "a non-previewable file shows no preview control and a working
  editor").
- **Markdown renders formatted:** `# Title` yields `<h1>Title</h1>`, the
  raw source doesn't leak as text, and GFM tables render
  (remark-gfm wired) — tests "a .md file previews as rendered markdown…"
  and "GFM tables render".
- **Preview = current buffer, no auto-save:** editing then previewing
  shows the edited heading with zero `file.write` calls (test "preview
  shows the current buffer…").
- **SVG as image:** `role="img"` with a `data:image/svg+xml` src, not
  source text (test "a .svg file previews as an image…").
- **HTML sandboxed:** `<iframe sandbox="" srcDoc=…>` — sandbox value ""
  (no allow-scripts/allow-same-origin), content from the buffer, `src`
  never set (test "an .html file previews inside a sandboxed iframe…").
- **No dangerouslySetInnerHTML anywhere:** react-markdown output is React
  elements; the only HTML sink is the sandboxed iframe. No
  `dangerouslySetInnerHTML` occurrences in `web/`.
- **Self-contained:** diff touches `file-explorer-pane.tsx`,
  `file-preview.tsx` (new), and `file-explorer-pane.test.tsx` only —
  `App.tsx`/`task-detail.tsx` untouched.
- **Editor state survives:** same EditorView instance across
  Preview→Edit with buffer and (undo) history intact; editor wrapper
  carries `hidden` while previewing (test "Preview then Edit restores…").
- **Checks:** `bunx tsc -b` clean; `bun run test` 53/53 (46 pre-existing
  + 7 new); `task lint` clean; `task build` succeeds;
  `internal/server/dist/.gitkeep` restored after the Vite
  `--emptyOutDir` wipe; `task test` (Go + web) green.
