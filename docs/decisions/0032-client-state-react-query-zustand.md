# 0032: Client state — TanStack Query for daemon data, zustand for shared UI state

## Status

Accepted (2026-10-18, by the Human). Plan: [`docs/plans/active/client-state-stores.md`](../plans/active/client-state-stores.md).

## Decision

The web UI (`web/packages/ui`, also the desktop app's webview) adopts two libraries, the same pair Paseo uses
(`refs/paseo/packages/app`):

1. **`@tanstack/react-query` owns every piece of daemon data** fetched over wsapi (provider catalog, accounts, plan
   usage, profiles, provider health, workspace/task lists, `daemon.info`, …) and the desktop bridge's read calls
   (connections, daemon status).
   - One `QueryClient` per daemon connection (`WsClient`), provided at the app root; switching connection swaps the
     client, so no data leaks across daemons.
   - Query keys are the RPC name plus its params: `["provider.list"]`, `["account.list"]`, `["profile.list"]`,
     `["account.usage", {cached: true}]`, `["provider.test", accountKey]`. One key module
     (`src/lib/query-keys.ts`) is the only place keys are built.
   - **Daemon events invalidate or patch queries** in one place (the existing `useDaemonEvents` subscription becomes
     the bridge): e.g. `account.updated`/`account.removed` → `account.list`, `provider.list`; `profile.*` →
     `profile.list`; `account.usage` → `setQueryData` on the usage query. Components never subscribe to events to
     refetch on their own.
   - Mutations go through `useMutation` and invalidate the keys they affect.
   - Rate-limited data keeps its daemon-side budget: plan-usage queries read the daemon's cache (`cached: true`) and
     never trigger provider calls by mounting, focusing or retrying; `refetchOnWindowFocus`, `refetchOnMount` and
     `retry` are off for those keys, and tests assert call counts (ADR-0028).
2. **`zustand` owns client-only state that more than one component reads**: open tabs, sidebar/pane layout,
   persisted UI preferences (via `persist` over `localStorage`), default agent id, settings-open, palette state.
   Stores live in `src/stores/`. Component-local state that nobody else reads stays `useState`.
3. Existing hand-rolled module stores (`useSyncExternalStore` in `use-account-usage`, usage/notification preferences,
   window state, …) and per-component `useEffect` + `client.call` fetches migrate to the above incrementally, starting
   with the entities whose duplicated state is user-visible.

## Alternatives considered

- **SWR + zustand** (ZCode, `refs/zcode/packages/ui`). Smaller, but weaker mutation/invalidation story and no
  devtools parity with Paseo, which is the interaction reference we port from.
- **Keep the hand-rolled stores and standardise them.** No new dependency, but every entity would re-implement
  caching, single-flight, invalidation and loading/error states; the audit found 7 `provider.list`, 4 `profile.list`
  and 4 independent `provider.test` caches drifting apart.
- **Redux Toolkit / RTK Query.** Covers both halves but is heavier and nothing we port from uses it.

## Rationale

A reuse audit (2026-10-18) found the same server entities fetched and cached independently per component, so
surfaces disagree: Providers shows an account as "not tested"/OK while Plan usage reports its login expired;
`account.updated` is never subscribed; profiles created in Settings never reach the composer until reconnect. A
shared query cache keyed by RPC with event-driven invalidation removes that class of bug, and zustand replaces prop
drilling from a 2,300-line `App.tsx` for shared UI state. Matching Paseo's stack makes porting its components
straightforward.
