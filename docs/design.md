# smind web UI: design rules

This is the living reference for `web/packages/ui`'s token vocabulary,
density, primitives, copy rules and keyboard-action API — written from
what actually landed (`docs/plans/active/ui-redesign-parity.md`'s Items 1,
2 and 4), not aspirational. It follows Paseo's own `refs/paseo/docs/design.md` where the
two overlap (Paseo is the parity plan's north star), scoped down to what
smind's surface actually needs.

## 1. Token layers

`web/packages/ui/src/index.css` has two layers:

1. **Static scale** — the original shadcn oklch tokens (`background`,
   `foreground`, `card`, `popover`, `primary`, `secondary`, `muted`,
   `accent`, `destructive`, `border`, `input`, `ring`, `chart-1..5`,
   `sidebar*`). One value per name per theme (`:root` / `.dark`). Every
   primitive already composed from these correctly before Item 1 — this
   layer wasn't rewritten.
2. **Semantic layer** — named-by-purpose tokens added by Item 1:
   - `surface-0..3`: an elevation scale, **aliased onto the static
     scale** (`surface-0` = `background`, `surface-1` = `card`,
     `surface-2` = `muted`, `surface-3` = `accent`) rather than a second
     independent set of hex values. See Decisions below for why.
   - `foreground-muted`: an explicit alias of `muted-foreground`, so new
     code can spell it the way `refs/paseo/docs/design.md` §6 does.
   - `status-{success,danger,warning,running}`: the text/icon tier for
     pills, PR-state icons, diff stats. One token per signal
     (`refs/paseo/docs/design.md` §13) — a surface never gets a
     quieter/louder variant of the same signal.
   - `status-dot-{success,danger,warning,running}`: the dot tier, higher
     chroma than the text tier — a 6px dot with no shape or label reads
     dimmer than the text beside it at the text tier's chroma, which is
     backwards for something signaling the row's own state.

`@theme inline` re-exports every semantic token as `--color-*`, which is
what makes `bg-status-warning`, `text-foreground-muted`,
`border-status-dot-success/40`, etc. exist as Tailwind utilities — Tailwind
v4 is CSS-first here (no `tailwind.config.js`), so anything named
`--color-X` in `@theme` becomes `bg-X`/`text-X`/`border-X`/... for free.

**Rule:** no component hardcodes a hex, oklch, or Tailwind palette color
(`bg-amber-500`, `text-emerald-600`, etc.) — every color comes from a
token, so light/dark/future-theme all stay correct without touching
component code. `src/test/no-hardcoded-colors.test.ts` greps for this.

## 2. Theming

`hooks/use-theme.tsx`'s `ThemeProvider` (mounted once, `main.tsx`) owns a
`light | dark | system` preference:

- Persisted to `localStorage` (`lib/theme.ts`'s `THEME_STORAGE_KEY`).
- `system` resolves via `prefers-color-scheme` and re-resolves live if the
  OS setting changes while `system` is still selected (pinning to
  light/dark stops listening — a user who picked light shouldn't have the
  OS silently steer them back).
- Applies by toggling the `dark` class on `<html>` — the only place any
  code should touch that class (`lib/theme.ts`'s `applyResolvedTheme`).
- `index.html` carries a synchronous, dependency-free inline `<script>`
  that applies the persisted class **before first paint**, so there's no
  flash of the wrong theme. It's a hand-synced copy of
  `lib/theme.ts`'s decision logic (can't `import` a module before first
  paint) — keep the two in sync by hand if either changes; both have a
  test (`lib/theme.test.ts` for the extracted function, an `index.html`
  string assertion for the embed).

`useTheme()` works outside a `<ThemeProvider>` too (a fully-functional,
non-reactive default), so a component that reads it can still be unit
tested in isolation the way this codebase's component tests already work
(rendering one component directly, not the whole app tree) — see that
hook's doc comment for the tradeoff this makes.

**The theme control** (`components/theme-toggle.tsx`) lives in the sidebar
header today. Item 13's settings screen is its eventual long-term home;
"reachable" was Item 1's bar, not "permanent."

**Third-party panes** each needed a different fix to stop clashing with
the app's theme:

- **CodeMirror** (`components/code-mirror-editor.tsx`): an
  `EditorView.theme` extension whose values are literal `var(--foreground)`
  etc. references — the browser's cascade updates them for free when
  `.dark` toggles, no JS reactivity needed. Chrome only (background,
  gutter, cursor, selection); syntax-highlighting colors are untouched.
- **diff2html** (`index.css`): the library ships its own light/dark
  variable pairs gated behind a `.d2h-dark-color-scheme` class or
  `prefers-color-scheme`, neither of which lines up with this app's own
  theme state. Overriding diff2html's *base* variable names
  (`--d2h-bg-color` etc.) to reference smind's own tokens means
  `diff-viewer-pane.tsx` needs no dark-mode-specific class at all.
- **xterm** (`components/terminal-pane.tsx`, `lib/terminal-theme.ts`):
  xterm's `ITheme` needs literal color strings, not live `var()`
  references. `resolveTerminalTheme()` resolves each token via a
  computed-style probe (set `color: var(--x)` on a detached element, read
  back the CSSOM's always-`rgb()` serialization) and `setTheme()` is
  re-applied whenever the resolved theme changes. Chrome only
  (background/foreground/cursor/selection) — a full ANSI 16-color theme
  is a design decision of its own, deferred to Item 20.

## 3. Primitives (`components/ui/`)

| Primitive | Purpose | Canonical usage |
| --- | --- | --- |
| `PaneHeader` | Title + right action slot, one padding scale (`px-4 py-2.5`), one bottom border | `task-detail.tsx`, `file-editor-pane.tsx`, `diff-viewer-pane.tsx`, `terminal-pane.tsx` |
| `StatusDot` | 6px filled dot, dot-tier tokens | sidebar task-attention marker, `accounts-dialog.tsx`'s connection dot |
| `StatusBadge` | Pill: status-tier text on a neutral tinted shell | (available; no consumer yet — see Decisions) |
| `Alert` | Page-level notice: 1px tinted border, transparent background, variant-tinted icon+title, muted description, actions in `children` | connection-lost banners, file-conflict banner, pending-permission card, inline error rows |
| `EmptyState` | Centered, muted, short noun-phrase title + optional description/action | "No runs yet", "No changes", sidebar's empty-workspace state |
| `InlineSpinner` | Small spinner next to the thing it relates to (loading is inline by default, not a page takeover) | "Loading runs…", "Loading diff…", "Loading…" |
| `Toast` (`toast()` + `<Toaster />`) | Fire-and-forget notification queue, mounted once (`main.tsx`) | infrastructure for later items (commit/PR success, composer errors) — no consumer yet, see Decisions |

A new recurring surface reuses one of these before inventing a new pattern
— that's the whole point of Item 2.

## 4. Density

One padding scale for pane headers: `px-4 py-2.5`, via `PaneHeader`. Before
Item 2, headers drifted between `px-3 py-2` (file-editor, terminal) and
`px-4 py-3` (task-detail, diff-viewer) with no reason for the split
(`docs/research/uiux-audit.md` §2.4) — adopting the shared component fixes
this as a side effect, not a separate pass over each pane.

**Layout stability**: a surface whose state changes (a badge arriving, a
skeleton resolving to content) must not move its neighbours. The sidebar's
task row reserves a fixed-width slot for the attention dot
(`data-testid="task-attention-slot"`) regardless of whether the dot itself
is rendered — the status-text label after it never shifts. New surfaces
with a conditionally-present badge/count follow the same shape: a
fixed-size wrapper always present, the signal inside it conditional.

## 5. Copy rules

Sentence case, no trailing period on a short noun-phrase state ("No runs
yet", "No changes", "Empty" — not "No changes.", "(empty)"). Buttons are
imperative (Save, Reload, Overwrite, Commit); in-flight labels are
present-participle with a literal ellipsis ("Saving…", "Reloading…",
"Overwriting…", "Committing…"). Error copy is direct and describes state
rather than editorializing ("save failed: …", not "Sorry, something went
wrong").

## 6. State rules

- **Loading** is inline (`InlineSpinner` next to what it relates to), not
  a page-level takeover, except where a pane truly has nothing to show yet
  (a file's first read replacing the whole editor area is the one
  accepted exception here — see `file-editor-pane.tsx`).
- **Empty** is `EmptyState`: short noun phrase, one optional action.
- **In-flight actions** disable and relabel: Save/Reload/Overwrite/Commit
  all follow `{inFlight ? "Verbing…" : "Verb"}` plus `disabled={inFlight}`.
  A per-item in-flight action (the diff pane's per-file stage checkbox)
  tracks its own path/id, not a single pane-wide flag, so one slow
  checkbox doesn't freeze every other row.
- **Alerts**: one `Alert` per region. `error` variant gets `role="alert"`
  (interrupts); every other variant gets `role="status"` (polite).

## 7. Keyboard actions — the API other surfaces call

The keyboard layer (`src/keyboard/`) is split so that **which keys do
what** and **who performs the work** are two independent lists:

- `keyboard/actions.ts` — the `ActionId` vocabulary. The contract.
- `keyboard/shortcuts.ts` — `SHORTCUT_BINDINGS`: combo → action, plus
  section/label for the help dialog. **Track A owns this file.**
- `keyboard/keyboard-provider.tsx` — one window-level `keydown` listener
  and the handler registry.

### Claiming an action

Any component under `<KeyboardProvider>` (i.e. anything inside `App`)
claims an action by mounting a hook. It needs to know nothing about keys,
platforms, or focus:

```tsx
import { useActionHandler } from "@/keyboard/keyboard-provider";

useActionHandler("composer.focus", () => textareaRef.current?.focus());
useActionHandler("run.interrupt", () => stopRun(), { enabled: runIsLive });
```

- The **most recently registered enabled** handler wins. React runs effects
  child-first, so the innermost mounted claimant — the composer of the task
  actually on screen — takes the action.
- `enabled: false` keeps the registration but skips it, so the action falls
  through to an outer handler instead of being swallowed by a component
  that can't currently perform it. Prefer this over conditionally calling
  the hook (which breaks the rules of hooks anyway).
- `handler` is read through a ref: a fresh closure each render is fine, no
  `useCallback` needed.
- If **nothing** claims an action, the key event is left alone — the
  browser's own `Cmd+W` still works rather than being swallowed into a
  no-op.

`useTheme()`-style, `useActionHandler` is a no-op outside a provider, so a
component that claims an action still mounts in its own unit test with no
wrapper.

### Adding a new action

1. Add the id to `ActionId` in `keyboard/actions.ts`.
2. Add a row to `SHORTCUT_BINDINGS` (`keyboard/shortcuts.ts`) with a
   `section` and a sentence-case `label` — that's all the help dialog
   needs; it is generated, never hand-maintained.
3. Claim it with `useActionHandler` wherever it belongs.

Combos are written in one spelling (`keyboard/shortcut-string.ts`):
`Mod+Alt+T`, `Shift+?`, `Escape`, `Mod+Alt+Digit`. **`Mod` means Cmd on
mac, Ctrl everywhere else** — write `Mod`, not two platform rows. Matching
is `KeyboardEvent.code`-first so non-US layouts work; modifiers must match
exactly, so `Mod+K` never fires for `Mod+Shift+K`.

### Focus scoping

By default a binding does **not** fire while focus is in an `<input>`,
`<textarea>`, a `contenteditable`, CodeMirror (`.cm-editor`) or xterm
(`.xterm`). Set `when: { global: true }` for one that should — `Mod+K`,
`Escape`-to-interrupt and the navigation shortcuts are global; `Shift+?`
is not (typing `?` must insert a `?`).

No binding fires while a dialog holds the keyboard. A dialog claims that
with `useModalKeyboardLock(open)`; locks are counted, so overlapping
dialogs behave.

### Marking a custom surface

A widget that swallows typing but isn't an `<input>`/`contenteditable`
should carry `data-keyboard-editor` (or `data-keyboard-terminal`) on its
wrapper — see `keyboard/focus-scope.ts`.

### Rebinding

Bindings are user-rebindable. Overrides are keyed by **binding id** and
persisted to `localStorage` (`keyboard/overrides.ts`), so **never rename a
binding id** — that silently drops a user's rebind. The UI is
`components/shortcuts-dialog.tsx`; its `<ShortcutRows />` is exported so
Item 13's settings screen can embed the same list without a dialog around
it.

## 8. Decisions

- **`surface-0..3` alias the existing static scale rather than
  introducing a second independent set of hex values.** The existing
  scale (`background`/`card`/`muted`/`accent`) already satisfies the
  elevation need with no known contrast problems; a second scale would
  just be two sources of truth for the same four steps.
- **`status-danger` aliases `--destructive` rather than introducing a
  second red.** Unlike Paseo (which distinguishes a PR/CI-state red from
  a destructive-action red), smind has exactly one flavor of "bad" today.
  Revisit if a future surface needs the two to diverge.
- **`status-running`'s hue is new** — Paseo's own status-family text tier
  has no "running" color (only its dot tier does); smind's plan calls for
  one at both tiers, so this is a new blue hue at the same L/chroma band
  as the other three, picked clear of the identity-color palette the same
  way Paseo's own running dot is.
- **The dark `--sidebar-primary` anomaly** (`docs/research/uiux-audit.md`
  §2.5) was resolved by making it achromatic, matching the light theme's
  polarity — confirmed unused anywhere in `src/` before changing it.
- **`StatusBadge` and `Toast` ship with no consumer yet.** Both are
  complete, tested primitives; wiring them into a real surface (a
  running-task pill, a commit-success toast) is left to whichever later
  item first needs one, rather than inventing a speculative call site
  here.
- **CodeMirror/diff2html/xterm each needed a different integration
  shape** (live `var()`, override the library's own dark variables,
  re-applied literal colors) rather than one shared mechanism — see
  §2 above for why each library's own architecture forced a different
  answer.
- **Full terminal ANSI theming is deferred to Item 20.** Item 1's bar was
  "xterm takes its chrome from app tokens instead of always defaulting to
  light," not a full 16-color ANSI palette, which is a design decision of
  its own.
