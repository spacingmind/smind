# Client state stores and shared run-config components

Decision: [ADR-0032](../../decisions/0032-client-state-react-query-zustand.md) (TanStack Query for daemon data,
zustand for shared UI state). Source: the 2026-10-18 reuse audit of `web/packages/ui/src` (summary below; the full
audit is in the Supervisor's notes). Status: **spec**, nothing built.

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

## Progress
- [ ] PR 1 (S1+S2)
- [ ] PR 2 (S3)
- [ ] PR 3 (S4)
- [ ] PR 4 (AC17)
- [ ] PR 5 (AC18)
- [ ] PR 6 (AC19)

## Validation
(filled as PRs land)
