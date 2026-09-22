# Mobile app: adopt `@expo/ui` for native controls

## Context

`docs/ROADMAP.md`'s Phase 3 target is explicitly "Mobile app (Expo +
**@expo/ui**)" — but `@expo/ui` was never actually installed. Confirmed
by reading `mobile/package.json`: dependencies are `expo`,
`expo-status-bar`, `react`, `react-native`, `@noble/*` only. (An earlier
plan doc, `mobile-ui-polish.md`, mistakenly claimed it was "installed but
unused" — that was wrong; this plan corrects it.)

`mobile/AGENTS.md` says: *"Expo HAS CHANGED — read the exact versioned
docs at https://docs.expo.dev/versions/v57.0.0/ before writing any
code."* `mobile/package.json` pins `expo: ~57.0.24`, well past the
universal `@expo/ui` layer's SDK 56+ floor — the universal layer is
fully available and runs in Expo Go, no custom dev build needed.

**Every current touchable/input in the app**, enumerated by reading the
three screens directly (not assumed):

| Screen | Element | Context |
|---|---|---|
| `PairingScreen.tsx` | Connect button (`TouchableOpacity`, line 55) | Fixed, single instance |
| `PairingScreen.tsx` | Pairing-URL input (`TextInput`, line 45) | Fixed, single instance |
| `TasksScreen.tsx` | Retry button (line 95) | Fixed, single instance |
| `TasksScreen.tsx` | Disconnect link (line 109) | Fixed, single instance |
| `TasksScreen.tsx` | Task card tap (line 150) | **Inside a `FlatList` row** — repeats per task |
| `TaskDetailScreen.tsx` | Back button (line 203) | Fixed, single instance |
| `TaskDetailScreen.tsx` | Tool-call row expand/collapse (line 313) | **Inside a `.map()` per tool call in a run's transcript** |
| `TaskDetailScreen.tsx` | Permission option buttons (line 254) | Inside a `.map()` over one request's options — bounded to a handful, not a growing collection |
| `TaskDetailScreen.tsx` | Compose input (line 275) | Fixed, single instance |
| `TaskDetailScreen.tsx` | Send button (line 283) | Fixed, single instance |

Read `.claude/skills/expo-ui/`'s `SKILL.md` and `references/universal.md`
in full before writing any code — they're already loaded into this
session and cited below, but the implementing agent should read them
directly rather than trust secondhand paraphrase.

**Key facts from those skill docs, verified this session:**

- Universal components: `Host` (required root wrapper), `Column`/`Row`/
  `Spacer`/`ScrollView` (layout), `Text`/`Icon` (display), `Button`/
  `Switch`/`Checkbox`/`Slider`/`TextInput`/`Picker` (controls),
  `BottomSheet`/`Collapsible` (presentation), `List`+`ListItem`/
  `FieldGroup` (collections/forms). Import everything, including `Host`,
  from the `@expo/ui` package root.
- **`List`/`ListItem` is explicitly unsuitable for this app's task
  list** (`references/universal.md`: "not suitable for large lists...
  each `ListItem` is a JSX node processed on the JS thread") —
  `TasksScreen`'s `FlatList` is correct today and stays untouched.
- **`@expo/ui`'s `TextInput` is NOT React-Native-API-compatible.** Its
  `value`/`selection` props take an `ObservableState` (from
  `useNativeState`), not a plain string — `onChangeText` runs as a
  worklet on the UI thread and writes to that observable directly,
  bypassing a React re-render. This **requires `react-native-worklets`**
  as a real dependency, not just `@expo/ui` itself. This is a materially
  different API shape than every other change this app has made so far
  (id-keyed RPCs, event subscriptions) — see Decisions for how this
  plan manages that risk.
- `expo-design-system`'s rule: *"Do not wrap platform components that
  already carry the design language ... `@expo/ui` views ... just to
  route them through the system. Native styling **is** the design
  system for those."* — `Button`/`TextInput` should use their own
  native variant/styling hooks (read the installed package's `.d.ts` for
  the actual prop names, since `@expo/ui`'s API is versioned with the
  SDK and the docs track `latest`, not this project's pinned version),
  not be force-fitted with `mobile/src/theme.ts`'s custom
  `StyleSheet`-based button styles the way the current `TouchableOpacity`
  buttons are.
- `expo-animation`'s guidance doesn't add new work here: `@expo/ui`'s
  native controls (button press feedback, sheet transitions) already
  animate on their own platform's terms — no custom Reanimated work is
  in scope for this plan.
- `expo-brownfield` doesn't apply — this is a pure Expo app from day
  one, not native code adopting React Native incrementally.

## Decisions

- **Two sequential items, split by risk, not by screen.** Item 1
  (`Button` only, all six fixed single-instance action buttons) is the
  safe, high-value slice: `Button`'s API is a straightforward
  `onPress`/children/variant shape, no new dependency beyond `@expo/ui`
  itself, and every one of this app's existing tests that assert on
  button behavior (disabled state, press calling a handler) should keep
  passing conceptually since the interaction contract doesn't change —
  only the rendered element does. Item 2 (`TextInput`, the two text
  inputs) is the higher-risk slice: a genuinely different state model
  (`useNativeState`/`ObservableState` instead of `useState<string>`)
  plus a new `react-native-worklets` dependency. Sequencing this way
  means Item 1 lands and is verified before the riskier item is
  attempted — if Item 2 proves too disruptive to the surrounding
  validation logic (`canSend`, `pairingUrl.trim()`), it is acceptable to
  keep React Native's `TextInput` for now and document why in
  Validation, rather than forcing a fragile integration. This is not a
  cop-out — it is the same "smallest real slice, don't force a novel
  piece through if it doesn't fit" discipline every prior mobile plan
  in this repo has used.
- **Row-repeated touchables stay React Native, only fixed single-instance
  actions become `@expo/ui` `Button`.** The task-card tap (inside
  `FlatList`, unbounded) and the tool-call-row expand/collapse toggle
  (inside a `.map()` whose length is a run's real tool-call count, not
  bounded like the permission options) keep their current
  `TouchableOpacity`/`Pressable` — matching `references/universal.md`'s
  own warning that a `List`'s `ListItem` nodes are JS-thread-processed
  and don't scale, which is the same underlying concern for any
  `@expo/ui` control repeated per row of a scrolling collection. The
  permission-option buttons are the one per-`.map()` exception: bounded
  to a handful of options per request (never large, never virtualized),
  so they're in scope for Item 1.
- **No component-level test coverage is possible for either item.**
  This codebase's mobile test suite is logic-layer only (no
  `@testing-library/react-native`, established since Milestone 1) —
  native `@expo/ui`-rendered output can't be exercised by that style of
  test at all, and this plan does not introduce a new testing framework
  to attempt it. Verification is `npx tsc --noEmit` clean plus a written
  manual-check list for whenever a simulator/device is available —
  matching `mobile-ui-polish.md`'s already-accepted "manual light/dark
  smoke test outstanding" precedent, now extended to native-control
  behavior too.
- **Read the installed package's actual `.d.ts` for exact prop names
  before writing any `Button`/`TextInput` usage**, per
  `references/universal.md`'s own "Confirming the API" section — the
  docs track `latest`, this project is pinned to whatever `@expo/ui`
  version resolves against `expo: ~57.0.24`, and those can disagree.
  Do not guess a prop name from the docs' code samples alone.
- **`Host` wraps the whole app once, at the root** (`App.tsx`), not
  per-screen — matches `references/universal.md`'s example structure
  and avoids repeating the wrapper in three files.

## Acceptance Criteria

### Item 1 — `@expo/ui` `Button` for fixed, single-instance actions

- `@expo/ui` is installed via `npx expo install @expo/ui` (not raw
  `npm install`, so the resolved version matches this project's Expo
  SDK).
- `App.tsx`'s root renders a single `Host` wrapping the existing screen
  stack.
- These six elements render via `@expo/ui`'s `Button` instead of
  `TouchableOpacity`, preserving their exact current behavior
  (disabled states, the handler each one calls, loading/spinner states
  where they exist today): `PairingScreen`'s Connect button,
  `TasksScreen`'s Retry button, `TasksScreen`'s Disconnect link,
  `TaskDetailScreen`'s Back button, `TaskDetailScreen`'s permission
  option buttons, `TaskDetailScreen`'s Send button.
- The task-card tap and the tool-call-row expand/collapse toggle are
  explicitly **not** touched — still `TouchableOpacity`/`Pressable`, per
  Decisions.
- `npx tsc --noEmit` is clean; every existing test in
  `mobile/src/__tests__/` that doesn't depend on rendering the changed
  JSX (i.e. everything, since this test suite is logic-layer only)
  still passes unmodified.

### Item 2 — `@expo/ui` `TextInput` for the two text inputs (best-effort)

- `react-native-worklets` is installed via `npx expo install` if this
  item proceeds.
- `PairingScreen`'s pairing-URL input and `TaskDetailScreen`'s
  follow-up compose input render via `@expo/ui`'s `TextInput` +
  `useNativeState`, preserving current behavior: the Connect/Send
  buttons' disabled state still correctly reflects whether the field is
  effectively empty (trimmed), typed text is not lost on a failed
  send/connect (mirrors the existing `canSend`/draft-restore logic).
- **OR**, if the `ObservableState` model proves genuinely disruptive to
  that surrounding validation logic within a reasonable effort: this
  item is documented as deferred in Validation, with the specific
  friction point named, and React Native's `TextInput` stays in place.
  Either outcome is an acceptable "done" for this item — the acceptance
  bar is "tried it for real and made an honest, specific call," not
  "forced it to work."
- `npx tsc --noEmit` clean in either outcome.

## Test Scenarios

- No automated test can exercise either item's actual rendered output
  (native `@expo/ui` views, not something jsdom/vitest can render or
  inspect) — this is a known, accepted gap, not an oversight.
- **Item 1**: `npx tsc --noEmit` passes; manual-check list (to run
  whenever a simulator/device is available, not now) covering: each of
  the six buttons calls its existing handler, shows correctly
  disabled/enabled per its existing condition, and (for Connect/Send)
  still shows a busy/spinner state while its async action is in flight.
- **Item 2** (if attempted): manual-check list covering: typing in
  either field updates what's sent on submit, the Connect/Send button's
  enabled state tracks the trimmed input correctly, and a failed
  connect/send still restores the typed text for retry (mirrors this
  app's existing "never silently lose a draft" behavior).

## Progress

- [ ] Item 1 — `@expo/ui` `Button` for fixed, single-instance actions.
- [ ] Item 2 — `@expo/ui` `TextInput` for the two text inputs
      (best-effort; deferral with a named reason is an acceptable
      outcome).
- [ ] Hand off implementation via Paseo (GLM as primary implementer,
      per the user's standing preference — direct `deny` + specific
      `send_agent_prompt` redirect if it shows the "many turns, no
      commits" stall pattern; escalate to Sonnet in the same
      worktree/branch if a redirect doesn't produce real progress
      within a further reasonable number of turns, per this session's
      established mobile-ui-polish precedent).
- [ ] Independent verification of agent-reported work before merge.

## Validation

To be filled in as each item lands, mapping back to each Acceptance
Criterion with the specific check that confirmed it (given no automated
test coverage is possible, this will lean on direct code review + tsc,
not a test run count).
