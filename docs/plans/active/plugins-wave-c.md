# Plugins Wave C: declarative UI (AC24-AC30)

Status: **spec, 2026-10-31.** Parent plan: [plugins.md](plugins.md) (item PC); decision: [ADR-0042](../../decisions/0042-plugin-system.md) section 5 (accepted).
Waves A and B are merged (host, contributions, process, hooks, `task.send`, `plugin.call`, `plugin-examples/hello-process`).

Size: **L (8)**: scope 2 (daemon, web, mobile), PRs 2 (three, ordered by waiting for merges), risk 1 (untrusted data rendered in the app; no client code), decisions 1
(defaults below), verification 2 (light/dark frames at 1280 and 390, relay smoke). One Lead; peer-glm implements from step briefs, the Lead reviews every line.

Rule for every PR: non-stacked, branched from `origin/develop`; the next PR starts only after the previous one is on `develop`.

## Decisions (conservative)

All are "Supervisor default 2026-10-31, Human may veto".

- **D1 ui level handshake.** The wsapi has no connect-time hello, so a client states its level per request: `plugin.panels {ui: 1}` and `plugin.view {..., ui: 1}`.
  Omitted or 0 means "no panels" (a client that never heard of Wave C sees nothing). Stateless, so it is identical on `/ws` and over the relay.
  The daemon withholds a panel whose `ui` is above the level and `plugin.view` refuses it with `ui_level_too_low`.
- **D2 manifest panel shape.** `{id, title, icon?, where: "task"|"sidebar"|"palette", ui?: 1, view: {file}|{rpc}}`. `view.file` is a plugin-relative `.json`
  file read through the existing confined-path helper; `view.rpc` names a declared `contributes.rpc` entry and is called through `Manager.Call` with
  `{panelId, context}`. `contributes.sidebar` items stay `{id,title,icon?,opens}` and `opens` must name a declared panel. `where: "palette"` panels
  are listed in Ctrl-K only (an entry opens the panel as a tab on the current task, or the sidebar page when there is no task).
- **D3 caps.** view document <= 256 KiB (equals `MaxCallResult`), depth <= 12, nodes <= 1000, strings <= 8 KiB, list/table rows <= 500, table columns <= 12,
  form fields <= 24, SVG <= 64 KiB. Over a cap is a typed `view_invalid` error that names the JSON path; nothing is truncated.
- **D4 `plugin.event`.** A plugin process asks for a refresh with a native session method `ui.refresh {panelId?}` (no capability needed beyond declaring the panel;
  rate limited to 4/s per plugin). The host publishes `plugin.event {pluginId, kind: "refresh", panelId?}` on the bus. `plugin.event` joins `knownTopics`.
  `plugin.updated` already exists. The payload carries no view data: clients refetch.
- **D5 actions are client executed, daemon validated.** `rpc` -> the client calls `plugin.call` on the same plugin; `prompt` inserts text into the composer;
  `command` runs a palette command by qualified name; `openPanel` opens a declared panel of the same plugin; `openUrl` opens the OS browser behind a confirm.
  The daemon's sanitiser checks every action when the view is built (rpc must be declared in `contributes.rpc`; `openPanel` must name a declared panel;
  `openUrl` is `http`/`https` only, no credentials, and the sanitiser adds `host` for the confirm dialog), so a client never trusts an unchecked name.
  There is no `plugin.action` RPC: egress is only `plugin.call`.
- **D6 no image node in `ui: 1`.** The ADR vocabulary has no `image`; markdown images and `![]()` are rendered as their alt text (never fetched). Icons are a
  Lucide name (pattern `^[A-Z][A-Za-z0-9]{0,39}$`, resolved by the client registry, unknown name = generic icon) or `{svg}` under the Paseo rules.
- **D7 fixtures.** One shared directory `internal/plugins/uiview/testdata/fixtures/` (`<name>.view.json` + `<name>.expect.json`, where expect is `valid` with the
  normalised document or `invalid` with the error path). Go consumes it in `TestViewSchema_FixtureSuite_Go`; web and mobile read the same files by relative
  path in PC2/PC3 (the way `plugin-theme-tokens.test.ts` reads `themetokens.json`).

## PR split

| PR | Content | Needs |
|---|---|---|
| plan | this file | none |
| **PC1** | Go view schema + validation + sanitiser (`internal/plugins/uiview`), manifest `panels`/`sidebar` contribution, daemon RPCs `plugin.panels`, `plugin.view`, `plugin.event`/`ui.refresh`, level gating, shared fixtures, hello-process panel, ADR/plan notes | plan merged |
| **PC2** | Web renderer, `plugin` tab kind, sidebar items, palette entries, react-query keys, placeholder, frames; `public-docs/plugins.md` + `plugins-authoring.md` panel sections | PC1 merged |
| **PC3** | Mobile renderer (react-native-web at 390 px), lint rule banning `eval`/`new Function`, frame-cap refusal over the relay, E2EE smoke | PC1 merged (PC2 preferred) |

