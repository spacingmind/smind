# Reference audit: deepseek-harness / `dsh` (`refs/deepseek-harness`)

**What it is**: DeepSeek's agent harness. Everything is a plugin on a
Cordis-style DI/slot runtime. The web client is a *composition* of ~35
`ui-*` plugin packages rather than an app with a component tree.

**Where the UI lives**: not `apps/web` — that is a 10-line bootstrap
(`refs/deepseek-harness/apps/web/src/main.ts`) over
`@deepseek-ai/dsh-client-web`. The real UI is
`refs/deepseek-harness/packages/client/ui-*`. Each package's `README.md`
is an unusually precise behavioural spec; they are the primary source used
here.

The e2e suite (`refs/deepseek-harness/apps/web/tests/*.e2e.ts`) plus its
ARIA-tree snapshots (`refs/deepseek-harness/apps/web/tests/snapshots/**/*.expected.md`)
are a second, machine-checked description of the same behaviour — and the
snapshot format itself is worth noting as a testing idea (assert the
accessibility tree, not the DOM).

---

## 1. Information architecture

**Three-column AppFrame** — `refs/deepseek-harness/packages/client/ui-layout`:

```
┌──────────┬───────────────────────────┬──────────┐
│ sidebar  │      conversation         │ details  │
│ (56px    │  header / tabs / scroll   │  (tool   │
│  rail    │  port / composer stack    │  detail, │
│  when    │                           │  closes  │
│  closed) │                           │  to 0)   │
└──────────┴───────────────────────────┴──────────┘
```

- Slots declared by the layout plugin: `sidebar`, `conversation`,
  `details`, `conversation.empty`, into a runtime-owned `root` slot.
- **Concession chain**: as the window narrows only `details` shrinks, then
  auto-closes. A closed sidebar keeps a 56px control rail; details closes
  to zero width.
- The sidebar resize boundary is an invisible hit strip; the details
  boundary keeps a visible floating pill.
- Panel geometry is deliberately **transient** — reload restores defaults;
  switching session ids closes details before paint. (Listed as a known
  limitation in their own README.)

**Object model**: `Workspace` → `Session`. Flatter than Paseo — no
project/worktree layer, no host layer. Sessions can have **subagent
lineage** (breadcrumbs in the conversation header).

**Slot system** is the extension mechanism everywhere: `sidebar.workspaces`,
`sidebar.brand.mark`, `sidebar.brand.name`, `sidebar.settings`,
`conversation.view` (the view-tab ring), `conversation.chat.node`
(keyed chat-row renderers), `conversation.chat.turnTail`,
`conversation.composer` (selector-routed composer takeover),
`conversation.input.dock`, `conversation.input.plan`,
`conversation.input.model`, `conversation.session.header.actions`,
`conversation.session.header.lineage`, `conversation.details.tool`,
`tool.call.toolview` (keyed per wire tool name), `settings.section`,
`settings.action`, `settings.onboarding`, `settings.plugins.tab`,
`settings.general.item`.

---

## 2. Agent/task interaction model

### Composer
`refs/deepseek-harness/packages/client/ui-conversation`.

- Sticky composer stack inside the scrollport: **stats dock** →
  **input docks** → **input bar**. The scrollport reserves its scrollbar
  gutter unconditionally so the input card never shifts horizontally when
  the transcript starts scrolling.
- **Input docks** are an ordered stack of standalone cards
  (`conversation.input.dock`): Todo plan strip (order ~0), **GoalBar**
  (order 10, `ui-goal`), Queue rows.
- Named single seats in the input row: `conversation.input.model`
  (`ui-model-selection`), `conversation.input.plan` (`ui-plan` — a
  warn-coloured "Plan ×" chip while plan mode is on), the access-mode
  (permission preset) chip.
