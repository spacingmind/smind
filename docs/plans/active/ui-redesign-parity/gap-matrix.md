# Gap matrix: smind vs paseo / deepseek-harness / cliproxyapi

Verdicts are about **smind today**:

- **has** — present and roughly at reference quality.
- **partial** — present but materially thinner than the north star.
- **missing** — not present at all.
- **n/a** — the reference capability does not apply to smind's architecture
  (see `audit-paseo.md` §10 for the honest list).

"North star" names which reference smind should follow for that dimension.
Paseo wins ties by default (`docs/plans/active/smind-dogfood.md`: Paseo is
the product being replaced).

Sources: `audit-paseo.md`, `audit-deepseek-harness.md`,
`audit-cliproxyapi.md`, `audit-smind-current.md`.

---

## A. Shell, navigation, addressability

| Aspect | Paseo | dsh | cliproxyapi | smind | Verdict | North star |
| --- | --- | --- | --- | --- | --- | --- |
| Routing / deep links | Expo Router; `/h/:server/workspace/:id`, `/agent/:id`, `/settings/:section` | SPA, session-addressed | n/a | none — one `useState` selection | **missing** | Paseo |
| Reload survives state | route + persisted layout | transient by design (own limitation) | n/a | loses task + all tabs | **missing** | Paseo |
| Object model depth | Project → Workspace → Agent session, + Host | Workspace → Session | n/a | Workspace → Space → Task → Run | **partial** (no project/host layer; Space ≈ nothing in refs) | Paseo, adapted |
| Pane splits | full split tree, focus/move/close | fixed 3 columns | n/a | none | **missing** | Paseo |
| Side dock / "open to side" | Explorer dock + side pane + placement intents | `details` column | n/a | none | **missing** | Paseo |
| Tab model | 14 kinds, host-aware registry, reorder, drag | slot-registered views | n/a | 5 kinds, data-driven registry, no reorder | **partial** | Paseo |
| Tab persistence | per-workspace persisted layout | transient | n/a | in-memory only | **missing** | Paseo |
| Command palette | `Cmd+K` command center, scoped contributions | `/` + `@` in composer | n/a | none | **missing** | Paseo |
| Settings screen | full list+detail, ~12 sections | modal panel, ledger of sections | full control panel | **none** (one accounts dialog) | **missing** | Paseo |

## B. Theming and appearance

| Aspect | Paseo | dsh | cliproxyapi | smind | Verdict | North star |
| --- | --- | --- | --- | --- | --- | --- |
| Dark mode | 8 themes + auto, `Cmd+Alt+T` | light/dark/system, host-backed | unknown | `.dark` block exists, **never applied** | **missing** | Paseo |
| No-flash theme boot | n/a (native) | synchronous `<body>` bootstrap | n/a | n/a | **missing** | dsh |
| Semantic token layering | static palette → semantic tokens, documented | `--dsw-*` static scale → alias layer | n/a | shadcn defaults, one stray chromatic token | **partial** | dsh (layering) + Paseo (vocabulary) |
| Status colour family | `status*` + separate higher-chroma `statusDot*` family, generated | tone helpers | status strings | none — monochrome dot | **missing** | Paseo |
| Typography scale settings | interface / content / code sizes, UI+mono font pickers | — | — | none | **missing** | Paseo |
| Syntax theme | selectable, shared by code/diff/editor | shiki sheet | — | 3 unharmonized sources (CodeMirror, diff2html, highlight.js) | **partial** | Paseo |
| Terminal palette tied to theme | yes (ANSI tokens per theme) | — | — | no | **missing** | Paseo |
| Written design law | `docs/design.md`, 15 sections + forbidden list | per-package READMEs | — | none | **missing** | Paseo |
| Density / rhythm discipline | explicit rules + canonical surfaces table | — | — | two padding scales, hand-rolled headers | **partial** | Paseo |

## C. Sidebar, workspace/task list, attention

