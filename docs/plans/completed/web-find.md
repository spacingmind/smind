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

- [x] Shared FindBar + `pane.find` action
- [x] AC1 chat Find
- [x] AC2 file Find (+ replace)
- [x] AC3 terminal Find
- [x] Tests + docs

## Validation

- **Shared FindBar + `pane.find`**: `components/find/find-bar.tsx` +
  `find-bar.test.tsx` (Enter/Shift+Enter/Esc, replace row, imperative
  focus). `keyboard/actions.ts`/`shortcuts.ts` add the `pane.find` action
  bound to `Mod+F` (`global: true`, so it reaches into the editable/
  terminal focus scopes); `use-pane-focus-within.ts` is the per-pane
  focus-within gate each Find surface's `useActionHandler` call is
  `enabled` on, so only the one pane holding real DOM focus claims it —
  verified by `chat-find.test.tsx`'s "only once the chat pane is focused"
  case.
- **AC1 chat Find**: client-side, no host RPC (`use-chat-find.ts`),
  matching Decisions. `chat-find-text.ts` is the pure match model (regex
  build, counting, wraparound) — unit-tested in `chat-find-text.test.ts`
  for counting, case-insensitivity, whitespace-tolerant queries, and an
  empty query giving zero matches. `chat-find-dom.ts` walks
  `[data-chat-find-text]` containers (added to user/thinking text,
  `TimelineMarkdown`, and tool-call name/summary) to find matches, then
  paints them via the **CSS Custom Highlight API** (`CSS.highlights`/
  `Highlight`, styled in `index.css`) — the same technique Paseo's web
  build uses. `chat-find.test.tsx` exercises the full stack through
  `TaskDetailPane`: `Mod+F` gated on chat-pane focus, highlight count/
  status, Enter/Shift+Enter wraparound, close clearing every highlight,
  and — AC1's explicit streamed-text scenario — a match found in an
  assistant chunk that arrives *after* Find is already open.

  **Post-merge-review fix**: the first cut of this highlighted matches by
  wrapping each one in a real `<mark>` via `Range.surroundContents`, with
  `clearChatHighlights` unwrapping it via `parentNode.normalize()`. Code
  review flagged that both mutate the exact Text nodes React's own fibers
  reference for the live transcript — a real risk while Find is open and
  the last assistant item is still streaming, since React commits its own
  text updates/removals against those nodes independent of this code.
  Regression tests reproducing this against the pre-fix implementation
  (`chat-find-react-safety.test.tsx`) confirmed both predicted failure
  modes: a streamed update to a highlighted-then-cleared text child
  silently kept the *stale* text instead of the new one, and removing one
  outright threw `Failed to execute 'removeChild' on 'Node': The node to
  be removed is not a child of this node.` (A narrower `<p>{text}</p>`
  shape didn't reproduce anything — React DOM's own "single Text child"
  fast path (`setTextContent`) rebuilds or reuses whatever's there
  regardless of identity, self-healing by luck; the bug needed a sibling
  text expression, e.g. `<p>foo {chunk} bar</p>`, which gives the streamed
  piece its own independently-tracked fiber — the shape a real assistant
  message with surrounding text takes.)

  The fix replaces all DOM mutation with the CSS Custom Highlight API:
  `applyChatHighlights`/`restyleChatHighlights`/`clearChatHighlights` now
  only build `Range` objects and register/unregister them as `Highlight`s
  — a pure paint overlay that never touches the DOM tree, so there is
  nothing left for React's reconciliation to trip over. Feature-detected
  (`typeof Highlight`, `CSS.highlights`); an environment without the API
  degrades to counting matches and scrolling the active one into view with
  nothing painted, never falling back to a DOM-mutating technique. Since
  jsdom implements neither global, `test/css-highlight-stub.ts` installs a
  minimal, inspectable stand-in (a real seam: it patches
  `globalThis.Highlight`/`CSS.highlights`, exercising the actual
  feature-detected code path rather than mocking `chat-find-dom.ts`
  itself) that `chat-find-dom.test.ts` and `chat-find.test.tsx` assert
  against instead of querying for `<mark>` elements. `chat-find-dom.ts`
  also gained an explicit "never mutates the DOM" test, and
  `chat-find-react-safety.test.tsx` keeps both reproduced scenarios (now
  passing) plus a third confirming the same holds with no Highlight API
  available at all. Full suite (`bun run test`) stayed green throughout
  (866/866 → 887/887 with this fix), confirming AC5.
