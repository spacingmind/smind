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
- [ ] P0.1  [ ] P0.2
- [ ] PA.1  [ ] PA.2  [ ] PA.3  [ ] PA.4  [ ] PA.5
- [ ] PB.1  [ ] PB.2  [ ] PB.3  [ ] PB.4
- [ ] PC.1  [ ] PC.2  [ ] PC.3
- [ ] PD.1  [ ] DOC.1

## Validation

Filled as items land; for each acceptance criterion, name the test or manual check that confirmed it. Planned evidence by group:

- AC1–AC9, AC10–AC15 (PA): the named Go tests under `internal/plugins`, the wsapi guard tests, web `settings-plugins` screenshots (light/dark, every state), a 390 px
  mobile smoke, and a manual install/remove of the example data-only plugin on a real daemon on macOS.
- AC16–AC23 (PB): the named process/hook tests, the fake-LLM harness assertion that `beforeStart` edits reach the launch, and a Windows QA run for the process and job-object
  tests (ADR-0029/0030).
- AC24–AC30 (PC): shared fixture suite on Go/web/mobile, screenshots of panel open/refresh/disable transitions on web and mobile, and the relay E2EE smoke.
- AC31–AC33 (P0, PD): the provider registry table test, a manual run of a custom ACP provider against a local ACP agent, and the generic-icon rendering check on web/mobile.
- AC37 (P0 provider instances): the `TestProviderInstance_*` and `TestProviderDirSuggestions_NamesOnly` tests, plus a manual run of two claude-native instances side by side on an isolated daemon.
- AC34–AC36: log/UI audit script that greps for unlabeled plugin output, the example CI job, and the release-notes diff.
- Every PR runs `task test`, `task lint` and the `verify` skill; UI PRs add the light+dark screenshot pass; nothing here has been run yet.
