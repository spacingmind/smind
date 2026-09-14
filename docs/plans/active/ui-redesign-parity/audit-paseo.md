# Reference audit: Paseo (`refs/paseo`)

The north-star reference. Paseo is the product smind is meant to replace as
the maintainer's daily driver (`docs/plans/active/smind-dogfood.md`), so
where the three references disagree, Paseo's model wins unless there is a
stated reason otherwise.

**What it is** (`refs/paseo/docs/product.md`): a client/server agent IDE.
A Node daemon owns agent processes and streams over WebSocket; clients are
Expo/React-Native-Web apps shipped as desktop (Electron), mobile
(iOS/Android), web, plus a CLI. Multi-provider (Claude Code Agent SDK,
Codex app-server, Copilot via ACP, OpenCode, Pi, OMP, plus a one-click ACP
provider catalog), self-hosted, BYOK.

**Where the UI lives**: `refs/paseo/packages/app/src`. ~180 e2e specs under
`refs/paseo/packages/app/e2e/browser/` are themselves a readable feature
inventory. Design law is written down in `refs/paseo/docs/design.md`;
terminology in `refs/paseo/docs/glossary.md`.

---

## 1. Information architecture

Expo Router file-based routes, `refs/paseo/packages/app/src/app/`:

| Route | Screen |
| --- | --- |
| `app/index.tsx` | root redirect / last host |
| `app/welcome.tsx` | first-run |
| `app/new.tsx` | New workspace (hero) |
| `app/sessions.tsx`, `app/h/[serverId]/sessions.tsx` | history (past agent sessions) |
| `app/schedules.tsx` | cron-style scheduled agents |
| `app/open-project.tsx`, `app/h/[serverId]/open-project.tsx` | add/open a project |
| `app/pair-scan.tsx` | pair a mobile device with a daemon |
| `app/h/[serverId]/index.tsx` | host root |
| `app/h/[serverId]/workspace/[workspaceId]/index.tsx` | **the workspace canvas — the main surface** |
| `app/h/[serverId]/agent/[agentId].tsx` | deep link to an agent, resolved into its workspace |
| `app/settings/index.tsx`, `app/settings/[section].tsx` | settings shell (list+detail) |
| `app/settings/hosts/[serverId]/…` | per-host settings, per-project settings |
| `app/h/[serverId]/plugin/[pluginId]/[surfaceId].tsx` | plugin-contributed surfaces |

**Three-level object model** (`refs/paseo/docs/glossary.md`):
`Project` (a stable filesystem root, grouped by normalized git remote) →
`Workspace` (one concrete `cwd` on one daemon, `workspaceKind` of
`directory | local_checkout | worktree`) → `Agent session` (one provider,
one model, one cwd, one timeline — opens as a tab).
Plus `Host` = a client-side connection profile pointing at a daemon; the
sidebar can span several hosts at once.

**Shell topology** (`refs/paseo/docs/design.md` §9):
- Left sidebar — projects/workspaces list, pinned on desktop, overlaid on
  compact. `packages/app/src/components/left-sidebar.tsx`,
  `sidebar-workspace-list.tsx`.
- Workspace canvas — a splittable tab tree.
  `packages/app/src/screens/workspace/workspace-screen.tsx` (4.4k lines),
  `packages/app/src/components/split-container.tsx`.
- Explorer sidebar — a *dedicated dock* (not a pane in the split tree) for
  Files and Changes, own persisted width, `Cmd+E`.
  `refs/paseo/docs/explorer-sidebar.md`,
  `packages/app/src/workspace-tabs/explorer-sidebar.ts`.
- Side pane — an ordinary right-side pane for "open beside".
  `packages/app/src/workspace-tabs/open-beside.ts`.

**Tab kinds** (`packages/app/src/workspace-tabs/model.ts`): `new_tab`,
`draft`, `agent`, `provider_subagent`, `terminal`, `browser`,
`changes_tree`, `files`, `pull_request`, file, `working_diff`,
`commit_diff`, `setup`, plugin panel. Each panel registers the `PaneHost`
values it supports (`packages/app/src/panels/panel-manifest.ts`,
`panel-registry.ts`); launchers filter by host and tab moves reject
unsupported destinations.

