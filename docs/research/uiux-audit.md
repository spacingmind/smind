# UI/UX audit: crash triage + polish assessment

Research doc, not a plan. Triggered by the maintainer's first real
browser dogfood session: a crash on an empty database, and a general
"this looks unpolished" reaction. Everything below is grounded in code
read directly in this repo or a cited web source. Layout/review-flow
architecture is **not** re-litigated here — `docs/research/dual-mode-ui.md`
already did that analysis in depth and its recommendations still hold;
this doc audits what exists today for robustness and visual/interaction
polish, and points at that doc rather than repeating it.

## 1. Crash catalog (P0)

### 1.1 The reported crash, confirmed and traced to its root

`app-sidebar.tsx:61` — `list.map(...)` on the result of
`client.call<Workspace[]>("workspace.list")`. `handleWorkspaceList`
(`internal/wsapi/handlers.go:151-155`) passes through
`wm.ListWorkspaces()` → `Store.ListWorkspaces()`
(`internal/store/workspaces.go:43-62`):

```go
var workspaces []Workspace
for rows.Next() { ... workspaces = append(workspaces, w) }
return workspaces, rows.Err()
```

Zero rows means `workspaces` is never assigned, so it stays a **nil Go
slice**, which `encoding/json` marshals as the JSON literal `null`, not
`[]`. `list.map` on `null` throws `TypeError: Cannot read properties of
null (reading 'map')` exactly as reported. Every fresh install with an
empty database hits this on load.

### 1.2 A second, deeper instance of the same bug — not fixed by 1.1 alone

`space.list` (`handleSpaceList` → `wm.ListSpaces` →
`Store.ListSpacesByWorkspace`, `internal/store/spaces.go:43-63`) and
`task.list` (`handleTaskList` → `wm.ListTasks` →
`Store.ListTasksByWorkspace`, `internal/store/tasks.go:46-66`) use the
**identical** `var x []T; for rows.Next() { x = append(...) }` pattern —
nil slice, `null` on the wire, for any workspace that currently has zero
spaces or zero tasks.

`app-sidebar.tsx:60-65` fetches both in parallel per workspace:

```ts
const [spaces, tasks] = await Promise.all([
  client.call<Space[]>("space.list", { workspaceId: ws.ID }),
  client.call<Task[]>("task.list", { workspaceId: ws.ID }),
]);
```

then at `app-sidebar.tsx:69`, `for (const task of tasks)` — a `for...of`
over `null` throws `TypeError: tasks is not iterable` — and at
`app-sidebar.tsx:81`, `spaces.map((sp) => ...)` throws the same `.map`
error as 1.1.

**Consequence: fixing only `workspace.list`'s null (the reported
symptom) does not fix the empty-database crash.** The very next
workspace a user creates — with no spaces and no tasks yet, which the
code's own comment at `app-sidebar.tsx:219-223` calls "today's
common/default case" — reproduces the identical crash one level down,
inside the same `useWorkspaceTree` effect. A fix needs to cover all
three call sites, ideally at the source.

### 1.3 Recommended fix shape

Two defensible layers, not mutually exclusive:

- **Server-side (preferred, closes it for every current and future
  client):** `internal/store`'s three `List*` functions should
  initialize `x := []T{}` (or equivalently
  `make([]T, 0)`) instead of `var x []T`, matching the idiom already
  used elsewhere in this same codebase — see 1.4.
- **Client-side (defense in depth):** `useWorkspaceTree`
  (`app-sidebar.tsx:47-101`) should coerce every list result with
  `?? []` before `.map`/`for...of`, the same guard already present at
  `diff-viewer-pane.tsx:61` (`setFiles(result.files ?? [])`).

Both are cheap, and the client-side guard is worth keeping regardless of
the server fix, since nothing in the TypeScript types
(`Workspace[]`, `Space[]`, `Task[]` in `lib/types.ts`) actually
guarantees the Go side never sends `null` for an array field — the type
is an assertion about the wire contract, not an enforced one.

### 1.4 Why this is inconsistent, not systemic — the codebase already knows the right pattern

The nil-slice bug is **not** how this codebase generally writes list
handlers:

- `internal/files.List` (`internal/files/files.go:89`):
  `entries := make([]Entry, 0, len(dirEntries))` — always a real,
  non-nil slice, `[]` on the wire for an empty directory. `file.list`
  cannot produce this crash.
