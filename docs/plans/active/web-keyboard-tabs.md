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
- [x] AC2 pane focus
- [x] AC3 split/tab actions
- [x] AC4 Mod+, and Mod+digit
- [x] AC5 Settings → Shortcuts
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

- **AC2 (pane focus) / AC3 (split/tab actions) / AC4 (Mod+, and
  Mod+digit).** `lib/split-tree.ts` already carried `TaskLayout.
  focusedPaneId` and the layout functions (`focusPaneInLayout`,
  `closePaneInLayout`, `splitPaneEmptyInLayout`, `moveTabToPaneInLayout`)
  from an earlier item, unused by any UI -- this item is mostly wiring,
  not new data model. Added `lib/split-navigation.ts` (`findAdjacentPane`,
  ported from Paseo's `utils/split-navigation.ts` -- normalizes every
  pane's bounding box from the split tree's direction/sizes, picks the
  nearest candidate in a direction). `hooks/use-task-tabs.ts` grows
  `focusPane`/`closePane`/`splitPaneEmpty`/`moveTabToNextPane` over the
  existing layout functions. `App.tsx`'s `tab.close`/`tab.jump`/new
  `tab.next`/`tab.prev` handlers read `focusedPane` (was `defaultPane`,
  now dead and removed); new `pane.focus.*` handlers call
  `findAdjacentPane` then `focusPane`. The focused pane gets a visible
  ring (`PaneTabStrip`'s outer div, `ring-1 ring-inset ring-ring`, only
  once `paneCount > 1`) and a click-anywhere-in-the-pane handler
  (`onPointerDownCapture`) sets it, mirroring Paseo's
  `shouldFocusPaneFromEventTarget` intent without its interactive-target
  exemption (unneeded here -- see the function's own doc comment).
  `tab.new` pops the focused pane's own `NewTabButton` menu open via a
  new controlled `open`/`onOpenChange` pair on that component (mirrors
  Paseo's `workspace.tab.menu.open`, which opens a picker rather than a
  fixed kind). Every pane-focus binding carries `when: { global: true }`
  (smind's existing bypass-editable-scope mechanism), which satisfies
  "fires while typing" without needing Paseo's more elaborate
  default-combo-vs-override guard-dropping (`withoutDefaultComboGuard`) --
  smind's overrides never touch a binding's `when`, so `global: true`
  already covers both the default and any future rebind uniformly.
  "Move tab to the next pane" (plan's Decisions, singular -- not 4
  directional variants) cycles through `collectAllPanes`' tree order,
  wrapping.
  - `lib/split-navigation.test.ts`: adjacent-pane resolution across a
    nested 2x2 grid, overlap-over-center-distance tie-breaking, no
    candidate in a direction, unknown focused pane, single-pane tree.
  - `hooks/use-task-tabs.test.ts`: `focusPane`/`closePane`/
    `splitPaneEmpty`/`moveTabToNextPane`, including the last-pane guard
    and the max-tree-depth cap.
  - `App.test.tsx` ("App pane focus and pane/tab keyboard actions (Item
    6)"): the focus ring moving between panes, a pane-focus shortcut
    firing from inside the composer textarea, `tab.jump` targeting
    whichever pane is currently focused (not always the default one),
    `Mod+\` creating an empty split, `Mod+Shift+W` closing a pane (never
    the last), `Alt+Shift+T` opening the focused pane's new-tab menu,
    `Alt+Shift+]`/`[` cycling tabs with wraparound, `Mod+Shift+M` moving
    a tab to the next pane, `Mod+,` opening Settings, `Mod+<digit>`
    jumping to the Nth sidebar task (distinct from `Ctrl+Alt+<digit>`'s
    tab-position jump).

  **Combo choices not already fixed by the plan's Decisions** (the plan
  asked for defaults matching Paseo's table where it has one, adapted to
  avoid browser/OS conflicts on smind's web-only runtime):
  - `pane.split.right` `Mod+\`, `pane.split.down` `Mod+Shift+\`,
    `pane.close` `Mod+Shift+W`, `pane.focus.*` `Mod+Shift+Arrow*`,
    `pane.move-tab.next` `Mod+Shift+M` -- Paseo's own pane-management
    combos are mac-desktop-only rows (`when: { mac: true }`, no non-mac
    variant); smind's single `Mod` token makes one row cover both
    platforms, so these needed no second row.
  - `tab.new` `Alt+Shift+T`, `tab.next`/`tab.prev` `Alt+Shift+]`/`[` --
    not `Mod+T`/`Mod+Tab` (a browser tab's own shortcuts, unblockable in
    a page's own JS), matching Paseo's own substitution pattern for its
    web runtime (its `close-tab`'s `Alt+Shift+W`, its already-universal
    `Alt+Shift+[`/`]` for tab prev/next).
  - `settings.open` `Mod+,` and `sidebar.task-jump` `Mod+<digit>` are the
    plan's own explicit choices.

- **AC5 (Settings → Shortcuts).** `components/shortcuts-dialog.tsx`'s
  `<ShortcutRows />` was already written reusable ("exported so Item 13's
  settings screen can embed it") and already had rebind/reset/reset-all/
  conflicts -- only search and chord-capture were missing, plus removing
  the dialog wrapper now that Settings is rebinding's permanent home. Added
  `keyboard/shortcut-help-search.ts` (`filterShortcutHelpSections`, scaled
  down from Paseo's combinatorial-alias version: a fixed `Mod`/`Alt`/`Cmd`/
  `Ctrl` -> word-alias table against `HelpRow`'s new `effectiveCombo` field,
  since smind's rows carry one formatted string rather than Paseo's
  per-step chord array). `ShortcutRow`'s capture grew a second, opt-in
  "Record chord…" mode (multi-step, committed by Enter) alongside the
  original single-key "Change" (commits immediately, unchanged) -- a
  real timer-based capture (waiting out `CHORD_TIMEOUT_MS` after every
  key to guess "is a second step coming?") would make the overwhelmingly
  common single-key rebind feel laggy, so chord recording is a distinct,
  explicit mode instead. `components/settings/shortcuts-section.tsx` is
  the new registered section (search box + `<ShortcutRows query={...} />`).
  `SettingsScreen` grew an `initialSectionId` prop; `shortcuts.help`
  (`Shift+?`) now deep-links to `"shortcuts"` instead of opening a
  separate dialog -- the old dialog is removed outright (AC5 offered
  "reduced to help" as an alternative; removal was simpler and avoids
  keeping two overlapping rebind UIs in sync).
  - `keyboard/shortcut-help-search.test.ts`: label/note/keys matching,
    section-title-matches-keeps-everything, `cmd`/`command`/`ctrl`/
    `control` aliases resolving to a `Mod`-bound row regardless of
    platform, an unassigned row still matching by label.
  - `components/shortcuts-dialog.test.tsx` (renamed in place from testing
    the dialog to testing `<ShortcutRows />` directly): listing, grouping,
    key rendering, search narrowing a section vs. a title match keeping it
    whole, the empty state, single-key rebind, bare-modifier-stays-in-
    capture, Escape-cancels-before-any-step, conflict flagging, reset/
    reset-all, chord recording (two steps, Enter commits, `Escape`
    captured as this chord's own second step once a first step exists,
    Cancel-button abandons a chord recording in progress).
  - `components/settings/shortcuts-section.test.tsx`: registered and
    reachable from the nav, lists every binding, search narrows and
    clearing restores the full list.
  - `components/settings/settings-screen.test.tsx`: `initialSectionId`
    opens on that section; an unknown id falls back to the first
    registered one instead of blanking the screen.
  - `App.test.tsx`: `Shift+?` lands on the Shortcuts section with every
    binding listed; `Mod+,` opens Settings on its default section even
    right after a `Shift+?` left it on Shortcuts (the stale-initial-
    section reset).
