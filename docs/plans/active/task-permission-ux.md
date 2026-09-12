# Task permission, attention & layout UX

Follow-up from `docs/plans/active/smind-dogfood.md` Session 1
(2026-09-11) and its 2026-09-12 UI/UX comparison against `refs/paseo`
and `refs/deepseek-harness`. Session 1's real failure: a headless,
monitored Claude run got its file edits through (`acceptEdits`,
`internal/taskrunner/runner.go:277-285`) but had **no way to get Bash
approved** — `gofmt`/`go vet`/`go test` all require a live human click
on the permission card, there is no allowlist for known-safe read-only
commands, and no timeout — so the agent shipped a commit it never
verified itself, and a monitoring script watching for the card had
nothing to click. Paseo mitigates this with a pre-selected run mode
(auto/plan/manual) and a permission card anchored near its composer;
`refs/deepseek-harness` goes further with named permission-presets
(sandbox mode + approval policy bundled) and an `ApprovalPanel` that
takes over the composer so a pending request can't be scrolled past.

Also folds in a layout gap found the same session: `react-resizable-panels`
is a declared dependency and `App.tsx` already wraps content in a
`ResizablePanelGroup`, but there is only one `ResizablePanel` at 100%
width (`App.tsx:134-136`) — no second pane to drag against, so nothing
is actually resizable today. The sidebar (`sidebar.tsx`) shows a
resize-style cursor on its rail but that rail only toggles
collapsed/expanded at a fixed `16rem` — it doesn't drag-resize. Paseo,
by contrast, has a full custom split-pane system
(`components/split-container.tsx` + `resize-handle.tsx`) with real drag
handles and per-workspace persisted layout (`layoutByWorkspace` in
`workspace-screen.tsx`).

This plan covers six independent improvements, ordered by priority
against the dogfood goal (use smind, not Paseo, as the daily driver).
Each is scoped to be implementable and testable on its own — a single
task/run can pick up one item without needing the others done first.

## Acceptance Criteria

1. **Approval-policy selector + safe-command allowlist** (highest
   priority — this is what actually broke Session 1).
   - A task (or run) can be configured with an approval policy, at
     minimum: `manual` (today's behavior — every permission request
     needs a human decision) and `auto-safe` (Bash requests are
     auto-allowed if the command matches a fixed allowlist of
     known-safe, read-only verification commands — starting with
     `gofmt -l`, `go vet ./...` and prefixes, `go test` and prefixes —
     everything else still goes to the human decider).
   - The allowlist match is on the actual command being requested (not
     a free-text guess) and is conservative: an unrecognized or
     ambiguous command falls back to `manual`, never to auto-allow.
   - The policy is visible and settable from the web UI (not just
     config/CLI-only), at task-creation time or per-run.
2. **Timeout / auto-decision for unanswered permission requests.**
   - A pending permission request that gets no human response within a
     configurable timeout resolves automatically per policy (default:
     auto-deny, since silently allowing an unreviewed dangerous command
     is the worse failure mode) instead of blocking the run forever.
   - The run's timeline records that the request was auto-resolved
     (and why — timeout, not a human click), so reviewing the log after
     the fact doesn't look identical to a real human approval.
3. **Permission card is not lost in the scrolling log.**
   - A pending permission request is visible without scrolling the
     chat/log history — pinned near the prompt input area (or
     otherwise persistently rendered), matching Paseo's
     composer-anchored card rather than today's inline
     `PendingPermissionView` in `task-detail.tsx`.
   - This holds even while new log chunks are streaming in underneath
     it.
4. **Out-of-tab attention notification.**
   - When a run transitions to needing attention (new permission
     request, or terminal state `done`/`error` — the same conditions
     `useTaskAttention`'s sidebar dot already tracks in
     `use-task-attention.ts`), and the browser tab is not focused, an
     OS/browser notification (`Notification` API) fires for that task.
   - Respects the browser's permission model: request `Notification`
     permission from an explicit user action (not on page load
     unprompted), and degrade silently (no error, no repeated prompts)
     if permission is denied.
