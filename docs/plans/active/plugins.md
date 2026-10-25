# Plugin system (ADR-0042)

Status: **spec only, 2026-10-24.** [ADR-0042](../../decisions/0042-plugin-system.md) is **Proposed**; no implementation starts until the Human answers its open
questions. This plan assumes the ADR's recommendations; where an answer changes the plan, the item says so.

Size: **XL (10)** by the protocol table — scope 2 (daemon, web, desktop via web, mobile), PRs 2 (about eighteen, with ordering), risk 2 (code execution, install
paths, relay/E2EE surface), decisions 2 (ADR-0042 and twelve questions), verification 2 (real macOS/Windows daemon, 390 px mobile web smoke plus a device pass for the
renderer). Per `docs/WORKSPACE_PROTOCOL.md`, XL is split into L items with one Lead each and disjoint write scopes:

| Item | Wave | Scope | Size | Starts when |
|---|---|---|---|---|
| **P0** Provider registry | 0 | daemon + web/mobile catalog types | L (6): scope 2, PRs 1, risk 1, decisions 0, verify 2 | ADR accepted (Q9); does not need plugins accepted |
| **PA** Host + declarative contributions | A | daemon, web settings page, small mobile | L (7): scope 2, PRs 2, risk 1, decisions 1, verify 1 | ADR accepted (Q1–Q4) |
| **PB** Process + hooks | B | daemon only (plus examples) | L (7): scope 1, PRs 1, risk 2, decisions 1, verify 2 | PA's PA1–PA3 merged; Q7, Q8 answered |
| **PC** Declarative UI | C | web + mobile + a thin daemon schema | L (8): scope 2, PRs 2, risk 1, decisions 1, verify 2 | PA merged, PB1–PB2 merged; Q5 answered |
| **PD** Providers as plugins | D | daemon + docs | M (4): scope 1, PRs 1, risk 1, decisions 0, verify 1 | P0 and PA merged |

Every PR is **non-stacked off `develop`**: a PR that needs an earlier PR's code waits for it to merge (no `reset --soft origin/develop` history rewrites). Peers
are Sonnet-class or GLM per the standing model rule, never Opus; `refs/` is read-only reference. Item write scopes: P0 owns `internal/taskrunner/provider*.go`,
`internal/config` provider keys and the two catalog files; PA owns the new `internal/plugins` and the plugin store tables; PB owns `internal/plugins/host`; PC owns
the new web/mobile renderer directories; no item edits another's files without a `DEPENDENCY_REQUEST`.

## Acceptance Criteria

Numbered so a reviewer can grade each independently. Items name which ACs they own in brackets.

### Foundation and trust (PA)
- AC1 [PA] `smind plugin install <dir>` copies a valid plugin to `$SMIND_HOME/plugins/<id>/`, records it in the `plugins` table **disabled**, and prints the consent
  summary (what runs, capabilities, source, version, hash). Enabling requires explicit confirmation (`--yes` or the UI button).
- AC2 [PA] With `plugins.enabled: false` (the default), no plugin contribution is loaded, listed as active or callable, and `plugin.list` reports the global switch.
- AC3 [PA] Install, enable, disable, remove, update, settings edit and reload are rejected with `forbidden` for agent callers (run-scoped sessions) and for relay
  sessions; `plugin.list`/`plugin.get`/`plugin.view` stay readable over the relay.
- AC4 [PA] Install sources: directory, link (not copied), Git pinned to a commit, archive pinned to a sha256. An unpinned Git ref or a hash mismatch fails with a
  typed error and leaves no files behind.
- AC5 [PA] Archive/dir extraction rejects path escape (`..`, absolute, drive letters), symlinks, more than the entry/size caps, and a manifest over its size cap.
- AC6 [PA] The manifest is validated: id grammar, `api` in the supported set, `engines.smind` satisfied by the running daemon version (prereleases handled per
  semver), unknown keys inside `engines`/`capabilities` rejected, unknown top-level keys ignored; every error names the field and the fix.
- AC7 [PA] An update that changes the declared capabilities, process command, MCP/ACP commands or hook events stays pending until the Human re-consents; the previous
  version keeps running meanwhile. A failed install/update/reload leaves the last good version active and visible with its error.
- AC8 [PA] `remove` deletes the plugin's files, rows and projections, and deletes `data/` unless `--keep-data`; `link`ed sources are never deleted.
- AC9 [PA] A repository's contents can never install, enable or configure a plugin (ADR-0024); nothing under a workspace path is read as a plugin source unless
  the Human passes it explicitly to `install`/`link`.

### Declarative contributions (PA)
- AC10 [PA] An enabled plugin's `mcpServers` appear in `mcp.list` as owned rows named `<id>:<name>`, read-only in the UI and over `mcp.update/delete`, are injected into
  runs exactly as user rows (ACP and claude-native), and disappear when the plugin is disabled or removed. Secrets from plugin settings are substituted only into
  env/headers and are redacted on every read path.
- AC11 [PA] Plugin `skills` are delivered through the read-only projection (never written under `~/.claude/skills`, repo `.claude/skills` or `.agents/skills`);
  `AgentProfile.Skills` accepts `<plugin>:<skill>`; a skill from a disabled plugin is neither listed nor delivered.
- AC12 [PA] Plugin `profiles` appear as owned profiles (read-only, `<id>:<name>`), including seats; a profile that raises permission mode above the user default is
  flagged on the consent screen.
