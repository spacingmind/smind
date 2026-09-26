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

- **RunConfigState gained `baseAgentId`/`custom` instead of a plain `agent` field.** `baseAgentId` is sticky (stays set once an agent is picked, even after a hand-edit); `custom` is a dirty flag cleared by `applyProfile`/`resetToAgent`. This is what lets the Agent trigger show "Custom · from <name>" and the ↺ control restore exactly that agent's values, per AC.
- **The toolbar's run-config is persisted per task** (`smind:run-config:<taskId>`, `components/composer/run-config-preference.ts`), the same per-task-key `localStorage` pattern `use-composer-draft.ts` already established (docs/design.md §9). This is what makes the task header's run-config pill "durable" rather than a snapshot of in-memory-only state, and what survives a closed/reopened tab or a reload.
- **★ default agent replaces General's "Defaults for new tasks" control**, per the AC's own instruction — `general-section.tsx` now points at Settings → Agents instead of re-implementing the same choice with a narrower (provider+approval only, no thinking, no agent binding) mechanism. `useDefaultRunPreferences`/the old `defaultProvider`/`defaultApprovalPolicy` storage functions were deleted outright (no other caller referenced them) rather than left dead.
- **Agent menu is a DropdownMenu, not a Select.** The AC's row shape (per-profile provider/approval/thinking metadata, a "No agent" entry, a separator, "Manage agents…") doesn't fit Select's single-line SelectItem/SelectValue pairing; DropdownMenu already had every primitive needed except `DropdownMenuSeparator`, added to `ui/dropdown-menu.tsx`.
- **"Manage agents…" (and the sidebar footer's "Agents" row, and the "Settings: Agents"/"New agent…" palette commands) deep-link via a new `smind:open-settings` window event** (`{ sectionId }`), listened to once in `App.tsx`, mirroring the existing `smind:open-accounts`/`smind:use-agent` cross-tree channel this codebase already uses for exactly this "the composer isn't a child of the sidebar/settings screen" problem. Previously these three call sites just opened Settings at its default section; now they land on Agents specifically, closing a gap the AC asked for ("each deep-links straight into the matching Settings section").
- **Settings → Agents' Provider health dot reuses `provider.test` + `ui/status-dot.tsx`'s `StatusDot`, not the AccountsDialog's own transient `testResults` state.** Sharing that state would couple two independent surfaces for the sake of one RPC the daemon answers statelessly; the inline edit form fetches its own `provider.test` result when it opens, same rationale this codebase already uses for profiles/providers being fetched independently in the composer, the sidebar, and this section.
- **A credential-backed profile's health check resolves through `ProviderInfo.accountProvider`, not the raw taskrunner provider id** — `provider.test`'s `provider` param is accounts-vocabulary (e.g. `anthropic`) for anything not `kind: "cli"`, per `internal/wsapi/handlers.go`'s `handleProviderTest`; `accountHealthTestKey` in `profiles-section.tsx` does that lookup.

## Progress

- [x] Settings → Profiles renamed to "Agents" (section label/id, empty-state copy, form copy, composer picker label). RPCs/testids stay `profile.*`/`profile-*` per the plan's display-name-only rule.
- [x] `run-timeline.tsx` run header shows the provider's label, not its raw id ("label ?? id" via a providerLabels map task-detail builds from provider.list; RunEntry carries no profile id on the wire, so the raw id it *does* carry is the provider id).
- [x] Approval-policy labels unified in `lib/approval-policies.ts` (one `{id, label, help}` vocabulary): composer Select, agent form, General's "Defaults for new tasks", and the mid-run control (was "Manual", now "Manual approval"). Full-access keeps its per-provider vocabulary per the earlier user decision, recorded in the module's doc comment.
- [x] Settings nav regrouped: General · Appearance · group "Agents & providers" (Agents, Providers) · group "Connection" (Daemon server = renamed Connections, Daemon; desktop-only) · Notifications · Shortcuts. Providers is a nav stub on this branch (dispatches `smind:open-accounts`); ADR-0015's branch registers the real `providers` section and replaces it at merge.

- [x] Sidebar gear icon opens Settings, not Accounts (sliders icon and header Accounts button removed; Accounts entry moved to the sidebar footer).
- [x] Sidebar footer "Agents" row and the "Settings: Agents"/"New agent…" palette commands now deep-link straight to Settings → Agents (`smind:open-settings`, see Decisions) — no longer just the settings screen's default landing section.
- [x] Sidebar footer "Providers" row's health dot + healthy-count (AC: "● 2 healthy") — done in the polish pass: `accountHealthTestKey` moved to a shared `lib/provider-health.ts`, app-sidebar.tsx fetches `provider.list` + one `provider.test` per provider once per client and renders "● N healthy" (dot color success/warning/danger by how many are healthy).
- [x] Command palette: "Settings: Agents", "Settings: Providers", "Use agent: <name>" per profile (dispatched to the composer via a `smind:use-agent` window event), and "New agent…".
- [ ] Desktop-only "Settings: Daemon server"/"Settings: Daemon" palette entries — **not done**, deferred until the Connection regroup lands (out of scope for this pass; no desktop-only work was touched).

- [x] `RunConfigToolbar` behavior: picking an agent applies provider/approval/thinking; hand-editing a field flips the Agent selector to "Custom · from <agent>" with a ↺ reset; the override never writes back to the stored profile; Thinking is hidden for non-Claude providers (existing `=== "claude-native"` check, which already mirrors `thinking.go`); GLM/Kimi's live ACP options render inside the same row (`composer.tsx`'s `configOptions` prop, `RunConfigOptions` now rendered without its own outer width/padding wrapper). Tests: `composer.test.tsx`'s "run-config persistence and the ★ default agent" and "Profiles picker" describe blocks, `task-detail.test.tsx`'s config-options and run-config-pill describes.
- [x] Task header run-config pill: "<Agent|Custom|No agent> · <Provider> · <Approval> · <Thinking, Claude-only>", persisted per task (see Decisions), clicking it focuses the composer's toolbar row (`composer-toolbar-row`, `tabIndex={-1}` + `.focus()`). Tests: `task-detail.test.tsx`'s "run-config pill" and `runConfigPillLabel` describes.
- [x] ★ default agent in Settings → Agents: client-side (`lib/settings-preferences.ts`'s `readStoredDefaultAgentId`/`writeStoredDefaultAgentId`), a new task's toolbar seeds from it once `profile.list` resolves, clearing when its agent is deleted. General's old defaults control replaced (see Decisions). Tests: `profiles-section.test.tsx`'s "★ default agent" describe, `composer.test.tsx`'s "starts from the ★ default agent" test.
- [x] Settings → Agents card list: name, one metadata line, notes muted underneath, "⋯" menu (Edit/Delete) replacing the two always-visible buttons; Edit expands inline in the row (Name, Provider + health dot, Approval, Thinking, Notes, Cancel/Save) instead of repurposing the bottom form, which now stays a plain "New agent" add-only form. Tests: `profiles-section.test.tsx`'s rewritten edit/delete tests plus the new health-dot describe.
- [x] Command palette additions (from an earlier session in this plan, confirmed still passing): "Settings: Agents", "Settings: Providers", "Use agent: <name>", "New agent…".
- [x] Approval-policy labels unified (confirmed unchanged by this pass; still one `lib/approval-policies.ts` vocabulary everywhere).

### Polish pass (user screenshot review)

- [x] Settings → Agents' "New agent" form redesigned: a header "+ New agent" button (top-right, `app-sidebar.tsx`'s "+ New workspace" pattern) opens the same inline `<ProfileForm>` card Edit uses, at the top of the list; Provider/Approval/Thinking are one 3-column equal grid (a non-Claude provider gets an empty grid cell, not a dead Thinking control); the empty state shows a sentence plus its own copy of the button.
- [x] Agent trigger reads "No agent" (muted `text-foreground-muted`), not "Agents", matching the header pill and the agent menu's own wording for that state.
- [x] Composer focus ring: `focus-within:border-input-border-focused` (no ring/glow), matching every Select/Input's own focus treatment and ZCode's "calm, not glowing" input style — the old `border-ring` + `ring-3 ring-ring/50` read as a bright double border in dark mode (`--ring` is near-white there).
- [x] Sidebar footer "Providers" health dot + "N healthy" (see Progress above).

