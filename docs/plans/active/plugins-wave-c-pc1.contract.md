---
v: 1
milestone: "Plugins PC1: ui:1 view schema, panel manifest, plugin.panels/plugin.view/ui.refresh, fixtures, hello-process panel"
base: origin/develop
planRef: docs/plans/active/plugins-wave-c.md
acs:
  - id: AC-C1
    text: "Every ui:1 node type and action has a valid fixture; invalid documents (unknown type or field, depth, node, size, row, column, field caps) are refused with the exact JSON path and nothing is truncated; output is deterministic."
    covers: { tests: [TestViewSchema_Validate_CapsDepthNodesBytes, TestViewSchema_FixtureSuite_Go, TestViewSchema_EveryNodeTypeHasFixture, TestViewSchema_UnknownNodeType_IsInvalidForGo_ButClientsPlaceholder, TestViewSchema_Validate_Deterministic] }
  - id: AC-C2
    text: "Untrusted view data cannot carry active content: raw HTML and images stripped from markdown, SVG no script/style/foreignObject/handlers/external refs and under 64 KiB, tones only theme tokens, no URL field except openUrl.url which is http(s) only with no userinfo and gains a daemon-derived host."
    covers: { tests: [TestSanitize_Markdown_StripsRawHTML, TestSanitize_Markdown_ImagesBecomeAltText, TestSanitize_SVG_64KiB_NoScript, TestSanitize_Icon_NameOrSVGNotBoth, TestSanitize_Tone_OnlyTokens, TestSanitize_OpenUrl_HttpHttpsOnly_AddsHost, TestSanitize_NoRemoteImageFields] }
  - id: AC-C3
    text: "A view can only name an rpc the plugin declares in contributes.rpc and a panel the plugin declares; no plugin.action RPC exists and a plugin process cannot call plugin.* or ui methods other than ui.refresh."
    covers: { tests: [TestSanitize_Rpc_MustBeDeclared, TestSanitize_OpenPanel_MustBeDeclared, TestPluginAPITable_NoPanelMethod_ForPlugins] }
  - id: AC-C4
    text: "The manifest accepts panels (where task|sidebar|palette, view file xor rpc, ui level 1) and sidebar items, rejects malformed ones naming the field, and view files are declared paths."
    covers: { tests: [TestManifest_Panels_Valid, TestManifest_Panels_Invalid, TestManifest_PanelUiLevelAbove1_Rejected, TestManifest_Panels_FileInDeclaredPaths, TestView_StaticFileInvalid_FailsInstall] }
  - id: AC-C5
    text: "plugin.panels lists only active plugins' panels, withholds panels above the client ui level (omitted level lists nothing), vanishes on disable and with the global switch off, and is refused to agents."
    covers: { tests: [TestPanels_OnlyActive_OrderedByPluginID_GatedByLevel, TestPanels_DisabledPluginVanishes, TestPanels_SwitchOff_Empty, TestPluginPanels_List_OnlyActive_GatedByUiLevel, TestPluginPanels_DisabledPluginVanishes, TestPluginPanels_RunScopedRefused] }
  - id: AC-C6
    text: "plugin.view serves a static file or an rpc-produced view, validates every rpc result, returns typed errors (unknown_panel, ui_level_too_low, view_invalid, plugin_unavailable) and never a truncated or unvalidated document; refused to agents, allowed over the relay."
    covers: { tests: [TestView_File_And_RPC, TestView_UnknownPanel_Typed, TestView_UiLevelTooLow_Typed, TestView_InvalidDocument_TypedNoTruncation, TestView_PluginDown_Typed, TestPluginView_File_And_RPC, TestPluginView_UiLevelTooLow_Typed, TestPluginView_RunScopedRefused, TestPluginView_RelaySessionAllowed] }
  - id: AC-C7
    text: "A plugin's ui.refresh publishes plugin.event for a panel it declares, is rate limited per plugin, refuses an undeclared panel, and plugin.event is a known client topic."
    covers: { tests: [TestPluginEvent_UiRefresh_PublishesTopic, TestPluginEvent_RateLimited, TestPluginEvent_UndeclaredPanelRefused, TestPluginEvent_InKnownTopics] }
  - id: AC-C8
    text: "The hello-process example declares a task panel (rpc view) and a sidebar panel (static file) that validate and render through plugin.view."
    covers: { tests: [TestExamples_HelloProcess_PanelValid] }
