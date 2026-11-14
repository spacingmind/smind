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

7. When a terminal session has ended (closed or interrupted) the pane replays its retained output and
   shows "Session ended" with a labelled **Restart terminal** button.
8. Restart creates one new session, writes a dim separator line into the same xterm (the old output stays
   above it) and attaches to the new session. One click is one `terminal.create`; the button is disabled while
   pending and an error shows inline.
9. If the old session is no longer retained, the pane says "Session ended; its output is no longer kept"
   and still offers Restart.

### C. Send set

10. A "Send to agent" menu on the terminal and the editor offers: terminal selection, last 100 terminal
    lines, open file (a reference), editor selection. Every entry shows its size ("2.1 KB") before it is
    used; nothing is sent automatically.
11. Text goes into the composer through the existing queue as a fenced block with a one-line header;
    the person edits and sends. A block over 16 KiB is cut at the end with an explicit "(N KB omitted)"
    line and the size shown includes the cut. The menu says it adds text to the agent's chat.

## Test scenarios

A: `terminal-links.test.ts` (pure matchers: path/line/col, URL inside text, trailing punctuation, no
`file:`/`javascript:`/`data:` link, userinfo rejected, 2048 cap); `terminal-path.test.ts` (resolve: `..`
escape, `~`, UNC, device, absolute inside and outside the worktree, Windows drive forms);
`terminal-links-open.test.tsx` (plain click opens nothing; Ctrl+click calls `file.stat` then `openTab` and
a reveal; stat failure shows the notice and opens nothing; URL goes through `openExternalUrl` only);
`file-reveal.test.tsx` (editor scrolls to and marks the line; a second reveal on an open tab works).
B: `terminal-restart.test.tsx` (ended pane replays output; Restart calls `terminal.create` once, writes the
separator, attaches; double click is one create; not-retained copy; error path).
C: `terminal-send-set.test.tsx` (sizes shown; selection disabled when empty; text queued as a fenced block;
over-cap cut and "omitted" line; nothing queued on menu open).

Frames (web, light and dark, 1280 and 390): link hover with hint, the not-a-file notice, the file open at
a line, the ended pane with Restart and the restarted terminal with the separator, the Send menu with
sizes, and the composer after a send. Desktop real-window frames only if the DT0 audit tooling exists.

## Decisions

- Restart is client-side (a new `terminal.create` into the same xterm), not a new daemon RPC: the
  existing retention already keeps the output, and no daemon change is needed.
- The send set puts text in the composer (editable, visible, undoable) rather than attaching a file: the
  person sees exactly what the agent will get. Reversible: a file attachment can be added later.
- Links need Ctrl/Cmd+click so accidental clicks never open anything.
- The reveal request is a store, not a route, so deep links are unchanged.

## Progress

- [ ] spec
- [ ] A: matchers, resolver, link provider, reveal store and editor scroll
- [ ] B: restart
- [ ] C: send set

## Validation

(filled in per PR)