Placement intent is a first-class concept (`refs/paseo/docs/explorer-sidebar.md`):
`pane` (move it here) / `prefer` (new targets here, don't yank existing) /
`focused` / `ambient`. Explicit "Open to Side" uses `pane`, implicit opens
use `prefer`.

---

## 2. Agent/task interaction model

### Composer
`packages/app/src/composer/` — the whole prompt surface. Vocabulary is
fixed in the glossary: **Composer** (whole surface), **Composer input**
(`composer/input/input.tsx`), **Composer toolbar** (bottom control row),
**Agent controls**, **Composer footer**, **Composer track**,
**Attachment tray**.

- **Agent controls** (`composer/agent-controls/`): provider, model, mode
  (plan/default/full-access — icon-only), thinking level, provider-specific
  feature toggles. Model picker is a two-level Model/Effort menu
  (`refs/paseo/packages/client/ui-model-selection` equivalent lives in
  `composer/agent-controls/model-sheet.tsx`).
- **Attachments** (`composer/attachments/`, `packages/app/src/attachments/`):
  forge issue/PR, review context, uploaded file, text, image; drag-drop
  (`components/file-drop/`), clipboard image paste
  (`composer/clipboard-image.ts`, `composer/native-pasted-image.ts`).
- **Autocomplete / input triggers**: `@file` references, slash commands
  (`packages/app/src/client-slash-commands/`), skills
  (`packages/app/src/agent-skills/`).
- **Send behaviour** is a setting: `interrupt | steer | queue`
  (`packages/app/src/hooks/use-settings/storage.ts:193`). **Steer** =
  admit a follow-up into the already-running turn (glossary entry
  "Steer"); **Queue track** = hold client-side until the turn ends.
- **Tracks** (`composer/tracks.tsx`): Queue track (a lane in the composer),
  Subagents track and Tasks track (pills floating over the transcript,
  each opening a popover/sheet). `packages/app/src/panels/agent-tracks.tsx`
  owns placement.
- **Drafts** (`composer/draft/`, `stores/draft-store/`): a draft survives
  workspace switches and is mirrored into the session store.
- **Voice / dictation** (`packages/app/src/voice/`, `dictation/`): realtime
  voice mode and push-to-talk dictation, with their own shortcuts.
- **Diff-stat pill** (`composer/diff-stat-pill.tsx`) and Changes pill — a
  two-stage desktop action that first reveals Explorer on Changes, then
  routes to the working diff.

### Streaming timeline
`packages/app/src/agent-stream/` + `packages/app/src/timeline/`.

Stream item union (`packages/app/src/types/stream.ts`):
`user_message | assistant_message | thought | tool_call | todo_list |
activity_log | compaction`. This is the single most important structural
difference from smind — the transcript is a typed item list, not text.

- Separate native and web scroll strategies (`strategy-native.tsx`,
  `strategy-web.tsx`, `web-virtualization.ts`) with a bottom-anchor
  controller (`bottom-anchor-controller.ts`), text reveal smoothing
  (`text-reveal.ts`), history-window pagination
  (`history-window.ts`, `history-start-pagination.ts`).
- Turn model: `turn-boundary.ts`, `turn-membership.ts`, `turn-footer.tsx`,
  `turn-liveness.ts`. The turn footer carries elapsed timer, progress
  loader, copy, and the **fork menu**.
- **Chat outline** (`agent-stream/chat-outline/`): a hover rail giving a
  jump-to-prompt outline of the conversation. Toggled by
  `chatOutlineEnabled`.
- **Compaction** rows, **rewind** (`packages/app/src/components/rewind/`),
  **fork** (`hooks/use-fork-agent.ts`, glossary "Fork" — start a new agent
  seeded with a copy of curated history; on an in-flight turn it captures
  everything up to now).
- **Timeline sync** (`refs/paseo/docs/timeline-sync.md`,
  `timeline/timeline-sync-plan.ts`, `viewed-timeline-sync.ts`): authoritative
  catch-up, retry, viewed-state.

### Tool-call rendering
`packages/app/src/tool-calls/`, `components/tool-call-details.tsx`,
`components/tool-call-sheet.tsx`.

- `tool-calls/presentation.ts` builds a `ToolCallPresentation`:
  `displayName`, `summary`, `errorText`, `icon`, `isLoadingDetails`,
  `hasDetails`, `canOpenDetails`, `openFilePath`, `isPlan`.
- **Detail level** is a user setting (`toolCallDetailLevel:
  "detailed" | "overview"`, `hooks/use-settings/storage.ts:134`).
  Overview mode groups consecutive calls
  (`tool-calls/detail-level/grouping.ts`) and offers an overview sheet
  (`tool-calls/detail-level/overview/sheet.tsx`).
- A tool call with a file path is clickable straight into a file tab.
- `plan-card.tsx` renders plan-type tool details specially.
- Shimmer while details load (`e2e/browser/tool-call-shimmer.spec.ts`).

### Permission prompts
`packages/app/src/agent-stream/view.tsx:139-149, 1292-1450`.

- Pending permissions render as `PermissionRequestCard`s in a **list
  header pinned above the composer**, not inline in the transcript — so
  they cannot scroll away. One card per pending request, filtered by
  `agentId`.
- Actions come from the provider's own option list; deny/accept get
  distinct styling, plan-mode gets "Implement" instead of "Accept".
- Responding uses `respondToPermissionAndWait` — the card stays disabled
  until the daemon confirms, and clears when the resolution event arrives
  (from this client or another).
- A **question form** variant (`components/question-form-card.tsx` +
  `question-form-card-core.ts`) handles structured multi-question asks:
  single/multi select, "other" free text, allow-empty, dismiss label,
  progress navigation.
- A **plan-review** intent variant renders the plan as markdown with
  `Chat about it / Refuse / Approve`.
- OS-level notifications for permissions: `utils/os-notifications.ts`,
  `push-notifications/` (native push via Expo, web via Notification API).

### Diffs and review
`packages/app/src/git/`, `packages/app/src/panels/changes/`,
`packages/app/src/panels/diff-panel.tsx`,
`packages/app/src/components/git-diff-pane.tsx`,
`packages/app/src/review/`.

- Live diff subscription per checkout; `working_diff` tab, `changes_tree`
  tab, `commit_diff` tab keyed by sha.
- **Draft line comments** persisted per checkout (`review/state.ts`,
  `review/store.ts`) — GitHub-style review, routed back to the agent.
- `git/commits-section/`, `git/pull-request-panel/`, `git/forges/`
  (GitHub / GitLab / Gitea / Forgejo adapters), branch switcher
  (`components/branch-switcher.tsx`), `WorkspaceActions`
  (commit / push / create PR / merge from base).
- Diff perf is a tracked concern (`e2e/browser/diff-performance.spec.ts`).

### File explorer / editor
`packages/app/src/file-explorer/`, `file-pane/` (editor, live-file,
markdown-preview, preview-lifecycle, source), `components/file-explorer-pane.tsx`,
`components/material-file-icon.tsx` + `material-file-icons.ts` (icon theme,
`refs/paseo/docs/file-icons.md`), context actions
(`e2e/browser/file-explorer-context-actions.spec.ts`), file-change conflict
handling (`refs/paseo/docs/file-observation.md`), `vimKeybindings` setting.

### Terminal
`packages/app/src/terminal/` — three renderers (web xterm, native grid,
webview), scrollback setting, terminal profiles (named shell commands,
host-wide config), activity indicators
(`refs/paseo/docs/terminal-activity.md`), alternate-screen handling, split
and resize, local link detection, drop targets. A terminal is a workspace
tab like any other and is also a New Workspace launch target.

### Subagents
`refs/paseo/docs/agent-lifecycle.md` — the definitive text. Paseo
subagents (managed agents with `paseo.parent-agent-id`) and provider
subagents (Claude/Codex/OpenCode child sessions) both appear in the
parent's **subagents track**; clicking opens a tab (read-only for provider
subagents). Cascade-archive, detach, "Archive finished".

