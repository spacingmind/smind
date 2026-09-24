# Web UI: chord shortcuts, pane/tab keyboard actions, Shortcuts settings

## Context

Paseo is keyboard-first: about 156 binding rows covering roughly 60
actions, multi-step chord shortcuts, rebinding in Settings with search,
and many workspace actions in its Command Center. See
`docs/research/paseo-uiux-2026-09.md` gaps #3, #4, #9 and the
`Cmd+digit` part of #8.

smind today:

- `web/packages/ui/src/keyboard/shortcuts.ts` has about 15 single-combo
  bindings. Its own comment (around line 73) calls the matcher
  "chord-less".
- Overrides persist (`keyboard/overrides.ts`). Rebinding happens in
  `components/shortcuts-dialog.tsx`, which has no search.
- `App.tsx` (around lines 488-495) says outright that there is no "which
  pane has keyboard focus" concept. `Ctrl+W` and `Ctrl+Alt+<digit>` only
  reach the default pane's tabs.
- The split tree (`lib/split-tree.ts`) exists but is mouse-only.
- The only tab dropdown is "split" (`App.tsx` around line 1282).
- Palette entries are few (`App.tsx` around lines 869/899,
  `app-sidebar.tsx` around lines 370/377).

**Port the behavior from `refs/paseo/packages/app/src` (updated
2026-09-24):**