- **Composer blocks** (`ctx.conversation.blocks`): another plugin can make
  the composer inert with its own localized reason shown as the
  placeholder. The model seat stays live because it is usually the thing
  that clears the block. The no-workspace state wins over other blocks.
- **Hero state**: with no session, the whole dashed composer card becomes
  the trigger for a workspace picker; the textarea stays read-only and
  keyboard-accessible, and the shell/DOM identity is preserved when a real
  session arrives (no remount, no layout jump).
- Textarea growth is capped, then scrolls; wheel-chaining from the capped
  draft to the scrollport; a Safari-only soft-wrap reflow recovery.
- `@file` / `@session` references (`ui-reference`): a file pick becomes an
  *atomic inline reference chip* (file glyph + coloured filename, no
  capsule); a directory stays editable path text with a folder glyph and
  keeps the menu open at its trailing slash so you can descend. Paths with
  spaces serialize as `@"path with spaces"`.
- `/` slash commands (`ui-commands`): a session-keyed command directory
  cache with three dispatch kinds — `execute`, `popupSelect`,
  `leadingInput` — plus `decorate()`, which hangs a picker on an *existing*
  host command without forking it.
- `/skill` invocation (`ui-skill`), agent presets (`ui-agent-preset`),
  attachments (`ui-attachment`).

### Streaming timeline
- **Grouped step-summary flow** with **streaming tail isolation** (only
  the tail re-renders while streaming) and explicit turn status.
- Chat rows are an *open registry*, not a closed union: a plugin
  declaration-merges its `ChatNodeDataMap` key, registers a
  `ConversationNodeDefinition`, and registers a keyed renderer on
  `conversation.chat.node`. There is a cookbook
  (`refs/deepseek-harness/docs/cookbook/adding-a-conversation-node.md`).
- **View tabs** (`conversation.view`): the chat view is one entry;
  `ui-trajectory` contributes another. Each view owns its own chrome and
  the composer stays put underneath.
- **Trajectory view** (`ui-trajectory`): a turn-aware event ledger —
  selectable User/Assistant/Tool/Subtool records, thick rules at turn
  boundaries, inline step markers, a local inspector showing token usage,
  duration, input, output, timing; a fixed timing **Overview** strip above
  the ledger; virtualized with prepend-safe keys and ARIA indexes;
  "load one older page" control as the first row.
- **Compaction** renders as one collapsed row at the checkpoint's flow
  position — the transcript above is not replaced. Manual `/compact`
  starts as a running command row and folds into the checkpoint row on
  success under the same React key.
- Message actions: copy, branch, feedback (`ui-message-feedback`).
- **Deliverables** (`ui-deliverables`): a finished turn ends with a
  produced-files row derived from the *tools' own reported locations*, not
  from the model's closing prose — up to six file chips with a measured
  `+ N files` overflow whose width is reserved so the row never reflows.
  Inline code references in the closing prose become links.

### Tool-call rendering
`refs/deepseek-harness/packages/client/ui-tool`.

- One root `ToolCallBlock` containing recursive `subCalls`; the tree is
  walked and every node dispatched through a **keyed slot**
  `tool.call.toolview` addressed by the *wire tool name*. Unregistered
  names get a generic card.
- Shared pure card models for render intents `terminal`, `read`, `diff`,
  `search`, `web` — the same model renders both the inline row and the
  details pane (`conversation.details.tool`).
- Generic rows classify unknown tools into search / read / shell / write /
  edit / code / generic variants. Lifecycle is running / success / failure /
  interrupted, read only from the frozen call/result slice.
- DOM contract `data-chat-anchor-key="call:<id>"` for paging and selection.
- Paths relativize to the session `cwd`, then `~` for the host home.
- Atoms in `ui-primitives`: `TerminalBlock`, `DiffBlock`, `ReadBlock`,
  `SearchBlock`, `WebBlock`, `JsonTree`, `JsonBlock`, `MarkdownText`.

