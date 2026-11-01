# Plugins Wave D: plugin-contributed providers (AC33, AC36)

Status: **spec, 2026-11-01. Nothing is implemented until the Steward says this spec is accepted.** Parent plan: [plugins.md](plugins.md) (item PD, rows AC33, AC36);
decisions: [ADR-0042](../../decisions/0042-plugin-system.md) section 7 (accepted) and [ADR-0046](../../decisions/0046-provider-spec-harness-overlay.md) (provider = spec + harness + overlay).
Related plans: [providers-epic.md](providers-epic.md), [picker-gate-reasons.md](picker-gate-reasons.md), [run-failure-classes.md](run-failure-classes.md), [plugins-wave-c.md](plugins-wave-c.md) (style).
Waves 0, A, B and C are merged. Wave D is the last wave of ADR-0042.

Size: **L (7)**: scope 2 (daemon; web and mobile pickers/settings; docs and an example), PRs 2 (four, ordered by waiting for merges), risk 2 (the daemon executes a command a plugin
supplies, with an env it supplies and a secret slot), decisions 1 (seven Human decisions below, all defaulted), verification 2 (fake-agent end to end, light/dark frames at 1280 and 390).
One Lead; peers only where stated in the peer plan.

Rule for every PR: non-stacked, branched from `origin/develop`; a PR that needs an earlier PR's code starts only after that PR is on `develop`.

## What exists today (read before the spec; file references checked at `f4c331d5`)

- `internal/plugins/manifest.go:181` already parses a **stub** `ProviderContribution {id, label, acp.command}`; `Validate` checks the id (`checkName`) and the argv; `consent.go:111` lists it as
  `AcpEntry{ID, Command}`, it counts as "runs code" and is in the fingerprint. **Nothing registers it**: no provider appears. Wave D turns the stub into a real contribution.
- `internal/providers` is the registry: one `Spec` per provider (`Runner` claude-native|codex-native|acp, `Custom`, `Command`, `Env`, `SecretEnv`, `Models`, `Modes`, `Vendor`, `Health`, `Delivery`, ...).
  `Registry.Register`/`Unregister` exist; `SetCustom(specs)` **replaces every custom entry** and is called by `provider.reload` and at start (`cmd/smind/serve.go:130`), so a plugin layer needs a
  separate slot or a reload would wipe it. `ReservedID` already refuses the prefixes `smind/` and `plugin/`.
- Ids: `providers.custom` ids match `^[a-z0-9][a-z0-9._-]{0,63}$` (no `/`), built-in ids have no `/`, plugin ids match `^[a-z][a-z0-9-]{0,63}$`. A `<plugin>/<provider>` id therefore **cannot equal** a core or custom id
  by construction; the rule below makes that a tested invariant, not an accident.
