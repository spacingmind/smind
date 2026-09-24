# smind web UI: design rules

This is the living reference for `web/packages/ui`'s token vocabulary,
density, primitives, copy rules, and the keyboard/palette registration APIs
— written from what actually landed. Visual values (tokens, typography,
spacing, radii, shadows) follow ZCode (Z.AI's Apache-2.0 AI coding
workbench, cloned read-only at `refs/zcode`) as of the
`zcode-visual-parity` plan's P1 phase — `refs/zcode/DESIGN.md` is the
source of truth for those, and this file summarizes what P1 actually
landed plus the behavioral systems (keyboard, palette, persistence,
responsive layout) ZCode has no equivalent for. See `NOTICE` for the
Apache-2.0 attribution this incorporation requires.

## Character

smind is a calm technical console: quiet, dense, and operational rather
than decorative — the same thesis `refs/zcode/DESIGN.md`'s Product
Character section states for ZCode. Every visual decision serves either
*act on this* or *understand this* — never *look at this*.

Consistency comes from component reuse, not from hand-matching styles
across surfaces. When two surfaces do the same semantic thing in two
different ways, one of them is wrong — the shared primitive (§3) wins,
and the canonical-surfaces table (§15) says which file is the reference.

Interaction behavior (Find, chord shortcuts, split panes, attention/
unread/pins) is smind's own, ported from the Paseo-derived UX that
predates this plan — `zcode-visual-parity` changes what these things
*look* like, not what they *do*.

## 1. Token layers

`web/packages/ui/src/index.css` has two layers:

1. **Static layer** — the original shadcn oklch scale (`background`,
   `foreground`, `card`, `popover`, `primary`, `secondary`, `muted`,
   `accent`, `destructive`, `border`, `input`, `ring`, `chart-1..5`,
   `sidebar*`). `background`/`foreground`/`card`/`popover`/
   `popover-foreground`/`primary`/`primary-foreground`/`secondary`/
   `accent`/`destructive`/`border`/`sidebar` are overwritten in place
   with ZCode's Zai Light/Zai Dark values (the plan's P1 Step 2) — this
   is also where the old teal `--primary` is superseded by ZCode's
   neutral black/white primary; the sidebar wordmark is the one element
   that keeps its own hue (see the Brand colour note below). `--ring` is
   neutralized (chroma zeroed, same lightness) rather than aliased —
   ZCode has no ring/focus-glow token, and its inputs are "calm and
   integrated, not glowing by default" (`refs/zcode/DESIGN.md`). `--input`
   and `--chart-*` are untouched holdouts (see the input token note
   below); `--radius*` is gone entirely (§ Radius).
2. **Semantic layer** — ZCode's own token vocabulary, ported verbatim
   (names and values) from `refs/zcode/packages/ui/src/styles.css`'s
   `.theme-zai-light`/`.theme-zai-dark` blocks (the active light/dark
   experiences per `refs/zcode/DESIGN.md`'s Theme Modes section; ZCode's
   plain `:root`/`.dark` fallback palettes are not ported). Covers:
   background/surface/card/popover/menu/input families, foreground
   hierarchy (`-subtle`/`-subtlest`/`-inverse`), border families,
   success/warning/destructive, diff added/removed, the git-status
   family, find-highlight, hover/selected/primary/secondary, toast/
   tooltip/tag, and the terminal 16-color set. `--ui-font-size` and the
   `--text-ui-*` type scale (§12) live here too. `--foreground-muted` (an
   explicit alias of `muted-foreground`) is the one pre-existing semantic
   token that survived unrelated to ZCode's vocabulary.