---

## 3. Workspace / project model

- Projects auto-detected from the filesystem, tagged by git remote; opaque
  `prj_<16 hex>` ids; a `projectKey` groups the logical project across hosts.
- **Isolation** is a create-time choice — Local (reuse checkout) vs New
  worktree (`~/.paseo/worktrees/{name}`) — remembered as a form preference.
  `workspaceKind` is the derived, persisted property.
- New workspace screen (`screens/new-workspace-screen.tsx`) is a hero with
  project picker, isolation control, provider/model/mode controls, launch
  target (chat agent or a terminal profile), and a composer draft that
  survives.
- Workspace **scripts** declared in `paseo.json` — `service` and `script`
  types, health tracked for services and surfaced on sidebar rows
  (`components/sidebar/workspace-meta-row/service-summary.ts`).
- **Workspace labels** (`packages/app/src/workspace-labels/`): colored
  chips, picker, manager modal.
- Workspace recovery (`workspace-recovery/`), setup panel and streaming
  setup output (`panels/setup-panel.tsx`), archive with a risk warning when
  the worktree has uncommitted/unpushed work.
- **Directory-backed vs workspace-owned state** is an explicit boundary
  (glossary): diff/status/PR/file content are keyed `(serverId, cwd)`;
  tabs/agents/terminals/panes/title/review drafts are keyed `workspaceId`.