- `internal/plugins/host` builds a plugin process env (fixed `PATH`, `HOME` = the plugin's `data/`, no `SMIND_*`); PB.3 validates hook env names (`validateBeforeStart`); `plugin_settings` stores plugin
  secrets write-only; `internal/wsapi/plugin_session.go` never-list covers `provider.` so a plugin process can never call a `provider.*` method.
- ADR-0046: `provider_secrets` table, `provider.setSecret` / `clearSecret` human-only and write-only, `Spec.SecretEnv`, `provider.updateInstance`, cooldown per provider id, `ratelimit.Classify` takes a vendor
  profile from the spec (`generic` always applies), health check `Spec.Health`, `gate.Derive` (reasons `not_signed_in`, `out_of_plan`, `cooling_down`, `unsupported_by_harness`, `model_not_in_plan`),
  failure classes in `internal/runs/failure`.
- `internal/taskrunner/fakeagent` is a scripted ACP agent with scenarios `reply`, `quota-error`, `auth-error`, `model-error`, `transient-error`, `residual-429`, `plain-error`, `permission*`, `hang`, ...
- `plugin-examples/hello-process` is the only example (Python 3, standard library only).

## Decisions taken (Lead defaults; H1-H7 below are **Supervisor default 2026-11-01, Human may veto**)

Status of H1-H7: the Supervisor took all seven as recommended on 2026-11-01 (Supervisor default 2026-11-01, Human may veto); implementation proceeds on them.

- **D1 One data path.** A plugin provider is a `providers.Spec` (`Runner: acp`, `Custom: true`, new field `Plugin: "<plugin id>"`) in the SAME registry, resolved by the same `resolve()` as a custom ACP entry, and
  run by the same `runACP`. No parallel table, no second runner, no change to run events, usage, model selection or permission modes.
- **D2 Derived, never stored, layered.** Like the PA.2 owned rows, plugin providers are computed from the validated manifests of the **active** plugins (enabled, consented, global switch on). The registry gets
  a third layer: `Registry.SetPlugins(map[pluginID][]Spec)` replaces the plugin layer atomically; `SetCustom` replaces only the custom layer; the table is rebuilt as built-ins, custom, plugins (sorted by plugin id,
  then manifest order). A bad plugin entry rejects that plugin's providers only (the error is `lastError` on the plugin), never the whole layer and never the daemon.
- **D3 Sync points.** The daemon calls one `syncPluginProviders()` where it already calls `SyncHost`: start (after `SetCustom`), enable, disable, reload, applied update, remove, a flip of `plugins.enabled`
  (the 2 s watcher), and `provider.reload`. Each change that alters the layer publishes `provider.updated` (existing topic, `id: ""`: clients refetch).
- **D4 id.** `<plugin>/<provider>`; the provider part follows the plugin id grammar `^[a-z][a-z0-9-]{0,63}$`; total at most 129 bytes. Plugin ids `smind` and `plugin` are reserved (install refuses with
  `reserved_plugin_id`), matching `ReservedID`. The id is a plain string everywhere; it is **never** used as a path component: any directory a plugin provider needs is
  `plugins/<plugin>/data/providers/<provider>/` built from the two validated parts.
- **D5 harness.** `acp` only (ADR-0042 section 7: direct providers are not in v1). The manifest field `harness` is optional and, when present, must be `"acp"`; `extends` is not a manifest field in v1.
- **D6 What a plugin provider cannot do.** No `extends`, no recipe, no `endpoint`, no `usage` meter, no login flow (`account: none`): these are core-provider features that need reviewed code. `login` is text
  only (the health message). `provider.updateInstance` and `provider.setEnv` are refused with the existing typed code `read_only_owned` (`data: {plugin}`); `provider.setSecret`, `clearSecret` and
  `clearCooldown` work (that is their purpose).
- **D7 Failure classes and gates need no new run class.** An ACP plugin agent's errors go through the same `ratelimit.Classify` with the spec's vendor profile (`generic` by default; the manifest may name
  `zai`, `anthropic`, `openai`). A process that exits mid-run (plugin disabled, crash) ends the run as the existing `cli_exited`. One new picker reason, `plugin_unavailable`, is added (D8).
- **D8 Reference to a provider whose plugin is off.** Tasks, chats, profiles and automations store the provider id as a string and are never rewritten. At use time:
  - the **send/run-start guard** refuses with the typed code `provider_unavailable` and `data {provider, plugin, state: "disabled" | "global_off" | "removed" | "invalid"}` for every caller (human included: nothing can run);
  - `provider.list {includeInactive: true}` (profile editor, chat chip) adds a row per provider of an **installed but inactive** plugin with a hard gate `plugin_unavailable`, action `enable_plugin` (opens the
    plugin's page), `until` absent; the default `provider.list` omits them (AC33 "vanish with the plugin");
  - a **removed** plugin leaves nothing to derive a row from: clients show the raw id with the generic icon and the daemon's refusal text names the plugin id taken from the id's prefix;
  - `plugin_unavailable` is a gate reason with **no failure-class twin** (a stated "no twin" entry in `twins`, like `unsupported_by_harness`): nothing ran, so there is no run to classify;
  - history stays: run rows, usage rows and cost rows keep their `acme/agent` id and keep appearing in `usage.summary` grouped by that string; the UI labels a provider id with a `/` that is not in the catalog as
    "acme/agent (plugin not installed)". Only the provider secrets and in-memory cooldown of the provider go away with `remove` (not with `disable`, so re-enabling restores the key).
- **D9 Failover.** A plugin provider can be a fallback or `wait` target **only when a Human-authored profile names it** (the profile row is the consent). A plugin-owned profile may name only providers of its own
  plugin or core providers; automatic failover never picks a plugin provider that no profile names; if a named fallback is unavailable the chain skips it with a visible run note.
  Cooldown (ADR-0041 F1) is per provider id and needs no change.
- **D10 Slash ids.** The wire is JSON strings (no change). Web: any route or query that carries a provider id uses `encodeURIComponent`; CLI: ids are positional/flag strings (`--provider acme/agent`); MCP:
  `task_new`/`task_send` take the string as is; `config.yaml`: plugin providers are never config keys (`providers.<id>` stays built-in-only, `validateProviders` unchanged); a `providers.custom` entry may
  not `extends` a plugin provider id (refused with `providers.custom[i].extends: ... is provided by a plugin and may vanish`); nothing derives a path or a git ref from the id.

## Manifest shape (`contributes.providers[]`)

```json
{
  "id": "my-agent",
  "label": "My agent",
  "description": "ACP coding agent for Foo",
  "icon": "Bot",
  "harness": "acp",
  "acp": {
    "command": ["bin/my-agent", "--acp"],
    "env": { "FOO_MODE": "acp" },
    "secretEnv": [{ "name": "FOO_API_KEY", "label": "Foo API key", "required": true }],
    "inheritHome": false,
    "supportsMcpServers": true
  },
  "models": [{ "id": "large", "label": "Large", "default": true }, { "id": "small", "label": "Small" }],
  "modes": [{ "id": "default", "label": "Ask" }, { "id": "auto", "label": "Auto", "autoApproves": true }],
  "defaultMode": "default",
  "vendor": "generic",
  "health": { "install": "npm i -g my-agent", "login": "run `my-agent login`", "versionArg": "--version", "minVersion": "1.2.0" }
}
```

Rules (every error names its field and the fix; shared validation is the Go package, the fixtures are the cross-language contract):

- `id`, `label` required (label at most 80); `description` at most 300; `icon` a Lucide name (pattern of Wave C D6; unknown name = the generic provider icon on the client).
- `acp.command`: argv, at least one entry, each at most 4 KiB, no NUL/newline, at most 32 entries; **never a shell string**. `argv[0]` forms: a path with a separator **inside the plugin directory** (relative,
  `..`-free, confined by the existing path helper, must be a regular file and executable at registration; symlinks rejected), or a **bare name** looked up on the fixed system `PATH` of the process host
  (`/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin`; `System32` on Windows). An absolute path outside the plugin directory is refused (`command_not_allowed`).
  Windows `.cmd` shims go through `internal/shimguard`.
- `acp.env`: literal, non-secret entries, at most 16. Names `^[A-Z][A-Z0-9_]{0,63}$`; refused (named): `PATH`, `HOME`, `SHELL`, `USER`, proxy names, `SMIND_*`, `ANTHROPIC_*`, `CLAUDE*`, `OPENAI_*`, `CODEX_*`,
  `GIT_*`, `LD_*`, `DYLD_*`, `NODE_*`, `BASH_*`, and any name ending `_KEY`, `_TOKEN`, `_SECRET`, `_PASSWORD` (those go in `secretEnv`). It is the **same checker as the PB.3 hook env** (one function,
  moved to a shared file). Values at most 4 KiB. **There is no passthrough from the daemon's env** for a plugin (ADR-0046's third value kind is not available to plugins).
- `acp.secretEnv`: `[{name, label, required}]`, at most 8, names under the same grammar but the secret suffixes are allowed here, no overlap with `env`. A secret is a slot, never a value: the manifest cannot
  hold one. Values are set by the Human with `provider.setSecret {instanceId: "<plugin>/<provider>", name, value}` (ADR-0046 store, write-only, human-only).
- `acp.inheritHome` (default false): see Human decision H2. `acp.supportsMcpServers` mirrors `providers.custom[].acp.supportsMcpServers`.
- `models` (at most 64; id syntax is `ValidateModelSyntax`, one `default` at most), `modes` (at most 16, unique ids, `defaultMode` must exist): same shapes as `providers.custom`, static; ACP
  discovery layers the agent's own lists on top as for every ACP provider.
- `vendor`: `generic` (default) | `zai` | `anthropic` | `openai`. `health`: same keys as `providers.Health`; **only text and a version check**, `provider.test` never makes a model call and never runs the plugin's
  command beyond `argv[0] --versionArg` (the existing health rules; the version probe runs with the scrubbed env of H2).
- Unknown keys inside `acp`, `health`, a model or a mode are rejected (a misspelled constraint must not skip a check); unknown top-level keys stay ignored (ADR-0042).
- Duplicate provider ids inside one manifest are rejected. A manifest with providers and no process is valid (a data-only plugin can contribute a provider; it still **runs code** for consent purposes).
- **Not in the manifest**: `extends`, `endpoint`, `recipe`, `usage`, `account`, `args` outside argv, `cwd`, `passthrough`, anything that changes the permission mode of a run.
- Shared fixtures: `internal/plugins/providerfixtures/testdata/<name>.provider.json` + `<name>.expect.json` (`valid` with the normalised entry, or `invalid` with the error field). Go consumes them in
  `TestProviderManifest_FixtureSuite_Go`; web and mobile read the same files by relative path (the way Wave C reads the uiview fixtures) only for the **display** model (label, icon, secret slots, plugin badge).

## Trust and permissions

1. **No new capability group (H1).** `capabilities` mediate calls a plugin *process* makes into the host API; a provider makes none (it is a child the host spawns, and `provider.` stays on the plugin
   never-list). The contribution itself is the declaration, and it is what consent shows. A plugin that declares only `contributes.providers` is "data plus a command": `DataOnly` stays false.
2. **Consent (AC7 mechanics, extended).** `AcpEntry` grows from `{id, command}` to `{id, label, command, envKeys, secretEnv (names, required), inheritHome, vendor}`. The consent screen (web, CLI) prints, per
   provider: the id `acme/my-agent`, the **exact argv** and how `argv[0]` resolves ("plugin file bin/my-agent" or "looked up on PATH: opencode"), **env keys only**, the secret slot names, and
   a red line when `inheritHome` is true. The fingerprint covers all of it **and a digest of the literal env values** (a base URL change must re-confirm) but the summary never prints a value.
   An update that changes any of it stays pending, the previous version keeps serving (the existing `.pending/` flow); a **linked** plugin's reload that changes it disables the plugin until re-confirmation.
3. **Registered only after trust (H4).** A provider exists in the registry only while its plugin is active: enabled by the Human with the consent fingerprint, global switch on. Install alone registers nothing;
   `plugin get` lists the providers a disabled plugin *would* add, marked inactive.
4. **Env passed to the agent.** Built, never inherited: the process host's fixed `PATH`, `HOME` = `plugins/<id>/data/providers/<provider>/home` (created 0700; `USERPROFILE` on Windows), temp in its `tmp/`,
   `LANG`/`LC_*`/`TZ`, the manifest's literal `env`, then the resolved secrets, then smind's own ACP additions (the run id of the MCP bridge is the existing `acp.WithEnv` path and carries no daemon token for
   the agent beyond what a core ACP provider gets today; the Lead verifies this by diff, AD14). With `inheritHome: true` only `HOME`/`USERPROFILE` is the daemon user's real home (the agent can read its own
   login files, and everything else in that home: said on the consent screen). Never `SMIND_*`, never a daemon token, never `passthrough`. cwd is the task worktree, as for every provider.
