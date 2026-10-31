# Terminal touch key bar (port of Paseo's mobile terminal key bar)

Human request: on `pointer: coarse` the plain Copy/Paste row becomes Paseo's mobile terminal key bar, above the on-screen keyboard. Fine pointers: no bar. Scope: web/desktop only. The Expo mobile app (`mobile/`) has no terminal screen today (grep finds none), so nothing to do there.

## Paseo reference notes (refs/paseo/packages/app, read-only)
Sources: `src/terminal/runtime/terminal-virtual-keyboard.ts`, `terminal-key-dispatch.ts`, `src/components/terminal-pane.tsx` (styles ~l.1320+), `packages/protocol/src/terminal-key-input.ts` (encoder), `terminal-input-mode.ts`, `src/utils/terminal-keys.ts`.
- **Keys** (id/label/DOM key): Esc `Escape`, Tab `Tab`, ↑ ↓ ← → `Arrow*`, Enter `Enter`; (Backspace and Space exist as constants but are in no row). Modifier toggles: Ctrl, Shift, Alt. Controls: Paste, Keyboard show/hide toggle. Copy is NOT in the bar (a floating Copy appears on selection, native only).
- **Rows, default (compact/phone, width 300..659)**: row1 `Esc Tab Ctrl ↑ Shift Keyboard`; row2 `Alt Paste ← ↓ → Enter`. **Wide (non-compact, width >= 660)**: one row `Esc Tab Ctrl Alt Shift ← ↓ ↑ → Paste Keyboard Enter`. **Narrow (< 300)**: 4 rows of 3: `Esc Tab Ctrl / Alt Shift Keyboard / Paste ↑ Enter / ← ↓ →`.
- **Layout/sizes**: container `border-top 1px`, surface bg, padding 8px; rows column with 4px gap (max width 880, centered); each row flex with 4px gap; each key `flex:1`, `min-width:0`, height 34px, radius md, 1px border, surface1 bg, muted text base size, medium weight; pressed/hover = surface2; active modifier = primary border + surface2 bg + foreground text. No horizontal scrolling: keys share the row width.
- **Sticky modifiers**: tapping Ctrl/Shift/Alt toggles it on/off (aria selected). No double-tap lock in Paseo. Any virtual key press sends the key with the armed modifiers then clears all of them. The next character typed on the OS keyboard also consumes them: single printable-ASCII `data` (and \r, \t, \x7f, \x1b mapped to Enter/Tab/Backspace/Escape) is re-encoded as a key with the modifiers (Ctrl+c -> \x03); non-mappable data is sent raw and the modifiers still clear. Tapping a bar key refocuses the terminal and requests a reflow.
- **Encoding** (`encodeTerminalKeyInput`): printable: Ctrl -> control char (A-Z -> code-64, plus @ [ \ ] ^ _ / ? mappings), Alt prefixes ESC, Shift uppercases; Enter `\r`; Tab `\t` (Shift+Tab `\x1b[Z`, Alt prefix); Backspace `\x7f`; Escape `\x1b`; arrows `ESC [ A/B/C/D`, or `ESC O A..D` when the terminal reports application-cursor mode, with modifier param `ESC [ 1 ; <1+shift+2alt+4ctrl+8meta> X`.
- **Above the keyboard**: native uses a reanimated `useKeyboardShiftStyle` padding that tracks the keyboard height; on keyboard change it pulses terminal refits at 0/48/144/320 ms so rows recompute. The bar is shown when native or compact layout.
- **Port to web**: keyboard height from `visualViewport` (hook `useKeyboardInset`, already in this PR) as bottom margin + `env(safe-area-inset-bottom)` when closed; the existing debounced ResizeObserver refit recomputes rows. Deviation: web adds a Copy key (human asked for copy+paste in the bar) next to Paste; Keyboard toggle = blur/focus the xterm textarea.

## Acceptance criteria / named tests
- `lib/terminal-keys.test.ts`: one test per sequence: Esc `\x1b`, Tab `\t`, Enter `\r`, Backspace, arrows CSI and application-cursor SS3, Ctrl+c `\x03`, Alt+x `\x1bx`, Shift+Tab `\x1b[Z`, modifier param arrows (Ctrl+Up `\x1b[1;5A`).
- `terminal-key-bar.test.tsx` / `terminal-pane.test.tsx`: each bar key writes its sequence via `terminal.write`; `sticky modifier applies to the next key`; `sticky modifier clears after one key`; modifier armed + OS-typed `c` -> `\x03`; `touch control row renders after the terminal` (DOM order); fine pointer: bar hidden via `pointer-coarse:` class only (jsdom has no media queries); keyboard inset margin; rows resolve by width.
- Mutation per load-bearing test. Frames 390 light/dark, keyboard closed/open, + Paseo reference description.

## Decisions
- Reuse Paseo semantics exactly (toggle, no lock). Not a new ADR: UI port, no architecture change.

## Progress / Validation
- Encoder `lib/terminal-keys.ts`, bar `components/terminal-key-bar.tsx`, pane wiring (sticky modifiers, OS-keyboard consumption, application-cursor via `term.modes.applicationCursorKeysMode`) done by peer-sonnet (GLM rate-limited until 18:28), reviewed line by line.
- Validation: 156 tests pass (terminal-keys, key-bar, pane, find, keyboard-inset, App); tsc clean. Mutations, each FAILED its test: sticky clear removed -> `sticky modifier clears after one key`; onData consumption disabled -> `an armed Ctrl turns the next typed c into ETX`; application-cursor branch off -> 2 SS3 tests; Ctrl code-64 -> 65 -> `Ctrl+c sends ETX`; width 660 -> 760 -> `resolveKeyBarRows picks the layout by width`; Escape returns "" -> `Escape sends ESC`.
- Frames: /tmp/steward4/after/terminal-touch/ (see PR). No Paseo screenshots exist locally; the Paseo bar is described above from source.