| Aspect | Paseo | dsh | cliproxyapi | smind | Verdict | North star |
| --- | --- | --- | --- | --- | --- | --- |
| Grouped list | projects → workspaces, pin, reorder, filter | workspaces → sessions, 5-then-Show-more | n/a | workspace → space → task | **partial** | Paseo |
| Search / filter | project filter, host filter, search | collapsed search expanding across the header | n/a | none | **missing** | dsh (the interaction) |
| Row metadata | branch, project, host, PR state, labels, services health, CI checks, diff stat — each toggleable | status dot | n/a | title + one attention dot | **missing** | Paseo |
| Reorder / drag | yes, host-durable | yes, Manual vs Last-updated orders | n/a | none | **missing** | Paseo |
| Rename / labels | rename, colored labels, manager modal | rename | n/a | none | **missing** | Paseo |
| Attention reasons | `finished / error / permission` + viewed-timeline model | unread marking (local) | n/a | **same three reasons**, one dot | **partial** | Paseo |
| Aggregate workspace status | per-`workspaceId` bucket | — | n/a | none | **missing** | Paseo |
| Cross-client list freshness | daemon events for everything | host events | n/a | **local `refresh()` only** — another tab never sees a new task | **missing** | Paseo |
| Sidebar callouts / alerts | `SidebarCallout` + `<Alert>` + toasts | Toast atom | — | inline `<p>` only | **missing** | Paseo |

## D. Agent timeline (the biggest gap — needs daemon work)

| Aspect | Paseo | dsh | cliproxyapi | smind | Verdict | North star |
| --- | --- | --- | --- | --- | --- | --- |
| Timeline item model | 7 typed kinds (`user_message`, `assistant_message`, `thought`, `tool_call`, `todo_list`, `activity_log`, `compaction`) | open registry of chat nodes | n/a | **4 wire events, `text` only** (`internal/taskrunner/event.go:7-29`) | **missing** | Paseo for the vocabulary, dsh for the registry shape |
| Tool-call rendering | per-tool cards, icons, summary, detail sheet, detail-level setting, click-to-file | keyed `tool.call.toolview` by wire tool name + 5 render intents + generic fallback | n/a | none | **missing** | dsh (registry) over Paseo's data |
| Markdown in transcript | yes, with its own content font size | `MarkdownText` primitive | n/a | none (dep present, unused here) | **missing** | Paseo |
| Reasoning / thinking blocks | `thought` items, `autoExpandReasoning` setting | declared reasoning | n/a | none | **missing** | Paseo |
| Turn model + footer | boundaries, elapsed timer, copy, fork | turn boundaries, thick rules | n/a | none (run = the only unit) | **missing** | Paseo |
| Scroll / tail behaviour | bottom-anchor controller, virtualization, text-reveal smoothing, pagination | streaming tail isolation, virtualized ledger, reserved gutter | n/a | plain `overflow-y-auto`, **no auto-follow** | **missing** | Paseo |
| Token / context usage | context-window meter, provider usage cards | trajectory inspector per record | `api-key-usage` | none | **missing** | Paseo |
| Fork / rewind / retry | all three | branch action | n/a | none | **missing** | Paseo |
| Subagents | full lifecycle + track + provider subagents | lineage breadcrumbs + catalog | n/a | none | **missing** (and smind has no subagent concept at all) | Paseo — but defer |
| Chat outline / jump | hover rail outline | trajectory overview strip | n/a | none | **missing** | Paseo |
| Background jobs | — | header-action popover, live elapsed | usage queue | none | **missing** | dsh |

## E. Composer

| Aspect | Paseo | dsh | cliproxyapi | smind | Verdict | North star |
| --- | --- | --- | --- | --- | --- | --- |
| Multiline input | autogrow, capped, Shift+Enter | capped + wheel-chaining + IME-safe | n/a | **single-line `<input>`** | **missing** | Paseo |
| Provider / model / mode controls | provider, model+effort, mode, thinking, features | model+effort seat, plan chip, access-mode chip | n/a | provider select + approval-policy select | **partial** | Paseo |
| Send behaviour | `interrupt / steer / queue` setting | queue rows in the input dock | n/a | fire-and-forget only | **missing** | Paseo |
| Stop / interrupt | stop button + `Escape` | — | n/a | Stop button on the run card only | **partial** | Paseo |
| Attachments | files, images, issues/PRs, paste, drop | attachments package | n/a | none | **missing** | Paseo |
| `@file` references | yes | atomic inline chips, directory descent, quoted paths | n/a | none | **missing** | dsh (the chip behaviour) |
| Slash commands / skills | yes | 3-kind dispatch + `decorate()` | n/a | none | **missing** | dsh |
| Draft persistence | survives workspace switch, mirrored to store | InputHub carries drafts | n/a | none | **missing** | Paseo |
| Composer blocks / disabled reason | — | typed blocks with localized reason as placeholder | n/a | disabled when no client, no reason | **missing** | dsh |
| Empty/hero state | New workspace hero with full controls | dashed composer card = workspace picker, identity preserved | n/a | "Select a task to get started." | **partial** | dsh |

