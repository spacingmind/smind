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
- Below the breakpoint, the sidebar-vs-content split and the side dock
  (Item 6) both stop being resizable panels: no drag handle renders for
  either, and the sidebar becomes an overlay instead of a fixed column.
- The side dock's split doesn't apply below the breakpoint even when a
  task already has one open from a wider session — its tabs merge back
  into the single visible strip rather than disappearing, and the split
  reappears, unchanged, once back above the breakpoint.
- The "open to side" affordance is absent below the breakpoint (there is
  nowhere for a moved tab to land).
- The composer and permission-card controls carry a stated compact touch-
  target size below the breakpoint, and the original dense size at/above
  it (assert the class list, since jsdom has no layout — plus a manual
  check recorded in Validation).

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
- **Item 21's compact model is two destinations, not Paseo's three.**
  `audit-paseo.md` §8 describes `agent-list` / `agent` / `file-explorer` as
  three mutually exclusive top-level destinations because Paseo's file
  explorer is its own screen. smind's Files pane is already one of a
  task's tabs (ADR 0004's tab registry, Item 17), not a sibling route —
  building a third top-level destination for it would duplicate
  navigation smind already has. The compact model that actually fits is
  two: the sidebar (task list), rendered as shadcn's existing
  `useIsMobile()`-driven Sheet overlay, and the task pane (which already
  contains Files as one of its tabs) — one shared boolean (the Sidebar
  primitive's own `openMobile`) is the "one selection value" the plan
  calls for, so the two can't disagree the way a hand-rolled second flag
  could.
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

**Item 10 (landed)** — Track B's composer decisions:

- **The provider/policy controls stay native `<select>`s**, given visible
  `<label>`s instead of bare `aria-label`s. Item 10's complaint was
  "unlabelled native selects"; swapping in the Radix `Select` would have
  rewritten four passing `run.start`-payload tests for no user-visible
  gain, against this item's own "no regression" scenario.
- **Submitting while a run is live queues, it does not steer.** The daemon
  has no "add input to a run in flight" RPC, so the composer holds the
  text and starts it as its own run when the live one ends. The queue is
  in-memory and per task: a follow-up whose meaning is "right after the
  run I was watching" doesn't survive that run's session.
- **A run in flight does not disable the composer**, unlike no-connection
  and no-task. The placeholder still states what's different ("Queue a
  follow-up — it sends when this run finishes"), which is what the item's
  block contract actually asks for.
- **Stop moved off the run card entirely** rather than existing in both
  places: two controls with the same meaning, one of which scrolls out of
  view mid-run, is the problem Item 10 names.

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

**Item 18 (landed)** — where the plan was ambiguous and what was decided:

- **A daemon RPC was added, per the plan's own "measure before adding"
  rule.** Measured against this repo's own worktree before reaching for
  one: a client-side walk (repeatedly calling `file.list`, one directory
  at a time — the plan's stated default) is O(directories) round trips,
  and this worktree alone has ~150 directories excluding `node_modules`
  and ~3,900 including it (`web/` doesn't gitignore it — bun vendors
  there). Each would be its own WS round trip under a client-side walk.
  `task.searchIndex` (`internal/workspace/search.go`,
  `internal/wsapi/handlers.go`) returns the whole list in one RPC via a
  single `git ls-files -co --exclude-standard` invocation — additive (a
  new method, no change to `file.list`/`file.read`/`file.write`), and it
  gets gitignore correctness for free: smind doesn't parse `.gitignore`
  itself, git already does.
- **It's a `task.*` method, not a `file.*` one.** `file.list`/`file.read`/
  `file.write` take a user-supplied `path` and sandbox it inside the
  worktree (`resolveInRoot`); `task.searchIndex` takes only `taskId` and
  is inherently scoped to the whole worktree, so it follows
  `task.files`/`task.fileDiff`'s naming and Manager-method shape instead.
  Named `searchIndex`, not `files`, to stay unambiguous next to the
  already-existing `task.files` (which means something different: the
  base→worktree *diff*'s changed-file list, not "every file").
- **Fuzzy matching and ranking are entirely client-side**
  (`lib/fuzzy-match.ts`), not part of the RPC. The daemon's job is
  enumeration (which needs git); ranking is product logic that belongs
  next to the UI it's rendered in, and keeping it client-side means it's
  unit-testable without a wire round trip and free to change without
  touching the RPC contract.
- **Two-tier scoring, not a single edit-distance score**: a substring
  match on the **filename** always outranks a scattered subsequence match
  anywhere in the path. A person typing a name they remember
  ("editor-pane") and a person typing initials across directories
  ("cmpne") are doing different things, and conflating them into one
  score produced surprising rankings during testing (see the two-tier
  design's own doc comment in `lib/fuzzy-match.ts`).
- **The dialog prefetches the index while closed**, as soon as a task is
  selected — not lazily on first open. "Quick" open should not show a
  loading spinner the first time it's opened; App.tsx already mounts
  `QuickOpen` once per task-selection lifetime (see the Track A hook note
  below), so prefetching costs one RPC per task selection, not per open.
- **No global shortcut registry exists yet (Item 4 hasn't landed)**, so
  `hooks/use-quick-open-shortcut.ts` is a plain, swappable function — a
  local `document`-level Ctrl/Cmd+P listener — rather than a component or
  a registered action, exactly per this track's scoping instructions.
  Track A should replace the call site in `App.tsx` with a real
  `keyboard/actions.ts` entry once Item 4 lands; the hook's own doc
  comment says so.

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
active, and `TerminalPane` takes `tabKey`/`active`/`onNewTerminal`. Item
18 adds a fourth, outside the `Tabs` tree entirely: `<QuickOpen>` is
mounted once as a sibling of `ResizablePanelGroup`, with its own
`open`/`onOpenChange` state and a `useQuickOpenShortcut` call that wires
Ctrl/Cmd+P — no layout change, since `Dialog` portals to `document.body`
regardless of where it's rendered. Items 3 and 6 should preserve all
four when they restructure the shell — in particular, Item 6's side dock
must keep the force-mount behavior for any terminal tab it moves, or a
moved terminal would silently stop being able to mark its own activity
while backgrounded, and Items 4/5 should replace the local
`useQuickOpenShortcut` call with a real `keyboard/actions.ts` entry
(`hooks/use-quick-open-shortcut.ts`'s own doc comment says the same).

**Item 21 (landed)** — where the plan was ambiguous and what was decided:

- **The breakpoint is 768px** — `hooks/use-mobile.ts`'s existing
  `useIsMobile()` (already shipped for the shadcn Sidebar primitive) uses
  this number, and it's Tailwind's own `md` breakpoint, so the compact
  touch-target classes below (`h-11 ... md:h-6`) key off the exact same
  threshold with zero JS coordination between the two. No new hook was
  written; `useIsMobile()` was fit for purpose as-is.
- **Two destinations, not three** — see the Decisions section above.
- **The sidebar overlay was already half-built and defeated by Item 6's
  layout.** shadcn's `Sidebar` primitive (`components/ui/sidebar.tsx`)
  already renders itself as a `Sheet` once `useIsMobile()` is true — that
  was never the gap. The gap was `App.tsx` unconditionally wrapping it in
  a `ResizablePanel` with `SIDEBAR_MIN_WIDTH` (192px): on a 375px viewport
  that reserved over half the screen for a component rendering into a
  portal, which is what a "compact layout" that was actually zero lines
  of code would look like from the outside. The fix is entirely in
  `App.tsx`: below the breakpoint, `AppSidebar` and `SidebarInset` render
  as plain flex children instead of `ResizablePanel`s, so the overlay
  gets the full-width, unsqueezed shell the primitive was already built
  to assume.
- **The side dock's tabs merge into one strip on compact, rather than
  being hidden.** An earlier draft of this work only rendered `primary`'s
  tabs below the breakpoint and left `side`'s tabs unreachable until the
  window widened again — technically "gracefully doesn't apply" (nothing
  crashes, no data is lost), but a tab the user had open simply vanishing
  from view is not graceful in any user-facing sense. Since
  `useTaskTabs.ts`'s `activate`/`closeTab` already resolve a key to
  whichever pane holds it, merging `[...primary.tabs, ...side.tabs]` into
  one `PaneTabStrip` (primary's kinds first, side's after) costs nothing
  extra and keeps every open tab reachable regardless of viewport width;
  only the "open to side" affordance (`showMoveAffordance`) disappears,
  since there's nowhere left for it to move a tab to.
- **44px (WCAG 2.5.5 AAA / Apple HIG) for the compact touch targets** —
  the permission-card buttons (`h-6`, 24px) and the composer's Send/Stop/
  provider/policy controls (`h-7`, 28px) were both below any reasonable
  minimum for a phone. Rather than threading an `isMobile` prop through
  every one of these components, the fix is plain responsive Tailwind —
  `COMPACT_TOUCH_BUTTON_CLASS`/`SELECT_CLASS`/
  `COMPACT_TOUCH_ACTION_BUTTON_CLASS` all read `h-11 ... md:h-<original>`
  — since `md:` already keys off the same 768px, and `cn`'s `twMerge`
  correctly resolves the unprefixed/`md:`-prefixed pair without either
  clobbering the other. The timeline's own tool-call-toggle button was
  left alone: its tap target is the *whole row* (`min-w-0 flex-1`), not a
  small icon, so height alone already clears the 24px WCAG AA floor by a
  comfortable margin, and bumping it would visually disrupt the timeline's
  established row density for no reachability gain.

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
- [x] Item 3: routing + persisted UI state *(Track A)*
- [x] Item 4: keyboard registry + shortcuts help *(Track A)*
- [x] Item 5: command palette *(Track A)*
- [x] Item 6: split panes / side dock *(Track A)*
- [x] Item 7: structured timeline events *(Track B — **ADR gate**)*
- [x] Item 8: timeline renderer *(Track B)*
- [x] Item 9: tool-call cards *(Track B)*
- [x] Item 10: composer v2 *(Track B)*
- [x] Item 11: permission UX v2 *(Track B)* — kind-styled options and
  keyboard focus shipped; question-form/plan-review variants render from
  synthetic fixtures only, no wire producer for either and none added
  this pass — investigated and intentionally not built (see Validation)
- [x] Item 12: sidebar signal *(Track D)*
- [x] Item 13: settings screen *(Track D)*
- [x] Item 14: accounts v2 *(Track D)* — OAuth cancel shipped; disable/
  enable/remove/status-richness need a wire change, gated on AGENTS.md
  rule (d) (see Validation)
- [x] Item 15: quota / usage surface *(Track D)* — scoping note only, per
  the item's own gate: `internal/quota` has no real data source and no
  RPC (see Validation)
- [x] Item 16: daemon lifecycle events *(Track D — **ADR gate**)* — backend (ADR 0009, `internal/wsapi`/`internal/workspace`) and UI consumption (`lib/workspace-tree.ts`, `hooks/use-daemon-events.ts`, `useWorkspaceTree`) both landed
- [x] Item 17: file explorer / editor polish *(Track C)*
- [x] Item 18: quick file open *(Track C)*
- [x] Item 19: diff / review v2 *(Track C)*
- [x] Item 20: terminal v2 *(Track C)*
- [x] Item 21: responsive / compact layout *(Track A)*

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

**Item 11 follow-up (permission resolution reason, gap fix)** —
2026-09-15, `web/` only:

- **Gap**: `permission_resolved`'s `reason` field (`human` | `auto_safe` |
  `timeout`, `task-permission-ux.md` Item 2) was already decoded correctly
  by the wire layer (`lib/types.ts`'s `PermissionResolvedEventParams`) but
  never rendered anywhere. Worse, once resolved the request left the
  timeline with **no trace at all**: `permission_resolved` was in
  `use-run-timeline.ts`'s `NON_ITEM_EVENTS`, so it only cleared the
  pending-permission dock (Item 11) and produced no row of its own — a
  finished run's transcript showed nothing where a permission had been
  asked and answered.
- **Fix**: `permission_resolved` now also becomes its own
  `TimelinePermissionItem` row (`use-run-timeline.ts`), rendered by
  `timeline-row.tsx`'s new `PermissionRow` as one line of muted text plus
  a `StatusBadge` for the reason (`components/timeline/permission-reason.ts`
  maps `human` → "You approved" / `success`, `auto_safe` → "Auto-approved" /
  `running`, `timeout` → "Timed out" / `warning`) — the first consumer of
  `StatusBadge`, which `docs/design.md` had shipped with none. No new card;
  `permission_request` is unchanged (still dock-only, per Item 11).
- **Missing/unrecognised reason** (an older server payload, or a future
  value this build hasn't seen): renders the plain "Permission resolved"
  line with no badge, not a crash — asserted directly, plus exercised
  transitively by every pre-existing `permission_resolved` fixture in
  `task-detail.test.tsx` that never set `reason`.
  `timeline-model.test.ts` and `run-timeline.test.tsx` cover all three
  reasons plus the missing-reason case, and that earlier items keep their
  object identity when a permission item is appended.
- `task test` (Go + web, 623 web tests), `task lint`, `bunx tsc -b` clean.
  (`internal/terminal`'s `TestRegistry_CreateWriteSubscribe_RealShell`
  fails in this sandbox — `fork/exec /bin/bash: operation not permitted`
  — pre-existing and unrelated, untouched by this fix.)

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

### Item 11 — permission UX v2

**2026-09-15 audit follow-up.** An independent audit found that this item's
question-form and plan-review variants shipped real, tested UI
(`components/permission/{question-form-card,plan-review-card}.tsx`,
`permission-card.tsx`'s shape-dispatch, `permission-card.test.tsx`) but no
wire path on either provider ever produces a request shaped that way —
those tests exercise only synthetic `PendingPermission` fixtures the test
itself constructs. Investigated whether to close that gap this pass;
concluded neither variant is a bounded fix, for two different reasons, and
built neither. What *is* genuinely shipped vs. not, in full:

**Shipped and wired end-to-end:**
- Kind-styled option buttons — destructive/deny reads as deny, the
  recommended (first allow-kind) option reads as primary
  (`permission-option-button.tsx`, `options-card.tsx`), driven by the real
  `kind` ACP already carries and Claude/Codex's adapters already
  synthesize (`internal/taskrunner/permission.go`).
- Keyboard: the pending card is a focusable, labelled group that grabs
  focus on a new request unless a text field is mid-keystroke, and options
  are real `<button>`s in tab order (`permission-card.tsx`).
- The pre-existing cross-connection resolution guarantee (cleared by
  `permission_resolved`, never local-click-only) — unchanged, still
  covered by `permission-card.test.tsx`.

**Not shipped, not attempted this pass:**
- **"What is being requested" (command line / diff).** The item's own
  first bullet. `PermissionDecider.Decide` already receives a `command`
  string (`internal/taskrunner/permission.go`), but it is dropped before
  reaching `taskrunner.Event` (`event.go`'s `Permission*` fields have no
  command/diff field) or `internal/wsapi`'s `permissionRequestParams`
  (`handlers.go:652-656`, `options`/`summary` only) — so the UI has never
  had this data to render. Bounded (thread one more string through three
  layers, no external unknowns) but out of scope for this pass, which
  focused on the audit's specific finding below.
- **Question-form variant — real source is Claude Code's `AskUserQuestion`
  tool.** Confirmed reachable without any new run-mode concept: it's a
  fully generic tool call through the same `can_use_tool` hook every other
  tool already uses (vendored SDK's `engine.go:327-339` builds
  `CanUseToolRequest{ToolName, Input}` with zero name-based filtering), and
  its *request* shape is documented
  (`refs/claude-code/plugins/plugin-dev/skills/command-development/references/interactive-commands.md:34-59`):
  `Input.questions[]`, each `{question, header, multiSelect, options[]}`.
  The blocker is the *answer* side: Claude Code's changelog says a
  PreToolUse hook can satisfy `AskUserQuestion` "by returning `updatedInput`
  alongside `permissionDecision: allow`" (`refs/claude-code/CHANGELOG.md:3345`),
  but the exact shape `updatedInput` must take for the real CLI to treat it
  as the human's actual answers (vs. running the tool with empty ones — a
  failure mode the same changelog documents happening silently,
  `CHANGELOG.md:3915`) is not documented anywhere available in this
  environment: not in the vendored `claude-agent-sdk-go@v0.3.2` (grepped
  `client.go`/`permission.go`/`messages.go`/every test — zero
  `AskUserQuestion` special-casing, it's treated as an opaque tool name
  like `Bash`), not in `refs/claude-code`'s docs, not in
  `refs/claude-agent-sdk-python`/`typescript`. This environment has no live
  CLI to empirically verify it against. Shipping a guessed `updatedInput`
  shape would be worse than shipping nothing — it would silently run the
  model's own question tool with wrong or empty answers rather than fail
  loudly, exactly the class of bug the vendor's own changelog flags as a
  real, previously-shipped incident. This is an external protocol fact to
  verify against a real CLI, not an internal architecture choice — no ADR
  to write until that's known.
- **Plan-review variant — real source is Claude Code's `ExitPlanMode`
  tool.** Also reachable through the same generic `can_use_tool` hook in
  principle, but only when the CLI is launched with
  `--permission-mode plan` (`claude-agent-sdk-go@v0.3.2/client.go:181-186`
  documents `"plan"` as a valid `WithPermissionMode` value). smind's
  claude-native runner never does this — `runClaudeNative`
  (`internal/taskrunner/runner.go:354-374`) hardcodes `"default"` or
  `"acceptEdits"`, and there is no concept anywhere in smind (no
  `task.prompt`/`run.start` param, no composer toggle, no third
  `ApprovalPolicy`-like axis) for a caller to request a plan-mode run at
  all. Wiring this for real means inventing that concept from scratch end
  to end (composer UI → wire param → `RunPrompt` →
  `WithPermissionMode("plan")`), plus deciding two behaviors nothing in
  this repo or the audits resolves: what happens to the turn after
  Approve/Refuse, and what "Chat about it" does server-side — today's
  `PlanReviewCard.onChat` doesn't call `onRespond` at all
  (`plan-review-card.tsx`), so the agent's blocked `Decide()` call would
  simply hang forever with nothing to unblock it. That is a new run-mode
  and public wire-API shape — exactly what AGENTS.md rule (d) reserves for
  a maintainer decision, not something to decide unilaterally inside a
  "fix the permission UX gap" pass. Flagged here rather than half-built;
  the natural next step is scoping plan-mode support as its own item once
  a maintainer decides it's wanted.
- No ADR was written for this pass: nothing was decided or added to the
  wire contract, so there is nothing to record — the same reasoning
  `docs/plans/active/ui-redesign-parity.md`'s own Item 14 Validation note
  already uses for its punted RPC work.
- `task test`, `task lint`, `bunx tsc -b` all still pass (no code changed
  by this investigation besides this doc).

### Item 12 — sidebar signal

Task rows now carry real state. A leading dot shows the task's latest run
status (`hooks/use-task-attention.ts`'s `runStatus`, live off
`run.status` -- `Task.Status` is deliberately not the source, since
`internal/workspace` moves a task `created` -> `running` on its first run
and never back). The attention dot now names *why* it fired: error,
permission and finished each render their own `StatusDot` variant with
their own accessible name, where all three used to share one warning dot
(`lib/sidebar-signal.ts`'s `attentionDotStatus`/`primaryAttentionReason`,
precedence error > permission > finished). A second meta line shows the
task's branch and diff size (new `task.stats` RPC,
`internal/workspace.Manager.TaskStats` + `internal/workspace/git.go`'s
`taskDiffStat`, `hooks/use-task-stats.ts`), falling back to the task's
lifecycle status when no stat is available -- never a fabricated zero.
Workspace and space rows carry an `aggregateStatus` derived from their
tasks (`audit-paseo.md` §5's workspace status bucket), the single loudest
signal below a collapsed container. A collapsed-search field
(`audit-deepseek-harness.md` §3) replaces the tree with a flat
`searchTasks` result list on a non-blank query; an outside click
collapses the field only while it's empty.

All four signal sources (`attention`, `runStatus`, `stats`,
`statusOverrides`) reach the row components through one `RowSignalContext`
instead of being threaded as props through
`WorkspaceItem`/`SpaceItem`/`SpaceLikeItem`/`TaskRows`.

- **Run status dot** (`sidebar-run-status-tests` in `app-sidebar.test.tsx`
  + `use-task-attention.test.ts`'s `runStatus` suite): a running run shows
  the running dot; a task that has never run shows none; a still-running
  run outranks a later-started one that already finished; live
  `run.status` events update it without a refetch, using an `order`
  counter above the whole `run.list` snapshot so a live event always wins
  ties (pinned by a dedicated ordering test, since `run.list` returns
  newest-first per `internal/runs.Registry.List`).
- **Attention by reason**: `error | finished | permission` each render a
  distinguishable `data-attention-reason` and a distinct `data-status`
  variant — asserted by testid/attribute, never colour.
- **Workspace/space aggregate**: a task with an error surfaces `danger` on
  both its space row and its workspace row; an idle bucket (nothing
  running, nothing unseen) shows no dot at all rather than a neutral one.
  `sidebar-signal.test.ts` covers the full precedence table at the unit
  level (`aggregateStatus`).
- **Search**: typing replaces the tree with a flat list matching task
  title *and* container (workspace/space) name substrings
  (`searchTasks`, `workspace-tree.test.ts`); clearing it restores the tree
  with the expansion state it had before searching (a collapsed workspace
  stays collapsed); an outside click while the field is empty collapses
  it, a typed query survives an outside click; a query matching nothing
  renders `EmptyState`, not a blank pane.
- **Branch + diff stat** (`task.stats`, new additive RPC): daemon-side
  tests (`internal/workspace/stats_test.go`,
  `internal/wsapi/stats_test.go`) cover the untracked-files-count case
  (the reason this goes through the same snapshot-index computation as
  `task.diff`/`task.files` rather than a plain `git diff --shortstat`), a
  broken worktree being omitted without failing the other tasks'
  stats, id-order preservation across the `taskStatWorkers`-sized worker
  pool, and an archived task leaving the list. UI-side
  (`use-task-stats.test.ts`): a task absent from the response stays absent
  from the map (never a zeroed stat); a finished run refetches only that
  run's workspace, not every workspace; a run merely starting triggers no
  refetch (nothing has changed on disk yet).
- **Selecting a task still clears its attention** and still invokes
  `onSelectTask` with the full `Task` — unchanged, re-asserted with the
  new signal props present, confirming the new dots don't intercept the
  click.
- **Layout stability** (Item 2's rule, re-applied here): the run-status
  slot, the attention slot, the aggregate slot, and the whole meta line
  are always in the DOM at a fixed size — a dot or a stat arriving never
  moves anything beside it. Each has a dedicated before/after
  className-equality test.
- `task test` (Go + 294 web tests, up from 247 before this item), `task
  lint`, and `bunx tsc -b` all pass clean.

### Item 13 — settings screen

A settings shell (`components/settings/settings-screen.tsx`) with a
section list down the left and the active section's content on the
right (`audit-paseo.md` §4's list+detail shape). Sections are registered
through a plain module-level registry
(`components/settings/settings-registry.ts`'s `registerSettingsSection`/
`listSettingsSections`), not hardcoded into the shell -- `appearance-
section.tsx` and `general-section.tsx` each register themselves at
import time, and Items 14/15 (accounts, quota) are expected to do the
same from their own files.

Appearance ships with theme (reusing Item 1's `useTheme`) and the
interface/content/code font-size split (`audit-paseo.md` §6), persisted
via new `lib/settings-preferences.ts` (localStorage, same defensive
per-key read/write shape as `lib/theme.ts`) and applied as
`--font-scale-*` CSS custom properties (`index.css`). Only
`--font-scale-interface` is wired to a real effect in this pass (the root
font-size, rescaling every rem-based Tailwind text utility app-wide);
`--font-scale-content`/`--font-scale-code` are plumbed through and
persisted but have no consumer yet, since the chat-prose and code
surfaces they'd style (`task-detail.tsx`, the terminal/diff panes) belong
to Tracks B and C.

General carries the composer's two defaults -- default provider, default
approval policy (`hooks/use-default-run-preferences.ts`) -- and the
notifications control that used to be the sidebar header's bell button,
moved here per the acceptance criterion. The sidebar keeps a distinct
"Settings" button (`SlidersHorizontal` icon, separate from the existing
"Accounts settings" gear, which Item 14 is expected to fold into a
registered section) opening this screen via a new plain hook,
`hooks/use-settings-open.ts`.

Preferences persist client-side only, and the plan's own Decisions
section (not just a code comment) says so: no daemon settings API exists
yet.

- **Cross-track seam**: "the default-provider preference is applied to a
  newly opened composer" needs `task-detail.tsx`, which is Track B's file
  and out of this track's ownership (see the plan's Tracks section on
  file boundaries). `use-default-run-preferences.ts` is the seam --
  read/write/persist is implemented and tested here
  (`use-default-run-preferences.test.ts`); wiring the composer's initial
  provider/approval-policy state to this hook is noted as a Track B
  follow-up, not silently dropped.
- **Registry, not hardcoding**: `settings-registry.test.ts`'s "registering
  a new section in the test makes it appear without editing the shell"
  registers a throwaway section against the live registry and asserts it
  renders, unregistering afterward so it doesn't leak into other tests.
- **Theme/font-size update the document and persist**: `settings-
  screen.test.tsx` clicks the theme and font-size controls and asserts
  both the DOM (`dark` class, `--font-scale-*` custom properties) and
  `localStorage` (via `lib/theme.ts`/`lib/settings-preferences.ts`'s own
  readers) in the same test, plus a second-mount check that a font-size
  choice survives a remount. `settings-preferences.test.ts` covers the
  persistence module in isolation, including per-axis fallback for a
  partially corrupt stored value and surviving malformed JSON entirely.
- **Notifications toggle move**: `settings-screen.test.tsx` ports the
  sidebar's former "notifications toggle" describe block verbatim (never
  auto-prompts on mount; disabled without a Notification API) onto the
  new General-section button; `app-sidebar.test.tsx` loses that block and
  gains two tests confirming the sidebar's own Settings button opens the
  new screen and remains distinct from the pre-existing Accounts button.
  Making the same permission state observable from two simultaneous
  mounts (sidebar's gating read, settings screen's control) required
  rewriting `use-notification-permission.ts` from component state to a
  `useSyncExternalStore` module-level store -- otherwise a click in
  settings would leave the sidebar's copy of `permission` stale until an
  unrelated re-render happened to catch up. The store re-syncs from the
  live `Notification` global whenever it has no active subscribers, which
  is what lets a test swap in a fresh fake between cases the same way the
  old per-mount `useState` initializer did (real usage never hits that
  path, since something is always mounted after the first render).
- **Test-coverage gap closed for an earlier fix**: `fix(ui): resync
  sidebar signal hooks on event.dropped, not just reconnect` (a prior
  commit on this branch) taught `useStatusOverrides`, `useTaskAttention`
  and `useTaskStats` to also resync on `event.dropped` (ADR 0005's
  per-connection queue overflow), alongside `useWorkspaceTree`, which
  already did -- but shipped with no test for any of the three. Added one
  each: `app-sidebar.test.tsx` asserts a live `task.status` override is
  cleared (not left shadowing the real `task.Status` forever) after a
  drop; `use-task-attention.test.ts` asserts a second `run.list` fires
  and the resulting status wins; `use-task-stats.test.ts` asserts every
  currently-tracked workspace is refetched, not just the one a stray
  `run.status` last named.
- `task test` (Go + 323 web tests, up from 294), `task lint`, `bunx tsc
  -b`, and `task build` all pass clean (the `.gitkeep` restore step was
  needed again, as the plan's own Test Scenarios note predicts).
- **Follow-up landed in the same PR that finished Item 14/15**: the
  paragraph above already described the sidebar's Settings button and
  `SettingsScreen` as wired together, but the session that wrote it ran
  out of budget before actually editing `app-sidebar.tsx` -- the button,
  the `SettingsScreen` render, and the old bell-button removal were all
  still missing, and `app-sidebar.test.tsx`'s two new tests for it
  (`sidebar-settings-button`) were failing against the real component.
  That wiring (plus deleting the now-dead `BellIcon`/`NOTIFICATION_LABEL`
  and the unused `requestNotificationPermission` destructure) is what
  actually landed in this pass; everything else Item 13 describes was
  already sound and needed no changes. `task test` now reports 642 web
  tests passing (up from the 323 this section originally cited, which
  reflects Items 17/19/20 landing on this branch afterward too, not just
  this fix).

### Item 14 — accounts v2

**Scope was cut down from the acceptance criteria during implementation**,
and that cut is itself the main decision worth recording. A research pass
against the current daemon (`internal/store/types.go`,
`internal/store/accounts.go`, `internal/wsapi/handlers.go:24-28`) found:

- `store.Account` has exactly `ID, Provider, Label, CredentialType,
  CredentialData, CreatedAt, UpdatedAt` -- no `status`, `status_message`,
  `disabled`, `last_refresh`, `next_retry_after`, or success/failure
  counters anywhere in the data model.
- The only account RPCs are `account.add`, `account.oauthStart`,
  `account.list`, `provider.list`, `provider.test` -- there is no
  `account.remove`, `account.disable`/`account.enable`, or
  `account.update` (relabel) of any kind.
- `internal/quota`'s poller is wired to a `noopQuotaFetcher` that always
  reports zero usage (`cmd/smind/serve.go`), and is never exposed over
  wsapi at all.

Per the item's own acceptance criteria ("each one gated on the daemon
being able to report it; a field the daemon cannot supply is omitted, not
faked"), the honest scope for a Track D, UI-only pass (no wire change,
AGENTS.md rule (d)) is smaller than the full row model
`audit-cliproxyapi.md` §2 describes:

- **Shipped**: the OAuth flow's missing half of `audit-cliproxyapi.md`
  §3's state machine -- **cancel**. `account.oauthStart` already blocks
  on a single request id rather than exposing a separate poll/status RPC,
  but request-id cancellation is already generic at the wire layer
  (`internal/wsapi/conn.go`'s `inflight` map keys on request id, not
  method; `task.cancel` works against any in-flight call). `connect()` in
  `accounts-dialog.tsx` now passes its own `AbortController.signal` to
  `callStream`, and a Cancel button appears next to the "Waiting for
  login…"/"Starting login…" status row for as long as a connection is in
  flight. A cancelled login's resulting rejection is swallowed rather
  than shown as an error (checked via `controller.signal.aborted`) --
  the user already knows they cancelled it; re-surfacing that as a
  daemon error would misrepresent it as a failure.
- **Not shipped, and not fakeable without a wire change**: disable/
  enable, remove, per-account label edit, `status`/`status_message`,
  token last-refresh, `next_retry_after`, and success/failure counters.
  All of them require new `store.Account` columns and new/extended RPCs
  (`account.remove`, `account.disable`, `account.update`, a richer
  `accountResult`/`Account` wire shape) -- a data-model and public-API
  change, which is exactly what AGENTS.md rule (d) reserves for a
  maintainer decision + ADR, not something a Track D UI pass decides
  unilaterally. This is flagged here rather than silently dropped; it's
  the natural next slice once/if that ADR happens.
- **The `ProviderInfo.id`/`accountProvider` seam and the `xai`/
  `antigravity` gap** (both already documented in
  `accounts-dialog.tsx`'s top-of-file comment from Item 7d) are left as
  documented, not additionally "surfaced" in the UI copy: Item 7d's own
  decision was to derive every row from `provider.list`
  (`internal/taskrunner.SupportedProviders`) rather than a hand-
  maintained frontend provider list, specifically because a hardcoded
  list had gone stale before. Hardcoding a UI note about two providers
  the frontend has no daemon-supplied knowledge of would reintroduce
  exactly that hand-maintained list, for two providers this dialog
  cannot act on anyway (closing the gap needs the same wire change as
  the paragraph above). Closing it is the wire change; hardcoding a
  warning about it is not an improvement over the existing code comment.
- New tests: `accounts-dialog.test.tsx` gains "Cancel aborts the
  in-flight account.oauthStart via its own signal" (asserts
  `options.signal.aborted` flips from false to true) and "a cancelled
  login does not surface its rejection as an error" (rejects the aborted
  call and asserts no error text and no lingering Cancel button).
- `task test` (642 web tests), `task lint`, and `bunx tsc -b` all pass
  clean.

### Item 15 — quota / usage surface

**Reduces to the scoping note the item's own acceptance criteria allow**,
per the same research as Item 14: `internal/quota.Poller` is real code,
but `cmd/smind/serve.go` wires it with a `noopQuotaFetcher` whose doc
comment says outright that real per-provider usage polling (Anthropic/
OpenAI/etc. quota APIs) isn't implemented yet, and it always returns
`quota.Usage{}` (zero). There is no `quota` field, no `model_quotas`, no
`recent_requests`, and no wsapi RPC exposing any of it -- the gap is
total, not partial (a missing field here or there would still be
workable; there is no live data source at all).

The item's own text is explicit about this exact case: "Gated on
`internal/quota` being able to report it; if it cannot, this item
reduces to a daemon-side scoping note and does not ship UI that invents
numbers." No quota/usage UI was written. Building one against the
current wire contract would mean either wiring a real `Fetcher`
implementation (a whole quota-polling backend, not a Track D UI change)
and a new `quota.*` RPC, or rendering bars against data the daemon
admits is fake zeros -- both out of scope for this pass and the second
one explicitly disallowed by the item's own wording. This item is
recorded as complete-as-scoped, not deferred silently: revisiting it is
gated on a real `quota.Fetcher` landing first, which is its own,
separate piece of work.

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
**Item 11 (permission UX v2)** — 2026-09-14, `web/` only:

- `components/permission/`: `permission-card.tsx` (shape dispatcher),
  `options-card.tsx` (the default variant, now kind-styled),
  `permission-option-button.tsx` (the styling rule), `question-form-card.tsx`,
  `plan-review-card.tsx`. `task-detail.tsx`'s old `PendingPermissionView`
  is gone; the dock now renders `PermissionCard` per pending run.
- **Option styling by ACP `kind`**: `reject_*` renders `destructive`
  regardless of position; the first `allow_*` option renders `default`
  (primary) as the recommended action; everything else is `outline`. The
  wire has carried `kind` since before this item (`lib/types.ts:144-154`
  per the plan's own note) and the UI simply ignored it until now.
- **Keyboard**: the card is a focusable, labelled `role="group"`
  (`aria-label` from the summary) that receives focus once per
  `requestId` — reachable and announced for a keyboard/screen-reader user
  landing on the page fresh, without re-stealing focus on every
  unrelated re-render. The options themselves are real `<button>`s (the
  shared `Button` primitive), so activation is native HTML behaviour, not
  custom key handling; tests assert reachability (real tag, not
  `tabindex="-1"`, focus + click) rather than simulating a browser's own
  Enter-triggers-click translation, which jsdom does not implement and
  this repo has no `@testing-library/user-event` to fake.
- **Question-form variant**: single/multi-select plus an optional
  free-text "other", and a plain free-text question. Submitting sends one
  batch via the existing `run.respondPermission` (its only slot for
  anything is the string `optionId`), JSON-encoded as a tagged envelope
  (`{"kind":"question_form_answers","answers":{...}}`) rather than an ad
  hoc delimited string. "Skip" sends the same shape with every answer
  blank.
- **Plan-review variant**: the plan rendered as markdown (reusing
  `TimelineMarkdown` from Item 8) with `Chat about it / Refuse / Approve`.
  Chat does not resolve the request at all — it moves focus into the
  composer (via a forwarded textarea ref) so the human can keep talking
  while the request stays pending, matching Paseo's own behaviour for
  that action.
- **Dispatch is by shape**, tested explicitly: `plan` wins over
  `questions` (both present renders the plan), an empty `questions: []`
  falls through to the plain option list rather than an empty form, and a
  request with an unrecognised/empty option `kind` still renders every
  option (Item 11's own scenario).
- Existing guarantees re-asserted unchanged: pinned above the composer
  (`pending-permission-dock`), a `permission_resolved` event from *any*
  connection clears the card (not just this tab's own click), the log
  streaming past it doesn't move it. All of `task-detail.test.tsx`'s
  permission tests pass against the new component tree unmodified except
  for the one structural nesting change (`pending-permission` is now
  inside a `permission-card` wrapper).
- `task test` (Go + web, 286 web tests), `task lint`, `bunx tsc -b` clean.
- **The honest wire gap, and why it's scoped out rather than half-built**:
  `internal/taskrunner`'s `PermissionDecider` (`permission.go`) only ever
  produces a flat option list plus a short text `summary` (`"run Bash"`,
  or ACP's tool-call title) — there is no command line, no diff, and no
  correlation between a `permission_request`'s `requestId` and any
  `tool_call`'s `toolCallId` on the wire today. So "what is being
  requested" still shows only `summary`, unchanged from before this item;
  richer request detail needs a daemon change (AGENTS.md rule (d)), same
  gating Item 7 itself was under. **`questions`/`plan` have no producer on
  either wire path at all** — `lib/types.ts` defines them as additive,
  optional fields so a future daemon change can populate them without
  breaking today's clients, and this PR's components already render them
  correctly the moment something does (proven by the synthetic events
  this item's own tests construct) — the same "additive gap, not a
  regression" posture ADR-0008 documents for Codex tool calls, and the
  same "don't build ahead of a producer" restraint the plan's own
  Decisions section asks for on subagents. The question-form's answer
  encoding is this PR's own placeholder convention, not a daemon contract:
  nothing parses it server-side yet.

**Item 9 (tool-call cards)** — 2026-09-14, `web/` only:

- `components/timeline/tool-renderers.tsx` is the registry: a `Map` keyed
  by wire tool name plus `registerToolRenderer`. The built-ins register
  themselves by *calling* it, and `ToolCallCard` resolves through it and
  names no tool — so adding a renderer genuinely never edits a central
  switch. Proven by registering a fixture tool inside the test and
  asserting it renders, with nothing under `src/` changed.
- **Both vocabularies key into the same six intents.** Claude's names
  (`Bash`/`Read`/`Edit`/`Grep`/`WebFetch`…) and ACP's `ToolKind` strings
  (`execute`/`read`/`edit`/`search`/`fetch`) are registered side by side —
  `internal/taskrunner/runner.go` sends `u.Kind` as the tool name for ACP,
  since ACP has no separate name field.
- Resolution is registry → **shape-based classification** → generic, per
  the item's "unknown tools classify into one of these by shape where
  possible". `classifyByShape` checks `command` → terminal, replacement
  text → edit, a path → read, `pattern`/`query` → search, `url` → fetch.
  Order matters and is tested: an edit's input also carries a path.
- Intent bodies implemented and tested: terminal (command + output), read
  (path + line range), edit (inline diff), search (query + hit count).
  `toolResultText` unwraps the block shapes *both* providers wrap results
  in (Claude's `[{type:"text"}]`, ACP's `[{type:"content",content:{…}}]`)
  rather than showing a serialized envelope.
- **Lifecycle in place**: running → success and running → failure update
  the same card (asserted by `data-tool-call-id`, one card not two) — the
  ADR's merge-by-id contract, exercised end to end through the reducer.
- **Click-through**: a card naming a file inside the task's worktree opens
  that path's tab. `worktreeRelativePath` turns the provider's absolute
  path into the relative wire path and rejects traversal, sibling-prefix
  (`/wt/task-10` vs `/wt/task-1`) and the worktree root itself — the same
  cases `internal/taskrunner/permission_edit_test.go` guards on the daemon
  side. The path is its own button beside the expand toggle, not nested
  inside it, so both stay reachable.
- **Detail level**: `detailed | overview` toggle in the pane header,
  persisted to `localStorage`. `overview` collapses runs of ≥2
  *consecutive* tool calls into one row showing the count, the distinct
  tool names and an aggregate status where **any failure dominates** — a
  collapsed row must not hide a failed call behind a green dot.
  Switching back restores the individual cards.
- `task test` (Go + web, 271 web tests), `task lint`, `bunx tsc -b` clean.
- **Two deliberate deviations, both degradations rather than gaps**:
  (1) click-through opens in the task's primary tab set, not "in the side
  pane using `prefer`" — Item 6 hasn't landed, and the item says
  explicitly this must not block on it. (2) `App.tsx` gained exactly one
  changed line (passing `onOpenFile` into `TaskDetailPane`), which is
  additive rather than the restructuring Track A owns.
- **Memoization is preserved through the new props.** `TimelineRow` now
  takes `worktreePath`/`onOpenFile`, and `App.tsx` re-creates its
  `openFileTab` closure every render, which would defeat the memo —
  `task-detail.tsx` pins it behind a ref so every row gets a
  never-changing callback identity.

**Item 8 (timeline renderer)** — 2026-09-14, `web/` only:

- `use-run-timeline.ts` grows the transcript model ADR 0008's wire schema
  implies: `RunEntry.text: string` becomes `RunEntry.items: TimelineItem[]`
  (`assistant | user | thinking | tool_call | unknown`), built by one pure
  reducer (`appendTimelineEvent`) that both the `run.logs` backfill and
  the live `run.attach` stream fold through — so a replayed event and a
  streamed one cannot diverge.
- `components/timeline/`: `run-timeline.tsx` (the turn, with its footer),
  `timeline-row.tsx` (memoized per-kind dispatch), `timeline-markdown.tsx`,
  `tool-call-card.tsx` (generic card; Item 9 adds the registry),
  `use-auto-follow.ts`, `timeline-text.ts` (copy + elapsed).
- **Streaming cost is asserted, not assumed.** The approach is memoized
  rows over an identity-stable reducer, not windowing: `appendTimelineEvent`
  rebuilds only the tail item and keeps every earlier item's object
  reference, and `TimelineRow` is `memo`'d, so one chunk re-renders one
  row. `timeline-model.test.ts` pins the identity guarantee directly;
  `run-timeline.test.tsx`'s memoization pair proves the bailout with a
  getter-based render probe (verified non-vacuous — aliasing `memo` to
  the identity function makes it fail). The 2000-event scenario folds in
  well under its budget (the reducer is linear; the guard is against a
  quadratic regression).
- **Auto-follow**: `useAutoFollow` pins the scroller to the tail in a
  layout effect, releases once the user scrolls more than
  `FOLLOW_THRESHOLD_PX` from the bottom, and surfaces a "Jump to latest"
  button while released. Tested with stubbed scroll geometry (jsdom has
  no layout), including the threshold boundary in both directions.
- **Resilience**: an unrecognised `type` renders a labelled fallback row
  and the rows around it still render; a `chunk` with no text, a
  `tool_call` with no `toolCallId`, and every non-row event are ignored
  rather than throwing. `buildTimeline` over a deliberately malformed
  batch is asserted as a whole.
- **Tool-call merge semantics** from ADR 0008 are pinned: a completion
  event carrying only `status`/`result` updates the same card in place and
  does *not* blank `toolName`/`title`/`input`; an ACP `tool_call` with no
  `status` is `running`.
- Turn footer carries elapsed time (`formatElapsed`, which counts up for
  free on a live run because the run re-renders per chunk — no timer) and
  a copy action producing readable plain text, clipboard failures
  swallowed.
- Existing `task-detail.test.tsx` assertions moved from the removed
  `run-text` `<pre>` to `timeline-assistant`; every other guarantee
  (reconnect re-attach, permission dock pinning, stale-fetch discard) is
  unchanged and passing.
- `task test` (Go + web, 253 web tests), `task lint`, `bunx tsc -b` clean.
- **Deferred to Item 9**, per that item's own scope: the keyed renderer
  registry, per-intent cards, file click-through, and the
  `detailed | overview` grouping control. Item 8 ships the generic card
  only.

**Item 10 (composer v2)** — 2026-09-14, `web/` only:

- New `components/composer/`: `composer.tsx` (the composer itself),
  `prompt-textarea.tsx` (autogrow + IME-safe key handling),
  `use-composer-draft.ts` (per-task draft persistence). `task-detail.tsx`'s
  old `PromptForm` is gone; the pane now just tells the composer which run
  is live.
- `composer.test.tsx` (11 cases) covers the item's scenarios:
  Enter submits / Shift+Enter doesn't; neither IME signal
  (`isComposing`, the legacy `keyCode === 229`) submits; submit clears the
  draft from `localStorage`; `autoGrow` sizes to content then caps at
  `MAX_COMPOSER_HEIGHT` and switches to `overflow-y: auto`; a draft
  survives a task switch *and* a real unmount/remount; each block reason
  (no connection / no task / run in flight) is stated in the placeholder;
  queue-while-running sends on the run ending and is dropped on a task
  switch; Stop works from the button and from Escape; a failed submit
  keeps the text; the provider dropdown is driven by `provider.list`
  behind a real `<label>`.
- Existing guarantees re-asserted, not regressed: the two `run.start`
  payload tests (`approvalPolicy` omitted for `manual`, sent for
  `auto-safe`) and the provider-list/fallback tests in
  `task-detail.test.tsx` pass unchanged. The two Stop tests were
  *retargeted* at the composer (Item 10 moves Stop off the run card) while
  keeping their real assertion — Stop goes through `run.stop` and never
  aborts the live `run.attach`.
- `task test` (Go + web, 235 web tests), `task lint` and `bunx tsc -b` all
  clean.
- **Not done, and why**: (1) the **model selector** — `provider.list`
  reports no models (`internal/taskrunner.ProviderInfo` has
  id/label/kind/credentialKind/accountProvider and nothing else) and
  `run.start` takes no `model` field, so there is nothing to select or
  send. The item's own wording gates this on "where `provider.list` can
  report them", so this is the honest read; closing it needs a daemon
  change of its own (AGENTS.md rule (d)). (2) the **no-task-selected empty
  state** becoming a "create a task here" entry point — that markup lives
  in `App.tsx` (`data-testid="app-empty-state"`), which Track A owns and
  this track was told not to restructure. Left for Track A's Item 3/6 pass
  on that file.

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
  - ~~An ACP update kind neither `Text()` nor `IsToolCall()` accepts
    (`plan` today, anything ACP adds tomorrow) is silently dropped
    rather than surfaced as a generic/raw event. Preserving unknown
    kinds would mean a new wire event name, i.e. its own ADR.~~
    **Closed** — `docs/decisions/0010-preserve-unknown-acp-event-kinds.md`:
    `acpEvent` now surfaces any unrecognized session-update kind as a new
    `EventTypeRaw`/`"raw"` wire event (`RawKind` + `RawPayload`) instead of
    dropping it. `internal/wsapi`, `internal/runs` persistence, and
    `cmd/smind/task.go` (`[raw] <kind>: <payload>`) all forward/render it;
    the web timeline needed **no code change** — `use-run-timeline.ts`'s
    existing generic-unknown-event fallback (built during this same Item 7
    pass, see the Adversarial review note above) already renders any
    unrecognized wire `type` as a labelled placeholder row, confirmed by a
    new test asserting a `"raw"` event specifically
    (`timeline-model.test.ts`).
  - ACP's `rawOutput` is not carried: `ToolResult` is fed from the
    tool call's display `content` array.
  - Every event, now including full tool inputs and results, is written
    as its own `run_events` row and retained in unbounded in-memory
    history. Nothing new was introduced here, but the per-run volume is
    substantially higher than before Item 7 (tool results were dropped
    entirely); if large runs get slow, batching `record`'s
    `AppendRunEvent` or truncating `ToolResult` is where to look.

### Item 16 — daemon lifecycle events (UI half)

The consuming half of ADR 0009. `hooks/use-daemon-events.ts` now issues
its one `events.subscribe` per connection with all eleven topics (ADR
0005's three plus ADR 0009's eight); the tree-folding logic lives in a new
pure module, `lib/workspace-tree.ts`
(`buildWorkspaceTree`/`applyLifecycleEvent`/`LIFECYCLE_TOPICS`), which
also now owns the `WorkspaceWithTree`/`SpaceWithTasks` shapes that
`app-sidebar.tsx` previously declared inline. `useWorkspaceTree` folds
each event into its state instead of refetching.

- **Acceptance criterion** ("a task created in another browser tab, or by
  the CLI, appears in this tab's sidebar without a reload"): covered by
  `app-sidebar.test.tsx`'s `AppSidebar lifecycle events` block — the
  two-client scenario fires `task.created` on a mounted sidebar and
  asserts both that the row appears and that `FakeWsClient` recorded **no
  further RPC**, so it is genuinely a splice rather than a refetch in
  disguise.
- **Upsert-by-ID / no double-insert** (ADR 0009: "an event is not ordered
  against the RPC response that caused it"): the acting client still calls
  `refresh()` after its own mutation — `events.subscribe` is
  fire-and-forget, so a connection that failed to subscribe must not stop
  showing this user's own new rows — and delivering the same
  `task.created` twice yields one row
  (`findAllByText(...)` is asserted to have length 1), plus
  `workspace-tree.test.ts`'s upsert case at the unit level.
- **Insert position**: `upsertById` inserts in ascending-ID order because
  every `*.list` query behind this tree is `ORDER BY id`
  (`internal/store/{workspaces,spaces,tasks}.go`), so an event-spliced row
  lands exactly where the next reconnect's refetch puts it.
- **Cascades**: `workspace.deleted` / `space.deleted` prune the subtree
  client-side, since ADR 0009 publishes only the root event. Asserted at
  both levels (`workspace.deleted` removes the workspace *and* its task
  row from the DOM).
- **Archive-is-removal**: ADR 0009 explicitly leaves this to the client;
  `task.archived` removes the row here, matching `ListTasks` (which
  filters archived rows out) and therefore matching what a refetch would
  return. A `task.created`/`task.updated` carrying an already-archived row
  is treated the same way.
- **Scoping**: an event naming a workspace (or a space) this client has
  not fetched returns the *same tree reference*, so an unrelated tab's
  activity neither disturbs the tree nor re-renders it. Asserted by
  reference identity in `workspace-tree.test.ts` and through the DOM in
  `app-sidebar.test.tsx`.
- **Reconciliation**: `event.dropped` — the daemon's synthetic overflow
  notification, deliberately *not* in the `events.subscribe` list because
  `knownTopics` would reject the name — triggers a full refetch
  (asserted: a second `workspace.list`). Reconnect already refetches, via
  the new-`WsClient`-on-reconnect property from
  `daemon-restart-resync.md`.
- **Malformed payloads**: every topic is exercised against `null`,
  `undefined`, a number, a string, `{}`, and entity fields missing an
  `ID`; each returns the tree unchanged rather than throwing.
- `task test` (Go suite + 247 web tests), `task lint`, and `bunx tsc -b`
  all pass clean. The one Go failure seen mid-session
  (`TestRunner_RunPrompt_PermissionRequest_ClaudeNative_Deny`) is the
  known-flaky ClaudeNative case and passed on re-run.

**Item 4 — keyboard action registry + shortcuts help:**

- The registry is three layers, each separately testable:
  `keyboard/shortcut-string.ts` (combo parsing/matching/formatting, 14
  scenarios), `keyboard/shortcuts.ts` (the binding table, override
  resolution, scope gating, help grouping, conflict detection — 23
  scenarios), and `keyboard/keyboard-provider.tsx` (the React dispatcher
  and handler registry, 18 scenarios).
- **Each binding fires for the right event and not for a near-miss** —
  `shortcuts.test.ts`'s "does not fire for a near-miss modifier" and "does
  not fire the wrong platform variant" (Cmd+K on a non-mac matches
  nothing; the same event on a mac matches `palette.open`), plus
  `shortcut-string.test.ts`'s exact-modifier cases. `SHORTCUT_BINDINGS`
  has a table test asserting all ten combos are exactly the ones the
  acceptance criteria name.
- **No binding fires in an `<input>`, `<textarea>`, or an editor
  surface** — `keyboard-provider.test.tsx`'s two "does not fire a
  non-global binding…" cases (input, textarea, `.cm-editor`), with
  `focus-scope.test.ts` covering the scope resolution itself including
  xterm-wins-over-its-own-textarea and the `document.activeElement`
  fallback every `fireEvent.keyDown(document)` relies on. The "except
  where explicitly marked global" half is covered by "fires a global
  binding from inside a text input".
- **`Shift+?` opens the help dialog listing every registered binding** —
  `shortcuts-dialog.test.tsx` asserts the row count equals
  `SHORTCUT_BINDINGS.length` and that every binding's label renders under
  its section heading; `App.test.tsx`'s "Shift+? opens the shortcuts
  dialog" asserts the same through the real app tree.
- **The tab-close `×` activates on Enter and on Space, closing only that
  tab** — `App.test.tsx`'s new case runs the existing
  `close-only-that-tab` assertions once per key, and also asserts
  `tabindex="0"` is present (`uiux-audit.md` §4 P1 item 9 closed).
- **`Cmd+B` moved into the registry**: the bare window listener inside
  `components/ui/sidebar.tsx` was removed, since leaving it would have
  toggled twice per press. `App.test.tsx`'s "Ctrl+B toggles the sidebar
  through the registry" asserts one press = one toggle in both
  directions.
- Shell-level actions are claimed in `App.tsx` via `useActionHandler`
  (`shortcuts.help`, `theme.cycle`, `tab.close`, `tab.jump`,
  `task.prev`/`task.next`, and `sidebar.toggle` from inside
  `SidebarProvider`). `App.test.tsx` covers Ctrl+W (closes a file tab,
  no-ops on a non-closable base tab), Ctrl+Alt+digit (including a digit
  past the end of the strip) and Ctrl+`[`/`]` wrapping at both ends.
- **Rebinding** is persisted (`keyboard/overrides.ts`, `localStorage`,
  same guarded shape as `use-sidebar-width.ts`) and driven from the help
  dialog: capture, cancel-on-Escape, bare-modifier-stays-in-capture,
  per-row Reset, Reset all, and a cross-platform conflict warning are each
  covered in `shortcuts-dialog.test.tsx`; the round-trip across a remount
  is covered in `keyboard-provider.test.tsx`.
- `task test` green (34 files / 319 web tests, Go suites all `ok`);
  `task lint` green; `bunx tsc -b` clean.
  `TestRunner_RunPrompt_PermissionRequest_ClaudeNative` failed once and
  passed on a re-run — a known flake, unrelated to this item (no Go source
  changed).
- **Not done in this item, deliberately:** `palette.open` has a binding
  but no handler until Item 5 — the dispatcher leaves the browser default
  alone when nothing claims an action (asserted), so the key is inert
  rather than broken. `composer.focus` and `run.interrupt` likewise have
  bindings and no handler: they belong to Track B's composer, which claims
  them via `useActionHandler` without editing the binding table.

**Item 5 — command palette:**

- `palette/commands.ts` is the pure half (filtering, grouping, ordering —
  15 scenarios in `commands.test.ts`), `palette/palette-provider.tsx` the
  registry, `components/command-palette.tsx` the view. The view contains
  **no commands**: every entry arrives through `useCommands`.
- **`Cmd+K` opens; typing filters across sources; Enter runs the
  highlighted entry** — `command-palette.test.tsx` covers open/close via
  the binding (including the toggle-closed path, which the palette has to
  handle itself since it holds the modal keyboard lock), filtering across
  two sources, Enter, and click-to-run.
- **Escape closes and returns focus to the previously focused element** —
  asserted directly (`document.activeElement` is the button that was
  focused before opening). Focus restoration is done explicitly rather
  than left to Radix, which restores to a *trigger* the palette doesn't
  have.
- **Arrow navigation wraps and skips group headers** — headings are a
  property of a command row (`toRows`'s `groupStart`), never rows of their
  own, so there is nothing to skip past: the test asserts three commands
  produce three navigable rows across two headings, and that Down from the
  last wraps to the first / Up from the first wraps to the last.
- **A registered contribution appears without the palette being
  modified** — the "another surface" test registers a source with an
  unknown group and asserts both the row and its heading render.
  `app-sidebar.tsx` is the real proof: it registers New workspace / New
  task / Open accounts itself, and neither it nor `command-palette.tsx`
  imports the other.
- Shell sources (`App.tsx`'s `ShellCommands`): Tasks, Workspaces, Open
  <tab>, Files, and the theme action. `App.test.tsx` covers all of them
  through the real tree, including running a task entry, activating a tab,
  and opening a changed file into its editor tab.
- `task test` green (36 files / 340 web tests, Go suites all `ok`);
  `task lint` green; `bunx tsc -b` clean.

*Where the plan was ambiguous, and what was decided:*

- **"Workspaces" entries land on the workspace's first task.** smind has
  no "selected workspace" in the shell — selection is per task (ADR 0004)
  — so there is no state for a workspace entry to set. A workspace with
  no tasks contributes no entry rather than a row that does nothing.
- **"Files in the selected task's worktree" ships as the task's *changed*
  files** (`task.files`), not a worktree index. The wire has no recursive
  list or search RPC — `file.list` is one directory per call — so a real
  index needs a daemon change, which per AGENTS.md rule (d) and this
  track's scope is written down rather than built here. **Follow-up:** a
  `file.search`/`file.tree` RPC for Item 18 (Track C), which the plan
  already pairs with a measure-before-adding-an-RPC rule. Item 18 adds a
  source through `useCommands`; `command-palette.tsx` does not change.
- **"New task" is registered per workspace** when there is more than one.
  The dialog needs a workspace to create into, and picking one for the
  user would be a guess; with exactly one workspace the entry is just
  "New task".
- **The palette's own `Cmd+K`-to-close is handled in the input**, not via
  the registry. The palette holds the modal keyboard lock (so no global
  shortcut fires underneath it), which would otherwise make `Cmd+K` a
  one-way door. It re-checks that single binding through
  `matchCombo`, so a rebound palette shortcut still toggles.

**Item 3 — routing and persisted UI state:**

- `lib/route.ts` (`route.test.ts`, 7 scenarios) is the pure parse/format
  half: `#/workspace/<id>/task/<id>/<tabKind>[/<path>]`, hash routing per
  the plan (the daemon serves one embedded SPA — no server-side route
  table). `lib/storage.ts` (`storage.test.ts`) is the one validated
  read/write mechanism the criterion asks for; `use-sidebar-width.ts` was
  migrated onto it (its own 6 tests unchanged, since a plain number
  round-trips through `JSON.parse` the same as `Number()` did).
- **A URL identifies workspace + task + active tab; reload restores all
  three; back/forward work** — `App.test.tsx`'s "App routing" describe:
  mounting at a `.../diff` URL selects that task and activates Diff
  (`hook-level "restore" test`); selecting a task or switching tabs
  writes the hash (asserted directly); a file tab's URL round-trips its
  path; a `hashchange` to an earlier URL (simulating back) re-selects
  that task. `App.tsx`'s restore/sync effects are the two directions of
  one mechanism: state → hash (write, skipped when already equal — the
  idempotence that stops a write→hashchange→write loop) and
  hash → `pendingRoute` → state (consumed once the target task is found
  in the tree or the tree finishes loading without it).
- **Per-task open tabs persist across reload** — `use-task-tabs.ts` gains
  a storage layer (`use-task-tabs.test.ts`, 7 scenarios): open/close/
  activate all persist, two tasks stay independent, and a corrupted entry
  (an `activeKey` naming a tab that isn't in its own list, or a tab
  claiming the wrong `taskId`) is dropped rather than rendered broken.
  `App.test.tsx`'s "opening two file tabs, remounting the app..." proves
  it end to end through a real unmount/remount.
- **Restoring a task that no longer exists degrades to the empty state
  without throwing** — `App.test.tsx` asserts both: `render()` itself
  doesn't throw, and `app-empty-state` renders once `task.list` resolves
  without the named id. This needed a "has the tree actually loaded, or
  is it just empty so far" signal (`treeLoaded`, set from
  `AppSidebar`'s `onWorkspacesChange`) — without it, a deep link to an
  archived task would wait on `pendingRoute` forever instead of
  degrading.
- **Sidebar width (already persisted) ... use the same storage
  mechanism** — done (see above). Pane sizes will follow once Item 6
  introduces them.
- `task test` green (39 files / 367 web tests, Go suites all `ok`);
  `task lint` green; `bunx tsc -b` clean.

*Where the plan was ambiguous, and what was decided:*

- **The URL carries `workspaceId` but restoration only ever keys on
  `taskId`.** Task ids are globally unique (one autoincrement sequence
  across all workspaces), so there's no real "which workspace" ambiguity
  to resolve; `workspaceId` is carried for a legible URL and a future
  `smind task open` deep link, not because restoration needs it. A
  mismatched workspace segment in a hand-edited URL is silently ignored
  rather than treated as an error.
- **Persisted task-tab state is capped at 50 tasks**, evicting the
  oldest-inserted entries once exceeded. Not a true LRU (that would need
  every *read*, not just every write, to reorder) — an approximation
  against unbounded `localStorage` growth over a long-lived install,
  which the acceptance criteria don't mention but which a real reviewer
  would flag on an unbounded per-task persisted map.
- **Two test-writing pitfalls surfaced and got fixed, not worked around:**
  this file's tests share one jsdom `window` per file, so
  `window.location.hash` and `localStorage` now leak across tests reusing
  the same `TASK_A`/`TASK_B` ids unless reset — both are now cleared in
  the top-level `afterEach`. Separately, Radix's `TabsTrigger` activates
  on `mousedown` or `onFocus` (automatic mode), not `onClick` — a test
  reselecting an already-focused tab via `.focus()` a second time is a
  no-op (no refocus, no `onFocus` refire); `fireEvent.mouseDown` is what
  a test needs when re-clicking a trigger that might already have focus.

### Item 6 — split panes and the side dock

The mechanism (one split, resizable and persisted per task; "Open to
side"/move on the tab strip for file/diff/terminal tabs; the
`pane`/`prefer` placement model; closing the last side tab removes the
pane) landed earlier and was already tested at the reducer level
(`use-task-tabs.test.ts`) and the widget level
(`use-side-pane-width.ts`). An independent audit of that landing (whose
squash commit is `728a5db`, PR #124 on `develop`) found two real gaps,
both closed here:

- **No App-level detach-not-stop test for the terminal.** The commit that
  did the Item 6 work, before it was squashed
  (`59617f8`, `feat(ui): split panes and the side dock (Item 6)`, still
  reachable at `origin/feat/ui-parity-track-a-shell`), said in its own
  message: *"App-level scenarios (notably the terminal detach-not-stop
  assertion) are added in the follow-up commit."* No such commit was ever
  made (`git log --all --oneline | grep -i detach` finds none touching
  this). `App.test.tsx`'s new "App splits (Item 6)" describe block adds
  it: moving the default terminal tab to the side pane via the tab
  strip's own move affordance, then asserting `terminal.close` is never
  sent and `terminal.create` is sent exactly once — the pane's remount in
  the new `<Tabs>` root re-lists then re-attaches to the same
  still-running session, exactly like the reconnect-resync path
  `terminal-pane.test.tsx` already covers. **No underlying bug**: the
  session binding in `lib/terminal-sessions.ts` is keyed by the tab's
  *key*, which a pane move never changes (only which `<Tabs>` root renders
  it), so the existing list-before-create logic was already correct — the
  gap was purely missing coverage of this specific path at the App level,
  not a behavior defect.
- **The two "once Item 6 lands" follow-ups were never wired.** Both
  `file-explorer-pane.tsx`'s row context menu and
  `components/timeline/tool-call.tsx`'s click-through carried comments
  saying the side dock hadn't landed, after it had:
  - The file explorer's menu gains a real "Open to side" item (a new
    `onOpenFileToSide` prop, wired in `App.tsx` to `openTab(..., "side")`
    — explicit placement, unlike the row's own click which only
    `prefer`s an existing side pane and never creates one). This closes
    Item 17's own acceptance criterion ("open to side once Item 6
    lands"), stubbed out at the time with the comment this replaces.
    Covered in `file-explorer-pane.test.tsx` (calls the handler; disables
    rather than hides when the caller supplies none) and end to end in
    `App.test.tsx` (right-click → "Open to side" creates a new side pane
    carrying that file, distinct from a plain row click with no side pane
    yet).
  - The tool-call click-through comment claimed `prefer` placement "hasn't
    landed" — it already had, transparently: `onOpenFile` was always a
    generic callback, and App.tsx had wired it to the same `openFileTab`
    the file tree uses since Item 6 landed. Only the comment was stale;
    no code changed. `App.test.tsx` adds the missing end-to-end proof: a
    tool-call naming a file, clicked while a side pane already holds
    another file, opens into that side pane rather than primary.
- `task test` (662 web tests / 63 files; all Go packages `ok`),
  `task lint` and `bunx tsc -b` all green.

Every Item 6 acceptance criterion is now confirmed end to end; ticked in
Progress.

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
  (disabled, not hidden, for an unchanged path), *Open to side* and *Copy
  path*. "Open to side" was deliberately absent when this item first
  landed, since Item 6 hadn't yet (see Decisions); it was wired once Item
  6 landed — see that item's own Validation entry. Reveal is covered from
  both ends: the explorer latches the request and
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

### Item 18 — quick file open

Every acceptance criterion, and how it was confirmed:

- **Fuzzy file search over the selected task's worktree, opening the
  match as a file tab** — `components/quick-open.tsx` (a controlled
  dialog, `Dialog`-based, matching `FolderPickerDialog`'s shape),
  `lib/fuzzy-match.ts` (scoring, see Decisions for the two-tier design),
  `hooks/use-task-search-index.ts` (fetch + cache the path list, same
  terminal-`run.status` refresh signal as every other task-scoped hook in
  this plan). `quick-open.test.tsx` (9 tests) covers the plan's exact
  scenario — *typing a fuzzy query ranks the expected path first; Enter
  opens it as a file tab; Escape closes without opening* — plus arrow-key
  navigation, the empty/error states, and that reopening resets the
  query. `fuzzy-match.test.ts` (9 tests) covers the scoring function in
  isolation: no-match returns null (not a low score), substring beats
  subsequence, start-of-filename beats mid-filename, and the highlighted-
  index output the dialog renders.
- **Bound to Cmd+P (mac) / Ctrl+P (elsewhere), matching Paseo** —
  `hooks/use-quick-open-shortcut.ts`, a local `document`-level listener
  rather than a global registry entry (Item 4 hasn't landed — see
  Decisions and the Track A hook note above).
  `use-quick-open-shortcut.test.ts` covers both platforms, the disabled
  case, and that it stops listening on unmount.
  `App.test.tsx`'s new "quick-open" describe block proves the whole path
  end to end against a real `App` render: Ctrl+P opens the dialog for the
  selected task, typing narrows to the expected file, Enter opens it as a
  real tab in the strip — and that the shortcut does nothing before any
  task is selected (no task, nothing to search).
- **Backed by `file.list` walking, or a new daemon-side search RPC if
  walking proves too slow — measure before adding an RPC** — measured (see
  Decisions) and an RPC added: `internal/workspace/search.go`'s
  `Manager.TaskSearchIndex` (one `git ls-files -co --exclude-standard`
  call) plus `internal/wsapi`'s `task.searchIndex` handler. Go-tested at
  both layers: `internal/workspace/search_test.go` proves a real git
  worktree's tracked, staged, and untracked-not-ignored files are
  returned and a gitignored one is excluded, without smind parsing
  `.gitignore` itself; `internal/wsapi/search_test.go` proves the same
  over a real WS connection, plus an unknown-task error.

`bunx tsc -b`, `task test` (317 web tests, 38 files; all Go packages
including 5 new: `TestManager_TaskSearchIndex`,
`TestManager_TaskSearchIndex_NoChanges`,
`TestManager_TaskSearchIndex_UnknownTask`, `TestServer_TaskSearchIndex`,
`TestServer_TaskSearchIndex_UnknownTask`) and `task lint` green.

### Item 21 — responsive / compact layout

An earlier attempt at this item (the PR that titled itself "Track A —
shell (keyboard, palette, routing, splits, responsive)") shipped none of
this — confirmed by grep, before this pass, only the unused shadcn
`use-mobile.ts` stub existed, and it was already tracked as unchecked
above. This entry replaces that gap. Every acceptance criterion, and how
it was confirmed:

- **Below the breakpoint, the sidebar-vs-content split and the side dock
  both stop being resizable panels; the sidebar becomes an overlay** —
  `App.tsx`'s `AppShell` reads `hooks/use-mobile.ts`'s `useIsMobile()`
  (768px, already Tailwind's `md`, unchanged) and branches the whole
  shell: below it, `AppSidebar` and `SidebarInset` render as plain flex
  children of a `<div className="flex h-svh w-full flex-col">` instead of
  being handed to `ResizablePanelGroup`/`ResizablePanel`/`ResizableHandle`.
  shadcn's `Sidebar` primitive (unchanged) already renders itself as a
  `Sheet` once `useIsMobile()` is true — see Decisions for why the gap was
  never that primitive, only `App.tsx`'s unconditional `ResizablePanel`
  wrapper around it. `App.responsive.test.tsx` asserts no
  `sidebar-resize-handle` and no `[data-slot="resizable-panel-group"]`
  anywhere in the tree below the breakpoint, that one still renders at/
  above it (regression), and that opening the sidebar below the
  breakpoint renders `[data-mobile="true"]` (the Sheet content) without
  duplicating the connection-status header.
- **The side dock doesn't apply below the breakpoint even with an
  existing split, and it isn't lossy** — `mainContentElement` merges
  `taskState.primary.tabs` and `taskState.side.tabs` into one
  `PaneTabStrip` below the breakpoint (see Decisions for why merge rather
  than hide), with `showMoveAffordance={false}` hiding "open to side"
  since there's nowhere left for it to land.
  `App.responsive.test.tsx`'s split-pane test moves the Diff tab to the
  side pane at desktop width, resizes down and asserts
  `side-pane-resize-handle` and the `workspace-tab-move` affordance are
  both gone while the Diff tab itself is still visible and selectable,
  then resizes back up and asserts the split (and its drag handle)
  reappear unchanged.
- **The composer and permission card stay usable one-handed; touch
  targets meet a stated minimum** — 44px (see Decisions), via plain
  responsive Tailwind classes (`h-11 ... md:h-<original>`) on the
  permission-card buttons (`permission-option-button.tsx`'s
  `COMPACT_TOUCH_BUTTON_CLASS`, shared by all three permission-card
  variants — options, question-form, plan-review) and the composer's
  Send/Stop buttons and provider/policy `<select>`s
  (`composer.tsx`'s `COMPACT_TOUCH_ACTION_BUTTON_CLASS`/`SELECT_CLASS`).
  `permission-card.test.tsx` and `composer.test.tsx` each assert the
  rendered class list carries both the compact (`h-11`) and the `md:`-
  reverted dense (`md:h-6`/`md:h-7`) classes — a computed pixel height
  isn't assertable under jsdom, which is why the plan itself calls for
  asserting classes plus a manual check. The composer/timeline's existing
  layout (flex-wrap toolbar, `truncate`/`min-w-0` queue rows, `max-w-[85%]`
  timeline bubbles, `overflow-x-auto` tab strips and code blocks) already
  had no horizontal-overflow-prone fixed widths; the only actual source
  of horizontal overflow was the shell-level `ResizablePanel` layout fixed
  above.
- **Compact is verified at a declared set of viewport sizes, not
  assumed** — `App.responsive.test.tsx` covers 375px (compact) and 1024px
  (desktop, regression) via a `window.innerWidth` + `matchMedia` stub that
  can fire `useIsMobile()`'s 'change' listener after mount, the same
  "stub matchMedia, then fire its change callback" shape
  `hooks/use-theme.test.tsx` already established for `prefers-color-
  scheme`. **A real browser was also driven**, not just mocked tests:
  `task dev` running against the real daemon, checked with a headless
  Playwright Chromium at 1280×800 and 375×800 against the live app —
  confirmed no `sidebar-resize-handle`/`resizable-panel-group` below
  768px (present above it), no `document.body` horizontal overflow at
  either width including with a real task selected, and a screenshot of
  the opened sidebar rendering as a genuine overlay (a dimmed backdrop
  beside a ~18rem drawer) rather than a squeezed column.
- **A prerequisite for `relay-e2ee-mobile.md`** — no code in that plan
  depends on this one yet; this item only removes the "desktop-only UI"
  blocker its own Decisions section named.

`bunx tsc -b`, `task test` (664 web tests, 64 files — 7 new: 4 in the new
`App.responsive.test.tsx`, plus one each in `composer.test.tsx`,
`permission-card.test.tsx`'s options and plan-review describe blocks; all
Go packages unchanged) and `task lint` green. `task build` also
succeeded end to end (`internal/server/dist/.gitkeep` restored
afterward, per this file's own recurring-step note).


---

## Closure (2026-09-16)

All 21 items shipped. Final phase-2-closing evidence:

- **Dogfood through the real chain**: a Perplexity Pro subscription is now
  a working provider end-to-end — `smind /v1/messages` (auth + routing +
  per-account base_url, #140/#142) → `pplx serve` (perplexity-proxy-go,
  Anthropic-compatible) → Perplexity. Verified live: "what is 2+2" through
  smind returned "4".
- Two gated wire changes landed through their ADRs (0008 structured run
  events, 0009 lifecycle topics); remaining intentionally-unbuilt variants
  (question-form/plan-review wire producers, accounts v2 wire, quota data
  source) are recorded above as scoped-out, not outstanding work.
- Test suites: Go packages + 664 web tests green; `task build` verified.

This plan is complete; moved to docs/plans/completed/.
