# Model/profile picker — parity with Paseo

**Ask (2026-10-12, pre-v0.11 polish):** "Cái select model/profile vẫn chưa tối ưu,
đọc refs paseo." The combined picker from PR #303
([composer-combined-model-picker](composer-combined-model-picker.md)) was a
*partial* port: the pure logic came over, the interaction details and visuals
did not. This plan lists every difference found reading Paseo end to end, then
ports the ones that apply.

**Size:** M. **Sources** (`refs/paseo/packages/app/src/`, read at `8d9e7d3a`):
`components/model-browser.tsx` (1723 l), `components/model-browser-view.ts`,
`components/combined-model-selector.tsx`,
`composer/agent-controls/{index,control,glyph,layout,model-sheet,model-sheet-flow,model-loading,utils}`,
`provider-selection/provider-selection.ts`, `agent-profiles/**`
(`use-agent-profile-picker`, `agent-profile-glyph`, `profile-summary`),
`protocol/src/search/text-match.ts`.

smind files: `components/composer/model-picker.tsx`, `lib/model-browser.ts`,
`components/composer/run-config-toolbar.tsx` (`RunConfigToolbarModelPicker`),
`components/settings/profiles-section.tsx`.

## Gap analysis (Paseo → smind)

Legend: **PORT** = in scope, **KEEP** = smind semantics win, **DEFER** = out of
scope with the reason.

### Behaviour

