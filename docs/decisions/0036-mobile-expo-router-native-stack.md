# 0036: The mobile app navigates with Expo Router's native stack

## Status

Accepted (2026-10-20, by the Human). Plan: [`docs/plans/active/mobile-zcode-redesign.md`](../plans/active/mobile-zcode-redesign.md).
Supersedes the hand-rolled screen stack that `mobile/App.tsx` introduced in
[`mobile-app-milestone-2`](../plans/completed/mobile-app-milestone-2.md) (a `useState` over a small `Screen` union).
Does not change the relay, device identity, password, queue or stop behaviour ([ADR-0021](0021-per-chat-prompt-queue.md),
[ADR-0031](0031-homelab-cli-distribution.md), [ADR-0035](0035-relay-e2ee-v2-daemon-ephemeral.md)); it moves where they are rendered.

## Context

The mobile redesign (ZCode visual parity; plan above) uses `@expo/ui` native components and the repo's Expo skills as its
rulebook. Three of those rules cannot be met by a hand-rolled stack:

- `expo-native-ui`: "ALWAYS use a navigation stack title instead of a custom text element on the page";
  `contentInsetAdjustmentBehavior="automatic"` and both safe-area insets handled by stack headers.
- `expo-animation`: screen-to-screen motion is "native stack options in Expo Router. Never hand-roll this".
- `vercel-react-native-skills` `navigation-native-navigators`: native navigators over JS ones.

Today's stack has no native header, no swipe-back (the iOS edge gesture), no push transition and no Android back handling, and
every screen hard-codes `paddingTop: 64` to clear the status bar. There are no users yet, so there is no compatibility
constraint on how the app is routed.

## Decision

1. **`expo-router` (SDK 58) with its native `Stack`** is the app's navigator. `mobile/package.json` `main` is
   `expo-router/entry`; `index.ts` and `App.tsx` are deleted. Routes live in `mobile/app/` and contain only route and
   `_layout` files; screens, hooks and logic stay in `mobile/src/` (the skill's rule: no co-located components in `app/`).
   Dependencies, installed with `npx expo install`: `expo-router`, `react-native-screens`, `react-native-safe-area-context`,
   `expo-linking`, `expo-constants`, and the Reanimated/Gesture Handler peers (`react-native-reanimated`,
   `react-native-gesture-handler`; `react-native-worklets` is already present).
2. **One root layout owns the connection.** `app/_layout.tsx` wraps the stack in `AppThemeProvider` and a
   `ConnectionProvider` (`src/connection/`) that holds the live `RelayConnection | null` and exposes `connect(conn)`,
   `disconnect()` and `resetIdentity()` (same semantics as the old `handleConnected`/`handleDisconnect`/`handleResetIdentity`:
   the old key is gone before the pairing screen can start a new connection; if it cannot be removed the app stays on the
   status screen). The provider is the only place that closes a connection; screens never do.
3. **Three `Stack.Protected` groups replace the screen union and the early returns in `App.tsx`**, chosen by one pure function,
   `routeGate(hasConn, state)` (`src/navigation/gate.ts`, unit-tested):
   - `pairing` — no connection (`app/pairing.tsx`);
   - `status` — a connection exists but the session is `password`, `locked`, `pending`, `revoked` or `failed`
     (`app/connection.tsx`, the former full-screen cards, with the same actions);
   - `app` — a usable session, including `connecting`/`reconnecting`/`reconnected` states (a banner, as today):
     `app/index.tsx` (the task list) and `app/task/[id].tsx` (task detail).
   A guard flip removes the history of the screens it hides and redirects to the first available screen (documented
   `Stack.Protected` behaviour), which reproduces the old behaviour of replacing the content with a card. One deliberate
   difference: after a mid-session `password`/`revoked` card the user returns to the task list, not to the task that was open.
4. **Task detail takes `id` (and a display `title`) as route params**, not the whole `Task` object. The title is the stack
   header title; a deep link without one shows `Task <id>`.
5. **Headers are native.** Screen titles, the back button and the Disconnect action are stack options (the status screen's header is the constant 'Connection'; the status card in the screen body carries the specific title)
   (`Stack.Screen`'s `title`, `headerRight`, token colours), not custom text; the in-screen "← Back" and title `Text`s and the
   `paddingTop: 64` hacks go. Transient banners render inside the screens, not above the navigator (the header owns the top inset).
6. **Web (the Playwright smoke target only) exports a single-page app** (`web.output: "single"`, Metro bundler), served by the
   same static server; the app scheme is `smind`.

## Trade-offs accepted

- **Seven new native dependencies** (above). All are Expo-managed and work in Expo Go and dev clients; `@expo/ui` stays the UI
  layer (Expo Router 58 needs no UI layer of its own).
- **Connection lifetime moves from `App.tsx` state to a root context.** A bug there would drop the session; the provider is small,
  has the same three operations as before, and is covered by the existing node integration suites (which drive `RelayConnection`
  directly) plus the web smoke's full device lifecycle (pending → approved → tasks → revoked, password, queue, stop).
- **Route params are strings.** `id` is parsed once (`parseTaskParams`, unit-tested); a non-numeric id shows the "couldn't load" state.
- **A reconnect to the task list after a status card** (decision 3) instead of the previous screen.

## Alternatives considered

- **Keep the hand-rolled stack with a custom header** (the first redesign proposal). Cheapest, but it violates the three skill
  rules above and would fake the header, swipe-back and transitions. Rejected.
- **`react-navigation` native-stack without Router.** Same native stack, but file-based routing, deep links and the `Stack.Toolbar`/
  `Stack.Screen.Title` APIs the skills assume come from Router; SDK 56+ also tells apps not to import `@react-navigation/*`
  directly. Rejected.
- **Router with the status screens as a modal over the app group.** Keeps the task screens mounted behind a dead session
  (their calls would reject and their effects keep running); the guard approach unmounts them, as `App.tsx` did.
