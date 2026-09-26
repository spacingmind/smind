# Run-config IA: sidebar, composer toolbar, Settings nav, palette

Frontend-only information-architecture cleanup for how providers, agents
(profiles), and per-run config are surfaced. Source: UX review
`docs/research/local/ux-review-providers-agents-2026-09.md` and the
user-approved sketch `docs/research/local/ui-sketch-run-config-2026-09.md`
(both local-only, not committed — read them if still present on disk;
otherwise the acceptance criteria below are the authoritative spec, they
were transcribed from those files verbatim). User's own words when
approving the sketch: "Ok hợp lí, làm đi, tôi cần nó clean như zcode/paseo
nhé" — match ZCode's density and restraint (P1 design system,
`docs/design.md`): neutral tokens, `text-ui-*` scale only, radius nesting
xl→lg→md→sm, one primary action per surface, muted metadata.

No backend/wire changes in this plan — everything here is read-only
against existing RPCs (`profile.list`, `account.list`, etc.). The
account-mutation RPCs and ADR-0015 are a separate, parallel task
("Providers settings: accounts as Settings section (ADR-0015)") — don't
duplicate that work here; this plan only *renders* provider/account data
that already exists.

## Acceptance Criteria

- **Sidebar gear icon opens Settings, not Accounts.** Today (per the UX
  review) the gear/cog icon in `app-sidebar.tsx` opens the Accounts
  dialog, and a separate sliders icon opens Settings — the reverse of
  what every user expects from a gear glyph. Swap so ⚙ opens Settings;
  drop the sliders icon.
- **Sidebar footer gets two shortcuts:** "Providers" (with a health dot
  and a healthy-count, e.g. "● 2 healthy") and "Agents" (with a count,
  e.g. "3"). Each deep-links straight into the matching Settings section
  (see nav below), not a modal.
- **`RunConfigToolbar`: one shared component/context replacing the
  current independent Selects** in the composer (agent/profile picker,
  provider, approval policy, thinking) — today these are separate
  components each with their own state (`run-config-options.tsx`,
  `approval-policy-control.tsx`, plus the profile picker). Layout:
  `[Agent ▾] [Provider ▾] [Approval ▾] [Thinking ▾] ... [Send]`.
  - Picking an agent (profile) fills the other three fields from that
    profile's stored config.
  - Hand-editing any one field after that flips the Agent selector to
    show **"Custom"** (with a small "· from <agent name>" hint) and a ↺
    reset control that restores the picked agent's values. This is a
    per-run override only — it must never write back to the stored
    profile.
  - Thinking is hidden entirely for providers that don't support it
    (mirror the existing `thinking.go` / `run-config-options.tsx`
    provider-capability check — don't hardcode a new list).
  - GLM/Kimi's existing live ACP run-config options (the per-session
    options surfaced via `task options`/`task set-option`) render in
    this same row, not a separate control elsewhere in the composer.
- **Agent menu inside the toolbar** lists each profile as
  "<name> (<provider> · <approval> · <thinking>)", plus a "No agent"
  entry, a separator, and "Manage agents… ⌘,". Selecting "Manage
  agents…" opens Settings → Agents.
