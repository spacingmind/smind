# UX audit small fixes

Nine small UX fixes from a flow audit of the web, desktop and mobile apps,
shipped as three PRs. This file tracks PR A; B and C add their sections.

## PR A: daemon-down state, shortcuts overlay, naming, no per-task cost

### Acceptance criteria

1. **Daemon-down cause + Retry (web).** When the first connect fails or does not
   answer within 8 s, the shell stops saying "Connecting…" and says what is
   wrong: the header pill, the sidebar status row and the home landing show the
   cause (the connect error text, or "No answer from the daemon after 8 s") and
   a Retry button, plus the hint "Start it with `smind serve`" on the
   non-desktop build. Retry re-runs the connect. A later successful connect
   clears all of it. Mid-session reconnects keep today's behaviour.
   Mobile has no equivalent screen (pairing screen already shows errors): not changed.
2. **`?` shortcuts overlay.** The `shortcuts.help` action (the `?` key, the
   header keyboard button, the Help menu entry) opens a dialog over the current
   view with a searchable, read-only cheat-sheet and a "Customize in Settings"
   link. It no longer navigates to Settings and the task view stays mounted.
   `?` typed in a text field still inserts `?`.
3. **Naming.** The sidebar footer "Host" popover is "This daemon". Settings
   "Agents" is "Profiles" everywhere a user reads it (nav label, heading,
   "New profile", footer popover row, palette entries, composer picker
   strings, public docs). Settings "Hosts" (other machines) is unchanged.
   Section ids, routes, test ids and the wire stay as they are.
4. **No per-task cost chip.** The "Task $x" usage chip in the task header and
   the cost label on the mobile task-tree row are gone; tokens stay where they
   were shown. The wire field stays; cost remains in Settings > Usage.

### Test scenarios

- `App.test.tsx`: connect rejects -> pill/sidebar/home show the cause and Retry; Retry calls connect again; a connect that never settles flips to the 8 s no-answer state under fake timers; success after Retry clears the error.
- `home-landing.test.tsx`: disconnected with an error shows cause + Retry.
- `keyboard`/`shortcuts-overlay.test.tsx`: the action opens a dialog, search filters, Esc closes, the task view stays mounted, `?` in the composer inserts `?`.
- `settings-screen.test.tsx`, `profiles-section.test.tsx`, `sidebar-footer` tests: new labels; old labels absent.
- `task-lineage.test.tsx`: no `$` text in the usage chip; tokens text still present.
- mobile `taskTree.test.ts`: tree row view model carries no cost.

### Decisions

- Item 3 of the audit scope (home composer Attach and effort chips) is not small
  (attachments must survive the task-create handoff): it moves to PR C.
- "Hosts" (fleet peers, ADR-0048) keeps its name; the collision is fixed on the
  sidebar footer side, which is about this daemon only.

### Progress

(filled in as work lands)

### Validation

(filled in at the end)
