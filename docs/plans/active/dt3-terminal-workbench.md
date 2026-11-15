# DT3 (E6.1): terminal links, restart, send to the agent

Desktop-track item 3 of the workbench epic. Web and desktop share the UI, so every piece
lands for both. Three PRs, each branched from `wave/desktop`, one at a time:
**A** links and open-at-line (high-risk-ish: it turns terminal text into actions),
**B** restart an exited terminal, **C** the composer "send set".

## Checked against `wave/desktop` (what exists, what is dropped)

- Terminal tab (`terminal-pane.tsx`, xterm + fit + search) and file editor tab
  (`file-editor-pane.tsx`, CodeMirror) exist. Neither has link providers, a reveal-at-line request, a
  restart control or a send-to-composer action. All four are built here.
- Daemon side needs **no new RPC**: `file.stat` / `file.read` already sandbox every path through
  `files.Resolve` (UNC and device forms refused before any filesystem access, symlinks resolved,
  containment in the worktree root enforced). The Rust `open_external` bridge already refuses any scheme
  but http and https. The terminal registry already retains closed sessions and replays their scrollback
  (`closedRetentionCap`, rehydrated after a restart). The composer already takes outside text through
  `useComposerAttachmentsStore.queueText` / `queueInsert`.
- Dropped: a daemon-side restart RPC and an attachment-style send (decisions below).

## Acceptance criteria

### A. Links and open at line

1. **Path links.** Terminal output `path`, `path:line` and `path:line:col` (a path has a `/` or a file
   extension) becomes a link on hover. A match inside a URL is a URL link, never a path link.
2. **Activation needs Ctrl/Cmd+click.** Plain click keeps selecting text. The hover shows an underline and
   the hint "Ctrl+click to open" (Cmd on macOS).
3. **Path validation, two layers.** Client first filter: reject `..` that leaves the root, `~`, UNC and
   device forms (`\\`, `//` at the start, `\\.\`), and absolute paths unless they lie inside the task's own
   worktree path (then made relative). Daemon authority: `file.stat` must succeed and describe a regular
   file; any error or a directory opens nothing and shows one quiet notice "Not a file in this task's
   workspace". No path is ever passed to a shell or an OS opener.
4. **Open at a line.** A valid path opens the file tab (same placement as the explorer's implicit open) and
   the editor scrolls to the line and puts the cursor on it (the active-line band is the mark; a whole-line selection is unreadable in dark); a tab already open is revealed again on the next click. The
   line request lives in a small store, not in the URL route.
5. **URL links.** Only `http:` and `https:` URLs are linkified; the match is parsed with `URL`, rejected if
   it has user info or exceeds 2048 characters, and trailing punctuation (including an unbalanced `)`) is
   trimmed. They open through `openExternalUrl` only (system browser on desktop, `noopener` in the browser).
   `file:`, `javascript:`, `data:` and custom schemes are never linkified.
6. No new RPC, no wire change, no shell.

### B. Restart an exited terminal

7. When a terminal session has ended the pane shows its retained output and a labelled **Restart terminal**
   button. Ended means: the session is reported closed or interrupted when the pane (re)connects, **or** the live
   `terminal.attach` stream finishes (the shell exited). A stream that fails or is aborted (connection loss, tab
   switch) is not an end. For a session found ended on connect the output is replayed through `terminal.attach`
   (the daemon backfills the scrollback of a retained session, then ends the stream).
8. Restart creates one new session, writes a dim separator line into the same xterm (the old output stays
   above it) and attaches to the new session. One click is one `terminal.create`; the button is disabled while
   pending and an error shows inline.
8b. While a session is ended the cursor is hidden (DEC private mode 25), so the pane does not look live; Restart
    shows it again, and a replayed ended session ends hidden even if its scrollback re-showed the cursor.
9. If the old session is no longer retained, the pane says "Session ended; its output is no longer kept"
   and still offers Restart.

### C. Send set

10. A **Send to agent** menu on the terminal pane and on the file editor pane. Terminal: *Selection* (disabled while
    nothing is selected) and *Last 100 lines*. Editor: *Open file* and *Selection* (disabled while nothing is
    selected). Every entry shows its size before it is used ("1.2 KiB"; the editor selection also its line range).
    Nothing is sent automatically, and opening the menu queues nothing. The menu says it adds to the agent's chat.
11. Delivery reuses the composer's existing queues, the way "Add to chat" and the PR panel do:
    terminal text goes in as one fenced block under a one-line header ("Terminal output, last 100 lines, 4.8 KiB:")
    through `queueText`; the editor's *Open file* is the `@path` reference and *Selection* is `@path#Lx-Ly` through
    `queueInsert` (no text is copied: the agent reads the lines). Both land in the focused chat's draft for the
    person to edit and send; a toast offers "Open chat". The editor entries need the daemon's `file_ref` support
    (like "Add to chat"); the terminal entries do not.
