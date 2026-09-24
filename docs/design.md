# smind web UI: design rules

This is the living reference for `web/packages/ui`'s token vocabulary,
density, primitives, copy rules, the keyboard/palette registration APIs,
and routing/persistence — written from what actually landed
(`docs/plans/active/ui-redesign-parity.md`'s Items 1-5, then the
visual-identity-console plan's Track A), not aspirational. It follows
Paseo's own `refs/paseo/docs/design.md` where the two overlap (Paseo is
the parity plan's north star), scoped down to what smind's surface
actually needs.

## Character

smind is a calm technical console: quiet, spacious, unhurried. Every
visual decision serves either *act on this* or *understand this* — never
*look at this*. Decoration is not a goal; the interface stays out of the
way while the user reads state and makes decisions.

Consistency comes from component reuse, not from hand-matching styles
across surfaces. When two surfaces do the same semantic thing in two
different ways, one of them is wrong — the shared primitive (§3) wins,
and the canonical-surfaces table (§15) says which file is the reference.

## 1. Token layers

`web/packages/ui/src/index.css` has two layers:

1. **Static scale** — the original shadcn oklch tokens (`background`,
   `foreground`, `card`, `popover`, `primary`, `secondary`, `muted`,
   `accent`, `destructive`, `border`, `input`, `ring`, `chart-1..5`,
   `sidebar*`). One value per name per theme (`:root` / `.dark`). Every
   primitive already composed from these correctly before Item 1 — this
   layer wasn't rewritten.
2. **Semantic layer** — named-by-purpose tokens added by Item 1, then
   extended by the visual-identity-console plan:
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
   - `diff-{addition,deletion}`: the diff tier — the +/- inside a diff
     view, deliberately separate from `status-success`/`status-danger`
     because line-scannable diff chrome wants more saturation than a
     status pill (Item 5; paseo's three-family model). diff2html's
     overrides read these; any future non-diff2html diff surface reuses
     them too.
   - **Type-role scale** (`--text-workspace-title`,
     `--text-section-title`, `--text-panel-title`,
     `--text-metadata-label`, `--text-code-annotation`,
     `--text-interface`, `--text-content`): the one legal spelling for
     each typographic job, with paired line-height (and, where the role
     fixes it, paired font-weight). See §12.
   - `--elevation-shadow-sm/md/lg` + `--duration-hover/menu/panel`:
     shadow tiers and motion durations, per §13.

`@theme inline` re-exports every semantic token as `--color-*` (and the
type-role/shadow entries as `--text-*`/`--shadow-*`), which is what
makes `bg-status-warning`, `text-foreground-muted`, `text-panel-title`,
`shadow-md` (tiered), `duration-(--duration-menu)`, etc. exist as
Tailwind utilities — Tailwind v4 is CSS-first here (no
`tailwind.config.js`), so anything named `--color-X`/`--text-X`/
`--shadow-X` in `@theme` becomes a utility for free.

**Rule:** no component hardcodes a hex, oklch, or Tailwind palette color
(`bg-amber-500`, `text-emerald-600`, etc.) — every color comes from a
token, so light/dark/future-theme all stay correct without touching
component code. `src/test/no-hardcoded-colors.test.ts` greps for this;
`src/test/token-presence.test.ts` asserts every semantic token exists in
both `:root` and `.dark`.

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
  (`--d2h-bg-color` etc.) to reference smind's own tokens (including the
  `diff-*` family for the +/- pairs) means `diff-viewer-pane.tsx` needs
  no dark-mode-specific class at all.
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
| `Button` (`buttonVariants`) | Six generic variants (`default`/`secondary`/`destructive`/`outline`/`ghost`/`link`) plus three additive semantic variants (visual-identity-console Item 4) with product meaning: `execute` — starts/stops a run; `approval` — the recommended/confirm action in a permission decision; `quiet` — a non-committal or structural action that shouldn't compete visually with the primary action nearby | `composer.tsx`'s Send/Stop pair (`execute`); `permission-option-button.tsx`'s recommended-allow option (`approval`); `crud-dialogs.tsx`'s shared `FormActions` Cancel button (`quiet`) |

A new recurring surface reuses one of these before inventing a new pattern
— that's the whole point of Item 2.

## 4. Density

One padding scale for pane headers: `px-4 py-2.5`, via `PaneHeader`. Before
Item 2, headers drifted between `px-3 py-2` (file-editor, terminal) and
`px-4 py-3` (task-detail, diff-viewer) with no reason for the split
(`docs/research/uiux-audit.md` §2.4) — adopting the shared component fixes
this as a side effect, not a separate pass over each pane.

**The spacing scale is Tailwind's default numeric scale — and nothing
else.** `1`=4px through `16`=64px (`1 2 3 4 6 8 12 16`, plus the
half-steps `0.5/1.5/2.5/3.5` Tailwind itself provides) is the one legal
spacing vocabulary; it is numerically identical to paseo's own `SPACING`
table, so this is a naming-and-enforcement statement, not a migration. An
arbitrary value (`p-[13px]`, `gap-[10px]`) is a Forbidden-list item
(§14): if the nearest scale step doesn't work, the layout is fighting the
rhythm, not missing a step.

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
Item 13's settings screen can embed the same list without a dialog around
it.

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
two callers; a pane-size preference (Item 6) is the next.

Hash routing (`lib/route.ts`) is the other piece of Item 3: the URL is a
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
sidebar-vs-content split and the side dock (Item 6) both stop being
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

See `docs/plans/active/ui-redesign-parity.md`'s Item 21 Decisions and
Validation for why this is two compact destinations (sidebar overlay,
task pane) rather than Paseo's three, and the full acceptance-criteria
trace.

## 11. Decisions

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
  **Superseded (2026-09-18):** smind now has an actual brand mark (the
  sidebar logo), so `--primary`/`--ring`/`--sidebar-primary`/
  `--sidebar-ring` were deliberately given the logo's own hue back —
  `oklch(0.489 0.08 194.8)` (light, ~#0d6e6e) / `oklch(0.904 0.136
  196.2)` (dark, ~#51fbfd, sampled directly from the logo's core glow).
  This is the one chromatic hue in the palette, confined to primary
  actions/focus rings/the active-sidebar-item token — matching the
  researched pattern other dev-tool brands use (Linear's lavender-blue,
  Raycast's red, Cursor's orange): a bold, glowing logo mark, but the
  same hue used *sparingly* in the product UI rather than spread across
  backgrounds or `--accent`/`--secondary`/`--muted` (those stay
  achromatic, unchanged). Everything else in the calm-technical-console
  thesis (§Character) still holds — this is one accent, not a repaint.
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
- **(visual-identity Track A) Type-role and shadow values are
  round-number pragmatic picks, not derived.** The `--text-*` roles
  tokenize what the components already rendered where possible
  (workspace-title keeps the wordmark's existing 14/600; panel-title
  tokenizes PaneHeader's existing 14/500); section-title unifies dialog
  (was 18/600) and sheet (16/500) titles at 16/500. Shadow alphas
  (light 0.04/0.06/0.08, dark 0.24/0.32/0.40) and durations
  (150/200/300ms) are paseo-asymmetry-inspired round values, not copied
  verbatim from paseo's tables.
- **(visual-identity Track A) `--text-interface` is a name, not a new
  ramp.** It documents the existing dense-UI base size (14px/20px); the
  sizes above and below it remain the stock Tailwind
  `text-xs`/`text-sm`/`text-base` utilities rather than a parallel set
  of invented token names. `--text-content` is the exception that earns
  its own token because it must *not* move when the interface-density
  setting rescales rem-based utilities (paseo's content-vs-interface
  split) — it is `calc(15px * var(--font-scale-content, 1))`,
  px-anchored on purpose.
- **(visual-identity Track A) Shadow tiers live in `@theme` as
  `--shadow-sm/md/lg`, overriding Tailwind's stock values.** Existing
  `shadow-md`/`shadow-lg` classes (dropdowns, dialogs, sheets, toasts)
  therefore resolve to the tiered tokens automatically with zero
  call-site churn; components must not hand-roll `shadow-[...]` literals
  (§14).
- **(visual-identity Track A) `resizable.tsx`'s hover-highlight timer
  was not repointed at `--duration-hover`** because the merged
  web-ui-fixes branch did not ship one — the handle is pure CSS with no
  JS timer, so there is nothing to point at a token. Revisit if a timer
  ever lands there.
- **(visual-identity Track B, Item 4) `execute`/`approval`/`quiet` reuse
  the existing `destructive` variant's tinted-background idiom** (10%
  alpha fill, saturated text, 20% on hover, doubled in dark mode) rather
  than inventing a new solid-fill-plus-contrast-text shape — `destructive`
  is the only precedent for a semantically colored button in this file,
  and matching it keeps every colored variant answering to the same
  recipe. `quiet` has no fill at all (`text-foreground-muted` only, no
  hover background) — deliberately lower-emphasis than `ghost`, which
  still gets a hover background.
- **(visual-identity Track B, Item 4) The composer's Send button is
  `execute` only while it would start a run; `default` once a run is
  already live**, because at that point the same button reads "Queue" —
  appending to the queue is not the run-control action, so it keeps the
  generic variant. The Stop button is always `execute`.
- **(visual-identity Track B, Item 7) The pending-permission card's
  structure was confirmed adequate, not rebuilt.** `PermissionCard`
  already dispatches to `OptionsCard`/`PlanReviewCard`/`QuestionFormCard`,
  each built on the shared `Alert` primitive (title/description, an
  optional detail block, an action row) — this matches dsh's
  `ApprovalPanel` shape from the parity-plan research (Item 11 there
  shipped it). This Item is a token/elevation pass only: the outer
  wrapper in `permission-card.tsx` (already the one element common to all
  three variants) gained `bg-surface-1 shadow-lg` for the "floating"
  recipe (§13); no new structure was added. The `approval` variant was
  adopted at `permission-option-button.tsx` (the default, most-common
  variant's recommended-allow option) — `PlanReviewCard`'s and
  `QuestionFormCard`'s own action buttons are unchanged, out of scope for
  this pass.

## 12. Typography — type roles and the 3-tier weight rule

Type roles (`index.css`'s `--text-*` entries; visual-identity-console
Item 2). Each row is the ONE legal spelling for that job, with its
canonical consumer:

| Role token | Size / line-height | Weight | Canonical consumer |
| --- | --- | --- | --- |
| `text-workspace-title` | 14px / 20px | 600 | `app-sidebar.tsx`'s wordmark — the **only** legal semibold in the app |
| `text-section-title` | 16px / 24px | 500 | `ui/dialog.tsx` + `ui/sheet.tsx` titles (modal/sheet headers) |
| `text-panel-title` | 14px / 20px | 500 | `ui/pane-header.tsx` |
| `text-metadata-label` | 12px / 16px | 500 | group headings (`command-palette.tsx`, `shortcuts-dialog.tsx`); form-field labels (`crud-dialogs.tsx`, `accounts-dialog.tsx`) |
| `text-code-annotation` | 12px / 16px | 500 | `file-status-marker.tsx` (pairs with `font-mono`) — dense git/terminal-style single-glyph annotations |
| `text-interface` | 14px / 20px | (inherits) | the dense-UI base size; the ramp above/below stays stock `text-xs`/`text-sm`/`text-base` |
| `text-content` | `calc(15px × --font-scale-content)` / 1.6 | (inherits) | `timeline/timeline-markdown.tsx` (message prose) — fixed 15px base so prose doesn't rescale with interface density |

**Weight has exactly three legal tiers** (paseo §3's rule):

1. **Workspace title** — `text-workspace-title` (600), nothing else.
2. **Structural labels** — `font-medium` (500): section/group headings,
   modal and dialog titles, form-field labels, dense metadata emphasis
   (the code-annotation glyph, a quick-open match highlight), toast/alert
   titles.
3. **Everything else** — `font-normal` (400): body text, row titles,
   button labels, badge text, sidebar list-item titles.

The condensed rule: text that *names* a surface or a group is medium;
text that *lives inside* one is normal. Markdown headings inside prose
(`timeline-markdown.tsx`'s h1-h3) are medium, not semibold — prose
emphasis comes from size and color, never a heavier hand.

## 13. Elevation, shadow, and motion

**Elevation vocabulary** (visual-identity-console Item 3) — five named
recipes on top of the raw `surface-N` tokens, so "floating" means the
same thing everywhere:

| Elevation | Surface | Shadow | Border | Canonical consumers |
| --- | --- | --- | --- | --- |
| **flat** | `surface-0` | none | none | the page background, pane content areas |
| **raised** | `surface-1` | none | `border` | cards, inline blocks, toasts sit on surface-1 with a hairline |
| **floating** | `surface-1`/popover | `shadow-lg` | `border` | dialogs, sheets, dropdown/select menus, the pending-permission card — anything detached over content |
| **focused** | (any) | none | none — `ring` | focus-visible states (`focus-visible:ring-ring/50`), never a shadow |
| **embedded** | `surface-2` | none | optional `border` | insets that recede: code blocks, wells, the composer input area |

A component uses the recipe's shadow tier or none — `shadow-sm` (the
remaining tier, the lightest hover lift on an already-raised surface) is
the only step between "no shadow" and "floating," and stacking more than
one shadow tier on a surface is a Forbidden-list item.

**Shadow tiers are theme-asymmetric on purpose** (paseo's model): light
mode uses very soft shadows (alpha 0.04/0.06/0.08) because a light
surface needs only a whisper of depth; dark mode uses harder ones (alpha
0.24/0.32/0.40) because a near-black surface doesn't read a soft shadow
at all. Never "fix" a dark shadow by copying the light value — the
asymmetry *is* the design.

**Motion** has three durations: `--duration-hover` 150ms (hover/active
feedback, `StatusBadge`'s color transitions), `--duration-menu` 200ms
(Radix popover/dropdown/dialog open-close — `dialog.tsx`, `sheet.tsx`,
`dropdown-menu.tsx` read it via `duration-(--duration-menu)`; Radix's own
transform-origin machinery is untouched, no second mechanism added), and
`--duration-panel` 300ms (panel-level transitions). A transition outside
these three durations is a review flag. State-change animation is for
agent moments (queued→running badge color, a permission card entering
the foreground) — not decorative entrance animation on every mount.

## 14. Forbidden

- **Hardcoded colors** — hex, oklch, or Tailwind palette utilities
  (`bg-amber-500` etc.) outside `index.css`.
  `src/test/no-hardcoded-colors.test.ts` fails the build on this.
- **More than one elevation tier of shadow per surface** (§13) — no
  stacked shadows, no hand-rolled `shadow-[...]` literals; use the
  tiered `shadow-sm/md/lg` utilities the `@theme` entries provide.
- **Color-only disabled state.** Disabled is `disabled:opacity-50` on the
  outer element (paseo §11) — a disabled control is the same control,
  dimmer, not a recolored one.
- **Ad-hoc pane-header padding outside `PaneHeader`.** The header bar's
  `px-4 py-2.5` lives in one component; a pane hand-rolling its own
  header div is a regression (uiux-audit §2.4's original finding).
- **Arbitrary Tailwind spacing values** outside the documented scale
  (§4) — `p-[13px]`, `gap-[10px]` are review flags; the numeric scale is
  the vocabulary.
- **`font-medium`/`font-semibold` on body text, row titles, button
  labels, or badge text.** Weight's three legal tiers are §12's; the
  wordmark is the only semibold in the app, and medium is reserved for
  structural labels. `font-bold` does not exist in this codebase.
- **A second font family.** One interface stack, one mono stack,
  differentiated by weight role and the content/interface split — no
  display or brand typeface (visual-identity Decisions).

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