---

## 4. Settings / accounts / providers

Settings shell is list+detail (`screens/settings-screen.tsx`), sections
under `app/settings/[section].tsx`:

- **Appearance** (`screens/settings/appearance/`): theme picker with live
  preview, interface size, content size, code size, UI font, mono font,
  syntax theme.
- **Layout** (`screens/settings/layout/layout-section.tsx`): per-surface
  "Open location" — Main panel vs On the side for Explorer files, diffs,
  chat files, files-from-diffs, subagents; PRs get a third option
  (Explorer sidebar, the default).
- **Keyboard shortcuts** (`screens/settings/keyboard-shortcuts-section.tsx`)
  — every binding rebindable.
- **Providers** (`screens/settings/providers-section.tsx`): per-provider
  rows with enable switch, auth state, diagnostics sheet
  (`components/provider-diagnostic-sheet.tsx`), ACP provider catalog
  (`components/provider-catalog-list.tsx`), custom ACP providers.
- **Provider usage** (`packages/app/src/provider-usage/`): balance bar,
  rolling-window bar, per-provider cards, a tooltip section, tone mapping
  — i.e. quota/spend is a first-class visible surface.
- **Agent profiles** (`packages/app/src/agent-profiles/`): named launch
  bundles (provider + model + mode + thinking + feature values + notes),
  applied into the agent controls and then forgotten.
- **Agent skills** (`packages/app/src/agent-skills/`), **Plugins**
  (`screens/settings/plugins-page.tsx`, `refs/paseo/docs/plugins.md`),
  **Terminal profiles**, **Metadata generation**, **Editor**,
  **Browser tools**, **Hosts** (add host, pair device, restart daemon,
  release channel, update).
- **Projects** settings screen (`screens/projects-screen.tsx`,
  `project-settings-screen.tsx`) editing `paseo.json`, with a
  stale-write conflict alert.
- Onboarding / welcome (`components/welcome-screen.tsx`).
- **i18n** (`packages/app/src/i18n/`, `refs/paseo/docs/i18n.md`) — every
  string goes through `react-i18next`.

---

## 5. Status / attention signalling

- **Status dots** — their own token family `statusDot{Success,Danger,Warning,Running}`
  read only through `utils/status-dot-color.ts`; the running one pulses.
  The reasoning for a separate, higher-chroma band is written out in
  `refs/paseo/docs/design.md` §13.
- **Status pills** — `components/ui/status-badge.tsx`, status token text on
  a neutral `surface3` shell.