5. **Secrets (H3).** Stored in `provider_secrets` keyed `("<plugin>/<provider>", name)`; the existing write-only, human-only RPC; never in argv, logs, events, `provider.list` (it reports `{set, fingerprint}`
   only), errors, transcripts or the plugin's own log (the plugin log redactor is rebuilt with every provider secret too). The **plugin process never sees them** (it has no `provider.*` access and its
   env is built without them); only the agent child launched for a run gets them. A required unset slot is a hard gate `not_signed_in` through `gate.Inputs.UnsetSecrets` (existing path), whose action is
   `open_provider_settings`. `plugin.settings.set` is not the route for them (so an agent cannot reach them through a `plugin.*` write either: `provider.setSecret` is refused for run-scoped callers, as today).
6. **Honest limits, stated in the ADR note and the docs.** There is no OS sandbox in v1 (ADR-0042 Q12): the agent runs as the daemon user, in the task worktree, and can read and write anything that user can
   (including `~/.smind/token` and `relay-keypair.json`, which are 0600, same user). The scrubbed env, the confined command, the consent screen and the write-only secret slot are
   *disclosure and mediation*, not isolation; and an agent that is prompt-injected can read its own env (ADR-0046 spike), which for a plugin provider includes its API key, by design, because the agent needs it.
   Agents and repositories never authorize execution (ADR-0019 #6, ADR-0024): a workspace can never install, enable or edit a plugin or its provider (existing guards: `plugin.*` forbidden for run-scoped,
   relay and non-loopback-without-`allowRemoteManage` callers; `provider.setSecret` human-only).
