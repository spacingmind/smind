# 0049: Office mode — a client-side interface mode, binary-safe file reads, a PDF preview tab and deliverable cards

## Status

**Accepted — 2026-10-31.** Human answers 1–6 (section "Human answers 2026-10-31") are the Human's. Everything else is a **Supervisor
default 2026-10-31 under the Human's directive, Human may veto** (section "Supervisor defaults"); a veto changes the PR named next to it
in `docs/plans/active/office-mode.md`. Spec, acceptance criteria and PR split: `docs/plans/active/office-mode.md`.

Scope of this ADR: epic **A** (interface mode) and epic **E** (documents as deliverables: binary read, PDF preview, deliverable cards,
open in OS app). Epics **D**, **C**, docx/xlsx/pptx preview and mobile are *not* decided here; they are written down as later ADRs in
[section 8](#8-later-adrs-written-down-not-decided-here).

Inputs: ADR-0004 (per-task editor tabs, tab keys), ADR-0019 (an agent never answers a permission), ADR-0032 (shared preference stores),
ADR-0035 (relay E2EE, 1 MiB frame cap), ADR-0038 (attachments: sniffing, per-transport limits in `daemon.info`), ADR-0042 (plugins: the
`file.` prefix is on the plugin never-list; `files.read` and a sandboxed JS panel are parked), ADR-0045 (validation contracts),
ADR-0047 (policy floor), ADR-0048 (fleet: remote hosts), `docs/WORKSPACE_PROTOCOL.md`, and a **local research note** on ZCode's office
mode (gitignored, local-only, not copied here). That product is a *reference only*: this ADR reimplements the ideas in smind's own
code and copy; no ZCode code, skill text, strings, names or logos are used. `docs/plans/active/zcode-parity.md` says Office mode and
doc/PDF previews are "not cloned" by the *visual parity* epic; that was a rule for that epic's scope, and this Human decision opens a
separate epic.

## Context

smind is a developer tool, and developers also ask the agent for reports, specs, summaries and decks. Today that is awkward in three
places:

1. The UI is built around code: new tasks seed a Terminal and a Diff tab, the composer has a branch picker, tool cards print shell
   commands, output and diffs. A person who only wants "write me a one-page brief" sees noise.
2. `file.read` rejects any non-UTF-8 file (`internal/files/files.go`, `Read`: "not valid UTF-8 (binary files are not supported)"), so the
   Files tab and the editor cannot show a PDF the agent just produced, and there is no way to hand the bytes to the web UI at all.
3. Nothing in the timeline says "the agent produced `report.pdf`". The human must find it in the Files tab.

ZCode ships two separate things under "office mode": a presentation-only *interface preference*, and a *document capability*. The
research note's conclusion is to split them the same way, and to do the cheap, execution-free parts first. This ADR is those parts.

### What this ADR does not change

No daemon default, prompt, tool, permission mode, policy rule, model, workspace type or store schema changes. Workspaces stay git-only
(`internal/workspace/workspace.go` `ValidateWorkspacePath` still requires `.git`; epic D is a later ADR). Documents are produced by the
agent exactly as today (Write/Edit/Bash under the provider's permission mode); this ADR only *shows* them.

## Human answers 2026-10-31

All **ACCEPTED**. Wording is the Human's; the ADR text below implements it.

- **H1 Audience.** For now a simplified view for **developers working on documents**, not a separate non-developer product.
- **H2 Mode scope.** `interfaceMode: coding | office` is a **client-side preference only**. It changes no daemon defaults, prompts,
  tools, permissions or model.
- **H3 Platform.** Documents are viewed on **desktop and web first**. Mobile and the relay come later (1 MiB frame cap).
- **H4 Dependencies.** Agents **never install** LibreOffice, Poppler, Python or Node themselves. smind **detects** a missing dependency,
  **says so**, and gives the human the **install command**.
- **H5 Preview.** **PDF first.** docx, xlsx and pptx preview come **only after** an untrusted-document threat model in this ADR
  ([section 7](#7-threat-model-for-untrusted-documents-precondition-for-docxxlsxpptx-preview)), as a precondition for the later ADR.
- **H6 Fonts.** An OFL fallback font for Vietnamese and CJK **may be bundled**, needed only at the **document-generation stage** (so not
  in this ADR's PRs).

## Decision

### 1. Interface mode (epic A)

**1.1 Definition.** A new client preference `interfaceMode: "coding" | "office"`, default `"coding"`. It is read by the web UI only
(web, and the desktop app, which is the same UI). It is never sent to the daemon, never stored in the store, never part of any
request payload, and not a field of `daemon.info`, settings, a profile, or a plugin API. **Invariant (tested):** the JSON of every
request the UI sends (`run.start`, `task.create`, `chat.*`, `settings.*`) is identical in both modes for the same user actions, and
no wsapi method reads the mode.

**1.2 Persistence.** One shared preference store (ADR-0032) `useInterfaceModeStore = createPreferenceStore(readInterfaceMode,
writeInterfaceMode)` in `stores/interface-mode-store.ts`; the pure helpers go in `lib/interface-mode.ts`; the storage key
`STORAGE_KEYS.interfaceMode = "smind:interface-mode"` in `lib/storage.ts`. The stored value is the bare string `"office"`; anything
else (missing, `"coding"`, corrupt, an old build's junk) reads as `"coding"`. Per-device: **no sync** across devices or to the daemon
(S2). Other open tabs follow through the store's `storage` event; the desktop app's windows share the same webview storage.

**1.3 Entry points (four, one store).**

| Entry | Where | Behaviour |
|---|---|---|
| Settings row | Settings → General (`general-section.tsx`), "Interface" select: Coding / Office, one-line description | writes the store |
| Footer menu | sidebar footer / avatar menu (`sidebar-footer.tsx`): a radio group "Coding" / "Office" | writes the store |
| Shortcut | `Mod+Shift+U`, action `interface.toggle`, section "General", label "Switch interface mode" (S1) | toggles; announces the new mode (1.7) |
| Palette | command "Switch to Office mode" / "Switch to Coding mode" (shows the opposite), same action | toggles |

First-run stays the connect wizard; no onboarding step (S11).

**1.4 What "hidden" means (S5).** *Hidden = not rendered and not reachable* (removed from the DOM, the tab order, the palette and the
shortcut help), never "disabled and greyed". Only **creation and navigation entries** are hidden; **existing state is untouched**.

| Surface | Office mode |
|---|---|
| "New Terminal" and "Review" (diff) entries in the tab "+" menu, empty-state base section, sidebar quick actions | hidden |
| New tasks' tab seeding | seeds `chat` and `files` only (no `diff`, no `terminal`); existing tasks keep their seeded tabs |
| PR tab entry and "Open PR" actions | hidden (the PR tab, if already open, stays) |
| Composer branch / base-ref picker | hidden. The value it would hold (its current or default value) is still sent, so the payload is unchanged |
| Diff and branch commands in the palette ("Open diff", "Switch branch", "New terminal", "Open PR tab") | not listed |
| Shortcut actions for the hidden entries (`tab-new` terminal kind, diff and terminal openers) | no-op, and their rows are filtered out of the shortcut help dialog |
| Already-open Terminal / Diff / PR tabs, their PTYs, running agents, the bottom terminal if open | **kept and working**; switching mode never closes, restarts or re-seeds anything |
| Tool cards for read / search / fetch / MCP / plugin / thinking | unchanged (not simplified) |
| Permission requests, run errors, failover and budget cards | **never simplified** (ADR-0019: the human must see what they approve) |
| Files tab, file tabs, editor, Settings, usage, plugins, hosts | unchanged |

**1.5 Concise tool cards.** In office mode a tool card whose intent is `terminal` or `edit` (the existing `ToolIntent`s in
`timeline/tool-renderers.tsx`) renders **summary + status only**: one line (the renderer's existing title, e.g. "Ran a command",
"Edited `report.md`") and a status (running, done, failed). **Not rendered:** the command text, stdout/stderr, the diff, +/- counts and
the expand control. A **failed** card adds the one-line `error-summary` of the failure (existing `lib/error-summary.ts`) so a failure
is never silent; the full output stays behind coding mode. A card for a file keeps its file link (opens the file tab, or the PDF
preview). The existing `DetailLevel` (`detailed|overview`, collapsing runs) stays independent and still applies.

**1.6 Greeting and suggested prompts.** `lib/greeting.ts` gets an `OFFICE_GREETINGS` pool (same four time buckets) of **own copy**,
about *documents and tasks* ("What are we writing today", ...), picked the same way as today's. The home landing shows **up to four
suggestion chips in office mode** from an own-copy pool in `lib/suggested-prompts.ts` (document-shaped: "Summarize this folder's
documents", "Draft a one-page brief about …", "Turn these notes into a short report"); clicking one **fills the composer, never sends**.
If the landing already has a coding prompt pool when O2 starts, the office pool sits beside it and coding mode is unchanged; if it has
none, coding mode stays chip-less. Copy rule (S4): written by us, plain English (smind has no i18n yet, ADR-0042), no text from any
other product.

**1.7 Accessibility (S7).** The mode control is a native `<select>` in Settings and a `role="radiogroup"` of `role="radio"` items in the
menu; the toggle (shortcut or palette) updates a polite live region ("Office mode on" / "Coding mode on"). Hidden entries are removed
from the tab order. If the focused element disappears when the mode flips, focus moves to the composer (the mode control itself keeps
focus when it caused the flip). No state is conveyed by colour alone. The mode name is in the footer menu's accessible name.

**1.8 Telemetry (S8).** None: smind has none, and this ADR adds none.

### 2. Binary-safe file read (epic E, daemon)

`file.read` stays as is (UTF-8 text for the editor, with `expectedMtime` writes). Two **new, read-only, additive** wsapi methods serve
bytes. Both resolve the path with the same `resolveInRoot` (symlink-aware containment in the task worktree) as `file.read` and are
task-scoped like it; because the `file.` prefix is on ADR-0042's plugin never-list, plugins cannot call them without a later
decision.

**2.1 Transport choice.** Options weighed:

| Option | Verdict |
|---|---|
| One `file.read` variant returning the whole file as base64 | rejected: +33 % size, one frame; breaks on the relay's 1 MiB frame (a 700 KiB PDF is the ceiling) and holds 3× the file in memory at once |
| **Range RPC** `file.readRange` with base64 chunks, client assembles | **chosen**: works on every transport (direct, relay, fleet) with no new auth surface; resumable; a chunk size the daemon advertises per transport |
| HTTP `GET /api/file` with `Range` | rejected for v1: a browser `<iframe>`/`<img>` cannot send the bearer header, so it needs a token in the URL (the leak ADR-0038 avoided) and it cannot cross the relay |

**2.2 `file.stat`** `{taskId, path}` → `{size, mtime, kind, mime, ext, mismatch, previewable}`.
- `size` bytes, `mtime` as `file.read` returns it.
- `kind`: `"pdf" | "text" | "image" | "office" | "other"`. `mime` is **sniffed from the content, not the extension**: the first 1024
  bytes through `net/http.DetectContentType` plus explicit magic checks (`%PDF-` within the first 1024 bytes; `PK\x03\x04` zip with an
  OOXML marker is `office`, only *classified*, never opened). Valid UTF-8 without NUL bytes in the first 8 KiB is `text`.
- `mismatch` is true when the extension's expected type disagrees with the sniffed one (a `.pdf` that is not a PDF). A mismatched file
  gets `previewable: false` and never reaches a viewer.
- `previewable` is true only for sniffed `pdf` (this ADR) and for the existing text/image paths already handled by the editor.
- Directories, devices, FIFOs, sockets and other non-regular files: `invalid_params` ("not a regular file"). Missing: the existing
  `MissingError` mapping.

**2.3 `file.readRange`** `{taskId, path, offset, length, ifMtime?}` → `{offset, length, eof, data, mtime}`.
- `data` is base64 of the bytes `[offset, offset+n)`; `n ≤ min(length, maxChunkBytes)`; a `length` over the cap is clamped, not an
  error. `eof` is true when `offset+n == size` at read time.
- The daemon `Open`s the file once, `Stat`s **the open descriptor** (not the path again), checks it is regular, checks `size ≤
  maxReadBytes` (else `too_large`), then `ReadAt` — no time-of-check/time-of-use window between the checks and the read.
- `ifMtime`: when present and different from the descriptor's mtime, the call fails `conflict` (the file changed between chunks);
  the client restarts from offset 0 or shows "file changed, reload". A client assembling a file passes the `mtime` of chunk 0.
- Offsets past EOF return `{length: 0, eof: true}`; negative values are `invalid_params`.
- No handler logs params or bytes; errors name the field, never the content or the absolute path.

**2.4 Limits and discovery (S12).** `daemon.info` gains `files: {maxReadBytes, maxChunkBytes, previewKinds: ["pdf"]}` per connection,
the same way ADR-0038 §8 advertises `attachments`, so a client knows its transport's ceiling before it asks:

| Limit | Value | Why |
|---|---|---|
| `maxReadBytes` (whole file, server) | 50 MiB | a document, not a video; bounds client memory (a Blob, plus the viewer's copy) |
| `maxChunkBytes` direct (WebSocket, 16 MiB read limit) | 4 MiB raw | few round trips on LAN/loopback |
| `maxChunkBytes` relay (E2EE frame 1 MiB) | 512 KiB raw (≈ 700 KiB base64 + envelope) | stays under `maxFrameLen = 1<<20` (`internal/relay/e2ee/handshake.go`); this makes the relay *safe later* even though H3 defers mobile |
| client chunk concurrency | 1 (sequential) | keeps terminal input on the same socket from waiting behind a bulk read |

An older daemon answers `method_not_found` for both methods; the client then shows "Update the daemon to preview files" (the existing
daemon-version guard pattern), never an error toast storm.

### 3. PDF preview (epic E, web)

**3.1 Where it lives.** No new `TabKind`. A PDF opens in the existing **`file` tab** (`${taskId}:file:${path}`, ADR-0004), so keys,
restore, "open file" flows and the file mentions all keep working. `file-editor-pane.tsx` first calls `file.stat`: sniffed `pdf` →
`PdfPreviewPane`; text → the CodeMirror editor as today; any other binary → a calm "No preview for this file type" panel with the
**Open in app** / **Download** actions (section 5) instead of today's raw error. The editor never opens a binary file.

**3.2 Renderer choice (S13).**

| Option | Licence | Cost | Security | Verdict |
|---|---|---|---|---|
| **pdf.js** (`pdfjs-dist`) | Apache-2.0 (compatible with AGPL-3.0, ADR-0003; attribution kept) | main + worker ≈ 1 MB each before gzip, **lazy chunk**, zero cost until a PDF opens; measured in O4 | parse in a Worker; scripting, XFA, eval and the annotation layer are switchable off; one engine on web, WKWebView and WebView2 | **chosen** |
| Browser's built-in viewer in an `<iframe>`/`<object>` (blob URL) | n/a | 0 bytes | each engine ships a different viewer, some run Acrobat-style PDF JavaScript, WKWebView/Windows behaviour differs; a *sandboxed* iframe disables the viewer in Chromium | rejected |
| Native per-OS viewer through Tauri (PDFKit, Edge PDF) | n/a | bespoke per OS | none for web at all | rejected |

**3.3 Hardening (precondition for merge of O4).**
- pdf.js is pinned to an exact version at least as new as the fix for CVE-2024-4367 (arbitrary JS via a crafted font, fixed in 4.2.67);
  O4 chooses the pin and records it. `getDocument` options: `isEvalSupported: false`, `enableScripting: false` (documents' JS never
  runs), `enableXfa: false`, `useSystemFonts: false`, `disableAutoFetch: true`, `disableStream: true`, `maxImageSize` and
  `canvasMaxAreaInBytes` set, `data:` only (no `url:`, so the viewer can never fetch anything); the annotation layer is **off**
  (`annotationMode: DISABLE`), so links, forms and file attachments inside a PDF do nothing; a text layer is on for read-only copy.
- **Opaque-origin sandbox.** The viewer runs in an `<iframe sandbox="allow-scripts">` (no `allow-same-origin`, `allow-popups`,
  `allow-forms`, `allow-modals`, `allow-top-navigation`, `allow-downloads`) so a pdf.js exploit lands in an opaque origin and cannot
  read the app's `localStorage` (which holds pairing and session material) or call the daemon. The iframe document is a **separate
  built HTML entry** (`pdf-viewer.html`) served by the daemon with its own `Content-Security-Policy` response header:
  `default-src 'none'; script-src 'self'; worker-src blob: 'self'; img-src blob: data:; style-src 'self' 'unsafe-inline'; font-src
  'self' data:; connect-src 'none'; frame-ancestors 'self'`. cMaps and standard fonts are bundled same-origin assets, fetched lazily
  by the viewer; there is **no CDN and no network**. O4 verifies how `internal/server` serves `dist` and confirms the header.
- **Message protocol.** Bytes go host → viewer by `postMessage` (a transferred `ArrayBuffer`); the viewer answers only with typed
  `{type: "ready" | "pages" | "page" | "error", ...}` messages the host validates field by field and ignores otherwise; the host
  checks `event.source` is its own iframe. The viewer never gets a path, a task id or a token.
- **Resource caps.** Preview refuses files above `maxReadBytes` (50 MiB); at most 2000 pages are laid out, only the visible pages ±1
  are rendered; each canvas is capped at 16 megapixels; a 30 s parse timeout shows "This PDF could not be opened" and tears the
  iframe down. Password-protected PDFs show "This PDF is password protected" (no prompt in v1).

**3.4 UI.** Vertical scroll of pages, fit-width default, zoom in/out/fit (`Mod+=`, `Mod+-`, `Mod+0` while the pane has focus), page
`N / M`, keyboard `PageUp/PageDown/Home/End`, a reload button (re-stat; refetch if `mtime` changed), a loading skeleton, an error
panel. Light and dark: the page canvas is white in both themes (it is a document); the chrome follows the theme. Find-in-PDF, text
selection "Add to chat", thumbnails, printing and annotations are **not** in v1.

### 4. Deliverable cards (epic E, web)

A **deliverable card** in the timeline says "the agent produced this file" and offers actions. **Detection is client-side,
deterministic, and needs no wire change:**

1. **Candidates** from the run's timeline, in order, deduplicated by normalised path: (a) the `path` argument of every **successful**
   write-intent tool call (Write, Edit, MultiEdit, NotebookEdit; the existing `edit` intent), and (b) workspace-relative paths named
   in the **final assistant message** of the run: markdown links `[x](rel/path.ext)`, and inline-code spans that match
   `^[\w./@+-][\w ./@+-]*\.[A-Za-z0-9]{1,5}$` without `..`, a scheme, or a leading `/` or `~`. An absolute path under the task's
   worktree is accepted and converted to relative; any other absolute path is dropped. A shell command's text is **never parsed**
   (a Bash-made file is found only if the agent names it in the message or a write tool touched it).
2. **Verification** through `file.stat`: the file exists, is a regular file inside the worktree, has an allow-listed extension
   (S14: `pdf`, `docx`, `xlsx`, `pptx`, `md`, `txt`, `csv`, `html`, `png`, `jpg`, `jpeg`, `gif`, `webp`), is not `mismatch`, and its
   **`mtime` is at or after the run's `startedAt`** (both from the daemon's clock, so a card never claims an older file was produced
   by this run, and a Q&A about an old file makes no card). Candidates failing any check are dropped silently.
3. **Placement and limits (S14).** Under the run's final assistant text, once the run is terminal (and, for a still-running run,
   as files verify). At most **6** cards per run (the rest are summarised "+ N more files", linking to the Files tab); candidates
   checked ≤ 20 per run; one `file.stat` per candidate, cached for the run.

A card shows a file-type icon (`lib/file-icons.tsx`), the file name (the relative path as tooltip), a type label and size ("PDF · 240
KB"). **Actions (S14):** **Open** (PDF → preview tab; text/markdown/html/csv/image → the file tab as today; docx/xlsx/pptx → no
in-app preview until the later ADR, so Open is replaced by Open in app), **Open in app** (desktop, section 5), **Download** (web),
**Reveal in folder** (desktop, local daemon only), **Copy path**. A card **never auto-opens** a tab (S14). Cards appear in **both
modes**: they are epic E, independent of the mode (H2); office mode just makes them the main surface.

### 5. Open in OS app and download (epic E, desktop and web)

**5.1 Desktop.** A new Tauri command `open_path_default(root, rel_path)` in `desktop/src-tauri/src/` (next to `editors.rs`, reusing its
"local daemon only" check from the daemon-manager state: when the desktop is attached to a remote daemon the path does not exist
locally, so the action is **not offered**). The command, in order:
1. Joins `root` (the task's worktree path the UI already holds) and `rel_path`, **canonicalises both** (symlinks resolved), and
   requires the canonical target to be **inside** the canonical root (a symlink pointing out is refused).
2. Requires a **regular file** (not a directory, so a `.app` bundle or folder is refused; not a symlink after resolution).
3. Requires the lower-cased extension to be on an **allow-list** (S15): `pdf, docx, doc, xlsx, xls, pptx, ppt, odt, ods, odp, rtf,
   csv, txt, md, png, jpg, jpeg, gif, webp`. An allow-list, not a deny-list: executables and launchers (`app, command, exe, bat, cmd,
   ps1, sh, msi, dmg, jar, lnk, url, desktop, scr`), scripts and `html`/`svg` (a browser with file access) are therefore refused by
   default.
4. On Windows, refuses a canonical path that is a UNC network path (`\\server\share`, but not the local `\\?\C:\` form) so opening
   a file can never start an SMB authentication to a remote host.
5. Opens it with the OS default application through the opener plugin's path API (`tauri-plugin-opener`, already a dependency): **no
   shell, no argv string building, no `cmd /c start`**. Failure returns a short generic message ("No application is set to open this
   file type"); the absolute path is never put in an error shown to the agent or the log.
"Reveal in folder" reuses the file-manager reveal already in `editors.rs`.

**5.2 Web.** **Download** assembles the file with `file.readRange` (chunk 0's `mtime` as `ifMtime`), builds a `Blob`, and saves it with an
`<a download>` whose name is the **basename only**, with control characters and path separators stripped and a 120-character cap;
the object URL is revoked immediately afterwards; progress is shown for files over 5 MiB. The 50 MiB cap applies. On desktop,
Download is not shown (Open in app and Reveal replace it).

### 6. Missing dependencies (H4): detect, say, give the command

No PR in this ADR needs an external dependency: PDF preview is bundled pdf.js and Open in app uses what the OS has. The policy is
fixed *now* so every later consumer (epic C, docx/xlsx/pptx preview) follows it:

- **Agents never install.** Skill text written later must say "if a tool is missing, stop and tell the user", never offer an install
  step. smind's later floor (ADR-0047 `no-install-state` is the precedent) gets a `no-package-install` deny row for package-manager
  install commands in the epic-C ADR; until then ADR-0019's provider modes already make an install an ordinary permission request.
- **smind detects.** A `documents.deps` probe in the daemon's existing readiness surface (`readiness-panel.tsx`, `workspace-readiness`
  plan): `exec.LookPath` plus a `--version` run with a 3 s timeout and a scrubbed environment, no network, results cached 5 minutes.
- **smind says so and gives the command**, per OS, in the readiness panel and in an empty-state line where the feature is blocked.

The detection list and install strings are in S16 (human-run commands; smind never runs them).

### 7. Threat model for untrusted documents (precondition for docx/xlsx/pptx preview)

PDF preview ships under section 3. **docx, xlsx and pptx preview may not ship until a later ADR cites this section and answers each
row.** A document is hostile input: the agent may have fetched it, a user may have attached it, a repo may contain it. Assets and
actors: the app's origin (`localStorage` with pairing material, the live daemon connection), the user's files, the host machine, the
user's network. Attacker: whoever authored the document, including a prompt-injected agent.

| Threat | Required control before preview of an OOXML format |
|---|---|
| Parser bugs (memory corruption, prototype pollution, DoS) in a JS renderer | render **only** in an opaque-origin sandboxed iframe with the section-3 CSP, parse in a Worker, pinned exact versions, `connect-src 'none'`; no renderer in the main frame |
| Macros / VBA (`vbaProject.bin`), embedded OLE objects, DDE fields, ActiveX | **never executed and never loaded**: macro parts are ignored; a document carrying them shows "contains macros (not run)"; no OLE/embedded object rendering; `.docm/.xlsm/.pptm` are classified `office` but not previewable |
| External references: linked images, remote templates, `externalLink` workbooks, hyperlinks, `file://` and UNC links | **all external relationships are dropped at load**; `connect-src 'none'` and `img-src blob: data:` make a leak impossible even if one is missed; hyperlinks are text, not anchors (a "copy link" action at most); no popups |
| Zip bombs and XML bombs (entity expansion, billion laughs, deep nesting) | the container is opened with **hard caps** before any parse: compressed size ≤ 25 MiB, **uncompressed ≤ 200 MiB total, ≤ 50 MiB per part, ratio ≤ 100:1, ≤ 5000 entries**, XML depth ≤ 64, DTDs/entities refused, a 15 s wall-time and a memory ceiling in the worker, then abort with "too large to preview" |
| Path tricks in the zip (`../`, absolute names, symlinks, duplicate names) | entries are read in memory by exact name only; **nothing is extracted to disk**; unsafe names make the file `not previewable` |
| Script execution (JS in SVG, HTML parts, formulas calling out, `HYPERLINK()`/`WEBSERVICE()` in spreadsheets) | no script engine runs; formulas are **cached values only, never recalculated**; embedded SVG/HTML is rasterised or dropped; sanitisation by allow-list (text runs, tables, images from the package as `blob:`) |
| Information leak through rendering (fonts from URLs, external CSS) | no network at all (CSP), fonts bundled or system; images only from inside the package |
| **Conversion path** (docx → PDF for fidelity) | allowed only through **LibreOffice headless** run as a **sandboxed child process**: no network (OS-level where available: macOS `sandbox-exec` profile, Linux `bwrap --unshare-net`, Windows a job object without network rights as best effort, and the policy "refuse to convert" where none is available), a **fresh throwaway profile directory** per run (`-env:UserInstallation`), macros disabled, `--convert-to pdf` with a 60 s timeout and an output size cap, working directory a scratch dir (the task scratch), input copied in read-only, output re-validated by the section-3 PDF path; **never** run through a shell and never with the user's own Office installation |
| Resource exhaustion in the UI | the same size, page, time and memory caps as section 3; one preview iframe at a time |
| Cross-document confusion (a path that changes under the viewer) | `ifMtime` on every chunk (section 2.3); the viewer receives bytes, never a path |

Anything not answered by a row makes the format *not previewable*; "Open in app" remains the fallback (and the OS app is the user's
own, outside smind's trust model, so the document can attack it only as it could anywhere else).

## Supervisor defaults 2026-10-31 (the Human did not answer these; Human may veto)

Each row names the PR (see the plan) that changes if vetoed.

| # | Default | Veto changes |
|---|---|---|
| **S1** | Shortcut **`Mod+Shift+U`**, action `interface.toggle`, section "General" (free in `keyboard/shortcuts.ts`; user-rebindable through the existing overrides) | O1 |
| **S2** | Persistence key **`smind:interface-mode`** in `localStorage` via `createPreferenceStore`; value `"office"` or absent; **per-device, no cross-device sync, nothing on the daemon**; default `coding` | O1 |
| **S3** | Mode applies to the whole app window, not per task or per workspace | O1 |
| **S4** | Greeting and suggestion copy is **our own**, plain English, documents-flavoured; no strings from any other product; the office pool has ≥ 3 greetings per time bucket and exactly 4 suggestion chips; chips fill the composer and never send | O2 |
| **S5** | "Hidden" means **removed** (not disabled), for creation/navigation entries only; existing tabs, PTYs and runs are untouched | O1 |
| **S6** | Palette in office mode: terminal/diff/branch/PR-opening commands are **not listed**; their shortcuts are no-ops and filtered from the help dialog; every other command stays; the "Switch to … mode" command is always listed | O1 |
| **S7** | Accessibility as 1.7: native select, radiogroup, polite live region, hidden entries out of the tab order, focus moves to the composer if its element vanished | O1 |
| **S8** | **No telemetry** | n/a |
| **S9** | Failed concise cards show status plus the one-line error summary; permission and error cards are never simplified | O2 |
| **S10** | Office mode does not change tab *seeding* for tasks that already exist, only for tasks created while it is on; switching back does not add the tabs back to office-created tasks (the "+" menu can) | O1 |
| **S11** | No onboarding step or first-run question; the mode is discoverable from Settings, the footer menu, the palette and the shortcut | O1 |
| **S12** | File limits: `maxReadBytes` 50 MiB, `maxChunkBytes` 4 MiB direct / 512 KiB relay, sequential chunks, sniff window 1024 bytes (text window 8 KiB), `daemon.info.files` advertises them | O3 |
| **S13** | Renderer **pdf.js**, lazy chunk, opaque-origin sandboxed iframe with its own CSP header, annotation layer off, scripting off, no network, no CDN; no find/print/annotate in v1 | O4 |
| **S14** | Deliverable cards: extension allow-list `pdf docx xlsx pptx md txt csv html png jpg jpeg gif webp`; ≤ 6 cards per run, ≤ 20 candidates checked; both modes; actions Open / Open in app / Download (web) / Reveal in folder (desktop) / Copy path; **no auto-open**; verification by `file.stat` + `mtime ≥ run startedAt` | O5 |
| **S15** | Open-in-app allow-list `pdf docx doc xlsx xls pptx ppt odt ods odp rtf csv txt md png jpg jpeg gif webp`; canonicalise + inside-root + regular file + no UNC; opener plugin only, no shell; local daemon only | O5 |
| **S16** | Dependency detection list below | later (first consumer) |
| **S17** | Windows: path-separator and `\\?\` normalisation in the relative-path checks, case-insensitive extension compare, UNC refusal, share-mode-friendly open (Go's `os.Open` already shares read/write), long paths; every new test that touches paths has a Windows-aware table | O3, O5 |
| **S18** | The 1 MiB relay cap is honoured by design (S12) but **no mobile UI is built** (H3); mobile continues to show a deliverable as a plain file link | later (mobile ADR) |

**S16 detection list** (names, how detected, the install command smind *displays* for the human to run):

| Dependency | Detected by | macOS | Linux (Debian/Ubuntu) | Windows |
|---|---|---|---|---|
| LibreOffice (`soffice`) | `LookPath("soffice")`, else the macOS `/Applications/LibreOffice.app/Contents/MacOS/soffice` and the Windows `%ProgramFiles%\LibreOffice\program\soffice.exe`; `soffice --version` | `brew install --cask libreoffice` | `sudo apt install libreoffice` | `winget install TheDocumentFoundation.LibreOffice` |
| Poppler (`pdftoppm`, `pdftotext`) | `LookPath("pdftoppm")`; `pdftoppm -v` | `brew install poppler` | `sudo apt install poppler-utils` | `winget install oschwartz10612.Poppler` |
| Python 3 | `LookPath("python3")` (`python` on Windows); `--version` ≥ 3.9 | `brew install python` | `sudo apt install python3` | `winget install Python.Python.3.12` |
| Node.js | `LookPath("node")`; `--version` ≥ 18 | `brew install node` | `sudo apt install nodejs` | `winget install OpenJS.NodeJS.LTS` |

Rows are advisory text, kept in one Go table with a test per row (detect fakes, command strings). They are **never executed** by smind
or by an agent.

## 8. Later ADRs written down, not decided here

- **ADR-D: Folder workspaces without git.** Today a workspace must be a git repo and tasks are worktrees. A `folder` workspace kind
  would let a person point smind at a plain folder of documents. It touches the core invariant "a task is a git worktree": parallel
  tasks lose isolation (a single-writer lock per folder), there is no review or rollback unless a shadow repo or snapshot is added, and
  setup/teardown (ADR-0024), tab seeding (ADR-0004), git/PR state (ADR-0039) and fleet views (ADR-0048) must all degrade. Needs a
  data-model ADR and a real non-developer user (H1 says there is none yet). **Out of scope here; workspaces stay git-only.**
- **ADR-C: Document-generation skills as plugins.** Skill packs (docx, xlsx, pptx, pdf) delivered through ADR-0042 skill
  delivery, written as **original text** (the upstream licensing of the reference product's skills is ambiguous, so nothing is
  adapted), each with an env-check that follows section 6 (detect, say, give the command; never install), a read-only reviewer profile
  for visual QA (ADR-0041/0045), the scratch dir for renders, and the bundled OFL fallback font (H6). One format first. Depends on
  this ADR's preview and on D if a non-git audience appears.
- **ADR-P: docx, xlsx and pptx preview.** Only after this ADR's section 7 is answered row by row for the chosen renderer or the
  LibreOffice conversion path, with the same sandbox and caps as the PDF viewer, as a new `previewKinds` entry.
- **ADR-M: Mobile and relay document viewing.** Chunked `file.readRange` over the E2EE relay (512 KiB chunks, S12), thumbnails versus
  full view, and the rule that mobile never runs remote JS (ADR-0042 §5) mean a native or declarative viewer decision; mobile keeps
  showing a plain file link until then.

## Alternatives considered

1. **Daemon-side mode** (a profile or default per mode). Rejected by H2: the point is a safe, reversible presentation switch that
   cannot change what an agent may do.
2. **Hide-by-disable** (grey entries out). Rejected (S5): disabled controls invite "why", add tab stops, and keep the developer
   surface in the office view.
3. **Plugin-hosted viewer** (ADR-0042 panel). Rejected for v1: the declarative panel vocabulary has no image/file node and a JS
   panel needs the parked sandbox decision (Q5); a core viewer has the smaller trust surface and this ADR makes the sandbox decision
   for the viewer only.
4. **Daemon-side deliverable detection** (a per-run changed-files event from a before/after `git status`). Better coverage for
   Bash-made files and ignored paths, but it adds a wire event, a git invocation per run end, and a gitignore edge case. Deferred:
   if the client-side rule misses too many real files in dogfood, add an additive `run.files` event in a follow-up.
5. **Auto-open the first produced document** (as the reference product does for decks). Rejected (S14): stealing focus from the
   chat reads as a glitch.

## Consequences

- One new client preference, two new read-only wsapi methods and one `daemon.info` field (all additive; an older client ignores them,
  an older daemon answers `method_not_found`), one bundled lazy PDF engine, one extra HTML entry served with a CSP header, one Tauri
  command. No store migration, no config key, no permission or policy change.
- The UI has a new axis (mode) to test: every UI PR needs light and dark frames at 1280 and 390 in both modes.
- The PDF viewer adds a third-party parser to the shipped web bundle (lazy) and an ongoing duty to bump it when a CVE is published;
  the exact pin and its advisory feed go in the plan.
- Deliverable cards can miss a Bash-made file the agent never names; the fallback is the Files tab and a follow-up event (Alternative 4).
- `zcode-parity.md`'s "Not cloned: Office mode, doc/PDF previews" is amended by this ADR for the new epic (the visual-parity epic
  itself is unchanged).

## Open questions

1. Should office mode eventually seed a "Documents" Files-tab filter (only allow-listed types) by default? (Not in v1.)
2. Does a text layer in the PDF viewer need a per-line cap against huge text pages? O4 measures.
3. Is a per-workspace default mode wanted once epic D exists? Left to the D ADR.
