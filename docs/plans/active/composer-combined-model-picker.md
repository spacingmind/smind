# Combined provider + model picker (Paseo-style)

**Ask (2026-10-12, follow-up to [composer-redesign](../completed/composer-redesign.md)):**
"Chọn provider => chọn model gộp vào 1 selection dropdown giống paseo." The
composer toolbar's separate agent/profile pill, provider pill and model pill
become **one** selector, ported from Paseo's model browser.

**Size:** M.

**Sources ported from** (`refs/paseo/packages/app/src/`):
`components/combined-model-selector.tsx` (trigger + popover shell),
`components/model-browser.tsx` (root = pinned *Profiles* section + provider
drill-down rows with model count/state; provider view = header with back
button + model rows with check; search at both levels, cross-provider ranked
results at the root; unavailable/error empty state),
`components/model-browser-view.ts` (+ `.test.ts`: initial view, all-view,
profile grouping), `provider-selection/provider-selection.ts` (rows,
synthetic default row, ranking, selected-label resolution),
`composer/agent-controls/{index,model-sheet}.tsx` (compact → bottom sheet).
Paseo is React Native; this ports the interaction model and the pure logic
(near-verbatim, with its tests), not the RN components.

## Today (verified on `3ffad8ec`)

`run-config-toolbar.tsx` has three separate parts: `.Agent` (profile
DropdownMenu: "No agent", profile rows with provider/mode/thinking metadata,
"Manage agents…", plus the ↺ reset button when `custom`), `.Provider` (Radix
Select; read-only once the chat is bound, ADR-0016 P3) and `.Model` (Radix
Select; hidden when the provider lists no models; "Default" first, an
unlisted profile model shown as its raw id). State/actions live in the
context: `setProvider` resets `permissionMode`, `autoAccept` and `model`
(modes and models are provider-scoped, ADR-0019/0025); `setModel("")` means
"the provider's default, send nothing". `provider.test` (local PATH /
credential check, no network) already backs the sidebar's health dot via
`accountHealthTestKey`.

## Decisions

1. **One trigger replaces `.Agent` + `.Provider` + `.Model`** (the three parts
   are removed, not kept as dead code). Thinking and permission stay separate
   pills and keep re-deriving from the selected provider.
2. **Profiles become the pinned root section** of the popover, as in Paseo
   ("Profiles" above "Providers"): a "No agent" entry (detaches, as today),
   one row per profile with its `describeProfile` summary, the applied profile
   checked and marked "custom" when hand-edited, and a "Manage agents…" action
   (the existing `smind:open-settings` event). The ↺ reset-to-agent button
   stays as a small icon button next to the trigger, only while `custom`.
   Justification: a profile *is* a provider+model(+mode+thinking) preset, so it
   belongs beside the provider/model choice; keeping a separate "Agents" pill
   would leave two selectors again. All existing profile semantics
   (`applyProfile`, `clearAgent`, `resetToAgent`, `smind:use-agent`, the ★
   default seed) are untouched — only where they are rendered moves.
3. **Selection semantics are unchanged.** Picking a model of another provider
   calls one new atomic context action `setProviderModel(provider, model)`
   (= `setProvider` then the model; mode/autoAccept reset exactly as
   `setProvider` does today). Picking a model of the *current* provider only
   sets the model (mode kept). The per-provider "Default model" row (id `""`)
   is always first, like Paseo's synthetic default row, and means "send
   nothing".
4. **Trigger label** is `<Provider> · <Model>` (`Claude Code · Opus 5.5`;
   `Claude Code` alone for the default model). smind has no per-provider brand
   glyphs (Paseo does), so the provider name carries what Paseo's icon does;
   the leading icon stays `SquareTerminal`. Below `sm` the label hides and the
   trigger is the 44px icon-only button from composer-redesign.