7. **Update that changes the command (AC7).** Pending until re-consent; a running run of the old version is not killed by the pending state (only apply, disable, remove or reload restart things); on
   apply/reload the registry layer is swapped, and **new runs use the new command, runs in flight finish on the old child** (the child is not re-parented; disable/remove kill it: the group is swept like a
   plugin process, run ends `cli_exited`, run note names the plugin).

## Composition with ADR-0046, gates and failure classes (the cross-checks)

| Surface | Behaviour for `acme/my-agent` | Evidence |
|---|---|---|
| Registry / `provider.list` | a `Spec`; additive wire fields `plugin: "acme"`, `inactive?: true` (only with `includeInactive`), `custom: true`, `runner: "acp"`; `secrets[]` `{name,label,required,set,fingerprint}` | `TestPluginProvider_ListCarriesPluginBadgeAndSecretSlots` |
| Runs | `runACP`, run events, usage, model selection, permission modes unchanged | `TestPluginProvider_RunsViaACP_FakeAgent_UsageAndEvents` |
| Model catalogue | manifest static list + ACP discovery merged (`MergeModels`); no `updateInstance` | `TestPluginProvider_Models_StaticPlusDiscovery`, `TestPluginProvider_UpdateInstanceRefused_ReadOnlyOwned` |
| Cooldown / failover | per id; `clearCooldown` works; fallback only if a Human profile names it (D9) | `TestPluginProvider_Cooldown_PerID`, `TestFailover_PluginProviderOnlyWhenProfileNamesIt` |
| Failure classes | fakeagent `quota-error`, `auth-error`, `transient-error`, `residual-429` classify exactly as for a custom ACP provider; `cli_exited` on kill | `TestPluginProvider_FailureClasses_SameClassifier` |
| Gates | `not_signed_in` (unset required secret), `cooling_down`, `plugin_unavailable` (new, hard, no twin); `unsupported_by_harness`/`out_of_plan` never apply; the twin table test knows the new "no twin" entry | `TestGate_PluginUnavailable_HardNoTwin`, `TestGate_PluginProvider_UnsetSecret_NotSignedIn` |
| Learning delivery | preface (like the other ACP providers), `ToolEvents: true`, `Reflect: false` (a plugin agent is never a reflector source) | `TestPluginProvider_NotAReflectorSource` |
| Usage meter | none (D6) | covered by the manifest fixture `invalid/usage-key` |
| Recipes / instances | none in v1; `extends` a plugin provider refused (D10) | `TestCustomProvider_ExtendsPluginProvider_Refused` |

## UI (web and mobile, no new store)

- **Settings > Providers (web)**: plugin providers are rows in the existing list with a `PluginBadge` (the `OwnerBadge` of PA.4: "from acme") that opens the plugin's detail; the detail shows the command line
  summary, the env keys, the secret slots with a password field and "set / not set" (never re-displayed), the inactive state when the plugin is off ("Enable plugin acme" action), and **no** edit
  controls for models/tools (read-only owned). The icon is the manifest's Lucide name or the generic provider icon.
- **Pickers (web composer, new-task, profile editor; mobile new-task and pickers)**: rows come from `provider.list`; a row with a `plugin` wears the badge and the gate reasons of picker-gate-reasons (`plugin_unavailable`
  has its own copy and the `enable_plugin` action, shown only where `includeInactive` is requested). A stored id absent from the catalog renders the raw id, the generic icon and "(plugin not installed)".
- **Plugin detail page** gains a "Providers" section (id, label, state, secret slots); **mobile** shows the same read-only (no install, no secret entry on the phone: ADR-0042 section 8; a secret
  slot says "set on the computer running smind"). Setting a secret from the phone stays out (the call is allowed by ADR-0046 from a Human relay session, but the plugin page on mobile is read-only in v1).
- **CLI**: `smind provider list` shows `acme/my-agent  (plugin acme)`; `smind plugin show acme` prints the providers block of the consent summary; `smind provider set-secret acme/my-agent FOO_API_KEY`
  reads the value from stdin/tty, never argv (existing `provider` command conventions). **MCP**: `provider_list` carries `plugin` and `gate`; `task_new`/`task_send` accept the id as-is.
- Frames: Settings > Providers with one plugin provider (set and not set), inactive state, picker row with badge and a gated row, light and dark, 1280 and 390, **every transition frame** (enable/disable the plugin
  live, secret set) reviewed pair by pair (memory: frame review of theme pairs).