tests:
  - { name: TestViewSchema_Validate_CapsDepthNodesBytes, run: "go test ./internal/plugins/uiview -run '^TestViewSchema_Validate_CapsDepthNodesBytes$' -count=1", timeoutSec: 300 }
  - { name: TestViewSchema_FixtureSuite_Go, run: "go test ./internal/plugins/uiview -run '^TestViewSchema_FixtureSuite_Go$' -count=1", timeoutSec: 300 }
  - { name: TestViewSchema_EveryNodeTypeHasFixture, run: "go test ./internal/plugins/uiview -run '^TestViewSchema_EveryNodeTypeHasFixture$' -count=1", timeoutSec: 300 }
  - { name: TestViewSchema_UnknownNodeType_IsInvalidForGo_ButClientsPlaceholder, run: "go test ./internal/plugins/uiview -run '^TestViewSchema_UnknownNodeType_IsInvalidForGo_ButClientsPlaceholder$' -count=1", timeoutSec: 300 }
  - { name: TestViewSchema_Validate_Deterministic, run: "go test ./internal/plugins/uiview -run '^TestViewSchema_Validate_Deterministic$' -count=1", timeoutSec: 300 }
  - { name: TestSanitize_Markdown_StripsRawHTML, run: "go test ./internal/plugins/uiview -run '^TestSanitize_Markdown_StripsRawHTML$' -count=1", timeoutSec: 300 }
  - { name: TestSanitize_Markdown_ImagesBecomeAltText, run: "go test ./internal/plugins/uiview -run '^TestSanitize_Markdown_ImagesBecomeAltText$' -count=1", timeoutSec: 300 }
  - { name: TestSanitize_SVG_64KiB_NoScript, run: "go test ./internal/plugins/uiview -run '^TestSanitize_SVG_64KiB_NoScript$' -count=1", timeoutSec: 300 }
  - { name: TestSanitize_Icon_NameOrSVGNotBoth, run: "go test ./internal/plugins/uiview -run '^TestSanitize_Icon_NameOrSVGNotBoth$' -count=1", timeoutSec: 300 }
  - { name: TestSanitize_Tone_OnlyTokens, run: "go test ./internal/plugins/uiview -run '^TestSanitize_Tone_OnlyTokens$' -count=1", timeoutSec: 300 }
  - { name: TestSanitize_OpenUrl_HttpHttpsOnly_AddsHost, run: "go test ./internal/plugins/uiview -run '^TestSanitize_OpenUrl_HttpHttpsOnly_AddsHost$' -count=1", timeoutSec: 300 }
  - { name: TestSanitize_Rpc_MustBeDeclared, run: "go test ./internal/plugins/uiview -run '^TestSanitize_Rpc_MustBeDeclared$' -count=1", timeoutSec: 300 }
  - { name: TestSanitize_OpenPanel_MustBeDeclared, run: "go test ./internal/plugins/uiview -run '^TestSanitize_OpenPanel_MustBeDeclared$' -count=1", timeoutSec: 300 }
  - { name: TestSanitize_NoRemoteImageFields, run: "go test ./internal/plugins/uiview -run '^TestSanitize_NoRemoteImageFields$' -count=1", timeoutSec: 300 }
  - { name: TestManifest_Panels_Valid, run: "go test ./internal/plugins -run '^TestManifest_Panels_Valid$' -count=1", timeoutSec: 300 }
  - { name: TestManifest_Panels_Invalid, run: "go test ./internal/plugins -run '^TestManifest_Panels_Invalid$' -count=1", timeoutSec: 300 }
  - { name: TestManifest_PanelUiLevelAbove1_Rejected, run: "go test ./internal/plugins -run '^TestManifest_PanelUiLevelAbove1_Rejected$' -count=1", timeoutSec: 300 }
  - { name: TestManifest_Panels_FileInDeclaredPaths, run: "go test ./internal/plugins -run '^TestManifest_Panels_FileInDeclaredPaths$' -count=1", timeoutSec: 300 }
  - { name: TestPanels_OnlyActive_OrderedByPluginID_GatedByLevel, run: "go test ./internal/plugins -run '^TestPanels_OnlyActive_OrderedByPluginID_GatedByLevel$' -count=1", timeoutSec: 300 }
  - { name: TestPanels_DisabledPluginVanishes, run: "go test ./internal/plugins -run '^TestPanels_DisabledPluginVanishes$' -count=1", timeoutSec: 300 }
  - { name: TestPanels_SwitchOff_Empty, run: "go test ./internal/plugins -run '^TestPanels_SwitchOff_Empty$' -count=1", timeoutSec: 300 }
  - { name: TestView_File_And_RPC, run: "go test ./internal/plugins -run '^TestView_File_And_RPC$' -count=1", timeoutSec: 300 }
  - { name: TestView_UnknownPanel_Typed, run: "go test ./internal/plugins -run '^TestView_UnknownPanel_Typed$' -count=1", timeoutSec: 300 }
  - { name: TestView_UiLevelTooLow_Typed, run: "go test ./internal/plugins -run '^TestView_UiLevelTooLow_Typed$' -count=1", timeoutSec: 300 }
  - { name: TestView_InvalidDocument_TypedNoTruncation, run: "go test ./internal/plugins -run '^TestView_InvalidDocument_TypedNoTruncation$' -count=1", timeoutSec: 300 }
  - { name: TestView_PluginDown_Typed, run: "go test ./internal/plugins -run '^TestView_PluginDown_Typed$' -count=1", timeoutSec: 300 }
  - { name: TestView_StaticFileInvalid_FailsInstall, run: "go test ./internal/plugins -run '^TestView_StaticFileInvalid_FailsInstall$' -count=1", timeoutSec: 300 }
  - { name: TestPluginPanels_List_OnlyActive_GatedByUiLevel, run: "go test ./internal/wsapi -run '^TestPluginPanels_List_OnlyActive_GatedByUiLevel$' -count=1", timeoutSec: 300 }
  - { name: TestPluginPanels_DisabledPluginVanishes, run: "go test ./internal/wsapi -run '^TestPluginPanels_DisabledPluginVanishes$' -count=1", timeoutSec: 300 }
  - { name: TestPluginPanels_RunScopedRefused, run: "go test ./internal/wsapi -run '^TestPluginPanels_RunScopedRefused$' -count=1", timeoutSec: 300 }
  - { name: TestPluginView_File_And_RPC, run: "go test ./internal/wsapi -run '^TestPluginView_File_And_RPC$' -count=1", timeoutSec: 300 }
  - { name: TestPluginView_UiLevelTooLow_Typed, run: "go test ./internal/wsapi -run '^TestPluginView_UiLevelTooLow_Typed$' -count=1", timeoutSec: 300 }
  - { name: TestPluginView_RunScopedRefused, run: "go test ./internal/wsapi -run '^TestPluginView_RunScopedRefused$' -count=1", timeoutSec: 300 }
  - { name: TestPluginView_RelaySessionAllowed, run: "go test ./internal/wsapi -run '^TestPluginView_RelaySessionAllowed$' -count=1", timeoutSec: 300 }
  - { name: TestPluginEvent_UiRefresh_PublishesTopic, run: "go test ./internal/wsapi -run '^TestPluginEvent_UiRefresh_PublishesTopic$' -count=1", timeoutSec: 300 }
  - { name: TestPluginEvent_RateLimited, run: "go test ./internal/wsapi -run '^TestPluginEvent_RateLimited$' -count=1", timeoutSec: 300 }
  - { name: TestPluginEvent_UndeclaredPanelRefused, run: "go test ./internal/wsapi -run '^TestPluginEvent_UndeclaredPanelRefused$' -count=1", timeoutSec: 300 }
  - { name: TestPluginEvent_InKnownTopics, run: "go test ./internal/wsapi -run '^TestPluginEvent_InKnownTopics$' -count=1", timeoutSec: 300 }
  - { name: TestPluginAPITable_NoPanelMethod_ForPlugins, run: "go test ./internal/wsapi -run '^TestPluginAPITable_NoPanelMethod_ForPlugins$' -count=1", timeoutSec: 300 }
  - { name: TestExamples_HelloProcess_PanelValid, run: "go test ./internal/wsapi -run '^TestExamples_HelloProcess_PanelValid$' -count=1", timeoutSec: 300 }
