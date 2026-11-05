# Gap matrix: smind vs a reference workbench / a reference agent harness / a reference proxy project

Verdicts are about **smind today**:

- **has** — present and roughly at reference quality.
- **partial** — present but materially thinner than the north star.
- **missing** — not present at all.
- **n/a** — the reference capability does not apply to smind's architecture
  (see `audit-workbench.md` §10 for the honest list).

"North star" names which reference smind should follow for that dimension.
A reference workbench wins ties by default (`docs/plans/active/smind-dogfood.md`: a reference workbench is
the product being replaced).

Sources: `audit-workbench.md`, `audit-agent-harness.md`,
`audit-proxy-project.md`, `audit-smind-current.md`.

---

## A. Shell, navigation, addressability

| Aspect | A reference workbench | A reference agent harness | A reference proxy project | smind | Verdict | North star |
| --- | --- | --- | --- | --- | --- | --- |
| Routing / deep links | Expo Router; `/h/:server/workspace/:id`, `/agent/:id`, `/settings/:section` | SPA, session-addressed | n/a | hash routing, `#/workspace/:id/task/:id/:tabKind[/:path]` (`lib/route.ts`), reload/back-forward restore workspace+task+tab | **has** | A reference workbench |
| Reload survives state | route + persisted layout | transient by design (own limitation) | n/a | route restores task+tab; open tabs, sidebar width and side-pane width all persist across reload (`use-task-tabs.ts`, `use-sidebar-width.ts`, `use-side-pane-width.ts`) | **has** | A reference workbench |
| Object model depth | Project → Workspace → Agent session, + Host | Workspace → Session | n/a | Workspace → Space → Task → Run | **partial** (no project/host layer; Space ≈ nothing in refs) | A reference workbench, adapted |
| Pane splits | full split tree, focus/move/close | fixed 3 columns | n/a | one split — primary + optional side pane, resizable and persisted per task (`App.tsx`'s `moveTab`) | **has** | A reference workbench |
| Side dock / "open to side" | Explorer dock + side pane + placement intents | `details` column | n/a | file/diff/terminal tabs movable to the side pane; explicit "open to side" (`pane`) vs implicit click (`prefer`), never yanking a user-placed tab | **has** | A reference workbench |
| Tab model | 14 kinds, host-aware registry, reorder, drag | slot-registered views | n/a | 5 kinds, data-driven registry, multiple terminal-tab instances (Item 20), still no reorder/drag | **partial** | A reference workbench |
| Tab persistence | per-workspace persisted layout | transient | n/a | per-task open tabs + active tab persist in `localStorage`, capped at 50 tasks | **has** | A reference workbench |
| Command palette | `Cmd+K` command center, scoped contributions | `/` + `@` in composer | n/a | `Cmd+K` opens a searchable palette; contributions (tasks, workspaces, actions, changed files, theme) register through `useCommands`, none hardcoded in the view | **has** | A reference workbench |
| Settings screen | full list+detail, ~12 sections | modal panel, ledger of sections | full control panel | list+detail shell (`components/settings/`), sections self-register; 2 sections shipped (Appearance, General) — accounts stay a separate dialog, no daemon settings sync | **partial** | A reference workbench |

## B. Theming and appearance

| Aspect | A reference workbench | A reference agent harness | A reference proxy project | smind | Verdict | North star |
| --- | --- | --- | --- | --- | --- | --- |
| Dark mode | 8 themes + auto, `Cmd+Alt+T` | light/dark/system, host-backed | unknown | `light \| dark \| system` via `hooks/use-theme.tsx`, persisted, applies the `dark` class, live-reacts to OS changes on `system`, `Cmd+Alt+T` cycles it | **has** | A reference workbench |
| No-flash theme boot | n/a (native) | synchronous `<body>` bootstrap | n/a | dependency-free pre-paint `<script>` in `index.html` sets the class before `main.tsx` mounts | **has** | A reference agent harness |
| Semantic token layering | static palette → semantic tokens, documented | `--dsw-*` static scale → alias layer | n/a | two-layer set (`surface-0..3`, `foreground-muted`, `status-*`, `status-dot-*`), documented in `docs/design.md` §1–2; stray chromatic `--sidebar-primary` resolved | **has** | A reference agent harness (layering) + a reference workbench (vocabulary) |
| Status colour family | `status*` + separate higher-chroma `statusDot*` family, generated | tone helpers | status strings | `status-{success,danger,warning,running}` (pills) + a separate, higher-chroma `status-dot-*` family, both generated tokens; adopted by `StatusDot`/`StatusBadge` | **has** | A reference workbench |
| Typography scale settings | interface / content / code sizes, UI+mono font pickers | — | — | interface/content/code font-size split persisted (`lib/settings-preferences.ts`); only interface size drives a real effect (root font size) — content/code are plumbed but unconsumed; no font-family pickers | **partial** | A reference workbench |
| Syntax theme | selectable, shared by code/diff/editor | shiki sheet | — | chrome (background/foreground/cursor) now follows app tokens for all three, but syntax *highlighting* colors are still 3 unharmonized sources (CodeMirror, diff2html, highlight.js), no selectable syntax theme | **partial** | A reference workbench |
| Terminal palette tied to theme | yes (ANSI tokens per theme) | — | — | chrome (background/foreground/cursor/selection) re-applies on theme change (`lib/terminal-theme.ts`); ANSI colors 0–15 are still xterm's own defaults, deliberately deferred | **partial** | A reference workbench |
| Written design law | `docs/design.md`, 15 sections + forbidden list | per-package READMEs | — | `docs/design.md`, 10 sections (tokens, theming, primitives, density, copy, state, keyboard actions, palette contributions, persistence, decisions); no dedicated forbidden-list section | **has** | A reference workbench |
| Density / rhythm discipline | explicit rules + canonical surfaces table | — | — | one padding scale via the shared `PaneHeader` (adopted by task-detail/file-editor/diff-viewer/terminal panes); `docs/design.md` §4 states the density rule | **has** | A reference workbench |

## C. Sidebar, workspace/task list, attention

| Aspect | A reference workbench | A reference agent harness | A reference proxy project | smind | Verdict | North star |
| --- | --- | --- | --- | --- | --- | --- |
| Grouped list | projects → workspaces, pin, reorder, filter | workspaces → sessions, 5-then-Show-more | n/a | workspace → space → task | **partial** | A reference workbench |
| Search / filter | project filter, host filter, search | collapsed search expanding across the header | n/a | collapsed search field; a non-blank query replaces the tree with a flat result list matching task/container name, following a reference agent harness's interaction exactly (`lib/workspace-tree.ts`) | **has** | A reference agent harness (the interaction) |
| Row metadata | branch, project, host, PR state, labels, services health, CI checks, diff stat — each toggleable | status dot | n/a | run-status dot + branch + diff stat (`task.stats` RPC, `hooks/use-task-stats.ts`); still no PR state, labels, services health or CI checks | **partial** | A reference workbench |
| Reorder / drag | yes, host-durable | yes, Manual vs Last-updated orders | n/a | none | **missing** | A reference workbench |
| Rename / labels | rename, colored labels, manager modal | rename | n/a | none | **missing** | A reference workbench |
| Attention reasons | `finished / error / permission` + viewed-timeline model | unread marking (local) | n/a | same three reasons, each its own `StatusDot` variant + accessible name (`lib/sidebar-signal.ts`) — no longer one shared dot | **has** | A reference workbench |
| Aggregate workspace status | per-`workspaceId` bucket | — | n/a | `aggregateStatus` derived from a workspace/space's own tasks, same precedence rule (error > permission > finished) | **has** | A reference workbench |
| Cross-client list freshness | daemon events for everything | host events | n/a | 11-topic daemon lifecycle events (ADR 0009); a task created in another tab (or by the CLI) splices into the tree without a refetch | **has** | A reference workbench |
| Sidebar callouts / alerts | `SidebarCallout` + `<Alert>` + toasts | Toast atom | — | `Alert`/`Toast` primitives now exist and are used elsewhere in the app, but the sidebar's own connection-error and welcome copy is still inline `<p>`/a custom `StatusRow` | **missing** | A reference workbench |

## D. Agent timeline

| Aspect | A reference workbench | A reference agent harness | A reference proxy project | smind | Verdict | North star |
| --- | --- | --- | --- | --- | --- | --- |
| Timeline item model | 7 typed kinds (`user_message`, `assistant_message`, `thought`, `tool_call`, `todo_list`, `activity_log`, `compaction`) | open registry of chat nodes | n/a | 5 typed kinds (`assistant`, `user`, `thinking`, `tool_call`, `unknown`/`raw` fallback) via ADR 0008's wire schema (`internal/taskrunner/event.go`); still no `todo_list`/`activity_log`/`compaction` | **partial** | A reference workbench for the vocabulary, a reference agent harness for the registry shape |
| Tool-call rendering | per-tool cards, icons, summary, detail sheet, detail-level setting, click-to-file | keyed `tool.call.toolview` by wire tool name + 5 render intents + generic fallback | n/a | registry keyed by wire tool name + generic fallback (`components/timeline/tool-renderers.tsx`), shape-based classification into terminal/read/edit/search/fetch, lifecycle merge-by-id, click-through to file, `detailed \| overview` grouping | **has** | A reference agent harness (registry) over a reference workbench's data |
| Markdown in transcript | yes, with its own content font size | `MarkdownText` primitive | n/a | `TimelineMarkdown` (react-markdown + remark-gfm); the content-font-size setting exists (Item 13) but isn't wired to it yet | **partial** | A reference workbench |
| Reasoning / thinking blocks | `thought` items, `autoExpandReasoning` setting | declared reasoning | n/a | `thinking` event type, rendered collapsed by default | **has** | A reference workbench |
| Turn model + footer | boundaries, elapsed timer, copy, fork | turn boundaries, thick rules | n/a | turns with an elapsed-time footer and a copy action; no fork | **partial** | A reference workbench |
| Scroll / tail behaviour | bottom-anchor controller, virtualization, text-reveal smoothing, pagination | streaming tail isolation, virtualized ledger, reserved gutter | n/a | auto-follow to the tail, releases on scroll-up with a "Jump to latest" affordance (`use-auto-follow.ts`); no virtualization, pagination or text-reveal smoothing | **partial** | A reference workbench |
| Token / context usage | context-window meter, provider usage cards | trajectory inspector per record | `api-key-usage` | none | **missing** | A reference workbench |
| Fork / rewind / retry | all three | branch action | n/a | none | **missing** | A reference workbench |
| Subagents | full lifecycle + track + provider subagents | lineage breadcrumbs + catalog | n/a | none | **missing** (and smind has no subagent concept at all) | A reference workbench — but defer |
| Chat outline / jump | hover rail outline | trajectory overview strip | n/a | none | **missing** | A reference workbench |
| Background jobs | — | header-action popover, live elapsed | usage queue | none | **missing** | A reference agent harness |

## E. Composer

| Aspect | A reference workbench | A reference agent harness | A reference proxy project | smind | Verdict | North star |
| --- | --- | --- | --- | --- | --- | --- |
| Multiline input | autogrow, capped, Shift+Enter | capped + wheel-chaining + IME-safe | n/a | autogrowing textarea, Enter to send / Shift+Enter for a newline, capped then scrolling, IME-safe (`components/composer/prompt-textarea.tsx`) | **has** | A reference workbench |
| Provider / model / mode controls | provider, model+effort, mode, thinking, features | model+effort seat, plan chip, access-mode chip | n/a | labelled provider + approval-policy `<select>`s; no model control — `provider.list` reports no models and `run.start` takes none — and no thinking/features controls | **partial** | A reference workbench |
| Send behaviour | `interrupt / steer / queue` setting | queue rows in the input dock | n/a | submitting while a run is live queues (sends when it ends); no interrupt-and-replace ("steer") — the daemon has no add-input-to-a-live-run RPC | **partial** | A reference workbench |
| Stop / interrupt | stop button + `Escape` | — | n/a | Stop moved into the composer and bound to `Escape` via the keyboard registry (removed from the run card entirely) | **has** | A reference workbench |
| Attachments | files, images, issues/PRs, paste, drop | attachments package | n/a | none | **missing** | A reference workbench |
| `@file` references | yes | atomic inline chips, directory descent, quoted paths | n/a | none | **missing** | A reference agent harness (the chip behaviour) |
| Slash commands / skills | yes | 3-kind dispatch + `decorate()` | n/a | none | **missing** | A reference agent harness |
| Draft persistence | survives workspace switch, mirrored to store | InputHub carries drafts | n/a | per-task draft persisted to `localStorage`, survives a task switch and a real unmount/remount (`use-composer-draft.ts`) | **has** | A reference workbench |
| Composer blocks / disabled reason | — | typed blocks with localized reason as placeholder | n/a | placeholder states the reason: no connection, no task selected, or "queue a follow-up" while a run is live (a live run no longer disables the composer at all) | **has** | A reference agent harness |
| Empty/hero state | New workspace hero with full controls | dashed composer card = workspace picker, identity preserved | n/a | "Select a task to get started." — still a dead-end sentence, not yet a "create a task here" entry point | **partial** | A reference agent harness |

## F. Permissions and human-in-the-loop

| Aspect | A reference workbench | A reference agent harness | A reference proxy project | smind | Verdict | North star |
| --- | --- | --- | --- | --- | --- | --- |
| Placement | pinned list header above the composer | **takes over the composer** | n/a | pinned dock above the composer | **has** | — |
| Cross-connection resolve | yes | yes | n/a | **yes** | **has** | — |
| Option semantics | provider options, plan gets "Implement" | one-shot Refuse/Allow | n/a | options styled by ACP `kind`: `reject_*` always reads destructive, the first `allow_*` reads primary, the rest `outline` | **has** | A reference workbench |
| What's being asked | command line, diff, tool detail | reconstructed command line from the running call's args | n/a | `summary` string only — unchanged; the daemon's `PermissionDecider` has no producer for a command line or diff yet | **partial** | A reference agent harness |
| Structured question form | multi-question, multi-select, other, skip | same, plus progress nav and IME handling | n/a | UI renders single/multi-select, free-text "other" and "skip" when a request carries that shape (`question-form-card.tsx`), but **no wire producer exists on either provider path** — the daemon never sends a `questions` shape today | **partial** | A reference agent harness |
| Plan review | Chat about it / Refuse / Approve | same, as a presentation intent | n/a | UI renders the plan as markdown with Chat/Refuse/Approve when a request carries that shape (`plan-review-card.tsx`), but **no wire producer exists yet**, same gap as the question form | **partial** | a reference workbench or harness (identical) |
| Policy presets | per-agent mode + settings default | per-session `/permission` + future-session default with risk ack | n/a | per-run `manual / auto-safe` select | **partial** | A reference agent harness (the two-lifetime split) |
| OS notification | desktop + real push | — | n/a | web Notification, edge-triggered; the on/off preference moved into Settings → General (Item 13) | **partial** (push is **n/a**) | A reference workbench |

## G. Files, diffs, review, terminal

| Aspect | A reference workbench | A reference agent harness | A reference proxy project | smind | Verdict | North star |
| --- | --- | --- | --- | --- | --- | --- |
| File tree | icons, context actions, git decoration, collapse | directory picker | n/a | file-type icons, git A/M/D decoration with directory roll-up (`lib/file-icons.tsx`, `use-task-file-status.ts`), context menu (reveal in diff, copy path); still no "open to side" from the tree, no rename | **partial** | A reference workbench |
| Editor | CodeMirror + preview + conflict handling + vim | — | n/a | **CodeMirror + preview + `expectedMtime` conflict** | **has** | — |
| File search | `Cmd+P` + command-center file search | — | n/a | `Cmd/Ctrl+P` quick-open, fuzzy-matches `task.searchIndex` (a full `git ls-files` walk), opens the match as a file tab | **has** | A reference workbench |
| Changed-file review | changes tree + working diff + per-file | `DiffBlock` | n/a | per-file list **and** a whole-diff view with a unified/side-by-side toggle, expand, stage, viewed, commit, PR | **has** | — |
| Line comments / review drafts | persisted per checkout, routed to agent | — | n/a | per-line draft comments, persisted per task (`lib/review-drafts.ts`), survive a collapse/tab-switch, submitted as one prompt | **has** | A reference workbench |
| Commit history / branch ops | commits section, branch switcher, push, PR, merge-from-base | — | n/a | commit + create PR only | **partial** | A reference workbench |
| Terminal | multi, profiles, activity, scrollback, links, split | pwsh terminal | n/a | multiple terminals per task (each its own tab), activity indicator on an inactive tab, scrollback-size setting, copy/paste; still no profiles or clickable links | **partial** | A reference workbench |

## H. Settings, accounts, providers

| Aspect | A reference workbench | A reference agent harness | A reference proxy project | smind | Verdict | North star |
| --- | --- | --- | --- | --- | --- | --- |
| Settings shell | list+detail, ~12 sections, feature-owned | modal panel, section ledger | full panel | list+detail shell (`components/settings/settings-screen.tsx`), sections self-register via `registerSettingsSection`; 2 sections shipped (Appearance, General) — accounts stay a separate dialog, not folded in | **partial** | A reference workbench |
| Appearance settings | theme, 3 font sizes, 2 font families, syntax theme | light/dark/system | — | theme + interface/content/code font-size split persisted; only interface size is wired to a real effect (root font size); no font-family pickers, no syntax theme | **partial** | A reference workbench |
| Layout preferences | per-surface open location | — | — | none | **missing** | A reference workbench |
| Shortcut customization | every binding rebindable + search | — | — | every binding rebindable (per-row reset, reset-all, conflict warning), but reachable via the `Shift+?` shortcuts dialog rather than Settings, and not searchable | **partial** | A reference workbench |
| Account row richness | provider row + auth state + diagnostics sheet | models settings | **~20 fields incl. status_message, next_retry_after, success/failed, recent_requests, quota, priority, weight, note** | presence + health dot — unchanged; `store.Account` still has no `status`/`status_message`/`next_retry_after`/counters, which needs a wire change (AGENTS.md rule (d)) | **partial** | **A reference proxy project** |
| OAuth flow | provider-specific | — | start → URL → poll status → **cancel** | start → URL → **cancel** now shipped (aborts the in-flight `account.oauthStart` by request id); still no separate poll-status step | **partial** | A reference proxy project |
| Multiple accounts per provider | yes | — | yes (`auth_index`) | no | **missing** | A reference proxy project |
| Quota / usage visibility | balance bar + window bar + cards + tooltip | — | usage endpoints | none — `internal/quota`'s poller is wired to a `noopQuotaFetcher` that always reports zero, so no UI was built rather than shipping invented numbers | **missing** | A reference workbench (presentation) over a reference proxy project (data) |
| Error-log / request-trace access | diagnostics sheet | — | error-log list + per-request trace | none | **missing** | A reference proxy project |
| Config escape hatch | project settings editor | settings.yaml via host API | structured rows + raw YAML | none | **missing** | A reference proxy project |

## I. Keyboard

| Aspect | A reference workbench | A reference agent harness | A reference proxy project | smind | Verdict | North star |
| --- | --- | --- | --- | --- | --- | --- |
| Shortcut system | 1.7k-line data table, rebindable, searchable, 5 sections, help dialog | per-surface focus contracts | — | data-driven registry (`keyboard/shortcuts.ts`), 10 bindings across 4 sections, rebindable with per-row/reset-all and conflict detection, `Shift+?` help dialog — smaller table, and not searchable | **partial** | A reference workbench |
| Command palette | `Cmd+K` | — | — | `Cmd+K` opens a searchable palette with registered contributions | **has** | A reference workbench |
| Focus discipline | scoped handlers, focus-scope | Escape returns focus to trigger; list closes before control unmounts | — | scoped handlers + `keyboard/focus-scope.ts` (terminal > editable > modal precedence); tab-close `×` is now keyboard-operable (`tabIndex` + Enter/Space) | **has** | A reference agent harness |

## J. Responsive / mobile

| Aspect | A reference workbench | A reference agent harness | A reference proxy project | smind | Verdict | North star |
| --- | --- | --- | --- | --- | --- | --- |
| Compact-first discipline | yes, one `useIsCompactFormFactor()` branch per screen | concession chain, 56px rail | — | one 768px breakpoint, used by the shadcn sidebar only | **missing** | A reference workbench |
| Compact panel model | 3 mutually exclusive destinations, one position value, gesture arbitration | rail + auto-closing details | — | none | **missing** | A reference workbench (model), CSS not gestures |
| Native app | iOS/Android | — | — | — | **n/a** | — |

## K. Empty / loading / error states

| Aspect | A reference workbench | A reference agent harness | A reference proxy project | smind | Verdict | North star |
| --- | --- | --- | --- | --- | --- | --- |
| Written rules | `design.md` §11, prescriptive | per-package | — | `docs/design.md` §5 (copy rules) + §6 (state rules) exist; less exhaustive than a reference workbench's dedicated section | **partial** | A reference workbench |
| Loading | inline by default, page-level centred, card-level one line | explicit loading rows | — | shared `InlineSpinner` primitive + present-participle in-flight labels adopted across Save/Reload/Overwrite/Commit/Stage/quick-open/OAuth; a few panes still ad-hoc | **partial** | A reference workbench |
| Empty | short noun phrase, ≤1 ghost button | "No sessions yet" | — | shared `EmptyState` primitive, sentence-case no-period copy, adopted across task-detail, diff-viewer, file-explorer, sidebar and quick-open | **has** | A reference workbench |
| Errors | inline / `<Alert>` / imperative, one per region | inline per region | — | shared `Alert` primitive (`default \| info \| success \| warning \| error`, correct ARIA roles) adopted across diff-viewer, terminal, file-editor, task-detail and the permission cards; the sidebar's own connection banner is still a bare `<p>` | **has** | A reference workbench |
| Layout stability | "changing state must not move the layout" — reserve space | reserved gutters, reserved overflow width | — | reserved-space rule applied to the sidebar row (attention/run-status/aggregate slots, the meta line) and the run entry, per Item 2/12's own criteria; not applied as a blanket rule elsewhere yet | **partial** | A reference workbench |
| In-flight feedback | present-participle labels, pending menu items | pending states | — | Save/Reload/Overwrite/Commit/Stage all have present-participle in-flight labels and disabled states now; no "pending menu items" convention | **has** | A reference workbench |

---

## Top 10 gaps, ranked by daily-driver (dogfood) impact

**Re-audited 2026-09-15.** The original ten below (written in Phase 1,
before any implementation) are now mostly closed: structured timeline
+ tool-call cards, dark mode, the keyboard layer + command palette,
a multiline composer, routing/persistence, a settings screen, sidebar
signal, and cross-client staleness (items 1–8) all landed across
Items 1–20 — see the dimension tables above and
`docs/plans/active/ui-redesign-parity.md`'s Validation section for what
each actually shipped and what it deliberately left out. Item 9 (account
rows) is still genuinely thin. What follows is the current top-10,
ranked the same way, not the original list left standing.

1. **Permission requests carry no detail beyond a summary string, and two
   whole variants (question form, plan review) have UI with no wire
   producer.** The daemon's `PermissionDecider` has never been taught to
   emit a command line, a diff, or either shape — a Track B item shipped
   the rendering ahead of a producer, deliberately, but the gap is real
   until a daemon change lands (AGENTS.md rule (d)) (F).
2. **Account rows are still thin, and quota is entirely unimplemented.**
   `store.Account` has no `status_message`/`next_retry_after`/counters,
   and `internal/quota`'s fetcher always reports zero — both need a wire
   change before either UI can be more than a scoping note. The dogfood
   log's recurring "why did this run die" question still lives here (H).
3. **No responsive/compact layout at all (Item 21).** One 768px
   breakpoint used only by the sidebar; the composer, timeline and
   permission card are all desktop-only, which is what
   `docs/plans/active/relay-e2ee-mobile.md` needs fixed to be useful (J).
4. **No context/token usage meter, no fork/rewind/retry, no chat outline,
   no background-jobs affordance.** The timeline is structured now, but
   these four a reference workbench or harness capabilities have no smind equivalent yet (D).
5. **Composer has no model selector, no attachments, no `@file`
   references, no slash commands.** `provider.list` reports no models and
   `run.start` takes none, so the model gap is wire-gated like items 1–2;
   the rest are simply unbuilt (E).
6. **Sidebar rows can't be reordered, renamed, or labelled, and carry no
   PR/CI/services-health signal** — branch and diff stat landed, but the
   richer row metadata and manual ordering a reference workbench has did not (C).
7. **Settings is real but thin: two sections, no layout preferences, no
   config escape hatch, and accounts are still a separate dialog** rather
   than a registered section (H).
8. **Syntax highlighting and the terminal's ANSI palette still don't
   follow the app theme** — only UI chrome (backgrounds/borders/cursors)
   was unified in Item 1; CodeMirror/diff2html/highlight.js remain three
   separate syntax-color sources and xterm's ANSI 0–15 are still its own
   defaults (B).
9. **The file tree has no "open to side" and no rename**, even though
   Item 6 (splits) landed after Item 17 shipped the tree — the two were
   never reconciled (G).
10. **The sidebar's own error/callout copy never adopted the `Alert`/
    `Toast` primitives** Item 2 shipped for the rest of the app — it is
    still a bare `<p>`/custom `StatusRow` (C).

Honourable mentions just below the line: no fork on the timeline's turn
footer; no font-family or syntax-theme pickers in Appearance settings;
no drag-reorder on tabs; OAuth has cancel but still no separate
poll-status step.