- **AC2 file Find**: `components/file-editor-find.ts`'s `FileFindModel` is
  a near-verbatim port of Paseo's `file-pane/find/model.web.ts` (same
  `@codemirror/search` API, no React Native in the original to strip) --
  CodeMirror owns query/matching/selection/replacement; the model only
  drives its commands and republishes state as a snapshot. The floating
  bar (`components/file-editor-find-bar.tsx`) renders the shared `FindBar`
  outside the editor, wired into `code-mirror-editor.tsx` via a new
  `findExtension`/`onViewReady` prop pair (added, not changed, so every
  other caller/test is unaffected) and into `file-editor-pane.tsx` next to
  its existing per-tab-mount lifecycle. `pane.find`'s own copy of `Mod-f`
  is filtered out of the model's `searchKeymap` extension so it can't
  double-fire alongside the global keyboard action. `file-editor-find.test.ts`
  drives the model against a real headless `EditorView` (open, count,
  next/previous wraparound, empty query, replace, replace-all, and the
  read-only branch hiding replace) the same way `code-mirror-editor.tsx`'s
  own tests avoid mocking CodeMirror. `file-editor-find-bar.test.tsx`
  exercises the full stack through `FileEditorPane`: `Mod+F` gated on
  editor-pane focus, match count/prev/next, replace + replace-all editing
  the live document, and Escape closing the bar. Full suite: 878/878.
- **AC3 terminal Find**: `@xterm/addon-search`'s `SearchAddon` is loaded
  in `createRealTerminal` alongside the existing `FitAddon`, exposed
  through a new optional `find` field on `TerminalHandle` (search/clear/
  onDidChangeResults) -- optional the same way `setTheme`/`getSelection`
  already are, so `FakeTerminalHandle` (this file's own test suite) is
  unaffected and a handle with no search capability simply never shows the
  Find affordance. Match/active-match decoration colors come from
  `lib/terminal-theme.ts`'s existing CSS-variable-probe technique (a new
  `resolveSearchDecorations()`), not hardcoded hex, resolved fresh on
  every search so a theme toggle mid-search picks up the new colors
  immediately. The Find UI lives inline in `terminal-pane.tsx` (not a
  separate child component): a child's first effect commits *before* its
  parent's, so a separate component subscribing to
  `termRef.current?.find` would see a still-null handle on first mount --
  keeping it in the same component as the terminal-creation effect uses
  React's own same-component effect ordering (source order) to guarantee
  the handle exists first. `terminal-find.test.tsx` mocks the addon (per
  the plan's own test-scenario note -- a real `SearchAddon` needs xterm's
  canvas renderer, which jsdom doesn't implement) to verify: `Mod+F` gated
  on terminal-pane focus, never showing Find for a handle without `find`,
  search calls forwarding query/direction, the addon's own reported
  result index/count reflected in the bar, and Escape clearing the
  addon's decorations. Full suite: 883/883.
- **AC4** (shared component, tokens-only): every Find surface renders the
  one `components/find/find-bar.tsx`; `src/test/no-hardcoded-colors.test.ts`
  ran green after every item above (it greps the whole `src/` tree, so a
  regression in any of the new files would have failed it immediately).
- **AC5** (no regressions): full suite went 842/842 (baseline) →
  866/866 (chat) → 878/878 (file) → 883/883 (terminal) → 887/887 (the
  chat-highlighting fix below), never red at any step; `bun run
  typecheck`, `task lint` (`go vet` + `gofmt`), `task test` (Go + web), and
  a production `bun run build` all green at the end. No `internal/` or
  `cmd/` Go file was touched.
- **Docs**: `docs/design.md` §15's canonical-surfaces table gets a Find
  bar row.
- **Not done**: the plan's one manual scenario ("in the running app,
  check each of the three panes in both themes") wasn't exercised — this
  environment has no browser to drive. Worth a human pass before/at
  review, particularly the terminal's decoration colors and the file
  editor's top/bottom corner-flip, which only a real layout can show.
