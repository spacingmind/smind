# Icon audit — one registry, one size scale

Size: M (4) — scope 1 (web UI, ~45 files, mechanical), PRs 1, risk 0 (visual only), decisions 1 (glyph per concept, below), verification 2 (light/dark screenshots).

Human ask: "rà soát và đồng bộ các icon trong app 1 lần". Icons were added piecemeal across #297–#328; make one concept render as one glyph, one size per context, and make drift fail a test.

## Inventory

70 files import `lucide-react` (no other icon library). Non-lucide icons: provider brand marks (`composer/provider-glyph.tsx`, `account-usage/usage-source-icon.tsx`), the four desktop window-control SVGs (`desktop-window-controls.tsx`, `size-2.5`, native-chrome idiom, left alone), file-type glyphs (`lib/file-icons.tsx`, a domain of its own). Text glyphs: none used as icons (the `★` in `general-section.tsx` / `run-config-toolbar.tsx` is prose pointing at the `Star` icon; `…` and `·` are punctuation). Lucide v1.34 adds `aria-hidden="true"` to every icon that has no `aria-*` prop, so a missing explicit `aria-hidden` is not an a11y defect (it is redundant noise; left as is unless the line is touched).

### Concept → current icon(s) → files (before) → registry (after)

| Concept | Before | After (`Icons.*`) | Where |
|---|---|---|---|
| terminal | `SquareTerminal` (sidebar menu, bash tool) **vs** `Terminal` (header button, terminal tab) | `terminal` = `Terminal` (`>_`) — the Terminal tab was deliberately `>_` (app-header-and-tabs D8, `TB-AC4` asserts it), so the others follow it; ZCode/Paseo split ~50/50 | app-sidebar, tool-renderers, app-header, tab-registry |
| usage / plan usage | `CircleGauge` (footer, usage sidebar item), `Gauge` (chat pill, source fallback), `BarChart3` (settings section) | `usage` = `Gauge` | sidebar-footer, usage-sidebar-item, chat-usage-total, usage-source-icon, settings/usage-section |
| refresh vs retry vs reset | `RefreshCw` (settings usage), `RotateCw` (usage refresh ×2, task-setup retry), `RotateCcw` (run re-run, composer "reset to agent defaults") | `refresh` = `RefreshCw`, `retry` = `RotateCcw`, `reset` = `Undo2` | usage-refresh-button, usage-section, task-setup, run-timeline, run-config-toolbar |
| settings gear vs sliders | `Settings` (footer, usage options, model-picker manage), `Settings2` (General section header), `SlidersHorizontal` (run-options popover) | `settings` = `Settings` (app/provider settings), `options` = `SlidersHorizontal` (per-run options); `Settings2` removed | sidebar-footer, usage-options, model-picker-rows, general-section, run-config-options |
| keyboard shortcuts | header button used `CircleHelp` while Settings section uses `Keyboard` | `shortcuts` = `Keyboard` (header), `help` = `CircleQuestionMark` (footer help menu) | app-header, sidebar-footer, shortcuts-section |
| more menu | `MoreHorizontal` ×5 (deprecated alias of `Ellipsis`) | `more` = `Ellipsis` | app-header-task, app-sidebar, 3 settings sections |
| external link / open elsewhere | `SquareArrowOutUpRight` (sidebar "open in editor", explorer) | `externalLink` = `ExternalLink` (ZCode/Paseo) | app-sidebar, file-explorer-pane |
| edit | `Pencil` (most), `PenLine` (file editor toggle) | `edit` = `Pencil` | file-editor-pane, run-timeline, message actions, model-picker-rows |
| split | `Columns2` for left/right **and** up/down | `splitColumns` = `Columns2` (left/right), `splitRows` = `Rows2` (up/down) | App.tsx |
| space vs tool group | `Layers` used for both a sidebar space and a collapsed run of tool calls | `space` = `Layers`, `toolGroup` = `Boxes` | app-sidebar, tool-group-row |
| group-by toggle | `FolderTree` (the same glyph as the Files tab) / `ListChecks` | `groupByWorkspace` = `FolderGit2` (same as a workspace row), `groupByStatus` = `ListChecks`; `files` keeps `FolderTree` | app-sidebar |
| workspace | `FolderGit2` (sidebar, folder picker), `Folder` (header task path) | `workspace` = `FolderGit2`; plain `folder` stays for non-repo dirs/tree rows | app-sidebar, folder-picker-dialog, app-header-task |
| warning / error naming | `AlertTriangle` ×4 + `TriangleAlert` ×1; `AlertCircle` ×5 (deprecated aliases of `TriangleAlert` / `CircleAlert`, same glyph); a failed load drew `TriangleAlert` (model picker) and `AlertCircle` (sidebar, folder picker, explorer) | `warning` = `TriangleAlert`, `error` = `CircleAlert`; model-picker load failure → `error` | alert, toast, task-setup, permission-detail, model-picker-rows, app-sidebar, file-explorer-pane, folder-picker-dialog |
| success / info / spinner naming | `CheckCircle2`, `Loader2` ×7 (deprecated aliases) | `success` = `CircleCheck`, `spinner` = `LoaderCircle`, `info` = `Info` | alert, toast, inline-spinner, 5 more |
| chevrons | `ChevronRight` ×13, `ChevronDown`, `ChevronUp`, `*Icon` shadcn names | `chevronRight/Down/Up` | everywhere |
| check / close / add / copy / delete / archive / pin / search | consistent glyph already | `check`, `close`, `add`, `copy`, `delete`, `archive`, `pin`/`unpin`, `search` | — |
| provider brand mark | `provider-glyph.tsx` (real Claude/Codex marks, `Bot` fallback) **and** a second, hand-drawn Claude/OpenAI pair in `usage-source-icon.tsx` | `UsageSourceIcon` renders `ProviderGlyph` (`anthropic`→claude mark, `openai`→codex mark); one set of marks | usage-source-icon, provider-glyph |
| agent / model / thinking / profile | `Bot` (profiles, footer, glyph fallback), `Cpu` (model), `Brain` (thinking), `Star` (default profile) | `agent`, `model`, `thinking`, `defaultAgent` — unchanged, now named | run-config-*, timeline-row, settings/profiles |
| permission | `ShieldCheck` / `ShieldAlert` / `CheckCheck` | `permission`, `permissionDanger`, `checkAll` — unchanged | run-config-toolbar, timeline-row |
| diff view vs file-edit tool | `GitCompare` (tab, chips, explorer) / `FileDiff` (Edit tool) | `diff`, `fileEdit` — intentionally two concepts | tab-registry, tool-renderers |
| tokens / total / plan | `Coins` (run pill), `Sigma` (chat total), `Gauge` (plan) | `tokens`, `usageTotal`, `usage` — three scopes, kept distinct | run-usage-pill, chat-usage-total |
| host / connections | `Server` (footer host, daemon section), `Network` (connections) | `host`, `connections` — unchanged | sidebar-footer, settings |