## Examples

`plugin-examples/hello-provider/` (MIT, like hello-process): `smind-plugin.json` with one provider `acp.command: ["python3", "agent.py"]`, a Python 3 standard-library ACP agent `agent.py` (initialize,
`session/new`, `session/prompt` streaming two chunks, no network, no model) and a README. It is the docs walkthrough. Tests need scenario variety, so the Go end-to-end test **builds
`internal/taskrunner/fakeagent` into a temp plugin directory** (`bin/agent`) with a generated `smind-plugin.json` (helper `writeFakeAgentPlugin(t, scenario)`) and installs it through the real RPCs
(`plugin.install`, `plugin.enable` with the fingerprint); it never starts a model. `TestExamples_HelloProvider_ManifestValid_AndAgentAnswers` validates the shipped example and, when `python3` is on the
host's fixed PATH, runs a real turn through it (explicit "NOT VERIFIED" skip message otherwise, like `TestExamplePlugin_EndToEnd`).

## PR split and write scopes

| PR | Content | Write scope (one owner) | Needs |
|---|---|---|---|
| plan | this file | `docs/plans/active/plugins-wave-d.md` | none |
| **PD1** manifest + registry | `ProviderContribution` real shape and validation, command/env checkers (shared env-name checker moved out of `hooks.go`), fixtures, `reserved_plugin_id`, `Spec.Plugin`, registry plugin layer (`SetPlugins`, rebuild), `syncPluginProviders` wiring + `provider.updated`, `provider.list` additive fields and `includeInactive`, `read_only_owned` refusals, `extends`-a-plugin-provider refusal | `internal/plugins/manifest*.go`, `internal/plugins/providerfixtures/`, `internal/providers/` (registry only), `internal/config/providers_custom.go` (refusal), `internal/wsapi/provider*.go` (list + refusals), `cmd/smind/serve.go` (wiring), `internal/taskrunner/provider.go` (info) | plan merged |
| **PD2** trust + run path | consent/fingerprint extension, scrubbed env builder for the agent child, `inheritHome`, secrets slots in `provider_secrets` + gate wiring + delete-on-remove, `provider_unavailable` send guard, `plugin_unavailable` gate + twin-table entry, update/linked reload re-confirm, kill on disable/remove (`cli_exited`), fakeagent end-to-end and failure-class tests, mutations | `internal/plugins/consent.go`, `internal/plugins/providers.go` (new: derive specs + env), `internal/gate/`, `internal/taskrunner/` (launch env for plugin specs only), `internal/wsapi/` send guard + secrets rules, `internal/runs/failure/` tests only | PD1 merged |
| **PD3** UI | web: providers section badge/detail/secret slots/inactive, pickers, stale-id label, route encoding, plugin detail Providers section; mobile: catalog types, picker badge, read-only plugin section; CLI `provider list` and `plugin show` text; MCP text; frames | `web/packages/ui/src/components/{providers,pickers,settings}`, `web/packages/ui/src/lib`, `mobile/src`, `cmd/smind/provider*.go`, `internal/mcpserver` text | PD1 merged (PD2 preferred, for the gate and secret fields) |
| **PD4** docs + example | `plugin-examples/hello-provider`, `public-docs/plugins-authoring.md` provider section, `public-docs/plugins.md`, `public-docs/providers.md` pointer, ADR-0042 implementation note, release-notes line (AC36), drift test that the doc's manifest table equals the Go fixtures' accepted keys | `plugin-examples/hello-provider/`, `public-docs/`, `docs/decisions/0042-plugin-system.md` (note only) | PD2 merged |

PD3 and PD4 have disjoint scopes and may run in parallel after PD2 merges. **Peer plan** (Lead writes PD1 and PD2 itself: they touch trust and the run path):

- *PD3 web and mobile*: **peer-sonnet** (UI and screenshot work), one task, step briefs: (1) provider-catalog types and `PluginBadge` reuse, (2) providers-section detail and secret slots, (3) picker rows and the stale-id label,
  (4) mobile types and read-only section, (5) frames. The Lead reviews every diff and every frame.
- *PD4 docs and example*: **peer-glm**, one task, exact files and the test name above; its brief forbids `git push`, `gh pr create` and `git stash`; the Lead pushes.
- *Read-only research peers*: none needed.
- At most two Peers at a time; each Peer's commits are on the Lead's branch before the PR; the Lead archives the Peer task afterwards.

## Acceptance criteria and named tests

IDs are `AD1..AD24` (Wave D); the mapping to the parent plan is AD1-AD8 = AC33, AD22 = AC36.

### PD1 (manifest, registry, ids)

- AD1 The manifest validates the shape above; every error names its field. `TestProviderManifest_Valid`, `TestProviderManifest_Invalid` (table: bad id, duplicate id, `harness` not acp, empty/over-long argv, shell-looking
  string, NUL, absolute path outside the plugin, `..` escape, symlink, non-executable, env name refused (each refused family one row), secret suffix in `env`, overlap env/secretEnv, over caps, unknown key in `acp`,
  `usage`/`extends`/`endpoint` present), `TestProviderManifest_FixtureSuite_Go`, `TestProviderManifest_EnvChecker_SameAsHookEnv` (one function, both callers).