### Approvals and questions
- **Approval takes over the composer**: `ApprovalPanel` registers as a
  selector-routed `conversation.composer` entry and *replaces* the input
  bar while a wait is pending — amber strip, justification headline,
  the exact command line reconstructed from the running call's args, and
  one-shot Refuse / Allow. (Snapshot:
  `refs/deepseek-harness/apps/web/tests/snapshots/approval-composer/ui.expected.md`.)
- **Questions** (`ui-user-questions`): one question at a time with progress
  navigation, single/multi select, recommendation badges derived from label
  suffixes, custom "other" answers that coexist with multi-select, a capped
  card whose title/nav/actions stay fixed while detail and choices share an
  internal scroll region, Enter to advance/submit, Shift+Enter for newline,
  IME-safe. Submits one structured batch; "Skip this question" emits a blank
  item; close rejects the whole wait as `ASK_CANCELLED`.
- **Plan review** is a presentation *intent* on a single question: a "Plan
  review" strip, the plan as scrolling markdown, and
  `Chat about it / Refuse / Approve`.
- **Permission presets** (`ui-permission-presets`): a General-settings row
  writes the preset for *future* sessions (Full access requires an explicit
  risk acknowledgement); the current session switches through a
  `popupSelect` decoration on the host's own `/permission` command. Both
  read the same host-computed `permissions` projection.

### Background jobs
`ui-jobs`: one entry in `conversation.session.header.actions`, rendered
only when the session has at least one job. Badge counts `running` +
`stopping`, omitted at zero. Popover lists live rows first (by `startedAt`
asc) then settled rows (by `finishedAt` desc), each with producer kind,
label, status marker, the producer's `detail` in place of the generic
status word, and a live elapsed duration that ticks only while an open list
holds something moving. Escape closes and returns focus to the trigger.

---

## 3. Workspace / session model

`ui-workspace` owns both the sidebar browser and the hero picker:

- Grouped or flat session rows; workspace add / rename / reorder; session
  reorder. A workspace remembers open/closed; an open one shows five
  sessions with a transient **Show more**, resetting to five after a full
  close/reopen.
- Two session orders — **Manual** and **Last updated** — persisted per
  account. Entering Last updated does a full recency sort and later prompts
  promote a session once; entering Manual freezes positions. Dragging edits
  the current order in either mode; Manual drags on real workspaces write
  through to the host, Ungrouped/flat stay browser-local.
- **Collapsed search** is a header action beside view and add. Activating
  expands a field across the header; an outside click collapses only an
  empty query; a non-blank query replaces either browsing mode with one
  flat result list matching title and workspace substrings.
- In the 56px rail, add and search render as 36px controls on the shell's
  shared horizontal entry path.

---

## 4. Settings

`ui-settings` (base: `ctx.settingsScope`, `ctx.settingsSchema`, slot types)
+ `ui-settings-general` (the shell) + feature-owned sections.

- Settings is a **modal panel** launched from a bottom-pinned
  `sidebar.settings` seat, with a nav projected from the `settings.section`
  ledger. Feature packages own their own rows/sections (Permission,
  Language, Appearance, Models, Plugins).
- Preferences are **host-backed**: written through the host settings API
  into `$DSH_HOME/settings.yaml`, with namespace revisions, gesture-ordered
  serialization of rapid selections, and a reload of the durable value when
  a write is rejected. A remote (non-privileged) browser keeps its
  selection process-local.
- Onboarding is an ordered `settings.onboarding` ledger mounting exactly one
  step at a time, each owning its dialog chrome and the app-root `inert`
  lifecycle.
- `ui-settings-models` for model/provider config;
  `ui-settings-plugins` / `ui-settings-plugin-inventory` for the plugin
  inventory; `ui-directory-picker-browse` / `-native` for picking a cwd.

---

## 5. Theming and appearance

`refs/deepseek-harness/packages/client/ui-theme`.