ZCode / Paseo reference (counts of lucide imports, `refs/zcode/packages/ui/src`, `refs/paseo/packages/app/src`): usage Gauge/Activity (both); retry RefreshCw 22 / RotateCw 22; reset Undo2 (both); external ExternalLink (both); more Ellipsis / MoreHorizontal (both mix); terminal SquareTerminal/Terminal ~50/50 (both); settings Settings (both), Settings2 secondary; warning TriangleAlert/CircleAlert; success CircleCheck; spinner Loader2/LoaderCircle; edit Pencil; shield Shield*; thinking Brain/Sparkles; agent Bot; workspace Paseo `FolderGit2`/`Layers`, ZCode `Workflow`.

### Size / stroke / colour findings

- Size is mostly applied by the container (`Button` svg defaults: default/icon-sm 16px, sm 14px, xs/icon-xs 12px; sidebar & dropdown items 16px). Findings:
  - **Context menu items are 14px, dropdown menu items 16px** (`ui/context-menu.tsx`) — same kind of control. → 16px.
  - **Menu-item check marks**: `size-4` (model picker, select) vs `size-3.5` (theme toggle, open-in-editor). → `md`.
  - **Disclosure chevrons**: `size-3` (timeline, task-setup, sidebar group), `size-3.5` (accounts dialog ×2, file tree). → `xs` for dense rows/dialog sections; file tree keeps its 14px box (`sm`) to align with 14px folder glyph.
  - **Trailing chevrons**: model-picker `size-4 text-foreground-muted`, settings nav `size-4 text-foreground-subtle`. → both `text-foreground-subtle`.
  - Sidebar Pin/Layers group icons `size-3.5` inside 16px sidebar buttons — status-row scale, kept as `sm`.
  - Provider marks: `ProviderGlyph` takes a numeric `size` (default 16) while lucide uses classes; `UsageSourceIcon` hard-coded `size-4`. → both draw from `ICON_SIZE` (class) and `ProviderGlyph` accepts `className` sizing; numeric prop kept for call sites that pass it.
  - No `strokeWidth` overrides, no `h-4 w-4` pairs, no `absoluteStrokeWidth`; nothing to fix.