## Acceptance criteria and named tests

### PC1 (AC24, AC26 daemon half, AC27 validation half, AC28 push half, AC30 refusal half)

- AC24: `TestViewSchema_Validate_CapsDepthNodesBytes` (each cap, one table row each, at the limit passes and one over fails with the JSON path),
  `TestViewSchema_FixtureSuite_Go` (every node type and every action has a valid fixture; invalid fixtures name the expected path).
- AC24 node coverage: `TestViewSchema_EveryNodeTypeHasFixture`, `TestViewSchema_UnknownNodeType_IsInvalidForGo_ButClientsPlaceholder`
  (the daemon marks an unknown `type` invalid; the client placeholder rule is PC2/PC3).
- AC27 sanitiser: `TestSanitize_Markdown_StripsRawHTML`, `TestSanitize_Markdown_ImagesBecomeAltText`, `TestSanitize_SVG_64KiB_NoScript` (script, style, foreignObject,
  `on*` handlers, external `href`, DOCTYPE/entities rejected), `TestSanitize_Icon_NameOrSVGNotBoth`, `TestSanitize_Tone_OnlyTokens` (a raw color is invalid),
  `TestSanitize_OpenUrl_HttpHttpsOnly_AddsHost` (rejects `javascript:`, `data:`, `file:`, userinfo, control characters),
  `TestSanitize_Rpc_MustBeDeclared`, `TestSanitize_OpenPanel_MustBeDeclared`, `TestSanitize_NoRemoteImageFields` (no field may carry a URL except `openUrl.url`).
- Manifest: `TestManifest_Panels_Valid`, `TestManifest_Panels_Invalid` (bad where, duplicate id, view both/neither file and rpc, rpc undeclared, path escape,
  `sidebar.opens` unknown), `TestManifest_PanelUiLevelAbove1_Rejected`.
- Daemon: `TestPluginPanels_List_OnlyActive_GatedByUiLevel` (== `TestPanelWithheld_WhenClientUiLevelTooLow`), `TestPluginPanels_DisabledPluginVanishes`,
  `TestPluginView_File_And_RPC`, `TestPluginView_UiLevelTooLow_Typed`, `TestPluginView_InvalidDocument_TypedNoTruncation`, `TestPluginView_PluginDown_Typed`,
  `TestPluginView_RunScopedRefused`, `TestPluginView_RelaySessionAllowed`, `TestPluginEvent_UiRefresh_PublishesTopic`, `TestPluginEvent_RateLimited`,
  `TestPluginEvent_UndeclaredPanelRefused`, `TestPluginEvent_InKnownTopics`, `TestPluginAPITable_NoPanelMethod_ForPlugins` (plugins never call `plugin.*`; only native `ui.refresh`).
- Example: `TestExamples_HelloProcess_PanelValid` (the hello-process panel validates, the view rpc answers and passes the sanitiser).

### PC2 (AC25, AC26, AC27 client half, AC28, AC29 web)

Names from the parent plan: `plugin-view.test.tsx` (every shared fixture), `unknown-node-placeholder`, `markdown-strips-raw-html`, `remote-image-not-fetched`,
`openUrl-confirm-shows-host`, `prompt-action-inserts-not-sends`, `TestPanelClosesOnDisable_TabRestorable`, `TestSidebarAndPalette_ContributionsRegister`,
`TestPlugin_Event_InvalidatesQuery_NoPerPluginStore`; frames: panel open/refresh/disable, light/dark, 1280 and 390.

### PC3 (AC26 mobile, AC29 mobile, AC30)

`pluginView.test.tsx` over the same fixtures, `pluginSafety.test.ts` extended (no `eval`/`new Function` in `mobile/src`, `mobile/app` and web renderer dirs),
an ESLint rule test, `frameCapRefused` (a view over the cap is a typed error over the relay harness), E2EE-v2 smoke through the relay (ciphertext only), 390 px frames.

## Out of scope

Web/desktop JS panels (Q5), an `image` node, plugin-served assets, workspace-level panels (`where: "workspace"`), form submit persistence (a form action calls `rpc`
with the field values; the plugin stores them), per-plugin client caches.

## Progress

- [ ] plan PR
- [ ] PC1 (validation contract `plugins-wave-c-pc1.contract.md`, added with PC1)
- [ ] PC2
- [ ] PC3

## Validation

(filled per PR)