- `--dsw-*` token base stylesheets: a **static scale layer** plus an
  **alias semantic layer**. Five sheets in a fixed import order
  (`base.css`, `design-platform.css`, `scrollbar.css`,
  `gradient-shadow-text.css`, `shiki.css`) injected as plugin-owned global
  styles so unload/HMR removes them.
- `ThemeRuntime` owns the preference (`light | dark | system`), resolves
  `system` through `prefers-color-scheme`, and publishes immutable
  `ThemeSnapshot`s on a `theme/change` event — **it never touches the DOM**.
  `ui-layout` is the sole presenter: it sets `html { color-scheme }`,
  `body[data-ds-dark-theme]`, the alias tokens as inline vars on body, and
  one owned `<meta name="theme-color">` whose content is measured from the
  computed body background *after* tokens are applied.
- **No flash of wrong theme**: when the host composition has an HTTP
  server, it injects a synchronous bootstrap right after `<body>` carrying
  the registered `ui-theme.preference`, so `color-scheme` and the dark
  attribute are set before the loading page paints.
- **Scrollbars are a design surface**: `--dsh-scrollbar-thumb` /
  `-thumb-hover` are rebindable per container (elevated surfaces retint to
  l2 tokens; the sidebar rebinds to `transparent` while the pointer is
  elsewhere and keeps the thumb for 2s after it leaves). The Firefox
  standard-property path and the WebKit pseudo-element path are mutually
  exclusive by `@supports`, because a non-`auto` `scrollbar-color` makes
  WebKit discard every `::-webkit-scrollbar*` rule. The gutter reservation
  lives in the scrolling region so revealing a thumb never reflows.

---

## 6. States, motion, accessibility

- Sidebar collapse is a choreographed sequence: expanded content held at
  width while fading 150ms; the four upper controls share one 150ms fade +
  49px leftward translation into the rail; the settings seat fades without
  translating; then the layout's 300ms column slide. Starting collapsed
  renders the rail statically, and **reduced-motion disables both**.
- Hover cards with a pointer-leave grace, optional copy-on-click with an
  accessible name and a one-second confirmation that preserves card height.
- `useAnchoredMaxHeight` / `useAnchoredPosition` for viewport-clamped
  floating panels, re-placed on capture-phase scroll and resize.
- Focus discipline is stated explicitly per surface (Escape closes and
  returns focus to the trigger; the last row disappearing closes a list
  before its control unmounts so focus never vanishes).
- Bilingual by construction — every package has `README.md` +
  `README.zh.md`, and locale namespaces are per-package.
- Every README ends with **Model Experience** (does this reach a model
  request?), **KV Cache effect**, and **Known Limitations and Deferred
  Work** — an honesty discipline worth stealing for smind's plan docs.

---

## 7. What's worth borrowing for smind, and what isn't

**Borrow:**
- Tool-call rendering as a **keyed registry addressed by wire tool name**,
  with a generic fallback card and five shared render intents
  (`terminal`, `read`, `diff`, `search`, `web`) used by both the inline row
  and the detail pane.
- **Approval takes over the composer** — a stronger version of pinning,
  and it makes "you cannot miss this" structural rather than positional.
- Token layering: static scale → semantic alias, with a synchronous
  pre-paint theme bootstrap.
- Composer **input dock stack** as an ordered, extensible list of cards.
- ARIA-tree snapshot testing.
- The `settings.section` ledger shape — each feature owns its own settings
  page rather than one god-component.

**Don't borrow:**
- The Cordis plugin/slot runtime itself. smind is a Go daemon + a small
  React app; a DI slot system is architecture smind has no need for, and
  ADR 0002 already fixed the web stack.
- Transient panel geometry (dsh's own README lists it as a limitation).
- The three-column-only shell — Paseo's splittable tab tree is strictly
  more capable and is already the direction ADR 0004 and
  `docs/research/dual-mode-ui.md` committed to.