- AD2 A provider of an active plugin is registered as `<plugin>/<provider>` and is in `provider.list` with `plugin`; it vanishes on disable, remove, flip of `plugins.enabled`, and a failed reload keeps the last good layer.
  `TestPluginProvider_NamespacedId_NoShadowing_VanishesWithPlugin` (the parent plan's name), `TestPluginProviders_RegistryMatchesActiveSet` (after every Manager operation the layer equals the one derived from `Active()`),
  `TestPluginProviders_FailedReload_KeepsLastGood`, `TestPluginProviders_GlobalSwitchOff_NoProviders`.
- AD3 A plugin id or provider id can never produce an id equal to a core or custom id; a violation is refused with a named error. `TestPluginProvider_ID_CannotCollideWithCoreOrCustom` (a table over every built-in id and a
  custom id, with a fuzz of plugin/provider id strings: none yields a registered id without a `/`), `TestPluginInstall_ReservedPluginIDs_Refused` (`smind`, `plugin`: `reserved_plugin_id`),
  `TestPluginProviders_DuplicateAcrossPlugins_Impossible` (ids are unique per plugin; two plugins cannot clash).
- AD4 `SetCustom` and `SetPlugins` are independent layers: a `provider.reload` keeps plugin providers and a plugin change keeps custom ones. `TestRegistry_Layers_ReloadKeepsOtherLayer`, `TestRegistry_SetPlugins_Atomic_BadEntryRejectsThatPluginOnly`.
- AD5 Owned and read-only: `provider.updateInstance`, `provider.setEnv`, `provider.setDefaultModel` on a plugin provider fail `read_only_owned`; a custom entry that extends one is refused.
  `TestPluginProvider_UpdateInstanceRefused_ReadOnlyOwned`, `TestCustomProvider_ExtendsPluginProvider_Refused`, `TestPluginProvider_NeverAReflectorSource`.
- AD6 `provider.list` is the single source and its wire is additive. `TestPluginProvider_ListCarriesPluginBadgeAndSecretSlots`, `TestProviderList_IncludeInactive_OnlyInstalledInactive`, `TestProviderList_DefaultOmitsInactive`.

### PD2 (trust, secrets, run path, gates)

- AD7 Registered only after trust. `TestPluginProvider_NotRegisteredBeforeEnable`, `TestPluginProvider_EnableNeedsFingerprint_ThenRegisters`.
- AD8 The consent summary lists the exact argv, how `argv[0]` resolves, env keys (never values), secret slot names and `inheritHome`; the fingerprint changes on any of them and on a literal env value, and
  the summary never contains a value. `TestConsent_Provider_ListsCommandEnvKeysSecretSlots_NoValues`, `TestConsent_Provider_FingerprintCoversArgvEnvValuesAndInheritHome`,
  `TestConsent_Provider_ValueNeverInSummaryOrLogs` (sentinel).
- AD9 An update that changes a provider's command, args, env, secret slots or `inheritHome` stays pending with the old version serving; a same-fingerprint update applies; a linked plugin's changing reload disables it.
  `TestPluginUpdate_ProviderCommandChange_PendingKeepsOldRegistered`, `TestPluginUpdate_ProviderSameFingerprint_Applies`, `TestPluginReload_Linked_ProviderChange_DisablesUntilConfirm`.
- AD10 The agent child's env is built, never inherited: no `SMIND_*`, no daemon token, no inherited keys; `HOME` is the provider's data home unless `inheritHome`; the manifest literals and resolved secrets are present;
  a plugin cannot override `PATH`/`HOME`. `TestPluginProvider_AgentEnv_ScrubbedAndBuilt` (a fake agent that dumps its env, sentinel in the daemon env must be absent), `TestPluginProvider_InheritHome_OnlyHome`,
  `TestPluginProvider_AgentEnv_NoPassthrough`.
- AD11 Command confinement at launch, not only at registration: the plugin-relative path is re-resolved and re-checked (still inside, regular, not a symlink) before every spawn; a swapped file that escapes refuses the run.
  `TestPluginProvider_Launch_RechecksConfinedCommand`, `TestPluginProvider_BareName_LooksOnFixedPathOnly`.
- AD12 Secrets: write-only, human-only, keyed by the namespaced id, scoped to the agent child, never in argv/logs/events/list/errors/transcript/plugin log; the plugin process never receives them; delete on remove,
  kept on disable. `TestPluginProvider_Secret_WriteOnly_ListShowsFingerprintOnly`, `TestPluginProvider_Secret_NeverInArgvLogsEventsTranscript` (sentinel over a whole fake run, like the ADR-0046 redaction test),
  `TestPluginProvider_Secret_NotInPluginProcessEnvOrInitialize`, `TestPluginProvider_SetSecret_RefusedForRunScoped`, `TestPluginRemove_DeletesProviderSecrets_DisableKeeps`.
- AD13 Gates: a required unset secret is a hard `not_signed_in`; `plugin_unavailable` is a hard gate with the `enable_plugin` action for installed-inactive providers and has an explicit "no twin" entry.
  `TestGate_PluginProvider_UnsetSecret_NotSignedIn`, `TestGate_PluginUnavailable_HardNoTwin`, `TestGateTwins_NoTwinEntryRequired_ForPluginUnavailable` (the AC2 table test of picker-gate-reasons stays green and now names it).
- AD14 Send/run-start guard for a missing provider: typed `provider_unavailable` with `state`, for every caller, nothing spawned, no row rewritten. `TestSend_PluginProviderDisabled_ProviderUnavailable_Typed`,
  `TestSend_PluginProviderRemoved_ProviderUnavailable_NamesPlugin`, `TestSend_PluginProviderUnavailable_ProfileAndAutomationUntouched`, `TestHistory_UsageRowsSurviveRemove` (usage.summary still groups the id).
- AD15 Runs: the fake agent runs through `runACP` with events, usage, model selection and modes unchanged; disable or remove mid-run kills the child group and the run ends `cli_exited` with a note naming the plugin.
  `TestPluginProvider_RunsViaACP_FakeAgent_UsageAndEvents`, `TestPluginProvider_ModelSelectionAndModes`, `TestPluginProvider_DisableMidRun_CLIExited_NoteNamesPlugin`,
  `TestPluginProvider_UpdateApplied_InFlightRunFinishesOnOldChild`.
- AD16 Failure classes: the same classifier and vendor profile. `TestPluginProvider_FailureClasses_SameClassifier` (fakeagent `quota-error`, `auth-error`, `transient-error`, `residual-429` give the same `failureKind`
  as the same scenario on a `providers.custom` ACP entry), `TestPluginProvider_Vendor_ZaiWordingOnlyWhenDeclared`, `TestPluginProvider_Cooldown_PerID`.
- AD17 Failover and profiles: `TestFailover_PluginProviderOnlyWhenProfileNamesIt`, `TestProfile_PluginOwnedMayNotFallbackToOtherPluginProvider`, `TestFailover_NamedPluginProviderUnavailable_SkippedWithNote`.
- AD18 Health: `provider.test` for a plugin provider checks argv[0] resolution and the optional version, never starts a model call, shows `health.install`/`login`. `TestProviderTest_PluginProvider_NoModelCall_ShowsInstallAndLogin`.
- AD19 Slash ids are safe: no path, git ref or config key is built from the id. `TestPluginProvider_SlashID_NeverUsedAsPathOrRef` (a source scan of `taskrunner` and `wsapi` for `filepath.Join(... providerID ...)` over
  plugin specs plus a run in a temp dir that lists everything written), `TestConfig_ProvidersKey_PluginIDRejected`.
- AD20 AC34 labeling: every message, error and log line about a plugin provider carries `plugin <id>` (`TestPluginProvider_ErrorsAndLogs_LabeledByPlugin`).

### PD3 (UI, CLI, MCP)

- AD21 Web `plugin-providers.test.tsx` (badge, detail, secret slot set/not set, inactive state and action, read-only owned, generic icon, stale-id label, `encodeURIComponent` on every route/query that carries an id),
  `provider-fixtures.test.ts` (the shared fixtures' display model), `picker-plugin-gates.test.tsx`; mobile `pluginProviders.test.ts` (catalog types, badge, read-only section, no secret write call from `mobile/`:
  a test fails if `provider.setSecret` appears in `mobile/` for plugin ids); CLI `TestProviderList_ShowsPluginColumn`, `TestProviderSetSecret_ReadsStdinNeverArgv`; MCP `TestMCPProviderList_CarriesPluginAndGate`.
  Frames: as in the UI section; no row moves by 1 px between states.

### PD4 (docs, example, release notes)

- AD22 Release notes call out the breaking provider-id change (`<plugin>/<provider>`, reserved plugin ids `smind`/`plugin`, no shim). `TestDocs_ReleaseNotes_MentionPluginProviderIDs`.
- AD23 `public-docs/plugins-authoring.md` provider section (manifest reference, trust statement, "what a plugin provider can do to your machine", the example walkthrough pasted from a real run) and
  `TestDocs_ProviderManifestKeys_MatchCode` (the documented keys equal the Go validator's accepted keys, like `TestDocs_CapabilityTable_MatchesCode`).
- AD24 `TestExamples_HelloProvider_ManifestValid_AndAgentAnswers` (above).

## Mutation table (one mutation per load-bearing test; the test must FAIL, a build failure proves nothing; run by the Lead, listed in each PR body)

| # | File | Change | Test that must then fail |
|---|---|---|---|
| M1 | `internal/providers/providers.go` | `SetCustom` also clears the plugin layer | `TestRegistry_Layers_ReloadKeepsOtherLayer` |
| M2 | `internal/plugins/providers.go` | derive specs from installed-but-disabled plugins too | `TestPluginProvider_NotRegisteredBeforeEnable` |
| M3 | `internal/plugins/manifest.go` | drop the "absolute path outside the plugin dir" refusal | `TestProviderManifest_Invalid` |
| M4 | `internal/plugins/consent.go` | leave `inheritHome` out of the fingerprint | `TestConsent_Provider_FingerprintCoversArgvEnvValuesAndInheritHome` |
| M5 | `internal/plugins/consent.go` | print env values in the summary | `TestConsent_Provider_ValueNeverInSummaryOrLogs` |
| M6 | `internal/plugins/providers.go` | build the agent env from `os.Environ()` | `TestPluginProvider_AgentEnv_ScrubbedAndBuilt` |
| M7 | `internal/taskrunner/` launch | skip the confinement re-check at spawn | `TestPluginProvider_Launch_RechecksConfinedCommand` |
| M8 | `internal/wsapi/provider_secrets.go` | allow `provider.setSecret` for a run-scoped caller on plugin ids | `TestPluginProvider_SetSecret_RefusedForRunScoped` |
| M9 | `internal/plugins/providers.go` | pass the resolved secret to the plugin process `initialize` | `TestPluginProvider_Secret_NotInPluginProcessEnvOrInitialize` |
| M10 | `internal/wsapi/` send guard | treat a missing plugin provider as `unsupported_by_harness` / let the send through | `TestSend_PluginProviderDisabled_ProviderUnavailable_Typed` |
| M11 | `internal/gate/gate.go` | add a twin for `plugin_unavailable` | `TestGateTwins_NoTwinEntryRequired_ForPluginUnavailable` |
| M12 | `internal/plugins/manager.go` | remove: leave provider secrets behind | `TestPluginRemove_DeletesProviderSecrets_DisableKeeps` |
| M13 | `internal/wsapi/provider_instances.go` | `updateInstance` allowed on a plugin provider | `TestPluginProvider_UpdateInstanceRefused_ReadOnlyOwned` |
| M14 | `internal/taskrunner/` failover | pick a plugin provider as an automatic fallback | `TestFailover_PluginProviderOnlyWhenProfileNamesIt` |
| M15 | `web/.../provider-detail.tsx` | render the plugin provider without the badge | `plugin-providers.test.tsx` |

## DECISIONS FOR THE HUMAN (recommendation first; **all seven taken as Supervisor default 2026-11-01, Human may veto**)

AGENTS.md rule (d): none of these is decided in ADR-0042 or ADR-0046. Vetoing one changes the PR named in brackets.

- **H1. Capability and confirmation UX [PD2].** *Recommendation:* **no new `providers` capability**; the provider contribution itself is the declaration and the consent screen shows it as "runs code: ACP provider"
  with the exact argv, env keys and secret slots. Reason: capabilities mediate host-API calls and a provider makes none (it is a child process, and `provider.` stays on the plugin never-list). Alternative: a
  `providers` capability that must be declared and is shown as a group: more ceremony, same information, and one more thing to keep in the table test.
- **H2. The agent's environment and HOME [PD2].** *Recommendation:* **scrubbed by default** (fixed `PATH`, `HOME` = a private per-provider directory, no inherited env), with a per-provider **opt-in `inheritHome`**
  shown in red on the consent screen and in the fingerprint, for agents that must use the user's own login (a plugin wrapping `opencode` or `copilot`). Alternative: inherit the daemon's env like a
  `providers.custom` entry (more convenient; a plugin then gets the Human's whole environment, including any key exported to the daemon).
- **H3. Secrets [PD2].** *Recommendation:* the **ADR-0046 `provider_secrets` store**, keyed by the namespaced id, set only through the existing human-only write-only `provider.setSecret`, **no env passthrough
  for plugins**, deleted on remove and kept on disable. The plugin process itself never sees them. Alternative: reuse `plugin_settings` secrets (already write-only) at the cost of a second place and no
  gate/picker integration.
- **H4. A provider before trust [PD1/PD2].** *Recommendation:* **never registered before the Human enables the plugin with the consent fingerprint** (`plugin get` lists what it *would* add). Alternative: list
  declared providers as greyed picker rows before enable (discoverability; but an untrusted manifest then appears in pickers).
- **H5. Command forms [PD1].** *Recommendation:* a path **inside the plugin directory** or a **bare name on the fixed system PATH**; an **absolute path outside the plugin is refused** (stricter than Wave B's process
  command, because a provider also runs against every repo the Human works in). Alternative: allow any absolute path, shown on consent.
- **H6. Missing-plugin vocabulary [PD2].** *Recommendation:* a **new picker reason `plugin_unavailable`** (hard, action "Enable plugin X", **no failure-class twin**) plus a typed `provider_unavailable` send refusal,
  no new run failure class, history kept. Alternative: reuse `unsupported_by_harness` (wrong meaning and wrong action: "pick another provider" instead of "enable the plugin"); or a new failure class
  (nothing ran, so there is no run to classify).
- **H7. Failover [PD2].** *Recommendation:* a plugin provider may be a fallback/`wait` target **only when a Human-authored profile names it**, a plugin-owned profile may name only its own plugin's providers or
  core ones, and automatic failover never picks a plugin provider by itself. Alternative: allow any registered provider as a target (plugin code then receives a prompt the Human never routed to it).

Not a decision (already fixed): ACP only (ADR-0042 Q9), no OS sandbox (Q12), local-only management (Q4), no registry (Q2). A second look is only needed if the Human wants direct (non-ACP) provider plugins, which
need a connection protocol over the plugin process and are a new ADR.

## Out of scope

Direct (non-ACP) provider plugins; plugin recipes, endpoints, `extends`, usage meters, login/OAuth flows; model catalogue editing of plugin providers; passing the Human's secrets or env to a plugin; an OS sandbox;
installing agents for the Human (health text only); plugin providers on mobile management (read-only only); any change to the four core ADR-0046 harnesses; real third-party agents in tests (fake agent only);
live model calls (none are needed anywhere in this wave).

## Progress

- [ ] plan PR (this file; docs only)
- [ ] spec accepted by the Steward
- [ ] PD1  [ ] PD2  [ ] PD3  [ ] PD4

## Validation

Spec phase (2026-11-01): docs only; no code, no tests run for behaviour. Every file and line reference above was read at `f4c331d5` (`manifest.go:181`, `consent.go:111`, `providers.go` `Register`/`SetCustom`/
`ReservedID`, `providers_custom.go` `customIDRe`, `gate.go` `twins`, `failure/kinds.go`, `fakeagent/main.go` scenarios). Implementation validation (per AC, per-test results, mutation results M1-M15, frames) is
filled in by each PR.
