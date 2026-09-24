# Web UI: Find in chat, file and terminal (Paseo 0.9 parity)

## Context

Paseo 0.9 shipped `Cmd/Ctrl+F` Find across chat, file and terminal
(`docs/research/paseo-uiux-2026-09.md`, gap #1). smind has no Find
anywhere in `web/packages/ui/src`:

- **Chat:** the timeline (`components/timeline/*`, `hooks/use-run-timeline.ts`)
  has no search. It loads a run's full history on the client (`run.logs`,
  `use-run-timeline.ts` ~L448/L525), so chat Find can be purely
  client-side — no host search RPC like Paseo's.
- **File:** the CodeMirror editor only has the stock, unstyled search panel
  from `basicSetup`.
- **Terminal:** xterm has only `@xterm/addon-fit`; there is no
  `@xterm/addon-search`.

**Port the behavior from `refs/paseo/packages/app/src` (updated
2026-09-24), not the code verbatim.** Paseo is React Native + web, smind
is React + Tailwind + shadcn.

- `components/pane-find/index.tsx`: shared Find bar UI — input, match
  count "3/17", prev/next, close.
- `components/pane-find/find-shortcut.ts`: Cmd+F on macOS, Ctrl+F
  elsewhere (#5129).
- `components/agent-stream/chat-find/{model.ts,index.web.tsx,viewport.web.ts,ranges.web.ts}`:
  match counting across the whole chat (#5167), and matching inside
  assistant text that streamed in (#5146).
- `components/terminal/find/index.tsx`.
- `components/file-pane/find/{model.web.ts,index.web.tsx}`: file Find with
  replace (#4589).

## Decisions

- **One shared `FindBar` component** (a new `components/find/`), used by
  all three panes. Same look, same keys: Enter = next, Shift+Enter = prev,
  Esc = close and return focus to the pane.
- **Keyboard:** register a `pane.find` action in smind's existing keyboard
  registry (`keyboard/shortcuts.ts`), bound to `Mod+F` (Cmd on macOS,
  Ctrl elsewhere). It applies to whichever pane has focus: chat,
  file/editor or terminal. It must not steal the browser's Find when no
  smind pane is focused.
- **Chat Find is client-side.** Search the rendered timeline items' text
  (user messages, assistant markdown text, thinking, tool-call titles).
  Highlight matches in place, and scroll the active match into view. Keep
  it cheap: debounce input, and don't re-render the whole transcript per
  keystroke (the timeline rows are memoized — keep that property).
- **File Find:** style CodeMirror's `@codemirror/search` into the shared
  bar, or drive its commands from the shared bar. Include replace /
  replace-all for editable files. Read-only views get Find only.
- **Terminal Find:** add `@xterm/addon-search` (install with bun in the web
  workspace), wired to the shared bar with prev/next and decorations.
- **Out of scope:** find in diff (a separate follow-up), regex/case
  toggles beyond a simple case-sensitive toggle (optional), and
  cross-pane global search.

## Acceptance Criteria

- **AC1** `Mod+F` in a focused chat pane opens the Find bar. Typing
  highlights every match in the loaded transcript, shows "i/N", and
  Enter/Shift+Enter moves between matches, scrolling each into view.
  Esc closes the bar and clears the highlights. Matches inside
  streamed-in assistant text are found.
- **AC2** `Mod+F` in a focused file editor opens the same bar, backed by
  CodeMirror search, with match count and prev/next. Replace and
  replace-all work in editable files.
- **AC3** `Mod+F` in a focused terminal opens the same bar, backed by
  `@xterm/addon-search`, with prev/next over the scrollback.
- **AC4** One shared `FindBar` component, styled with smind tokens
  (`docs/design.md`) and theme-aware — no hardcoded colors (the existing
  no-hardcoded-colors test must pass).
- **AC5** Existing behavior is unchanged: the keyboard registry's other
  bindings, the timeline's memoized rendering, and all existing tests.

## Test Scenarios

- Unit: the chat-find match model — counting across items, case
  handling, the active-index wrap-around, and an empty query giving
  zero matches.
- Component tests (existing vitest + testing-library setup):
  - FindBar keys (Enter, Shift+Enter, Esc).
  - `Mod+F` opens the bar only when a pane is focused.
  - Chat highlights appear and clear.
  - File Find drives CodeMirror (open, count, replace).
  - Terminal Find calls the search addon (mock the addon).
- `cd web && bun run --filter '@smind/ui' test` and `typecheck` green;
  `task lint` green.
- Manual: in the running app, check each of the three panes in both
  themes.

## Progress

- [ ] Shared FindBar + `pane.find` action
- [ ] AC1 chat Find
- [ ] AC2 file Find (+ replace)
- [ ] AC3 terminal Find
- [ ] Tests + docs

## Validation

To be filled in as items land.
