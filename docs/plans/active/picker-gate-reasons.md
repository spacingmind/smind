# Gate reasons per provider and model in the picker

Size: L (11) — scope 3 (daemon derivation + wire, MCP/Lead errors, web + mobile pickers), PRs 4, risk 2 (a new field on `provider.list`, a new refusal on non-human sends), decisions 3, verify 3 (UI frames, both themes, two widths).

The provider/model picker lets a user choose a provider or model that cannot run,
or greys it out with a bare "Unavailable" (web: only the PATH health check sets
`unavailableReason`). Run failures already get a typed class and a next action
(`docs/plans/active/run-failure-classes.md`, ADR-0050 in its PR1). This item is
the **preventive twin**: the same state, computed before the send, shown per row
with the same way out. One derivation helper on the daemon, one action type, no
client-side guessing.

Phase 1 (this PR) is the spec only. Nothing is implemented until the Steward
accepts it.

## Acceptance Criteria

Vocabulary and derivation (daemon; the client never computes a gate):

- AC1 A fixed reason set: `not_signed_in`, `out_of_plan`, `cooling_down`,
  `unsupported_by_harness`, `model_not_in_plan`. Nothing else is a gate reason
  (an uninstalled CLI stays the existing `unavailableReason`, see Decision D4).
- AC2 Each reason has exactly one failure-class twin (table below); a test fails
  when a class is added to the human-fix / limit set without a twin or a stated
  "no twin" entry.
- AC3 One helper, `gate.Derive` (package `internal/gate`, Decisions section),
  builds a `Gate{reason, until?, action}` from (provider, model, now) using only
  memory/DB reads. The picker payload, the MCP/Lead refusal and the run-start
  guard all call it; no second implementation exists (a source-scan test).
