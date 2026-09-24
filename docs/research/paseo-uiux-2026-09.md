# Paseo UI/UX research — 2026-09-24

Two sources: `pplx -m best` (2 queries — public docs/changelog/releases for
0.8–0.9.1, and user feedback from HN, r/PaseoAI and GitHub issues), plus a
read-only code-level gap analysis of `refs/paseo/packages/app` (pulled to
`bbf8cce`, 2026-09-24) against `web/packages/ui/src` and `mobile/src`.

## What Paseo's UX is (public sources)

- **IA:** the sidebar is projects → workspaces (local dir or managed git
  worktree). A row can show host, PR, checks and scripts. Every session
  (agent, terminal, browser, diff) is a tab inside a split-pane grid
  (`⌘D` / `⌘⇧D`). There is an Explorer sidebar with Files and Changes
  tabs (`Cmd/Ctrl+E`).
- **0.8–0.9 headline features:**
  - `Cmd/Ctrl+F` Find in chat (including history outside the loaded
    window), in files (with replace) and in terminals.
  - Multi-select agent questions rendered as answer forms with an
    "Other" field.
  - Command Center for global, workspace and agent actions, plus
    model/mode/reasoning changes.
  - Customizable and multi-key shortcuts.
  - Voice mode.
  - npm plugins (tabs, slash commands, timeline renderers, attachment
    sources).
- **Users praise:** desktop↔phone continuity, one interface for many
  providers, split panes with terminal and diff side by side,
  self-hosting.
- **Users complain:**
  - Missing character-level diff highlighting, find-in-diff, and
    commenting in the commit pane.
  - Cryptic config errors.
  - Mobile memory resets on very long sessions.

## Already at parity in smind

These are not gaps (see `docs/plans/completed/ui-redesign-parity.md`,
`visual-identity-console.md`, the `pane-split-tree*` plans and
`tab-registry-side-dock.md`):

- Tokens and dark mode.
- Command palette and quick open.
- A rebindable, chord-less keyboard registry.
- A 4-direction split tree with drag-to-split.
- A structured timeline and tool cards.
- The composer card.
- Permission cards.
- Sidebar attention.
- Settings as a screen.
- Diff v2 with comments, and multiple terminals.

## Gaps, prioritized (code-level analysis)

1. **Find in chat, file and terminal, one shared widget** — *M*.
   - smind has no Find anywhere.
   - smind loads the full run history on the client
     (`use-run-timeline.ts` via `run.logs`), so chat Find can stay
     client-side.
   - Port from: `pane-find/*`, `agent-stream/chat-find/*`,
     `terminal/find/*`, `file-pane/find/*`.
2. **AskUserQuestion / ExitPlanMode wired end to end** — *M, daemon
   change*.
   - smind's question-form and plan cards are dead UI: nothing in
     `internal/` produces `questions`.
   - Port the multi-select + Other answer rule from
     `question-form-card-core.ts`.
3. **Pane and tab keyboard actions** (split, focus pane while typing,
   move/close tab, next/prev, `Cmd+,`) — *S–M*.
4. **Chord shortcuts, and rebinding moved into Settings → Shortcuts with
   search** — *S–M*.
   - smind's matcher is chord-less (`keyboard/shortcuts.ts`).
5. **@file mentions (web-only, reuses `task.searchIndex`) then slash
   commands (daemon must surface ACP `available_commands`)** — *M*.
6. **Attachments** (paste, drop, pills, lightbox) — *L, wire change*.
7. **Notifications that act** — *S*.
   - Click focuses the task, unread count in the tab title, optional
     sound, and a Notifications settings section.
   - smind's `new Notification(...)` has no `onclick`.
8. **Sidebar triage** — *M*.
   - Pin, mark unread, a hover card (branch/diff/PR), and a
     group-by-status view.
9. **Tab context menu and a richer palette** — *S*.
   - Close others/left/right, rename, copy path, a context-window meter,
     and `Shift+Tab` mode cycle.
10. **Mobile task detail to a usable level** — *L, push needs daemon*.
    - Markdown, Stop, auto-follow, question/plan cards, a model picker,
      QR pairing and push.

**Deliberately skipped:** voice mode and plugins (they were non-goals in
the parity plan).

**Needs a daemon/wire change (AGENTS.md rule d, ADR first):** items 2,
5 (slash only), 6 and 10 (push), plus rewind/fork.

## Batching (2026-09-24)

- **Batch A, web-only, started now:**
  - `web-find` (item 1).
  - `web-keyboard-tabs` (items 3, 4, 9, plus `Cmd+digit` task jump from
    item 8).
  - `web-sidebar-attention` (items 7, 8).
- **Batch B:** waits on user decisions.
