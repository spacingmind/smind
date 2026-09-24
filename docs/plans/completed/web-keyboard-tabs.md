# Web UI: chord shortcuts, pane/tab keyboard actions, Shortcuts settings

## Context

Paseo is keyboard-first: about 156 binding rows covering roughly 60
actions, multi-step chord shortcuts, rebinding in Settings with search,
and many workspace actions in its Command Center. See
`docs/research/local/paseo-uiux-2026-09.md` (local-only, gitignored) gaps #3, #4, #9 and the
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
  - A **rebound** pane-focus shortcut fires even while typing in an
    input, matching Paseo #5287 -- the shipped **default** does not: it
    collides with the browser/OS's own Shift+Arrow text-selection key
    (word-select on Win/Linux, line-select on mac), and that collision is
    exactly what Paseo's own `editable: false` guard exists to avoid for
    the default combo specifically.
- **New actions**, with suggested defaults (read Paseo's table for the
  exact defaults and avoid clashing with browser/OS keys):
  - split right, split down;
  - focus pane left/right/up/down;
  - move tab to the next pane;
  - close pane;
  - new tab;
  - next tab / previous tab;
  - open Settings (`Mod+,`);
  - `Alt+<digit>` jumps to the Nth task in the sidebar (task jump; keep
    the existing `Ctrl+Alt+<digit>` tab-position binding). Not
    `Mod+<digit>`: Chrome/Edge/Firefox keep Ctrl/Cmd+1-9 for switching
    the *browser's own* tabs and a page never reliably sees it, matching
    Paseo's own `desktop: true`-only `Mod+Digit` vs. its `Alt+Digit` web
    fallback;
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
  - A rebound pane-focus shortcut works while typing; the shipped
    default doesn't (it's the browser/OS's own text-selection key
    there).
- **AC3 — split/tab keyboard actions.** Split right/down, close pane, new
  tab, next/prev tab and move tab to the next pane all work from the
  keyboard and from the palette.
- **AC4 — `Mod+,`** opens Settings; **`Alt+<digit>`** opens the Nth
  sidebar task (not `Mod+<digit>` -- the browser already owns that).
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
  - a rebound pane focus fires from inside a textarea; the default
    doesn't.
- Component:
  - Shortcuts settings search/rebind/reset/conflict;
  - tab context menu actions;
  - `Mod+,`;
  - `Alt+<digit>` task jump;
  - Shift+Tab mode cycle.
- Manual: drive a 3-pane layout entirely from the keyboard, in both
  themes.

## Progress