- **Task header shows a run-config pill** summarizing the *durable*
  config the task is set to run with (e.g. "Quick Fixes · Claude Code ·
  Manual · Standard") next to the existing title/status/branch line.
  Clicking it moves focus to the composer toolbar. This is display-only,
  derived from the same state the toolbar reads — no new persistence.
- **Settings nav regrouped** into: General · Appearance ·
  **Agents & providers** (sub-items: Agents, Providers) ·
  **Connection** (desktop-only; sub-items: Daemon server, Daemon) ·
  Notifications · Shortcuts.
- **Settings → Profiles renamed to "Agents"** throughout the UI (route,
  nav label, empty-state copy) — the underlying RPCs/store table stay
  `profile.*`/`agent_profiles`, this is a display-name change only, not a
  wire rename.
  - Card list per agent: name, one metadata line
    ("<provider> · <approval> · <thinking>"), notes muted underneath, ⋯
    menu (Edit / Delete).
  - Add a **★ default** marker on one agent — the one a brand-new task
    starts with when nothing is picked yet. Store this as a client-side
    UI preference (e.g. localStorage), *not* a new daemon field — it
    replaces Settings → General's existing "Defaults for new tasks"
    control, which should be removed once this ships (having both would
    be two sources of truth for the same thing).
  - Edit opens **inline in the row** (expand/collapse), not a dialog:
    Name, Provider (with a health indicator for its backing account,
    reusing whatever the accounts dialog already computes for account
    health), Approval (with the existing help-line copy), Thinking,
    Notes, Cancel/Save.
- **Command palette additions:** "Settings: Agents" (⌘,), "Settings:
  Providers", "Use agent: <name>" for each profile, "New agent…", and
  (desktop builds only) "Settings: Daemon server", "Settings: Daemon".
- **Approval-policy labels unified.** The review found the toolbar,
  profile form, General's "Defaults for new tasks", and the mid-run
  approval control each phrase manual/auto-safe/full-access slightly
  differently. Pick one label set and use it in all four places (a
  single shared constant/lookup, not four copies).
- **`run-timeline.tsx` shows a profile/agent's *label*, not its raw id**,
  wherever a run is annotated with which agent produced it.
- Every new/changed surface is screenshotted (desktop viewport) in both
  light and dark theme against a temp daemon with sample data (workspace/
  space/tasks/accounts/profiles), saved to
  `Downloads\smind-run-config-ia\`, and visually checked against the
  ZCode density bar before this is called done — per standing
  instruction, "clean like ZCode/Paseo" is verified by screenshot, not
  assumed from the diff.

## Test Scenarios

1. Click the sidebar ⚙ → Settings opens (not Accounts). Click "Providers"
   footer shortcut → Settings opens scrolled/focused to the Providers
   section. Click "Agents" footer shortcut → same for Agents.
2. In the composer, pick an agent from the toolbar → provider/approval/
   thinking fields update to match. Change the approval field by hand →
   agent selector shows "Custom · from <name>" with a ↺; click ↺ → all
   three fields revert to the picked agent's values.
3. Pick a provider that doesn't support thinking (e.g. one without a
   thinking capability) → the Thinking control disappears from the row
   entirely, doesn't just disable.
4. Start a run on a GLM/Kimi task that exposes live ACP config options →
   those options render inside `RunConfigToolbar`'s row, not a separate
   panel.
5. Open a task with a profile applied → header pill reads
   "<agent> · <provider> · <approval> · <thinking>"; click it → composer
   toolbar receives focus.
6. Settings → Agents: mark a different agent ★ default, create a new
   task → its toolbar starts pre-filled with that agent. Confirm
   Settings → General no longer has a separate "Defaults for new tasks"
   control.
7. Settings → Agents: click ⋯ → Edit on a card → form expands inline in
   place (no dialog/overlay); Save collapses it back to the summary row
   with updated values.
8. ⌘K → type "agents" → see "Settings: Agents", "Use agent: <name>" per
   existing profile, "New agent…". Selecting "Use agent: X" applies it to
   the active composer the same as picking it from the toolbar.
9. Compare the four approval-policy label sites (toolbar, agent
   edit form, mid-run approval control, and wherever General's old
   defaults control used to be) — identical strings.
10. Light/dark screenshots of: sidebar, composer toolbar (closed +
    agent-menu open + one field hand-edited), task header pill, Settings
    → Agents (list + inline edit open), Settings nav, command palette
    open — all saved and self-reviewed against ZCode's density.

## Decisions

(none yet — fill in as implementation surfaces choices; if any choice
touches the daemon/wire or public API shape, stop and ask per AGENTS.md
rule (d) rather than deciding unilaterally)

## Progress

- [x] Settings → Profiles renamed to "Agents" (section label/id, empty-state copy, form copy, composer picker label). RPCs/testids stay `profile.*`/`profile-*` per the plan's display-name-only rule.

- [x] Sidebar gear icon opens Settings, not Accounts (sliders icon and header Accounts button removed; Accounts entry moved to the sidebar footer).
- [x] Sidebar footer shortcuts: "Providers" (opens the existing accounts dialog until Settings has a Providers section) and "Agents" (opens Settings). Health dot / counts deferred to the RunConfigToolbar pass so they read the same hooks the toolbar will.
- [x] Command palette: "Settings: Agents", "Settings: Providers", "Use agent: <name>" per profile (dispatched to the composer via a `smind:use-agent` window event), and "New agent…". Desktop-only "Settings: Daemon server"/"Settings: Daemon" deferred until the Connection regroup lands.

## Validation

(fill in once every acceptance criterion above is confirmed working,
citing the test run/screenshots that proved it, then move this file to
docs/plans/completed/)