- **Workspace status bucket** — an aggregate per `workspaceId`; same-cwd
  workspaces intentionally share agent/terminal buckets
  (`refs/paseo/docs/agent-lifecycle.md` §"Workspace activity").
- **Sidebar workspace meta row** (`components/sidebar/workspace-meta-row/`):
  branch, project, host badge, change-request state, labels, services
  health, CI checks summary, diff stat — each individually toggleable
  through the sidebar's "Show" display preferences
  (`components/sidebar/display-preferences/`).
- **Attention reasons** — `finished | error | permission`, and a viewed/
  unviewed timeline model (`timeline/viewed-timeline-sync.ts`,
  `e2e/browser/viewed-agent-timelines.spec.ts`).
- **Identity colors** (`styles/identity-colors.ts`) — a fixed ten-colour
  table for project icons, host badges, PR avatars, deliberately outside
  the theme palette so colour identifies rather than ranks.
- **Sidebar callouts** (`components/sidebar-callout.tsx`) for cross-cutting
  alerts (worktree setup, Rosetta, update available); page-level `<Alert>`
  (`components/ui/alert.tsx`) with `default/info/success/warning/error`;
  toasts (`components/toast-host.tsx`, `contexts/toast-context.tsx`).
- **Background jobs / context window meter** (`components/context-window-meter.tsx`).
- **Notifications**: OS notifications on desktop/web, real push on mobile
  (`packages/app/src/push-notifications/`).

---

## 6. Theming and appearance

`packages/app/src/styles/theme.ts` (863 lines) is the single token source;
`refs/paseo/docs/design.md` is the law.

- Semantic colour tokens: `surface0..4`, `surfaceDiffEmpty`,
  `surfaceSidebar`, `foreground`, `foregroundMuted`, `foregroundExtraMuted`,
  `border`, `borderAccent`, `accent`, `accentBright`, `accentForeground`,
  `primary`, `primaryForeground`, `destructive`, `interactionHighlight`,
  terminal ANSI palette, diff addition/deletion, status family, status-dot
  family.
- **Eight themes** (`THEME_OPTIONS`): `light`, `dark`, `auto`, plus dark
  variants `zinc`, `midnight`, `claude`, `ghostty`, `pureBlack`; plus a
  plugin-theme extension point. `Cmd+Alt+T` cycles.
- Typography: three independent size settings — interface (`uiBaseFontSize`,
  14 web/desktop, 15 native), content (`contentFontSize`, 15 — message
  bodies, composer, markdown, PR prose), code (`codeFontSize`, 12). UI and
  mono font families are user-selectable. Syntax theme selectable.
- Density/rhythm rules (§7), alignment rules (§8), a forbidden list (§14),
  and a canonical-surface table (§15) that names the reference
  implementation for every recurring pattern.
- Copy rules (§10): sentence case, no trailing periods on row titles or
  buttons, imperative buttons, present-participle in-flight labels
  ("Saving..."), direct error copy.

---

## 7. Keyboard shortcuts

`packages/app/src/keyboard/keyboard-shortcuts.ts` (1723 lines) — every
binding is data, rebindable (`shortcut-override-store.ts`), searchable
(`shortcut-help-search.ts`), grouped into five sections, and rendered in a
help dialog (`components/keyboard-shortcuts-dialog.tsx`, `Shift+?`).