mutations:
  - { id: M1, file: internal/plugins/uiview/validate.go, change: "drop the depth > MaxDepth check", expectFail: [TestViewSchema_Validate_CapsDepthNodesBytes] }
  - { id: M2, file: internal/plugins/uiview/sanitize.go, change: "accept any URL scheme in openUrl (remove the http/https check)", expectFail: [TestSanitize_OpenUrl_HttpHttpsOnly_AddsHost] }
  - { id: M3, file: internal/plugins/uiview/sanitize.go, change: "add script to the allowed SVG elements", expectFail: [TestSanitize_SVG_64KiB_NoScript] }
  - { id: M4, file: internal/plugins/uiview/validate.go, change: "skip the env.RPCs declared check for rpc actions", expectFail: [TestSanitize_Rpc_MustBeDeclared] }
  - { id: M5, file: internal/plugins/uiview/sanitize.go, change: "SanitizeMarkdown returns its input unchanged", expectFail: [TestSanitize_Markdown_StripsRawHTML] }
  - { id: M6, file: internal/plugins/panels.go, change: "Panels ignores the level argument: both the level <= 0 return and the ui > level skip are removed", expectFail: [TestPanels_OnlyActive_OrderedByPluginID_GatedByLevel, TestPluginPanels_List_OnlyActive_GatedByUiLevel] }
  - { id: M7, file: internal/plugins/panels.go, change: "View skips uiview.Validate for rpc-produced documents", expectFail: [TestView_InvalidDocument_TypedNoTruncation] }
  - { id: M8, file: internal/wsapi/plugin_view.go, change: "drop the runScoped refusal in plugin.view", expectFail: [TestPluginView_RunScopedRefused] }
  - { id: M9, file: internal/wsapi/plugin_refresh.go, change: "ui.refresh skips the declared-panel check for a named panelId", expectFail: [TestPluginEvent_UndeclaredPanelRefused] }
commands:
  - { id: C1, run: "task lint", timeoutSec: 900 }
  - { id: C2, run: "task test", timeoutSec: 1500 }
limits: { budgetUsd: 2, maxMinutes: 45 }
writeScope:
  paths: []
---

# PC1 validation contract

Daemon-only: no UI, no model call, isolated temp dirs. Key assertions: untrusted view data cannot carry HTML, script, remote images or non-http(s) URLs; a view can only name rpcs and panels its plugin declares; agents and plugin processes cannot reach plugin.view or plugin.panels; nothing is truncated.
Spec: `docs/plans/active/plugins-wave-c.md` (PC1).