- AC13 [PA] Prompt slash commands: typing `/` in the web composer shows plugin commands (name, description, source plugin); selecting one inserts its prompt text
  into the composer (never auto-sent); built-in commands win collisions, and the first plugin by id wins between plugins.
- AC14 [PA] Themes: a plugin theme with a complete token set is selectable in Settings → Appearance on web; an incomplete or invalid set is rejected at install with
  the missing tokens listed; the theme works in both the plugin's declared scheme and does not change the other scheme.
- AC15 [PA] Settings → Plugins (web) lists plugins with status, version, source, errors; shows the consent summary; has enable, reload, logs, remove; labels every
  plugin-owned row elsewhere in the app ("from plugin X"). Mobile lists plugins read-only and offers their prompt commands and themes.

### Plugin process and hooks (PB)
- AC16 [PB] An enabled plugin with `process` is spawned with a scrubbed environment (no `SMIND_*` secrets, allowlisted `PATH`), its own cwd and `data/` dir; stdio is
  LSP-framed JSON-RPC 2.0, bidirectional; `plugin.initialize` carries `api`, daemon version, dataDir and decrypted settings.
- AC17 [PB] Host calls from a plugin run through a plugin-scoped session: a method outside the plugin's declared capability groups returns `forbidden`; a method not
  in the documented plugin API never resolves; a test enumerates the capability table and asserts every method exists in `methodHandlers`.
- AC18 [PB] Lifecycle: start, ready, crash, restart with backoff, and a breaker after N crashes in M minutes (plugin marked `failed`, surfaced in UI/CLI); stop is
  SIGTERM, 5 s, then process-group kill (job object on Windows); reload and disable and daemon shutdown leave no orphan processes.
- AC19 [PB] `events.observe` delivers only allowlisted topics, live and best effort (no replay), redacted copies, with a bounded queue and `event.dropped` on overflow;
  a plugin that stops reading never delays other subscribers.
- AC20 [PB] `run.beforeStart` may append instructions, add env, add MCP servers the plugin itself contributes; any attempt to change provider, cwd, permission mode or
  profile fails the hook with a typed error; 10 s timeout; `onError: open` runs the task and surfaces the failure, `closed` fails the start with the reason; ordering
  by plugin id then registration; no deep merge.
- AC21 [PB] `task.send` from a plugin queues with source `plugin:<id>` and a `[message from plugin <id>]` header, is rate limited per plugin and per task tree, cannot
  create children beyond `orchestration.maxDepth` or the tree guard, and a hook-triggered send loop is stopped by the limiter with a visible reason.
- AC22 [PB] There is no hook or API that answers a permission request (ADR-0019); observing `permission.pending` works.
- AC23 [PB] `plugin.call {pluginId, rpc, params}` routes to the plugin's `rpc.<name>` with a timeout and size cap and returns typed errors; per-plugin logs are bounded,
  redact secrets, and are readable via CLI/UI/wsapi.

### Declarative UI (PC)
- AC24 [PC] The view schema (`ui: 1`) is defined once, validated in Go (size, depth and node-count caps), with a shared JSON fixture suite covering every node type
  and every action, consumed by both web and mobile tests.
- AC25 [PC] A panel contributed with `where: "task"` opens as a `plugin` tab in the task's tab area (`${taskId}:plugin:${pluginId}:${panelId}`), is restorable, and
  closes cleanly when the plugin is disabled; a sidebar item opens it; palette entries appear in Ctrl-K.
- AC26 [PC] Web and mobile render every node type; an unknown node type renders an "unsupported in this app version" placeholder and does not crash; clients report
  their `ui` level and the daemon withholds panels that need a higher level.
- AC27 [PC] Actions: `rpc`, `prompt` (inserts, never sends), `command`, `openPanel`, `openUrl` (OS browser behind a confirm showing the host). A view cannot load a
  remote image, run script or inject HTML: markdown raw HTML is stripped, remote image URLs are not fetched, inline SVG follows the 64 KiB / no-script rules.
- AC28 [PC] Panel data is react-query keyed by `(pluginId, panelId, context)`, refreshed by `plugin.event` and `plugin.updated`; no per-plugin cache or store is created
  (ADR-0032); UI-only state uses the shared zustand stores.
- AC29 [PC] Colors come from theme tokens (`tone`), icons from the client Lucide registry or sanitized SVG; the same panel is checked in light and dark on web and
  at 390 px on mobile.
- AC30 [PC] Over the relay, view documents and `plugin.call` are ordinary wsapi frames; a test asserts no plugin code, URL fetch or eval path exists on the mobile
  client (a lint rule bans `eval`/`new Function` in `mobile/src` and the plugin renderer directories) and that a view above the frame cap is refused with a typed
  error, not truncated.

### Providers (P0, PD)
- AC31 [P0] The provider catalog is a registry: adding an ACP provider needs one registry entry and no edits to `runner.go`'s switch, `modes.go`, the wsapi diag map,
  learning delivery's list, or the client unions; `provider.list` is the single source and web/mobile treat ids as strings with an offline fallback list.
- AC32 [P0] `providers.custom` in `config.yaml` adds an ACP provider (command, args, env, models, modes); a bad entry is rejected at startup/reload with the field named;
  runs use `runACP`, record run events/usage, honor model selection and permission modes.
