# Web UI/UX redesign: parity with paseo / deepseek-harness / cliproxyapi

## Context

The Phase 2 gate is "use smind, not Paseo, as the daily driver"
(`docs/plans/active/smind-dogfood.md`). Three dogfood sessions in, the
daemon side holds up — runs survive restarts, reconnect resyncs, permission
plumbing is correct end to end — and the thing standing between smind and
that gate is the web UI. This plan is the redesign that closes it.

Phase 1 of this work (research) is complete; its output is under
[`ui-redesign-parity/`](ui-redesign-parity/):

- [`audit-paseo.md`](ui-redesign-parity/audit-paseo.md) — the north star.
- [`audit-deepseek-harness.md`](ui-redesign-parity/audit-deepseek-harness.md)
- [`audit-cliproxyapi.md`](ui-redesign-parity/audit-cliproxyapi.md) —
  scope caveat: its UI source is not in `refs/`; audited from the
  management API it is built against.
- [`audit-smind-current.md`](ui-redesign-parity/audit-smind-current.md)
- [`gap-matrix.md`](ui-redesign-parity/gap-matrix.md) — 11 dimensions,
  per-dimension verdict and north star, ending in the top-10 ranked gaps.

This plan builds on, and does not re-litigate,
`docs/research/dual-mode-ui.md` (no mode switch; one task-scoped tabbed
canvas with an attention rail and a side dock), `docs/research/uiux-audit.md`
(whose P1/P2 items that are still open are folded in below), ADR 0004
(per-task tabs) and ADR 0006 (coarse, human-triggered commit flow).

**Scope boundary.** Two items (B1, D1) require daemon changes, not just
`web/`. They are called out explicitly and gated on maintainer sign-off
because they change the wire contract — AGENTS.md rule (d).

**Honest non-goals.** Paseo capabilities that do not port to a Go daemon +
web UI are listed in `audit-paseo.md` §10 with their closest web
equivalent: native mobile push (→ web Notifications, already present; Web
Push only if asked), voice/dictation (→ dropped), in-app browser tab (→
dropped), Electron window-chrome handling (→ Phase 4), the plugin system
(→ dropped). Subagents are deferred rather than dropped: smind has no
subagent concept at any layer yet, so UI parity there is meaningless until
the daemon grows one.

---

## Acceptance Criteria

