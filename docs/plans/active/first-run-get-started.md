# E5-A: guided first run ("Get started")

Item A of ADR-0059 (UX flow umbrella), web and desktop. Decision D1 is accepted: no scan of
the home directory; the folder browser stays and "recent projects" come from what sMind
already knows (its own workspace list). Wire changes: none.

## Checked against `develop` (what is dropped)

- **Daemon unreachable**: shipped in `ux-audit-small-fixes` (cause + Retry + `smind serve` hint on
  the home landing, `home-connection-notice`). Not rebuilt; this plan only adds a frame for it.
- **Desktop first run** (`first-run-gate.tsx`, ADR-0031 D7): exists and stays as it is.
- **"Connect your first account" card** in Settings > Providers: exists; only its copy changes (AC6).
- **Recent projects**: the project menu on the home landing already lists every known workspace and
  remembers the last pick (`home-project-store`). That is the "recent projects" surface; nothing new is
  stored or scanned. Dropped as a separate item.
- **Starter seeding / office mode**: E17, out of scope.
- **Mobile**: no workspace-creation flow exists to guide; out of scope for this PR.

## Acceptance criteria

1. **Get started card.** The home landing shows a "Get started" card above the composer while
   there is no project OR no ready provider; it is hidden when both are met. Rows are computed live:
   *Sign in to an agent* (one status word per provider: Ready, Not installed, Signed out, Checking)
   and *Add a project* (opens the existing add-project dialog; "Done" with the project name once one
   exists).
2. **No empty provider pre-selected.** When every provider has been tested and none is ready, the
   home composer is disabled with the placeholder "Sign in to an agent to start a task" (a project
   without a ready agent is not a startable state). While readiness is unknown (not yet tested or the
   check failed) the composer is not disabled on that account.
3. **Placeholder order**: not connected, then no ready agent, then no project, then the normal text.
4. **Dismiss and re-open.** The card has a Dismiss button, remembered per browser (local storage,
   wrapped in try/catch); a dismissed card stays hidden even while a step is missing, but the
   composer's disabled reason still shows. Settings > General has "Show the Get started guide" which
   clears the dismissal.
5. **No provider call on render.** The card runs the existing single-flight `ensure()` once on mount
   (a local PATH / stored-credential check, no network); re-renders and a hidden card make no
   `provider.test` call.
6. **Providers empty state leads with the action.** The first-account card says "Connect an account so
   agents can sign in", with the `/v1` proxy sentence moved to a muted "Advanced" line.
7. **Layout.** At 390 px the card stacks, rows keep 44 px targets and there is no horizontal scroll;
   light and dark use tokens and the icon registry; status is words, not colour alone.
8. **No scan, no new wire.** No new RPC, no `fs.listDir` call from the card.

## Test scenarios

- `get-started-card.test.tsx`: renders both rows from providers+health+workspaces; hides when both met;
  hides after Dismiss and stays hidden on remount; "Show the Get started guide" brings it back;
  Add a project row calls `onAddProject`; status words per health (ok, missing CLI, signed out,
  untested -> Checking); `ensure` called exactly once across re-renders; `provider.test` not called by a
  hidden card.
- `readiness.test.ts` (pure helper): `agentsReady(providers, health)` -> ready / none / unknown.
- `home-landing.test.tsx`: card shown with no project; composer disabled with the "Sign in to an agent"
  placeholder when none ready; not disabled when readiness unknown; placeholder order; card absent when
  both met.
- `general-section.test.tsx`: the re-open row clears the dismissal.
- `first-account-card.test.tsx` (extend `providers-screen.test.tsx`): new lead copy, proxy sentence under
  Advanced.
- Frames (below).

## Frames

`~/.smind/dogfood/first-run-get-started/`: light and dark at web 1280 and 390 for: empty (nothing ready, no
project), daemon unreachable, no provider (project exists), provider ready (no project), first project
(card hidden), dismissed. Script asserts mean luminance high in light and low in dark and
`scrollWidth <= clientWidth` at 390; README lists each frame.

## Decisions

- Readiness uses the existing `useProviderHealth` answers (ADR-0032 AC12), not a new RPC.
- "Ready" = `provider.test` ok for at least one provider of the daemon's own list.
- Dismissal is a browser preference (`smind:get-started-dismissed`), not daemon state.
- Desktop keeps its own first-run screen; the card still shows on its home landing by the same rule.

## Progress

- [ ] spec
- [ ] card + helper + tests
- [ ] landing wiring + placeholder
- [ ] Settings re-open + providers copy
- [ ] frames

## Validation

(filled in when the PR is ready)