## F. Permissions and human-in-the-loop

| Aspect | Paseo | dsh | cliproxyapi | smind | Verdict | North star |
| --- | --- | --- | --- | --- | --- | --- |
| Placement | pinned list header above the composer | **takes over the composer** | n/a | pinned dock above the composer | **has** | — |
| Cross-connection resolve | yes | yes | n/a | **yes** | **has** | — |
| Option semantics | provider options, plan gets "Implement" | one-shot Refuse/Allow | n/a | raw option labels, no kind styling | **partial** | Paseo |
| What's being asked | command line, diff, tool detail | reconstructed command line from the running call's args | n/a | `summary` string only | **partial** | dsh |
| Structured question form | multi-question, multi-select, other, skip | same, plus progress nav and IME handling | n/a | none | **missing** | dsh |
| Plan review | Chat about it / Refuse / Approve | same, as a presentation intent | n/a | none | **missing** | Paseo/dsh (identical) |
| Policy presets | per-agent mode + settings default | per-session `/permission` + future-session default with risk ack | n/a | per-run `manual / auto-safe` select | **partial** | dsh (the two-lifetime split) |
| OS notification | desktop + real push | — | n/a | web Notification, edge-triggered | **partial** (push is **n/a**) | Paseo |

## G. Files, diffs, review, terminal

| Aspect | Paseo | dsh | cliproxyapi | smind | Verdict | North star |
| --- | --- | --- | --- | --- | --- | --- |
| File tree | icons, context actions, git decoration, collapse | directory picker | n/a | lazy tree, keyboard rows, no icons/decoration | **partial** | Paseo |
| Editor | CodeMirror + preview + conflict handling + vim | — | n/a | **CodeMirror + preview + `expectedMtime` conflict** | **has** | — |
| File search | `Cmd+P` + command-center file search | — | n/a | none | **missing** | Paseo |
| Changed-file review | changes tree + working diff + per-file | `DiffBlock` | n/a | per-file list, expand, stage, viewed, commit, PR | **has** | — |
| Line comments / review drafts | persisted per checkout, routed to agent | — | n/a | none | **missing** | Paseo |
| Commit history / branch ops | commits section, branch switcher, push, PR, merge-from-base | — | n/a | commit + create PR only | **partial** | Paseo |
| Terminal | multi, profiles, activity, scrollback, links, split | pwsh terminal | n/a | one per task, xterm, detach-not-close | **partial** | Paseo |

## H. Settings, accounts, providers

| Aspect | Paseo | dsh | cliproxyapi | smind | Verdict | North star |
| --- | --- | --- | --- | --- | --- | --- |
| Settings shell | list+detail, ~12 sections, feature-owned | modal panel, section ledger | full panel | **none** | **missing** | Paseo |
| Appearance settings | theme, 3 font sizes, 2 font families, syntax theme | light/dark/system | — | none | **missing** | Paseo |
| Layout preferences | per-surface open location | — | — | none | **missing** | Paseo |
| Shortcut customization | every binding rebindable + search | — | — | none | **missing** | Paseo |
| Account row richness | provider row + auth state + diagnostics sheet | models settings | **~20 fields incl. status_message, next_retry_after, success/failed, recent_requests, quota, priority, weight, note** | presence + health dot | **partial** | **cliproxyapi** |
| OAuth flow | provider-specific | — | start → URL → poll status → **cancel** | start, no poll/cancel surfaced | **partial** | cliproxyapi |
| Multiple accounts per provider | yes | — | yes (`auth_index`) | no | **missing** | cliproxyapi |
| Quota / usage visibility | balance bar + window bar + cards + tooltip | — | usage endpoints | none | **missing** | Paseo (presentation) over cliproxyapi (data) |
| Error-log / request-trace access | diagnostics sheet | — | error-log list + per-request trace | none | **missing** | cliproxyapi |
| Config escape hatch | project settings editor | settings.yaml via host API | structured rows + raw YAML | none | **missing** | cliproxyapi |

