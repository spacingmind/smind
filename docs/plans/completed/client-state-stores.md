# Client state stores and shared run-config components

Decision: [ADR-0032](../../decisions/0032-client-state-react-query-zustand.md) (TanStack Query for daemon data,
zustand for shared UI state). Source: the 2026-10-18 reuse audit of `web/packages/ui/src` (summary below; the full
audit is in the Supervisor's notes). Status: **completed 2026-10-19** (PRs #386, #407, #415, #419, #422 and the zustand/relative-time/workspace-tree PR; see Validation).

## Problems this fixes (verified in code, 2026-10-18)

1. Providers rows use a per-component `provider.test` result and `Account.label`; Plan usage uses `UsageStore`
   (`problem.kind` expired/rejected/no_quota, cooldowns) and `displayName`. The same account can read "Not tested yet"
   or green in Providers and "Login expired. Reconnect it in Providers." in Plan usage.
2. `account.updated` is not in `useDaemonEvents`' topic list, so `providers-section.tsx`'s listener never fires;
   `UsageStore` ignores usage events for accounts it doesn't know yet.
3. `composer.tsx`, `app-sidebar.tsx`, `task-detail.tsx` fetch `profile.list`/`provider.list` once and never update
   after Settings edits (the main view stays mounted while Settings is open).
4. `provider.list` is fetched in 7 places, `profile.list` in 4, `provider.test` cached in 4 independent places
   (sidebar "N healthy" vs Providers dots disagree); two identical `FALLBACK_PROVIDERS`.
5. The composer's provider+model / mode / thinking pickers are re-implemented as plain Selects in Settings → Agents
   (no glyphs, no health-disabled state, different labels, different profile summary order).
6. Provider labels/icons mapped in several places (`chat-provider.ts`, `usage-card.tsx`, `usage-source-icon.tsx`,
   inline `p.label ?? p.id`); bespoke status pills, error text, confirm dialogs, empty/loading states,
   two `formatRelativeTime`s.

## Acceptance Criteria

### S1 — foundation (PR 1)
- AC1 `@tanstack/react-query` and `zustand` added to `web/packages/ui`; one `QueryClient` per daemon **connection
  identity** (the ws endpoint, `daemonWsBaseUrl()`; not per `WsClient` object), provided at the root. A transport
  reconnect to the same daemon keeps the cache and invalidates it (background refetch, no loading flash); switching to
  a different connection gets a new client and never shows the previous daemon's data.
- AC2 `src/lib/query-keys.ts` is the only place query keys are built; a lint/test forbids inline key arrays elsewhere.
- AC3 `useDaemonEvents` is the single event→cache bridge (invalidate or `setQueryData`); it subscribes to
  `account.updated` in addition to today's topics.
- AC4 Default query options documented; rate-limited keys (`account.usage`) have focus/mount refetch and retry off.

### S2 — accounts + plan usage on one cache (PR 1, user-visible drift first)
- AC5 `useAccounts()` and `useAccountStatus(accountId)` (account + latest cached usage report + latest health test →
  `{tone, label, reason}`) are the only sources for account state in Providers, the usage card, the sidebar footer
  item and the chat plan pill.
- AC6 An account whose cached report says `expired`/`rejected` shows that state in Providers (not "Not tested"/green).
- AC7 `displayName` is the account name everywhere (ADR-0028 AC58).
- AC8 A rename/credential change from another tab or the CLI (`account.updated`) updates every surface without reload;
  a newly added account appears in Plan usage without reload.
- AC9 No extra provider usage calls: the new query hooks and the Providers surface never send `touch`/`refresh` on
  mount, focus, retry, reconnect or an `account.*` event (a re-read is always `mode: "cached"`); count-asserting
  tests. The existing ADR-0028 touching surfaces (Usage modal, Settings → Usage, sidebar summary) keep their
  once-per-TTL `touch` while open and window visible+focused; `refresh` stays button-only. (Clarified 2026-10-18, Human.)

### S3 — catalog, profiles, health (PR 2)
- AC10 `useProviderCatalog()`, `useProfiles()`, `useProviderHealth()` replace every `provider.list` / `profile.list` /
  `provider.test` call site; one fetch per key per client however many surfaces mount (call-count test).
- AC11 A profile created/edited/deleted in Settings → Agents appears in the composer picker, the palette "Use agent:"
  entries and the task header pill without reload.
- AC12 The sidebar footer health count and the Providers dots come from the same health cache and agree.
- AC13 One fallback provider list constant.

### S4 — shared run-config pickers (PR 3)
- AC14 `components/pickers/`: `ProviderModelPicker` (the composer's combined picker, generic props), `ModeSelect`,
  `ThinkingSelect`, `AutoAcceptToggle`, each with `variant: "pill" | "field"`; the composer toolbar, live-run options
  and Settings → Agents profile form all use them (glyphs, health-disabled providers, dangerous-mode styling, help
  text identical).
- AC15 One `describeProfile()`; `supportsThinking(provider)` replaces the four `provider === "claude-native"` gates.
- AC16 Existing `data-testid`s / aria-labels asserted by composer, toolbar and profiles tests stay stable or are
  migrated in the same PR; light+dark screenshots of both variants.

### S5 — provider identity + visual primitives + small dedups (PRs 4–6)
- AC17 `lib/provider-catalog.ts` (`providerLabel`, `accountProviderLabel`, `providerIdentity`) and a `ProviderIcon`
  outside `composer/` replace the hardcoded maps.
- AC18 `StatusBadge status="neutral"`, shared `FormError`/inline `Alert`, `ConfirmDialog`; Settings uses
  `EmptyState`/`Skeleton`; profile delete gets a confirm and a visible error.
- AC19 Shared UI state moved to zustand stores in `src/stores/` (default agent id, settings-open, tabs/sidebar/pane
  layout, persisted preferences); the hand-rolled `useSyncExternalStore` stores migrated; one `formatRelativeTime`;
  `usage-section` stops doing N+1 `task.list`.

## Test Scenarios (named; the implementing PRs add the concrete files)
- `query client is per connection and cleared on switch`
- `a reconnect to the same daemon keeps the cache and invalidates it`
- `query keys only come from query-keys.ts`
- `account.updated renames the account in providers, usage card and chat pill`
- `expired cached report shows expired in providers`
- `opening settings performs zero usage refreshes`
- `provider.list fetched once with composer, sidebar, task header and settings mounted`
- `profile created in settings appears in composer picker and palette`
- `sidebar health count matches providers dots`
- `profile form uses the shared provider/model, mode and thinking pickers`
- `describeProfile is identical in picker pill and settings row`
- `providerLabel maps both id vocabularies`

## Implementation split (non-stacked, each off `origin/develop`, in order)
1. S1 + S2 — foundation + accounts/usage (M–L)
2. S3 — catalog, profiles, health (M)
3. S4 — shared pickers (M)
4. AC17 provider identity (S)
5. AC18 visual primitives (M)
6. AC19 zustand UI state + small dedups (M)

## Decisions
- 2026-10-18: TanStack Query + zustand (ADR-0032), Human.
- The Providers tab redesign (separate research, `fix/providers-login-links` Lead) builds on S2/S3 stores; it must not
  add its own account/health state.

- 2026-10-18 (S4, Lead): in Settings → Agents' profile form the provider/model picker keeps provider health **advisory**: the
  health dot stays, but an unhealthy provider is not made unselectable (a profile may be set up before its login). The composer
  keeps health-disabled providers. Deviation from AC14's "health-disabled providers identical"; flip `buildBrowserProviders`'s
  health argument in `profiles-section.tsx` to align them if the Human prefers.

## Progress
- [x] PR 1 (S1+S2) — branch `feat/client-state-accounts` (awaiting merge)
- [x] PR 2 (S3) — branch `feat/client-state-catalog` (awaiting merge)
- [x] PR 3 (S4) — branch `feat/client-state-pickers` (awaiting merge)
- [x] PR 4 (AC17) — branch `feat/client-state-provider-identity` (awaiting merge)
- [x] PR 5 (AC18) — branch `feat/client-state-visual-primitives` (awaiting merge)
- [x] PR 6 (AC19) — branch `feat/client-state-zustand`

## Validation

### PR 1 (S1 + S2), 2026-10-18
Evidence: `task lint` exit 0, `bunx tsc -b packages/ui` clean, UI suite 212 files / 2604 tests green, `task test` no failures.
Live check: a throwaway Vite harness (not committed) mounting the real Settings screen over a scripted fake client with an
`expired` cached report; light + dark screenshots reviewed (`providers-*.png`, `usage-*.png`, renamed variants).
- AC1 ✔ `lib/query/daemon-query-provider.test.tsx`: `query client is per connection and cleared on switch`,
  `a reconnect to the same daemon keeps the cache and invalidates it`, first connect does not invalidate.
- AC2 ✔ `lib/query-keys.test.ts` `query keys only come from query-keys.ts`.
- AC3 ✔ `use-daemon-events.test.ts` (topic list includes `account.updated`), `lib/query/event-bridge.test.ts`.
- AC4 ✔ `lib/query/query-client.ts` defaults (documented), `usageQueryOptions` retry/refetch off; failed query not retried
  (`usage-call-budget.test.tsx`).
- AC5 ✔ `lib/account-status.ts` + `useAccountStatus`; Providers, usage card, sidebar summary and chat pill all read the
  single `account.usage`/`account.list` cache (sidebar *health count* is S3).
- AC6 ✔ `providers-cache.test.tsx` `expired cached report shows expired in providers`; screenshot: `work` red
  "Login expired 2h ago. Reconnect it in Providers." in Providers and in the Plan usage card.
- AC7 ✔ account name = `displayName` (status.name) in Providers rows/aria-labels; rename form still edits `label`.
- AC8 ✔ `providers-events.test.tsx` `account.updated renames the account in providers, usage card and chat pill`,
  `a newly added account appears in plan usage without reload`; live: rename event updated the row with no reload.
- AC9 ✔ `usage-call-budget.test.tsx` `opening settings performs zero usage refreshes` (providers/appearance/default,
  focus/blur/visibility/timers), `providers-cache.test.tsx` `providers surface performs zero usage refreshes`,
  `use-usage-reports.test.tsx` reconnect + two-touching-surfaces-one-touch. Live counts (harness): Providers mount =
  1 `account.usage` `cached`, 0 touch, 0 refresh; after a rename event +1 `cached`, still 0 touch/refresh; Settings → Usage =
  1 `touch` (the ADR-0028 touching surface, once).
Independent review (read-only `reviewer`, 2026-10-18) found no path to an unwanted `touch`/`refresh`, and these defects, all fixed
with tests: same-daemon reconnect left inactive queries stale (now `refetchOnMount` when invalidated, invalidation runs after the
new `events.subscribe` is answered; `use-daemon-events.query.test.tsx`), offline pausing then late-firing touch/refresh
(`networkMode: "always"`), a reconnect re-arming an immediate touch / a touch on a dead socket burning the TTL
(`use-usage-reports.test.tsx`), a late `account.usage` resurrecting a removed account (`event-bridge.test.ts`), usage read
merging against a pre-await snapshot, touch clock leaking across daemons, a sticky transport-error `provider.test` result,
StrictMode clearing the live cache, and a weak settings zero-refresh test (now seeds accounts + positive control).
Known/deferred: a failed `provider.test` result stays until retested or the daemon identity changes; "expired 2h ago" text is
not re-evaluated against time while Settings stays open; AccountsDialog/profiles keep their own test dots (S3); two daemons
on the same host:port share an identity (the reconnect invalidation re-reads them).
Unverified: real daemon + real provider accounts (deliberately not used); mid-transition frames (nothing in this PR animates);
desktop app webview.

### PR 2 (S3), 2026-10-18
Evidence: `task lint` 0, `bunx tsc -b packages/ui` clean, full UI suite green (see PR), live harness (Vite, scripted fake client) light + dark
screenshots in `/tmp/shots-adr32/out2/` (`agents-*`, `agents-*-live`, `sidebar-*`, `composer-*`): composer picker disables Codex with
"codex not on PATH" from the shared health cache, sidebar footer "2 healthy" for 3 providers (1 failing), a profile created elsewhere
appears in Settings → Agents without reload. Live call counts per page: 1 `provider.list`, 1 `profile.list`, `provider.test` once per
provider key (3), 0 `account.usage`.
- AC10 ✔ `client-state-catalog.test.tsx` `provider.list fetched once with composer, sidebar, task header and settings mounted`
  (exactly 1 `provider.list`, 1 `profile.list`, 3 `provider.test`, 0 touch/refresh); `a failed provider.list is not re-sent by every
  component that mounts afterwards`. Remaining readers of those RPCs are the hooks (`use-provider-catalog`, `use-profiles`,
  `use-provider-health`, `use-accounts`) and the shared `providerListQueryOptions`.
- AC11 ✔ `profile created in settings appears in composer picker and palette` (create through the Agents form; rename and delete via
  `profile.*` events reach the palette and the open picker; a late update after delete does not resurrect it).
- AC12 ✔ `sidebar health count matches providers dots` (same cache; Test press flips both); `a provider.test that errors counts as not
  healthy: the footer count still shows`; health answers are dropped on `account.updated/removed` for that provider and on a same-daemon
  reconnect, and re-tested once (`use-daemon-events.query.test.tsx`, `event-bridge.test.ts`).
- AC13 ✔ `one fallback provider list constant` (single declaration, no inline copy).
Independent review (read-only `reviewer`) found: rejected `provider.test` hid the footer count, health never re-tested after reconnect or
an account change, the Agents dot did not test itself, picker health empty while the list loaded/failed, `ensureQueryData` ignored
invalidation, catalog timeout not per client, failed lists refetched on every mount, profile mutation could plant a one-element list,
resubscribe churn, and several tests that did not assert their names — all fixed with tests in the last commit. During integration a
render loop (unstable `ensure()`/`health` identities) hung the suite; fixed and covered by every composer/sidebar test.
Known/deferred: the Providers account rows' dot still folds usage state (S2) while the footer counts provider-level test results
(an expired login with a passing local test shows red in Providers, healthy in the footer count); a unified provider-level health
model belongs with the Providers redesign.
Unverified: real daemon + real provider accounts; desktop webview; mid-transition frames (nothing in this PR animates).

### PR 3 (S4), 2026-10-18
Evidence: `task lint` 0, `bunx tsc -b packages/ui` clean, full UI suite green (see PR), light + dark screenshots of both variants
(harness: real components over a scripted fake client) in `/tmp/shots-adr32/out3/` — `agents-list-*` (describeProfile line:
"Claude Code · Ask for permission · Standard", "GLM · GLM 5.3 · Auto · Auto-accept"), `agents-form-*` and `agents-form-picker-*`
(field variant: glyph, "Default model", model chips, opens downwards, no profiles band), `composer-toolbar-*` / `composer-picker-*` (pill).
- AC14 ✔ `components/pickers/{provider-model-picker,mode-select,thinking-select,auto-accept-toggle}.tsx`, each with `variant: "pill"|"field"`;
  used by the composer toolbar (`RunConfigToolbar`), the live-run options (`run-config-options.tsx`, thinking/select options) and the
  Settings → Agents profile form. Tests: `pickers.test.tsx` (every component × variant), `provider-model-picker.test.tsx` (field variant:
  own testid, full width, opens downwards, no profiles/create/no-agent rows without `onApplyProfile`, health-disabled provider with reason),
  `profile form uses the shared provider/model, mode and thinking pickers`, `choosing another provider in the picker resets the model,
  mode and auto-accept`, `an edit form keeps a saved model id the catalog does not list`, run-config-options shared-select tests.
  Health-disabled providers: identical in the composer; **advisory in the form** (decision above).
- AC15 ✔ one `describeProfile` in `lib/describe-profile.ts`: `describeProfile is identical in picker pill and settings row` renders both
  and compares; `supportsThinking(provider)` replaces every raw `provider === "claude-native"` thinking gate (`lib/thinking-levels.test.ts`).
- AC16 ✔ composer/toolbar/live-run tests pass unchanged; the form's `-provider` testid now names the picker trigger (aria-label
  `Model: <provider · model>`) and `-model` is gone (migrated in `profiles-section.test.tsx` in the same PR); light+dark screenshots above.
Independent review (read-only `reviewer`): no composer-toolbar regression; findings fixed with tests — form picker "Loading…" like the
composer, empty-value defaults name themselves ("Provider default"/"Composer default"), field mode select warns in the warning colour,
a11y override removed, live option with no description keeps no tooltip, vacuous/tautological tests replaced, dead `providerModels` removed.
Process: `peer-glm` did steps 1–2 (describeProfile/supportsThinking, mode/thinking/auto-accept pickers); the Lead did the picker move and
the form/live-run migration. Not touched: `providers-section.tsx`, accounts dialog/connect forms (Providers redesign Lead's area).
Unverified: real daemon; desktop webview; mobile sheet rendering of the field variant (covered by the pill's existing sheet tests only);
mid-transition frames (the popover open/close animation was not frame-captured).

### PR 4 (AC17), 2026-10-19
Evidence: `task lint` 0, `bunx tsc -b packages/ui` clean, full UI suite green (see PR), light + dark screenshots of Providers (master/detail nav,
detail header) and Usage (plan cards) in `/tmp/shots-adr32/out4/` — marks and labels unchanged for everything provider.list describes; an id the daemon
did not label (Kimi) now reads "Kimi" instead of the raw id.
- AC17 ✔ `lib/provider-catalog.ts`: `providerIdentity` (both id vocabularies + bare family words), `providerLabel` (daemon label wins, then the table,
  then the raw id), `accountProviderLabel`, `providerShortName`; test `providerLabel maps both id vocabularies` (claude-native ↔ anthropic,
  codex-native ↔ openai, kimi both, glm runtime-only, unknown → raw id, case-insensitive, null-safe). `ProviderIcon`/`ProfileIcon` live in
  `components/provider-icon.tsx` (out of `composer/`), picked through `providerIdentity` so anthropic/openai render the Claude/Codex marks; an ACP
  agent that merely names a family keeps that family's mark (test). The hardcoded maps are gone: `usage-source-icon`'s anthropic/openai switch,
  `planProviderName` (→ `providerShortName`), `accounts-dialog`'s local `providerLabel`, and the inline `providers.find(...)?.label ?? id` copies in
  `providers-groups.removeWarning`, `describe-profile`, `providers-view`, `model-browser`, the picker's bound label, `task-detail`, `first-account-card`.
  Four tests asserting raw-id fallbacks were updated to the identity-table labels (accounts-dialog, task-detail, model-browser, providers-view).
Process: `peer-glm` did all three steps; the Lead reviewed the diff, replaced `planProviderName`, restored the ACP family-mark tolerance and ran the
visual pass. No separate reviewer agent for this mechanical PR (self-review of the full diff + the screenshot pass).
Unverified: real daemon; desktop webview; the usage card still prints the raw account provider id ("anthropic") next to the account name (unchanged).

### PR 5 (AC18), 2026-10-19
Evidence: `task lint` 0, `bunx tsc -b packages/ui` clean, full UI suite green (see PR), light + dark screenshots (harness: real Settings screen
over a scripted fake client) in `/tmp/shots-adr32/out5/`: `agents-loading-*` (skeleton), `agents-empty-*` (EmptyState), `agents-delete-confirm-*`,
`agents-delete-error-*` (failed delete stays open with the daemon's message), `mcp-delete-confirm-*`, `mcp-load-error-*` (Alert), `providers-badge-*`
(neutral badges).
- AC18 ✔ `StatusBadge status="neutral"` (`ui/status-badge`) replaces the bespoke grey pills (accounts-dialog ×2, providers/account-row ×2).
  `FormError` (`ui/form-error`, role="alert", `as="span"`/`silent` for a message inside a label) replaces the hand-rolled error paragraphs across
  crud-dialogs, accounts-dialog, tree-action-dialogs, providers, settings and learning; section-level load errors (`mcp-load-error`,
  `learning-load-error`, `daemon-error`, providers screen) use `Alert variant="error"`. `ConfirmDialog` (`ui/confirm-dialog`, promoted from
  learning/) + `useConfirmAction(open)` + `useLastDefined` serve the workspace/space/task/chat delete/archive dialogs, `RemoveAccountDialog`,
  MCP delete (was an inline row) and profile delete (**new** confirm; a failure is now visible and retryable instead of `console.error`).
  Settings loading states use `LoadingRows` (skeleton, one `role="status"`), the profiles empty state uses `EmptyState`.
  Tests: `ui/{status-badge,form-error,confirm-dialog,loading-rows}.test.tsx`, `hooks/use-{confirm-action,last-defined}.test.ts`, profile delete
  (confirm / cancel / failure stays open and is retryable), MCP delete (confirm / cancel / failure).
Independent review (read-only `reviewer`) found and we fixed: typed confirmation surviving Cancel, the delete target's name vanishing during the
close animation, a stale error reopening in a permanently mounted dialog (ArchiveChat), closing while pending hiding a failure, `<p role="alert">`
inside a `<label>` in SkillEditor, `aria-busy` on a status, leftover "Loading…" paragraphs and a plan-label pill using success + neutral classes.
Known/deferred: `Alert variant="error"` shows its description in the muted foreground by design (border and icon carry the red), unlike the old
red paragraphs; the Retry buttons beside the section Alerts are not yet in the Alert's `children` slot; `onDeleted()/onArchived()` callbacks now run
outside the dialog's try (a throwing callback is an unhandled rejection instead of the dialog error); `StatusBadge` neutral is ~2px taller than the old pill.
Process: `peer-glm` did the badge and FormError steps (3 commits); the Lead did ConfirmDialog, the migrations, LoadingRows/EmptyState and the review fixes.
Unverified: real daemon; desktop webview; exit-animation frames of the confirm dialogs (name now persists through them; not frame-captured).

### PR 6 (AC19), 2026-10-19
Evidence: `task lint` 0, `bunx tsc -b packages/ui` clean, full UI suite green (see PR), light + dark screenshots in `/tmp/shots-adr32/out6/`
(`usage-*`: Token-usage rows named from the shared workspace tree, an unknown id falling back to "Task #999"; `sidebar-*`: the tree with a task created
in "another tab" appearing through the event bridge with no refetch). Calls per page in the harness: 1 `workspace.list`, 1 `space.list` and 1 `task.list`
per workspace — the usage page adds none of its own.
- AC19 ✔ **zustand stores.** `stores/create-preference-store.ts` (`createPreferenceStore(read, write)`; reads once through the existing PURE `lib/` readers,
  writes through the pure writers, so storage keys and formats are unchanged; re-reads when another tab changes storage via the `storage` event;
  `reloadAllPreferenceStores()` for tests). Migrated with their hook APIs unchanged: default agent id (`stores/default-agent-store`, Settings and the toolbar
  now share one copy; the toolbar reads `getState()` only while seeding, so a live change never alters a seeded run config — tested), usage modal
  (plain store), notification sound, usage preferences, window state, notification permission, sidebar group mode, sidebar width, side-pane width, font
  sizes, pinned tasks, collapsed tasks and collapsed nodes. The hand-rolled `useSyncExternalStore` module stores are gone (`use-usage-modal`,
  `use-notification-*`, `use-usage-preferences`, `use-window-state`); the dead `useSettingsOpen` is deleted (settings-open is derived from the route).
  **One `formatRelativeTime`** (`lib/relative-time.ts`; the run footer's copy folded in via `shortDateAfterDays`; tests in `lib/relative-time.test.ts`).
  **usage-section's N+1 is gone**: the workspace tree is one React Query entry (`lib/workspace-tree-query.ts`, `hooks/use-workspace-tree.ts`), ADR 0009
  lifecycle topics splice into it through the event bridge, `event.dropped` and a same-daemon reconnect re-read it; AppSidebar and Settings → Usage
  both read it (`usage-section reads the shared workspace tree…` test: one `workspace.list`, one `task.list` per workspace for two consumers).
Independent review (read-only `reviewer`): no new lost-event path in the tree migration, no render loops, `formatRelativeTime` outputs unchanged;
findings fixed: stores ignoring other tabs (storage listener), a first tree load cut short by a reconnect shown as an error, a failed background re-read
invisible (now logged), four hook tests that proved only in-memory sharing (now seed storage / assert storage), stray `;;`.
Known/deferred: `use-task-tabs` and `use-unread-tasks` (each has a single consumer and non-trivial logic) still hold their own state;
`useSidePaneWidth` has no production caller (it had none before); a failed background tree re-read keeps the stale tree on screen (logged, no UI).
Process: `peer-glm` migrated the ten hooks (8 commits); the Lead wrote the helper, default-agent/usage-modal stores, `formatRelativeTime`, the workspace-tree
query, the usage-section change and the test-harness changes (`FakeWsClient.onNotification/pushNotification`, `renderWithClientProp` mounts the real bridge).
Unverified: real daemon; desktop webview; real multi-tab storage events (jsdom dispatch only).

## Final acceptance (AC1–AC19)
| AC | Landed in | Where it is proven |
|---|---|---|
| AC1 per-connection QueryClient, same-daemon reconnect keeps the cache | #386 | `daemon-query-provider.test.tsx`, `use-daemon-events.query.test.tsx` |
| AC2 one key module | #386 | `lib/query-keys.test.ts` (`query keys only come from query-keys.ts`) |
| AC3 single event→cache bridge incl. `account.updated` | #386 (+ profile.* #407, lifecycle: this PR) | `event-bridge.test.ts`, `use-daemon-events.test.ts` |
| AC4 query defaults, rate-limited keys off | #386 | `query-client.ts`, `usage-call-budget.test.tsx` |
| AC5–AC9 accounts + plan usage on one cache, zero extra usage refreshes | #386 | `providers-cache.test.tsx`, `providers-events.test.tsx`, `usage-call-budget.test.tsx` |
| AC10–AC13 catalog, profiles, health; one fallback list | #407 | `client-state-catalog.test.tsx` |
| AC14–AC16 shared run-config pickers, one `describeProfile`, `supportsThinking` | #415 | `pickers.test.tsx`, `provider-model-picker.test.tsx`, profile-form tests |
| AC17 provider identity + `ProviderIcon` | #419 | `provider-catalog.test.ts`, `provider-icon.test.tsx` |
| AC18 StatusBadge neutral, FormError/Alert, ConfirmDialog, Loading/Empty states, profile delete confirm + error | #422 | `ui/*.test.tsx`, profile/MCP delete tests |
| AC19 zustand stores, one `formatRelativeTime`, no usage-section N+1 | this PR | `stores/*.test.ts`, `relative-time.test.ts`, `use-workspace-tree.test.tsx`, usage-section shared-tree test |
Named test scenarios from the spec: all eleven exist under their names (`query client is per connection and cleared on switch`,
`query keys only come from query-keys.ts`, `account.updated renames the account in providers, usage card and chat pill`,
`expired cached report shows expired in providers`, `opening settings performs zero usage refreshes`,
`provider.list fetched once with composer, sidebar, task header and settings mounted`, `profile created in settings appears in composer picker and palette`,
`sidebar health count matches providers dots`, `profile form uses the shared provider/model, mode and thinking pickers`,
`describeProfile is identical in picker pill and settings row`, `providerLabel maps both id vocabularies`).