Items are independently landable. Each is one PR-sized chunk with its own
criteria and test scenarios. **Track** and **Depends on** are stated per
item so several agents can work concurrently in separate worktrees; see
[Tracks and dependencies](#tracks-and-dependencies) for the graph.

### Item 1 — Design tokens, dark mode, theme switching
*Track A. Depends on: nothing.*

- `web/packages/ui/src/index.css` gains a documented two-layer token set:
  a static palette layer and a semantic layer, following
  `audit-deepseek-harness.md` §5's layering and `audit-paseo.md` §6's
  vocabulary. The semantic layer must at minimum add what smind has no
  token for today: `surface0..3`, `foreground-muted`,
  `status-{success,danger,warning,running}` for pills and a separate,
  higher-chroma `status-dot-*` family for dots (the reasoning is
  `refs/paseo/docs/design.md` §13 — a 6px dot at pill chroma reads dimmer
  than the text beside it, which is backwards).
- A `useTheme` hook + provider persists `light | dark | system` to
  `localStorage`, resolves `system` via `prefers-color-scheme`, applies
  the `dark` class on `<html>`, and reacts to OS changes while on
  `system`.
- A synchronous inline script in `web/packages/ui/index.html` applies the
  stored preference **before first paint** — no flash of the wrong theme
  (`audit-deepseek-harness.md` §5).
- Every existing pane renders correctly in dark: CodeMirror, diff2html and
  xterm all take their palette from the app tokens rather than their own
  defaults, so the three no longer disagree
  (`audit-smind-current.md` §6).
- The stray chromatic `--sidebar-primary` in `.dark`
  (`index.css:112`, flagged in `docs/research/uiux-audit.md` §2.5) is
  resolved deliberately, not left as a shadcn leftover.
- A theme control exists somewhere reachable (sidebar footer is fine
  until Item 13 lands a settings screen).

### Item 2 — Shared UI primitives + written design rules
*Track A. Depends on: Item 1.*

- New primitives under `components/ui/`, each used by at least the surfaces
  named: `PaneHeader` (title + right action slot; adopted by
  `task-detail`, `file-editor-pane`, `diff-viewer-pane`, `terminal-pane`,
  closing `uiux-audit.md` §4 P1 item 5), `StatusDot`, `StatusBadge`,
  `Alert` (`default | info | success | warning | error`), `EmptyState`,
  `InlineSpinner`, and a `Toast` host.
- One padding scale across panes; one empty/error copy convention
  (sentence case, short noun phrase) applied to all six panes audited in
  `uiux-audit.md` §2.2.
- `file-editor-pane.tsx`'s hand-rolled buttons move to the shared `Button`
  (`uiux-audit.md` §4 P1 item 7); Commit, Stage and the conflict banner's
  Reload/Overwrite gain in-flight labels/disabled states
  (§4 P1 item 8).
- **Layout stability**: a surface whose state changes (skeleton → content,
  badge arriving, count resolving) must not move its neighbours. The
  reserved-space rule from `refs/paseo/docs/design.md` §11 applies to at
  least the sidebar row and the run entry.
- `docs/design.md` is written: token vocabulary, when to use which
  primitive, copy rules, state rules, and a canonical-surface table naming
  the reference implementation for each recurring pattern. It is short and
  descriptive of what actually exists — not aspirational.

### Item 3 — Routing and persisted UI state
*Track A. Depends on: nothing (touches `App.tsx` — coordinate with Item 6).*

- The selected task is addressable. A URL identifies workspace + task +
  active tab; reload restores all three; browser back/forward work. Deep
  links from a future notification or CLI (`smind task open`) are then
  possible. Hash routing is acceptable — the daemon serves a single
  embedded SPA (`internal/server/web.go`); no server-side route table
  should be required.
- Per-task open tabs persist across reload
  (`hooks/use-task-tabs.ts` gains a storage layer) — today every open file
  tab is lost (`audit-smind-current.md` §1).
- Sidebar width (already persisted) and, later, pane sizes use the same
  storage mechanism.
- Restoring a task that no longer exists (archived, deleted) degrades to
  the empty state rather than erroring.

### Item 4 — Keyboard action registry + shortcuts help
*Track A. Depends on: Item 2 (dialog/primitives).*

- A data-driven action registry (`keyboard/actions.ts` +
  `keyboard/shortcuts.ts`) modelled on `audit-paseo.md` §7: each binding is
  `{id, section, label, combo, action}`, matched against a normalized key
  event, with platform variants (`Cmd` on mac, `Ctrl` elsewhere).
- Initial bindings, chosen to match Paseo where the action exists:
  `Cmd+B` toggle sidebar (already exists — moves into the registry),
  `Cmd+K` command palette (Item 5), `Cmd+L` focus composer,
  `Cmd+W` close current tab, `Cmd+Alt+<digit>` jump to tab,
  `Cmd+[`/`Cmd+]` previous/next task, `Escape` interrupt the running run,
  `Cmd+Alt+T` cycle theme, `Shift+?` shortcuts help.
- A shortcuts help dialog lists every binding grouped by section.
- The tab-close `×` becomes keyboard-operable (`tabIndex` + Enter/Space),
  closing `uiux-audit.md` §4 P1 item 9.
- Bindings do not fire while focus is in a text input, CodeMirror, or
  xterm, except where explicitly marked global.

### Item 5 — Command palette
*Track A. Depends on: Item 4.*

- `Cmd+K` opens a searchable palette with contributions grouped by source,
  following `audit-paseo.md` §7's command-center shape: tasks (switch to),
  workspaces, actions (new task, new workspace, open accounts, toggle
  theme, open a tab kind), and files in the selected task's worktree.
- Keyboard-only operation: type to filter, arrows to move, Enter to run,
  Escape to close returning focus to the previous element
  (`audit-deepseek-harness.md` §6's focus rule).
- Contributions are registered, not hardcoded in the palette component, so
  later items can add entries without editing it.

### Item 6 — Split panes and the side dock
*Track A. Depends on: Item 3 (shares `App.tsx` layout + persistence).*

- The main content area supports **one split**: a primary pane and an
  optional side pane, resizable, persisted per task. This is the "side
  dock" from `docs/research/dual-mode-ui.md` that ADR 0004's tab registry
  was the prerequisite for.
- "Open to side" is available on file tabs, the diff tab and the terminal
  tab; a tab can be moved between panes; closing the last tab in the side
  pane removes it.
- Placement follows `audit-paseo.md` §1's intent model, reduced to the two
  cases smind needs: explicit "Open to side" moves the tab (`pane`);
  implicit opens (clicking a file in the tree, clicking a file path in a
  tool call) use `prefer` — they open in the side pane if one exists but
  never yank a tab the user placed.
- The detach-not-stop contract is preserved: moving a tab between panes
  must not stop a run or close a terminal session
  (`audit-smind-current.md` §10.1).

### Item 7 — Structured timeline events (daemon + wire)
*Track B. Depends on: maintainer sign-off + an ADR. **Blocks Items 8, 9.***

> **Gate.** This changes `internal/taskrunner`'s event vocabulary and the
> `run.attach`/`run.logs` wire shape. Per AGENTS.md rule (d) it is not an
> agent's decision. Write `docs/decisions/0008-structured-run-events.md`
> and get sign-off before implementing.

- `internal/taskrunner.Event` grows beyond its four types
  (`event.go:7-29`) to carry, at minimum: assistant text, user message,
  reasoning/thinking, and **tool call** (id, tool name, input, status,
  result/error) — the subset of `audit-paseo.md` §2's seven-kind union
  that both supported providers can actually produce. The data already
  exists upstream: `Event.Raw` holds the provider message
  (`claudecode.Message` / the ACP update) and is discarded at the wire
  boundary today.
- `run.logs` and `run.attach` carry the new types; **older payloads still
  decode** — a client that only understands `chunk`/`done` keeps working,
  and a new client renders a run recorded before this change as it does
  today (text only).
- Both providers are covered: `claude-native` (Agent SDK messages) and the
  ACP path used by `glm`/`codex-native`.
- Persisted run history (`internal/runs`) round-trips the new event types
  across a daemon restart.

### Item 8 — Timeline renderer
*Track B. Depends on: Item 7, Item 2.*

- `task-detail.tsx`'s single `<pre>` per run is replaced by a typed item
  list: user message, assistant message (markdown, via the already-present
  `react-markdown` + `remark-gfm`), reasoning (collapsed by default), tool
  call (Item 9), and terminal status.
- Runs group into turns with a footer carrying elapsed time and a copy
  action (`audit-paseo.md` §2).
- **Auto-follow the tail**: the transcript sticks to the bottom while
  streaming and releases when the user scrolls up, with a "jump to latest"
  affordance — today there is no scroll handling at all
  (`audit-smind-current.md` §2).
- Streaming performance: appending a chunk must not re-render the whole
  transcript. A long run (≥2000 items) stays interactive; measure and
  state the approach taken (windowing or memoized rows), following
  `audit-deepseek-harness.md` §2's "streaming tail isolation".
- Rendering is resilient to a malformed or unknown event type: it renders
  a fallback row, never throws.

### Item 9 — Tool-call cards
*Track B. Depends on: Item 8.*

- A registry keyed by **wire tool name** with a generic fallback card, per
  `audit-deepseek-harness.md` §2 — adding a tool renderer must not require
  editing a central switch.
- Shared render intents implemented at minimum for: `terminal` (command +
  output), `read` (path + range), `edit`/`write` (inline diff), `search`
  (query + hit count). Unknown tools classify into one of these by shape
  where possible, else generic.
- Each card shows: icon, display name, one-line summary, lifecycle status
  (running / success / failure), and expands for detail.
- A tool call carrying a file path is click-through: it opens that file's
  tab (in the side pane if Item 6 has landed, using `prefer`).
- A detail-level control (`detailed | overview`) collapses consecutive
  tool calls into a group, matching `audit-paseo.md` §2's
  `toolCallDetailLevel`.

### Item 10 — Composer v2
*Track B. Depends on: Item 2. (Independent of Item 7 — can land first.)*

- Multiline autogrowing textarea with Enter to send and Shift+Enter for a
  newline, capped in height then scrolling, IME-safe
  (`audit-deepseek-harness.md` §2).
- A **composer toolbar** replaces the two bare `<select>`s: provider,
  approval policy, and (where `provider.list` can report them) model —
  as proper controls with labels, not unlabelled native selects.
- Draft persistence per task, surviving task switches and reload.
- Stop/interrupt moves into the composer (today it is only on the run
  card) and is bound to `Escape` via Item 4.
- A disabled composer states *why* in its placeholder rather than silently
  greying out (`audit-deepseek-harness.md` §2's block contract): no
  connection, no task selected, run in flight.
- The empty state (no task selected) becomes a usable entry point rather
  than a dead-end sentence — at minimum "create a task here", reusing the
  existing `CreateTaskDialog`.

### Item 11 — Permission UX v2
*Track B. Depends on: Item 2. (Independent of Item 7.)*

- Option buttons are styled by their ACP `kind`
  (`allow_once | allow_always | reject_once | reject_always`), which the
  wire already carries (`lib/types.ts:144-154`) and the UI currently
  ignores. Destructive/deny reads as deny; the recommended action reads as
  primary.
- The card shows **what is being requested**, not just `summary`: for a
  command, the command line; for an edit, the diff. Where the daemon can
  supply it (this may ride on Item 7's richer events, but must degrade
  gracefully without it).
- Keyboard: the pending card is focusable and its options are reachable
  and activatable without a mouse.
- A structured **question form** variant (multi-question, single/multi
  select, free-text "other", skip) per
  `audit-deepseek-harness.md` §2, rendered when the request carries that
  shape.
- A **plan review** variant: plan rendered as markdown with
  `Chat about it / Refuse / Approve`.
- The existing guarantees do not regress: pinned above the composer,
  cleared by the `permission_resolved` event from any connection, never by
  local click alone (`audit-smind-current.md` §2).

### Item 12 — Sidebar signal
*Track D. Depends on: Item 2. (Fully realised by Item 16.)*

- Task rows carry real state: run status dot using the Item 1 status-dot
  tokens (running pulses), branch name, and changed-file count.
- The single attention dot becomes distinguishable by reason — the three
  reasons already exist (`error | finished | permission`,
  `hooks/use-task-attention.ts`) and today all render identically.
- Workspace and space rows carry an aggregate status derived from their
  tasks (`audit-paseo.md` §5's workspace status bucket).
- A search/filter field over the tree, following
  `audit-deepseek-harness.md` §3's collapsed-search interaction: a
  non-blank query replaces the tree with a flat result list; an outside
  click collapses only an empty query.
- Rows do not reflow when a badge or count arrives (Item 2's layout-stability
  rule).

### Item 13 — Settings screen
*Track D. Depends on: Items 1, 2. (Item 3 if settings gets its own route.)*

- A settings surface with a section list, following
  `audit-paseo.md` §4's list+detail shape. Sections are registered, not
  hardcoded, so Items 14/15 add their own.
- **Appearance** section ships with it: theme, interface font size,
  content font size, code font size (the three-size split from
  `audit-paseo.md` §6 — it is the one that actually matters for an app
  that mixes chat prose, UI chrome and code).
- **General**: default provider and default approval policy for new runs,
  notification preference (moving the existing bell button's state here).
- Preferences persist client-side (`localStorage`) unless and until a
  daemon-side settings API exists; say so in the UI rather than implying
  they sync.

### Item 14 — Accounts v2
*Track D. Depends on: Item 2. Richer with Item 13.*

- Account rows carry what an operator actually needs when a run dies,
  following `audit-cliproxyapi.md` §2: `status` + `status_message`,
  token expiry / last refresh, **`next_retry_after`** (when a
  rate-limited account recovers), and success/failure counters — each one
  gated on the daemon being able to report it; a field the daemon cannot
  supply is omitted, not faked.
- The OAuth flow becomes a real state machine — start → show URL → poll
  status → **cancel** — rather than a fire-and-forget button
  (`audit-cliproxyapi.md` §3).
- Per-account actions: disable/enable, remove, and a per-account label.
- The `ProviderInfo.id` vs `accountProvider` seam and the CLI-only
  `xai`/`antigravity` providers (documented in `accounts-dialog.tsx` and
  `docs/plans/completed/task-permission-ux.md` Item 7d) are either closed
  or explicitly surfaced — not left silently inconsistent.

### Item 15 — Quota / usage surface
*Track D. Depends on: Item 14.*

- Per-account quota and rolling-window usage rendered as bars, following
  `audit-paseo.md` §4's provider-usage presentation over the data shape in
  `audit-cliproxyapi.md` §2 (`quota`, `model_quotas`, `recent_requests`).
- Gated on `internal/quota` being able to report it; if it cannot, this
  item reduces to a daemon-side scoping note and does not ship UI that
  invents numbers.

### Item 16 — Daemon lifecycle events for workspace/space/task
*Track D. Depends on: maintainer sign-off (wire change, smaller than Item 7).*

> **Gate.** Adds event topics to `internal/wsapi/events.go` (which today
> has exactly three: `task.status`, `run.status`, `permission.pending`).
> Additive, so a shorter ADR than Item 7 — but still rule (d).

- New topics covering create/update/archive/delete for workspace, space and
  task. `hooks/use-daemon-events.ts` and `useWorkspaceTree` consume them,
  replacing the local `refresh()`-after-mutation pattern
  (`app-sidebar.tsx:97-100`).
- A task created in another browser tab, or by the CLI, appears in this
  tab's sidebar without a reload.
- Subscription follows ADR 0005's existing topic-subscription contract;
  an unknown topic still errors.

### Item 17 — File explorer and editor polish
*Track C. Depends on: Item 2.*

- File-type icons in the tree and on tabs.
- Git status decoration in the tree (modified / added / untracked),
  sourced from the `task.files` data the diff pane already fetches.
- A dirty indicator on the file tab itself, not only inside the editor.
- Context actions on tree rows (at minimum: reveal in diff, copy path,
  open to side once Item 6 lands).
- CodeMirror's theme follows the app theme (also covered by Item 1 —
  whichever lands first owns it).

### Item 18 — Quick file open
*Track C. Depends on: Item 5 (registers as a palette contribution) or
standalone if Item 5 slips.*

- Fuzzy file search over the selected task's worktree, opening the match
  as a file tab. Paseo binds this to `Cmd+P` (`audit-paseo.md` §7).
- Backed by `file.list` walking, or a new daemon-side search RPC if
  walking proves too slow — measure before adding an RPC.

### Item 19 — Diff / review v2
*Track C. Depends on: Item 2.*

- A whole-diff view alongside the existing per-file list, and a
  side-by-side / unified toggle.
- Review comments: per-line draft comments held locally, submitted as a
  single prompt back to the agent — the mechanism
  `docs/research/dual-mode-ui.md` recommended and `audit-paseo.md` §2
  implements as persisted review drafts. Draft state survives collapsing a
  file and switching tabs.
- A diff stat (files / +lines / -lines) surfaced outside this pane, so the
  sidebar (Item 12) and composer can show it.
- Per-hunk staging stays out of scope — ADR 0006 deliberately collapses
  staged/unstaged/untracked into one base→worktree diff.

### Item 20 — Terminal v2
*Track C. Depends on: Item 2.*

- More than one terminal per task, each its own tab.
- An activity indicator on an inactive terminal tab
  (`audit-paseo.md` §2).
- Scrollback size as a setting (Item 13) and a copy/paste affordance.
- The existing detach-not-close contract is preserved, including the
  `interrupted` status path added by the daemon-restart work.

### Item 21 — Responsive / compact layout
*Track A. Depends on: Items 3, 6, 10.*

- A compact layout below the existing 768px breakpoint, following
  `audit-paseo.md` §8's model but implemented in CSS rather than gestures:
  three mutually exclusive destinations — task list, task pane, files —
  with one selection value so a panel and its backdrop can never disagree.
- The composer, permission card and timeline are usable one-handed on a
  phone-sized viewport; touch targets meet a stated minimum.
- Compact is verified at a declared set of viewport sizes, not assumed.
- This is a prerequisite for `docs/plans/active/relay-e2ee-mobile.md`
  being useful: reaching the daemon from a phone is worth little if the
  UI is desktop-only.

---

## Test Scenarios

All UI scenarios are Vitest + jsdom + Testing Library against the existing
`FakeWsClient` / `FakeSocket` fakes (`web/packages/ui/src/test/`), in the
style already established by `App.test.tsx`, `task-detail.test.tsx` and
`diff-viewer-pane.test.tsx`. Daemon scenarios are Go tests. Every item
must also leave `bunx tsc -b`, `task test` and `task lint` clean, and
`task build` succeeding (restore `internal/server/dist/.gitkeep` if Vite's
`--emptyOutDir` wipes it — a known recurring step).

**Item 1 — theming**
- `useTheme` defaults to `system`; with `matchMedia` reporting dark, the
  `dark` class is on `<html>`; flipping the media query flips the class
  while on `system` and does not while pinned to `light`.
- The preference round-trips through `localStorage` across a remount.
- The pre-paint script sets the class from stored state with no React
  mounted (unit-test the extracted function, plus an assertion that
  `index.html` embeds it).
- A snapshot-ish assertion that no component hardcodes a hex/oklch colour
  outside `index.css` (a grep-based test is acceptable and cheap).

**Item 2 — primitives**
- `PaneHeader` renders title + action slot; all four panes use it
  (assert by role/testid in each pane's existing suite).
- `Alert` renders each variant with the right role (`status` / `alert`).
- `EmptyState` renders copy and an optional single action.
- Layout stability: a sidebar row's measured box does not change when its
  badge prop goes from absent to present (assert reserved-space markup,
  not pixels — jsdom has no layout).
- In-flight: clicking Commit shows "Committing…" and disables; the
  conflict banner's Reload disables while in flight.

**Item 3 — routing/persistence**
- Mounting at a task URL selects that task and its active tab.
- Selecting a task updates the URL; back returns to the previous task.
- Opening two file tabs, remounting the app, restores both and the active
  one.
- A URL naming a task that `task.list` no longer returns lands on the
  empty state without throwing.

**Item 4 — keyboard**
- Each registered binding fires its action for the right key event and
  not for a near-miss (wrong modifier, wrong platform variant).
- No binding fires while focus is inside an `<input>`, `<textarea>`, or an
  element marked as an editor surface.
- `Shift+?` opens the help dialog listing every registered binding.
- The tab-close `×` activates on Enter and on Space and closes only that
  tab (extends the existing `close-only-that-tab` test).

**Item 5 — command palette**
- `Cmd+K` opens; typing filters across sources; Enter runs the highlighted
  entry; Escape closes and returns focus to the previously focused element.
- Arrow navigation wraps and skips group headers.
- A registered contribution appears without the palette component being
  modified (register one in the test).

**Item 6 — splits**
- "Open to side" on a file tab creates the side pane with that tab in it;
  the primary pane keeps its own active tab.
- Moving a *terminal* tab to the side pane does not call `terminal.close`
  and does not re-`create` — the same detach-not-stop assertion style as
  `terminal-pane.test.tsx`.
- Implicit open with an existing side pane uses it; implicit open with a
  tab already placed in the primary pane does **not** move it.
- Closing the last side tab removes the pane; sizes persist across remount.

**Item 7 — structured events (Go)**
- `internal/taskrunner`: a Claude Agent SDK tool-use message produces a
  tool-call event with id/name/input; its result message completes the
  same id; an error result marks it failed.
- The ACP path produces equivalent events for `glm`.
- `internal/wsapi`: `run.logs` for a run recorded **before** this change
  still decodes and returns text events (back-compat).
- `internal/runs`: the new event types round-trip across a store
  reopen — mirroring the existing run-history persistence tests.
- A client subscribing with the old expectations receives nothing it
  cannot parse (assert the additive shape).

**Item 8 — timeline renderer**
- A run's events render as distinct rows by kind; assistant markdown
  renders as HTML (a fenced code block becomes `<pre><code>`).
- A streaming chunk appends without remounting earlier rows (assert with a
  render-count probe on a memoized row).
- Auto-follow: with the container scrolled to the bottom, a new chunk keeps
  it there; after simulating a scroll up, a new chunk does **not** move it
  and the jump-to-latest affordance appears.
- An event with an unrecognised `type` renders a fallback row and does not
  throw (this is the regression test for adding kinds later).
- A run with 2000 events renders within a stated budget (a timing
  assertion with generous headroom — the point is to catch a quadratic
  regression, not to benchmark).

**Item 9 — tool-call cards**
- A registered tool name renders its own card; an unregistered one renders
  the generic card.
- `terminal`/`read`/`edit`/`search` intents each render their expected
  distinguishing element (command line, path+range, diff, query+count).
- Status transitions running → success and running → failure update the
  card in place (same card id, not a second card).
- Clicking a card with a file path opens that path's tab, in the side pane
  when one exists.
- `overview` detail level groups three consecutive tool calls into one row;
  switching back to `detailed` restores three.

**Item 10 — composer**
- Enter submits; Shift+Enter inserts a newline; submitting clears the
  draft; the textarea grows to its cap then scrolls.
- A draft survives switching to another task and back, and a remount.
- Stop in the composer calls `run.stop` for the live run and nothing else.
- With no connection / no task / a run in flight, the placeholder states
  the reason.
- Provider/policy/model selections are still sent on submit exactly as the
  existing `run.start`-then-`run.attach` tests assert (no regression).

**Item 11 — permissions**
- Each `kind` renders its expected variant; a request with no recognised
  kind still renders every option.
- The card is reachable by keyboard and an option activates on Enter.
- A resolution arriving over the socket (not from a local click) clears the
  card — this test exists today and must keep passing.
- A question-form-shaped request renders the form; submitting sends one
  structured answer batch; "skip" sends the blank shape.
- A plan-shaped request renders markdown and three actions.

**Item 12 — sidebar**
- A task with a running run shows the running dot; error / finished /
  permission attention each render a visually distinguishable marker
  (assert by testid/variant attribute, not colour).
- A workspace whose task has an error shows the aggregate marker.
- Typing in the filter replaces the tree with a flat result list; clearing
  it restores the tree with the previous expansion state.
- Selecting a task still clears its attention (existing test, unchanged).

**Item 13 — settings**
- Each section renders from the registry; registering a new one in the
  test makes it appear without editing the shell.
- Changing theme/font-size updates the document and persists.
- The default-provider preference is applied to a newly opened composer.

**Item 14 — accounts**
- A provider whose daemon response includes `status_message` /
  `next_retry_after` renders them; one without renders neither and does
  not render an empty row.
- OAuth: start renders the URL and begins polling; a success response
  closes the flow and refreshes the list; cancel stops polling and calls
  the cancel RPC.
- Disable/enable and remove each call the right RPC and reflect optimistic
  state that reverts on failure.

**Item 15 — quota**
- A quota payload renders its bar with the right fill and label; a missing
  payload renders nothing (not a zeroed bar).

**Item 16 — lifecycle events (Go + UI)**
- `internal/wsapi`: subscribing to each new topic delivers the event on
  create/update/archive/delete; an unknown topic still errors (ADR 0005).
- UI: a `task.created` event for the open workspace inserts the row
  without a refetch; an `archived` event removes it; an event for a
  different workspace does not disturb the tree.
- The acting client does not double-insert (local optimistic update plus
  the event).

**Item 17 — files**
- A `.go` path renders its icon; an unknown extension renders the generic
  one.
- A modified file shows its git decoration; an untracked one shows a
  different marker.
- A dirty buffer marks its tab; saving clears it.

**Item 18 — quick open**
- Typing a fuzzy query ranks the expected path first; Enter opens it as a
  file tab; Escape closes without opening.

**Item 19 — diff/review**
- Whole-diff view renders every changed file; the side-by-side toggle
  switches rendering mode.
- A draft comment survives collapsing the file and switching tabs;
  submitting sends one prompt containing every draft and clears them.
- The diff stat is exposed and matches the file list.
- Existing stage/viewed/commit/PR tests keep passing unchanged.

**Item 20 — terminal**
- Two terminal tabs in one task create two sessions; switching between
  them detaches rather than closes (existing assertion style).
- An inactive terminal receiving data marks its tab; activating clears it.

**Item 21 — responsive**
- Below the breakpoint, exactly one destination is rendered as active and
  the other two are inert.
- Switching destinations is idempotent for the current destination.
- The composer and permission card render without horizontal overflow at
  the declared narrow viewport (assert layout-affecting classes/props,
  since jsdom has no layout — plus a manual check recorded in Validation).

---

## Tracks and dependencies

Four tracks, runnable concurrently in separate worktrees. **Track A's
Items 1 and 2 are the shared foundation — they should land first**, since
Tracks B, C and D all build on those tokens and primitives. After that the
tracks touch mostly disjoint files.

```
Track A — shell, theming, navigation      (owns index.css, App.tsx, components/ui/)
  1 tokens+dark ──┬─> 2 primitives ──┬─> 4 keyboard ──> 5 palette
                  │                  │
                  └──────────────────┴─> 13 settings (Track D)
  3 routing+persistence ──> 6 splits ──┐
                                       ├─> 21 responsive
  10 composer (Track B) ───────────────┘

Track B — agent timeline & composer       (owns task-detail.tsx, hooks/use-run-timeline.ts)
  [GATE: ADR] 7 structured events ──> 8 timeline renderer ──> 9 tool-call cards
  2 ──> 10 composer v2
  2 ──> 11 permission UX v2

Track C — workspace panes                 (owns file-*, diff-*, terminal-* panes)
  2 ──> 17 files polish
  2 ──> 19 diff/review v2
  2 ──> 20 terminal v2
  5 ──> 18 quick open        (standalone fallback if 5 slips)

Track D — sidebar, settings, accounts     (owns app-sidebar.tsx, accounts-dialog.tsx)
  [GATE: ADR] 16 lifecycle events ──┐
  2 ──> 12 sidebar signal ──────────┘
  1,2 ──> 13 settings ──> 14 accounts v2 ──> 15 quota
```

**Cross-track coordination points** (the only places two tracks touch the
same file):

- **`App.tsx`** — Items 3 and 6 both restructure it. Land 3 before 6.
  Track B/C/D items should not need to touch it beyond adding a tab kind.
- **Item 2's primitives** are consumed by almost everything. Land it early
  and treat its API as stable.
- **Item 6 (splits)** changes how a tab is opened; Item 9's click-to-file
  and Item 17's "open to side" both degrade gracefully without it — they
  must not block on it.
- **Item 7 (events)** blocks only Items 8 and 9. Items 10 and 11 are on
  Track B but independent of it, so Track B has work while the ADR is
  pending.

**Suggested ordering by dogfood impact** (matching `gap-matrix.md`'s
top-10): 1, 2 → 7 (ADR in parallel), 10, 4 → 8, 3, 12, 16 → 9, 5, 11,
13, 6 → 14, 19, 17 → 15, 18, 20, 21.

---

## Decisions

- **Paseo is the north star; the others are consulted per dimension.**
  Where they conflict, Paseo wins — it is the product being replaced
  (`docs/plans/active/smind-dogfood.md`). Two deliberate exceptions, both
  recorded in `gap-matrix.md`: tool-call rendering follows
  deepseek-harness's **keyed-registry-by-wire-tool-name** shape (Paseo's
  equivalent is more entangled with its provider layer), and account-row
  richness follows **cliproxyapi**, which has by far the best credential
  model of the three.
- **No mode switch.** Re-affirming `docs/research/dual-mode-ui.md`: one
  task-scoped tabbed canvas, an attention rail, one optional side dock.
  Nothing in this plan introduces an "Agents mode" / "Editor mode" toggle.
- **ADR 0004 (per-task tabs) and ADR 0006 (coarse commit flow) stand.**
  Item 6 builds the side dock *on* the task-scoped tab keys; Item 19
  explicitly keeps per-hunk staging out of scope.
- **Two items are gated on maintainer sign-off** (AGENTS.md rule (d)):
  Item 7 (run-event vocabulary + wire shape) and Item 16 (new event
  topics). Both need an ADR before implementation. Item 7's is the
  material one — it decides whether smind's timeline is text or structure,
  and everything in Track B downstream of it follows from that answer.
- **Additive wire changes only.** Item 7 and Item 16 must leave an older
  client working and older persisted runs readable. smind has already been
  bitten once by a store migration (`task-permission-ux.md` Item 7's
  pre-#93 migration).
- **The existing correctness guarantees are non-negotiable** and are
  listed as such in `audit-smind-current.md` §10: detach-not-stop,
  reconnect-resync, cross-connection permission resolution, conditional
  `file.write`, daemon-derived provider list, task-scoped tab keys. Every
  item's test scenarios re-assert the ones it could plausibly break.
- **Preferences are client-side until proven otherwise.** Item 13 persists
  to `localStorage` and says so. A daemon settings API (the
  deepseek-harness model — `$DSH_HOME/settings.yaml` via a host RPC) is a
  reasonable later step but is not in scope here and should not be
  half-built.
- **Subagents are deferred, not dropped.** smind has no subagent concept
  in `internal/taskrunner` or `internal/runs`; a subagent track would be
  UI for data that does not exist. Revisit after Item 7.
- **Responsive work (Item 21) is scheduled, not optional.**
  `docs/plans/active/relay-e2ee-mobile.md` makes the daemon reachable from
  a phone; a desktop-only UI would waste that.
- **Performance is asserted, not assumed.** Item 8's long-run scenario and
  Item 18's measure-before-adding-an-RPC rule exist because both
  references treat streaming and diff performance as tracked concerns with
  their own test suites (`refs/paseo/packages/app/e2e/browser/agent-stream-smoothness.spec.ts`,
  `diff-performance.spec.ts`).
- **ARIA-tree snapshot testing is worth adopting** for the timeline and
  permission surfaces (`audit-deepseek-harness.md` §7): assert the
  accessibility tree rather than the DOM, which keeps tests readable and
  keeps accessibility from silently regressing. Optional per item, not
  mandated.

**Items 1–2 (landed)** — where the plan was ambiguous and what was
decided; full rationale is in `docs/design.md`'s own Decisions section,
this is the pointer:

- **`surface-0..3` alias the existing static token scale** rather than a
  second independent set of hex values — the plan named the vocabulary
  (`audit-paseo.md` §6) but not the implementation shape, and smind's
  existing background/card/muted/accent scale already satisfies the
  elevation need with no known contrast problems.
- **`status-danger` aliases `--destructive`** rather than introducing a
  second red — smind has exactly one flavor of "bad" today, unlike Paseo,
  which distinguishes a PR/CI-state red from a destructive-action red.
- **`status-running`'s hue is new**, since Paseo's own status-family text
  tier has no running color (only its dot tier does) and the plan asks
  for one at both tiers.
- **`useTheme()` outside a `<ThemeProvider>` returns a fully-functional,
  non-reactive default instead of throwing** — this codebase's component
  tests render one component directly rather than the whole app tree
  (confirmed while landing this item: `terminal-pane.test.tsx`,
  `app-sidebar.test.tsx` etc. all do this already), so a strict
  throw-without-provider design would have forced wrapping dozens of
  existing test call sites. `main.tsx`'s real `ThemeProvider` is what
  every reactive behavior in this item's own test suite exercises.
- **CodeMirror, diff2html and xterm each needed a different theming
  mechanism**, not one shared one: CodeMirror's chrome uses live
  `var(--x)` references (no JS reactivity needed), diff2html's own
  shipped dark variable pairs are activated by overriding its *base*
  variable names to point at smind's tokens (its own dark-mode gate,
  `.d2h-dark-color-scheme`/`prefers-color-scheme`, doesn't line up with
  smind's explicit theme state), and xterm needs literal re-applied
  colors on every theme change (`lib/terminal-theme.ts`'s computed-style
  probe). Full terminal ANSI-16 theming is deferred to Item 20 — Item 1's
  bar was the chrome (background/foreground/cursor/selection), not a full
  palette, which is its own design decision.
- **`StatusBadge` and `Toast` ship with no consumer yet** — both are
  complete, tested primitives; Item 2's acceptance criteria only named
  required adopters for `PaneHeader`. Wiring either into a real surface is
  left to whichever later item first needs one.

**Item 17 (landed)** — where the plan was ambiguous and what was decided:

- **"Untracked" is not a status the wire can report.**
  `internal/workspace/git.go`'s `taskChangedFiles` diffs a *snapshot
  index* against the task's base commit, which reports a brand-new
  untracked file as `A` — so the three decorations the tree can honestly
  draw are added (`A`), modified (`M`) and deleted (`D`), and Item 17's
  "an untracked one shows a different marker" is satisfied as
  added-vs-modified. Any other git code (`R` for a rename) is carried
  through by the daemon rather than collapsed, and renders as its own
  letter in the muted tier instead of being mislabelled as one of the
  three. Teaching the daemon a real `untracked` status would be a wire
  change (rule (d)) for no dogfood gain.
- **Directory rows get a rolled-up decoration**, beyond what the item
  asked for: a folder whose descendants are all added reads as added, any
  mix reads as modified. Without it a change several levels down is
  invisible until you expand to find it, which is the thing tree
  decorations exist to prevent.
- **The dirty marker travels through a module-level store**
  (`lib/dirty-buffers.ts`), not React state. The two ends live in
  different subtrees — `FileEditorPane` is inside one tab's *content*,
  the tab strip is its *sibling* — so lifting the state would mean
  App.tsx threading a setter into every pane, and a context provider
  would mean Track C owning a provider in Track A's shell. The store is
  keyed by the tab key both ends already have.
- **"Reveal in diff" uses a latch, not an event**
  (`lib/diff-reveal.ts`). The diff pane is almost always *unmounted* at
  the moment the explorer asks (its tab isn't in front), so a live-only
  broadcast would be missed every time; the pane consumes the pending
  request on mount instead. App.tsx's part is one call to `activate` —
  it doesn't carry the payload.
- **"Open to side" is absent from the row menu, not disabled.** Item 6
  hasn't landed; a permanently-dead menu entry is worse than one that
  isn't there yet. `FileExplorerPane` already takes the callback shape it
  will need.
- **CodeMirror theme-follows-app was already closed by Item 1** (that
  item's own acceptance criteria covered it; the plan says "whichever
  lands first owns it"). Nothing was re-done here.

**Item 19 (landed)** — where the plan was ambiguous and what was decided:

- **The diff stat is derived client-side from `task.diff`**, not added as
  a daemon numstat RPC. The whole-diff view has to fetch that text
  anyway, so the stat is free on the surface that needs it most, and it
  stays additive — no wire change, no rule-(d) gate.
  `hooks/use-task-diff.ts` is the shared consumer point Item 19 asks for
  ("surfaced outside this pane"): Item 12's sidebar and Item 10's
  composer can mount it without the diff pane existing. **Caveat for
  Item 12**: a per-row stat for *every* task in the tree would mean one
  whole-diff fetch per row, which is the wrong tradeoff — the sidebar
  should use `task.files`'s count (already available via
  `hooks/use-task-file-status.ts`) and take `+`/`−` only for the selected
  task, or the daemon should grow a numstat, which is its own decision.
- **Per-line comments attach by click, not by an inline gutter widget.**
  diff2html renders to `innerHTML`, so there is no React tree to hang a
  per-line control off. One delegated listener on the container resolves
  the clicked row (`lib/diff-lines.ts`, which handles both output formats
  — unified's `.line-num1`/`.line-num2` divs and side-by-side's bare-text
  number cell), and the composer/draft list render *beside* the diff. The
  alternative — injecting React roots into diff2html's output per line —
  would couple the pane to that library's exact markup far harder than
  reading two class names does.
- **Drafts are persisted to `localStorage`, not just held in memory.**
  The plan's scenario is that a draft survives switching tabs, and
  switching tabs *unmounts the diff pane* (App.tsx's Radix Tabs don't
  force-mount inactive content), so component state couldn't satisfy it
  by construction. Persisting gets survival across a reload for free, and
  matches Paseo's own persisted review drafts (`audit-paseo.md` §2) and
  the plan's "preferences are client-side" stance.
- **Submitting starts a run with the daemon's first reported provider**,
  with no provider control of its own. The daemon-derived provider list
  is a non-negotiable guarantee (`audit-smind-current.md` §10), so the
  list is fetched rather than hardcoded — but choosing a provider *per
  review* is composer-toolbar work (Item 10), and a second provider
  `<select>` on this pane would be exactly the unlabelled-native-select
  pattern Item 10 exists to remove.
- **The view/layout toggles persist app-wide, not per task.** Which
  layout you read diffs in is a fact about the person, not the task.
  Item 13's settings screen is where they should eventually be
  *surfaced*; `lib/diff-prefs.ts` is the storage.
- **Drafts stay visible while their file is collapsed.** Item 19 only
  requires that they survive; hiding a surviving draft would make it look
  lost, which is the failure the criterion is guarding against.
- **Per-hunk staging stayed out of scope**, as the item states — ADR 0006
  collapses staged/unstaged/untracked into one base→worktree diff and
  nothing here changes that.

**Item 20 (landed)** — where the plan was ambiguous and what was decided:

- **"More than one terminal per task, each its own tab" needed a
  session↔tab binding**, not just a second `TerminalPane` mount. Without
  one, two terminal tabs' independent `terminal.list` calls could both
  see the same running session and both attach to it (rendering one
  shell twice) — the existing single-terminal code picked "the first
  running session" unconditionally, which was fine when only one tab
  could ever ask. `lib/terminal-sessions.ts` (a module-level store, same
  shape as `lib/dirty-buffers.ts`) records which tab owns which session
  id; a fresh attach excludes ids already claimed elsewhere.
- **The activity indicator requires the backgrounded pane to keep
  attaching**, which requires App.tsx's tab strip to `forceMount` a
  terminal `TabsContent` and hide it with CSS
  (`data-[state=inactive]:hidden`) instead of the default
  mount-only-when-active. This is the one place Item 20 reaches past its
  own files into `App.tsx` — additively, as the plan's cross-track rules
  ask: only terminal tabs force-mount, every other kind is unchanged.
  Without it, "an inactive terminal receiving data marks its tab" would
  be unsatisfiable — a truly unmounted pane can't observe data arriving
  at all, active or not. The existing detach-not-close contract is
  unaffected: the pane still aborts its `terminal.attach` and never calls
  `terminal.close` when it genuinely unmounts (tab closed, task
  switched); force-mounting only changes when *that* happens.
- **Copy asks the handle whether anything is reportable before disabling
  itself.** A `TerminalHandle` without `onSelectionChange` (this file's
  own test fake, historically) would otherwise wire a button that can
  never enable — worse than an always-enabled one that's a no-op on an
  empty selection.
- **Paste writes through `terminal.write`, never into the emulator's own
  buffer.** The PTY is what echoes; a local write would show the pasted
  text twice and never actually reach the shell.
- **Scrollback size is a stored preference** (`lib/terminal-prefs.ts`),
  read once at terminal-creation time, following the plan's "as a
  setting (Item 13)" wording — this item wires the storage and the
  read, not a new control in the terminal's own header (which would be
  the un-grouped-setting pattern Item 13 exists to collect).

**Track A hook to wire** (noted per the plan's cross-track coordination
rules): Item 17 makes two small, additive edits to `App.tsx` rather than
restructuring it — the tab strip renders `<TabLabel entry={entry} />`
from `tab-registry.tsx` instead of a bare title `<span>` (this is what
puts the file-type icon and the dirty marker on the tab), and
`FileExplorerPane` is passed `onRevealInDiff`, which activates the
`${taskId}:diff` tab. Item 20 adds a third: terminal `TabsContent`
entries `forceMount` and are hidden via
`data-[state=inactive]:hidden` instead of the default mount-only-when-
active, and `TerminalPane` takes `tabKey`/`active`/`onNewTerminal`. Items
3 and 6 should preserve all three when they restructure the shell — in
particular, Item 6's side dock must keep the force-mount behavior for
any terminal tab it moves, or a moved terminal would silently stop being
able to mark its own activity while backgrounded.

---

## Progress

Phase 1 (research + plan) — this commit:

- [x] Audit paseo (`ui-redesign-parity/audit-paseo.md`)
- [x] Audit deepseek-harness (`ui-redesign-parity/audit-deepseek-harness.md`)
- [x] Audit cliproxyapi (`ui-redesign-parity/audit-cliproxyapi.md`)
- [x] Audit smind current (`ui-redesign-parity/audit-smind-current.md`)
- [x] Gap matrix + top-10 (`ui-redesign-parity/gap-matrix.md`)
- [x] Plan doc with items, tracks and dependencies (this file)

Phase 2 (implementation) — not started:

- [x] Item 1: design tokens, dark mode, theme switching *(Track A)*
- [x] Item 2: shared primitives + `docs/design.md` *(Track A)*
- [ ] Item 3: routing + persisted UI state *(Track A)*
- [ ] Item 4: keyboard registry + shortcuts help *(Track A)*
- [ ] Item 5: command palette *(Track A)*
- [ ] Item 6: split panes / side dock *(Track A)*
- [x] Item 7: structured timeline events *(Track B — **ADR gate**)*
- [ ] Item 8: timeline renderer *(Track B)*
- [ ] Item 9: tool-call cards *(Track B)*
- [ ] Item 10: composer v2 *(Track B)*
- [ ] Item 11: permission UX v2 *(Track B)*
- [ ] Item 12: sidebar signal *(Track D)*
- [ ] Item 13: settings screen *(Track D)*
- [ ] Item 14: accounts v2 *(Track D)*
- [ ] Item 15: quota / usage surface *(Track D)*
- [ ] Item 16: daemon lifecycle events *(Track D — **ADR gate**)* — **backend done** (ADR 0009, `internal/wsapi`/`internal/workspace`); UI consumption (`hooks/use-daemon-events.ts`, `useWorkspaceTree`) still open
- [x] Item 17: file explorer / editor polish *(Track C)*
- [ ] Item 18: quick file open *(Track C)*
- [x] Item 19: diff / review v2 *(Track C)*
- [x] Item 20: terminal v2 *(Track C)*
- [ ] Item 21: responsive / compact layout *(Track A)*

---

## Validation

Phase 1 (research) — what was and wasn't actually done:

- **Paseo** audited from source: `refs/paseo/docs/{product,design,glossary,
  explorer-sidebar,mobile-panels,agent-lifecycle,hub}.md` read in full,
  plus `packages/app/src/{app,screens,navigation,workspace-tabs,composer,
  agent-stream,tool-calls,timeline,keyboard,command-center,styles,
  panels,hooks/use-settings}` read directly, and the ~180 e2e spec
  filenames under `packages/app/e2e/browser/` used as a feature inventory.
  The 4.4k-line `workspace-screen.tsx` was read for its imports and
  structure, not line by line — stated here rather than implied.
- **deepseek-harness** audited primarily from the `ui-*` package READMEs
  under `refs/deepseek-harness/packages/client/`, which are precise
  behavioural specs, cross-checked against the e2e test names and ARIA
  snapshots under `refs/deepseek-harness/apps/web/tests/`. Component
  source was not read; the READMEs are the better source and this is
  noted in the audit.
- **cliproxyapi**: the management UI source is **not in `refs/`** — it is a
  runtime-downloaded `management.html` from a separate repo
  (`internal/managementasset/updater.go:29-36`). The audit is derived from
  the management API it is built against
  (`internal/api/server_management.go`, `internal/api/handlers/management/`)
  and says so at the top. No claim is made about its visual design.
- **smind current** audited from code only. The dev server was **not**
  run — no browser was driven, no screenshot taken, nothing was verified
  visually. Every claim in that audit cites a file; claims that would need
  a running app (e.g. "the transcript does not auto-follow") are grounded
  in the absence of any scroll handling in the source, which is stated as
  the evidence rather than dressed up as observation.
- **Gap matrix** covers 11 dimensions (A–K) with a per-dimension verdict
  and north star, and ends with the ranked top-10 and the honourable
  mentions below the line.
- No code under `web/` or `internal/` was modified in this phase.

Phase 2 — to be filled in per item as it lands: which test(s) cover which
acceptance criterion, plus `task test` / `task lint` / `task build` results
and any manual check performed. Follow the per-item format used in
`docs/plans/completed/task-permission-ux.md`'s Validation section.

**Item 1 — design tokens, dark mode, theme switching:**

- Two-layer token set landed in `web/packages/ui/src/index.css`:
  `surface-0..3`, `foreground-muted`, `status-{success,danger,warning,
  running}` and `status-dot-{success,danger,warning,running}`, all
  re-exported via `@theme inline` as Tailwind utilities.
- `hooks/use-theme.tsx`'s `ThemeProvider`/`useTheme` persists
  `light|dark|system` to `localStorage` (`lib/theme.ts`), resolves
  `system` via `matchMedia`, applies/removes the `dark` class, and
  live-reacts to an OS change only while `system` is selected — covered
  by `hooks/use-theme.test.tsx` (4 scenarios matching the acceptance
  criteria verbatim) and `lib/theme.test.ts` (pure-function coverage).
- `index.html` gained a dependency-free pre-paint `<script>`
  (`src/test/index-html-bootstrap.test.ts` asserts it's embedded before
  `main.tsx` and shares `lib/theme.ts`'s storage key) — the file's
  previous hardcoded `class="dark"` (permanently-on dev-time dark mode,
  not a real toggle) is gone.
- CodeMirror, diff2html and xterm all take their chrome from app tokens
  (see `docs/design.md` §2 for the three different mechanisms this
  needed) — manually verified via `task dev` in both themes plus the
  toggle's own tests; no automated visual test exists for this (out of
  scope for this pass).
- The stray chromatic dark `--sidebar-primary` is resolved (achromatic,
  confirmed unused in `src/` beforehand).
- Theme control: `components/theme-toggle.tsx` in the sidebar header,
  covered by `theme-toggle.test.tsx` (opens via pointerdown — Radix's
  `DropdownMenuTrigger` opens on `onPointerDown`, not `onClick` — sets the
  preference, marks the active option, and renders standalone without a
  provider).
- `src/test/no-hardcoded-colors.test.ts`: a grep-based test across every
  `.ts`/`.tsx` file for hardcoded hex/oklch literals and Tailwind
  palette-color utilities. It caught and forced the fix of pre-existing
  instances: `bg-amber-500`/`text-amber-600`/`text-amber-900` (connection
  banners, pending-permission card, file-conflict banner) and
  `bg-emerald-500`/`text-emerald-600` (accounts-dialog's connection dot)
  — all now on the new status tokens.

**Item 2 — shared primitives + `docs/design.md`:**

- `components/ui/{pane-header,status-dot,status-badge,alert,empty-state,
  inline-spinner,toast}.tsx`, each with its own test file.
  `PaneHeader` is adopted by all four named panes (`task-detail.tsx`,
  `file-editor-pane.tsx`, `diff-viewer-pane.tsx`, `terminal-pane.tsx`),
  unifying their padding to one scale as a side effect.
- `file-editor-pane.tsx`'s hand-rolled Save/Reload/Overwrite buttons moved
  to the shared `Button`; all three (plus Commit and the diff pane's
  per-file Stage checkbox) gained in-flight labels/disabled states
  ("Saving…"/"Reloading…"/"Overwriting…"/"Committing…", each tied to its
  own in-flight state rather than a single pane-wide flag for Stage).
- Layout stability: the sidebar's task-attention dot now sits in a
  fixed-width reserved slot (`task-attention-slot`) present regardless of
  whether the dot itself renders — `app-sidebar.test.tsx`'s new
  "layout stability" test asserts the slot's class is identical with and
  without attention.
- Empty/error copy normalized to sentence case, no trailing period,
  across task-detail ("No runs yet"), diff-viewer ("No changes"), and
  file-explorer ("Empty", was "(empty)").
- `docs/design.md` written: token layers, theming mechanism per
  third-party pane, the primitive table, density/copy/state rules, and a
  Decisions section (mirrored, pointer-only, in this plan's own
  Decisions above).
- `task test` (224 web tests, all Go packages), `task lint`
  (`go vet`/`gofmt`), `bunx tsc -b`, and `task build` all pass. `task
  build` surfaced one real bug worth recording: a doc comment containing
  the literal substring `chart-*/sidebar*` closed its own CSS comment
  early (`*/` inside prose), silently corrupting everything after it into
  raw CSS that Tailwind's build-time parser then failed on — caught only
  by `task build`, not by `tsc -b` or vitest (jsdom doesn't validate
  Tailwind's CSS generation). Fixed by rewording the comment; worth
  remembering that any future doc comment in this file must avoid a bare
  `*/` substring.
### Item 16 — daemon lifecycle events (backend half)

`docs/decisions/0009-lifecycle-event-topics.md` records the topic set,
payload shape (full entity for create/update/archive, id+scope for
delete), ordering, and reconciliation story. Implemented in
`internal/workspace` (new `Notifier` interface, `SetNotifier`, and a
`Notify*` call at every mutation site: `CreateWorkspace`, `DeleteWorkspace`,
`CreateSpace`, `DeleteSpace`, `CreateTask`, `RunTask`, `ArchiveTask`,
`DeleteTask`) and `internal/wsapi` (eight new topics/payload types in
`events.go`, `busWorkspaceNotifier` adapter in `server.go` replacing the
old task-status-only `SetTaskNotifier`).

- **Acceptance criterion** ("subscribing to each new topic delivers the
  event on create/update/archive/delete; an unknown topic still errors"):
  covered by `internal/wsapi/lifecycle_events_test.go` — one test per
  topic (`TestEvents_WorkspaceCreatedSubscribeAndReceive`,
  `TestEvents_WorkspaceDeletedIsRootOnlyCascade`,
  `TestEvents_SpaceCreatedSubscribeAndReceive`,
  `TestEvents_SpaceDeletedCascadesWithoutPerTaskEvents`,
  `TestEvents_TaskCreatedSubscribeAndReceive`,
  `TestEvents_TaskUpdatedOnRunTask`,
  `TestEvents_TaskArchivedSubscribeAndReceive`,
  `TestEvents_TaskDeletedOnManagerDeleteTask`), each asserting exactly one
  event with the documented payload and no extra event; unknown-topic
  rejection was already covered by ADR 0005's `TestEvents_UnknownTopicIsError`
  and needed no change (`knownTopics` is additive).
- **Two-client delivery**: `TestEvents_WorkspaceCreatedReachesASecondClient`
  — client A calls `workspace.create`, client B (subscribed, never having
  made the call itself) receives `workspace.created` — the actual
  cross-client-staleness scenario from `gap-matrix.md` item 8.
- **Cascade-is-root-only**: `TestEvents_WorkspaceDeletedIsRootOnlyCascade`
  and `TestEvents_SpaceDeletedCascadesWithoutPerTaskEvents` subscribe to
  the descendant topics too and assert silence after the one root event.
- **Ordering**: `TestEvents_TaskArchivedPrecedesTaskStatus` pins
  lifecycle-before-status on one connection subscribed to both.
- **Delete payload scope**: `TestEvents_TaskDeletedSpaceIDIsNullNotOmitted`
  asserts the raw payload always carries a `spaceId` key (null, not
  omitted, for an ungrouped task -- `decodePayload` cannot tell the two
  apart, so this one inspects the payload map directly), and
  `TestEvents_TaskDeletedCarriesSpaceIDForGroupedTask` asserts a task
  inside a space reports that space, which is the subtree the sidebar
  actually prunes from.
- **Emit-vs-error edge**: `TestEvents_WorkspaceCreatedFiresWhenAccountAttachFails`
  pins the one path that publishes before its method's success return --
  `CreateWorkspace` emits once the workspace row commits, before the
  `AddWorkspaceAccount` loop that can still fail, because that failure
  leaves the row in place (ADR 0009's "Ordering and delivery" note).
- **Non-regression**: `TestEvents_LifecycleTopicsAreOptIn` — a
  `task.status`-only subscriber sees nothing from a workspace create,
  confirming the change is additive; the full pre-existing
  `internal/wsapi`/`internal/workspace` suites pass unchanged.
- `task test` (Go suite + web UI suite, `web/` untouched) and `task lint`
  (`go vet` + `gofmt -l`) both pass clean.
- **Not wired**: `task.commit` and `task.createPr` emit nothing (by
  design — see the ADR's Consequences; they don't change the task row).
  `RunTask`/`DeleteTask` have no wsapi RPC calling them today (confirmed
  by grep — `task.prompt`/`run.start` never transition `store.Task.Status`,
  and there is no standalone `task.delete` method), so `task.updated` and
  a directly-invoked `task.deleted` are tested against the Manager
  directly rather than over the wire; the moment either method gets an
  RPC, its event fires for free.
- **Left for the UI track**: `hooks/use-daemon-events.ts` subscribing to
  the eight topics and `useWorkspaceTree` consuming them (this PR touches
  no code under `web/`, per this item's own scope split with the UI
  agent).
**Item 7 (structured timeline events)** — 2026-09-14, daemon + wire only
(this item does not touch `web/`; the timeline renderer that consumes this
schema is Item 8/9, Track B, separate PR):

- ADR `docs/decisions/0008-structured-run-events.md` written and accepted
  before implementation, per the plan's gate — schema, wire shape,
  persistence, and compatibility strategy recorded there.
- `taskrunner.Event` gains `EventTypeUserMessage`/`EventTypeThinking`/
  `EventTypeToolCall` (appended after the existing four, per the ADR's
  append-only-enum rule) plus `ToolCallID`/`ToolName`/`ToolTitle`/
  `ToolStatus`/`ToolInput`/`ToolResult` fields
  (`internal/taskrunner/event.go`).
- Both required backends produce them:
  `TestRunner_RunPrompt_ClaudeNative_ToolCallEvents` (thinking + two tool
  calls, one completing success, one failure, via the fake CLI's new
  `tool_call` scenario) and `TestRunner_RunPrompt_GLM_StructuredEvents`
  (thinking + user-message + a tool call completing success, via the fake
  ACP agent's new `structured` scenario) — both in
  `internal/taskrunner/runner_test.go`. Codex-native is explicitly **not**
  covered for tool calls (documented gap in the ADR's Decision section —
  `internal/codex` doesn't model Codex's item-lifecycle protocol at all
  yet); its text/done/permission events are unchanged and still pass.
- `internal/wsapi`: new wire event names (`user_message`/`thinking`/
  `tool_call`) added to `run.attach`'s stream and `run.logs`'s batch shape
  (`handlers.go`). `TestServer_RunLogs_StructuredEvents` proves all three
  render correctly over the wire.
  `TestServer_RunLogs_PreExistingEventsStillDecode` proves the back-compat
  half of the ADR's Test Scenario: a run whose events were persisted in
  the old four-field JSON shape (inserted directly into the store, no new
  fields) still decodes and serves via `run.logs` after a Registry
  rehydration, unchanged.
- `internal/runs`: `persist.go`'s `encodeEvent`/`decodeEvent` carry the new
  fields; no store schema migration needed (`event_data` is already a
  free-form JSON column — noted explicitly in the ADR).
  `TestRegistry_ToolCallEvents_RoundTripAcrossStoreReopen` proves the new
  event types (not just the four old ones) survive a simulated daemon
  restart identically.
- `cmd/smind/task.go`: `task attach`/`task logs` render the new event
  kinds as readable text — text-shaped events (`chunk`/`user_message`/
  `thinking`) print as before; `tool_call` prints one line via the new
  `toolCallNames.render` (name/title + lifecycle status, input while
  running), which remembers each `toolCallId`'s name so a completion
  renders as `[tool] Bash: done` rather than under a raw wire id.
  `cmd/smind/task_test.go` (new) covers the render table and pins the
  run.logs wire keys against the CLI's own structs.
- `task test` (`go test ./...` + the web suite) and `task lint`
  (`go vet ./...` + `gofmt -l`) both pass clean.
- **Wire-compat caveats the UI track (Items 8/9) must respect**: (1) a
  `tool_call` entry's `toolName`/`title`/`input` may be empty on a later
  event for the same `toolCallId` — ACP's `tool_call_update` is a partial
  update, so the UI must merge by ID, not replace; (2) Claude Code native
  never emits `EventTypeUserMessage` (its own prompt echo is dropped, not
  translated — the UI already has the human's own prompt from its own
  composer); (3) Codex-native never emits `tool_call` in this pass, so a
  Codex run's timeline is text-only even after Item 8/9 land, until a
  follow-up change teaches `internal/codex` its item-lifecycle protocol.

- **Adversarial review pass (2026-09-14)** on top of the above, with
  tests that fail against the first cut:
  - `internal/taskrunner/normalize_test.go` (new) tests `claudeEvents`
    and `acpEvent` directly, as translation tables, rather than only
    end-to-end through a fake agent. It caught three dropped/incorrect
    cases, all fixed: a `tool_result` block arriving on an *assistant*
    message (left its card stuck "running" forever), the server-side
    tool blocks `WebSearch`/`WebFetch` produce (no tool-call events at
    all for such a run), and an ACP `tool_call` with an absent or
    unrecognized `status` (ACP's `ToolCall.status` is optional with a
    `pending` default, so an initial `tool_call` is running, not
    status-less).
  - `internal/wsapi`'s two Item 7 tests were strengthened: the wire-key
    assertion is now made against untyped JSON (a `json:` tag typo was
    invisible before), and the pre-ADR row fixture now covers all four
    original `EventType` integers rather than just 0 and 1 -- only the
    permission pair can catch a constant inserted mid-enum.
  - ADR-0008's "additive only, in both directions" claim was corrected:
    on the ACP path this re-types `agent_thought_chunk`/
    `user_message_chunk` from `chunk`, so the shipped UI shows less text
    for a GLM/Kimi run until Item 8 lands.

- **Known gaps left open for Items 8/9 / a follow-up**, none of them
  regressions:
  - An ACP update kind neither `Text()` nor `IsToolCall()` accepts
    (`plan` today, anything ACP adds tomorrow) is silently dropped
    rather than surfaced as a generic/raw event. Preserving unknown
    kinds would mean a new wire event name, i.e. its own ADR.
  - ACP's `rawOutput` is not carried: `ToolResult` is fed from the
    tool call's display `content` array.
  - Every event, now including full tool inputs and results, is written
    as its own `run_events` row and retained in unbounded in-memory
    history. Nothing new was introduced here, but the per-run volume is
    substantially higher than before Item 7 (tool results were dropped
    entirely); if large runs get slow, batching `record`'s
    `AppendRunEvent` or truncating `ToolResult` is where to look.
### Item 17 — file explorer and editor polish

Every acceptance criterion, and how it was confirmed:

- **File-type icons in the tree and on tabs** — `lib/file-icons.tsx` maps
  a whole-filename table (`Dockerfile`, `bun.lock`, `.gitignore` — files a
  repo view is largely made of, which have no extension or a lying one)
  then an extension table onto a `FileIconKey`, which is rendered as
  `data-icon` so tests assert the resolution, not lucide's SVG markup.
  Adopted by `file-explorer-pane.tsx`'s rows and by `TabLabel` (base tabs
  get a per-kind icon from `TAB_KINDS`, file tabs get the file-type one).
  `lib/file-icons.test.tsx` (6 tests) covers the plan's scenario (*a `.go`
  path renders its icon; an unknown extension renders the generic one*)
  plus case-insensitivity, multi-dot names, dotfiles and
  no-extension-at-all.
- **Git status decoration in the tree, sourced from `task.files`** —
  `hooks/use-task-file-status.ts` (new, shared) fetches `task.files` and
  refetches on the same signal `diff-viewer-pane.tsx` already uses (a
  *terminal* `run.status` for this task). `components/file-status-marker.tsx`
  renders A/M/D on the `status-{success,warning,danger}` tokens.
  `file-explorer-pane.test.tsx` covers the plan's scenario (*a modified
  file shows its git decoration; an untracked one shows a different
  marker* — see Decisions for why that reads as added-vs-modified), that
  an unchanged file is undecorated, and the directory roll-up.
- **A dirty indicator on the file tab itself** — `lib/dirty-buffers.ts`
  (a `useSyncExternalStore` store keyed by tab key); `FileEditorPane`
  publishes, `TabLabel` subscribes. `tab-registry.test.tsx` (7 tests)
  covers the plan's scenario (*a dirty buffer marks its tab; saving
  clears it*) end to end — a real CodeMirror transaction into a real
  `FileEditorPane`, then a real `file.write` — plus the two cases a
  module-level store makes possible to get wrong: unmounting a dirty
  editor clears the marker, and only the owning tab is marked.
- **Context actions on tree rows** — a Radix context menu
  (`components/ui/context-menu.tsx`, new primitive) with *Reveal in diff*
  (disabled, not hidden, for an unchanged path) and *Copy path*.
  "Open to side" is deliberately absent until Item 6 — see Decisions.
  Reveal is covered from both ends: the explorer latches the request and
  calls `onRevealInDiff`, and `diff-viewer-pane.test.tsx` asserts the
  pane consumes a request latched *before it mounted* (the normal case —
  the diff tab isn't in front when you right-click in the tree),
  re-expands a collapsed file when revealed while already mounted, and
  leaves a request aimed at a different task alone.
- **CodeMirror's theme follows the app theme** — already closed by Item 1
  (`code-mirror-editor.tsx`'s `appChromeTheme`); the plan assigns it to
  whichever item lands first. Not re-done.

A real bug the tests caught before commit: the reveal-scroll effect
cleared its pending path unconditionally, so a reveal latched before
`task.files` resolved (i.e. every reveal, on a cold diff tab) scrolled
nowhere and then forgot itself. It now waits for `files` and scopes the
lookup to the pane's own list rather than the document, so Item 6's
second diff pane can't scroll the first one's row.

`bunx tsc -b`, `task test` (248 web tests, 30 files; all Go packages) and
`task lint` green.

### Item 19 — diff / review v2

Every acceptance criterion, and how it was confirmed:

- **A whole-diff view alongside the per-file list, and a side-by-side /
  unified toggle** — two segmented toggles in the pane header
  (`SegmentedToggle`, generalized from `file-editor-pane.tsx`'s
  Edit/Preview control rather than hand-rolled a third time), backed by
  `lib/diff-prefs.ts`. The diff2html render itself moved into
  `components/diff-render.tsx` so the per-file rows and the whole-diff
  view share one implementation instead of two copies of the same effect.
  `diff-viewer-pane.test.tsx` asserts the whole-diff view renders *every*
  changed file (both `file.txt` and `new.txt` in one container, and the
  per-file list gone), that the format toggle actually changes
  diff2html's `outputFormat`, and that the choice survives the pane
  unmounting — which is what a tab switch does.
- **Per-line draft comments, submitted as a single prompt, surviving a
  collapse and a tab switch** — `lib/review-drafts.ts` (per-task,
  `useSyncExternalStore`, mirrored to `localStorage`),
  `lib/diff-lines.ts` (click → `{side, line, text}`, both output
  formats), `components/review-comments.tsx` (draft list + composer).
  Covered end to end: writing a comment on a real diff2html-rendered
  line, collapsing the file, unmounting and re-mounting the pane, then
  submitting — one `run.start` carrying both comments, drafts cleared
  after. Failure keeps them (a submit error must not eat a review) and
  removing one draft leaves the other.
  `lib/diff-lines.test.ts` asserts the DOM reading against diff2html's
  *actual* markup (rendered in the test, not a hand-written fixture) for
  unified, side-by-side, deletions, hunk headers and non-rows.
  `lib/review-drafts.test.ts` covers per-task isolation, the stable empty
  array `useSyncExternalStore` requires, persistence, and the prompt's
  grouping.
- **A diff stat surfaced outside the pane** — `lib/diff-stat.ts`'s
  `parseDiffStat`/`formatDiffStat` and `hooks/use-task-diff.ts`. The pane
  renders it in its header (`data-files`/`data-additions`/
  `data-deletions` for assertion); the hook is the consumption point for
  Items 10/12. `diff-stat.test.ts` covers the `+++`/`---` header
  exclusion, the empty diff, and the headerless-diff fallback; the pane
  test asserts the rendered stat matches the `task.files` list.
- **Per-hunk staging out of scope** — unchanged, per ADR 0006.
- **Existing stage/viewed/commit/PR tests keep passing unchanged** — they
  do, byte for byte; the 20 pre-Item-19 tests in that file were not
  touched.

One thing worth stating plainly: a draft written against a file that
later leaves `task.files` (because it was committed) stops rendering in
the by-file view but is still counted by the submit bar and still
included in the prompt. The count is the honest number; surfacing such
drafts somewhere would need a "stale drafts" affordance that nothing in
this item asks for.

`bunx tsc -b`, `task test` (272 web tests, 33 files; all Go packages) and
`task lint` green.

### Item 20 — terminal v2

Every acceptance criterion, and how it was confirmed:

- **More than one terminal per task, each its own tab** —
  `tab-registry.tsx`'s `terminalTab`/`nextTerminalTab` (indices from 2 up,
  the base non-closable `${taskId}:terminal` tab being the implicit
  first, freed indices reused rather than counted forever) and
  `lib/terminal-sessions.ts`'s tab↔session binding (see Decisions for why
  a binding was necessary at all). `tab-registry.test.tsx` covers index
  assignment and reuse; `terminal-pane.test.tsx`'s new "multiple terminals
  per task" describe block drives two real `TerminalPane` mounts against
  two `FakeWsClient`s and asserts they land on two distinct session ids
  even when both sessions are already running server-side — the
  regression the binding exists to prevent.
- **An activity indicator on an inactive terminal tab** —
  `TerminalPane` takes `active`; a `data` event arriving while `active` is
  false calls `markTerminalActivity(tabKey)`, cleared the moment `active`
  flips true. `TabLabel` renders it as a `status-dot-running` dot,
  distinct from Item 17's dirty marker. Covered from both ends: a
  standalone `useTerminalActivity` consumer proves the flag flips on
  inactive data and clears on activation (and stays clear for data
  received while already active), and `tab-registry.test.tsx` proves
  `TabLabel` renders it correctly and distinctly from the dirty dot.
- **Scrollback size as a setting** — `lib/terminal-prefs.ts`
  (load/save, clamped, `DEFAULT_TERMINAL_SCROLLBACK` stated explicitly),
  read once via `createTerminal({ scrollback })` at terminal-creation
  time. `terminal-prefs.test.ts` covers the default, round-trip, and
  clamping a corrupted/hand-edited value; `terminal-pane.test.tsx`
  asserts the persisted value reaches xterm's constructor options.
- **A copy/paste affordance** — Copy reads `handle.getSelection()`,
  disabled until something is selected (and never permanently disabled
  for a handle that can't report selection at all — see Decisions); Paste
  reads the clipboard and sends it via `terminal.write`, never into the
  emulator's local buffer. Both, plus a paste-failure error line, are
  covered in `terminal-pane.test.tsx`'s new "copy/paste" describe block.
- **The existing detach-not-close contract, including the `interrupted`
  status path, is preserved** — unmounting still aborts the attach
  without calling `terminal.close`; the pre-Item-20 reconnect-resync
  tests (all 16 of the file's original tests) pass unchanged after adding
  a `resetTerminalSessions()` `afterEach` (the new tab↔session store is
  module-level, like `lib/dirty-buffers.ts`, so it needs the same
  per-test reset the dirty-buffer tests already established).

`bunx tsc -b`, `task test` (292 web tests, 35 files; all Go packages) and
`task lint` green.