| # | Paseo | smind today | Verdict |
|---|-------|-------------|---------|
| G1 | Opens on the selected provider's view (`resolveInitialModelBrowserView`). In Paseo the `hasProfiles → root` rule wins first; smind ported it verbatim, so **any user with a profile (the ★ default seeds one) always lands on the root**, and `selectedModel.length > 0` is required, so the *default model* (`""`, ADR-0025) also lands on the root. | root whenever profiles exist or model is `""` | **PORT, adapted**: open on the current provider's view whenever the provider exists (default model included); the provider view lists *that provider's* profiles (G2) and Back reaches every profile. Supervisor-requested deviation from Paseo's `hasProfiles` rule. |
| G2 | Provider view = header + profile rows of that provider (tinted, above the models) + models. Empty profile list → muted "Create profile" row. Profiles hidden while searching. | provider profiles + "No agent" + "Manage agents…" row, then a "Models" heading | **PORT** layout (tinted section, pencil in the heading, "Create profile" empty row); **KEEP** "No agent" (detach) and the ✓ + `custom` badge on the applied profile (Paseo's profile rows are one-shot actions, smind's are bound — ADR-0014). |
| G3 | Profiles **grouped by provider+model** (`groupProfilesByProviderModel`) and shown on the model rows they use: hover (always visible on touch) action on a model row — **+ create profile from this model** (seed `{provider, modelId, name: modelLabel}`), one profile → its glyph → **edit it**, several → glyph → profiles list. | helper ported, never used; model rows have no profile affordance | **PORT.** Create/edit land in Settings → Agents with the inline form opened (prefilled seed / that profile). The seed of the *currently selected* model row also carries the live mode/autoAccept/thinking ("create profile from the current selection"). |
| G4 | Profile glyph (`AgentProfileGlyph`: stored icon key + identity colour, default ★) leading every profile row and the row action. | no glyph | **PORT** the glyph slot with Paseo's default. **DEFER** per-profile icon/colour: smind's `AgentProfile` has neither field (store + wire change → ADR). |
| G5 | Provider glyph (`useProviderIcon`: Claude/Codex SVG marks, `Bot` fallback) on provider rows, the provider header, model rows, and the trigger. | none (trigger uses a generic terminal icon) | **PORT** Claude + Codex marks, `Bot` fallback (what Paseo does for providers without a mark: GLM, Kimi). |
| G6 | Provider header: glyph (foreground) + title + back + **settings icon** (opens that provider's settings). | back + title | **PORT**: gear opens Settings → Providers. |
| G7 | Desktop popover height **sized to content**: `clamp(220, 80 + n·40, 400)`, `n` = models in the view (all rows at the root). | fixed `min(26rem, avail)` → big empty area under 3 models | **PORT** (`resolveDesktopFixedHeight`, unit-tested), capped by Radix available height. |
| G8 | Row anatomy: 16px leading glyph · label · **description inline** (baseline, muted, truncated) · 16px check slot. Provider row: glyph · label · `N models` / spinner `Loading…` / ⚠ `Error` · chevron, hairline separators. | two-line stacked rows, no leading glyph | **PORT** (rows become ~36px, which is what G7's `40px/row` assumes). smind extra kept: unhealthy provider reason in the trailing slot, disabled. |
| G9 | Section headings `Profiles` (+ pencil "Edit profiles" tooltip at the right) and `Providers` (only when the Profiles section is shown); profiles sit on a `surface1` band with a bottom border. | "Manage agents…" text row; always a Providers heading | **PORT** (pencil replaces the "Manage agents…" row; same `smind:open-settings` target). |
| G10 | Loading and error states: provider `loading` → spinner row/empty state and `Loading…` trigger (`model-loading.ts`); `error` → ⚠ + message + Retry. | `buildBrowserProviders` always yields `models`; composer shows `FALLBACK_PROVIDERS` until `provider.list` resolves | **PORT** the states in picker + `buildBrowserProviders`; the composer passes a `providersLoaded` flag (3-line change in `composer.tsx`). Retry **DEFER**: smind has no per-provider refetch RPC. |
| G11 | Trigger = provider glyph + **model label only** (`buildSelectedTriggerLabel`), unlisted model → raw id, `""` → the catalog's default model label else first row. | `Provider · Model` text with a generic icon → "GLM · GLM 5.3" | **PORT**: glyph + model label; provider name stays in `title`/`aria-label`. When no catalog default is known the provider label is shown. |
| G12 | Toolbar density (`layout.ts`: full/condensed/tight hides carets + labels from measured width). | label hides `< sm`, caret always | **DEFER**: toolbar-wide, and `fix/live-run-composer-and-noise` edits `run-config-toolbar.tsx`. Inside the trigger only: label is `min-w-0 truncate` with a max width. |
| G13 | Search ranking `scoreTextFields` (multi-token AND, tiers exact < whole word < prefix < word start < substring < in-word subsequence, then offset, spread; ties by `modelLabel.localeCompare`), fuzzy off. | single-token score table, subsequence across whole field, ties by input order | **PORT** `text-match.ts` verbatim minus fuzzy, with its tests. |
| G14 | Compact sheet: search is **not** auto-focused (keyboard would cover the list); focusing it with an empty query shows **all model rows** (`searchAllOnFocus`); desktop popover autofocuses and keeps the browse root. | `autoFocus` always (mobile keyboard pops over the sheet); `isSearchFocused` hard-wired `false` | **PORT.** Rows `preventDefault` on pointer-down so tapping a row while the input is focused does not blur → re-render → lose the click. |
| G15 | Provider row count = `rows.length`. | counts the synthetic "Default model" row too ("GLM: 3 models" for 2 real) | **PORT, adapted**: count real models; only-default → "Default model". |
| G16 | Model description = `model.description ?? model.id`. | same | already equal |
| G17 | Compact sheet 80–90% snap, flexible viewport, 44px rows. | `h-[75vh]`, 36px rows | **PORT** height 80dvh and 44px row hit target `< md`. (Paseo's two-step sheet — settings list that pushes the browser — is **DEFER**: smind's thinking/permission are separate pills.) |
| G18 | Keyboard: none (React Native). | ↑/↓/Enter/←/Backspace/Esc | **KEEP** (AC10 of #303); new rows (profile action buttons) stay out of the arrow walk. |
| G19 | Profiles are actions, no ✓. | ✓ + `custom` for the applied profile; "No agent" | **KEEP** (ADR-0014 bound agent). |
| G20 | ADR-0019 mode reset on provider switch, ADR-0025 default model, bound chat, health-disabled provider | implemented | **KEEP**, unchanged. |

## Decisions

1. Everything stays inside `model-picker.tsx` (split into small files next to
   it), `lib/model-browser.ts`, a new `lib/text-match.ts`, a new
   `lib/settings-intent.ts`, and the minimal wiring in `run-config-toolbar.tsx`
   (+ 3 lines in `composer.tsx` for `providersLoaded`). No daemon, store or wire
   change.
2. Settings handoff without new routes: `lib/settings-intent.ts` holds one
   pending intent (`{kind:"newProfile", seed}` / `{kind:"editProfile", id}`);
   the picker sets it and fires the existing `smind:open-settings`;
   `ProfilesSection` consumes it on mount (and via a window event if already
   mounted) to open its inline form. Not an architectural change (same
   mechanism as the existing event).
3. Per-profile icon/colour and a Retry-provider RPC are deliberately not built
   (data-model / API surface → AGENTS rule d).
4. Deviation from Paseo's `hasProfiles → root`: see G1.

## Acceptance criteria

- **AC1 Opens on the current provider** (also for the default model and when
  profiles exist); sole provider skips the root; Back reaches the root.
- **AC2 Content-sized popover.** Desktop height = `clamp(220, 80+n·40, 400)`
  for `n` rows in the view, capped by the viewport; no empty band under a short
  list.
- **AC3 Row anatomy** as G8 (leading glyph, inline description, check slot,
  provider rows with count / loading / error / unhealthy reason + chevron).
- **AC4 Profiles section** as G2/G9: tinted band, "Profiles" heading with
  pencil, per-provider rows in the provider view, all rows at the root,
  "Create profile" when none, ✓/`custom` on the applied profile, "No agent".
- **AC5 Model-row profile actions** as G3 (create from model / edit one / list
  several), hover on desktop, always visible `< md`; create from the selected
  model seeds mode/autoAccept/thinking too.
- **AC6 Settings handoff:** create seeds the inline "new agent" form; edit opens
  that profile's inline editor; the provider gear opens Settings → Providers.
- **AC7 Glyphs:** provider glyphs (Claude, Codex, `Bot` fallback) and the
  default profile glyph render in rows, header and trigger.
- **AC8 Trigger:** glyph + model label (no `GLM · GLM 5.3`), full provider +
  model in `title`/`aria-label`; unlisted model → raw id; default model → the
  catalog default's label.
- **AC9 Search:** `scoreTextFields` ranking (multi-token AND, tiers, tie by
  label); empty focused search on the sheet shows all rows; desktop keeps the
  browse root.
- **AC10 Loading:** providers not yet loaded → trigger `Loading…`, provider
  rows spinner, provider view spinner; error kind → ⚠ + message.
- **AC11 Narrow:** `< md` bottom sheet, 80dvh, no search autofocus, 44px rows,
  tapping a row with the search focused selects it.
- **AC12 Keyboard** unchanged: ↑/↓ skip headings and disabled rows, Enter,
  ←/Backspace on empty query, Esc.
- **AC13 Semantics unchanged:** ADR-0019 mode reset, ADR-0025 default model,
  bound chat, profile apply/detach/reset/★, health-disabled providers.
- **AC14 No regression + visual:** web typecheck + tests, `task test`,
  `task lint`; light+dark screenshots before/after at 1440×900 and 390×844.

## Test scenarios (named)

`lib/text-match.test.ts` (ported): `exact beats whole word beats prefix`,
`multi-token query needs every token`, `subsequence is per word`,
`ties keep stable order`.

`lib/model-browser.test.ts` (extended): `opens the selected provider even for the default model`,
`opens the selected provider when profiles exist`,
`opens a sole provider directly`, `falls back to the root when the provider is gone`,
`desktop height grows with rows and clamps at 220 and 400`,
`provider count excludes the synthetic default row`,
`trigger label is the model label, the catalog default for "", the raw id when unlisted`,
`a loading provider has no model rows and a Loading label`,
`ranking breaks ties by model label`.

`model-picker.test.tsx` (extended): `opens on the current provider with profiles configured`,
`popover height follows the row count`,
`provider view shows the provider's profiles above its models`,
`root shows every profile under a Profiles heading with an edit pencil`,
`no profiles shows a Create profile row`,
`model row + creates a profile seeded with provider and model`,
`model row with one profile offers edit, with several offers the list`,
`the selected model's seed carries mode and thinking`,
`provider gear opens provider settings`,
`provider rows show glyph, count, loading and error states`,
`trigger shows the model label with the provider in the title`,
`search ranks with scoreTextFields and breaks ties by label`,
`sheet does not autofocus search and shows all rows on focus`,
`tapping a row while the search input is focused selects it`,
`keyboard walk skips headings, disabled rows and row action buttons`.

`profiles-section.test.tsx`: `a pending new-profile intent opens the add card prefilled`,
`a pending edit intent opens that profile's inline editor`.

## Progress

- [x] Gap analysis written (this file)
- [x] Before screenshots (`/tmp/mp-shots/before`, real daemon, isolated `SMIND_HOME`)
- [x] Step 1 — `text-match` + `model-browser` logic + tests
- [x] Step 2 — glyphs + settings intent
- [x] Step 3 — picker UI rebuild (+ review fixes)
- [x] Step 4 — toolbar/composer wiring, profile summary names the model, test migration
- [x] Step 5 — screenshots after + adversarial review

## Peer log

- `peer-glm` (`0643c380`, Step 1 pure logic): **ACCEPT** (`c41d0185`, `45d1f60c`).
  Lead fix `resolveTriggerTitle`: the Lead's own brief de-duplicated on
  "label starts with the provider name", which dropped the model from the
  title of `GLM · GLM 5.3`; now only an equal label is de-duplicated (test added).
- `peer-sonnet` (`8fcad3f8`, Step 2 glyphs + settings intent): **ACCEPT**
  (`11137eeb`, `3b912825`, `cbb8871a`).
- `peer-sonnet` (`d6ac36ec`, Step 3 picker rebuild, then the review fixes and
  the 17-test migration): **ACCEPT** (`1c33a964`, `ace62456`, `fef9b5ce`,
  `e6f6495f`, `3b3c8f5f`). Its REOPEN_REQUEST on the title bug was correct.
- `peer-glm` (`0a1b80e8`, test migration): **REJECT** — hit the 20-turn limit
  without a commit; the work was given to `peer-sonnet`.
- `reviewer` (`81f00633`, read-only adversarial review, 15 findings): fixed
  sheet height (was content-height: `data-[side=bottom]:h-auto` beat
  `h-[80dvh]`), keyboard route to Settings (pencil/gear are tab stops), Back
  losing focus, "No models match" while loading, sheet autofocus with a fine
  pointer, hidden row action on touch >= md, unhealthy-provider notice, Create
  profile row seeding the provider, bound-note height, thinking seed only for
  Claude, provider.list 8 s give-up. Left as residual: a11y nesting of the row
  action `<button>` inside `role=option`, highlight not re-seeded when data
  lands after open, bound chat whose provider is missing from the catalog,
  single-slot settings intent can go stale if Settings never mounts.
- Lead: wiring (`7778afab`), profile summary names the model (`09b24506`),
  provider.list timeout (`713cbca7`), fine-pointer fix (`bfc37e79`), title fix,
  screenshot harness, rebase onto develop (#319 live-run toolbar: one conflict
  in `run-config-toolbar.tsx` memo deps).

## Validation

Verified 2026-10-12 on `fix/model-picker-paseo-parity` rebased on `origin/develop`
(`594f3725`).

- AC1-AC13: `lib/text-match.test.ts`, `lib/model-browser.test.ts`,
  `model-picker.test.tsx` (38), `provider-glyph.test.tsx`,
  `lib/settings-intent.test.ts`, `profiles-section.test.tsx` (intent tests),
  migrated `composer.test.tsx` / `run-config-toolbar.test.tsx` /
  `task-detail.test.tsx`.
- AC14: web `tsc -b` clean, full web suite 2227/2227; `task test` and
  `task lint` — see the PR body for the run.
- Screenshots (real daemon on an isolated `SMIND_HOME`/port, headless Chromium,
  light + dark, 1440x900 and 390x844 with touch emulation): before
  `/tmp/mp-shots/before`, after `/tmp/mp-shots/after` — closed trigger (GLM-5.3
  and Opus 5.5 selected), open on the current provider, root with profiles,
  provider views (Claude Code, GLM), ranked search, multi-token search, empty
  search, row hover actions, create-profile-from-model landing in Settings →
  Agents with the form seeded, narrow sheet.
- Not verified: Windows/desktop shell, a real phone keyboard, screen readers.
- Residual: no per-profile icon/colour (store + wire change), no per-provider
  Retry, Paseo's two-step compact sheet and toolbar density rules not ported
  (see the gap table), tab-only keyboard path to "create profile from this
  model" (the pencil reaches Settings → Agents instead).
