# UI/UX polish research — web + mobile (2026-09-17)

Researched via `pplx -m best` (3 queries) after the user flagged smind's
web UI as still looking "ugly" despite the Phase 2 UI redesign
(`docs/plans/completed/ui-redesign-parity.md`). Current stack check
first, since "wrong tech" was the first hypothesis to rule out.

## Current stack (confirmed, not a gap)

`web/packages/ui/package.json` already has: Tailwind v4
(`@tailwindcss/vite`), `shadcn` CLI, `radix-ui`, `class-variance-authority`
(CVA), `tailwind-merge`, `tw-animate-css`, `lucide-react`. 95 `.tsx` files
under `web/packages/ui/src`, a solid shadcn primitive set already
installed (`button`, `dialog`, `dropdown-menu`, `select`, `tabs`,
`sidebar`, `toast`, `tooltip`, `context-menu`, `sheet`, `resizable`,
`scroll-area`, `separator`, `skeleton`, `alert`), plus `docs/design.md`
(368 lines, tokens/dark-mode written up in Phase 2's foundation track).

**So the gap is not "we haven't adopted shadcn/tailwind" — it's design
execution on top of an already-correct stack.** This matches query 1's
central finding almost exactly (see below): shadcn's defaults *are* the
generic look; a team has to deliberately customize the token/typography/
variant/motion layer, not just install the library, or the result reads
as templated regardless of tech choice.

## 1. Why shadcn UIs converge on "the same look" + how to break out

- **Root cause**: same neutral palette, same `rounded-md`, same thin gray
  borders, same Lucide icons, same Button/Card/Dialog/Tabs/Table/Sidebar
  used exactly as shipped, same conventional sidebar+topbar+cards layout.
  Each choice alone is reasonable; the *combination* is what reads as
  "generic dashboard."
- **Fix is systemic, not per-component**: define a product visual thesis
  first (3 adjectives + 1 metaphor — e.g. for smind: "calm technical
  console" vs "instrument panel" vs "code laboratory"), *then* build
  semantic surface tokens beyond `background`/`card`/`muted` (e.g.
  `--surface-1/2/3`, `--border-subtle/strong`, a brand color with **one
  specific job** rather than painting the whole UI, a separate — not
  inverted — dark-mode composition).
- **Typography**: pick a deliberate 3-way pairing (UI sans, a distinct
  display face for page/section titles only, a tuned mono for code/ids/
  commands) plus an actual type-role scale (workspace title, section
  title, panel title, metadata label, code annotation) instead of
  `text-xl font-semibold` everywhere.
- **Spacing rhythm**: pick a fixed scale (4/8/12/16/24/32/48-64px) and a
  *density tiers* model — compact for file trees/logs/tables, standard
  for forms/settings, relaxed for onboarding/explanations/empty states —
  rather than one density for the whole app.
- **Component variants should encode product meaning, not just visual
  weight**: CVA variants named `execute`/`agent`/`approval`/`quiet`
  (semantic) beat `default`/`secondary`/`destructive` alone for a tool
  whose actions have real stakes (run a command, ask the agent, approve
  a diff).
- **Depth**: prefer a small elevation vocabulary (flat/raised/floating/
  focused/embedded) over `shadow-sm` on every card; borders, surface
  contrast and accent rails communicate hierarchy better than shadows in
  technical UIs. Restrained texture (a barely-visible grid, a gradient
  wash behind the agent conversation) can make a large empty canvas feel
  intentional if it disappears once noticed.
- **Motion**: small vocabulary (120-160ms hover, 180-240ms menus,
  250-400ms panels), and — most relevant for an agent product — animate
  *state transitions* (queued→running, streaming tokens, diff line
  accepted, permission request entering foreground) rather than
  decorating every mount.

## 2. Agent-UI-specific patterns (directly applicable to smind's timeline/composer/permission v2)

The central design test from the research: at every moment, the user
should be able to answer *what is the agent doing, why, what changed,
what's waiting on me, how do I undo it, how much detail am I currently
seeing* — without reading every token.

- **Turn = narrative (prose) + typed timeline (operational record), not
  one bubble.** Tool activity renders as compact, typed rows under the
  streaming prose (`Reading`, `Searching`, `Editing`, `Running`, `Waiting
  for approval`, `Completed`, `Failed`), each with a **stable row height
  while streaming** — avoid reflowing the whole conversation per token.
  Show the tool's *semantic title* immediately ("Search for
  `PasswordResetToken` in src/"), not the raw tool name ("Calling
  ripgrep...").
- **Multi-step tasks need nested/collapsible hierarchy** (plan → step →
  file-level sub-steps), so a 25-tool-call turn reads as progress at
  whatever level the user cares about, not a flat scroll.
- **Streaming behavior**: pin the latest active item near the bottom;
  stop auto-scroll once the user scrolls up, with an explicit "Jump to
  live" control; make interruption explicit (`Stopped by user`, not a
  silently truncated message); preserve partial output after cancel.
- **Tool cards have a predictable anatomy** per type — edit (`+12 -3`,
  preview/open/revert), command (exit code, duration, collapsed output
  with a "Show output" affordance, failed output *expanded by default*
  with the actionable line surfaced), search (match count, "Show
  matches"). Never dump unbounded output into the transcript — bounded
  preview + line count + "open full output."
- **Diffs are first-class objects, not text blobs in chat**: file list
  with status (modified/added/deleted/renamed), per-file +/- counts, a
  dedicated diff pane for multi-file changes (not a narrow chat column),
  and — a strong pattern from Windsurf Cascade — **named checkpoints**
  the user can revert to, labeled by what produced them ("Revert
  changes made by 'Implement reset endpoint'"), not one ambiguous global
  Undo.
- **Permission prompts must state the exact action, not an opaque
  category**: exact command/path, cwd, what files may change, whether
  it's reversible, *why* the agent wants to do it. Offer risk-scoped
  "always allow" (`Allow commands matching: npm test *`, not a blanket
  "always allow this tool"), and preview the resulting rule before
  saving it. Keep the approval card anchored *in the timeline* next to
  the tool call it belongs to, never a detached modal that loses
  context — directly relevant to smind's permission v2 UI and to the
  CLI rendering just added in PR #144 (same principle, different
  surface: show the exact command + options, not just "needs approval").
- **Three-level density model for the IDE-style layout**: persistent
  chrome (repo/branch, task name, agent status, permission mode, model,
  change count, stop control) → operational rail (plan outline, files
  changed, pending approvals, checks, background tasks) → one detailed
  main surface at a time (chat/diff/editor/terminal), with pin/split/
  pop-out rather than showing everything at full size simultaneously.
  "Summary by default, detail on demand" (a transcript density toggle —
  Normal/Verbose/Summary, per Claude Code Desktop) is the concrete
  version of this.

## 3. Mobile companion app patterns (Phase 3 relevant — pairing, push, approval)

Directly extends ADR-0007's mobile scope and the push-notification
findings already in `docs/research/relay-design-status.md` §7 (wake-up-
only push, no content/size leak to FCM/APNs):

- **Actionable push is the core interaction, not a link back to a chat
  screen.** Real examples (Claude Remote, claude-push/ntfy-based tools)
  put the operation description *in the notification body* ("wants to
  run `rm -rf build/` in project dir") with inline Approve/Reject
  buttons — reportedly ~62% of approvals happen without ever opening the
  app. Two notification classes: "agent decided something" (task done/
  error — fire-and-forget, like a CI pipeline result) vs "action
  required" (permission/question — always takes priority, always
  actionable inline).
- **Small-screen timeline**: collapsible tool-call cards (not raw
  terminal dump), read vs write operations visually distinguished, a
  fixed bottom bar with quick actions (Continue/Stop/Approve/Reject),
  status at a glance (running/waiting/finished/error) for the session
  list.
- **Quick-approve**: one-tap Approve/Deny directly on the notification
  banner is the standard; explicit tap targets are preferred over swipe
  gestures for anything with real consequences (avoids accidental
  rejection of a critical operation via swipe).
- **QR pairing UX, concrete precedent beyond paseo**: CLI/daemon
  generates the QR (trust originates on the already-trusted machine, not
  the phone); always offer a manual code fallback (6-digit) for no-camera
  cases; some implementations add a second-factor explicit CLI approval
  step after the phone scans (`<tool> devices approve <requestId>`)
  before the pairing is live — worth considering as an optional
  extra-paranoid mode for smind's pairing flow beyond what ADR-0007 (g)
  already specifies, not a replacement for it.

## Not yet done

This is design research, not implementation — no CLAUDE.md/design.md
edits made yet. Natural next step: a design-token/typography refresh pass
on `web/packages/ui` (semantic surface tokens, type-role scale, CVA
variant naming) informed by §1, likely worth its own plan doc given the
existing `docs/design.md` foundation from Phase 2 — not started as part
of this research pass.