- Colour: leading concept icons `text-foreground-muted`, trailing affordances `text-foreground-subtle`; the model-picker trailing chevron and `Plus` were the muted/subtle outliers (fixed above).
- A11y: icon-only buttons all carry an `aria-label` except the shadcn `ui/sheet.tsx` / `ui/sidebar.tsx` triggers, which have `sr-only` text (verified when touched).

## Convention

`web/packages/ui/src/lib/icons.ts` is the only module (besides `lib/file-icons.tsx`, the file-type table) that imports `lucide-react`. It exports:

- `Icons` — semantic name → lucide component (canonical lucide names only, no deprecated aliases).
- `ICON_SIZE` — `xs` `size-3` / `sm` `size-3.5` / `md` `size-4` with the context table in its doc comment (xs: inline text, timeline markers, icon-xs buttons; sm: tab strip, tree rows, settings headers, status rows; md: sidebar, header, menus, pickers, dialogs).
- `IconComponent` type (replaces `LucideIcon` in props/records).

Components render `<Icons.settings />` (or `const Icon = Icons.x` when the glyph is chosen at runtime) and size through the container's svg defaults or `ICON_SIZE`.

## Acceptance criteria

- **AC1** No component or lib file outside `lib/icons.ts` and `lib/file-icons.tsx` imports `lucide-react` (tests excluded).
- **AC2** Each row of the table above renders the "After" glyph (registry inline snapshot is the contract).
- **AC3** Context-menu items, menu check marks, disclosure chevrons and trailing chevrons follow the size/colour rules above.
- **AC4** `UsageSourceIcon` and `ProviderGlyph` share one set of Claude/Codex marks and sizing.
- **AC5** Existing behaviour and `data-testid`s unchanged; `task test`, `task lint`, `bunx tsc -b packages/ui` green.
- **AC6** Before/after screenshots (light + dark; 1440×900, plus 390×844 for composer and sidebar) show only the intended icon swaps.

## Test scenarios

- `lib/icons.test.ts › binds each concept to the intended lucide glyph` (inline snapshot of `Icons`).
- `lib/icons.test.ts › is imported through, not around: no component imports lucide-react directly` (AC1).
- `lib/icons.test.ts › uses no deprecated lucide alias names in the allowed files`.
- Existing component tests (`provider-glyph`, `usage-source-icon`, `tab-registry`, `alert`, `toast`, `run-timeline`) keep passing; where a test asserts a lucide class (`lucide-*`), update to the new glyph.

## Decisions

- Glyph per concept: ZCode/Paseo choice where one exists, else the majority of the current code (table above). `Gauge` (not `Activity`/`BarChart3`) for usage; `Undo2` for reset; `RotateCcw` for retry; `RefreshCw` for refresh.
- Registry is a plain object of components (not a wrapper `<Icon name>` component): keeps tree-shaking, typing, and lets primitives take `IconComponent`.
- File-type glyphs stay in `lib/file-icons.tsx` (own domain; allowlisted). Desktop window-control SVGs stay (native-chrome idiom).
- `UsageSourceIcon` adopts `ProviderGlyph`'s real marks (the hand-drawn pair was a duplicate) — a visible change to the usage rows; trivially revertible.
- No ADR: no architecture choice (rule d).

## Progress

- [x] Inventory + convention + registry module + drift test (this commit)
- [ ] Peer A: sidebar / footer / usage items / account-usage
- [ ] Peer B: header / tabs / App.tsx / file panes / terminal / find / folder picker / open-in-editor
- [ ] Peer C: composer, model picker, run-config, timeline, tool rows, task-setup, run footer
- [ ] Peer D: settings, `ui/*` primitives, theme toggle, misc
- [ ] Screenshots after, review, PR

## Validation

(to fill in at the end: AC → evidence)