- AC37 [P0] A `providers.custom` entry with `extends: claude-native|codex-native` adds a provider instance with its own id, label and allowlisted env
  (`CLAUDE_CONFIG_DIR` / `CODEX_HOME`); it runs through the extended provider's runner, side by side with the base provider, and appears in every picker, the
  health check and usage grouping. Settings → Providers suggests `~/.claude*` / `~/.codex*` directories found in the daemon user's home (names only, no file reads)
  and adds an instance in one click (human-only RPC).
- AC33 [PD] A plugin's `providers` entries register as `<plugin>/<provider>`, vanish with the plugin, and cannot shadow a core or custom provider id; the command in the
  entry is on the consent screen.

### Cross-cutting
- AC34 Every plugin-originated message, row, event and error is labeled with the plugin id in the UI and in `daemon.log`.
- AC35 Plugin authoring docs: `docs/plugins.md` (manifest reference, capability table, view node reference, hook semantics, versioning, trust statement, "what a plugin
  can do to your machine") and two examples in `plugin-examples/` that CI installs and exercises end to end.
- AC36 Release notes call out the breaking provider-id change (no installed base; no shim).

## Test Scenarios

Named up front. Go tests live next to the code; the named screenshots and smoke stages follow the repo's screenshot-verify rule (light + dark, every transition
frame reviewed).