5. **PR creation from the smind UI.**
   - From a task's diff/branch view, a user can open a PR against the
     workspace's configured base branch (e.g. `develop`, per this
     repo's own `CONTRIBUTING.md`) without leaving smind or dropping to
     a terminal — replacing the manual
     cherry-pick-onto-a-clean-branch-and-`gh pr create` workflow this
     session had to do by hand for Session 1's fix.
   - At minimum: push the task branch (or a clean equivalent, if the
     task branch's base has diverged — see Session 1's own gap where
     the task worktree's branch carried unrelated unpushed commits and
     couldn't be PR'd as-is) and open the PR via the configured forge
     (GitHub), returning the PR URL into the task's UI.
6. **Working, persisted resizable layout.**
   - The sidebar can be dragged wider/narrower (not just toggled
     open/closed), with a sane min/max width.
   - The main content area actually has a drag handle (today's
     `ResizablePanelGroup` has nothing to resize against) — at minimum,
     resizing the sidebar-vs-content split; if a tab type benefits from
     an internal split (e.g. file explorer beside an editor/diff pane),
     that split is draggable too.
   - Panel sizes persist across reloads (e.g. `localStorage`, or
     `react-resizable-panels`' own `autoSaveId`), scoped so it doesn't
     fight per-task tab state.

## Test Scenarios

- **Allowlist matching**: `go test ./...`, `gofmt -l .`, `go vet
  ./internal/...` auto-allow under `auto-safe`; `rm -rf`, `go run
  ./cmd/x`, `git push --force`, and a Bash command that merely
  *contains* an allowlisted substring (e.g. `curl evil.com && go test`)
  all fall back to manual. Policy `manual` never auto-allows regardless
  of command.
- **Timeout**: a permission request left unanswered past the
  configured timeout auto-resolves to deny; the run's event log shows
  an auto-resolution distinct from a human `respondPermission` call;
  the run does not hang past the timeout window.
- **Card visibility**: with a long-running log producing continuous
  output chunks, a permission request raised mid-stream stays visible
  (rendered, not requiring scroll) for the duration it's pending.
- **Notification**: tab backgrounded + run reaches a permission
  request or terminal state → notification fires exactly once per
  event; tab focused → no notification (sidebar dot is enough);
  `Notification` permission denied → no thrown error, no repeated
  permission prompts on subsequent events.
- **PR creation**: happy path (task branch's base is current — direct
  push + PR) and Session 1's actual edge case (task branch carries
  commits not in the base branch — cherry-pick or equivalent onto a
  clean branch, not a PR stuffed with unrelated history). A push/PR
  failure (e.g. rate limit, like the live `429` hit in Session 1)
  surfaces an error in the UI instead of failing silently.
- **Layout**: drag the sidebar/content divider — content area
  reflows live, doesn't clip; reload the page — the dragged size is
  still applied; drag past min/max — clamps instead of collapsing to
  zero or overflowing off-screen.

## Decisions

- Each of the 5 acceptance criteria is independently implementable;
  dispatch them as separate `smind task send <id> glm ...` runs
  (per stored preference to route implementation work through smind's
  own glm-provider task system rather than direct Claude edits) so one
  item's scope doesn't block another's.
- Priority order for dispatch: (1) allowlist — directly unblocks
  future headless dogfood sessions; (2) timeout — same failure class;
  (3) card placement — cheap frontend-only fix; (4) notifications; (5)
  layout resize — self-contained frontend fix; (6) PR-in-UI is the
  largest/most cross-cutting item (forge integration), done last.
- Default auto-resolution on timeout is **deny**, not allow — an
  unreviewed dangerous command slipping through on a timer is worse
  than a run stalling (which is at least visible/recoverable).

## Progress