5. **Bound chat (ADR-0016 P3):** the provider cannot change, so the popover
   opens straight into the bound provider's view with no back button, no other
   providers and a one-line note ("Bound to X — start a new chat to switch
   providers"). Profiles of other providers are not offered.
6. **Unavailable providers** (Paseo: disabled with reason): `provider.test`
   per provider, lazily on the first open per client (single-flight, local
   check only — no polling, no per-render calls), results cached. A failed
   provider's root row is disabled with `detail` as the reason; it is still
   shown (and stays usable as the *current* selection so an existing choice is
   never silently dropped). Untested/pending = enabled.
7. **Narrow (`< md`, the app's mobile breakpoint):** the picker renders in a
   bottom `Sheet` (Paseo's `model-sheet`) instead of a `Popover`; same content.
8. **Keyboard:** search input autofocuses; ↑/↓ move the active row, Enter
   activates it (select model / drill into provider / apply profile), ←/Backspace
   on an empty query goes back, Esc closes.
9. **Where the pure logic lives:** `lib/model-browser.ts` (no React), tested
   like Paseo's `model-browser-view.test.ts`. The UI is
   `components/composer/model-picker.tsx`.

## Acceptance criteria

- **AC1 One selector.** The toolbar has a single provider+model trigger; the
  Agent, Provider and Model pills are gone; thinking and permission pills
  remain and re-derive from the newly selected provider (Claude-only thinking,
  the provider's own modes — behaviour unchanged).
- **AC2 Provider switch.** Choosing a model under another provider switches
  provider and model together and resets mode/autoAccept to that provider's
  defaults; choosing under the same provider keeps the mode.
- **AC3 Default model.** Every provider has a first "Default model" entry that
  sends no model; a provider with no catalog is selectable through it alone.
- **AC4 Browse.** The popover opens in the current provider's view (current
  model checked); Back shows the root with Profiles and Providers (model count
  per provider); a sole provider skips the root.
- **AC5 Search.** Search filters inside a provider view; at the root it ranks
  models across providers (provider-qualified), shows "no matches" when empty,
  and ignores providers with no rows.
- **AC6 Profiles.** Profiles are a pinned root section with "No agent" and
  "Manage agents…"; applying one seeds provider/mode/thinking/model as before;
  custom/↺ reset keep working.
- **AC7 Health.** An unhealthy provider is disabled with its reason; health is
  fetched lazily, once per client, single-flight.
- **AC8 Bound chat.** Only the bound provider's models; no provider switching.
- **AC9 Narrow.** `< md` renders a bottom sheet; `< sm` the trigger is
  icon-only 44px.
- **AC10 Keyboard.** ↑/↓/Enter/←/Esc as in decision 8.
- **AC11 No regression.** `task test`, `task lint`, web typecheck/tests green;
  the existing composer behaviour tests (submit provider/model/mode/thinking,
  persistence, bound provider, profile apply/custom/reset, ★ default) are
  migrated to the new controls, not deleted.
- **AC12 Visual.** Light+dark screenshots at 1440×900 and 390×844: closed
  trigger, open popover (provider view), search filtered, provider switch
  result, narrow sheet; compared with Paseo's picker.

## Test scenarios (named)

`lib/model-browser.test.ts` (ported from Paseo's `model-browser-view.test.ts`
and `provider-selection.test.ts`):

- `opens a sole provider directly`
- `opens the selected provider when there is one`
- `opens the root so pinned profiles are reachable`
- `opens a sole provider directly regardless of root content`
- `falls back to the root when the selected provider is gone`
- `groups profiles by provider and model, skipping profiles without a model`
- `trims model ids so whitespace cannot create a separate key`
- `browses providers while the query is empty`
- `shows every searchable model as soon as empty search receives focus`
- `ranks the same model label across every provider that offers it`
- `matches models by their provider label`
- `reports no matches instead of falling back to the provider list`
- `ignores providers that are still loading or errored`
- `every provider gets a Default model row first`
- `a provider without a catalog is selectable through its default row`
- `an unhealthy provider is disabled with its reason`
- `trigger label is provider and model, provider alone for the default model`
- `an unlisted model id shows as its raw id`

`model-picker.test.tsx`:

- `trigger shows the provider and model and is labelled`
- `opens in the current provider's view with the current model checked`
- `Back shows Profiles and Providers with model counts`
- `picking a model of another provider selects provider and model together`
- `root search ranks models across providers`
- `search with no matches says so`
- `an unhealthy provider is disabled with its reason and cannot be entered`
- `a bound chat shows only its provider and no back button`
- `Arrow keys and Enter select, Left goes back, Escape closes`
- `renders a bottom sheet below the md breakpoint`
- `profile rows apply, No agent detaches, Manage agents opens settings`
- `health is fetched once per client on first open`

Migrated in `composer.test.tsx` / `run-config-toolbar.test.tsx`: provider
switch resets mode and model; thinking shows only for claude-native; model sent
on `run.start` (default omits it); profile apply/custom/↺; bound provider;
persistence + ★ default agent.

## Progress

- [x] Spec committed
- [x] Step 1 — `lib/model-browser.ts` + ported tests
- [x] Step 2 — `model-picker.tsx` (popover + sheet, keyboard) + tests
- [x] Step 3 — wire into toolbar/composer (`setProviderModel`, health hook,
      old parts removed), composer + task-detail tests migrated
- [x] Step 4 — screenshot pass + fixes (highlight starts on the current
      selection; calmer search focus; guard-safe 16px mobile search)

## Peer log

- `peer-glm` (`5b12901a`, Step 1: pure logic + 18 ported tests): **ACCEPT**
  (`8c2d1b62`, `3f15b62e`). Reviewed the diff; its ranking follows the brief's
  scoring (ties keep input order) instead of Paseo's `scoreTextFields`.
- `peer-sonnet` (`96d35cb1`, Step 2: picker UI + 12 tests): **ACCEPT**
  (`185f724c`, `69776699`, `6f653de4`) with Lead fixes after review: type-scale
  tokens, highlight starting on the current selection, calmer search focus.
  Used Sonnet rather than GLM because the popover/sheet/keyboard work is the
  trickier outcome.
- `peer-sonnet` (`d8dcbab0`, Step 3: composer/toolbar test migration, 18
  failing tests, nothing deleted, 3 added): **ACCEPT** (`891e8d76`,
  `35677a50`).
- Lead: the lazy health hook (`9bf5c191`), toolbar/composer wiring
  (`c2a9bf3d`), the 4 `task-detail.test.tsx` migrations and the text-scale
  guard fix.

## Validation

Verified 2026-10-12 on `feat/composer-combined-model-picker` (develop merged).

- AC1-AC10: `model-browser.test.ts` (18), `model-picker.test.tsx` (12),
  `use-provider-health.test.ts` (3), migrated `composer.test.tsx` /
  `run-config-toolbar.test.tsx` / `task-detail.test.tsx` (incl. provider
  switch resets mode, same-provider pick keeps mode, default model omits
  `model`, health once per client, unhealthy provider disabled, bound chat).
- AC11: web `tsc -b` clean, full web suite 1740/1740 (run with
  `--testTimeout=90000 --maxWorkers=3`: the machine was at load 15-20 and the
  default 5 s timeout flakes a different App/composer test per run, unrelated
  to this change); `task lint`, `go test ./...` and the desktop node tests
  pass. There is no ESLint config in `web/` yet.
- AC12: light+dark screenshots at 1440x900 and 390x844 (closed trigger,
  provider view, root with profiles, search filtered, no match, unhealthy
  provider, bound chat, provider switch result, GLM view; at 390px the picker
  is the bottom sheet and the trigger is icon-only), rendered from the real
  `<Composer>` against a fake client (local harness, `tmp/`). Not run against
  a live daemon.
- Residual: a model row's secondary line is `description ?? id` (as Paseo does),
  so a catalog model without a description shows its raw id; the trigger shows
  `Provider · Model` (e.g. `GLM · GLM 5.3`), a little redundant when the labels
  repeat.
