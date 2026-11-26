# Browser for agents, PR 5: the provider gating table

Governing: ADR-0061 section 9 (which providers can use it), AC8 of `desktop-browser-for-agents.md`, and the 2026-11-25 Human decision to resume the lane (PR 5 only; the Steward names the next item). PRs 1 to 4 are merged; `browser.GateReady` is true; the per-workspace switch is off by default and human-only, so this PR changes who an enabled workspace reaches, not whether anything is reachable by default.

Size: M (5) — scope 2, risk 3 (it decides who can reach the browser). HIGH-RISK: foreground two-sub-agent review (standards, attack), then the Steward's own and mutations.

## Why

Today the decision "does this provider get `browser_*`" is spread over four places: `browserRowDelivered` (a plugin provider never does), the codex-native branch (`deliver=false`), `spec.NoMCPServers` (a per-spec flag), and `imagesReadFrom(info.Images)` (screenshots). A new provider (a registry entry, a compatible recipe, a plugin) silently gets whichever behaviour the code path happens to give it, and nothing says so to the person. This PR puts the decision in ONE table in code, with a row for every registered provider, a test that fails when a provider has no row, and text that says what a provider without the browser gets.

An observed gap this closes: a provider **instance** (a compatible recipe such as one pointed at another vendor's endpoint) inherits its base's `Images: "yes"`, so a model that cannot read images would be sent screenshots. Instances are treated as images `unknown` (= no images) for the browser.

## The table (`internal/providers/browser_reach.go`)

```go
type Reach string // ReachYes | ReachConditional | ReachNo
type ReachRow struct {
    Reach     Reach
    Mechanism string // how the row reaches the agent ("--mcp-config", "session/new mcpServers", "none")
    Why       string // one sentence a person can read (shown for ReachNo and ReachConditional)
    Images    string // "" = use the provider's catalogue Images; else "yes" | "no" | "unknown"
}
```

- `ReachYes`: the row is built and handed to the agent. `ReachConditional`: it is handed over, but whether the agent uses it is not guaranteed (an ACP agent that may not advertise MCP; a compatible endpoint whose model may ignore tools). `ReachNo`: no row is built and no socket opened.
- Rows keyed by **provider id** for every built-in (`claude-native`, `codex-native`, `glm`, `kimi`, `opencode`, `copilot`, `cline`, `goose`, `cursor`), by **recipe id** for every compatible recipe (`recipes.yaml`), and three **class rows** for entries that have no fixed id: `class:custom-acp` (a `providers.custom` ACP entry), `class:plugin` (a plugin provider, always no) and `class:instance` (an instance with no recipe: inherits the base's reach, images unknown).
- `BrowserReachOf(spec Spec) ReachRow` resolves a registered spec: plugin first (no), then `NoMCPServers` (no, whatever the table says: the agent cannot take the row), then recipe row (`spec.Recipe`), then the id row, then the class row (custom ACP, instance via its base), else **no** (an unknown provider never gets the browser).
- `(ReachRow).Offered()` is Yes or Conditional. `BrowserImages(spec)` is the effective image capability: the row's `Images` when set, `unknown` for an instance, else `spec.Images`; the broker reads images only for `"yes"` (unknown = no images, as already pinned by `imagesReadFrom`).
- `BrowserUnavailableMessage(spec)` is the text for a provider without the browser: `the browser is not available for this agent (<label>): <why>`.

Initial rows (evidence in `desktop-browser-for-agents.md`, "Provider reach"): claude-native yes (`--mcp-config`, CLI pid); codex-native no (no per-thread MCP); glm conditional (an ACP agent), images no; kimi, opencode, copilot, cline, goose, cursor conditional (ACP `session/new`; images as in the catalogue); class custom-acp conditional, images unknown; class plugin no; recipes whose harness is `claude-code` conditional with images unknown, `codex-app-server` no, `acp` conditional.

## Acceptance criteria

- AC1 Every provider in `providers.New().List()` and every recipe in `providers.Recipes()` has a reach row, and every row key names a registered provider, a recipe or a class (no stale rows). A provider registered without a row fails `TestBrowserReachDriftEveryProviderHasARow`.
- AC2 `BrowserReachOf` resolves: a plugin provider is No even when its manifest declares MCP; a spec with `NoMCPServers` is No; a recipe instance uses its recipe's row; a custom ACP entry uses the class row; an unknown id is No.
- AC3 The runner builds the browser row only when `BrowserReachOf(spec).Offered()`, on the ACP path and the claude path; codex-native builds none (unchanged). No socket is opened for a provider that is No.
- AC4 Images: a provider whose effective images are not `"yes"` (including every instance and every `unknown`) gets the text note for a screenshot, never an image block; claude-native (yes) and opencode (yes) get the image.
- AC5 At call time the daemon refuses a run whose provider is not offered with `the browser is not available for this agent (<label>): <why>`, before the permission gate, so no card is raised for it and the agent does not read a generic "daemon closed the socket".
- AC6 `smind browser enable` and `smind browser status`, the settings view (`browser.settings.get`, a new `providers` list of `{id, label, reach, why, images}`), and `public-docs/browser.md` name what a provider without a row or with `no` gets, and list the providers that cannot use it. The settings list is produced from the table (a test pins that it matches).
- AC7 Nothing widens: with the table in place, no provider that did not get the row before gets it (`TestReachTableNeverWidensTheOldRule`: the set of registered built-in providers with the row equals the pre-change set).

## Named tests

- `TestBrowserReachDriftEveryProviderHasARow`, `TestBrowserReachNoStaleRows`, `TestBrowserReachRecipeHarnessConsistent` (a recipe's row agrees with its harness: claude-code never No, codex-app-server always No) — `internal/providers/browser_reach_test.go`
- `TestBrowserReachResolution` (AC2, table-driven: plugin with MCP, NoMCPServers, recipe instance, custom ACP, plain instance, unknown id), `TestBrowserImagesInstanceIsUnknown`, `TestBrowserUnavailableMessageNamesTheProvider`
- `TestBrowserRowOnlyForOfferedProviders` (runner: a fake registry with a No provider on the ACP path builds no row and opens no socket; a Conditional one does) — `internal/taskrunner/mcp_browser_test.go`
- `TestCallRunRefusesProviderWithoutTheBrowser` (no card raised, message names the provider), `TestImagesReadFollowsReachTable` — `internal/wsapi/browser_call_test.go`
- `TestBrowserSettingsListsProviders` pinned against the table; CLI status test for the text — `internal/wsapi/browser_settings_test.go`, `cmd/smind/browser_test.go`
- A docs drift test: every provider id in the table that is No appears in `public-docs/browser.md` (by label), run by `go test ./internal/providers`.

## Mutations (committed files only, each must fail a named test)

- A provider row deleted from the table -> the drift test.
- A plugin provider's row set to Yes -> `TestBrowserReachResolution`.
- `NoMCPServers` check removed from `BrowserReachOf` -> `TestBrowserReachResolution`.
- `Images` for an instance left as the base's -> `TestBrowserImagesInstanceIsUnknown`.
- The call-time refusal removed from `callRun` -> `TestCallRunRefusesProviderWithoutTheBrowser`.
- `browserRowDelivered` ignoring the table (back to plugin-only) -> `TestBrowserRowOnlyForOfferedProviders`.

## Decisions

1. **One table in the leaf `providers` package**, next to the Spec it describes, not in `browser` (which imports nothing of the registry). The drift test needs the registry and the recipes, both here.
2. **Images follow the table, not the catalogue alone**, because instances inherit the base's `Images`; the catalogue field stays as is (it also drives attachments, ADR-0038, which this PR does not change).
3. **Unknown provider = No.** Fail closed.
4. **Conditional is offered**, and says so in the text ("offered, not guaranteed"); it is not a promise that a model calls the tools.
5. No new RPC, no new IPC command, no store change: the settings view gains a read-only field on an existing human-only method.

## Not in this PR

Settings UI (B3), persistent profiles, the live-model run, desktop host, Windows, codex-native support, a per-agent capability probe at runtime.

## Progress

- [x] Spec committed (this file)
- [x] Table and drift tests (`internal/providers/browser_reach.go`, `browser_reach_test.go`)
- [x] Wiring: runner (ACP and claude paths), call time (before the gate and again after a card), images
- [x] Settings view (`providers` list, with `images`), CLI status/enable text, `public-docs/browser.md`
- [x] Two-sub-agent review (standards, attack), mutations, full test run

## Validation

- AC1: `TestBrowserReachDriftEveryProviderHasARow`, `TestBrowserReachNoStaleRows`, `TestBrowserReachRecipeHarnessConsistent` (all pass; deleting the kimi row fails the drift test).
- AC2: `TestBrowserReachResolution` (10 cases plus "a recipe cannot lift a no base", found by the attack review).
- AC3: `TestBrowserRowOnlyForOfferedProviders` (decision table) and `TestClaudePath_ReachTableDecidesTheSocket` (a claude-harness instance without MCP servers opens no socket; an offered one does). The ACP call site is covered at runner level by `TestACPPath_ReachTableDecidesTheSocket` (no-MCP agent: no socket; offered agent: one socket; unknown provider: never starts) and `TestACPPath_PluginProviderGetsNoBrowserSocket` (a plugin provider whose manifest declares MCP).
- AC4: `TestBrowserImagesInstanceIsUnknown`, `TestImagesReadFollowsReachTable`.
- AC5: `TestCallRunRefusesProviderWithoutTheBrowser` (no card raised), `TestCallRunRechecksTheProviderAfterTheCard`.
- AC6: `TestBrowserSettingsListsProviders`, `TestBrowserCLI_StatusNamesAgentsWithoutTheBrowser`, `TestBrowserDocsNameEveryNoProvider` (checks the doc's table rows).
- AC7: `TestReachTableNeverWidensTheOldRule` (built-ins, instances with every recipe, plugin and no-MCP specs).
- Mutations (committed files, each fails a named test): row deleted, plugin row yes, `NoMCPServers` check removed, instance images inherited, call-time refusal removed, post-card re-check removed, `browserRowDelivered` ignoring the table, claude path ignoring the table, codex row yes, a recipe lifting a no base.
- Reviews: standards and attack sub-agents; accepted and fixed: recipe row beating a no base (display only), no provider re-check after the card, missing claude-path wiring test, weak AC7 and docs tests, doc and plan wording. Steward review round 1 (accepted and fixed): the docs now have a row for the Codex-harness recipe, plain instances and instances of custom entries, and the docs test reads recipe and class rows; the reason for a Codex-harness recipe names the harness; the instance comment and Mechanism say it is the fixed class row; the AC7 test asserts the exact offered set.
- Not verified: a live run with a real model, Windows CI.