## I. Keyboard

| Aspect | Paseo | dsh | cliproxyapi | smind | Verdict | North star |
| --- | --- | --- | --- | --- | --- | --- |
| Shortcut system | 1.7k-line data table, rebindable, searchable, 5 sections, help dialog | per-surface focus contracts | — | **3 bindings total** (`Cmd+B`, `Mod-s`, tree Enter/Space) | **missing** | Paseo |
| Command palette | `Cmd+K` | — | — | none | **missing** | Paseo |
| Focus discipline | scoped handlers, focus-scope | Escape returns focus to trigger; list closes before control unmounts | — | tab-close `×` not keyboard-operable | **missing** | dsh |

## J. Responsive / mobile

| Aspect | Paseo | dsh | cliproxyapi | smind | Verdict | North star |
| --- | --- | --- | --- | --- | --- | --- |
| Compact-first discipline | yes, one `useIsCompactFormFactor()` branch per screen | concession chain, 56px rail | — | one 768px breakpoint, used by the shadcn sidebar only | **missing** | Paseo |
| Compact panel model | 3 mutually exclusive destinations, one position value, gesture arbitration | rail + auto-closing details | — | none | **missing** | Paseo (model), CSS not gestures |
| Native app | iOS/Android | — | — | — | **n/a** | — |

## K. Empty / loading / error states

| Aspect | Paseo | dsh | cliproxyapi | smind | Verdict | North star |
| --- | --- | --- | --- | --- | --- | --- |
| Written rules | `design.md` §11, prescriptive | per-package | — | none | **missing** | Paseo |
| Loading | inline by default, page-level centred, card-level one line | explicit loading rows | — | ad-hoc text per pane | **partial** | Paseo |
| Empty | short noun phrase, ≤1 ghost button | "No sessions yet" | — | inconsistent casing/punctuation | **partial** | Paseo |
| Errors | inline / `<Alert>` / imperative, one per region | inline per region | — | inline `<p>` only, no alert primitive | **partial** | Paseo |
| Layout stability | "changing state must not move the layout" — reserve space | reserved gutters, reserved overflow width | — | not considered | **missing** | Paseo |
| In-flight feedback | present-participle labels, pending menu items | pending states | — | Save has it; Commit/Stage/Reload don't | **partial** | Paseo |

---

## Top 10 gaps, ranked by daily-driver (dogfood) impact

1. **Structured agent timeline.** A `<pre>` of text vs typed items with
   tool-call cards. Everything about reviewing what an agent did depends on
   this, and it needs a daemon event-vocabulary change first (D).
2. **No dark mode.** The tokens exist; nothing applies them. Highest
   ratio of complaint to effort in the whole list (B).
3. **No keyboard layer and no command palette.** Three bindings total (I).
4. **Composer is a single-line input** with no model/mode control, no
   multiline, no draft, no steer/queue (E).
5. **Nothing is addressable and nothing persists.** Reload loses the
   selected task and every open tab; no deep link to a task (A).
6. **No settings screen at all** — theme, fonts, density, defaults,
   shortcuts, notifications all unreachable (H).
7. **Sidebar carries almost no signal** — one monochrome dot, no branch,
   no diff stat, no run status, no search, no aggregate status (C).
8. **Cross-client staleness** — workspace/space/task mutations emit no
   daemon events, so a second tab (or a CLI-created task) never appears
   without a manual reload (C).
9. **Account rows are thin** — no status message, no token expiry, no
   rate-limit recovery time, no usage; the dogfood log's recurring
   "why did this run die" questions live exactly here (H).
10. **No splits / side dock** — reviewing a diff while watching the agent
    is impossible in one view, which was the original motivation in
    `docs/research/dual-mode-ui.md` (A).

Honourable mentions just below the line: no auto-follow on the transcript
scroll; no toast/alert primitive; no `PaneHeader` primitive (four
hand-rolled headers, two padding scales); no responsive/compact layout
ahead of the Phase 3 mobile work.