- `internal/runs.Registry.List` (`internal/runs/registry.go:750`):
  `out := make([]RunSummary, 0, len(rs))` — same safe idiom.
  `run.list` cannot produce it either.
- `internal/terminal.Registry.List` (`internal/terminal/registry.go:717`):
  same `make(..., 0, ...)` idiom.

Only the three **SQL-backed** `internal/store` list functions
(workspaces, spaces, tasks) use `var x []T` with append-only population.
This is a narrow, mechanical fix (three functions, one idiom change
each), not a rewrite — worth calling out explicitly so it isn't
over-scoped into "audit all list endpoints" work when the actual gap is
three call sites plus one matching client-side guard.

### 1.5 Other null/undefined-safety candidates checked and cleared

Went through every nullable field in `lib/types.ts` and every list-style
RPC result against how the UI consumes it:

| Field / RPC | Nullable on wire? | UI handling | Verdict |
| --- | --- | --- | --- |
| `Task.SpaceID` | yes (`number \| null`) | `app-sidebar.tsx:70,223` branches on `=== null` explicitly | Safe |
| `Task.Branch` | yes | `task-detail.tsx:41` — `{task.Branch && ...}` | Safe |
| `Task.WorktreePath` | yes | Never read client-side; server-side `taskWorktreePath` (`internal/wsapi/files.go:20-29`) errors cleanly ("task %d has no worktree") instead of passing nil through, surfacing as the pane's normal `error` state | Safe |
| `Task.ArchivedAt`, `RunSummary.FinishedAt`, `TerminalSessionStatus.ClosedAt` | yes | Declared in `types.ts` but never read by any component (`grep` confirms zero usages) | Dead type surface, not a bug — see §6's archive-UI gap for why `ArchivedAt` in particular has no UI to read it |
| `TaskFilesResult.files` | could be nil (`workspace.TaskFiles` returns `taskChangedFiles`'s result on the error-free path, untested for nil) | `diff-viewer-pane.tsx:61` — `result.files ?? []` | Already defensive |
| `TaskDiffResult.diff`, `TaskFileDiffResult.diff` | no — Go `string` zero value is `""`, never `null` | `diff-viewer-pane.tsx:292-295` explicitly distinguishes `null` (still loading, client-local state) from `""` (server said "no changes") | Safe, and a good pattern |
| `ProviderListResult.providers` | no — `taskrunner.SupportedProviders()` (`internal/taskrunner/provider.go:45-52`) is a static 4-entry literal | `task-detail.tsx:217` still guards with `result.providers.length > 0` before swapping in the fetched list | Defensive beyond what's needed, harmless |
| `run.list`, `terminal.list` | no (see §1.4) | `use-run-timeline.ts:212` spreads/filters directly, no `?? []` | Only safe because the Go implementation happens to pre-allocate — see the structural point in §1.3 |

The general shape of the gap: **the newer panes (`diff-viewer-pane.tsx`,
`task-detail.tsx`) already default to defensive `?? []` / length-checks
before trusting a list result; the older `app-sidebar.tsx` (the first
pane built, per git history) does not.** This is a consistency lapse
that crept in over time, not a one-off mistake — worth a lint/review
convention going forward ("every `T[]` result gets `?? []` at the call
site, regardless of what the Go handler currently guarantees") rather
than just patching the three spots found today.

## 2. UI/UX audit by surface

### 2.1 Information hierarchy

- **App shell** (`App.tsx:117-178`): header is a single-line connection
  status string (`STATUS_LABEL`, `App.tsx:22-27`) plus a sidebar
  trigger — there is no app title/logo/workspace-context breadcrumb in
  the header itself (`sidebar` carries "smind" at
  `app-sidebar.tsx:160`, but that's hidden entirely once the sidebar is
  collapsed to icon mode, per `group-data-[collapsible=icon]:hidden` at
  the same line — collapsing the sidebar removes the *only* place the
  product's name appears anywhere on screen).
- **Sidebar tree** (`app-sidebar.tsx:225-264`): three-level nesting
  (workspace → space → task) uses identical `ChevronRight` +
  toggle-on-click affordance at every level (`app-sidebar.tsx:230`,
  `321`), which is consistent, but nothing visually distinguishes "this
  is a workspace row" from "this is a space row" beyond icon
  (`FolderGit2` vs `Layers`) and one level of indent — at a glance a
  deeply nested tree reads as a flat list of similar rows.
- **Task rows** (`app-sidebar.tsx:362-387`): status text
  (`statusOverrides.get(task.ID) ?? task.Status`) is the *only* signal
  for what state a task is in beyond the attention dot — a task that is
  simply "idle, nothing to review" and one that is "actively running"
  render with the same visual weight (both are plain uppercase 10px
  gray text), so a glance at the tree can't answer "what's actually
  happening right now" without reading every row's text.
- **Task detail pane header** (`task-detail.tsx:37-43`): title, status,
  branch — reasonable, minimal, appropriately terse.

### 2.2 Empty / loading / error states

Surveyed every pane's three-state handling directly:

| Pane | Loading | Empty | Error |
| --- | --- | --- | --- |
| Sidebar tree (`app-sidebar.tsx:169-173`) | Spinner + "Loading workspaces…" | "No workspaces yet." — **dead end, no CTA** (see §4.1) | Red icon + raw error string |
| Task detail / chat (`task-detail.tsx:51-56`) | "Loading runs…" | "No runs yet. Send a prompt to start one." — good, actionable | Red inline text |
| File explorer (`file-explorer-pane.tsx:68-76`) | Per-directory spinner row | "(empty)" per directory — fine, scoped correctly | Per-directory red row, doesn't block the rest of the tree |
| File editor (`file-editor-pane.tsx:293-296`) | "Loading…" centered | n/a (a file always has content, even if empty string) | Red text, editor doesn't render underneath it |
| Diff viewer (`diff-viewer-pane.tsx:170-175`) | "Loading diff…" | "No changes." — good | Red text (`diff-error`) |
| Terminal (`terminal-pane.tsx:345-373`) | "starting terminal…" status text | n/a | Red text, distinguishes `interrupted` (daemon restarted) from a generic error — a genuinely good honest-state pattern worth reusing elsewhere |
| Preview (`file-preview.tsx:107-117`) | n/a | "Nothing to preview yet" with icon | n/a |

Every pane *has* all three states — that part of the codebase is
disciplined. The gap is narrower than "missing states": it's (a) the
sidebar's empty state has no next action (§4.1), and (b) empty/error
strings are inconsistently punctuated and cased across panes ("No runs
yet.", "No changes.", "(empty)", "Nothing to preview yet" — three
different capitalization/punctuation conventions for the same
semantic "there's nothing here" message), which reads as unpolished in
exactly the way the maintainer flagged, even though functionally each
one is fine in isolation.

### 2.3 Feedback affordances (what happens on action)

- **Save** (`file-editor-pane.tsx:249-256`): button label flips to
  "Saving…" and disables — clear.
- **Stop run** (`task-detail.tsx:106-117`): button stays visible but
  disabled while `stopping`, with no spinner — a user clicking Stop
  sees the button gray out but has no progress indicator distinguishing
  "request in flight" from "stuck."
- **Commit** (`diff-viewer-pane.tsx:217-219`): button label is always
  "Commit (N staged)" — clicking it while `committing` is true doesn't
  change the label at all (contrast: Save's "Saving…" pattern isn't
  reused here), so a slow commit gives zero visual feedback beyond the
  button being disabled.
- **Stage/unstage checkbox** (`diff-viewer-pane.tsx:264-270`): no
  optimistic UI — `toggleStage` (`diff-viewer-pane.tsx:118-129`) waits
  for `task.stage` to resolve before flipping `file.staged`
  server-round-trip-first, so on a slow connection a checkbox click
  visibly lags before reflecting the new state.
- **Conflict banner** (`file-editor-pane.tsx:259-290`): Reload/Overwrite
  buttons have no in-flight state at all (no disabling, no label
  change) — a double-click during a slow Reload could fire two
  concurrent `file.read`s.
- **Terminal**: genuinely good — `endedStatus`
  (`terminal-pane.tsx:118`) distinguishes "interrupted" (daemon
  restart) from "closed" (explicit), rendered as distinct, honest
  copy (`terminal-pane.tsx:364-368`) rather than a generic error.

Pattern: **the two newest features (file-conflict banner, per-file
commit bar) are the ones missing loading/in-flight feedback on their
buttons** — the older Save/Stop patterns got it right first. This
reads as the polish gap being about incremental features shipping
without carrying forward an established convention, not about the
convention being unknown.

### 2.4 Density, spacing, and visual weight

- Consistent Tailwind spacing scale throughout (`px-3 py-2`, `px-4 py-3`
  pattern repeats across every pane's header/footer bars) — this part
  is actually disciplined, not sloppy.
- But every pane reinvents its own header bar rather than sharing one
  component: `task-detail.tsx:37`, `file-editor-pane.tsx:219`,
  `diff-viewer-pane.tsx:157`, `terminal-pane.tsx:344` each hand-roll a
  `flex items-center justify-between border-b px-{3,4} py-{2,3}` div
  with slightly different padding (`px-3 py-2` in file-editor and
  terminal, `px-4 py-3` in task-detail and diff-viewer) — no visible
  reason for the split, just drift. A single `PaneHeader` primitive
  would both fix the inconsistency and cut ~15 lines of repeated markup
  per pane.
- Buttons are a mix of the shared shadcn `Button` component
  (`task-detail.tsx`, `diff-viewer-pane.tsx`, `terminal-pane.tsx`) and
  raw hand-styled `<button>` elements with inlined Tailwind classes
  (`file-editor-pane.tsx:249-256, 272-287`, the Edit/Preview toggle at
  `226-247`) — two different button visual languages coexist in
  panes that sit one tab apart from each other in the same task.

### 2.5 Color and theme usage

`index.css:51-118` defines a complete shadcn-default light/dark pair
(oklch tokens, near-zero chroma — pure grayscale except `--destructive`
red and one stray blue, see below). Two concrete findings:

- **Dark mode is defined but unreachable.** `.dark` (`index.css:86-118`)
  exists and every shadcn primitive is wired to consume it via CSS
  variables, but nothing in the app ever adds a `dark` class to
  `<html>`/`<body>`, reads `prefers-color-scheme`, or exposes a
  toggle — confirmed by `grep -rn "\.dark\|classList\|prefers-color-scheme"`
  across `web/packages/ui/src` returning nothing outside `index.css`
  itself. The app is permanently light-mode today regardless of the
  user's OS theme, even though half the design tokens for a dark theme
  already exist unused.
- **One un-intentional-looking color anomaly:** every dark-mode token in
  `index.css:86-118` is achromatic (`oklch(x 0 0)`) *except*
  `--sidebar-primary: oklch(0.488 0.243 264.376)` (`index.css:112`) — a
  saturated blue, the only chromatic token in the entire palette,
  inside a file that is otherwise deliberately monochrome plus one red
  accent. This is very likely an artifact of the shadcn theme generator
  never being fully swept for this project (the equivalent light-mode
  token at `index.css:78`, `--sidebar-primary: oklch(0.205 0 0)`, is
  properly grayscale) rather than a deliberate accent choice — worth a
  maintainer confirmation either way (see §5, open question).
- Beyond the stray token, the palette is the shadcn scaffold verbatim —
  zero customization of radius, font pairing beyond swapping in Geist,
  or brand color. This is consistent with "looks unpolished/generic":
  it looks exactly like an un-themed shadcn starter, because it is one.

### 2.6 Keyboard accessibility

- File tree rows (`file-explorer-pane.tsx:151-167`) implement
  `role="button"`, `tabIndex={0}`, and Enter/Space activation
  explicitly — correctly done, and the *only* place in the app that
  does this by hand.
- Tab strip close buttons (`App.tsx:147-159`): the `×` close affordance
  is a bare `<span role="button">` with a click handler and no
  `tabIndex`/`onKeyDown` — unlike the file tree rows, this one is not
  keyboard-operable at all; a keyboard-only user cannot close a file
  tab.
- Everything built on shadcn/Radix primitives (`Tabs`, `Sidebar`,
  `Tooltip`) inherits Radix's own keyboard handling for free — arrow-key
  tab navigation, focus trapping, etc. work correctly there by
  construction.
- CodeMirror's own `Mod-s` save keybinding (`code-mirror-editor.tsx:58-66`)
  is a nice touch, but it's the *only* non-text-editing keyboard
  shortcut anywhere in the app — there's no keyboard way to switch
  tasks, switch tabs by number, or open the command palette that
  `dual-mode-ui.md`'s gap inventory already flags as missing
  (`docs/research/dual-mode-ui.md:209`).

### 2.7 Responsive behavior

`use-mobile.ts` defines a 768px breakpoint hook (`useIsMobile`), but
`grep` confirms its only consumer is shadcn's own `Sidebar` primitive
(`components/ui/sidebar.tsx:68`), which uses it to swap the sidebar into
an overlay `Sheet` below that width. **Nothing else in the app is
responsive**: `App.tsx`'s main content area is a single
`ResizablePanelGroup` with one `ResizablePanel` (`App.tsx:134-176`,
confirmed still true — see `dual-mode-ui.md:198`'s identical finding),
and the tab strip (`App.tsx:143-163`) has no wrapping/scrolling
behavior for narrow viewports or many open file tabs — it will simply
overflow the header row. This is not a phone-support gap specifically
(smind is presumably desktop-first); it's that a laptop window narrower
than ~900px with 4+ file tabs open already has nowhere for the tab strip
to go.

### 2.8 Consistency between panes

Already itemized above per-topic (headers §2.4, buttons §2.4, empty-state
copy §2.2, in-flight feedback §2.3). The unifying read: **each pane is
internally well-built** — good state machines, honest error handling,
sensible use of refs/sessions to guard stale async work (a pattern that
repeats correctly across `use-file-explorer.ts`, `use-run-timeline.ts`,
`terminal-pane.tsx`, all citing each other's `Session`
cancelled-flag idiom in their own comments) — but **panes were visibly
built independently over time** without a shared primitives layer for
headers, buttons, or empty-state copy. That is the concrete, code-level
version of "looks unpolished": not broken, not sloppy engineering, just
zero design-system discipline layered on top of otherwise solid
component engineering.

## 3. Benchmark: how 2026's agent IDEs present this surface

`dual-mode-ui.md`'s comparison table (Paseo, VS Code, Cursor 3,
Antigravity, Windsurf, Claude Code IDE ext., Continue, Aider, GitHub PR
review) still stands as the primary reference — re-read here and still
accurate against current code. What's changed since that doc's sources
(mostly Nov 2025) that's directly relevant to *this* audit's polish and
first-run questions:

- **VS Code's Agents Window went to Stable in 1.120 (May 13, 2026)** and
  by 1.136 (September 2026) ships **"Chat sessions: Organize related
  chats in a session hierarchy and quickly see which ones need your
  attention."** This is the same "attention rail" concept
  `dual-mode-ui.md` recommended (§ Recommended layout model) — now
  shipped and stable in the most directly comparable mainstream product,
  which is a stronger validation signal than the Nov 2025 preview
  sources it was based on. It reinforces (does not change)
  `dual-mode-ui.md`'s recommendation.
- **Google Antigravity 2.0 (shipped May 19, 2026) dropped the embedded
  IDE entirely** — the product is now agent-manager-only, no editor
  surface at all. This is a data point *against* over-investing in a
  dual editor/manager mode split: even the one mainstream product that
  shipped literal dual-mode navigation walked further toward
  manager-only, not toward a more elaborate mode switch. Consistent
  with `dual-mode-ui.md`'s existing recommendation to not build a mode
  switch.
- **Conductor.build** (macOS, git-worktree-per-agent, the closest
  product analog to smind's actual data model) ships **"Suggested Git
  Actions"** — a button in the UI that recommends the next concrete step
  (stage, commit, open PR) rather than presenting a bare action palette.
  This is directly applicable to smind's commit bar
  (`diff-viewer-pane.tsx:197-220`): today the bar is a static
  textarea + button with no guidance about *when* to use it or what a
  good message looks like; a "Suggested commit message" affordance
  (even a simple heuristic like the diffstat, not necessarily an LLM
  call) would track this pattern.
- No 2026 source surfaced a public first-run/empty-state pattern for
  Cursor, Antigravity, or VS Code's Agents Window specifically — these
  are all products where "add a folder"/"sign in" is itself the
  onboarding flow, and none of them are analogous to smind's
  workspace-must-already-exist gap (§4.1), because none of them
  require creating a *workspace database record* before a folder can be
  opened at all.

## 4. Redesign proposal

No rewrites. Every item below is a targeted, independently shippable
change against the existing 8.5k-line codebase, respecting ADR 0004
(per-task tabs) and `dual-mode-ui.md`'s unified-layout stance (no mode
switch).

### P0 — crash fixes (ship first, independently of everything else)

1. Fix the three nil-slice list functions in `internal/store`
   (`ListWorkspaces`, `ListSpacesByWorkspace`, `ListTasksByWorkspace`) to
   initialize `[]T{}` instead of `var x []T` (§1.3, §1.4).
2. Add `?? []` at `useWorkspaceTree`'s three call sites
   (`app-sidebar.tsx:59,63,64`) as defense in depth, matching the
   pattern already used at `diff-viewer-pane.tsx:61`.
3. Adopt a house convention (worth a one-line note in `AGENTS.md` or a
   review-checklist item): every `client.call<T[]>(...)` result gets
   `?? []` before use, regardless of what the current Go handler
   guarantees — this is what would have prevented both 1.1 and 1.2 from
   needing a second look.

### P1 — usability (the "looks unpolished" complaint, concretely)

4. **First-run experience for an empty database** (see §4.1 below) —
   turn the sidebar's "No workspaces yet." dead end into an actual path
   forward.
5. **Extract a shared `PaneHeader` primitive** (title + right-aligned
   action slot) and standardize on one padding scale (`px-3 py-2`, the
   more common of the two found) across `task-detail.tsx`,
   `file-editor-pane.tsx`, `diff-viewer-pane.tsx`, `terminal-pane.tsx`.
6. **Standardize empty/error copy**: one casing/punctuation convention
   ("No X yet." with a period, consistently) across all six panes
   audited in §2.2.
7. **Replace `file-editor-pane.tsx`'s hand-rolled buttons** (Save,
   Reload, Overwrite, Edit/Preview toggle) with the shared shadcn
   `Button` component already used everywhere else, closing the
   two-button-language split noted in §2.4.
8. **Add in-flight feedback** to Commit (label → "Committing…", matching
   Save's existing pattern), Stage checkboxes (optimistic toggle, revert
   on failure), and the conflict banner's Reload/Overwrite buttons
   (disable while in flight) — four small, mechanical changes following
   a pattern the codebase already uses correctly elsewhere (§2.3).
9. **Make the tab-close `×` keyboard-operable** (`App.tsx:147-159`) —
   `tabIndex={0}` + Enter/Space handler, mirroring the file-tree row
   pattern already written once in `file-explorer-pane.tsx:151-167`.

### P2 — delight / theme

10. **Resolve the dark-mode question** (see §5, open question 1) —
    either wire up a real light/dark toggle (the tokens already exist
    and are otherwise complete) or delete the unused `.dark` block if
    dark mode isn't planned; leaving a half-wired theme as dead CSS is
    itself a small tax on future changes.
11. **Fix or confirm the stray chromatic sidebar-primary token**
    (`index.css:112`, §2.5, open question 2).
12. **A `Suggested commit message` affordance** on the commit bar,
    inspired by Conductor's "Suggested Git Actions" (§3) — even a
    non-LLM heuristic (file count + dominant change kind) would be a
    concrete, cheap step toward "this looks like a professional tool"
    without any new architecture.
13. Consider a per-workspace/per-space visual distinction beyond icon +
    indent (§2.1) — e.g., a subtle background tint per nesting level —
    once real usage shows the flat-tree read from §2.1 is actually a
    problem in practice, not preemptively.

### 4.1 First-run experience: no workspaces → guide to create one

Traced explicitly: `grep -rn "workspace.create\|space.create\|task.create"`
across `web/packages/ui/src` returns **zero matches**. There is no way,
today, to create a workspace, space, or task from the web UI at all —
only from the CLI (`cmd/smind/workspace.go`, `cmd/smind/space.go`).
This means the sidebar's current empty state —
`"No workspaces yet."` (`app-sidebar.tsx:173`), plain text, no button, no
instructions — is not just an unpolished empty state, it is *correct*
given today's architecture: the web UI genuinely cannot do anything
about it.

Two honest options, not a recommendation to pick one without maintainer
input (this crosses into "does the web UI get a workspace.create RPC
call at all," which is a real scope decision):

- **Minimal (ships today, no new RPC wiring beyond what already
  exists):** Replace the dead-end text with copy that tells the user
  the actual next step — e.g. *"No workspaces yet. Run `smind workspace
  create <path>` to add one, then refresh."* This costs one string
  change and is honest about the current architecture.
- **Fuller (needs a small form + wiring to the already-existing
  `workspace.create` RPC, which the daemon supports —
  `internal/wsapi/handlers.go:136-149` — but no web caller uses yet):**
  A small inline form in the empty state (path + title fields, submit →
  `workspace.create`) turns first run into a self-contained flow. This
  is a real, if small, feature addition — worth scoping as its own
  follow-up plan rather than folding into the crash-fix pass, since it's
  additive functionality, not a bug fix.

Either way, shipping P0 (§4, item 1-2) without also touching this empty
state means a fresh install goes from "crashes" to "shows an honest but
still-dead-end message" — a real improvement, but the maintainer should
decide whether that's the intended stopping point for this pass or
whether 4.1's fuller option belongs in the same cycle.

## 5. Open questions for the maintainer

1. **Is dark mode planned?** The tokens are fully defined
   (`index.css:86-118`) but nothing switches them on (§2.5). This
   materially changes whether item P2.10 is "wire it up" or "delete the
   dead CSS."
2. **Is `--sidebar-primary`'s dark-mode blue
   (`oklch(0.488 0.243 264.376)`, `index.css:112`) intentional?** It's
   the only chromatic token in an otherwise fully grayscale-plus-red
   palette, sitting on a token (`--sidebar-primary`) that shadcn's
   `Sidebar` component would use as an active/selected-item accent if
   dark mode were ever switched on. Could be a deliberate accent choice
   that just hasn't been carried into light mode yet, or a leftover
   from an unswept theme generator default — worth a direct answer
   either way before P2.11 touches it.
3. **Does workspace/space/task creation belong in the web UI at all, or
   does the web UI stay strictly a viewer/actor on daemon state that the
   CLI provisions?** (§4.1) This is the same category of "materially
   affects the product surface" question `dual-mode-ui.md` already
   raised for tabs/commit/git-status — worth deciding once, not
   re-litigating per empty-state fix.
4. **Should archiving a task get a web UI affordance at all?** Traced:
   `task.archive` exists as an RPC (`internal/wsapi/handlers.go:245-255`)
   but has zero callers anywhere in `web/packages/ui/src` — there is
   currently no way to archive a task from the browser. Combined with
   `dual-mode-ui.md`'s existing flag that archive force-removes the
   worktree with no safety commit
   (`docs/research/dual-mode-ui.md:133-135`, open question 5 there),
   this is a case where "no UI for it yet" is arguably safer than
   shipping an easy-to-click archive button before that safety-commit
   question is resolved — flagging so it isn't accidentally added as a
   quick win during the P1 polish pass without that context.

## Sources

Local code (read directly):

- `web/packages/ui/src/App.tsx`, `main.tsx`, `index.css`
- `web/packages/ui/src/components/{app-sidebar,task-detail,file-explorer-pane,file-editor-pane,diff-viewer-pane,terminal-pane,file-preview,code-mirror-editor,tab-registry}.tsx`
- `web/packages/ui/src/components/ui/{button,resizable,sidebar}.tsx`
- `web/packages/ui/src/hooks/{use-file-explorer,use-run-timeline,use-task-attention,use-mobile}.ts`
- `web/packages/ui/src/lib/types.ts`
- `internal/wsapi/{handlers.go,files.go}`
- `internal/store/{workspaces.go,spaces.go,tasks.go}`
- `internal/files/files.go`, `internal/runs/registry.go`, `internal/terminal/registry.go`
- `internal/taskrunner/provider.go`
- `cmd/smind/{workspace.go,space.go}` (confirms CLI-only workspace/space creation)
- `docs/decisions/0004-per-task-editor-tabs.md`
- `docs/research/dual-mode-ui.md` (prior research; layout/review-flow analysis reused by reference, not repeated)

Web:

- [Hands On with the New Agents Window in VS Code 1.120 — Visual Studio Magazine](https://visualstudiomagazine.com/articles/2026/05/13/hands-on-with-the-new-agents-window-in-vs-code-1,-d-,120.aspx)
- [Visual Studio Code 1.136 release notes](https://code.visualstudio.com/updates/v1_136)
- [Your Home for Multi-Agent Development — VS Code blog](https://code.visualstudio.com/blogs/2026/02/05/multi-agent-development)
- [Google Antigravity: Complete Guide to the Agent IDE](https://www.aibuilderclub.com/blog/google-antigravity-complete-guide)
- [Antigravity 2.0 vs Claude Code vs Cursor 2026 — MCP.Directory](https://mcp.directory/blog/antigravity-2-vs-claude-code-vs-cursor-2026)
- [Conductor.build: Run a Team of Parallel AI Coding Agents on Your Mac — CodePick](https://codepick.dev/en/guides/conductor-build-intro/)
- [Conductor vs Intent (2026): macOS Agent Orchestrators — Augment Code](https://www.augmentcode.com/tools/intent-vs-conductor-macos-agent-orchestrators)