- [x] AC1 chords
- [x] AC2 pane focus
- [x] AC3 split/tab actions
- [x] AC4 Mod+, and Alt+digit
- [x] AC5 Settings → Shortcuts
- [x] AC6 tab context menu
- [x] AC7 Shift+Tab mode cycle

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
  Alt+digit).** `lib/split-tree.ts` already carried `TaskLayout.
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
  fixed kind). "Move tab to the next pane" (plan's Decisions, singular --
  not 4 directional variants) cycles through `collectAllPanes`' tree
  order, wrapping.

  Pane-focus's editable-scope rule (fixed in review, see below):
  `BindingWhen` grows `editableWhenRebound?: true`, ported from Paseo's
  `editable: false` + `withoutDefaultComboGuard` -- the pane-focus
  *default* combos (`Mod+Shift+Arrow*`) stay blocked in `editable` scope
  (they're the browser/OS's own text-selection keys there: word-select
  on Win/Linux, line-select on mac), but once the user rebinds one, the
  block lifts for that binding's new combo. `bindingAllowedInScope` reads
  `ResolvedBinding.overridden` (already computed by `resolveBindings`) to
  tell "still on the default" from "rebound" apart; `terminal` scope is
  untouched either way, matching Paseo's own guard being `editable`-only.
  - `lib/split-navigation.test.ts`: adjacent-pane resolution across a
    nested 2x2 grid, overlap-over-center-distance tie-breaking, no
    candidate in a direction, unknown focused pane, single-pane tree.
  - `hooks/use-task-tabs.test.ts`: `focusPane`/`closePane`/
    `splitPaneEmpty`/`moveTabToNextPane`, including the last-pane guard
    and the max-tree-depth cap.
  - `keyboard/shortcuts.test.ts`: `bindingAllowedInScope` with
    `editableWhenRebound` (blocked in editable at the default, allowed
    once overridden, never extended to terminal or modal), plus the same
    through `matchShortcut` against the real `pane-focus-left` binding
    (default blocked in editable/allowed elsewhere, a rebind allowed in
    editable and the old default no longer matching anything).
  - `keyboard/keyboard-provider.test.tsx`: the same over a real
    `KeyboardProvider` and a real `<textarea>` -- the default neither
    fires nor `preventDefault`s there, a rebind does fire, the default
    still fires outside a text field.
  - `App.test.tsx` ("App pane focus and pane/tab keyboard actions (Item
    6)"): the focus ring moving between panes, the default pane-focus
    combo *not* firing from inside the composer textarea, a rebound one
    firing there, `tab.jump` targeting whichever pane is currently
    focused (not always the default one), `Mod+\` creating an empty
    split, `Mod+Shift+W` closing a pane (never the last), `Alt+Shift+T`
    opening the focused pane's new-tab menu, `Alt+Shift+]`/`[` cycling
    tabs with wraparound, `Mod+Shift+M` moving a tab to the next pane,
    `Mod+,` opening Settings, `Alt+<digit>` jumping to the Nth sidebar
    task (distinct from `Ctrl+Alt+<digit>`'s tab-position jump).

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
  - `settings.open` `Mod+,` is the plan's own explicit choice.
    `sidebar.task-jump` shipped as `Mod+<digit>` initially and was
    corrected to `Alt+Digit` in review (see below) -- Chrome/Edge/Firefox
    keep Ctrl/Cmd+1-9 for their own tab switching and a page never
    reliably sees it, matching Paseo's own `desktop: true`-only
    `Mod+Digit` vs. `Alt+Digit` on web. Exact-modifier matching
    (`matchCombo`) is what keeps `Alt+Digit` from colliding with
    `tab-jump`'s `Mod+Alt+Digit`. A future desktop (Tauri) runtime that
    can actually own Ctrl/Cmd+digit could offer `Mod+Digit` there;
    nothing here forecloses it.

  **Review fixes (post-implementation, before merge):** two defaults
  shipped wrong and were corrected here, both against the same source
  (`refs/paseo/packages/app/src/keyboard/keyboard-shortcuts.ts`) misread
  the first time:
  1. Pane-focus's `Mod+Shift+Arrow*` originally carried `when: {
     global: true }`, so it fired inside a text field -- stealing
     Ctrl+Shift+Arrow's word-select and Cmd+Shift+Arrow's line-select
     from the composer. Paseo's actual rule (`editable: false`, lifted
     only on a rebind via `withoutDefaultComboGuard`) is what's
     implemented now, via the new `editableWhenRebound` flag above.
  2. `sidebar.task-jump`'s default was `Mod+Digit`, which
     Chrome/Edge/Firefox already reserve for browser tab switching and a
     page never reliably receives -- changed to `Alt+Digit`, matching
     Paseo's own `desktop: false` fallback for the same action.

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

- **AC6 (tab context menu).** `hooks/use-task-tabs.ts` grows
  `closeOtherTabs`/`closeTabsToLeft`/`closeTabsToRight` (all pane-scoped:
  they only ever touch the clicked tab's own pane, via
  `findPaneContainingTab` + a shared `closeManyFromLayout` that reapplies
  `closeTab`'s own sole-pane/preserve-empty guard fresh before each
  closure) and `renameTab` (a plain `TabEntry.title` override, trimmed,
  a no-op on blank). `App.tsx`'s `DraggableTabTrigger` wraps the existing
  tab trigger in `components/ui/context-menu.tsx`'s `<ContextMenu>`
  (already used by the file explorer's row menu -- same component,
  same "Copy path" implementation) with Close / Close others / Close to
  the left / Close to the right (each `disabled` when there's nothing to
  do, derived once per pane render rather than re-scanned per item) /
  Rename (terminal tabs only) / Copy path (file tabs only) / the existing
  Split entries. Rename swaps the trigger for a bare `<input>` in the
  same slot rather than nesting one inside `TabsTrigger`'s own `<button>`
  (invalid HTML, and unlike the close "×" span this one is a real form
  control jsdom and real browsers both refuse to focus there); Enter
  commits via blur, Escape cancels via a ref flag guarding the blur
  handler. Rename is terminal-only: a file/diff/chat tab's title is
  derived from real identity (a path, or what the tab fundamentally is),
  so a cosmetic override would just be misleading, whereas a terminal
  tab's title is already arbitrary.
  - `hooks/use-task-tabs.test.ts`: each of the three multi-close
    functions (closes the right set, no-ops at either edge, only ever
    touches the tab's own pane), `renameTab` (overrides the title, trims
    and ignores blank, persists across a remount, no-ops for an unopened
    tab).
  - `App.test.tsx` ("App tab context menu (Item 6)"): Copy path shown
    only on a file tab and Rename only on a terminal tab (never both),
    Copy path writes to the clipboard, rename commits on Enter and is
    discarded on Escape, close others/left/right each verified against a
    3+-tab strip, the existing Split entries still present alongside the
    new ones. (Uncovered a real cross-test leak while writing these:
    `lib/terminal-sessions.ts`'s tab<->session binding is a module-level
    singleton that outlives any one render tree, so this file's
    `afterEach` now calls `resetTerminalSessions()` -- the same fix
    `terminal-pane.test.tsx` already has, needed here for the first time
    now that more than one test opens a terminal tab for the same task
    id.)

- **AC7 (Shift+Tab mode cycle).** `components/composer/composer.tsx`
  already had everything the plan's Decisions call for except the
  keystroke itself: a submission-time `approvalPolicy` `useState`, and
  `approvalPolicyOptions(provider)` giving the exact ordered, per-provider
  option list ("Manual approval" / "Auto-safe" / that provider's own
  full-access wording). Added
  `components/composer/approval-policy-cycle.ts`'s
  `resolveNextApprovalPolicy` (ported from Paseo's
  `composer/agent-controls/mode.ts`'s `resolveNextAgentModeId`: cyclic
  next-index, wrapping, starting from the first option if the current
  value isn't found), wired into `handleKeyDown` on `Shift+Tab` (gated on
  `!inactive`, matching the Select's own disabled state). No separate
  visible-feedback indicator was added: the toolbar's existing Select
  re-rendering with the new value already is that feedback, and a second
  indicator would be one more thing to keep in sync with it for no
  reason.
  - `components/composer/approval-policy-cycle.test.ts`: advances,
    wraps, starts from the first option for an unrecognized current
    value, returns null with fewer than two options.
  - `components/composer/composer.test.tsx`: `Shift+Tab` cycles through
    all three policies and wraps, the cycled-to value is what the next
    submission actually carries, a plain `Tab` (no Shift) leaves the
    policy untouched.

## Not done

- **The Shortcuts settings capture UI doesn't record a chord in the same
  motion as a plain rebind.** "Change" (single key, commits immediately)
  and "Record chord…" (multi-step, Enter commits) are two separate
  buttons/modes rather than one unified capture that infers which you
  meant. A real timer-based unification (wait `CHORD_TIMEOUT_MS` after
  every key to see if another follows) was rejected because it would
  make the overwhelmingly common single-key rebind feel laggy; an
  Enter-driven second mode was the tradeoff made instead. `rebind()`
  itself fully supports a chord string either way (AC1's "chords can be
  rebound" holds at the data layer), so this is a capture-UX gap, not a
  functional one.
- **A context-window meter** was explicitly out of scope (needs daemon
  usage data) per the plan's Decisions, and no daemon change was made
  (per the task's own instructions).
- **Rename applies to terminal tabs only**, not file tabs, per the
  plan's "decide per kind": a file/diff/chat tab's title is derived from
  real identity, so a cosmetic override would just be misleading, and
  an actual on-disk file rename is a materially different, much larger
  feature this item never scoped.

## AC8 validation

Full green run after every item above (including the review fixes),
from the repo root: `cd web && bun run --filter '@smind/ui' test` (934
tests, 77 files, 0 failures), `cd web/packages/ui && bun run typecheck`
(clean) and `bun run build` (clean production build), plus `task lint`
and `task test` from the repo root (Go tests, `go vet ./...` and
`gofmt -l` all clean -- no Go code touched by this plan). No existing
test was weakened to make it pass; where an existing test's own premise
changed (the `Shift+?` dialog's tests, `defaultPane`-targeted tab
actions, and -- after review -- the pane-focus-in-a-textarea and
`Mod+<digit>` tests), the test was rewritten to assert the new, correct
behavior rather than deleted.

**Rebased onto `origin/develop`** after #186 (`feat/web-sidebar-attention`)
and #187 (`feat/web-find`) merged. Two textual conflicts, both plain
unions kept as-is (no semantic decision needed): `keyboard/actions.ts`'s
`ActionId` union (`pane.find` next to this plan's own new ids) and
`settings-screen.tsx`'s section-registration imports (`notifications-
section` next to `shortcuts-section`). `pane.find`'s binding
(`keyboard/shortcuts.ts`) merged in cleanly and needed no changes to work
through this plan's chord matcher (a single-combo binding is a chord of
length 1) or to appear in Settings → Shortcuts (both read the same
`SHORTCUT_BINDINGS` array). Found and fixed one stale comment in
`App.tsx` left over from before the review-fix commit (a
`pane.focus.*`-handler comment still claiming `when: { global: true }`,
which was already wrong post-review even before the rebase). Verified
this plan's split-tree "focused pane" (used by `tab.close`/`tab.jump`/
etc., set by a click via `onPointerDownCapture`) and web-find's own
per-component `usePaneFocusWithin` (real DOM focus/blur, gating each
mounted pane's own `pane.find` handler) are genuinely independent
mechanisms with no shared state -- confirmed with a new App-level test
(a Chat pane and a split-off Terminal pane, `Mod+F` opening Find only in
whichever one actually has DOM focus, and the split-tree's own
`onPointerDownCapture` not swallowing or otherwise interfering with that
focus). Post-rebase, still green: 1067 web tests across 94 files (this
plan's own plus develop's, including #186's and #187's), typecheck
clean, production build clean, `task lint` clean.