**Trust and install (PA)**
- `TestInstall_Dir_RecordsDisabledAndShowsConsent`, `TestInstall_Requires_PluginsEnabled`, `TestEnable_NeedsConfirmation`
- `TestInstall_Rejected_ForAgentCaller`, `TestInstall_Rejected_OverRelaySession`, `TestPluginList_ReadableOverRelay`
- `TestSource_GitUnpinnedRef_Rejected`, `TestSource_ArchiveShaMismatch_NoFilesLeft`, `TestSource_Link_NotCopied_NotDeletedOnRemove`
- `TestExtract_PathEscape_Symlink_EntryCap_SizeCap_Rejected` (zip-slip, `..`, abs, `C:\`, symlink, bomb)
- `TestManifest_IdGrammar`, `TestManifest_ApiUnsupported`, `TestManifest_EnginesRange_Prerelease`, `TestManifest_UnknownEnginesKey_Rejected`, `TestManifest_UnknownTopLevel_Ignored`, `TestManifest_PathsConfined`
- `TestUpdate_CapabilityChange_StaysPendingUntilConsent_OldVersionKeepsRunning`, `TestUpdate_Failed_KeepsLastGood`
- `TestWorkspaceRepo_CannotInstallOrEnable` (a repo containing `smind-plugin.json` is inert)
- `TestRemove_DeletesDataUnlessKeep`, `TestStagedInstall_CrashMidwayLeavesNoPartial`

**Declarative contributions (PA)**
- `TestMcpContribution_OwnedReadOnly_InjectedAcpAndClaude_RemovedOnDisable`, `TestMcpContribution_SecretSubstitution_RedactedOnRead`
- `TestSkillContribution_ProjectionOnly_NeverWritesHomeSkillDirs`, `TestProfileSkills_AcceptsPluginNamespace`, `TestSkillFromDisabledPlugin_NotDelivered`
- `TestProfileContribution_OwnedSeat_PermissionEscalationFlagged`
- `TestNameCollision_CoreWins_ThenPluginIdOrder`, `TestNamespacing_NoShadowing`
- `TestSlashCommands_Autocomplete_InsertsNeverSends` (web component), `TestSlashCommands_BuiltinWinsCollision`
- `TestTheme_IncompleteTokenSet_RejectedWithMissingList`, `TestTheme_AppliesPerScheme`
- Web: `settings-plugins.spec` screenshots — empty, list, consent, error state, reload failure, owned-row badges, light/dark; mobile 390 px read-only list.

**Process and hooks (PB)**
- `TestProcess_ScrubbedEnv_NoSmindSecrets`, `TestProcess_StdioFraming_Bidirectional`, `TestProcess_InitializeCarriesSettings`
- `TestCapabilities_TableMethodsAllExist`, `TestCapabilities_UndeclaredGroup_Forbidden`, `TestCapabilities_UnlistedMethod_NotResolvable`
- `TestLifecycle_CrashBackoff_Breaker`, `TestLifecycle_StopEscalatesToKill_NoOrphans` (Unix process group; Windows job object under the Windows QA job)
- `TestReload_FailureKeepsLastGood`, `TestDisable_StopsProcessAndUnregisters`, `TestDaemonShutdown_StopsAllPlugins`
- `TestEvents_AllowlistOnly_RedactedCopies`, `TestEvents_SlowPlugin_DoesNotBlockBus`, `TestEvents_DroppedOnOverflow`
- `TestBeforeStart_AllowedEdits`, `TestBeforeStart_DisallowedEdit_ProviderCwdModeProfile`, `TestBeforeStart_Timeout_OpenAndClosed`, `TestBeforeStart_Ordering_NoDeepMerge`
- `TestTaskSend_PluginSource_Header_RateLimited`, `TestTaskSend_LoopStoppedByLimiter`, `TestTaskSend_CannotExceedMaxDepth`
- `TestNoPermissionAnswerSurface` (no capability maps to `permission.respond`)
- `TestPluginCall_Timeout_SizeCap_TypedErrors`, `TestLogs_Bounded_SecretsRedacted`
- Fixture: a tiny process plugin in Go test code (`httptest`-style), not a network dependency; a fake-LLM harness run asserts the beforeStart edits reach the agent launch.

**Declarative UI (PC)**
- `TestViewSchema_Validate_CapsDepthNodesBytes`, `TestViewSchema_FixtureSuite_Go` (golden fixtures shared with TS)
- Web: `plugin-view.test.tsx` renders each fixture; `unknown-node-placeholder`, `markdown-strips-raw-html`, `remote-image-not-fetched`, `openUrl-confirm-shows-host`, `prompt-action-inserts-not-sends`
- Mobile: `pluginView.test.tsx` runs the same fixtures; `lint: no eval / new Function` rule test; frame-cap refusal test
- `TestPanelWithheld_WhenClientUiLevelTooLow`, `TestPanelClosesOnDisable_TabRestorable`, `TestSidebarAndPalette_ContributionsRegister`
- `TestPlugin_Event_InvalidatesQuery_NoPerPluginStore`
- Screenshots: web panel in light/dark at desktop and narrow width; mobile 390 px light/dark; every transition frame (open panel, refresh, disable) captured and reviewed.
- E2EE smoke: the existing E2EE-v2 smoke overlay opens a plugin panel through the relay harness and asserts the relay-visible bytes are ciphertext only.

**Providers (P0, PD)**
- `TestProviderRegistry_AddACPProvider_NoSwitchEdits` (a table test registering a fake provider and exercising run, modes, diag, delivery, catalog), `TestProviderList_SingleSource`
- `TestCustomProvider_ConfigValidation_FieldNamed`, `TestCustomProvider_RunsViaACP_UsageAndEvents`, `TestCustomProvider_ModelSelectionAndModes`
- `TestProviderInstance_ExtendsClaudeNative_OwnConfigDir`, `TestProviderInstance_EnvAllowlistOnly`, `TestProviderInstance_SideBySideWithBase`,
  `TestProviderDirSuggestions_NamesOnly` (no file under a suggested dir is opened)
- Web/mobile: catalog renders an unknown provider id with a generic icon; offline fallback list; `provider-catalog.test` updated.
- `TestPluginProvider_NamespacedId_NoShadowing_VanishesWithPlugin`

**Docs and examples (cross-cutting)**
- `TestExamples_InstallAndExercise` (CI installs both examples against a built daemon, enables, hits each contribution), `TestDocs_CapabilityTable_MatchesCode` (the doc table is generated or diffed)

## PR split (non-stacked; each branches from `origin/develop`)

Ordering is by waiting for merges, not by stacking. "Needs" lists the PRs that must be merged first.

| PR | Item | Content | Needs |
|---|---|---|---|
| P0.1 | P0 | Provider registry in the daemon; `providers.custom`; remove the ten hardcoded touch points; tests | accepted ADR |
| P0.2 | P0 | Web + mobile catalogs: provider ids as strings, catalog from `provider.list`, offline fallback, generic icon | P0.1 |
| PA.1 | PA | `internal/plugins`: manifest, validation, store table, layout, staged install, sources (dir, link, git@sha, archive@sha256), CLI `smind plugin`, wsapi `plugin.*` with the local-only guards, global switch | accepted ADR |
| PA.2 | PA | Contributions: MCP, skills (projection), profiles, owner columns, namespacing, enable/disable propagation | PA.1 |
| PA.3 | PA | Prompt slash commands (daemon registry, web composer autocomplete, palette) and themes (web tokens, completeness check) | PA.2 |
| PA.4 | PA | Web Settings → Plugins page (list, consent, detail, logs, reload), owned-row badges across MCP/profile/skill UIs | PA.2 |
| PA.5 | PA | Mobile: read-only plugin list, prompt commands, themes (390 px verified) | PA.3 |
| PB.1 | PB | Process host: spawn, scrubbed env, LSP-framed JSON-RPC, lifecycle, breaker, logs, Windows job object | PA.1 |
| PB.2 | PB | Plugin-scoped session, capability table + test, `events.observe`, plugin settings (typed, secrets) | PB.1, PA.2 |
| PB.3 | PB | Hooks: `run.finished` observe, `run.beforeStart` with allowed edits, `task.send` source/limits | PB.2 |
| PB.4 | PB | `plugin.call` routing for RPCs and RPC-backed slash commands; example process plugin | PB.2, PA.3 |
| PC.1 | PC | View schema, Go validation, shared fixtures, `plugin.view`, `plugin.event`, client `ui` level in `hello`, frame caps | PB.2 |
| PC.2 | PC | Web renderer, `plugin` tab kind, sidebar and palette registries, tests and screenshots | PC.1 |
| PC.3 | PC | Mobile renderer and plugin panel screen, lint rule, E2EE smoke | PC.1 |
| PD.1 | PD | Plugin-contributed ACP providers; consent entry; docs | P0.1, PA.2 |
| DOC.1 | all | `docs/plugins.md` authoring guide, `plugin-examples/`, CI example job, release-notes entry | PB.4, PC.2 |

Parallel after PA.2 is merged: PB.1 and PA.3/PA.4 (disjoint scopes: `internal/plugins/host` vs. composer/settings UI). PC.2 and PC.3 can run in parallel after PC.1.
Reopen rules from the protocol apply: a PR that needs another item's file raises `DEPENDENCY_REQUEST`; a changed shared contract (capability table, view schema,
manifest) raises `REOPEN_REQUEST` to the Lead that owns it.

### P0.1 touch-point inventory (K1, 2026-10-25; ticked as each moves to the registry)

Found with `grep -rnE 'ProviderClaudeNative|ProviderCodexNative|ProviderGLM|ProviderKimi|"claude-native"|...' internal cmd`; the plan said ten, there are more.
`internal/providers` holds one `Spec` per provider; each hit below reads it instead.

- [x] `taskrunner/runner.go:453-461` RunPrompt dispatch switch -> `Spec.Runner`
- [x] `taskrunner/runner.go:343-344,263` default ACP commands / codexCommand -> `Spec.Command`
- [x] `taskrunner/runner.go:965,986,1009,1039,1088,1237,1273,1287,1301,1322`, `isolated.go:255`, `acp_modes.go:232`: launch env and session handle keyed by the base constant -> keyed by the provider (instance) id
- [x] `taskrunner/modes.go:125-127` staticModes -> `Spec.Modes`
- [x] `taskrunner/modes.go:211-245` ProviderCatalog (acpProvider / codex cases) -> `Spec.Runner`
- [x] `taskrunner/provider.go:136-148` SupportedProviders literal -> registry
- [x] `taskrunner/config_options.go:69-76,83,105,108` acpProvider and claude/codex checks -> `Spec.Runner`
- [x] `taskrunner/live_mode.go:45,56` -> `Spec.Runner`
- [x] `taskrunner/models.go:52` staticModels -> `Spec.Models`
- [x] `taskrunner/isolated.go:200`, `probe.go:72`, `attachments.go:92`, `model_switch.go:68` claude-only guards -> `Runner == claude`
- [x] `wsapi/handlers.go:2292-2296` providerCLICommand diag map -> `Spec.HealthCommand`
- [x] `wsapi/provider_env.go:70,91` env allowlist lookup by id -> by `Spec.Base()`
- [x] `learning/eligible.go:19-46` provider id list -> `Spec.ToolEvents/Reflect`
- [x] `learning/delivery/delivery.go:102,122,213`, `learning/probe/service/eligibility.go:147` -> `Spec.Delivery`
- [x] `runs/queue.go:802,829`, `runs/registry.go:2087` claude-only guards -> `Runner == claude`
- [x] `runs/permission_mode.go:71`, `profiles/registry.go:140` ProviderInfoFor -> registry
- [x] `config/config.go:165`, `config/edit.go:47,184`, `config/provider_env.go:29-30,96` id lists -> `providers.BuiltinIDs()`
- [x] `cmd/smind/serve.go:119-131` startup wiring of per-provider default model / env -> registry loop
- [x] `cmd/smind/task.go:602,861` (CLI note about claude-native's unpinned model) and `cmd/smind/learning_wire.go:131`: left as is on purpose (client-side wording / a legacy accounts check); recorded here. Also left on purpose: `learning/probe/service/eligibility.go:147` and the reflector/probe `Start(..., ProviderClaudeNative, ...)` calls: they name the BASE claude login deliberately, so an instance's run is not probed or reflected on another login

## Decisions

- ADR-0042 (Accepted 2026-10-25, all Q1–Q12 as recommended, plus provider instances in Wave 0 as AC37) holds the architecture: trusted local plugins, off by default, consent and mediated capabilities, data-first contributions, plugin process over
  stdio JSON-RPC, declarative view documents (no remote JS on clients), provider registry first, no registry/store, no OS sandbox in v1.
- Plan-level choices made here and reversible without an ADR change: the PR order and item boundaries above; Wave 0 (P0) can start on acceptance because it needs no
  plugin decision; example plugins are first-party fixtures, not a catalog.
- Human answers 2026-10-25: Q1–Q12 as recommended, so the plan stands; Wave 0 and Wave A start now, B–D wait for a Human decision after Wave A is on screen.
  Added AC37 (provider instances: `extends` + allowlisted env, directory suggestions) to P0.

## Progress

- 2026-10-24: Research done (local-only notes in `docs/research/local/plugins-research.md`); ADR-0042 and this plan written; docs PR opened for review. **No implementation
  has started; no Leads spawned.** Nothing below is checked until the ADR is accepted.
- [x] P0.1 (K1, daemon + AC37 daemon side)  [x] P0.2 (K2: web + mobile + Settings UI)
- [x] PA.1 (merged #630)  [x] PA.2 (merged #631)  [x] PA.3 (merged #634)  [x] PA.4 (#635)  [x] PA.5 (mobile, PR in review)
- [ ] PB.1  [ ] PB.2  [ ] PB.3  [ ] PB.4
- [ ] PC.1  [ ] PC.2  [ ] PC.3
- [ ] PD.1  [ ] DOC.1

- 2026-10-25 K1 (P0.1 + AC37 daemon): new leaf package `internal/providers` (one `Spec` per provider; `Default()` registry that config load writes and runner, learning, runs and profiles read). Runner dispatch, modes, models, env and session handles, the wsapi health check, learning delivery and the run-registry guards all read it. `providers.custom` (ACP entries and `extends` instances) in `internal/config/providers_custom.go`; validated at load and by `provider.reload`, field named. New human-only RPCs: `provider.reload`, `provider.addInstance`, `provider.dirSuggestions` (run-scoped callers get `codeForbidden`). `acp.WithEnv` added for a custom provider's command env. Decisions made on the way: `provider.list` keeps `kind` (auth classification, "cli") and adds `runner`, `extends`, `custom`; an instance clears `accountProvider`/`credentialKind` (it uses a CLI login in its own dir, not a smind account row) and is not a reflector source; run usage, cooldowns and `usage.summary` group by the provider id string so an instance is its own key with no code change; smind-held account plan windows (internal/accounts, quota) remain per account row, an instance has none; `provider.setEnv`/`setDefaultModel` stay for the four built-ins only (an instance's dir is edited in config.yaml and applied with `provider.reload`).

## Validation

Filled as items land; for each acceptance criterion, name the test or manual check that confirmed it. Planned evidence by group:

- AC1–AC9, AC10–AC15 (PA): the named Go tests under `internal/plugins`, the wsapi guard tests, web `settings-plugins` screenshots (light/dark, every state), a 390 px
  mobile smoke, and a manual install/remove of the example data-only plugin on a real daemon on macOS.
- AC16–AC23 (PB): the named process/hook tests, the fake-LLM harness assertion that `beforeStart` edits reach the launch, and a Windows QA run for the process and job-object
  tests (ADR-0029/0030).
- AC24–AC30 (PC): shared fixture suite on Go/web/mobile, screenshots of panel open/refresh/disable transitions on web and mobile, and the relay E2EE smoke.
- AC31–AC33 (P0, PD): the provider registry table test, a manual run of a custom ACP provider against a local ACP agent, and the generic-icon rendering check on web/mobile.
- K1 results (2026-10-25): AC31 `TestProviderRegistry_AddACPProvider_NoSwitchEdits` + `TestProviderList_SingleSource` (wsapi); AC32 `TestCustomProvider_ConfigValidation_FieldNamed`, `TestCustomProvider_RunsViaACP_UsageAndEvents`, `TestCustomProvider_ModelSelectionAndModes`; AC37 daemon `TestProviderInstance_ExtendsClaudeNative_OwnConfigDir`, `_EnvAllowlistOnly`, `_SideBySideWithBase`, `TestProviderDirSuggestions_NamesOnly`, `TestProviderInstanceRPCs_RefuseAgentCallers`, `TestProviderReload_AddsAndRemovesInstanceLive`, `TestProviderAddInstance_...`. Mutations all caught (re-added switch, non-allowlisted env, file read under a suggested dir, dropped run-scoped refusal). Manual run on an isolated daemon with two claude-native instances plus the base through the fake CLI: see the PR body. Still open for K2: the generic-icon rendering check and the offline fallback (AC31 client half), P0.2.
- AC37 (P0 provider instances): the `TestProviderInstance_*` and `TestProviderDirSuggestions_NamesOnly` tests, plus a manual run of two claude-native instances side by side on an isolated daemon.
- AC34–AC36: log/UI audit script that greps for unlabeled plugin output, the example CI job, and the release-notes diff.
- Every PR runs `task test`, `task lint` and the `verify` skill; UI PRs add the light+dark screenshot pass; nothing here has been run yet.
- 2026-10-25 K2 (P0.2 + AC37 UI): web/mobile provider ids are strings (closed `Provider` union removed; capability checks ask the catalog `runner`, built-in table only as offline answer); generic provider icon via `Icons.providerGeneric` (drift snapshot updated); instances wear their base's mark (zustand `provider-families-store` fed by the catalog hook); offline fallback = four built-ins, never merged with a loaded list; Settings -> Providers lists instances, `Add instance` area (dirSuggestions chips, manual form, inline errors), custom entries show config dir + Reload providers (no human remove RPC exists: edit config.yaml + reload). Mobile has no provider glyph surface, so only the catalog/fallback/labels changed (not verified on device). Public docs updated.

- 2026-10-25 PA.1 (host foundation + install; Lead, solo): `internal/plugins`, the `plugins` table and `plugins.enabled`, the `smind plugin` CLI and the `plugin.*` RPCs. Decisions on the way: `ssh` Git URLs are out of v1; `plugin.enable` takes the consent fingerprint as its confirmation; a pending update is staged in `.pending/<id>/` and applied only on `update <id> --yes`; `plugin.list` reports `globalEnabled`; settings edit and the `plugin.updated` event are not in PA.1 (settings arrive with PB.2, the event with the first client in PA.4); the public `plugins.md` page is left as "planned" because no contribution loads yet.
- PA.1 validation: AC1 `TestPluginInstall_RecordsDisabled_ConsentSummary`, `TestEnable_RequiresConfirmation` (+ CLI `TestEnable_RequiresConfirmation_CLI`); AC2 `TestGlobalSwitchOff_NothingLoadedListedOrCallable`, `TestInstall_Requires_PluginsEnabled`, `TestPluginRPC_OverWS_AgentRefused_SwitchDefaultOff`; AC3 `TestPluginRPC_RunScopedCallersForbidden`, `TestPluginRPC_RelaySessionsForbidden_ListGetAllowed` (settings edit: not here); AC4 `TestPluginInstall_Git_UnpinnedRefRejected`, `TestPluginInstall_Git_PinnedSHA_VerifiesHead`, `TestPluginInstall_Archive_Sha256Pinning`, `TestRemove_KeepData_AndLinkNeverDeleted`; AC5 `TestExtract_PathTraversalRejected`, `TestExtract_SymlinkAndHardlinkRejected`, `TestExtract_SymlinkEscapeViaDirSourceRejected`, `TestExtract_SizeAndEntryCapsEnforced`, `TestExtract_StreamCountedNotHeader`, `TestExtract_DuplicateAndCaseCollisionRejected`; AC6 `TestManifest_Validation_FieldNamed`, `TestEngines_SemverAndPrerelease`, `TestManifest_UnknownTopLevel_Ignored`, `TestManifest_PathsConfined`; AC7 `TestUpdate_CapabilityChange_StaysPending_PreviousKeepsRunning`, `TestUpdate_Failed_KeepsLastGood`, `TestReload_And_Start_Revalidate`, `TestStagedInstall_FailureLeavesNothing`; AC8 `TestRemove_KeepData_AndLinkNeverDeleted`; AC9 `TestWorkspaceRepoContents_NeverInstallAsPlugin`, `TestPluginSourceLocalPath_HumanExplicitOnly`. Seven mutations (unpinned ref accepted, hash check skipped, `..` allowed, symlinks allowed in tar/zip/dir, relay refusal dropped on install, run-scoped refusal dropped on enable, size counted from headers only) each failed their tests and were reverted. Manual run on an isolated daemon: see the PR.

- 2026-10-25 PA.2 (contributions; Lead, solo): owned rows are **derived at read time, not stored** (decision recorded in ADR-0042's PA.2 note): the `store.Overlay` (profiles, MCP) plus `plugins.Manager` as its only implementation; synthetic ids in `[2^40, 2^41)`; `read_only_owned` typed error; `skill.list`; `plugin.updated` event plus owned-row created/deleted diffs; skills through `projection.ProjectFileSet` / inline preface (`internal/plugins/skilldelivery`); profile skills `<plugin>:<skill>`; consent flags escalating profiles (in the fingerprint only when flagged). Decisions made on the way: injected MCP name is `<plugin>__<name>`; every enabled plugin skill is offered to every user run (profiles may additionally inline one); plugin profiles may use `onRateLimit` none/wait only (a stored fallback id cannot be referenced); plugin settings storage is still PB.2, so placeholder servers are listed but not injected in the real daemon (tests use `Manager.SetSettings`); a stored profile cannot reference an owned one.
- Supervisor defaults 2026-10-25 (Human may veto): git over ssh stays OUT of v1; `link` sources get a warning on enable and on the Plugins page ("live directory: changes after consent are not re-reviewed"; the consent carries `linkWarning`, `smind plugin enable` prints it, the page is PA.4).
- Human decisions 2026-10-25 (replace the former "open decisions gating PB.1"; **PB.1 is no longer gated on them**): (1) remote management is OFF by default via `plugins.allowRemoteManage` (loopback incl. a same-host TLS proxy may manage; a direct non-loopback login may only when the key is true; relay and agents never; reads unaffected); (2) the same-user limit: confining what an agent can read is the agent provider's sandbox/permission job, so smind has no boundary against a same-user agent reading the token/config. Deny rules for smind's own secrets in the claude-native `--settings` permissions are a separate small item queued after PA.2. Tests `TestPluginRPC_RemoteManage_DefaultOff_NonLoopbackForbidden_ReasonNamesSetting`, `TestPluginRPC_RemoteManage_On_NonLoopbackAllowed`, `TestPluginRPC_Relay_And_Agent_RefusedRegardlessOfSetting`, `TestPluginRPC_Loopback_AllowedWhenOff`; mutations 7 (setting ignored) and 8 (relay allowed when true) each failed their test and were reverted.
- PA.2 validation: AC10 `TestMcpContribution_OwnedReadOnly_InjectedAcpAndClaude_RemovedOnDisable` (taskrunner, fake ACP agent + fake claude CLI; disable, remove and switch-off), `TestMcpContribution_SecretSubstitution_RedactedOnRead` (plugins unit and a wsapi end-to-end sentinel audit over mcp.list/get, plugin.list/get, daemon log, run events, request log and error strings), `TestPluginContribution_WireCarriesOwner_ReadOnlyForEveryCaller` (human, agent, relay), `TestRegistry_OwnedRows`; AC11 `TestSkillContribution_ProjectionOnly_NeverWritesHomeSkillDirs`, `TestProfileSkills_AcceptsPluginNamespace`, `TestSkillFromDisabledPlugin_NotDelivered` (plugins and skilldelivery), `TestBuildInstructions_PluginSkills_ActiveOnly`; AC12 `TestProfileContribution_OwnedSeat_PermissionEscalationFlagged`, `TestUpdate_AddsEscalatingProfile_StaysPending`, `TestOwnedProfiles_ReadOnly_ForEveryCaller`; namespacing `TestNameCollision_CoreWins_ThenPluginIdOrder`, `TestNamespacing_NoShadowing`; propagation `TestPluginContribution_Propagation_EventsAndNoRestart`; AC2 `TestGlobalSwitchOff_NoContributionListed`. Six mutations (owned `mcp.update` allowed; owned rows injected while disabled; a skill written under HOME; the secret in `mcp.list`; escalation flag skipped; a plugin row shadowing a user row) each failed their tests and were reverted. Manual run on an isolated daemon (evidence `/tmp/smind-pa2-evidence`): see the PR. Not done in PA.2: plugin settings storage (PB.2), slash commands and themes (PA.3), the Plugins page and owned-row badges (PA.4).

- 2026-10-26 PA.4 (Plugins page + owner badges; Lead, one frame-harness Peer): web only, no daemon change. Settings -> Plugins (`plugins-section.tsx`): off state (no control for the switch), empty state, list (version, source kind, Enabled/Disabled, Pending update, Error, last error, link warning), detail (consent summary with escalating agents flagged, contributions with `OwnerBadge`, conflicts, logs = last error + timestamps + "Process logs arrive with plugin processes."), Enable (confirmation showing the consent summary and the link warning, sends the displayed fingerprint), Disable, Reload, Check for update / Review and apply (diff of capabilities and commands), Remove (Keep plugin data, default off), Install form. Mutations answering `forbidden` show the daemon's message inline, are not retried, and lock the page. `OwnerBadge` ("from <plugin>", opens the plugin's detail) is shown in MCP servers, Agents, the composer's agent picker and the skill suggestions of the agent editor (Settings has no skills list, so that editor is the only picker); owned MCP and agent rows have no toggle or edit/delete menu. Decisions (Supervisor defaults): the link warning on row, detail and enable; the Install form is local-only in effect (the UI cannot know the channel in advance, the first refusal locks the page); the event bridge entry for `plugin.updated` (one case + two query keys) overlaps the PA.3 PR's lines.
- PA.4 validation: AC15 `plugins-section.test.tsx` (off, empty, list, detail, enable, link warning, remove keepData, pending/apply, install, forbidden, relay, live), `owner-badges.test.tsx`, picker badge test in `provider-model-picker.test.tsx`; AC1 enable sends the fingerprint exactly once; AC2 off state has no switch control; AC3 forbidden inline and read-only; AC7 pending diff and Apply. Five mutations (enable without fingerprint; owned MCP row editable; link warning hidden; a switch control in the off state; `plugin.updated` ignored) each failed their tests and were reverted. Light+dark frames at 1280 and 390 and 8+ transition frames per scenario at /tmp/smind-pa4-evidence (no row moved or resized by 1 px).
- PA.3 validation: AC13 `TestSlashCommands_Autocomplete_InsertsNeverSends`, `TestSlashCommands_BuiltinWinsCollision`, `TestSlashCommands_FirstPluginByIdWinsBetweenPlugins` (web lib + composer, daemon `internal/plugins`), `TestSlashCommands_NoPluginCommands_ComposerUnchanged`, `TestSlashCommands_PaletteInsertsNeverSends` (hook + store), `TestPluginCommands_RPC_OnlyEnabled_SwitchOff_Empty` (wsapi, relay caller, exact wire JSON), `TestPluginCommands_ActiveOnly_SwitchOff_Empty`, `TestPluginCommands_BadPromptFiles_RejectedAtInstall`; AC14 `TestTheme_IncompleteTokenSet_RejectedWithMissingList`, `TestTheme_PartialSchemes_Accepted`, `TestTheme_HostileValues_Rejected` (Go table + web `isSafeThemeValue`), `TestTheme_TokenListDrift` (Go and web), `TestTheme_AppliesPerScheme`, `TestTheme_FallbackWhenPluginDisabled` (+ offline keeps the remembered theme, update replaces tokens, client refuses unsafe/extra/incomplete). Five mutations (auto-send on select; plugin beats built-in; incomplete set accepted; `url(` allowed; theme leaks into the other scheme) each failed their test and were reverted (`/tmp/smind-pa3-evidence/mutations.txt`). Real-app frames on an isolated daemon, light/dark at 1280 and 390, with a plugin of 3 commands and a Dusk theme installed through the CLI: all in the PR body. Not done: mobile (PA.5), built-in slash commands (none exist), a pre-paint bootstrap in index.html for the remembered theme (it is applied before React renders, not before first paint of the HTML).
- PA.5 validation (mobile, [plugins-pa5-mobile.md](plugins-pa5-mobile.md)): AC15 mobile part: `pluginsList.test.ts` (off, empty, list, every status chip, link warning, detail read-only, routes), `pluginsReadOnly.test.ts` (only plugin.list/get/commands/themes are ever sent; no mobile source names a mutating plugin RPC; `plugin.updated` refetch over the relay harness); AC13 mobile: `pluginSlash.test.ts` (`InsertsNeverSends`, `BuiltinWinsCollision`, `FirstPluginByIdWinsBetweenPlugins`, `NoPluginCommands_ComposerUnchanged`, ported web cases); AC14 mobile: `pluginTheme.test.ts` (`TokenListDrift`, mapping completeness, `HostileValues_Rejected`, oklch, `AppliesPerScheme`, `FallbackWhenPluginDisabled`); AC30 note: `pluginSafety.test.ts` (no `eval`/`new Function` anywhere in `mobile/src` and `mobile/app`, plugin screens render text only). Four mutations (select sends; a mutating RPC in the detail load; `url(` accepted; built-in loses the collision) each failed their test and were reverted (`/tmp/smind-pa5-evidence/mutations.txt`). 390 px light+dark frames from the Expo web export against a real daemon API (devharness + a second `plugins.Manager`), stage `75-plugins`: all in the PR body. Not done: a native device pass (no device here), the AC30 frame-cap and `plugin.call` parts (PC).
