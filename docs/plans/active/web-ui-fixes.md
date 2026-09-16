# Web UI fixes: dropdown theme, resizable-panel drag, sidebar text truncation

Three concrete bugs found live-testing `web/packages/ui` (real daemon,
headless-Chromium screenshots at desktop/tablet/mobile widths) after the
user flagged the UI as "sai thiết kế, không responsive" — not a request
for a visual redesign pass (see `docs/research/ui-ux-polish-2026.md` for
that, separate/later concern), these are functional/behavioral defects.

**Port from `refs/paseo` where it has already solved the same problem**
(per user, 2026-09-17: smind's own from-scratch attempts at UI pieces
like these keep shipping small bugs) — adapt the *behavior*, not
verbatim code: paseo's UI is React Native + react-native-unistyles +
react-native-gesture-handler (`packages/app`), smind's is plain web
React + Tailwind v4 + shadcn (`web/packages/ui`) — there is no file to
copy-paste, only interaction logic/structure to port into smind's DOM-
based stack.

## Acceptance Criteria

- **Provider/approval-policy dropdowns use the app's actual design
  system.** `web/packages/ui/src/components/composer/composer.tsx`
  currently renders two raw native `<select>`/`<option>` elements (lines
  ~257, ~276) with a hand-tuned Tailwind class approximating the shadcn
  trigger's closed-state look (`SELECT_CLASS`) — but the *open* dropdown
  list is 100% OS-native chrome (no rounded corners, no dark-mode
  awareness, doesn't match the app's `bg-popover`/`text-popover-foreground`
  tokens used everywhere else). Replace with the already-installed shadcn
  `Select`/`SelectTrigger`/`SelectContent`/`SelectItem`/`SelectValue`
  (`web/packages/ui/src/components/ui/select.tsx`, Radix-based). Preserve:
  the visible `<label htmlFor>` association (a past deliberate decision,
  see composer.test.tsx's "Item 10: not unlabelled native selects"
  comment — Radix's `SelectTrigger` accepts a real `id` prop so this
  still works), the `disabled` state while `inactive`, the
  `title={APPROVAL_POLICY_HELP}` tooltip on the policy select, and the
  44px/7-unit responsive height split (`SELECT_CLASS`'s `h-11 ...
  md:h-7 ...`, Item 21's touch-target rule) on the trigger.
- **Resizable panel drag actually works and feels right.** Both
  `ResizablePanelGroup` uses in `web/packages/ui/src/App.tsx`
  (`sidebar-resize-handle`, `side-pane-resize-handle`, both via
  `web/packages/ui/src/components/ui/resizable.tsx` wrapping
  `react-resizable-panels` v4's `Group`/`Panel`/`Separator`) need to
  actually resize smoothly when dragged — verify with a real
  mouse-down/move/up sequence (headless Chromium or equivalent), not
  just that the code compiles. If `react-resizable-panels` v4's own
  drag handling is the problem (confirm before assuming), port the
  *interaction model* paseo already ships in
  `refs/paseo/packages/app/src/components/resize-handle.tsx` and
  `sidebar-resize-handle.tsx`: pointer capture on drag start
  (`setPointerCapture`/`releasePointerCapture`) so a fast drag never
  "loses" the handle even if the pointer leaves its bounding box, a hit
  area meaningfully wider than the visible 1px separator line (paseo
  uses a 10px pointer hit area / 24-88px touch hit area, vs the current
  `after:w-1` ~4px), delta computed as a ratio of container size (not
  raw pixels) so resizing is stable across container size changes, and
  a hover-delay-then-highlight affordance so the handle doesn't flicker
  on incidental mouse-over. Whether this means patching smind's
  `resizable.tsx` wrapper or replacing `react-resizable-panels` with a
  custom pointer-event hook modeled on paseo's is an implementation
  decision — the acceptance bar is "drag resizes smoothly and reliably",
  not a specific library choice.
- **Sidebar task/workspace title text truncates correctly, not garbled.**
  Live screenshots (`/tmp/smind-ui-detail-desktop.png`,
  `-tablet.png` from this session, not committed to the repo) show task
  titles in the sidebar rendering as trailing fragments ("e verify
  commands)", "esizable layout", "x Item 7a") instead of the full title
  truncated with a leading ellipsis or clean trailing cutoff — reproduced
  at both 1440px and 768px widths, so it's a layout bug, not a viewport-
  specific one. `web/packages/ui/src/components/app-sidebar.tsx` already
  applies `min-w-0 truncate` on the title/branch `<span>`s (lines ~752,
  890, 947, 1029) — the bug is an ancestor flex container somewhere in
  the chain not constraining width (a classic Tailwind flexbox
  truncation gotcha: every flex ancestor needs `min-w-0` too, or the
  child never actually gets clipped). Find and fix the actual missing
  constraint; add a regression test asserting a long title renders
  fully truncated (e.g. assert the rendered text is a prefix of the
  full title, or check `scrollWidth <= clientWidth` after render) so
  this doesn't silently regress again.
- Out of scope: the broader visual-design refresh from
  `docs/research/ui-ux-polish-2026.md` (design tokens, typography,
  variant naming, motion) — that's a separate, larger, later pass with
  its own plan if pursued. This plan is bug fixes only.

## Test Scenarios

- Vitest: composer's provider/approval-policy selects render via the
  shadcn `Select` primitive (assert on `role="combobox"` / the trigger
  element, not `role="option"` before the popover opens — Radix renders
  options in a portal only once open); `screen.getByLabelText("Provider")`
  still resolves to the trigger (label association preserved); opening
  the select and reading its items reproduces the existing
  "renders the provider dropdown from provider.list" assertions
  (`composer.test.tsx` around line 224) adapted to the open-then-query
  interaction Radix requires; the 44px/dense touch-target class
  assertion (line ~248) still passes against the trigger element.
- Vitest or Playwright (whichever this repo already has wired for
  interaction tests — check before choosing): a simulated pointer-down
  on a resize handle, move, and pointer-up changes the adjacent panel's
  rendered width/flex-basis; a fast/large pointer move (simulating a
  quick drag) still resizes correctly, not just a slow small move —
  this is the scenario most likely to expose the "loses the handle"
  failure mode paseo's pointer-capture pattern exists to prevent.
- Vitest: a task with a title long enough to overflow the sidebar's
  available width renders truncated (not garbled/fragmented) at both a
  desktop and a narrow/mobile viewport width.
- Manual: live smoke test via a real running daemon + headless browser
  screenshot at 1440/1024/768/390px widths (the four widths already
  used this session) confirming visually: (a) both dropdowns open with
  shadcn-styled popovers matching the rest of the app's theme in both
  light and dark mode, (b) dragging each resize handle produces a smooth,
  immediate, reliable resize with no dead zone or handle-loss, (c) every
  sidebar row's title/branch text is a clean truncation, never a
  fragment.

## Decisions

No ADR needed — these are bug fixes to existing, already-decided UI
architecture (shadcn/Tailwind component system, `react-resizable-panels`
or a straightforward pointer-event replacement for it), not new
architectural choices. If the resizable-panel fix ends up requiring
dropping `react-resizable-panels` entirely in favor of a custom hook,
that's a dependency removal worth a line in the PR description, not a
formal ADR (it doesn't change any accepted architecture, just an
implementation detail of one component).

## Progress

- [x] Composer dropdowns ported to shadcn `Select`
- [x] Composer tests updated for the new interaction model
- [ ] Resizable-panel drag fixed (root cause confirmed first, then
      patched or replaced per Acceptance Criteria) — **not started**, see
      Validation: the plan requires confirming the root cause with a real
      interaction test *first*, and no test runner can be started in this
      environment
- [ ] Resizable-panel interaction test added
- [x] Sidebar text-truncation root cause found and fixed
- [x] Sidebar truncation regression test added
- [ ] Live smoke test (four viewport widths) confirming all three —
      **blocked**, no daemon/dev server/browser can be started here
- [ ] Verification — **blocked**, `task test` / `task lint` cannot run here

## Validation

**Written, not yet run.** The implementing session had no usable
toolchain: `web/node_modules` was absent in the worktree and `bun`,
`npm`, `npx`, `task`, `go`, and `gh` were all refused by the sandbox
(only read-only shell builtins and `git` were permitted). So nothing
below has been executed, and the live smoke test at 1440/1024/768/390px
has not happened. Re-run `task test` + `task lint` and the manual smoke
test before this is considered done.

### Composer dropdowns — done (unrun)

`composer.tsx` now renders `Select`/`SelectTrigger`/`SelectContent`/
`SelectItem`/`SelectValue`. `SELECT_CLASS` shrank to
`SELECT_TRIGGER_CLASS` (sizing only) because the trigger supplies the
border/background/focus-ring/disabled styling it used to approximate by
hand; `cn`'s tailwind-merge drops the trigger's conflicting `h-8`,
`px-2.5` and `text-base md:text-sm` in favour of the Item 21 rule.
Acceptance criteria are covered by tests: label association
(`getByLabelText("Provider")` resolves the trigger — `<button>` is a
labelable element, so `htmlFor`/`id` still works, the same pattern
`accounts-dialog.test.tsx` already relies on), `disabled` while
inactive, the `title` tooltip on the policy trigger, and the
44px/`md:h-7` split on both triggers. The provider-list test now opens
the select before reading `role="option"`, and a new test drives
open → pick GLM → send to prove the selection reaches `onSubmit`.

### Sidebar truncation — root cause found, fixed (unrun)

Not a flex ancestor after all. `AppSidebar` wraps the task tree in
`<ScrollArea>`, and Radix's `ScrollArea.Viewport` always wraps *its*
children in a div styled inline `{ minWidth: "100%", display: "table" }`.
A table box is shrink-to-fit, so that wrapper takes the width of the
widest row instead of the viewport's: `min-w-0 truncate` on the title
span never had a width to clip against, rows overflowed the viewport
horizontally, and titles rendered as fragments. Fixed in
`components/ui/scroll-area.tsx` by pinning the wrapper back to `block`
with an `!important` rule (Radix's is an inline style, so nothing less
displaces it) — which also fixes `folder-picker-dialog.tsx`'s entry
names, same `truncate`, same defeated ancestor. The task row's own flex
wrapper also gained `min-w-0` so the chain is explicit.

The regression test is structural, not dimensional: jsdom has no layout
and no Tailwind, so `scrollWidth`/`getComputedStyle` are uniformly
0/empty and the plan's suggested `scrollWidth <= clientWidth` assertion
cannot work there. It instead asserts the override is present on the
viewport and that every flex box between the title and the viewport
carries `min-w-0` (or `overflow-hidden`). A real measurement belongs in
the manual smoke test.

### Resizable panel drag — not attempted

The plan (and the task) require confirming the root cause with a real
pointer-down/move/up interaction test *before* deciding between
patching `resizable.tsx` and replacing `react-resizable-panels` with a
paseo-style pointer-capture implementation. No test can be run here, and
`refs/paseo` is a symlink outside the worktree that the sandbox refuses
to read, so the paseo sources named in the Acceptance Criteria were not
readable either. Guessing at a fix for an interaction bug and shipping
it unverified is exactly the failure mode this plan exists to avoid, so
this item was left untouched.

Two concrete suspects for whoever picks it up, both smind-side and both
cheap to check first:

1. `App.tsx` feeds live state back into `defaultSize`
   (`defaultSize={sidebarWidth}` with `onResize` calling
   `setSidebarWidth`). `defaultSize` should be the *initial* size; if v4
   re-applies it when the prop changes, the group is re-defaulted on
   every drag frame and fights the drag.
2. The shadcn `Sidebar` renders a `position: fixed`, `z-10` container of
   width `--sidebar-width`, which abuts — and with `ResizableHandle`'s
   `after:w-1` (~4px, half of it to the left of the 1px line) partly
   overlaps — the handle's hit area, while the handle itself has no
   stacking context of its own. That alone would read as a dead zone.
   Paseo's remedy (a ~10px pointer / 24-88px touch hit area above its
   siblings, pointer capture on drag start) addresses this directly.