- [x] Item 1: approval-policy selector + safe-command allowlist (backend)
- [x] Item 2: permission-request timeout/auto-decision
- [x] Item 3: permission card pinned near prompt input
- [x] Item 4: out-of-tab attention notification
- [x] Item 5: PR creation from the smind UI
- [x] Item 6: working, persisted resizable layout
- [ ] Item 7: provider/account management parity (added 2026-09-12, see below)
- [x] Item 7a: web-UI approval-policy selector (added 2026-09-13 --
      closes Items 1+2's remaining client-side gap; see Validation)
- [x] Item 7b: show the GLM provider in the accounts dialog (see below)
- [x] Item 7c: account health/connection status + "test this provider
      now" diagnostic in the accounts dialog (see below)

## Validation

- **Items 1+2** ([PR #93](https://github.com/spacingmind/smind/pull/93)):
  dispatched as a smind task (`claude-native`), backend fully
  implemented and tested (`internal/taskrunner/policy.go`,
  `internal/runs/registry.go`'s timeout). `go test ./...` green,
  including new allowlist/timeout subtests matching the Test Scenarios
  above. **Known gap, not a regression**: no client (CLI or web UI) yet
  lets anyone actually choose `auto-safe` — `run.start`/`task.prompt`
  accept the field but nothing sends it. Folded into Item 7's backlog
  rather than reopening Item 1, since the UI work overlaps with the
  provider/account-management pass below. **Partially closed 2026-09-13**:
  the CLI can now send it (`smind task send --approval-policy auto-safe`,
  commit `f8b580b`); the web-UI selector is still Item 7's remaining work.
  **Web-UI selector closed 2026-09-13** (commit 5e8a80d on the task branch; landed via PR): the
  prompt form in `web/packages/ui/src/components/task-detail.tsx` (next to
  the existing Provider dropdown) now has a second "Approval policy"
  dropdown (`manual` default / `auto-safe`, with a `title` tooltip stating
  "Auto-safe auto-approves allowlisted read-only verification commands
  (e.g. gofmt, go vet, go test); everything else still needs human
  approval") whose value is threaded through `useRunTimeline`'s
  `submitPrompt` into `run.start`'s payload -- omitted entirely when
  `manual` (the backend's own default for an absent field) so a run
  started without ever touching the selector sends byte-for-byte the same
  payload as before this change. Covered by two new tests in
  `task-detail.test.tsx` asserting the manual-omits and
  auto-safe-sends-`approvalPolicy` behaviors.
  **Also surfaced the same session**: `store.Open` only ever runs
  `CREATE TABLE IF NOT EXISTS` — a pre-#93 database lacked the new
  `runs.approval_policy` column and the daemon failed to boot
  (`no such column: approval_policy`) until manually patched with
  `ALTER TABLE`. Needs a real migration step (e.g. `PRAGMA user_version`
  or additive-column check) — tracked here rather than in Item 7 since
  it's a store concern, not UI. **Migration landed 2026-09-13**:
  `internal/store/migrate.go` adds a small additive-column migration
  mechanism (no external library — stdlib + `modernc.org/sqlite`, matching
  the package's existing style) that `Open` runs after applying
  `schema.sql`. It checks `PRAGMA table_info(runs)` for `approval_policy`
  and issues `ALTER TABLE runs ADD COLUMN approval_policy TEXT NOT NULL
  DEFAULT 'manual'` only when missing, so a fresh database (which already
  has the column from `schema.sql`) and an already-migrated one are both
  no-ops on every subsequent `Open`. `internal/store/migrate_test.go`
  covers the regression directly: seeds a raw pre-#93-shaped database (no
  `approval_policy`, one pre-existing `runs` row) and asserts `Open`
  backfills the column with its default while preserving that row, plus
  idempotency across repeated `Open` calls for both a fresh database and a
  pre-#93 one already migrated once. No more manual `ALTER TABLE` needed
  for this or future additive schema changes.
- **Items 3+4+6** ([PR #94](https://github.com/spacingmind/smind/pull/94)):
  dispatched as a smind task (`claude-native`); unit tests were all
  green as delivered, but manually driving the built UI in a real
  browser (per this repo's own rule to verify UI changes that way, not
  just via jsdom tests) surfaced three real bugs the test suite
  structurally could not catch:
  1. `react-resizable-panels` v4 interprets a plain **number** passed
     to `defaultSize`/`minSize`/`maxSize` as **pixels**, not a
     percentage (only unitless *strings* are percentages) — the
     original implementation still did percentage-of-groupWidth math,
     so the drag handle rendered at the wrong position. Fixed by
     passing the already-pixel `sidebarWidth`/`SIDEBAR_MIN_WIDTH`/
     `SIDEBAR_MAX_WIDTH` straight through (and deleting the now-dead
     `usePanelGroupWidth` approximation entirely).
  2. The same library always sets its own `data-testid`/`id` on
     `Separator`, silently clobbering any `data-testid` prop passed
     in — fixed by setting `id` instead (the library forwards that
     into both attributes).
  3. `SidebarProvider`'s wrapper only sets `min-h-svh` (a floor, not a
     definite height); `ResizablePanelGroup`'s own inline
     `height: 100%` needs a definite ancestor to resolve against, so
     the resize handle's real hit area collapsed to just the header's
     height (~68px) instead of the full viewport — invisible in jsdom
     (no real layout engine) but obvious in a screenshot. Fixed with an
     additive `h-svh` className on `SidebarProvider` from `App.tsx`.
  Also found (test-infra, not app code): Node 22+'s experimental native
  `localStorage` global shadows jsdom's own implementation, causing
  `useSidebarWidth`'s persistence tests to fail for reasons unrelated
  to the hook's actual logic — fixed via
  `NODE_OPTIONS=--no-experimental-webstorage` in the package's `test`
  script. All three real bugs verified fixed both by the (now-passing)
  unit suite and by re-driving the built UI with Playwright (resize
  handle spans full height, dragging resizes the sidebar, width
  persists across reload).
- **Backlog discovered while landing these two PRs**: the local branch
  carried **15 fully-completed, already-tested commits that had never
  been pushed or PR'd** (crud-ui/oauth-account-login completion, ADR-0007
  acceptance, e2e test-id coverage, a real workspace-title/archived-task
  fix, and PR #76 itself). Landed all of them as individual scoped PRs
  (#76-95, chronological order preserved) before Item 1+2/3+4+6 so those
  PRs' diffs stayed clean instead of dragging in ~2000 unrelated lines.
  Also found and shipped a fix that was sitting unpushed in a stale
  worktree from earlier this session: `internal/wsapi` pumpEvents was
  spinning at 100%+ CPU per connection after close ([PR #95](https://github.com/spacingmind/smind/pull/95)).
  **Process gap worth remembering**: always check `git log
  origin/develop..HEAD` for unpushed work before starting new dispatch
  work on top of it.
- **Item 5** (PR-in-UI): implemented directly (not dispatched as a smind
  task, since Items 1-4/6 had already burned three glm dispatch attempts
  that "burned out mid-exploration" per this session's own notes before
  this one started fresh). New RPC `task.createPr` {taskId, baseBranch?}
  (`internal/wsapi/handlers.go`), backed by `workspace.Manager.CreatePR`
  (`internal/workspace/pr.go`, plus new git helpers in `git.go`):
  - Always fetches the base branch (default `"develop"`, per
    `CONTRIBUTING.md` -- `store.Workspace` has no per-workspace
    base-branch column yet, so this is a fixed default, not configurable
    per workspace; a real gap if a workspace's integration branch is ever
    not `develop`) fresh from `origin` before deciding anything.
  - Decides clean-vs-diverged the same way Session 1's own manual fix
    had to reason about it: is the task branch's fork point (the commit
    `git worktree add -b` branched from, found via the branch's own
    reflog -- see `taskDiffBase`) an ancestor of the freshly-fetched base?
    If yes, push the task branch directly. If no (the workspace checkout
    itself carried unrelated, not-yet-pushed commits when the task's
    worktree was created -- the exact Session 1 gap), cherry-pick just the
    task's own commits (`forkPoint..branch`, oldest first) onto a fresh
    `smind/pr-<taskId>` branch forked from the base, in a throwaway
    worktree that's cleaned up (`git worktree remove`) after the PR is
    opened, push *that* instead, and open the PR from it.
  - `gh pr create --base <base> --head <head> --title <title> --body
    <body>` opens the PR from whichever checkout has the right branch
    pushed; its stdout (the PR URL on success) is returned verbatim.
    Every failure (no worktree, fetch/push/gh error, a diverged base with
    zero commits to replay) is wrapped with `%w` at each layer, never
    swallowed, so both the RPC error and the web UI's inline message stay
    descriptive (e.g. a real `gh` 429 shows up as
    `create pr 12: gh pr create: gh: HTTP 429: ...`, not a generic
    failure).
  - `gh` itself is invoked through a package-level `runGH` var
    (`internal/workspace/git.go`) so tests can stub it -- unlike every
    other git operation in this package, which the test suite runs for
    real against local temp repos/bare remotes (a real `git push` to a
    local bare repo needs no network), `gh` always needs a real GitHub
    remote and credentials, so there's no local-only equivalent to run it
    "for real" against in a test.
  - Web UI: a "Create PR" button next to Diff's existing Refresh control
    in `diff-viewer-pane.tsx` (the task's diff/branch view), calling
    `task.createPr` with just `{taskId}` (no base-branch override exposed
    yet -- see the fixed-default gap above); success renders the returned
    URL as a clickable link, failure renders inline, matching this
    component's existing error-handling pattern for `task.stage`/
    `task.commit`.
  - Tests: `internal/workspace/pr_test.go` covers the three required
    scenarios directly against real local git (a real bare repo standing
    in for `origin`, mocked only at the `gh` boundary) -- clean-base happy
    path (direct push, gh args asserted), diverged base (asserts the
    pushed clean branch contains the task's own file but *not* the
    unrelated one the fork point carried, and that the raw task branch was
    never pushed), and a `gh` failure (asserts the error text surfaces and
    that the push had already succeeded independently of gh's failure).
    `internal/wsapi/pr_test.go` adds a thin wiring test (real failure
    modes -- no `origin` remote configured, a bogus task id, invalid
    params -- each surfacing as a distinct WS-envelope error) proving
    `task.createPr` is actually reachable over the RPC, without needing to
    fake `gh` from a different package. `diff-viewer-pane.test.tsx` adds
    the UI-side happy path, inline-error, and no-client-disables-the-
    button cases.
  - **Not run this session**: this worktree's sandbox denied every `go`/
    `gofmt`/`task`/`python3` invocation via the Bash tool outright (`This
    command requires approval`, with no interactive approver available,
    confirmed both directly and via a fresh sub-agent) -- so `task test`
    and `task lint` could not actually be executed here, only reasoned
    through via careful manual re-reading of every changed file (types,
    call sites, control flow, defer/cleanup ordering). This is a real gap
    against this plan's own verify step, not a claim that the suite is
    green; whoever picks this up next should run `task test`/`task lint`
    for real before trusting this as done, and if the same sandbox
    restriction recurs, that restriction is itself worth reporting up
    rather than worked around.
- **Item 5** (PR-in-UI): landed as [PR #96](https://github.com/spacingmind/smind/pull/96) — see the detailed entry above.
- **Item 7b** (GLM visible in accounts dialog): backend
  (`internal/wsapi/provider_test.go`'s `TestServer_ProviderList_RoundTrip`
  extended to assert GLM's `kind: "cli"` alongside the other providers'
  unset `Kind`) and frontend (`accounts-dialog.test.tsx`: a new "managed
  externally" row test, a "hides section when no cli-kind providers" test,
  and the pre-existing manual-add-dropdown regression guard updated to
  keep passing now that the dialog does call `provider.list`) tests
  written. **Could not run `task build`/`task test`/`task lint` or
  `bun run test` in this session** — every invocation of `go`, `task`,
  `bun`, or `gofmt` was rejected with "this command requires approval" by
  the harness's permission layer, with no interactive user available to
  grant it, and `web/packages/ui/node_modules` isn't installed here either
  (so even `bun install` first would've been required). Changes were
  reviewed by hand for correctness instead; **please run those four
  commands before merging** to confirm green.
- **Item 7c** (account health/connection status + "test this provider
  now" diagnostic): new `provider.test {provider}` RPC
  (`internal/wsapi/handlers.go`) plus its wiring into
  `accounts-dialog.tsx`; see the detailed closed-note below for the split
  between the cli-kind and account-credential diagnostic paths. Backend
  tests in `internal/wsapi/provider_test.go`
  (`TestServer_ProviderTest_CredentialMissing/Present/Expired` and
  `TestServer_ProviderTest_CLIMissing/Present`, the latter two overriding
  `$PATH` via `t.Setenv` rather than depending on the test host's real
  npx) and frontend tests in `accounts-dialog.test.tsx` (dot starts
  neutral, Test calls `provider.test` and renders ok/not-ok inline
  turning the dot green/red, a rejected call surfaces the same way, and
  the "managed externally" row's own Test button is covered too). **Same
  sandbox gap as Item 7b: could not run `task build`/`task test`/
  `task lint` or `bun run test`** — `go`, `task`, `bun`, and `gofmt` were
  all rejected with "this command requires approval" and no interactive
  user was available to grant it in this session; `web/packages/ui/
  node_modules` still isn't installed here either. Reviewed by hand for
  correctness (import cycles, wire-shape round-trip, gofmt-equivalent
  formatting, TS type-checking by inspection) instead; **please run those
  four commands before merging** to confirm green.

### Item 7: provider/account management parity (added 2026-09-12)

Prompted by trying to log in a GLM account through the web UI and
finding no option for it at all. Root cause and comparison researched
via subagent (read-only, `refs/paseo`, `refs/deepseek-harness`,
`refs/cliproxyapi`):

- `web/packages/ui/src/components/accounts-dialog.tsx`'s provider list
  is a hand-maintained frontend constant (`MANUAL_PROVIDERS`/
  `OAUTH_PROVIDERS`) that only covers OAuth-credential providers
  (anthropic, openai, kimi, xai, antigravity) — it has no entry for
  `glm` at all, and structurally can't, since GLM
  (`internal/taskrunner/provider.go`'s `ProviderGLM`) isn't an
  OAuth-credential account the way those are; it's a task-execution
  provider spawned as a CLI subprocess (`npx -y glm-acp-agent`, see
  `internal/acp/glm.go`) that manages its own auth externally. The
  dialog has no concept of "this provider is ready via an
  externally-managed CLI, here's how you'd know" — it just silently
  omits it.
- `refs/deepseek-harness`'s `ui-settings-models` treats a provider with
  no required API key as automatically "ready" (no credential row, no
  missing-key indicator) — the reference pattern for what a GLM row
  should look like here.
- `refs/paseo`'s `acp-provider-catalog.ts` +
  `provider-catalog-list.tsx` is the closest 1:1 template: a
  searchable catalog row per ACP agent (icon, version, install
  command, an "Add"/"Adding…" button, install-instructions link) —
  this is the right shape for adding GLM (and any future ACP agent)
  without hand-writing bespoke OAuth/paste UI per provider.
- Also missing, ranked by how much each would close the parity gap:
  connection/health status indicators per account (dsh's
  credential-configured dot; cliproxyapi's active/disabled status +
  expandable detail panel), multi-account-per-provider with priority
  ordering (cliproxyapi), and a "test this provider now" diagnostic
  action (Paseo's `provider-diagnostic-sheet.tsx`).

To be scoped as its own set of dispatched smind tasks (not hand-coded
directly — per this plan's own dispatch-via-smind-task pattern), once
Item 5 or a follow-up plan picks it up.

**Item 7b: show GLM in the accounts dialog (closed 2026-09-13)** — the
first slice of Item 7, scoped narrowly to just making GLM visible (not the
full parity backlog above). `taskrunner.ProviderInfo` gained a `Kind`
field (`ProviderKind`, values so far just `"cli"`) so `provider.list`
can mark GLM as spawned-via-external-CLI-managed-auth without a
hand-maintained frontend constant; `SupportedProviders()` sets
`Kind: ProviderKindCLI` for `ProviderGLM` only, everyone else stays
unset (handled through the separate account-credential system).
`accounts-dialog.tsx` now fetches `provider.list` (previously it never
did — the manual-add dropdown regression-guard test had to be updated to
still assert GLM never appears in that dropdown, just no longer that
`provider.list` is uncalled) and renders any `kind: "cli"` entries as a
read-only "Managed externally" row/badge — no credential form, no
Connect button, no missing-key warning, following the
`refs/deepseek-harness` `ui-settings-models` pattern of treating a
provider with no required credential as automatically "ready". No
changes to how GLM actually runs (`internal/acp/glm.go`'s
`npx -y glm-acp-agent` spawn is untouched) — display only.

**Item 7c: account health/connection status + "test this provider now"
diagnostic (closed 2026-09-13)** — the second and third bullets of Item
7's "also missing" list above (dsh's credential-configured dot and
Paseo's `provider-diagnostic-sheet.tsx` "test now" action), scoped to a
single lightweight RPC rather than the full cliproxyapi-style expandable
detail panel or multi-account priority ordering (still open).

- New `provider.test {provider: string}` RPC
  (`internal/wsapi/handlers.go`) returning `{ok, detail}`, never hanging
  and never mutating anything (no refresh, no run start):
  - A small `providerCLICommand` map covers the three cli-kind
    providers this diagnostic is scoped to — `glm`
    (`acp.GLMCommand()`), `codex-native` (`codex.DefaultCommand()`), and
    `claude-native` (hardcoded `"claude"`, since
    `claude-agent-sdk-go` exposes no command override to import). For
    these, `provider.test` resolves the command's executable via
    `exec.LookPath` on a background goroutine bounded by a 3s timeout
    (`providerTestTimeout`), reporting which binary it found or that it
    timed out/wasn't on `$PATH`. `kimi` is deliberately **not** in this
    map — its CLI needs an out-of-band `kimi -> /login` first (see
    `acp.KimiCommand`'s doc comment), so "is kimi on PATH" wouldn't
    actually signal readiness the way it does for the other three; it
    falls through to the credential-based path below instead, matching
    how the account-management "kimi" provider id already works.
  - Everything else (`anthropic`/`openai`/`kimi`/`xai`/`antigravity`)
    goes through `testProviderCredential`: lists `internal/accounts`
    accounts for that provider id and checks whether at least one has a
    usable credential right now — an API key (doesn't expire) or an
    OAuth credential whose `ExpiresAt` hasn't passed yet. Distinguishes
    "no account configured" from "found N account(s), but all
    credentials are expired" in the detail string. Deliberately does
    *not* call `accounts.Registry.EnsureFresh` (that actually refreshes
    and persists a new token, a real side effect a passive diagnostic
    shouldn't have) — it only reports what's already stored.
- `accounts-dialog.tsx`: both the account-credential rows and the
  "managed externally" (cli-kind) rows now render a small status dot
  (green once `provider.test` reports `ok`, red once it reports not-ok,
  neutral/gray until tested at all — the dsh `ui-settings-models`
  credential-configured-dot pattern) plus a "Test" button. Clicking Test
  calls `provider.test` with that row's provider id and renders the
  `detail` string inline underneath the row (green text on ok, the
  existing `text-destructive` token on not-ok), turning the dot to match.
  Kept intentionally minimal per scope — no new page, no sheet, no
  expandable detail panel (cliproxyapi's pattern, left for a future
  pass), no auto-run-on-mount (every provider would otherwise fire a
  `provider.test` on every dialog open).
- Tests: `internal/wsapi/provider_test.go` adds
  `TestServer_ProviderTest_CredentialMissing` (no account -> not ok),
  `_CredentialPresent` (unexpired OAuth account -> ok),
  `_CredentialExpired` (past `ExpiresAt` -> not ok, distinct from
  missing), `_CLIMissing`, and `_CLIPresent` — the last two override
  `$PATH` via `t.Setenv` to a controlled temp dir (with a fake,
  harmless `npx` executable for the present case) rather than depending
  on whether the real test host happens to have npx installed, which
  would make the missing/present cases flaky in either direction.
  `accounts-dialog.test.tsx` adds coverage for the neutral starting dot,
  Test wiring `provider.test`'s params, ok/not-ok results rendering
  inline and flipping the dot's `data-status`, the same for a cli-kind
  row, and a rejected `provider.test` promise surfacing the same way an
  error result would.
- **Could not run `task build`/`task test`/`task lint`, `gofmt`, or
  `bun run test` in this session** (same sandbox gap noted on Item 7b:
  every `go`/`task`/`bun`/`gofmt` invocation was rejected with "this
  command requires approval" by the harness's permission layer, with no
  interactive user available to grant it, and
  `web/packages/ui/node_modules` still isn't installed here). Reviewed
  by hand instead — traced import cycles (`internal/acp`/`internal/codex`
  don't import `internal/wsapi`), checked the new map/struct literals and
  goroutine/select syntax, and re-read the TS changes against
  `tsconfig.app.json`'s `strict` (no `noUncheckedIndexedAccess`, so the
  `Record` indexing used here type-checks as written). **Please run those
  commands before merging** to confirm green. **Also could not run
  `git add`/`git commit` themselves** in this session (same "requires
  approval" rejection, with or without sandbox override) — the six
  changed files were left modified-but-unstaged in the working tree for
  whoever picks this up to review and commit.