12. Terminal text over 16 KiB is cut at the end on a character boundary with an explicit "(N KiB omitted)" line, and
    the size shown on the entry is the size of what will actually be added. The fence is longer than any run of
    backticks in the text, so the block cannot be closed early by its own content.
13. With no focused chat for the task the menu is not rendered.

### D. Send set as context chips (follow-up to C)

Found on the first run of C: terminal text landed in the composer as raw fenced text and the browser's spellchecker
underlined code words red; and the terminal's trigger at 390 was a lone arrow icon in an empty bar.

14. Every send-set item becomes a removable **context chip** in the composer's tray (the attachment pill's look),
    not text in the draft: `Terminal output · 100 lines · 106 B ×`, `Terminal selection · 3 lines · 46 B ×`,
    `handler.go L1–4 · 78 B ×`. Nothing raw is added to the textarea, so nothing is underlined.
15. The fenced block is built **only at send time**: each terminal chip becomes one block (header with the shown
    size, fence longer than any backtick run, the 16 KiB clip and its "(N KiB omitted)" line, all as in AC 11-12),
    appended after the typed prompt separated by a blank line, in the order the chips were added. The prompt itself
    is never changed in the draft.
16. An editor chip (open file or selection) is sent as a `file_ref` attachment (`{path}` or `{path, line, endLine}`),
    deduplicated against any `@path` token for the same reference; it replaces the `@path#Lx-Ly` token the editor
    entries used to put in the draft. "Add to chat" from the Files/Diff panes is unchanged.
17. Chips live in the composer store per task and chat, like the remembered mentions: they survive the chat tab being
    unmounted (the usual case: the send menu is used from another tab), are removed by their ×, and are cleared
    after a successful send; after a failed send they stay. A chip alone cannot be sent (the prompt text is still
    required).
18. The terminal's send trigger shows the visible label "Send to agent" at every width (also on the editor's, which
    already did), with a matching tooltip and aria-label.

## Test scenarios