- AC4 `action` is the **same Go type** as the run failure action
  (`failure.Action{kind, target?, label, ...}`), same kind values
  (`sign_in`, `wait_until`, `switch_profile`, `pick_model`) plus two kinds added
  to the shared vocabulary (`clear_cooldown`, `pick_provider`). An unknown kind
  renders as its `label` (the failure spec's rule).
- AC5 Precedence is deterministic (see Precedence): at most one reason per row,
  chosen by a fixed order, with the loser reachable from the detail text only
  when it changes the action.
- AC6 A provider is gated only when **no** usable account/credential can serve it
  (several accounts: one healthy account ungates the provider; the gate then
  appears on the models that account list excludes).
- AC7 Reads are cache-only: opening the picker, `provider.list` and a gated send
  never make a live usage-endpoint call, never refresh a credential, never start a
  fetch (counted: a test with a fetcher that fails the test on any call).

Wire (additive, `omitempty`):

- AC8 `provider.list` entries gain `gate` (provider-level) and `modelGates`
  (`{modelId: Gate}`, only gated models). Absent when nothing is gated, so an old
  client and an old daemon interoperate (no installed base either way).
- AC9 `provider.updated` is published when a gate can have changed (account
  added/removed/credential replaced, cooldown set/cleared, usage report refreshed
  across an exhausted-threshold edge), coalesced, so an open picker refreshes.
- AC10 The gate wire shape round-trips the fleet wire (`internal/fleet/wire`) and
  tolerates an unknown `reason` (rendered as the label, not selectable).

Callers other than the picker (Lead/automation):

- AC11 MCP `task_new` / `task_send` (and CLI `smind task send`, automation
  starts) to a hard-gated provider/model return a typed error
  `{code: "gated", gate: {reason, until?, action}}` plus a one-line message that
  says what not to do, never a silent fallback to another provider.
- AC12 `cooling_down` keeps today's `ProviderCooldownError` semantics
  (non-human fast-fail, human send allowed) and is re-expressed as the same
  `gated` error (same text, now typed).
- AC13 A Lead can route on `reason` + `action.kind` from the MCP output alone.

UI:

- AC14 Web: a gated provider row and a gated model row stay visible, are
  `aria-disabled`, show the reason text and a Badge, and render one action control
  (rows below). Not selectable by click or keyboard; the action is.
- AC15 Keyboard: gated rows stay in the roving order (so the action is reachable),
  Enter runs the row's primary action instead of selecting, the action button is
  `Tab`-reachable, the reason is linked with `aria-describedby`; Esc/arrow keys
  unchanged.
- AC16 `not_signed_in` action deep-links Settings → Providers for that account
  or provider (existing `smind:open-settings` event, `sectionId: "providers"`);
  `out_of_plan` shows the reset time and a switch control when a fallback exists;
  `cooling_down` offers Clear only where the Human is allowed (`provider.clearCooldown`).
- AC17 Home landing, composer run-config toolbar and Settings → Agents profile
  form pickers all show the same gates (one component). The trigger chip shows a
  warning dot when the *selected* provider/model is gated, with the reason in its
  title.
- AC18 Mobile new-task picker shows the same reasons (label suffix + a notice line
  with the action) and blocks selecting a gated row.
- AC19 Frames, light and dark, at 1280 and 390, of: provider list with all five
  reasons, model list with `model_not_in_plan`, the keyboard-focused gated row,
  the action in flight, the trigger chip warning, the mobile picker; every frame
  looked at by the Lead (standing no-glitch rule, transition frames included).

Docs:

- AC20 ADR amendment pointers (ADR-0028 routing section, ADR-0041 F1, ADR-0046
  provider spec) and one paragraph in the run-failure-classes ADR (twin table),
  `docs/cli.md` via `task docs:cli` if the CLI text changes, the MCP
  instructions table.

## 1. Where gate state comes from today, and what is missing

| Reason | Source today | What is missing |
|---|---|---|
| `not_signed_in` | Accounts: `accounts.Registry.List()` (`internal/accounts/registry.go`, `Account{Provider, CredentialType, Models, OAuth.ExpiresAt, APIKey}`). `provider.test` (`internal/wsapi/handlers.go` `testProviderCredential`, ~2404) already derives "no account" / "all credentials expired" as free text, only on demand, for the web health check. Provider-level keys: provider secrets (ADR-0046 §4, `provider.list` secrets `set`). `ProviderInfo.CredentialKind` / `AccountProvider` (`internal/taskrunner/provider.go`) say which provider needs which. | The check is a string, not typed; it is per provider, not per account; OAuth refresh-token death is only known after a failed run (the failure spec's `auth_expired`). No "needs sign-in" account flag exists (the failure spec explicitly defers it). v1 derives from stored facts only: no account, API key empty, OAuth access token expired with no refresh token, required provider secret unset. A refresh token that the vendor revoked is unknowable cache-only; it gates after the first `auth_expired` run via the memory flag in D2. |
| `out_of_plan` | Plan windows: `quota.Reader.Peek(accountID)` (cache only), `Router.windowExhaustion` / `exhaustionDetail` (`internal/routing/router.go` ~223-290: threshold, overage, `AppliesTo(model)`, TTL aging) for accounts; `quota.ProviderUsage.Exhausted(instanceID, pct, now)` (`internal/quota/providerusage.go`) for provider instances (ADR-0028, `docs/plans/active/account-usage-windows.md`). Passive hard-rejection cooldowns: `Reader.Cooldown(accountID, model, now)` with reason `rejected` / `fallback`. | `exhaustionDetail` is private to the router, so no other caller can reuse the verdict. Export a read-only `routing.Exhaustion(accountID, model, now) (Verdict{Exhausted, TransientOnly, Until, Window})` (move, not copy); the picker must not reimplement threshold or overage rules. |
| `cooling_down` | `Registry.ProviderCooldowns()` (`internal/runs/cooldown.go`, store `provider_cooldowns`, ADR-0041 F1), already exposed as `provider.list` `cooldownUntil`; `ProviderCooldownError` fast-fail for non-human sends; `provider.clearCooldown` (human-only, `docs/plans/active/clear-provider-cooldown.md`, merged). | Only a bare timestamp today; no reason text, no action. The passive account back-off (`ReasonBackoff`, ≤ 60 s) is **not** a gate: surfaces must not show it as a rate limit (`passive_types.go`). |
| `unsupported_by_harness` | `ProviderInfo.Harness` / `Runner` (ADR-0046 §2), `Images` capability, `LiveModeSwitch`, `ModelsDiscovered` (an id outside a discovered catalogue is rejected up front, ADR-0025), the Codex rule that chat-completions-only endpoints cannot run through the Codex harness (ADR-0046 §6, today a runtime error), `ValidateModelSyntax`. | No capability table and no per-model "this harness cannot run it" fact. v1 gates only on facts the spec already encodes: endpoint kind vs harness (Codex + chat-only endpoint), a model id the discovered catalogue rejects, an instance whose `Extends` base harness lacks the feature the row would use. Anything fuzzier is out of scope (D4). |
| `model_not_in_plan` | `accounts.Account.MatchesModel` / `Models` globs (ADR-0020 §5): an account with a model list that matches none of the model id. The model catalogue: `ModelInfo` + `ProviderInfo.Models` (`internal/taskrunner/models.go`, ADR-0025). | Only a user-written glob list exists; the provider's real entitlement is unknown until a run fails with `model_not_in_plan`. v1 gates from the globs; learning from run failures is D2. |

Web health today: `provider.test` per provider on picker open
(`web/packages/ui/src/lib/model-browser.ts` `BrowserProvider.unavailableReason`,
`provider-model-picker.tsx` `isDisabledRow`) — one free-text reason, provider rows
only, never models. The gate generalises that slot: `unavailableReason` stays for
"not installed / health failed"; a gated row uses `gate`.

## Twin table (the shared vocabulary; AC2)

| Picker reason (preventive) | Run failure class (reactive; the failure spec) | Shared action kinds |
|---|---|---|
| `not_signed_in` | `auth_expired` | `sign_in` target `settings:providers?account=<id>` or `?provider=<id>` |
| `out_of_plan` | `plan_window_exhausted` | `wait_until` (resetsAt) / `switch_profile` target `profile:<id>` |
| `cooling_down` | the cooldown a `plan_window_exhausted` run sets (ADR-0041 F1); never `transient_429` | `wait_until` / `clear_cooldown` (new) |
| `model_not_in_plan` | `model_not_in_plan` | `pick_model` target `models:[...]` |
| `unsupported_by_harness` | none (a run would fail as `cli_exited` / a plain error; this is caught before) | `pick_provider` (new), or `pick_model` |
| (none) | `transient_429`, `rate_limited` residual, `cli_exited` | never gates a picker row |

Naming rule (D1): the picker keeps the Human's fixed names; the twin table is the
only mapping and is enforced by `TestTwinTable_CoversEveryHumanFixAndLimitClass`.
Same-named where the meaning is identical (`model_not_in_plan`).

## 2. Wire shape (additive, daemon-computed)

```
Gate        {reason, until?, action?, accountId?, window?}   // all omitempty except reason
  reason    "not_signed_in" | "out_of_plan" | "cooling_down" | "unsupported_by_harness" | "model_not_in_plan"
  until     RFC 3339 UTC (resetsAt / cooldown end; absent when unknown)
  action    failure.Action {kind, target?, label, models?[]}   // the failure spec's type, not a copy
  accountId the account the gate talks about (sign-in / out-of-plan with several accounts)
  window    plan window name when known ("five_hour", "weekly"), as the usage meters call it

provider.list entry  += gate?: Gate, modelGates?: { [modelId]: Gate }
```

Go: `type Gate struct` and `Derive` live in `internal/gate` (new, imports
`internal/runs/failure` for `Action` and the class names, `internal/accounts`,
`internal/quota`, `internal/routing`); `providerListEntry` gains `Gate *gate.Gate`
and `ModelGates map[string]gate.Gate` (`internal/wsapi/handlers.go` ~2271). The
map lives on the list entry, **not** on the shared `taskrunner.ModelInfo`, so the
cached catalogue stays immutable and unaware of accounts. TS: `ProviderInfo` in
`web/packages/ui/src/lib/api` types and `mobile/src/api.ts` get the same optional
fields; `BrowserProvider` / `ModelRow` carry `gate?`. Existing `cooldownUntil`
stays (one more reader depends on it); `gate.reason === "cooling_down"` is
consistent with it by test.

Who computes: `internal/gate.Derive(ctx Inputs, provider, model string, now)`
where `Inputs` is a small struct of read-only interfaces (accounts lister, quota
`Reader`, cooldown reader, provider spec lookup, profile fallback lookup), built
once in `cmd/smind` next to the router. No client, plugin or relay peer ever
derives a gate.

## 3. Precedence (several reasons apply)

Per row, first match wins; the order goes from "waiting will not help" to "waiting
helps":

1. `unsupported_by_harness` (structural; no account, wait or sign-in fixes it)
2. `not_signed_in` (a human fix; and an expired credential makes every usage
   figure meaningless)
3. `model_not_in_plan` (human fix, model rows only)
4. `out_of_plan` (a fetched/rejected window with a reset time; carries `until`
   and the switch option)
5. `cooling_down` (provider-wide cooldown, ADR-0041 F1)

`out_of_plan` beats `cooling_down` because it is the more specific fact and the
action is richer (the same state usually produces both, since a rejected window
sets the cooldown). `until` is the later of the two ends, so the picker never
promises an earlier time than the daemon will honour. A provider row takes its
own gate; a model row takes its own model gate; in the flat all-models view a model of a
gated provider shows the provider's gate (inherited, same text and action), and the
provider drill-in is blocked, so a model view only ever opens for an ungated
provider. Test:
`TestDerive_PrecedenceMatrix` over all 2^5 combinations.

Multi-account (AC6): provider gate = every candidate account is gated (the
reason of the first in precedence order across accounts, `accountId` of that
account, `until` the **earliest** end, because the first account to recover
ungates the provider). A model gate = every account whose globs/health admit the
model is gated.

## 4. UI

Components: existing `ProviderRow`, `ModelRowItem`, `RowShell`,
`UnavailableNotice` in `provider-model-picker-rows.tsx`; existing `Badge`
(`components/ui/badge`), `Button` size xs ghost, `Icons.*` from the registry
(no new icon outside `lib/icons.ts`), tokens only (`text-foreground-muted`,
`border`, the warning tokens already used by the rate-limit footer). No new
colour, no new primitive.

Gated row (both provider and model rows):

```
[icon] Claude Code (a@x.com)        [Sign in]   ← not_signed_in: muted label, Badge "Signed out", ghost button
[icon] Codex      Resets 17:35 · in 2h 3m   [Switch to GLM]  ← out_of_plan: reason text right-aligned like today's detail slot
[icon] GLM        Cooling down until 17:35  [Clear]          ← Clear only for a Human caller; hidden on relay without the right
[icon] Kimi       Not supported by the ACP harness           ← no action button when kind is info-only
[icon] opus-4     Not in your plan  [Pick another]           ← model row, model view
```

- Disabled-but-visible: reduced opacity on the icon/label only (the action stays
  full contrast), `aria-disabled`, no chevron, cursor default. The reason sits in
  the existing detail slot (truncate, `title` has the full sentence with date).
- Reset times render in the user's locale with a relative part, same formatter as
  the usage meters (one `formatReset` helper; AC test compares the two).
- Keyboard (AC15): the existing roving list currently *removes* disabled rows
  from the order (`provider-model-picker.tsx` ~272). Gated rows are different:
  they stay in the order. Enter on a gated row runs its primary action; there is
  no select. Health-`unavailableReason` rows keep today's behaviour (excluded).
  Tab moves into the row's button only when the row is active. Screen readers get
  `aria-disabled="true"` + `aria-describedby` → the reason id.
- Trigger chip (AC17): when the selected provider/model carries a gate, a small
  warning dot and `title` with the reason; the composer's Send is unchanged here
  (the send path owns refusal, section 5).
- Profiles band: a profile whose provider/model is gated shows the same reason
  text on its row and is not applicable (`onApplyProfile` blocked), same action.
- Sheet (< md): same rows in the bottom Sheet; the action button is at least 40 px
  high; the reason wraps to a second line instead of truncating on 390 px.
- Light and dark: tokens only; frames in both. Settings → Agents profile form
  (`variant="field"`) shows the same rows.
- Mobile (`mobile/src/screens/NewTaskScreen.tsx`, `mobile/src/providerCatalog.ts`):
  the native `Picker` cannot disable individual iOS items, so a gated option's
  label gets a suffix (`Codex — signed out`), `selectProvider` refuses a gated id
  (pure function, vitest-covered like the rest of `providerCatalog.ts`), and a
  notice line under the field shows the reason and the action: text for
  `sign_in` ("Sign in on the desktop app", until mobile gets a providers
  section), a real button for `clear_cooldown` and `switch_profile`. Native frames
  need the simulator; if unavailable, the PR says what was not verified.

## 5. Selectability and non-human callers

Recommendation (D3): a gated row is visible and **not selectable** for every
reason. `cooling_down` is the one the brief asked about ("selecting queues per
the existing whenBusy policy"). It does not hold: `whenBusy: queue` (ADR-0024)
queues behind a *busy chat*, and the held-retry machinery (`chat_queue.not_before`,
ADR-0041 F2b) exists only for orchestrator `wait` retries; a Human send into a
cooldown today simply runs and fails. Letting the picker select a cooling provider
would therefore promise a queue that does not exist. So `cooling_down` offers
`clear_cooldown` ("I reset it") and, with a fallback profile, `switch_profile`;
once cleared the row is selectable. A "send after reset" for Humans is a new
feature, listed as the alternative in D3, not v1.

Daemon-side refusal (AC11-AC13), in `run.start` / `task.prompt` before a run row
is created:

| Reason | Non-human caller (MCP `task_new`/`task_send`, automation, orchestrator) | Human caller (web/CLI) |
|---|---|---|
| `unsupported_by_harness`, `not_signed_in`, `model_not_in_plan` | refused: `gated` error with the typed gate; never a silent fallback to another provider | allowed through (advisory: the picker already blocks; a stale client or a CLI power user still gets the run and the failure class explains it) |
| `cooling_down` | refused (today's `ProviderCooldownError`, now typed as `gated`) | allowed (today's behaviour, unchanged) |
| `out_of_plan` | **not refused**: the verdict is cache-derived and routing + profile `wait`/`fallback` policy (ADR-0041 F2b) already own this path; refusing on a stale cache would block a send that routing would have rerouted | allowed |

Why `out_of_plan` is advisory for sends: it is the only reason derived from a
cache that can be stale; every other reason is derived from authoritative stored
facts. This is the single place the two twins differ by design.

Error text (Lead-facing, per the failure spec's AC11): "gated: not_signed_in
(account a@x.com). A human must sign in (Settings → Providers). Do not retry and
do not fall back to another provider." `mcp_instructions.go` gets the reason table
beside the failure-class table; `TestMCPInstructions_ListEveryGateReason`.

## 6. Caching and the usage-endpoint rate limit

Hard rule (memory: provider usage endpoints are rate-limited; ADR-0028 budgets):
`internal/gate` accepts only the read-only `quota.Reader` (`Peek`, `Cooldown`,
`TTL`), the accounts lister (SQLite read), the cooldown reader and in-memory specs.
It cannot reference `Poller.Touch/Refresh/Fetch` or `ProviderUsage.Touch/Refresh`
at compile time (the same interface trick the router uses). A stale or absent
report means **no `out_of_plan` gate** (unknown does not suppress a cooldown, does
not create a block; ADR-0028 §4). TTL aging of an exhausted window with no reset
time reuses the router's rule via the exported `routing.Exhaustion`. The
picker's `onOpen` keeps calling `provider.list` (memory/DB only); it must never
call `account.usage` or `provider.usage`. Recompute cost: one `accounts.List()` and
one `Peek` per account per `provider.list` call; the result is not cached beyond
the request (invalidation is by `provider.updated`, AC9), so there is no second
cache to go stale.

Test: `TestProviderList_Gates_NeverFetch` (a fetcher that calls `t.Fatal`;
`Poller` `Touch/Refresh` unreachable) and `TestGate_PackageImportsNoFetcher`
(import scan: `internal/gate` imports neither `quota.Poller` fetch methods nor
`internal/quota/fetch*`).

## Test scenarios (named; the implementer writes these first)

Derivation (`internal/gate`, table tests, fake readers):

1. `TestDerive_NotSignedIn_NoAccount` / `_ExpiredNoRefresh` / `_SecretUnset` / `_OneHealthyAccountUngates`.
2. `TestDerive_OutOfPlan_WindowOverThresholdWithReset` (carries `until`, `window`), `_OverageEnabledDoesNotGate`, `_ModelScopedWindowGatesOnlyThatModel`, `_StaleReportDoesNotGate`, `_NoReportDoesNotGate`.
3. `TestDerive_CoolingDown_ProviderCooldown` (`until` matches `cooldownUntil`), `_PassiveBackoffIsNotAGate`.
4. `TestDerive_UnsupportedByHarness_CodexChatOnlyEndpoint`, `_DiscoveredCatalogueRejects`.
5. `TestDerive_ModelNotInPlan_GlobListMisses` and `_NoListNoGate`.
6. `TestDerive_PrecedenceMatrix` (all combinations), `TestDerive_UntilIsLaterOfWindowAndCooldown`, `TestDerive_MultiAccountEarliestEnd`.
7. `TestTwinTable_CoversEveryHumanFixAndLimitClass`, `TestActionIsTheFailureActionType` (reflect: same type), `TestGate_NoSecondDerivation` (source scan: nobody else builds a `Gate`).

Daemon wiring:

8. `TestProviderList_CarriesGateAndModelGates` (additive; absent when nothing gated), `TestProviderList_Gates_NeverFetch`.
9. `TestProviderUpdated_PublishedOnGateEdges` (account add/delete, cooldown set/clear, usage edge, coalesced).
10. `TestRunStart_GatedNonHumanRefusedTyped` per hard reason, `TestRunStart_OutOfPlanNotRefused`, `TestRunStart_HumanNotRefused`, `TestProviderCooldownError_IsTypedGate`.
11. `TestMCP_GatedErrorRoutesOnReason` (message says not to retry / not to fall back for `not_signed_in`), `TestMCPInstructions_ListEveryGateReason`.
12. `TestFleetWire_GateRoundTrip_UnknownReasonTolerated`.

Web (vitest):

13. `provider-model-picker`: gated provider row per reason (text, Badge, action, `aria-disabled`, not selectable by click or Enter).
14. `provider-model-picker`: gated rows stay in arrow order; Enter runs the action (`smind:open-settings` with `sectionId: "providers"`; `provider.clearCooldown` call; fallback selects the other row); health-`unavailableReason` rows still excluded.
15. `model-browser`: `modelGates` map onto model rows; model view of a gated-model provider; profile row inherits.
16. Reset formatter equals the usage meter's (`formatReset` shared).
17. Trigger chip warning for a gated selection; home-landing, run-config-toolbar and profile-form pickers render the same row (one component).
18. Unknown reason renders its label, not selectable.

Mobile (vitest): 19. `selectProvider` refuses a gated id, `defaultSelection` skips a gated remembered provider, label suffix, notice per reason.

## Mutation table (one mutation each; the named test must FAIL, a build failure proves nothing)

| File | Mutation | Must fail |
|---|---|---|
| `internal/gate/derive.go` | swap `out_of_plan` and `cooling_down` in the order | `TestDerive_PrecedenceMatrix` |
| same | gate on a stale report | `TestDerive_OutOfPlan_StaleReportDoesNotGate` |
| same | one healthy account no longer ungates the provider | `TestDerive_NotSignedIn_OneHealthyAccountUngates` |
| same | treat `ReasonBackoff` as a gate | `TestDerive_CoolingDown_PassiveBackoffIsNotAGate` |
| `internal/gate/inputs.go` | accept a `Poller` (fetch-capable) instead of `quota.Reader` | `TestGate_PackageImportsNoFetcher` |
| `internal/wsapi/handlers.go` | drop `ModelGates` from the list entry | `TestProviderList_CarriesGateAndModelGates` |
| `internal/runs/cooldown.go` / run start | stop refusing a non-human send for `not_signed_in` | `TestRunStart_GatedNonHumanRefusedTyped` |
| same | refuse a send for `out_of_plan` | `TestRunStart_OutOfPlanNotRefused` |
| `web/.../provider-model-picker.tsx` | let `activate` select a gated provider row | picker gated-row test (13) |
| same | filter gated rows out of the roving order | keyboard test (14) |
| `web/.../provider-model-picker-rows.tsx` | render the sign-in action for `cooling_down` | per-reason row test (13) |
| `mobile/src/providerCatalog.ts` | `selectProvider` accepts a gated id | mobile test (19) |

## PR split (non-stacked, each from origin/develop)

PR1 starts only **after failure-classes PR1 (ADR-0050, `internal/runs/failure`,
`failure.Action`) merges**: the shared `Action` type lives there. Anything this
item needs inside that package (the two new action kinds, a `TwinOf` table entry)
is written as a small additive commit in this item's PR1, not touched in Lead 334's
branch ("after failure-classes PR1 merges").

| PR | Scope | Write scope (one owner) | Owner |
|---|---|---|---|
| PR1 derivation + wire | `internal/gate` (`Derive`, `Inputs`, twin table), export `routing.Exhaustion`, `provider.list` `gate`/`modelGates`, `provider.updated` edges, TS wire types, contract file (ADR-0045: wsapi) | `internal/gate/**`, `internal/routing/router.go`, `internal/wsapi/handlers.go` (+ new `provider_gate.go`), `internal/runs/cooldown.go`, `internal/fleet/wire`, `cmd/smind` wiring | Lead core; peer-glm for the table tests from my exact case list |
| PR2 non-human refusal | `gated` error for run start / MCP / CLI, typed `ProviderCooldownError`, MCP text + instructions table, docs | `internal/runs/**` (start path), `cmd/smind/mcp_*.go`, `cmd/smind/task*.go`, `docs/cli.md` | peer-glm with step briefs, Lead reviews and runs mutations |
| PR3 web picker | gated rows, model rows, keyboard, trigger chip, profile band, shared `formatReset`, frames, contract (UI) | `web/packages/ui/**` | peer-sonnet (screenshots); Lead reviews every frame |
| PR4 mobile | `providerCatalog.ts`, new-task notice, tests | `mobile/src/**` | peer-glm; native frames by the Lead if a simulator is available |

PR2, PR3 and PR4 start after PR1 merges (they read the wire), and are disjoint
from each other. Each PR: `task test`, `task lint`, one mutation per
load-bearing test, plan Progress/Validation updated, a validation contract
stored via `validation_contract_submit` for PR1 and PR3. Peers get the standing
brief: no `git push`, no `gh pr create`, no `git stash`, commit per step, the
Lead pushes.

Delegation plan: the spec phase is done by the Lead alone (reading + writing).
Implementation: GLM for derivation tables, wire plumbing and mobile; Sonnet for
frames. Parallel when PR1 is merged: PR2 + PR3 (two Peers), PR4 when one frees up.

## Decisions

Already decided in the repo: typed cooldown + clear action (ADR-0041 F1,
clear-provider-cooldown), cache-only routing reads (ADR-0028), harness kinds and
Codex chat-only constraint (ADR-0046), `smind:open-settings` deep link, `whenBusy`
(ADR-0024), the shared `Action` type and twin classes (run-failure-classes spec).

DECIDED 2026-11-01: D1-D5 below are all **Supervisor default 2026-11-01, Human may
veto** (the recommendation of each). Spec #767 accepted by the Steward.

DECISIONS (recommendation first, as recorded):

- **D1 Reason names.** Recommend: keep the five picker names above and map to the
  failure classes through one enforced twin table. Alternative: reuse the class
  names verbatim (`auth_expired` for a picker reason). The preventive and reactive
  states differ in meaning (never signed in vs refused just now) and the Human
  already named the picker set; the twin test keeps them from drifting.
- **D2 `model_not_in_plan` and the not-yet-failed `not_signed_in`.** Recommend v1
  from stored facts only (account model globs, expiry, unset secret). Also
  recommend a small in-memory (not persisted) "recently failed" flag set by a
  `model_not_in_plan` / `auth_expired` run, valid 1 hour or until the credential
  or model list changes, so the row gates right after the first failure instead
  of inviting the same failing send. Alternative: no flag (simpler, the row stays
  selectable until the Human reads the failed run), or persisted per
  (account, model) (survives restart, adds a table and a staleness problem).
- **D3 Selectability.** Recommend: visible, not selectable for all five reasons;
  `cooling_down` gets `clear_cooldown` (Human) and `switch_profile`, not a queue.
  Alternative: build a Human "send after reset" (a queued send with `not_before`,
  extends ADR-0024/ADR-0041 F2b) so a cooling or out-of-plan row is selectable
  with a banner; that is a feature of its own, size M, not in this item.
  Also: Human API sends stay advisory (not refused) for every reason; only
  non-human callers are refused. Alternative: refuse humans too for
  `unsupported_by_harness`.
- **D4 Vocabulary stays at five.** Recommend not adding `not_installed` or
  `health_failed`; those remain the existing free-text `unavailableReason`.
  Alternative: fold them in as a sixth reason `unavailable` with an install hint
  action, which would merge the two disabled-row paths.
- **D5 Package home.** Recommend a new `internal/gate` (imports `failure`,
  `accounts`, `quota`, `routing`; imported only by `wsapi`/`runs`/`cmd`). It
  avoids a cycle (`runs/failure` must not import accounts/quota). Alternative:
  put `Derive` in `internal/runs/failure` beside `NextAction`, one package for
  both twins, at the cost of widening that package's imports.

Mutation checks, evidence and frames are recorded under Validation as each PR
lands. Out of scope: live provider calls, new usage fetches, a persisted
"needs sign-in" account state (the failure spec defers it), a Human queued send,
mobile Settings → Providers, changing routing or failover behaviour.

## PR1 brief (ready to start once failure-classes PR1, #766, is on develop)

Start: `task_new baseRef origin/develop`; first `git ls-tree origin/develop
internal/runs/failure/` must list the package, and read its `Action` type and
`NextAction` before writing code (the field names here are assumed from the spec).
Peers: peer-sonnet only while GLM is rate limited. Briefs forbid `git push`,
`gh pr create`, `git stash`; commit per step; the Lead pushes.

Steps, one commit each (Lead does 1-3, a Peer may do 4-5 from the exact case list):

1. `internal/routing/router.go`: export `Exhaustion(accountID int64, model string,
   now time.Time) Verdict{Exhausted, TransientOnly bool; Until time.Time; Window
   string}` by moving `exhaustionDetail` + `windowExhaustion`; `selectAccount`
   calls it. No behaviour change: existing `router_*_test.go` stay green.
2. `internal/runs/failure`: add action kinds `clear_cooldown`, `pick_provider`
   (additive consts only).
3. `internal/gate/{gate.go,derive.go,inputs.go,twin.go}`: `Gate`, `Inputs`
   (interfaces: accounts lister, `quota.Reader`, cooldown reader, provider spec
   lookup, fallback profile lookup), `Derive`, `TwinOf`.
4. `internal/gate/derive_test.go`, `twin_test.go`, `import_test.go`: test
   scenarios 1-7 above by name.
5. `internal/wsapi/provider_gate.go` (new) + `handlers.go` `providerListEntry`
   (`Gate`, `ModelGates`), `providerListResultFor` fills them; wire `Inputs` in
   `cmd/smind` next to the router; `provider.updated` on gate edges (account
   notifier, cooldown set/clear in `internal/runs/cooldown.go`, usage `onFetch`
   edge, coalesced); `internal/fleet/wire` field; TS types in
   `web/packages/ui/src/lib` provider types and `mobile/src/api.ts`.
   Tests: scenarios 8, 9, 12.
6. Contract `docs/plans/active/picker-gate-reasons-pr1.contract.md`, stored with
   `validation_contract_submit`; then `validation_request`.

Checks: `task test`, `task lint`, one mutation per row of the PR1 mutation
table, `HOME=$(mktemp -d)` with shared GOMODCACHE/GOCACHE exported first and the
temp HOME removed after. PR2 brief (after PR1 merges): `internal/runs` start path
guard calling `gate.Derive` for non-human callers, typed `gated` error,
`cmd/smind/mcp_*.go` text + instructions table, tests 10-11. PR3 brief: picker
rows/keyboard/chip per section 4, tests 13-18, frames by peer-sonnet. PR4:
`mobile/src/providerCatalog.ts`, `NewTaskScreen.tsx`, test 19.

## Findings during PR1 (corrections to the spec above)

- **F1 smind accounts are not a launch prerequisite.** Runs use the CLI's own login
  (ADR-0020 decision 1; `launchEnvEntries` reads only provider secrets), so "no smind
  account" or an expired account OAuth token does NOT mean the row cannot run, and the
  account model globs (ADR-0020 §5) only matter to the proxy. Consequences: the
  account-based `not_signed_in` and glob-based `model_not_in_plan` of section 1 are NOT
  derived. `not_signed_in` is hard only for an unset required provider secret (a launch
  fails on it) and advisory after a recent `auth_expired` run; `model_not_in_plan` is
  advisory, from a recent failed run (D2's flag).
- **F2 `advisory` gates.** `Gate.advisory` marks a gate resting on cached or inferred
  evidence (account windows, recent failures): the UI warns but keeps the row
  selectable (stranding a Human out of Claude because an unrelated smind account is
  spent would be worse than the warning). Only hard gates refuse non-human sends,
  which refines D3 ("not selectable for all five"): the UI PR makes hard gates not
  selectable and advisory gates selectable with a warning.
- **F3** `provider_list` over the fleet wire does not exist, so AC10's fleet round trip
  has nothing to carry in PR1; the gate rides `provider.list` (wsapi/relay) only.
- **F4** The provider-instance window edge (`quota.ProviderUsage` fetches) does not publish
  `provider.updated` yet; account usage events, failures, credential changes and
  cooldown changes do (coalesced by a gate signature). A `until` in the past is treated as
  ungated by clients (UI PR).
- **F5** `Gate.window` and the `switch_profile` fallback of `out_of_plan`/`cooling_down`
  are not in PR1: the fallback profile is per profile, not per provider. UI PR can add it.

## Progress

- 2026-11-01 Phase 1: read ADR-0025/0028/0041/0046, the usage-windows, cooldown,
  providers-epic and run-failure-classes plans, `internal/{accounts,quota,routing}`,
  the `provider.list` handler, the web picker and the mobile new-task picker; this
  spec written.
- 2026-11-01 Spec accepted by the Steward (#767); D1-D5 recorded as Supervisor defaults; PR1 brief added; PR1 waits for #766 on develop.
- 2026-11-01 PR1 (daemon + wire + typed refusal + MCP text): `internal/gate`, `routing.Exhaustion`, `provider.list` gate/modelGates, `provider.updated` edges, `runs.GatedError`, `SetGate`/`SetFailureHook`, TS types. UI (PR3) and mobile (PR4) not started.

## Validation

PR1 (daemon, wire, typed refusal, MCP text; branch gate-reasons-pr1):

- AC1/AC2/AC3: `TestTwinTable_CoversEveryHumanFixAndLimitClass`, `TestGate_NoSecondDerivation` (internal/gate).
- AC4: gate uses `failure.Action` and `failure.NextAction`; kinds `clear_cooldown`, `pick_provider` added.
- AC5: `TestDerive_PrecedenceMatrix` (all 32 combinations).
- AC6: `TestDerive_OutOfPlan_OneHealthyAccountUngates`, `..._AllAccountsExhaustedIsAdvisoryWithReset` (earliest end). Account-based `not_signed_in` dropped, see F1.
- AC7: `TestGate_PackageImportsNoFetcher` (no `internal/quota`/`accounts` import: no fetch path), `TestProviderList_Gates_OutOfPlanFromCachedWindowsIsAdvisory`.
- AC8: `TestProviderList_CarriesGateAndModelGates` (absent when nothing gated).
- AC9: `TestProviderUpdated_PublishedOnGateEdges` (coalesced), `TestProviderGate_AccountChangeClearsRecentFailures`.
- AC10: not applicable in PR1 (F3).
- AC11/AC12/AC13: `TestRunStart_GatedNonHumanRefusedTyped`, `TestRunStart_GatedErrorSaysWhatNotToDo`, `TestRunStart_AdvisoryGateNotRefused`, `TestRunStart_OutOfPlanNotRefused`, `TestRunStart_HumanNotRefusedByGate`, `TestProviderCooldownError_IsTypedGate`, `TestMCPInstructions_ListEveryGateReason`.
- AC14-AC19 (UI, mobile, frames): PR3/PR4, not done. AC20: ADR-0050 amendment + pointers in ADR-0028/0041/0046 done; `docs/cli.md` unchanged (no CLI help text changed).
- Mutations (each failed its named test, none a build failure): see the PR body.

Each AC above is confirmed here with the test name and the
mutation result when its PR lands; the plan moves to `docs/plans/completed/`
when all of AC1-AC20 are confirmed.