**The input token exception.** smind's pre-existing `border-input`
(shadcn's `--input`, used as a border color) and ZCode's `--color-input`
(a field *background*) are different concepts that happen to share a
name upstream. `--input`'s raw value is left alone; ZCode's field
background/border family is ported as `--color-input`/
`--color-input-border`/`--color-input-border-hover`/
`--color-input-border-focused`/`--color-input-focused` instead, and
`components/ui/` primitives (`input.tsx`, `select.tsx`) read those, not
the old `border-input`/`bg-input` pair.

`@theme inline` re-exports every semantic token as a `--color-*` var,
which is what makes `bg-success`, `text-foreground-subtle`, `bg-menu`,
etc. exist as Tailwind utilities with zero extra config (Tailwind v4 is
CSS-first — there is no `tailwind.config.js` in this package).

**Rule:** no component hardcodes a hex, oklch, or Tailwind palette color
(`bg-amber-500`, `text-emerald-600`, etc.) — every color comes from a
token, so light/dark stay correct without touching component code.
`src/test/no-hardcoded-colors.test.ts` greps for this;
`src/test/zcode-tokens.test.ts` resolves every ZCode-derived token through
`index.css`'s own var() chains and asserts it matches ZCode's source value
in both themes (`src/test/zcode-tokens.fixture.json`); `src/test/
token-presence.test.ts` covers the handful of tokens that survive from
before this plan (shadow tiers, durations, `--text-content`,
`--diff-addition`/`-deletion`).

**Retired tokens.** smind's pre-ZCode semantic layer (`surface-0..3`,
`status-{success,danger,warning,running}`, `status-dot-*`, and the
type-role scale minus `--text-content`) is gone — every component was
migrated onto ZCode's tokens and the aliases deleted in the same PR (the
plan's P1 Step 4). `--diff-addition`/`--diff-deletion` still alias onto
`--diff-added`/`--diff-removed` for now — `index.css`'s diff2html
override block is the only remaining consumer, and repointing it fully
is `zcode-visual-parity`'s P4 scope.

## 2. Theming

`hooks/use-theme.tsx`'s `ThemeProvider` (mounted once, `main.tsx`) owns a
`light | dark | system` preference:

- Persisted to `localStorage` (`lib/theme.ts`'s `THEME_STORAGE_KEY`).
- `system` resolves via `prefers-color-scheme` and re-resolves live if
  the OS setting changes while `system` is still selected (pinning to
  light/dark stops listening — a user who picked light shouldn't have the
  OS silently steer them back).
- Applies by toggling the `dark` class on `<html>` — the only place any
  code should touch that class (`lib/theme.ts`'s `applyResolvedTheme`).
  ZCode's Zai Light/Zai Dark values live directly in `:root`/`.dark`
  (§1) — the `.theme-zai-*` class *names* are not shipped, only their
  values, so this mechanism needed no changes.
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
header today.

**Third-party panes** each needed a different fix to stop clashing with
the app's theme:

- **CodeMirror** (`components/code-mirror-editor.tsx`): an
  `EditorView.theme` extension whose values are literal `var(--foreground)`
  etc. references — the browser's cascade updates them for free when
  `.dark` toggles, no JS reactivity needed. Chrome only (background,
  gutter, cursor, selection, now reading `--card`/`--surface` instead of
  the retired `--surface-1`/`--surface-2`); syntax-highlighting colors are
  untouched.
- **diff2html** (`index.css`): the library ships its own light/dark
  variable pairs gated behind a `.d2h-dark-color-scheme` class or
  `prefers-color-scheme`, neither of which lines up with this app's own
  theme state. Overriding diff2html's *base* variable names
  (`--d2h-bg-color` etc.) to reference smind's own tokens (including the
  `diff-added`/`diff-removed` family for the +/- pairs) means
  `diff-viewer-pane.tsx` needs no dark-mode-specific class at all. Full
  parity with ZCode's git-status colors is P4 scope.
- **xterm** (`components/terminal-pane.tsx`, `lib/terminal-theme.ts`):
  xterm's `ITheme` needs literal color strings, not live `var()`
  references. `resolveTerminalTheme()` resolves each token via a
  computed-style probe (set `color: var(--x)` on a detached element, read
  back the CSSOM's always-`rgb()` serialization) and `setTheme()` is
  re-applied whenever the resolved theme changes. Chrome only today
  (background/foreground/cursor/selection); ZCode's ported 16-color
  terminal set (`--color-terminal-*`) exists in `index.css` and is wired
  through the same probe in P4.

## 3. Primitives (`components/ui/`)

| Primitive | Purpose | Canonical usage |
| --- | --- | --- |
| `PaneHeader` | Title + right action slot, one padding scale (`px-4 py-2.5`), one bottom border | `task-detail.tsx`, `file-editor-pane.tsx`, `diff-viewer-pane.tsx`, `terminal-pane.tsx` |
| `StatusDot` | 6px filled dot, `success`/`destructive`/`warning` (ZCode has no distinct "running" hue — see §12) | sidebar task-attention marker, `accounts-dialog.tsx`'s connection dot |
| `StatusBadge` | Pill: the semantic color on a neutral tinted shell | (available; no consumer yet — see Decisions) |
| `Alert` | Page-level notice: 1px tinted border, transparent background, variant-tinted icon+title, muted description, actions in `children` | connection-lost banners, file-conflict banner, pending-permission card, inline error rows |
| `EmptyState` | Centered, muted, short noun-phrase title + optional description/action | "No runs yet", "No changes", sidebar's empty-workspace state |
| `InlineSpinner` | Small spinner next to the thing it relates to (loading is inline by default, not a page takeover) | "Loading runs…", "Loading diff…", "Loading…" |
| `Toast` (`toast()` + `<Toaster />`) | Fire-and-forget notification queue, mounted once (`main.tsx`); `rounded-2xl`/`bg-toast`/`shadow-lg` per ZCode's toast exception | infrastructure for later items (commit/PR success, composer errors) — no consumer yet, see Decisions |
| `Button` (`buttonVariants`) | Six generic variants (`default`/`secondary`/`destructive`/`outline`/`ghost`/`link`) plus three additive semantic variants with product meaning: `execute` — starts/stops a run; `approval` — the recommended/confirm action in a permission decision; `quiet` — a non-committal or structural action that shouldn't compete visually with the primary action nearby. One radius (`rounded-lg`) across every size variant — ZCode: "Control size... do not independently change radius." | `composer.tsx`'s Send/Stop pair (`execute`); `permission-option-button.tsx`'s recommended-allow option (`approval`); `crud-dialogs.tsx`'s shared `FormActions` Cancel button (`quiet`) |

A new recurring surface reuses one of these before inventing a new pattern
— that's the whole point of this table.

## 4. Density

One padding scale for pane headers: `px-4 py-2.5`, via `PaneHeader`.

**The spacing scale is Tailwind's default numeric scale — and nothing
else.** `1`=4px through `16`=64px (`1 2 3 4 6 8 12 16`, plus the
half-steps `0.5/1.5/2.5/3.5` Tailwind itself provides) is the one legal
spacing vocabulary — this matches `refs/zcode/DESIGN.md`'s own Spacing
section (base unit 4px). An arbitrary value (`p-[13px]`, `gap-[10px]`) is
a Forbidden-list item (§14): if the nearest scale step doesn't work, the
layout is fighting the rhythm, not missing a step.

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
- **State changes transition, they don't teleport.** A `StatusBadge`
  whose status flips (queued→running) transitions its colors over
  `--duration-hover` — state-change motion is for agent moments like
  this, never decorative entrance animation on every mount (§13).

## 7. Keyboard actions — the API other surfaces call

The keyboard layer (`src/keyboard/`) is split so that **which keys do
what** and **who performs the work** are two independent lists:

- `keyboard/actions.ts` — the `ActionId` vocabulary. The contract.
- `keyboard/shortcuts.ts` — `SHORTCUT_BINDINGS`: combo → action, plus
  section/label for the help dialog.
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

`useTheme()`-style, `useActionHandler` is a no-op outside a provider, so
a component that claims an action still mounts in its own unit test with
no wrapper.

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
a `<textarea>`, a `contenteditable`, CodeMirror (`.cm-editor`) or xterm
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
the Settings screen can embed the same list without a dialog around it.

## 8. Command palette — contributing entries

`Mod+K` opens `components/command-palette.tsx`. **That component contains
no commands.** Entries come from sources registered through
`palette/palette-provider.tsx`:

```tsx
import { useCommands } from "@/palette/palette-provider";
import type { Command } from "@/palette/commands";

const commands = useMemo<Command[]>(
  () => files.map((path) => ({
    id: `file-${path}`,
    group: "Files",          // the heading this row appears under
    title: path.split("/").pop()!,
    subtitle: path,
    keywords: [path],        // matched, not displayed
    action: "tab.close",     // optional: renders that action's shortcut
    run: () => openFile(path),
  })),
  [files, openFile],
);
useCommands("my-surface:files", 3, commands);   // (sourceId, groupRank, commands)
```

- `sourceId` is namespaced into each command's key, so two sources can use
  the same command `id`. Registering again under the same `sourceId`
  replaces the previous set; unmounting removes it.
- `groupRank` orders groups for an empty query. Current ranks: tasks 0,
  workspaces 1, tabs 2, files 3, shell actions 4, sidebar actions 5.
- **Memoize `commands`.** Not memoizing is survivable (registration is
  render-free by design) but re-registers every render.
- Matching is per-field fuzzy subsequence, title-weighted. Filtered
  results stay grouped; groups order by their best match.
- Outside a `<PaletteProvider>`, `useCommands` is a no-op — a component
  that contributes commands still mounts bare in its own unit test.

A surface registers the commands for the dialogs *it* owns —
`app-sidebar.tsx` registers New workspace / Open accounts, not `App.tsx`.

## 9. Persisting a UI preference

`lib/storage.ts` is the one mechanism: `readStored(key, fallback, validate)`
/ `writeStored(key, value)`, both `localStorage`-backed, both unable to
throw. `readStored` takes a type guard rather than a schema library — check
the parts you actually read — and falls back on anything that doesn't
validate: absent, malformed JSON, or well-formed JSON of the wrong shape
(the normal case after a deploy changes a stored shape, not an exotic one).
Every key lives in `STORAGE_KEYS`, namespaced `smind:`, so a collision is
visible in one place. `use-sidebar-width.ts` and `use-task-tabs.ts` are the
two callers.

Hash routing (`lib/route.ts`) is the other piece: the URL is a
*mirror* of selection state (`App.tsx`'s restore/sync effects), not its
source of truth — selecting a task or switching a tab writes the hash, and
a `hashchange` (back/forward, a hand-typed URL) feeds back through the
same restore path. See `App.tsx`'s routing section for the two-effect
shape and why it's loop-safe.

## 10. Responsive / compact layout

`hooks/use-mobile.ts`'s `useIsMobile()` is the one breakpoint: 768px,
Tailwind's own `md`. Nothing below the breakpoint uses a different number
— the shell's layout branch (`App.tsx`), the shadcn Sidebar primitive's
own overlay switch, and every compact touch-target class all read the
same `md:` cutoff, so there's no separate JS threshold to keep in sync
with the CSS one.

**The shell** (`App.tsx`'s `AppShell`): below the breakpoint, the
sidebar-vs-content split and the side dock both stop being
`ResizablePanelGroup`/`ResizablePanel` layouts and become plain flex
children instead — a `ResizablePanel` reserves its `minSize` even for a
child that renders nothing into its own DOM position (shadcn's `Sidebar`
primitive renders into a portaled `Sheet` once `useIsMobile()` is true),
so wrapping one in a panel below the breakpoint would reserve real
column width for an invisible box. The side dock's two tabs merge into
one visible strip rather than one of them disappearing — `useTaskTabs.ts`
already resolves a tab key to whichever pane holds it, so a merged strip
costs nothing beyond hiding the now-nowhere-to-go "open to side"
affordance.

**Touch targets**: 44px (WCAG 2.5.5 AAA / Apple HIG) below the
breakpoint, reverting to the existing dense size at/above it, via plain
responsive Tailwind (`h-11 ... md:h-6`, etc.) rather than a JS `isMobile`
prop threaded through each component — `cn`'s `twMerge` resolves the
unprefixed/`md:`-prefixed pair without conflict, so the same className
string is correct at every width. `permission-option-button.tsx`'s
`COMPACT_TOUCH_BUTTON_CLASS` (shared by all three permission-card
variants) and `composer.tsx`'s `COMPACT_TOUCH_ACTION_BUTTON_CLASS`/
`SELECT_CLASS` are the two places this landed; a surface with a small
(non-full-row) tap target follows the same pattern rather than inventing
a new size scale.

## 11. Decisions

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
- **`status-running` (and its dot-tier equivalent) alias onto `warning`,
  not a distinct hue.** `refs/zcode/DESIGN.md`'s own workflow-timeline
  section uses `--color-warning` for a running station lamp, so smind's
  "running" states (composer's execute-button variant, `StatusDot`'s
  `running` status) do the same rather than keeping the old
  Paseo-inspired blue "running" color the ZCode vocabulary doesn't have
  an equivalent for. This is a deliberate loss of distinctness, an
  accepted cost of "adopt ZCode's token vocabulary wholesale."
- **`--ring` is neutralized rather than left teal or deleted.** ZCode
  defines no ring/focus-glow token at all and its inputs are explicitly
  "not glowing by default" — but generic `focus-visible:ring-*` usage
  outside `components/ui/`'s input-family primitives still exists
  (buttons, checkboxes, etc.), so the token stays, just without the old
  brand hue. `components/ui/input.tsx`/`select.tsx`'s own focus states
  use `border-input-border-focused`/`bg-input-focused` instead of a ring
  at all, per ZCode's actual pattern.
- **The `text-ui-*` scale replaces the old type-role tokens** rather than
  living alongside them — see §12.

## 12. Typography — the `text-ui-*` scale

`refs/zcode/DESIGN.md`'s "Highest-priority UI constraint": application
interface text must use `text-ui-xl`/`text-ui-lg`/`text-ui-base`/
`text-ui-caption`/`text-ui-sm`/`text-ui-xs` — never Tailwind's built-in
`text-base`/`text-sm`/`text-xs`, never an arbitrary `text-[13px]`, never
an inline `font-size`. `src/test/text-ui-scale.test.ts` is the guard —
sibling to `no-hardcoded-colors.test.ts`, it fails the build on any of
those three, with deliberately-violating fixtures proving the detector
actually fires.

| Token | Formula | Default |
| --- | --- | --- |
| `text-ui-xl` | `--ui-font-size + 4px` | 18px |
| `text-ui-lg` | `--ui-font-size + 2px` | 16px |
| `text-ui-base` | `--ui-font-size` | 14px |
| `text-ui-caption` | `--ui-font-size - 1px` | 13px |
| `text-ui-sm` | `--ui-font-size - 2px` | 12px |
| `text-ui-xs` | `--ui-font-size - 4px` | 10px |

`--ui-font-size` (default 14px, `:root`-only — it doesn't vary by theme)
is the one interface-scaling variable; changing it rescales every
`text-ui-*` utility. `--text-ui-2xs` and `text-mobile-input-safe` are not
ported — no consumer (workflows and mobile-Web iOS are out of scope for
smind; revisit if either changes).

**Exceptions**: code, diff, and terminal *content* keep their own
independent numeric font-size settings (smind's existing
`--font-scale-code`/`--font-scale-content` mechanism, and CodeMirror's/
xterm's own font handling) — their surrounding chrome (headers, buttons,
error rows) is not exempt and uses `text-ui-*` like everything else.
`--text-content` (§1) is the one pre-existing type-role token that
survives, for exactly this reason: markdown prose must not rescale with
the interface-density setting.

**Weight** follows `refs/zcode/DESIGN.md`'s looser guidance rather than
the old strict three-tier token system (retired along with the type-role
tokens it was defined against): prefer `font-medium` for headings and
labels, avoid heavier weights without a reason, and keep `font-normal`
for body text, row titles, button labels, and badge text. The sidebar
wordmark (`app-sidebar.tsx`) is still the one deliberate `font-semibold`
in the app — the plan's Brand colour decision, not a typography rule.

Markdown's own heading hierarchy (`components/timeline/
timeline-markdown.tsx`) mechanically migrated onto `text-ui-*` in P1
(`text-base`→`text-ui-base`, `text-sm`→`text-ui-sm`, preserving the
existing pixel sizes) without yet adopting ZCode's h1/h2 sizing
(`text-ui-xl`/`text-ui-lg`) — that's `zcode-visual-parity`'s P3 scope,
"Timeline rows match `v4/ConversationTimeline.tsx`".

## 13. Radius, elevation, shadow, and motion

**Radius is Tailwind's own default scale — no override.** `index.css` has
no `--radius-*` theme entries (ZCode's own `styles.css` has none either).
Nesting follows `refs/zcode/DESIGN.md`'s Radius section: the first rounded
container in any tree starts at `rounded-xl`; nested rounded containers
step down `rounded-lg` → `rounded-md` → `rounded-sm` (the floor). Basic
controls (buttons, inputs, selects, menu triggers) default to `rounded-lg`
regardless of size — size and emphasis never independently change radius.
Menu/dropdown/context-menu/select-popover shells use `rounded-lg`; their
items use `rounded-md`; anything nested inside an item uses `rounded-sm`.
Dialogs use `rounded-2xl`. Three more approved `rounded-2xl` exceptions:
the main composer input shell, the toast shell, and (per ZCode) a
conversation-status floating panel smind has no equivalent of yet.
`rounded-full` is reserved for actual pills/circles.

**Elevation vocabulary** (ZCode's four recipes, `refs/zcode/DESIGN.md`'s
Elevation and Depth section):

| Elevation | Shadow | Canonical consumers |
| --- | --- | --- |
| **Base** | none | page background, pane content areas |
| **Surface** | none (border-led) | cards, inline blocks |
| **Overlay** | `shadow-md` | menus, popovers, dialogs, sheets |
| **Attention** | `shadow-lg` | toast, the pending-permission card, rare emphasized panels |

The underlying `shadow-sm`/`-md`/`-lg` box-shadow recipes are still
smind's own theme-asymmetric values (`--elevation-shadow-sm/md/lg` in
`index.css` — light mode very soft, alpha 0.04–0.08; dark mode harder,
alpha 0.24–0.40, so a near-black surface still reads depth) — ZCode's
`DESIGN.md` doesn't prescribe exact shadow recipes, only usage tiers, so
these weren't replaced, just re-pointed at the right components (P1 Step
5 moved `dialog.tsx`/`sheet.tsx` from `shadow-lg` to `shadow-md`, and
`toast.tsx` from `shadow-md` to `shadow-lg`, to match the table above).
Stacking more than one shadow tier on a surface is a Forbidden-list item.

**Motion** has three durations: `--duration-hover` 150ms (hover/active
feedback, `StatusBadge`'s color transitions), `--duration-menu` 200ms
(Radix popover/dropdown/dialog open-close — `dialog.tsx`, `sheet.tsx`,
`dropdown-menu.tsx` read it via `duration-(--duration-menu)`; Radix's own
transform-origin machinery is untouched, no second mechanism added), and
`--duration-panel` 300ms (panel-level transitions), matching ZCode's
"keep transitions fast and low-drama" motion guidance. A transition
outside these three durations is a review flag. State-change animation is
for agent moments (queued→running badge color, a permission card entering
the foreground) — not decorative entrance animation on every mount.

## 14. Forbidden

- **Hardcoded colors** — hex, oklch, or Tailwind palette utilities
  (`bg-amber-500` etc.) outside `index.css`.
  `src/test/no-hardcoded-colors.test.ts` fails the build on this.
- **Tailwind's built-in text-size utilities, arbitrary `text-[…px]`
  sizes, or inline `font-size`** in application UI — `text-ui-*` (§12)
  is the one legal spelling. `src/test/text-ui-scale.test.ts` fails the
  build on this.
- **More than one elevation tier of shadow per surface** (§13) — no
  stacked shadows, no hand-rolled `shadow-[...]` literals; use the
  tiered `shadow-sm/md/lg` utilities the `@theme` entries provide.
- **Color-only disabled state.** Disabled is `disabled:opacity-50` on the
  outer element — a disabled control is the same control, dimmer, not a
  recolored one.
- **Ad-hoc pane-header padding outside `PaneHeader`.** The header bar's
  `px-4 py-2.5` lives in one component; a pane hand-rolling its own
  header div is a regression.
- **Arbitrary Tailwind spacing values** outside the documented scale
  (§4) — `p-[13px]`, `gap-[10px]` are review flags; the numeric scale is
  the vocabulary.
- **Arbitrary radius values, or the ambiguous bare `rounded` utility**
  (§13) — `rounded-full` is reserved for actual pills/circles, per ZCode's
  Radius rules.
- **A second font family.** One interface stack, one mono stack — no
  display or brand typeface.
- **ZCode/Zai branding** — no logos, icons, or names anywhere in smind's
  UI, code identifiers, or comments beyond the license-required
  attribution (`NOTICE`). smind's own sidebar wordmark is the only mark;
  see §1's Brand colour note.

## 15. Canonical surfaces by pattern

| Pattern | Reference |
| --- | --- |
| List + detail (sidebar tasks → task pane) | `components/app-sidebar.tsx`, `components/task-detail.tsx` |
| Form dialog (label + input fields, primary + cancel) | `components/crud-dialogs.tsx`, `components/accounts-dialog.tsx` |
| Destructive confirmation | `components/crud-dialogs.tsx`'s archive-task confirm |
| Sidebar list row | `components/app-sidebar.tsx` |
| Pane header | `components/ui/pane-header.tsx` |
| Composer / message input | `components/composer/composer.tsx` |
| Alert / banner | `components/ui/alert.tsx` |
| Empty state | `components/ui/empty-state.tsx` |
| Tool-call card | `components/timeline/tool-call-card.tsx` (registry: `components/timeline/tool-renderers.tsx`) |
| Permission card | `components/permission/permission-card.tsx` |
| Dialog / modal | `components/ui/dialog.tsx` |
| Toast | `components/ui/toast.tsx` |
| Status pill | `components/ui/status-badge.tsx` |
| Status dot | `components/ui/status-dot.tsx` |
| Find bar (floating, per pane) | `components/find/find-bar.tsx` — used by chat (`components/timeline/use-chat-find.ts`), the file editor (`components/file-editor-find-bar.tsx`), and the terminal (inline in `components/terminal-pane.tsx`) |