| Section | Bindings (mac) |
| --- | --- |
| General | `Cmd+K` command center, `Cmd+P` search files, `Shift+?` shortcuts, `Cmd+,` settings, `Cmd+Alt+T` cycle theme |
| Workspaces | `Cmd+O` open project, `Cmd+N` new workspace, `Cmd+<digit>` jump to workspace, `Cmd+[` / `Cmd+]` prev/next workspace, `Cmd+Shift+P` pin, `Cmd+Shift+Backspace` archive |
| Tabs & Panes | `Cmd+T` new tab, `Cmd+Shift+A` new agent, `Cmd+Shift+T` new terminal, `Cmd+Shift+B` new browser, `Cmd+Shift+G` changes, `Cmd+Shift+E` files, `Cmd+W` close tab, `Cmd+Alt+<digit>` jump to tab, `Cmd+\` split right, `Cmd+Shift+\` split down, `Cmd+Shift+Arrow` focus pane, `Cmd+Alt+Shift+Arrow` move tab, `Cmd+Shift+W` close pane |
| Layout | `Cmd+B` left sidebar, `Cmd+E` Explorer sidebar, `Cmd+.` both sidebars, `Cmd+Shift+F` focus mode |
| Agent Input | `Cmd+L` focus composer, `Shift+Tab` cycle agent mode, `Cmd+Shift+D` voice, `Cmd+D` dictation, `Escape` interrupt, `Space` mute |

**Command center** (`packages/app/src/command-center/`): `Cmd+K`, a
registry with root / workspace / agent-control contribution scopes,
grouped results, file search, workspace actions.

---

## 8. Mobile / responsive

- **Compact-first** (`refs/paseo/docs/design.md` §9). The branching is one
  `useIsCompactFormFactor()` check per screen; list and detail are the same
  components in both layouts.
- **Mobile panels** (`refs/paseo/docs/mobile-panels.md`,
  `packages/app/src/mobile-panels/`): three mutually exclusive destinations
  — `agent-list` (left), `agent` (centre), `file-explorer` (right) — driven
  by *one* normalized position value (-1/0/1), so a panel and its backdrop
  can never disagree. Gesture arbitration with revision ownership.
- Compact Explorer is a full-screen overlay that closes after a file opens;
  wide-native uses a resizable inline dock.
- Tabs collapse on compact, panes split on desktop.
- Hidden tabs/workspaces stay mounted via `RetainedPanel` with an active
  signal so timers/polling stop without unmounting native subtrees.
- Narrow-desktop "concession": app navigation yields to content topology
  rather than adding a second compact breakpoint.
- Electron window-control obstruction is modelled explicitly (which top
  corners a surface occupies).

---

## 9. Empty / loading / error states

`refs/paseo/docs/design.md` §11 is prescriptive:

- Loading is inline by default (`<LoadingSpinner size={14}>` next to the
  thing); page-level loading is a centred large spinner; card-level loading
  is one short line, not a spinner; dropdown items own their pending state.
- Empty states are short noun phrases, centred, muted, one or two lines.
  At most one ghost button. "Illustrations and CTAs disguised as empty
  states are wrong."
- Inline errors: one sentence, `red[300]`, `xs`, under the field.
- Page alerts: `<Alert>`, one per region.
- Partial failure: bordered banner above the list, list still renders.
- **"Changing state must not move the layout."** Reserve the space the
  loaded state will need.
- Disabled = `opacity[50]`, never a colour change.

---

## 10. What smind cannot copy 1-to-1

| Paseo capability | Why it doesn't port | Closest web equivalent |
| --- | --- | --- |
| Native iOS/Android app | smind is a Go daemon + web UI; there is no RN client and ADR 0002 commits to Vite/React for `web/` | Responsive web UI + PWA; `docs/plans/active/relay-e2ee-mobile.md` already owns remote access |
| Real push notifications (`push-notifications/index.native.ts`) | Needs an app + APNs/FCM | Web Notifications API (already in smind) + Web Push via service worker if wanted later |
| Voice mode / dictation (`voice/`, `dictation/`) | Needs native audio engines; `expo-two-way-audio` is a custom native module | Out of scope; Web Speech API is the fallback if ever asked for |
| In-app browser tab (`desktop/browser/`) | Electron `<webview>` | An `<iframe>` tab is possible but of low value here |
| Electron window-chrome obstruction handling | No desktop shell yet (Roadmap Phase 4) | Skip until Phase 4 |
| Plugin system (`refs/paseo/docs/plugins.md`) | Large, and orthogonal to parity | Skip |
| Unistyles / Reanimated animation contracts | RN-specific | CSS transitions |

Everything else in this document is reachable from a React + Tailwind web
UI over smind's existing WebSocket API — some of it only after the
daemon's event vocabulary grows (see `gap-matrix.md`, dimension D).
