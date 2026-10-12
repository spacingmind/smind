# Sidebar real-data polish (pre-v0.11)

Size: M (3) — scope 1, PRs 1, risk 0, decisions 0, verify 1 (screenshots).

## Goal

The real macOS app with a real DB (30+ tasks, long titles, all "29d" old)
showed four sidebar problems the fixture-sized UI never hit. Fix them in one
PR.

## Findings and decisions

1. **Header scrolled away.** `SidebarContent` (`overflow-auto`) was the
   scroller, so the "Workspaces · tree/search/+" label scrolled with the list;
   the inner `ScrollArea` never got a bounded height. Fix: `SidebarContent`
   becomes `overflow-hidden`, the group/content chain is `min-h-0 flex-1`, and
   the Radix `ScrollArea` is the only scroller. Workspace headers are
   `sticky top-0`, space/ungrouped headers `sticky top-8` (ZCode's sticky group
   headers); each is bounded by its own `li`.
2. **Indentation ate the title.** Space/ungrouped content carried a second
   ~21px guide line at ≥14rem, every root reserved a 12px chevron gutter, rows
   had a 4px inset, 12px per tree level, 12px status slot. Now: no space-level
   guide at any width (the workspace guide already frames it), gutter only when
   a root in the same list has children, inset 0, 10px per level, 8px slot.
   Depth-0 title at 256px: 88px → 47px from the sidebar edge (title width
   121 → 162px; at 192px 108 → 128px). The 44px target was missed by 3px on
   purpose: going lower would put the dot 3px from the highlight edge.
3. **Dot noise.** The green dot is the `finished` attention reason
   (`useTaskAttention`): a terminal run not in the in-memory `seenRef`. That set
   is not persisted, so every reload resurrected every historic run as
   unseen. (The unread set is already edge-triggered and persisted, so it did
   not flood.) Rule: `finished` only for runs that finished in the last 24h
   (`FINISHED_ATTENTION_WINDOW_MS`; a run with no recorded finish time ages from
   its start); `error` and `permission` are unbounded; latest-run-errored red
   dots are unchanged.
4. **Scrollbar.** The classic always-visible bar was the native scroller
   (`SidebarContent`) styled by the global `::-webkit-scrollbar` rule. The
   scroller is now Radix's overlay scrollbar (hover/scroll-only, absolutely
   positioned, so it cannot shift rows and `scrollbar-gutter` is not needed).

## Acceptance criteria

1. With the list scrolled, the "Workspaces" label row stays at the top of the
   sidebar content; only the tree scrolls (light + dark, 256px + 192px).
2. Workspace and space headers pin while their tasks scroll and are pushed out
   by the next header.
3. At 256px a depth-0 title inside a space starts ≤ 48px from the edge; no
   gutter in a list with no parents; a list with a parent still shows its
   chevron and aligns siblings.
4. A run finished > 24h ago yields no `finished` attention; one finished < 24h
   ago, and a live finish, still do; an old error still yields `error`.
5. The sidebar scrollbar is invisible until hover/scroll.
6. `task test`, `task lint`, web typecheck pass.

## Test scenarios

- `task-attention-finished-recent-run-dots`
- `task-attention-finished-old-run-quiet`
- `task-attention-finished-unrecorded-finish-ages-from-start`
- `task-attention-error-old-run-still-attention`
- `task-attention-live-finish-dots`
- `app-sidebar-rows.test.tsx`: "indentation is per-row padding by depth…", "a flat
  list reserves no chevron gutter; a list with a parent does", "flat views
  (pinned, search) render root rows with no gutter", "task-tree nested lists
  add no indent…".
- `app-sidebar.test.tsx`: "lists without parents reserve no gutter…".

## Validation

Seeded sandbox daemon (35 tasks, 2 workspaces, 3 spaces, titles 40–60 chars,
runs finished 5–29d ago, plus one finished 1h ago and one running), isolated
`SMIND_HOME` and port. Screenshots under `tmp/before`, `tmp/after` (local only):

| | before | after |
|---|---|---|
| Workspaces label top (scrolled to middle) | −354px (scrolled away) | 56px (pinned) |
| depth-0 title x @256 / @192 | 88 / 67 | 47 / 47 |
| title width @256 / @192 | 121 / 108 | 162 / 128 |
| dots on 35 old rows | 35 | 4 (2 old errors + 1h-old finish + running) |

`bun run test` 2161 passed, `tsc -b` clean, `task lint` clean, `task test` Go
green except the known-flaky `TestServer_TerminalResize_ReachesPTY` (passes on
rerun, ×3). Not verified: real WebKit (no WebKit build available here; the
Radix scrollbar is plain DOM, so it renders the same, but a capture of the Tauri
app is the confirmation).