- `keyboard/keyboard-shortcuts.ts`:
  - the `workspace-pane-*` / `workspace-tab-*` rows;
  - `ChordState` and `CHORD_TIMEOUT_MS` (around lines 1120-1380);
  - scoped `when` flags;
  - a rebound pane-focus shortcut fires while typing (#5287);
  - a multi-key shortcut survives re-renders between keys (#5255,
    #5272).
- `keyboard/shortcut-string.ts`: `parseChordString` and `chordToString`
  (around lines 172 and 235).
- `keyboard/shortcut-help-search.ts`.
- `screens/settings/keyboard-shortcuts-section.tsx`.
- `components/split-container-pane-focus.ts` and
  `split-container-focus.ts`.
- `command-center/workspace-contributions.ts` (around lines 268-460).
- `screens/workspace/workspace-tab-menu.ts`.
- `composer/agent-controls/mode.ts` (Shift+Tab mode cycle).

## Decisions

- **Chords:** extend a binding from one `KeyCombo` to `KeyCombo[]`. A
  first key that starts a chord waits for up to a timeout (take the value
  from Paseo's `CHORD_TIMEOUT_MS`). The in-progress chord survives
  re-renders. Existing single-combo bindings and persisted overrides
  keep working unchanged, so migration is transparent.
- **Pane focus:**
  - Introduce a "focused pane" concept in the split tree (the last
    interacted pane, plus explicit focus actions).
  - Tab actions (`Ctrl+W`, the digit shortcuts, next/prev) target the
    focused pane, not just the default one.
  - Pane-focus shortcuts fire even while typing in an input, matching
    Paseo #5287.
- **New actions**, with suggested defaults (read Paseo's table for the
  exact defaults and avoid clashing with browser/OS keys):
  - split right, split down;
  - focus pane left/right/up/down;
  - move tab to the next pane;
  - close pane;
  - new tab;
  - next tab / previous tab;
  - open Settings (`Mod+,`);
  - `Mod+<digit>` jumps to the Nth task in the sidebar (task jump; keep
    the existing `Ctrl+Alt+<digit>` tab-position binding);
  - Shift+Tab in the composer cycles approval mode/policy.
- **Settings → Shortcuts section:** move rebinding into the Settings
  screen (`components/settings/settings-registry.ts`, a new section).
  - Search/filter by action name or key.
  - Show effective bindings and conflicts.
  - Reset per binding and reset all.
  - The old dialog may stay as a quick-help view that links to Settings.
- **Tab context menu:** close, close others, close to the left, close to
  the right, rename (file tabs only if rename doesn't make sense
  elsewhere — decide per kind), copy path (file tabs), and the existing
  split entries.
- **Palette:** add the new pane/tab actions, open Settings, and
  switching the composer mode.
- **Out of scope:** a context-window meter (it needs usage data from the
  daemon — defer), and any daemon change.

## Acceptance Criteria

- **AC1 — chords.** A binding can be a chord (e.g. `Mod+K` then `S`);
  the matcher waits for the timeout, then cancels. Chords can be
  rebound, and single-combo bindings and persisted overrides still work.
- **AC2 — pane focus.**
  - Keyboard actions move focus between panes of the split tree, with a
    visible focused-pane indicator.
  - Tab actions apply to the focused pane.
  - Pane-focus shortcuts work while typing.
- **AC3 — split/tab keyboard actions.** Split right/down, close pane, new
  tab, next/prev tab and move tab to the next pane all work from the
  keyboard and from the palette.
- **AC4 — `Mod+,`** opens Settings; **`Mod+<digit>`** opens the Nth
  sidebar task.
- **AC5 — Settings → Shortcuts** lists every action, with search, rebind,
  per-binding reset, reset-all and conflict warnings. The old dialog is
  removed or reduced to help.
- **AC6 — tab context menu:** close, close others, close left/right,
  copy path, and the existing split entries.
- **AC7 — Shift+Tab** in the composer cycles the mode/approval policy,
  with visible feedback.
- **AC8** — all existing tests pass. `typecheck`, the web tests and
  `task lint` are green.

## Test Scenarios

- Unit:
  - chord parse/format round-trip;
  - the chord matcher (completes, times out, a wrong second key cancels,
    survives a re-render);
  - override migration from single to chord.
- Unit/component:
  - focused-pane transitions across a nested split tree;
  - tab actions target the focused pane;
  - pane focus fires from inside a textarea.
- Component:
  - Shortcuts settings search/rebind/reset/conflict;
  - tab context menu actions;
  - `Mod+,`;
  - `Mod+<digit>` task jump;
  - Shift+Tab mode cycle.
- Manual: drive a 3-pane layout entirely from the keyboard, in both
  themes.

## Progress

- [x] AC1 chords
- [ ] AC2 pane focus
- [ ] AC3 split/tab actions
- [ ] AC4 Mod+, and Mod+digit
- [ ] AC5 Settings → Shortcuts
- [ ] AC6 tab context menu
- [ ] AC7 Shift+Tab mode cycle

## Validation

- **AC1 (chords).** `keyboard/shortcuts.ts` grows `resolveChordStep`
  (pure, given a `ChordState`) alongside the existing `matchShortcut`
  (now a chord-less convenience wrapper over it at `INITIAL_CHORD_STATE`,
  so every pre-existing single-combo caller/test is unchanged).
  `ResolvedBinding.parsed` is now `KeyCombo[] | null` (a chord of length
  1 for a plain binding) via `shortcut-string.ts`'s new `parseChord`/
  `chordToString`/`canonicalChord`/`formatChord`. The real
  `CHORD_TIMEOUT_MS` (1500ms) timer lives in `keyboard-provider.tsx`,
  which threads `ChordState` through a `useRef` (not React state) so an
  in-progress chord survives a re-render between its two keys.
  Overrides need no migration code: `resolveBindings` already re-parses
  whatever string is stored, chord or not, each render.
  - `keyboard/shortcut-string.test.ts`: chord parse/format round-trip,
    canonicalization, `isModifierKeyCode`.
  - `keyboard/shortcuts.test.ts`: `resolveChordStep` (waits, completes,
    wrong-second-key cancels, bare-modifier-mid-chord is a no-op,
    auto-repeat ignored), override migration both directions
    (single→chord, chord→single).
  - `keyboard/keyboard-provider.test.tsx`: chords over the real
    `KeyboardProvider` with real timers (`vi.useFakeTimers`) --
    end-to-end fire, timeout abandons the attempt, survives a re-render,
    wrong second key cancels.