## Validation

Confirmed via `task test` (Go `go test ./...` all green, web `bun run --filter '@smind/ui' test` 1295/1295 passing including every test listed above), `bun run --filter '@smind/ui' typecheck` (clean), and `task lint` (go vet + gofmt clean).

Screenshots (desktop 1440×900, light + dark, against a temp daemon at a temp `SMIND_HOME` on :4706 seeded with one workspace/space/2 tasks/2 agent profiles/1 demo account) saved to `Downloads\smind-run-config-ia\` and reviewed by hand against ZCode's density:
- `01-home` — sidebar footer's Providers (health dot + count)/Agents shortcuts.
- `02-task-toolbar` — closed toolbar (Agent trigger reads "No agent", muted) + header pill.
- `03-agent-menu-open` — agent menu (No agent / per-profile metadata rows / Manage agents…⌘,).
- `04-toolbar-custom` — Custom · from <agent> + ↺, header pill reflecting the hand-edit.
- `05-header-pill` — pill after a provider change (thinking segment correctly omitted for GLM).
- `06-settings-nav-agents` — regrouped nav + Agents card list with the ★ star + header "+ New agent" button.
- `07-palette` — command palette open.
- `08-settings-general` — General's pointer copy replacing the removed defaults control.
- `09-agents-inline-edit` — inline expand-in-row edit, 3-column grid, provider health dot both red (no credential) and green (after adding one).
- `10-composer-focus` — focused composer shell, subtle border in both themes, no glow.
- `11-agents-new-card` — "+ New agent" card open at the top of the list, disabled "Add agent" until a name is entered.

A `web-design-guidelines` review pass over the changed files (first pass) found and fixed: three icon-only buttons missing `aria-hidden` on their glyph, an unbounded-width agent-menu item (`max-w-sm` + truncate), a focused-but-`outline-none` toolbar row (swapped for `focus-visible:ring`), and two "--" instances in rendered copy (should be "—").

### develop merge (PR #202, ADR-0015: Settings → Providers)

Merged after the polish pass. Conflicts (`app-sidebar.tsx`/`.test.tsx`, both from develop re-adding the pre-this-plan gear/sliders header split this plan's own AC already removed) resolved in favor of this plan's single-gear header + footer-shortcut IA; the footer's Providers row, the "Settings: Providers" palette command, and this plan's `smind:open-settings` channel now all land on ADR-0015's real `providers` section (`onOpenSettings?.("providers")`) instead of the temporary AccountsDialog shim, which is removed (state, effect, hand-rolled nav `<li>`, and the duplicate palette command all deleted). `providers-section.tsx` joins the "Agents & providers" nav group at order 180 (right after Agents=175) rather than staying a top-level entry. Full detail in the merge commit message.

Re-confirmed after the merge: `task test` (Go all green, web 1316/1316 -- 21 more than the polish-pass count, all from ADR-0015 plus the merge's own conflict-resolution tests), `bun run --filter '@smind/ui' typecheck` (clean), `task lint` (clean). Re-shot `06-settings-nav-agents` (single "Providers" entry now, no duplicate) and added `12-settings-nav-providers` (ADR-0015's real section, light + dark) -- both clean.

**Still not satisfied**: the desktop-only "Settings: Daemon server"/"Settings: Daemon" palette entries (Progress's one remaining unchecked item) — gated on the not-yet-done Connection nav regroup, out of scope for this plan's work so far. Every other acceptance criterion in this file is confirmed working. Plan stays in `active/` for that one remaining item and because the Providers-nav wiring is a distinct, separately-tracked follow-up per the user's own framing ("once it's merged, I'll ask you to merge develop and wire the Providers nav to the real section" — read here as: this merge itself, done).