A: `terminal-links.test.ts` (pure matchers: path/line/col, URL inside text, trailing punctuation, no
`file:`/`javascript:`/`data:` link, userinfo rejected, 2048 cap); `terminal-path.test.ts` (resolve: `..`
escape, `~`, UNC, device, absolute inside and outside the worktree, Windows drive forms);
`terminal-links-open.test.tsx` (plain click opens nothing; Ctrl+click calls `file.stat` then `openTab` and
a reveal; stat failure shows the notice and opens nothing; URL goes through `openExternalUrl` only);
`file-reveal.test.tsx` (editor scrolls to and marks the line; a second reveal on an open tab works).
B: `terminal-restart.test.tsx` (ended pane replays output; Restart calls `terminal.create` once, writes the
separator, attaches; double click is one create; not-retained copy; error path).
C: `send-set.test.ts` (byteLength of multi-byte text; clipText leaves a short text alone, cuts a long one on a character boundary
and reports the omitted bytes; fenceBlock uses a fence longer than the text's backtick runs and carries the header and size);
`send-menu.test.tsx` (sizes shown; a disabled entry cannot be chosen; opening queues nothing; choosing calls the handler once);
`terminal-send-set.test.tsx` (selection entry disabled when empty and showing its size when not; choosing queues one fenced block for
the focused chat's key; last 100 lines; over-cap cut with the omitted line and the cut size shown; no focused chat means no menu);
`file-send-set.test.tsx` (Open file queues `{path}`; Selection queues `{path, line, endLine}` for the selected lines; empty selection
disables it; without `file_ref` support the editor menu is not rendered).

D: `context-chips-store.test.ts` (add/remove per key; survive `clear(key)` of the tray; cleared by `clearChips`; keyed per chat);
`send-set.test.ts` additions (`terminalChipLabel` wording and line count; `appendChipBlocks(text, chips)` appends one fenced block per
terminal chip after a blank line in order, applies the clip and fence rules, and returns the text unchanged with no chips);
`context-chip-tray.test.tsx` (labels and sizes shown; × removes; Backspace on a focused chip removes; nothing rendered when empty);
`composer-send-chips.test.tsx` (the textarea never contains the block; send passes typed text + blocks to onSubmit and clears the
chips; a failed send keeps them; a file_ref chip is sent as a `file_ref` attachment and deduplicated against its `@` token;
chips alone leave Send disabled); updated `use-send-text-to-chat`, `terminal-send-set`, `file-send-set` and `send-to-agent-menu` tests
(chips instead of queued text or inserts; the trigger label is visible at every width).

Frames (web, light and dark, 1280 and 390): link hover with hint, the not-a-file notice, the file open at
a line, the ended pane with Restart and the restarted terminal with the separator, the terminal and editor Send menus with
sizes (open, one entry disabled), and the composer after a send. Desktop real-window frames only if the DT0 audit tooling exists.

## Decisions

- Restart is client-side (a new `terminal.create` into the same xterm), not a new daemon RPC: the
  existing retention already keeps the output, and no daemon change is needed.
- The send set puts terminal text in the composer (editable, visible, undoable) and files/selections as `@path#L` references through the
  queues that "Add to chat" already uses, rather than attaching files: the person sees exactly what the agent will get, and no
  new wire or composer path is added. Reversible: a file attachment can be added later.
- Links need Ctrl/Cmd+click so accidental clicks never open anything.
- The reveal request is a store, not a route, so deep links are unchanged.

## Progress

- [x] spec
- [x] A: matchers, resolver, link provider, reveal store and editor scroll (PR A, #889)
- [x] B: restart (PR B)
- [x] C: send set (PR C)
- [x] D: send set as context chips (PR D)

## Validation

PR A: matcher, resolver, open-logic and reveal tests (`terminal-links.test.ts`, `terminal-path.test.ts`,
`terminal-links-open.test.tsx`, `file-reveal.test.tsx`); web suite 464 files / 5259 tests, mobile 116 files / 1130 tests,
`task lint`, tsc. Frames (web, light and dark, 1280 and 390: hover underline, not-a-file notice, file open at the line)
all opened pairwise; a plain click opens nothing and Ctrl+click opens exactly one tab or one URL (browser popup count
checked). The frames found two real bugs the unit tests missed (a reveal spent on the empty initial document; a reveal
dropped when the lazy editor view appeared after the read), both fixed with tests. Two-axis review run before the PR (findings in the PR body).
PR B: `terminal-restart.test.tsx` (8 tests) plus the existing terminal suites; web 465 files / 5267 tests, mobile 116 / 1130,
`tsc -b`, `vite build`. Mutations on committed code, all killed: restart ref guard removed (double-click test), stream-finish
handler removed (shell-exit test), separator write removed, emulator reset removed, "no longer kept" branch removed.
A first run of the ref-guard mutation SURVIVED (the test only used the disabled state); the test was strengthened (two clicks
in one act) and the mutation then failed it. Frames: 12 (running, ended, restarted; light and dark; 1280 and 390), all opened.
PR B follow-up (cursor while ended, supervisor nit): 3 tests (a running session never touches the cursor; hidden on exit and restored by
Restart; hidden after a replay whose scrollback re-showed it); two mutations killed (cursor effect disabled; hide-after-replay removed).
Ended and restarted frames re-shot and opened, light and dark at 1280 and 390: no block cursor while ended, the outline cursor is back after Restart.
Not verified (PR A): a real daemon, the desktop webview (the `open_external` bridge is existing code), wrapped long lines
(only the row under the pointer is scanned), mutation checks (the runs were not approved).

PR C: `send-set.test.ts`, `send-menu` (`send-to-agent-menu.test.tsx`), `use-send-text-to-chat.test.tsx`, `terminal-send-set.test.tsx`,
`file-send-set.test.tsx`; web 470 files / 5294 tests, `tsc -b`, `vite build`. Mutations on committed code, all killed: clip cap removed,
fence length fixed at 3, terminal Selection never disabled, editor end-of-line adjustment removed, hook no longer queues the text.
Frames (28, light and dark, 1280 and 390: both menus open, one entry disabled, the toast, the composer after each send) all opened.
The frames found a layout flaw (a floating button covered the first output line at 390), fixed with a slim row of its own.
Note: in the dev server (StrictMode) a text queued while the chat tab is unmounted is lost on remount; production builds are fine
(frames were taken on a production build). Not verified: a real daemon, the desktop webview, `file_ref` against a real daemon.

PR D: `context-chips-store.test.ts`, `send-set.test.ts` additions, `context-chip-tray.test.tsx`, `composer-send-chips.test.tsx`,
`use-send-ref-to-chat.test.tsx` and the updated send-set tests; web 474 files / 5319 tests, `tsc -b`, `vite build`. Mutations on
committed code, all killed: blocks no longer appended at send; chip-ref dedupe removed; chips cleared before a failed send;
chips dropped by the tray's clear(key); the trigger label hidden below sm. Frames (light and dark, 1280 and 390: both menus with the
visible label, the composer holding the chips and a typed prompt with no raw text) all opened.
Not verified: a real daemon, the desktop webview, `file_ref` against a real daemon; a chip alone cannot send (by design).
