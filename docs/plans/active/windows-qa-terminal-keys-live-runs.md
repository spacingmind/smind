# Windows QA 2026-10-11: terminal keys, live runs, workspace context menu, CLI approve

Source: a Windows QA pass of smind desktop 0.10.0 (2026-10-11). Each item was
re-checked against `origin/develop` (v0.10.0-21) before specifying; findings are
in Decisions. Related: `desktop-native-feel.md` (D3 shortcuts),
`orchestration-parity-paseo.md` (the run/queue work behind item 2).

## Acceptance Criteria

### K — Terminal pane keeps its shell keys (QA item 12, High)

- **K1.** With focus in the `terminal` scope on a non-Mac platform, a binding
  whose *resolved* combo is a shell control key (exactly Ctrl, no Shift/Alt/Meta,
  key A–Z or `[` (ESC), `\` (SIGQUIT), `]`; `Mod` resolves to Ctrl off-Mac) does
  **not** fire. That covers the shipped rows (`Mod+K` palette, `Mod+B` sidebar,
  `Mod+W` close tab, `Mod+L` composer, `Mod+P` quick open, `Mod+F` find, desktop
  `Mod+T` new tab, `Mod+[`/`Mod+]` previous/next task, `Mod+\` split right) and any
  user rebinding to such a combo, and each step of a chord. The keydown is neither
  `preventDefault`ed nor `stopPropagation`ed, so xterm receives it.
- **K2.** On macOS nothing changes: `Cmd+letter` bindings still fire in the
  terminal scope.
- **K3.** Nothing changes in non-terminal scopes (`other`, `editable`, `modal`) on
  any platform: `Ctrl+K`/`Ctrl+B`/`Ctrl+W`/`Ctrl+[` etc. still fire exactly as before.
  Ctrl+Shift+anything, Alt-combos and non-control keys (`Mod+,`, `Mod+Digit`) are
  not shell keys for this purpose and keep firing in the terminal.
- **K4.** Escape hatches: non-Mac-only rows (new `when.mac: false` gate) that fire
  from every scope including the terminal and never resolve to a Ctrl+letter:

  | Action | Escape-hatch combo |
  |---|---|
  | `palette.open` | `Mod+Shift+P` |
  | `tab.close` | `Alt+Shift+W` (Paseo's web close-tab combo) |
  | `sidebar.toggle` | `Mod+Shift+B` |
  | `pane.find` | `Mod+Shift+F` |
  | `composer.focus` | `Mod+Shift+L` |
  | `quick-open.open` | `Mod+Shift+O` |
  | `tab.new` (desktop only) | `Mod+Shift+T` |
  | `task.prev` | `Mod+Shift+[` |
  | `task.next` | `Mod+Shift+]` |
  | `pane.split.right` | `Alt+Shift+\` (`Mod+Shift+\` is already `pane.split.down`) |

  No hatch combo collides with an existing default (checked against the table).
- **K5.** The shortcuts dialog lists the hatch rows on non-Mac only (they are
  absent on Mac), each with a note that it is the one to use while a terminal has
  focus, because Ctrl+<letter> goes to the shell there. The existing rows' notes
  are unchanged.
- **K6.** Rebinding/conflict logic still works with the extra rows (a hatch row can
  be rebound or unassigned independently; `conflictingBindings` sees them).

### L — Runs started by any client appear live in the open chat (QA item 2, Med)

- **L1.** A `run.status` for the open chat with a run id the UI did not start
  (CLI, another client, a queue delivery) adds exactly one entry and, while the run
  is running, exactly one `run.attach`. (Already true since #259 — locked in by test.)
- **L2.** A `permission.pending` for the open chat whose run is unknown to the
  timeline is also an adoption trigger (the run may be adopted before any
  `run.status` was seen), and the run's pending permission card renders without a
  reload: via the attach backfill, which replays the unresolved request.
- **L3.** A `permission.pending` for a run the timeline *already* knows is ignored
  by the timeline hook (the attach stream owns permission state; a second source
  could resurrect a card the stream already resolved).
- **L4.** No duplicates: any mix/order/burst of `run.status` and `permission.pending`
  for the same run, and a `run.start` response racing them, yields one entry and
  one `run.attach`.
- **L5.** Events for another chat or task are ignored.

### W — Workspace row context menu (QA item 3, Low)

- **W1.** Right-clicking a workspace row in the sidebar opens a context menu with
  the same items, in the same order, as the row's "…" menu: Add task, Add space,
  Reveal/Open in <editor> (desktop local daemon only), Worktree scripts, Delete
  workspace. Single source of truth for the item list (no second copy).
- **W2.** Left-click still toggles expand/collapse; the "…" button still works; right
  click does not toggle.
- **W3.** The menu matches the file tree's `ContextMenu` styling in light and dark.

### C — CLI `smind task approve` (QA item 4, Low)

- **C1.** `smind task approve <runId> <requestId> <optionId>` responds to the pending
  request with that exact option (today: 3 args hit the `len(args) > 2` usage error,
  while the usage text itself advertises `[requestId] [optionId]`).
- **C2.** `approve <runId>` and `approve <runId> <requestId>` keep today's behaviour
  (first allow option). An `optionId` not offered by the request is an error that
  lists the valid ids; it is never sent.
- **C3.** `deny` keeps `<runId> [requestId]` and rejects a third argument with usage.
  Usage text for approve stays truthful.

## Test Scenarios

Names are the exact `it(...)` / `Test...` titles.

**Web, `keyboard/shortcuts.test.ts` (matcher level) and `keyboard-provider.test.tsx` (real keydown)**
- `terminal-scope-ctrl-letters-pass-through` — non-Mac, terminal scope: Ctrl+K, Ctrl+B,
  Ctrl+W, Ctrl+L, Ctrl+P, Ctrl+F resolve to no match; via the provider, the event is not
  default-prevented and no handler runs. Also a rebound `Ctrl+J` binding and the 2nd
  step of a chord.
- `terminal-scope-mac-cmd-still-fires` — Mac, terminal scope: Cmd+K/B/W match.
- `terminal-scope-ctrl-bracket-passes-through` — non-Mac terminal: Ctrl+[, Ctrl+],
  Ctrl+\\ resolve to no match (`task.prev`/`task.next`/`pane.split.right`), the provider
  leaves the event un-prevented; on Mac Cmd+[ / Cmd+] still fire in the terminal; the
  hatches `Ctrl+Shift+[`, `Ctrl+Shift+]`, `Alt+Shift+\\` fire there.
- `terminal-scope-ctrl-shift-and-non-letter-still-fire` — non-Mac terminal: Ctrl+Shift+W
  (pane close), Ctrl+Shift+\\ (split down) and Ctrl+, still match.
- `non-terminal-scopes-ctrl-letters-unchanged` — non-Mac `other`/`editable`: Ctrl+K,
  Ctrl+B, Ctrl+W match as before.
- `terminal-escape-hatch-fires-in-terminal` — non-Mac terminal: `Ctrl+Shift+P` →
  `palette.open`, `Alt+Shift+W` → `tab.close`; one test for each K4 row via table.
- `escape-hatch-rows-hidden-on-mac` — `platformBindings`/help sections on Mac exclude the
  hatch rows; on non-Mac include them (and `tab-new` hatch only on desktop).
- `no-duplicate-default-combos` — every pair of platform-visible rows with the same
  resolved combo is a bug (guards K4's collision claim).
- `shortcuts-dialog-lists-terminal-hatch-rows` (`shortcuts-dialog.test.tsx`) — non-Mac
  render shows "Ctrl+Shift+P" with the terminal note.

**Web, `hooks/use-run-timeline.test.tsx` (fake `WsClientLike` + fake `DaemonEvents`)**
- `adopts a run started outside the UI on run.status and attaches once`
- `adopts a run on permission.pending when no run.status was seen, and shows its pending permission`
- `permission.pending for a known run does not touch its pendingPermission`
- `run.status + permission.pending burst for one run yields one entry and one run.attach`
- `run.start response racing an adoption does not duplicate`
- `events for another chat are ignored`

**Web, `components/app-sidebar.test.tsx`**
- `workspace row context menu mirrors the actions menu` (items equal, same order, desktop
  local path actions included/excluded as in the "…" menu)
- `right click on a workspace row does not toggle expansion`
- `workspace context menu Delete workspace opens the delete flow`

**Go, `cmd/smind`**
- `TestTaskApproveExplicitOption` — 3 args responds with that option id.
- `TestTaskApproveUnknownOptionListsValid` — error, no `run.respondPermission` call.
- `TestTaskApproveDefaults` — 1 and 2 args as before.
- `TestTaskDenyRejectsThirdArg`.

**Manual / screenshot (recorded in Validation)**
- Light + dark screenshots against a sandbox daemon: workspace row context menu open;
  shortcuts dialog on a non-Mac user agent showing the hatch rows.
- Windows re-test list is in the PR description (item 12 especially).

## Decisions

- **Rule, not a per-row flag (K1).** One predicate in the matcher (`terminal` scope,
  non-Mac, resolved combo is bare Ctrl+letter → skip) rather than flipping every row off
  `global`. A rebound combo is covered for free, and the table stays the single place a
  binding is declared. Paseo does it per row (`when: { mac: false, terminal: false }`) and
  gives no hatch for most; we add hatches because the terminal is a first-class pane here.
- **Letters plus `[` `\` `]` (supervisor decision, 2026-10-11).** Ctrl+`[` is ESC (vim
  users), Ctrl+`\` is SIGQUIT, Ctrl+`]` is GS; the shell owns them like the letters, so
  K1 covers them and `task.prev`/`task.next`/`pane.split.right` get Shift hatches. Ctrl+digit
  and Ctrl+`,`/`.` are not control bytes and keep firing.
- **Hatch combos.** `Ctrl+Shift+W` as suggested is already `pane.close`, so close-tab uses
  `Alt+Shift+W` (Paseo's own web close-tab combo, and it matches the existing
  `Alt+Shift+T/[/]` tab family). The others are the Shift variant of the default, which is
  not a readline key. Hatches fire in every scope (not just terminal) so the dialog has one
  honest list.
- **Platform gate.** `BindingWhen.mac?: false` and an `isMac` argument on
  `platformBindings`, mirroring Paseo's `when.mac`. Not an architecture change: the table
  shape is unchanged.
- **Item 2 status on develop.** `run.status` adoption (#259, post-0.10.0, not in the QA'd
  build) already covers the "run appears" half; the QA build predates it. Remaining gap
  closed here: `permission.pending` as an adoption trigger. It is adoption-only on purpose
  (L3): attach backfill replays the unresolved request, and bus-vs-stream ordering is not
  guaranteed, so applying it to a known run could resurrect a resolved card.
- **Item 4 on develop.** Still broken (`len(args) > 2` in `cmdTaskDecide`); #273 added
  `deny` only. Fixed by making `optionId` a real third argument for approve only.

## Progress

- [x] Spec written; items checked against origin/develop (2026-10-11)
- [x] K — keys (`isShellControlKey` matcher rule, `when.mac` gate, 10 hatch rows, rows ordered next to their base row)
- [x] L — live runs (`permission.pending` adoption trigger; #259 already covered `run.status`)
- [x] W — workspace row context menu (single `items` list feeds "..." and the context menu)
- [x] C — CLI `task approve <run> <req> <opt>`
- [x] Adversarial review, light/dark screenshots, real-browser terminal check
- [ ] Windows re-test by the user (see PR description); then `git mv` to `completed/`

## Validation

Commands: `task test` (Go + web), `task lint`, `cd web && bunx tsc -b packages/ui`, all green on the PR branch
(web: 126 files / 1570 tests). One `internal/wsapi` TempDir-cleanup flake
(`TestServer_RunConfigOptions_GLM_RealSelectRoundTrip`, known CI flake, package untouched) passed 3/3 on rerun.
Earlier web failures reported by coders only occurred while four agents ran vitest concurrently; none on a quiet machine.

| AC | How it was confirmed |
|---|---|
| K1 | `terminal-scope-ctrl-letters-pass-through` and `terminal-scope-ctrl-bracket-passes-through` (matcher + keyboard-provider keydown, event not default-prevented). Real Chrome, Win32 user agent, focus in the xterm of a sandbox daemon: `Ctrl+K` opened nothing, `Ctrl+W` kept both tabs. |
| K2 | `terminal-scope-mac-cmd-still-fires` |
| K3 | `non-terminal-scopes-ctrl-letters-unchanged`, `terminal-scope-ctrl-shift-and-non-letter-still-fire`; full existing keyboard suites unchanged |
| K4 | `terminal-escape-hatch-fires-in-terminal` (table-driven over all 10 rows), `no-duplicate-default-combos`; real Chrome: `Ctrl+Shift+P` opened the palette and `Alt+Shift+W` closed the tab from the terminal |
| K5 | `escape-hatch-rows-hidden-on-mac`, `shortcuts-dialog-lists-terminal-hatch-rows`; screenshots `shortcuts-dialog-{light,dark}` (Settings → Shortcuts on a Win32 UA, rows labelled "… (in a terminal)" next to their base row) |
| K6 | existing rebind/conflict/override suites green with the extra rows; shortcut-help-search matches "terminal" |
| L1–L5 | six named tests in `use-run-timeline.test.tsx`; only the `permission.pending` adoption test failed before the fix |
| W1–W3 | `workspace row context menu mirrors the actions menu (with/without localPathActions)`, `right click on a workspace row does not toggle expansion`, `workspace context menu Delete workspace opens the delete flow`; real Chrome: left click collapses, right click does not toggle; screenshots `workspace-context-menu-{light,dark}` |
| C1–C3 | `TestTaskApproveExplicitOption`, `TestTaskApproveUnknownOptionListsValid`, `TestTaskApproveDefaults`, `TestTaskDenyRejectsThirdArg` |

Not verifiable here: a real Windows webview (`Win32` was spoofed on macOS Chrome); the Reveal/Open-in-editor items
(desktop local daemon only; covered by the mocked `useLocalPathActions` test); a CLI-started run appearing live in a
real UI (covered by the fake-client tests, not run end to end).
