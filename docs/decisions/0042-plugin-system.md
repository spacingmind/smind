# 0042: Plugin system — trusted local plugins, data-first contributions, declarative UI

## Status

**Accepted — 2026-10-25** (Human: all twelve open questions as recommended, plus provider instances in Wave 0; see [Human answers](#human-answers-2026-10-25)). Proposed 2026-10-24. Spec and PR split:
[`docs/plans/active/plugins.md`](../plans/active/plugins.md).

Closes the "Extend" gap in the Build → Review → Ship → **Extend** framing the Human took from Paseo (paseo.sh): smind covers Build, Review and Ship, and has no
way for anyone but the core repo to add a panel, a command, a hook, a theme or a coding-agent provider.

Inputs (local-only notes in `docs/research/local/plugins-research.md`): Paseo's plugin docs and runtime (`refs/paseo` at `b8bb335c`: `public-docs/plugins/*`,
`packages/plugin`, `packages/server/src/server/plugins/*`, `packages/app/src/plugins/*`), `refs/deepseek-harness` (Cordis), `refs/zcode` (Claude-Code-format
plugins and marketplace), and a map of smind's own seams at `bf7285bf`. Related ADRs: 0001, 0003, 0004, 0005, 0007, 0009, 0011, 0014, 0017, 0018, 0019, 0020,
0021, 0024, 0026, 0027, 0031, 0032, 0035, 0036, 0037, 0040, 0041.

## Context

### What exists in smind today

"Plugin" has no meaning in smind's code or docs. What exists is a set of seams, most of them closed:

| Concept | State |
|---|---|
| MCP servers (ADR-0017/0018) | Data-driven: SQLite rows, `mcp.*` RPCs, injected per run (ACP list, claude `--mcp-config`; codex unsupported). A plugin can contribute rows. |
| Skills (ADR-0026/0037) | Seven bundled (`go:embed`), manifest-managed install on request; learned skills in SQLite delivered as a **read-only projection** (claude `WithAddDirs`, GLM/Kimi inline preface). Profile `skills` names bundled skills only. ADR-0026 §4: smind never writes under `~/.claude/skills`. |
| Profiles / seats (ADR-0014/0027/0041) | Data-driven (`agent_profiles`), human-authored only (ADR-0019 #6). The role pack is a hardcoded Go slice. |
| Providers | **Hardcoded**: constants, `SupportedProviders()`, a `switch` in `runner.go`, and ten touch points across daemon, web (`provider-catalog.ts` union type) and mobile. No custom-provider or ACP-command config. |
| Themes | `light`/`dark`/`system` on web (CSS variables), one `AppTheme` on mobile. No custom themes. |
| Slash commands | None in the composer (ACP `available_commands_update` is dropped as benign). The Ctrl-K palette has an open `CommandSource` registry. |
| Hooks | Fixed: worktree setup/teardown scripts (ADR-0024, human-edited), the permission decider, the git-approval classifier. None pluggable. |
| Events (ADR-0005/0009) | A closed `knownTopics` list; live-only, no replay. |
| wsapi | A hardcoded handler map, plus a run-scoped allowlist (`RunScopedMethods`, `session.identify`) that already gates MCP-spawned agents. Relay sessions are "not local": admin handlers return `forbidden`. |
| Web UI | Settings sections: a real registry. Tab kinds: a descriptor registry but a closed union (ADR-0004). Sidebar: hardcoded. |
| Mobile | Expo SDK 58, everything compiled in, **no `expo-updates`**, no registry. It holds the E2EE keys in its JS runtime (ADR-0035). |
| Desktop | Tauri thin client that loads the daemon's own UI (ADR-0012). A daemon-served plugin UI reaches web and desktop for free. |

### What the references teach

- **Paseo** ships trusted, unsandboxed TypeScript plugins, off by default (`pluginsEnabled`). The server half runs in a subprocess (`globalThis.eval` with a
  `require` that falls through to Node). The client half is compiled **by the daemon**, sent to every client as a JS string over the normal channel and `eval`'d
  in the app, including on phones, with host React/React Native/TanStack Query injected. Lifecycle hooks (`on` observe, `before` rewrite) cover config/MCP
  injection, env, follow-ups and permission answers. Version checks run on the daemon **and on each client** before it evaluates client code. A reviewed
  registry pins exact artifacts. One codebase for desktop, web and mobile is what makes it attractive. Its cost: plugin JS runs with full app privileges, and
  on mobile that is the runtime that holds the relay keys.
- **deepseek-harness** is Cordis end to end: everything, including the core, is a plugin with injected services. It has no trust model and a heavy mental
  model (fibers, realms, two dependency graphs, a vendored and patched framework). Worth stealing: named UI slots, registrations as idempotent effects,
  transactional recompose with last-good fallback.
- **ZCode** reuses the Claude Code plugin format (`.claude-plugin/plugin.json`, `marketplace.json`; skills, commands, agents, `hooks.json` shell hooks, MCP
  servers; `userConfig`). Worth stealing: sha256-pinned archives, atomic staged install with rollback, a per-plugin `data/` directory, namespaced component names,
  typed diagnostics. It has **no** consent screen or permission model, no UI code and no provider extensibility.

### What constrains the design

1. **ADR-0001: one Go binary.** No in-process Go plugins. Daemon-side plugin code must be out of process.
2. **Three clients with different code-delivery rules.** Web and desktop are served by the daemon. Mobile is a store-distributed native app with no OTA, whose JS
   runtime holds the E2EE keys (ADR-0035), reached through a relay that must stay blind (ADR-0007/0011).
3. **Agents and repositories never authorize execution** (ADR-0019 #6, ADR-0024): configuration that runs code is human-authored, locally.
4. **No smind-side auto-approval** (ADR-0019 deleted it because command-string matching was bypassable), so a hook cannot be a permission oracle.
5. **No installed base yet** (memory: breaking changes are fine; note them).

## Decision

### 1. Scope of v1: data first, then code, then UI, then providers

Plugins are delivered in **four waves**, ordered by value per unit of new risk. Each wave is independently shippable and each ends at a point where stopping is
fine.

| Wave | Extension points | New execution surface | Why here |
|---|---|---|---|
| **0** prerequisite (no plugin concept) | **Provider registry from data** (replace the hardcoded `switch` and the ten touch points with one registry; add `providers.custom` ACP entries to `config.yaml`) | none | Providers are blocked by the registry, not by the lack of plugins. Useful alone, and the cheapest route to "add a coding agent". |
| **A** host + declarative contributions | manifest, install/enable/disable/remove/link, consent screen, `$SMIND_HOME/plugins`; contributions: **MCP servers, skills, profiles/role packs, prompt slash commands, themes** | none beyond what the user could already configure by hand | All data, so identical on every client; reuses existing registries; the first useful plugin needs no code. |
| **B** plugin process + hooks | a long-lived stdio JSON-RPC process per plugin; **lifecycle hooks** (observe + one `before` hook); plugin RPCs; per-plugin settings | a user-approved subprocess | Hooks and RPCs need code, and the process is also the data source for Wave C. |
| **C** declarative UI | **sidebar items, task/workspace panels, palette entries** as view documents rendered natively by web and mobile; RPC-backed slash commands | none on clients (data only) | The visible half of "Extend", delivered without shipping code to phones. |
| **D** providers as plugins | `contributes.providers`: ACP agents with a model catalog | the declared ACP command | Rides on Wave 0 + A; direct-SDK providers stay out of v1. |

**Not in v1** (parked, with the reason): JS UI bundles for web/desktop (needs a sandboxing decision, Q5); permission-answering hooks (ADR-0019); a registry or
store (hosting, review process, money); direct (non-ACP) provider plugins; composer pills, timeline renderers, file panels, attachment sources; i18n for plugin
strings (smind has no i18n yet; plugin strings are plain English until the app has a mechanism); mobile-side install; OS-level sandboxing; first-party features
re-expressed as plugins.

This differs from the Human's ordering guess (providers, UI, commands, hooks). Providers and commands are first-class in the list, but data-only contributions
and the registry refactor come first because they carry no new risk and unblock the rest, and UI is gated on a decision (declarative vs JS) that is cheaper to
get right with the process and hook machinery already in place.

### 2. Trust model: trusted local plugins, with disclosure and mediation, not a sandbox

- **Plugins are code you chose to run as yourself.** smind says so in the UI and the docs. A plugin that declares a process, an MCP server or an ACP provider
  runs with the daemon user's privileges: files, network, credentials. That is already true of any MCP server or setup script the user configures (ADR-0018,
  ADR-0024), and no smind-side check can make it false. smind does not claim isolation it does not have.
- **Off by default.** `plugins.enabled: false` in `config.yaml`; turning it on is a local human action (settings UI on a local connection, or `smind plugin
  enable-all` / editing the file). Each installed plugin then has its own enable switch. Neither can be changed by an agent caller (extends ADR-0019 #6) or over
  the relay (relay sessions are "not local"; same `forbidden` as the other admin handlers).
- **Sources in v1 are local only:** a directory (`smind plugin install <dir>`, copied), a linked directory for development (`smind plugin link <dir>`, not copied,
  reload on request), and a Git URL **pinned to a commit** or an archive **pinned to a sha256** (ZCode's pattern; size, entry-count and path-escape limits on
  extraction; symlinks rejected). A workspace's repository can never install or enable a plugin (ADR-0024).
- **Consent screen at install and on any update that changes declared capabilities.** It lists what will run (process command, MCP commands/URLs, ACP commands,
  hook events), what host API groups the plugin may call, and the version, source and hash. Declared capability groups are **mediated, not sandboxed**: the host
  enforces them on every host-API call (a plugin that did not declare `tasks.write` gets `forbidden`), but a plugin process is free to do anything the OS lets the
  user do. The consent screen separates "data only" plugins (Wave A, Wave C views: nothing runs) from "runs code" plugins.
- **Identity of the plugin on the host API.** A plugin process does not receive the daemon token. Its host calls arrive on its own stdio channel and are executed
  in-process through a **plugin-scoped session**, the same mechanism as the run-scoped allowlist (`session.identify`, `RunScopedMethods`) with an allowlist built
  from the declared capability groups. What a plugin sends to a client is tagged with its id (`[message from plugin X]`, as agent-sourced queue items already are in
  ADR-0021) so the user can always tell plugin-originated content from the agent's or their own.
- **Honest limits, stated in the ADR so nobody builds on a wrong assumption.** A plugin process can read `~/.smind/token` and `relay-keypair.json` (0600, same
  user); it can open sockets. The mitigations are: the process gets a scrubbed environment (no `SMIND_*` secrets, an allowlisted `PATH`), a per-plugin working and
  data directory, process-group/job-object teardown (`internal/winjob` on Windows), a crash-loop breaker, and the consent screen. Real isolation (OS sandbox:
  seatbelt, bwrap/landlock, Windows job limits) is parked; ADR-0037's sandbox-attested arms are the precedent if the Human wants it later.
- **Registry and signing: none in v1.** A reviewed registry (Paseo) or a CDN catalog (ZCode) needs hosting, a review policy and someone to run it. That is a
  money and operations decision for the Human and gets its own ADR if wanted. Nothing in the manifest or the on-disk layout prevents adding one.

### 3. Packaging, versioning, loading

**Layout.** `$SMIND_HOME/plugins/<id>/` holds the installed copy; `$SMIND_HOME/plugins/<id>/data/` is persistent per-plugin storage that survives updates and is
deleted on remove unless `--keep-data`. Install state lives in SQLite (`plugins`: id, version, source, hash, enabled, consented-capabilities hash, installed_at),
**not** in the plugin directory, so a plugin cannot enable itself. Installs are staged in a temporary directory and swapped atomically; a failed install,
update or reload keeps the last good version (the ZCode/dsh transactional pattern). Ids match `^[a-z][a-z0-9-]{0,63}$`; every contributed name is namespaced
`<id>:<name>` (MCP servers, skills, profiles, commands, themes, providers) so plugins cannot shadow core or each other. Contributed rows are **owned**: read-only
in the UI ("from plugin X"), removed with the plugin, never editable by agents.

**Manifest.** `smind-plugin.json` at the plugin root:

```json
{
  "id": "acme-review",
  "name": "Acme review tools",
  "version": "1.2.0",
  "api": 1,
  "engines": { "smind": ">=0.15.0" },
  "description": "Reviews changes before merge",
  "process": { "command": ["node", "server.js"] },
  "capabilities": ["events.observe", "tasks.read", "tasks.write", "hooks.beforeRun"],
  "settings": [{ "key": "token", "type": "secret", "label": "API token", "required": true }],
  "contributes": {
    "mcpServers":  [{ "name": "search", "transport": "stdio", "command": ["node", "mcp.js"] }],
    "skills":      ["skills/review"],
    "profiles":    ["profiles/reviewer.json"],
    "commands":    [{ "name": "review", "description": "Run the review bot", "prompt": "commands/review.md" },
                    { "name": "refresh", "description": "Refresh review state", "rpc": "refresh" }],
    "themes":      [{ "id": "dusk", "label": "Dusk", "file": "themes/dusk.json" }],
    "panels":      [{ "id": "overview", "title": "Review", "icon": "ListChecks", "where": "task",
                      "view": { "rpc": "overview.view" } }],
    "sidebar":     [{ "id": "overview", "title": "Review", "icon": "ListChecks", "opens": "overview" }],
    "hooks":       [{ "event": "run.finished" }, { "event": "run.beforeStart", "onError": "open" }],
    "providers":   [{ "id": "my-agent", "label": "My agent", "acp": { "command": ["my-agent", "--acp"] },
                      "models": [{ "id": "large", "label": "Large" }] }]
  }
}
```

Paths are relative, forward-slashed, `..`-free and confined to the plugin directory. Unknown top-level keys are ignored; unknown keys inside `engines` and
`capabilities` are rejected, so a misspelled constraint cannot silently skip a check (Paseo's rule). Sizes are capped (manifest, each view document, icons).

**Versioning.** Two independent gates, both checked at install, enable, reload and daemon start:
- `api`: an integer, the plugin API major. The daemon supports a set (v1: `{1}`); a plugin whose `api` is not in the set does not load and the error says why.
  The plugin API is **additive within a major**; a breaking change bumps it and the daemon may support two majors for a release.
- `engines.smind`: a semver range on the daemon version. A missing range is rejected.

Clients are versioned too, because the phone and the daemon upgrade independently. Each client reports its **UI schema level** (`ui: 1`) when it connects; the
daemon sends a plugin's panels and sidebar items only if the client supports the view schema level they need, and a client renders an unknown view node as an
"unsupported in this app version" placeholder instead of failing (the Paseo "each client checks itself" rule, as data, with no client-side eval).

**Loading and reload.** `smind plugin reload <id>` (and a button on the plugin's page) re-reads the manifest and contributions, swaps registrations
transactionally, restarts the process if there is one (SIGTERM, 5 s, then kill the process group / job object) and emits `plugin.updated`. A failed reload
keeps the last good state and reports the error on the plugin (`smind plugin ls`, the page, `smind plugin logs <id>`). Linked directories add an opt-in
`--watch` (debounced, manifest and declared files only); installed copies never hot-reload on their own. Per-plugin logs are a bounded ring buffer, with host
messages (`[smind]` load/ready/stop/crash) and the process's stderr; secrets from `settings` are redacted on every read path (ADR-0018 rule).

### 4. API surface: a small stable plugin API over the existing wsapi, not a second API

There are three surfaces and each is small:

1. **Declarative contributions** (the manifest). This is the stable contract for most plugins and needs no code.
2. **Plugin process ↔ host:** bidirectional JSON-RPC 2.0 over the process's stdio (LSP framing), so no sockets, no tokens and the same behaviour on Windows.
   - *Host → plugin:* `plugin.initialize {api, smindVersion, settings, dataDir}`, `hook.<event>`, `rpc.<name>`, `plugin.shutdown`.
   - *Plugin → host:* **a documented subset of wsapi methods plus `events.subscribe`**, gated by capability group: `tasks.read` (`task.list/get/tree`, run and chat
     reads), `tasks.write` (`task.send`, whose queue source is `plugin:<id>`, rate-limited per plugin and per task tree), `events.observe` (subscribe to an allowlisted
     set of topics), `mcp.read`, `profiles.read`, `files.read` (workspace-confined reads), and `ui.notify` (a toast, not a push; ADR-0040 push stays core-only).
   Everything else is not callable, so the plugin API **is** the wsapi contract, narrowed. A breaking wsapi change is a breaking plugin API change and must bump
   `api` or keep a shim; the capability table lives in one Go file next to `RunScopedMethods` with a test that every listed method exists.
3. **Client ↔ plugin (UI):** two generic wsapi methods, `plugin.list` / `plugin.view {pluginId, panelId, context}` and `plugin.call {pluginId, rpc, params}`,
   which the daemon routes to the plugin process, plus one event topic `plugin.event {pluginId, kind}` for push refresh. Clients never get a method per plugin,
   so `knownTopics` and the handler map stay closed.

**Events and hooks.**
- *Observe:* `events.observe` delivers a subset of the ADR-0009 topics (task/run/permission/chat/workspace/profile/forge/git), live and best effort exactly as
  ADR-0005 states: no replay, at most 256 queued, a synthetic `event.dropped`. Payloads are redacted copies, never live objects. A slow plugin cannot slow the bus.
- *One `before` hook in v1:* `run.beforeStart`. It may append instructions, add environment variables, and add MCP servers **that the plugin itself contributes**;
  it may not change the provider, the cwd, the permission mode or the profile. Timeout 10 s. `onError` is declared per hook, default **open** (the run proceeds and
  the failure is surfaced on the task and the plugin page); `closed` makes the start fail and is shown on the consent screen. Ordering: plugin id, then
  registration order, no deep merge.
- *Follow-ups* (`task.send` from `run.finished`) are allowed with the rate limits above and the `plugin:<id>` source header; a plugin cannot spawn children
  (`orchestration.maxDepth` and the tree guard apply, ADR-0041).
- *Permission answering is out of v1.* A hook that approves tool calls is smind-side auto-approval, which ADR-0019 removed deliberately. Observing
  `permission.pending` and notifying is allowed.

**Relation to ADR-0032 and the clients.** Plugin data is ordinary client state: `plugin.list` and `plugin.view` are react-query keys on web and mobile,
`plugin.event` and `plugin.updated` invalidate them through the existing daemon-events bridge, and UI-only state (which panel is open) lives in the existing
zustand stores. Plugin UI never gets its own cache or its own store (ADR-0032's "shared stores/pickers" rule). Panels open in the existing tab system as one new
tab kind `plugin` keyed `${taskId}:plugin:${pluginId}:${panelId}` (ADR-0004); sidebar items and palette entries come from two small registries replacing the
hardcoded sidebar list's extension point and reusing the open `CommandSource` registry.

### 5. UI: declarative view documents, rendered natively; no remote JS on clients

A panel is a **view document**: JSON, schema level `ui: 1`, produced by the plugin (static file or `rpc`) and rendered by a native renderer that web and mobile
each implement once. v1 vocabulary: `stack`, `row`, `heading`, `text`, `markdown`, `keyValue`, `list`, `table`, `badge`, `progress`, `code`, `diff`, `form`
(text, select, toggle, secret), `button`, `empty`, `error`. Interactions are **actions** the renderer maps to host operations only: `rpc` (a `plugin.call` on the
same plugin), `prompt` (insert text into the composer; the user presses send), `command` (run a palette command), `openPanel`, and `openUrl` (OS browser, with the
host shown, behind a confirm). There is no `eval`, no remote fetch, no HTML: markdown has raw HTML disabled and remote images are not loaded (images are
`data:` URIs or daemon-served plugin assets, size-capped), so a view cannot beacon. Icons are a name from the **client's Lucide registry** (`lib/icons.ts`) or a
sanitized inline SVG under Paseo's rules (one `<svg>`, ≤64 KiB, no script/style/foreignObject/handlers, fragment-only hrefs). Colors come from theme tokens
(`tone: "success" | "warning" | "danger" | "muted"`), never raw values, so every theme and both color schemes work. Themes are JSON token sets validated for
completeness against the host's token list (dsh's lesson: no validation of completeness produces broken themes).

Why declarative and not Paseo's evaluated bundles: on mobile an evaluated plugin runs in the same JS runtime as the E2EE session keys and the relay pairing, so
"the relay cannot read your data" would depend on every plugin author; it also leans on app-store rules about downloaded code and cannot be sandboxed in React
Native. A declarative renderer cannot exfiltrate through the client at all: the only egress from a view is a daemon RPC to its own plugin. The cost is
expressiveness (no custom canvases, no bespoke components); the vocabulary grows by schema level, not by escape hatch. Whether web and desktop should also get a
sandboxed-iframe JS panel is a separate, later decision (Q5) and does not change the mobile rule.

### 6. Relation to existing concepts: contributions, not replacements

MCP servers, skills, profiles and themes **remain first-class core concepts** with their own tables, RPCs and settings UI. A plugin contributes **owned rows**
(`owner_plugin_id`, namespaced name, read-only, deleted with the plugin, hidden when the plugin is disabled).
- *MCP servers:* inserted into the same registry; enabled per workspace as today; `${setting.key}` substitution from the plugin's settings (secrets only into env/headers, ZCode's rule).
- *Skills:* delivered through the **learning projection mechanism** (read-only projection, claude `WithAddDirs`, GLM/Kimi inline preface; codex gets nothing, as
  today), never by writing into `~/.claude/skills` (ADR-0026 §4). `AgentProfile.Skills` may name `<plugin>:<skill>` in addition to bundled names.
- *Profiles:* inserted as owned profiles; the plugin cannot set a permission mode more permissive than the user's default without it showing on the consent screen.
- *Bundled smind skills, the role pack, learned skills:* unchanged. Re-expressing the role pack as a first-party plugin is a good later dogfood, not v1.
- *Worktree scripts (ADR-0024) and the git-approval classifier:* unchanged, not replaced by hooks.
- *Claude-Code-format plugins (ZCode's route):* a **read-only import adapter** can map `.claude-plugin/plugin.json` skills, commands and `.mcp.json` into the
  same contributions, so existing community plugins work as data-only plugins. Not v1 core; proposed as Q6.

### 7. Provider plugins (Wave 0 + D)

Wave 0 turns the provider list into a registry: `ProviderInfo` gains the fields the ten touch points hardcode today (kind `acp|claude-native|codex-native`,
command, default modes and models, diagnostics probe, learning-delivery mode, login/account capability), `SupportedProviders()` reads it, and web and mobile treat
provider ids as strings with a catalog fetched from the daemon (the fallback list stays for offline rendering). `config.yaml` gets `providers.custom` entries
(ACP command, env, models) so a user can add an agent with no plugin at all; Wave D lets a plugin contribute the same entries, ids `<plugin>/<provider>`. A
contributed provider reuses `runACP`, run events (ADR-0008/0010), usage (ADR-0020), model selection (ADR-0025) and permission modes (ADR-0019) unchanged. Direct
(non-ACP) provider plugins, which need a connection protocol over the plugin process, are not in v1.

### 8. Mobile and relay

- Plugin management (install, enable, remove, settings with secrets) is **local only**. The phone can list plugins and their state, render panels and trigger
  actions (it can already create and message tasks, so `plugin.call` is not a new class of power), and show hook failures.
- View documents, plugin RPCs and `plugin.event` are ordinary wsapi frames inside the E2EE channel (ADR-0035). The relay sees the same ciphertext envelopes,
  sizes and timing as today (ADR-0007/0011). No plugin code reaches the phone, so there is no client-side path to the keys. View documents are capped (256 KiB)
  and `plugin.call` payloads follow the existing frame limit.
- Plugins cannot send push notifications in v1 (ADR-0040 stays core-only); `ui.notify` is an in-app toast for connected clients.
- A plugin that needs a client feature the phone lacks degrades per node (the "unsupported" placeholder), never per app.

## Alternatives considered

- **No plugin system.** Every new provider, panel or theme is a core PR; nothing is shareable; the Human's Extend gap stays open. Wave 0 would still be worth doing
  and is split out so this alternative is not all-or-nothing.
- **MCP-only extension.** MCP already gives agents tools and smind packages it (ADR-0017/0018). It does not provide UI, themes, commands, hooks or providers, and
  it is agent-facing: a human-facing panel or a pre-run hook has nowhere to live. Kept as one contribution type, not the extension model.
- **Paseo's model: compile TS on the daemon, `eval` on every client.** One codebase on three clients and a mature SDK to copy. Rejected for mobile because
  plugin JS would run beside the E2EE keys with no sandbox and with store-policy exposure; for web/desktop it is the same code with the same privileges as the
  daemon UI (acceptable under "trusted", and revisitable under Q5). Keeping only the *server* half, hooks and RPCs, is what Wave B does, in any language over stdio.
- **Full Cordis / everything-is-a-plugin (deepseek-harness).** Would mean rewriting the core around an injection framework; Go has no equivalent and ADR-0001 says one
  binary. Its fibers, realms and two dependency graphs are the maintenance cost its own docs warn about. Ideas kept: named slots, effects as idempotent registrations,
  transactional reload.
- **Adopt the Claude Code plugin format wholesale (ZCode).** Free ecosystem compatibility, but markdown and shell hooks only: no UI, no providers, no consent
  model, and `hooks.json` shell hooks conflict with ADR-0019/0024 posture. Kept as an import adapter (Q6), not the native format.
- **WASM plugins (wazero, pure Go).** A real sandbox and compatible with one binary. Rejected for v1: providers, git, subprocess and network I/O are exactly what
  plugins here need, WASI's story for them is weak, and authors would need a new toolchain. Remains the candidate if the Human later wants isolation for hooks.
- **Dynamic Go plugins (`plugin` package, hashicorp/go-plugin gRPC).** `plugin` is Linux/macOS only and ABI-fragile (no Windows, ADR-0029). go-plugin is
  sound but heavier than stdio JSON-RPC for this surface and drags in gRPC for authors.

## Rationale

- **Data-first matches smind's shape.** Most of the value the references show (commands, skills, MCP, themes, panel content) is data. Data has the same behaviour
  on all clients, needs no consent beyond disclosure, and reuses registries that already exist (ADR-0017/0018/0014/0026).
- **The honest trust model is the cheapest correct one.** smind's users already run MCP servers and setup scripts as themselves. A consent screen, mediation of
  the host API, a scrubbed environment and local-only management add real safety; claiming a sandbox would add none and mislead. Matching Paseo's "off by default,
  trusted local plugins" meets the Human's stated reference while fixing its gap (no consent or capability disclosure in either reference).
- **Declarative UI is the only design where the E2EE promise survives plugins.** Everything that reaches the phone is data and travels in the existing encrypted
  channel; there is nowhere for plugin code to run next to the keys.
- **Narrowing wsapi beats inventing an API.** The scoped-session pattern exists (`RunScopedMethods`), the contract is already tested, and a plugin author can use
  the same `smind` CLI and docs to explore it. The cost is that wsapi changes become plugin-API changes, handled by `api` and the capability table test.
- **Provider extensibility needs the registry, not the plugin.** Doing Wave 0 first removes a ten-file hardcoded change for every future provider whether or not
  plugins ship.

## Consequences

- New `$SMIND_HOME/plugins/`, a `plugins` table and an `owner_plugin_id` column on `mcp_servers`, `agent_profiles` and the skill projection; a `plugin.*` wsapi
  group and one event topic; a **breaking** change to the web `provider-catalog.ts` union type and mobile `providerCatalog.ts` (provider ids become strings). No
  installed base, so no migration shim (note in release notes).
- A new tab kind and two registries in the web UI; a declarative renderer implemented twice (web and mobile). Divergence between the two is the main ongoing
  cost, so the schema ships with a shared JSON fixture suite (every node type, rendered on both, screenshot-verified in light and dark).
- Windows (ADR-0029/0030): process spawning follows the existing shim rules (`.cmd` launchers, job objects); `plugin link --watch` uses polling fallback.
- **Licensing (ADR-0003, AGPL):** a plugin that talks to the daemon over stdio JSON-RPC and wsapi is a separate program, but the Human should confirm the
  position and whether `plugin-examples/` carries a more permissive licence. This ADR does not give legal advice.
- Support burden: third-party plugin crashes will look like smind bugs unless every plugin-originated message, row and error is labeled with its id (required in the AC).

## Open questions for the Human

Each has a recommendation; the plan assumes the recommendations. Answers change the plan's wave list only where noted.

1. **Scope and order (§1).** Waves 0 → A → B → C → D, each independently shippable, v1 ending after the last wave you approve. *Recommendation:* approve
   0 + A now (Wave 0 is justified without plugins; A is data-only and low-risk) and decide B–D after seeing A on screen. *Changes the plan:* which Leads start.
2. **Trust model (§2).** Local trusted plugins, off by default, consent screen, mediated capabilities, no sandbox claim, no registry and no signing in v1.
   *Recommendation:* yes. A registry/store is a separate ADR when you want to spend on hosting and review.
3. **Install sources (§2).** Local directory, link (dev), Git pinned to a commit, archive pinned to a sha256; no marketplace index. *Recommendation:* yes; add a
   `marketplace.json`-style index file in a later ADR only if a catalog is wanted.
4. **Phone as management surface (§8).** View, render, act on plugins; never install, enable, remove or edit secrets from the phone. *Recommendation:* local-only
   management in v1 (consistent with relay admin handlers).
5. **Client code on clients (§5).** Declarative view documents only; **never remote JS on mobile**; web/desktop JS panels (sandboxed iframe on a separate origin,
   postMessage bridge) deferred and decided later. *Recommendation:* declarative only for v1; revisit web/desktop JS only if a concrete plugin cannot be expressed in
   the vocabulary. This is the decision that most differs from Paseo; overturning it means giving up the E2EE argument on mobile.
6. **Claude-Code-format import (§6).** A read-only adapter so `.claude-plugin` skills/commands/MCP work as data-only plugins. *Recommendation:* defer past Wave A,
   build it if users bring plugins; native manifest stays `smind-plugin.json`.
7. **Hooks (§4).** One `before` hook (`run.beforeStart`, limited edits), default `onError: open`, no permission answering (ADR-0019), follow-ups rate-limited.
   *Recommendation:* yes. Alternative: add `run.beforeStart` fail-closed as the default for plugins that declare a policy purpose; I would not.
8. **Plugin process protocol (§4).** Bidirectional JSON-RPC over stdio, host calls executed through a plugin-scoped session over a narrowed wsapi (vs. a socket
   with a token, or MCP as the protocol). *Recommendation:* stdio JSON-RPC; MCP stays a contribution type for agents.
9. **Provider scope (§7).** ACP providers only in v1, with `providers.custom` in `config.yaml` landing first and independently; direct-SDK providers later.
   *Recommendation:* yes; start Wave 0 right after acceptance since it needs no plugin decision.
10. **Dogfood and examples.** Ship two first-party example plugins in `plugin-examples/` (a data-only theme/commands pack, a process plugin with a panel and a
    `run.finished` hook) as the test fixtures and docs, and later move the role pack onto the plugin path. *Recommendation:* examples yes, role-pack move later.
11. **Licensing (Consequences).** Confirm the stdio/wsapi boundary position under AGPL and the licence for `plugin-examples/`. *Recommendation:* permissive
    (MIT) for examples and the plugin protocol docs; counsel if third-party distribution grows.
12. **OS sandbox (§2).** Whether to invest in seatbelt/bwrap/job-object confinement of plugin processes. *Recommendation:* no in v1; revisit when a registry or
    non-local sources are on the table, because that is when the threat model changes.

## Human answers (2026-10-25)

All twelve open questions are answered **as recommended**: Wave 0 + Wave A start now and B–D are decided after A is on screen (Q1); the trust model (Q2);
install sources without a marketplace index (Q3); local-only management, the phone views and acts but never installs (Q4); declarative view documents only,
no remote JS on mobile, web/desktop JS panels deferred (Q5); Claude-Code-format import deferred past Wave A (Q6); one fail-open `run.beforeStart` hook and no
permission answering (Q7); stdio JSON-RPC (Q8); ACP-only plugin providers with `providers.custom` first (Q9); two first-party examples, role-pack move later
(Q10); MIT for examples and protocol docs (Q11); no OS sandbox in v1 (Q12).

**Addition to Wave 0: provider instances.** A `providers.custom` entry may also *extend* a core provider (`claude-native`, `codex-native`) instead of naming an
ACP command: it keeps that provider's runner and adds a label and environment from the existing config-directory allowlist (`CLAUDE_CONFIG_DIR`, `CODEX_HOME`,
ADR-0013 amendment / #603). Two instances of the same CLI then run side by side (for example "Claude" on `~/.claude` and "Claude S" on `~/.claude-scopedocs`),
which is how Paseo's `extends` + `env` providers work. Settings → Providers lists `~/.claude*` and `~/.codex*` directories found in the daemon user's home as
one-click suggestions; discovery reads directory names only, never file contents or credentials.

### Implementation note, 2026-10-25 (Wave 0, daemon side)

`internal/providers` is the registry (one `Spec` per provider, a leaf package); `providers.custom` holds ACP entries and instances (`extends: claude-native|codex-native`
with an allowlisted env), validated with the field named. An instance runs the extended provider's code with its own launch env, appears in `provider.list`
(`runner`, `extends`, `custom` are additive wire fields; `kind` keeps meaning auth), the health check and the run-usage/cooldown grouping (all keyed by provider id),
and is not a reflector source. Humans add one through `provider.dirSuggestions` (directory names under the daemon user's home only) and `provider.addInstance`;
`provider.reload` re-applies config.yaml. Web, mobile and the Settings UI follow in P0.2.

### Implementation note, 2026-10-25 (Wave A, PA.1: host foundation and install)

`internal/plugins` holds the manifest (`smind-plugin.json`, 256 KiB cap, every error names its field and the fix), a small semver/range implementation (no semver module is in go.mod; prereleases follow the npm rule, a `dev` build skips `engines.smind`), the `plugins` table (disabled by default; the installed version's consent summary and fingerprint, any pending update, the last error) and the on-disk layout `$SMIND_HOME/plugins/<id>/` with `data/` inside, `.staging/`, `.pending/` and `.trash/` beside it.

- **Staged install.** Every source is fetched into `.staging/s-*`, checked, validated and only then moved into place with a rename; a failure at any point deletes the staging directory and leaves no files and no row. A linked source is never copied and never deleted by `remove` (only `plugins/<id>/`, which holds just `data/`, is removed).
- **Sources.** A directory (copied), a link, `git+<url>@<40-hex sha>` and `<archive>#sha256=<64-hex>`. A Git ref that is not a full commit sha fails `unpinned_ref`; the fetch is `git fetch --depth 1 <url> <sha>` into a fresh repo with a scrubbed environment (no inherited config, credentials, hooks or filters, prompts off, only the `file` and `https` transports, a 2-minute timeout) and the checked-out HEAD must equal the pin. **`ssh` is not accepted in v1** (it would need credentials the scrubbed environment deliberately does not have); `http`, `ext::`, option-looking URLs and URLs carrying a password are refused. The archive is copied once into staging while its sha256 is computed, compared with the pin before anything is extracted, and extracted from that same copy (`hash_mismatch` otherwise).
- **Extraction (AC5).** tar, tar.gz and zip, format detected from the bytes. Rejected: `..`, absolute paths, drive letters and `:`, backslashes, NUL, Windows device names, symlinks and hardlinks (never followed), devices and FIFOs, duplicate and case-colliding names (also directories), a file with entries under it, more than 5000 entries, 16 MiB per file, 64 MiB in total (counted on the bytes actually written, not on header sizes; the gzip stream is capped too). A directory source goes through the same checks (`.git` at the top is skipped; a symlink anywhere fails; the root may be a symlink the Human typed, resolved once).
- **Consent (AC1, AC7).** `Consent` lists what would run (process, MCP stdio commands, ACP commands, hook events with `onError`), the capability groups, source, version and content hash. Its fingerprint covers only the fields that run or grant something. `plugin.enable` takes that fingerprint as `confirm`, so a bare id never enables; the CLI shows the summary and needs `--yes` or an interactive y/N (a non-terminal without `--yes` refuses). An update with the same fingerprint applies at once; a changed one is staged in `.pending/<id>/`, the previous version keeps serving, and applying needs the Human (`update <id> --yes`); the staged tree is re-hashed before it goes live. A failed update or reload keeps the last good version and records its error. For a **linked** plugin a reload that changes the fingerprint disables it until re-confirmation (the old files are gone, so there is nothing to keep serving). At daemon start leftovers are deleted and an enabled plugin whose files no longer match their install hash, or whose consent no longer matches, is disabled.
- **Guards (AC3).** `plugin.install|link|enable|disable|remove|update|reload` are refused with `forbidden` for a run-scoped caller and for any connection whose `requestContext.local` is false (the existing flag: set for the token `/ws` connection, false for `ServeDevice` and the legacy `ServeTransport`); a nil context fails closed. `plugin.list` and `plugin.get` are readable over the relay and are not on `RunScopedMethods`. Settings edit is not in PA.1 (settings arrive with PB.2).
- **Global switch (AC2).** `plugins.enabled` in `config.yaml`, default false, read on every call (no restart) and failing closed on an unreadable file. With it off, `Active()` is empty, `plugin.list` reports `globalEnabled: false` with no plugin `active`, and install, link, enable, update and reload fail `plugins_disabled`; disable and remove still work. There is no RPC for the switch.
- **Honest limit.** The refusals are a drift guard, like `RunScopedMethods`: an agent with a shell can read the daemon token and `config.yaml` as the same user, so it can edit the switch, call `plugin.install` unidentified and read the fingerprint it must echo. The real boundary stays the provider permission mode (ADR-0019) plus the fact that nothing a plugin declares runs in PA.1; Wave B (a plugin process) must not start until the Human decides whether the switch and the consent need a stronger local-presence proof than a file edit and a flag.

### Implementation note, 2026-10-25 (Wave A, PA.2: contributions)

An enabled plugin's `mcpServers`, `skills` and `profiles` become real. Owned rows are **derived, never stored** (a deliberate departure from the `owner_plugin_id` column the Consequences section sketched): `internal/plugins` computes them from the cached, validated manifests of the *active* plugins on every read, and one `store.Overlay` installed on the store makes every registry over that database (the wsapi one and the runner's own) see them. They vanish by construction on disable, remove and a flip of `plugins.enabled`, can never be edited or leak into the user's tables, and nothing needs migrating. Because profiles are referenced by id everywhere (task send, seat admission, failover, run provenance), an owned row gets a stable synthetic id in `[2^40, 2^41)` (a hash of plugin, kind and name, probed past a collision); autoincrement ids never reach it and JSON numbers stay exact in JavaScript. `GetAgentProfile` resolves an owned id through the overlay; the stored-row writers refuse it.

- **Wire (additive).** `mcp.*` results carry `owner` (plugin id or `null`) and `readOnly`; `profile.*` rows carry `owner` and `readOnly` (the profile wire is still the store struct, so these two are the only lowercase keys); new read-only `skill.list` returns bundled and active-plugin skills (`name`, `source`, `owner`, `readOnly`, `description`); `plugin.get` and `plugin.list` carry `conflicts` and the consent now carries `skills`, `profiles` (with `escalations`) and `linkWarning`. A write to an owned row fails with the typed code `read_only_owned` and `data: {plugin}` for every caller, human, agent and relay alike. New event `plugin.updated {id, globalEnabled}` (`id` is empty for a switch flip); each appearing or vanishing owned row also publishes the existing `mcpServer.created|deleted` and `profile.created|deleted`, so current clients refetch with no change. A 2 s watcher announces a flip of `plugins.enabled`, which is a file edit with no daemon hook.
- **MCP.** Owned servers are named `<plugin>:<name>` in lists and reach a run as `<plugin>__<name>` (a colon does not survive every protocol's key rules), through the same `ListForWorkspace` source both runners already use, so ACP and claude-native both receive them with no new injection site. A user server with the same run name wins. `env` and `headers` may hold `${setting.KEY}`, nowhere else (the manifest rejects a placeholder in `command` or `url`); the substitution runs only inside the run-injection form and its secret is also in the set `mcpSecrets` scrubs from run errors. Setting storage is PB.2: the daemon wires no provider, so a placeholder server is listed but not injected and `plugin.get` says which setting is unset; `Manager.SetSettings` is the seam the tests use.
- **Skills.** Delivered by the projection mechanism, never by writing a skills directory: claude gets a content-addressed read-only add-dir under `<home>/plugin-projection` (built by `projection.ProjectFileSet`, SKILL.md rewritten with host-written frontmatter so a plugin cannot smuggle `allowed-tools` or hooks), ACP providers get a marker-wrapped inline preface under a byte budget, Codex gets nothing. Every enabled plugin's skills go to every user run, like learned skills; `AgentProfile.Skills` accepts `<plugin>:<skill>` (validated against active plugins at write time, inlined into the role text at run time, skipped when the plugin is off so the profile still loads).
- **Profiles.** `contributes.profiles` files are JSON with the profile fields (`childProfiles` names the plugin's own profiles; `onRateLimit` is `none` or `wait`). Owned profiles are validated by the profile registry's provider checks; one that fails is left out and listed as a conflict. A stored profile may not name an owned profile as a fallback or child, and a user profile may not take an owned name (an existing one wins and the owned row is hidden with a conflict note). Consent flags a profile that sets a mode other than default, plan or read-only, auto-accepts, has orchestration tools or may launch children; only flagged profiles enter the fingerprint, so an update adding one stays pending (AC7) and a plugin with no such profile keeps its PA.1 fingerprint.
- **Open for the Human, gating PB.1** (Supervisor defaults 2026-10-25, Human may veto): (a) loopback-only install and enable: today "local" means any direct connection, including a password-authenticated remote one; (b) the same-user boundary: an agent with a shell can read the daemon token and `config.yaml`, flip the switch and call `plugin.install` unidentified. Defaults recorded in the plan: git over ssh stays out of v1; `link` sources carry a warning (consent `linkWarning`, printed by `smind plugin enable`; the Plugins page shows it in PA.4).

### Amendment, 2026-10-25 (Human): remote management and the same-user limit

Plugin management (install, link, enable, disable, remove, update, reload, and later settings and secret edits) is refused on a direct connection that is not loopback unless `plugins.allowRemoteManage: true` is set in `config.yaml` (default false, read on every call, fails closed); the refusal is `forbidden` and names the setting. "Loopback" is the daemon's own `ChannelLoopback`, which includes a same-host TLS-terminating reverse proxy. Relay sessions and run-scoped agent callers stay refused whatever the setting is; reads are unaffected. Separately, the Human's position is that confining what an agent can read is the job of the agent provider's sandbox and permission settings, not smind's: there is **no smind-level boundary** against an agent running as the same user reading the daemon token or `config.yaml`, flipping `plugins.enabled`, or calling `plugin.install`, and the docs say so plainly. A later small item adds deny rules for smind's own secrets to the claude-native permissions. This replaces the two questions the PA.1 and PA.2 notes held open for the Human; PB.1 is no longer gated on them.

### PA.3 note, 2026-10-26: prompt commands and themes

- **Commands.** `contributes.commands` entries with `prompt` are read at install and reload (UTF-8, no NUL, non-empty, at most 16 KiB; a duplicate name or a bad file rejects the plugin). `plugin.commands` (read-only, relay-readable like `plugin.list`) returns the ACTIVE plugins' prompt commands in plugin-id order: `{plugin, name, qualified, description?, prompt, alias?}`, `[]` with the global switch off. `rpc` commands are not listed (PB.4). A command inserts text and nothing else. Names stay namespaced (`/<plugin>:<name>` always works); the bare `/<name>` is an alias owned by the first plugin by id, and the loser gets a `command` entry in `conflicts`. Built-ins win a bare name; the daemon cannot know them, so the web client derives the alias again from the list (there are no built-in slash commands yet; the registry is `lib/slash-commands.ts`). Only a `/` that starts the composer text opens the list.
- **Themes.** A theme file is `{"light": {...}, "dark": {...}}`, each declared scheme a COMPLETE set of the 115 colour tokens of index.css (those declared in both `:root` and `.dark`, minus font size, shadows, durations and content size). The list is `internal/plugins/themetokens.json` (embedded; the one daemon copy); `TestTheme_TokenListDrift` (Go) and the web test of the same name pin it and `PLUGIN_THEME_TOKENS` to index.css, so adding a token without updating them fails. Values are a strict ASCII grammar (hex, `rgb/rgba/hsl/hsla/oklch` with numbers, plain lengths, `transparent`), at most 64 bytes; install and update list missing, unknown and unsafe tokens by name. `plugin.themes` returns `{plugin, id, qualified, label, schemes: {light?, dark?}}`. The web applies a theme as inline custom properties on `<html>` only for a scheme the theme declares, after re-validating the same grammar and token set, and never as stylesheet text; the selection (with a copy of the tokens, so a reload paints it before the daemon answers) is a preference store and is dropped when the daemon reports the theme gone. Mobile (PA.5) reads the same two RPCs.
- **Found by the real-app pass:** the web must subscribe to `plugin.updated` (it was daemon-allowed but not in the client's topic list), or a disable never reached an open page.

### Implementation note, 2026-10-26 (Wave A, PA.4: the Plugins page)

Settings → Plugins (web) and the "from <plugin>" owner badges ship on the PA.2 wire with no daemon change. Decisions (Supervisor defaults, Human may veto): the page has **no control for the global switch** (only a human editing `config.yaml` turns plugins on; with the switch off the page says so and is read-only); the `link` warning (the daemon's `linkWarning`) is shown on the row, in the detail and in the enable confirmation; the Install form is local-only in effect, because every mutation answers `forbidden` for a relay session or a remote login with `plugins.allowRemoteManage` off, and the page then shows the daemon's message and goes read-only without retrying (the UI cannot know the channel in advance). Enable sends the consent fingerprint the confirmation displayed. There is no plugin process in Wave A, so the Logs block shows the recorded last error and timestamps and says "Process logs arrive with plugin processes." instead of inventing a log. Owned rows are read-only in the UI (no toggle, no edit or delete menu) and labelled by one `OwnerBadge`, which opens the plugin's detail; Settings has no list of skills, so skills carry the badge in the agent editor's skill suggestions (`skill.list`) and in the plugin detail. The plugin list is one TanStack Query entry refetched on `plugin.updated`.


### Implementation note, 2026-10-26 (Wave A, PA.5: mobile read-only)

The phone lists plugins, offers their prompt commands and applies their themes on the PA.1-PA.3 wire with no daemon change: it sends only `plugin.list`, `plugin.get`, `plugin.commands` and `plugin.themes` (a test fails if any other plugin RPC name appears in `mobile/`) and refetches on `plugin.updated`. Decisions (Lead defaults, Human may veto): entry is a **Plugins** row in the Connection sheet (mobile has no settings screen); the slash-command precedence function is a pure port of web's, not shared code; themes ship, through ONE token-to-key table (`mobile/src/plugins/themeMap.ts`) with a completeness test: 29 of the 30 colour keys of the mobile theme map onto web tokens, `unread` is documented as not themeable, `mode`, `boxShadow`, type, spacing, radius and duration are not colours; **oklch is converted** (OKLab to sRGB, gamut-clipped) instead of rejected, because the web themes are written in it; values go through the daemon's grammar first and any unsafe value refuses the whole scheme; only the selected `<plugin>:<id>` is remembered (expo-secure-store, memory on web: no new native module), never colours, and the active theme is the remembered one only while `plugin.themes` still offers it. A dev-only `devharness` hook (`plugin-install/link/enable/disable/reload/update`) drives the 390 px frames through a real `plugins.Manager`.


### Implementation note, 2026-10-27 (Wave B, PB.1: the process host)

`internal/plugins/host` runs a plugin's `process` (the first time smind executes plugin code as a child). The Manager owns one `host.Host` when `Options.Host` is set (the daemon sets it; every Wave A test leaves it nil and nothing runs). Hosts are reconciled by the same paths that change what should run: daemon start, enable, disable, reload, an applied update, remove, and a flip of `plugins.enabled` (the 2 s watcher calls `SyncHost`). With the switch off nothing is spawned, even for an enabled plugin; the supervisor also asks the switch before every restart. A host failure is contained in that plugin's supervisor: it never reaches the daemon or another plugin.

- **Spawn.** `process.command` is an argv and never goes through a shell. A first entry containing a path separator is a program inside the plugin directory (a relative path may not leave it) or an absolute path; a bare name is looked up on the plugin's own `PATH`. cwd is the plugin directory (the linked directory for a link), `data/` and `data/tmp` are created. The environment is **built, never inherited**: `PATH` is a fixed system list (`/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin`; `System32` and `SystemRoot` on Windows), `HOME` (and `USERPROFILE`) is the plugin's `data/`, the temp directory is `data/tmp`, and only `LANG`, `LC_ALL`, `LC_CTYPE`, `LC_MESSAGES`, `TZ` (plus `SystemRoot`, `windir`, `ComSpec`, `PATHEXT` on Windows) pass through from the daemon. No `SMIND_*`, no `*_TOKEN/KEY/SECRET`, no `SMIND_PLUGIN_*` (the plan names none; the plugin id is in its own manifest). Windows `.cmd` shims go through `internal/shimguard`. Honest limit unchanged: the process can still read `~/.smind/token`.
- **Framing.** LSP `Content-Length` JSON-RPC 2.0 both ways over stdin/stdout. Strict reader: header lines end in CRLF, are ASCII without control bytes and at most 1 KiB (8 lines); only `Content-Length` (1 to 10 digits, no sign, space or hex) and `Content-Type` are accepted, each once; the body is at most 1 MiB and is allocated only after the declared length passed that cap. Any violation, a body that is not a JSON-RPC 2.0 object (batches included), or an id that is not a short string or number closes the connection with a typed `FrameError` (`header_too_large`, `bad_header`, `duplicate_header`, `missing_length`, `bad_length`, `body_too_large`, `truncated`, `bad_json`, `bad_message`, `write_stalled`), writes it to the plugin's log and kills the process group; that counts as a crash. Caps: 32 host-to-plugin requests outstanding (`ErrBusy`), 16 plugin-to-host requests handled at once (the 17th is answered `-32002`), a 30 s request timeout (late answers are ignored, no leak), and a 10 s write timeout (a plugin that stops reading stdin is killed). A partial frame that never completes is bounded by memory (at most the declared body) and ended by the start or request timeout, not by a read deadline.
- **Registry.** Plugin-to-host requests go to a `host.Registry`; PB.1 wires `DenyAll`: every request is answered `-32601 method_not_found`, notifications are dropped, the connection stays open. `ErrCodeForbidden = -32001` is reserved for PB.2's capability mediation. Handlers and notifications are recovered from panics. PB.2 constraint: a Registry handler runs while the Manager may be waiting for that plugin to stop (disable, remove and reload hold the Manager's lock across a stop), so a handler must honour its `ctx` and must not take the Manager's lock.
- **Handshake.** `plugin.initialize {api, smindVersion, dataDir, settings}` (`settings` is always present, `{}` until PB.2). The plugin's answer IS the ready signal (no separate `plugin.ready` notification was added); it has `StartTimeout` 10 s. `plugin.shutdown` is a notification the host sends before SIGTERM. No hook or event vocabulary was added here; when PB.3 names hook events it should align with Claude Code's hook vocabulary (`SessionStart`, `PreToolUse`, `PostToolUse`, `Stop`) where it fits, against the ADR's `run.beforeStart` / `run.finished`.
- **Lifecycle.** starting, running, restarting, failed. Backoff 1 s doubling to a 30 s cap; a run of at least 60 s resets the count; the breaker trips at 5 crashes within 10 minutes (`host.DefaultPolicy`, no config keys yet) and the plugin becomes `failed` with its last error: it stays enabled, is not restarted, and leaves that state by `reload`, or disable and enable (a fresh supervisor). The status is `process` on `plugin.list/get` (`state`, `pid`, `restarts`, `since`, `retryAt`, `lastError`; absent for a data-only or disabled plugin and while the switch is off); a failed process also fills `lastError` (`process failed: ...`), which is what the PA.4 page and `smind plugin list/show` already render as an error. Every state change publishes `plugin.updated {id}` through a coalescing notifier on its own goroutine (a callback taking the Manager's lock can never deadlock a supervisor the Manager is waiting for).
- **Stop.** `plugin.shutdown`, SIGTERM to the process GROUP, 5 s, SIGKILL to the group, and the group is swept with SIGKILL even when the leader exited by itself, so a grandchild never outlives its plugin. Windows: a kill-on-close job object (`internal/winjob`, suspended start then assign); graceful stop is the notification plus closed stdin. Reload, disable, remove (before the files are deleted) and `StopPlugins` at daemon shutdown wait until the group is gone. A reload whose manifest fails to validate returns before touching the host, so the last good process keeps running; a reload or applied update that is good restarts it; a pending update changes nothing. Limits stated plainly: if the daemon is SIGKILLed a Unix plugin process is orphaned (no `Pdeathsig`: it is tied to the creating thread in Go); a pid reused as a group id between the leader's reap and the sweep is theoretically possible and is accepted.
- **Logs.** A per-plugin in-memory ring (1000 lines, 256 KiB, 2 KiB per line, at most 8 KiB of a line is even read, 200 stderr lines per second) of stderr and `[smind]` host messages. Lines have control characters removed (a plugin cannot drive a terminal that prints its log) and go through the existing learning redactor (`internal/learning/redact`: exact secrets, token and key shapes, home paths) BEFORE they are stored. They survive a stop and vanish on daemon restart or remove. `plugin.logs {id, limit}` and `smind plugin logs <id> [--lines=N] [--json]` read them. Decision: `plugin.logs` is guarded exactly like `plugin.get`: relay sessions may read it (the data is redacted, and a phone is the Human), it is NOT on `RunScopedMethods` so an agent connection never reaches it.
- **Not here.** The plugin-scoped session and capability table (PB.2), plugin settings and secrets (PB.2: `settings` is `{}`), hooks and `events.observe` (PB.2/PB.3), `plugin.call` (PB.4), a log view in the web page (it still says logs arrive with processes; use the CLI), config keys for the policy numbers, `link --watch`.


### Implementation note, 2026-10-27 (Wave B, PB.2: plugin-scoped session, `events.observe`, settings)

A plugin's calls into the daemon now go through a plugin-scoped session (`internal/wsapi/plugin_session.go`). `host.Registry` stays the PB.1 interface; a registry that also implements `host.SessionOpener` gets one `Session` per process run, built with the plugin's id, its declared capabilities (`Spec.Capabilities`, from the validated manifest) and a push function. The session ends with the connection, so a subscription cannot outlive its process. The daemon wires `pluginRegistry`; `DenyAll` remains for every Wave A test.

- **Capability table (AC17).** `PluginCapabilityTable` (capability group -> methods) is the whole plugin API: `tasks.read` (`task.list/get/tree`, `run.list`, `chat.list`, `workspace.list`, `space.list/get`), `mcp.read` (`mcp.list/get`), `profiles.read` (`profile.list/get`), `events.observe` (`events.subscribe/unsubscribe`, answered by the session itself). Resolution order: a method on the never-list (prefixes `plugin.`, `provider.`, `account.`, `relay.`, `push.`, `learning.`, `terminal.`, `forge.`, `automation.`, `settings.`, `permission.`, `device.`, `auth.`, `fs.`, `file.`, `usage.`, `daemon.`, plus exact names such as `run.respondPermission`, `run.setPermissionMode`, `mcp.update`, `workspace.get`, `run.logs`, `task.diff`) is `forbidden`; a method not in the table is `method_not_found`, so a wsapi method added later is denied until a human lists it; a listed method whose group was not declared is `forbidden` naming the capability. Only then does the handler run, from a private copy of the handler map, with `runScoped: true` and `local: false` so the guards inside handlers refuse it too, under a 25 s timeout, and its result is a redacted copy. Left out of `tasks.read` on purpose: `run.logs` and `task.diff` (transcripts, file contents), `workspace.get` (returns the setup scripts), `chat.get` and queue reads (prompt text). `tasks.write`, `files.read`, `ui.notify` and `hooks.beforeRun` can be declared and grant nothing until their PRs. Tests: `TestPluginSession_CapabilityTable_EveryMethodExists` (every method exists in `methodHandlers`, none on the never-list, a non-read verb needs a `Why`), `TestPluginSession_DefaultDeny_EveryOtherWsapiMethod` (a table over `methodHandlers` with spy handlers, plus names that do not exist yet).
- **`events.observe` (AC19, AC22).** Allowlisted topics only (`PluginEventTopics`): task, run, permission, workspace, space, chat lifecycle and `plugin.updated`. Left out: `chat.queueUpdated` (prompt text), `profile.*`, `account.*`, `mcpServer.*`, `provider.updated`, `task.git`, `forge.status`, `learning.*`. Each session owns a `subscriber` on the bus (256 events, drop-oldest, a latched drop count), so publishers never wait; a pump goroutine pushes `event {topic, seq, payload}` and `event.dropped {count}`. A plugin that stops reading blocks only its own pump; the host's 10 s write timeout then ends its connection. Payloads are marshalled into plain JSON before they leave the daemon (a copy), `summary` is dropped from `permission.pending` (a request body: a command, a diff preview), the task row's `SetupLog` and `SetupError` are dropped everywhere, a string under a key that contains token/secret/password/credential/cookie/authorization/apikey is replaced, and every string goes through `internal/learning/redact` (token shapes, home paths). `permission.pending` may be observed with its ids and option names; no method of the table answers or alters a permission.
- **Settings (AC16).** The manifest `settings` entries gained `enum` (with `options`), `default`, `description` and `maxLength`; a `secret` may not have a default. Values live in a new `plugin_settings` table, which is how provider credentials (`accounts.credential_data`) and MCP env/headers are already stored: a plain column of `smind.db`. No separate encryption was added because no key management exists to anchor it; the table is deleted with the plugin (also with `--keep-data`). `plugin.settings.get` is readable like `plugin.get` (relay too, never by an agent) and returns, for a secret, only `set: true`: no channel, local ones included, ever gets a secret value back (decision: there is no use case for reading it, and one for never being able to). `plugin.settings.set {id, values}` is `humanLocalOnly` (agent, relay, non-loopback without `plugins.allowRemoteManage`), validated against the declared type, all or nothing, a `null` clears; an error names the setting, never the value. A change **restarts** the process (the spec differs, so the supervisor replaces it); notifying a live plugin was rejected as a second protocol. `plugin.initialize` carries the decrypted values and defaults. `${setting.KEY}` substitution in a plugin's MCP env/headers now reads the same store (the PA.2 `SetSettings` seam stays for tests), and the plugin log redactor is rebuilt with every plugin's current secret values (exact match, 8 bytes or more: a shorter secret cannot be scrubbed by match, stated in the public docs). No CLI or page edits settings yet (a later UI item); the RPCs are the contract.
- **Hook vocabulary for PB.3 (no hooks here).** Claude Code's names fit where they mean the same thing: `PreToolUse`/`PostToolUse` have no smind equivalent (smind does not see tool calls of every provider), `Stop` is `run.finished`, `SessionStart` is nearest `run.beforeStart`. Exit-code semantics (exit 2 blocks) only apply to process-less command hooks, which smind does not have; a plugin hook answers with a JSON-RPC result. No hook may ever decide a permission (ADR-0019).
- **Not here.** Hooks and `run.beforeStart` (PB.3), `tasks.write` and `task.send` (PB.3), `plugin.call` (PB.4), a settings CLI and a settings form in the Plugins page, config keys for the queue size.

