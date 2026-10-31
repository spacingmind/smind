---
v: 1
milestone: "Plugins PC2: web renderer for the ui:1 declarative plugin UI, plugin tab, sidebar, palette, actions"
base: origin/develop
planRef: docs/plans/active/plugins-wave-c.md
acs:
  - id: AC-W1
    text: "Every shared ui:1 fixture renders in the web renderer without throwing; every node type has golden text; an unknown or malformed node degrades to an 'Unsupported in this app version' placeholder and never crashes the panel."
    covers: { tests: [renders_every_shared_fixture_without_throwing, unknown-node-placeholder] }
  - id: AC-W2
    text: "Untrusted view data cannot become active content in the web app: no raw HTML, markdown images render only as alt text and are never fetched, svg icons are rebuilt from an allowlist, and no HTML sink (dangerouslySetInnerHTML, innerHTML, eval, new Function) exists in the renderer directories."
    covers: { tests: [markdown-strips-raw-html, remote-image-not-fetched, svg_icon_is_rebuilt_from_an_allowlist, no_raw_html_sinks_in_the_plugin_renderer_directories] }
  - id: AC-W3
    text: "Actions are executed by the client with the documented limits: a prompt action only inserts text into the composer and never sends; openUrl asks first and the dialog shows the host; forms pass values."
    covers: { tests: [form_submit_passes_values_and_is_blocked_until_required_fields_are_filled, prompt-action-inserts-not-sends, openUrl-confirm-shows-host] }
  - id: AC-W4
    text: "A plugin panel opens as a restorable tab, and its tab closes by itself when the plugin is disabled (not while the panel list is loading or failed); sidebar items and Ctrl-K entries register."
    covers: { tests: [TestPanelClosesOnDisable_TabRestorable, TestSidebarAndPalette_ContributionsRegister] }
  - id: AC-W5
    text: "The client states its ui level with plugin.panels and plugin.view, a plugin.event refreshes only that plugin's cached views (react-query, no per-plugin store), and an older daemon reads as no panels."
    covers: { tests: [TestPlugin_Event_InvalidatesQuery_NoPerPluginStore, TestPanelsQuery_ReportsUiLevel] }
tests:
  - { name: renders_every_shared_fixture_without_throwing, run: "cd web/packages/ui && NODE_OPTIONS=--no-experimental-webstorage bunx vitest run src/test/plugin-view.test.tsx -t 'renders every shared fixture without throwing'", timeoutSec: 600 }
  - { name: unknown-node-placeholder, run: "cd web/packages/ui && NODE_OPTIONS=--no-experimental-webstorage bunx vitest run src/test/plugin-view.test.tsx -t 'unknown-node-placeholder'", timeoutSec: 600 }
  - { name: markdown-strips-raw-html, run: "cd web/packages/ui && NODE_OPTIONS=--no-experimental-webstorage bunx vitest run src/test/plugin-view.test.tsx -t 'markdown-strips-raw-html'", timeoutSec: 600 }
  - { name: remote-image-not-fetched, run: "cd web/packages/ui && NODE_OPTIONS=--no-experimental-webstorage bunx vitest run src/test/plugin-view.test.tsx -t 'remote-image-not-fetched'", timeoutSec: 600 }
  - { name: svg_icon_is_rebuilt_from_an_allowlist, run: "cd web/packages/ui && NODE_OPTIONS=--no-experimental-webstorage bunx vitest run src/test/plugin-view.test.tsx -t 'svg icon is rebuilt from an allowlist'", timeoutSec: 600 }
  - { name: form_submit_passes_values_and_is_blocked_until_required_fields_are_filled, run: "cd web/packages/ui && NODE_OPTIONS=--no-experimental-webstorage bunx vitest run src/test/plugin-view.test.tsx -t 'form submit passes values and is blocked until required fields are filled'", timeoutSec: 600 }
  - { name: no_raw_html_sinks_in_the_plugin_renderer_directories, run: "cd web/packages/ui && NODE_OPTIONS=--no-experimental-webstorage bunx vitest run src/test/plugin-view-safety.test.ts -t 'no raw html sinks in the plugin renderer directories'", timeoutSec: 600 }
  - { name: prompt-action-inserts-not-sends, run: "cd web/packages/ui && NODE_OPTIONS=--no-experimental-webstorage bunx vitest run src/components/plugin-panel/plugin-actions.test.tsx -t 'prompt-action-inserts-not-sends'", timeoutSec: 600 }
  - { name: openUrl-confirm-shows-host, run: "cd web/packages/ui && NODE_OPTIONS=--no-experimental-webstorage bunx vitest run src/components/plugin-panel/plugin-actions.test.tsx -t 'openUrl-confirm-shows-host'", timeoutSec: 600 }
  - { name: TestPanelClosesOnDisable_TabRestorable, run: "cd web/packages/ui && NODE_OPTIONS=--no-experimental-webstorage bunx vitest run src/hooks/use-plugin-tab-reaper.test.tsx -t 'TestPanelClosesOnDisable_TabRestorable'", timeoutSec: 600 }
  - { name: TestSidebarAndPalette_ContributionsRegister, run: "cd web/packages/ui && NODE_OPTIONS=--no-experimental-webstorage bunx vitest run src/App.test.tsx -t 'TestSidebarAndPalette_ContributionsRegister'", timeoutSec: 600 }
  - { name: TestPlugin_Event_InvalidatesQuery_NoPerPluginStore, run: "cd web/packages/ui && NODE_OPTIONS=--no-experimental-webstorage bunx vitest run src/lib/query/event-bridge.test.ts -t 'TestPlugin_Event_InvalidatesQuery_NoPerPluginStore'", timeoutSec: 600 }
  - { name: TestPanelsQuery_ReportsUiLevel, run: "cd web/packages/ui && NODE_OPTIONS=--no-experimental-webstorage bunx vitest run src/hooks/use-plugin-panels.test.tsx -t 'TestPanelsQuery_ReportsUiLevel'", timeoutSec: 600 }
  - { name: prototype_member_names_fall_back_to_the_generic_icon, run: "cd web/packages/ui && NODE_OPTIONS=--no-experimental-webstorage bunx vitest run src/lib/plugin-icon.test.ts -t 'prototype member names fall back to the generic icon'", timeoutSec: 600 }
mutations:
  - { id: M1, file: web/packages/ui/src/components/plugin-view/plugin-view.tsx, change: "an unknown node type renders null instead of the UnsupportedNode placeholder", expectFail: [unknown-node-placeholder] }
  - { id: M2, file: web/packages/ui/src/hooks/use-plugin-action-runner.ts, change: "the prompt action also calls client.call('run.start', {})", expectFail: [prompt-action-inserts-not-sends] }
  - { id: M3, file: web/packages/ui/src/hooks/use-plugin-action-runner.ts, change: "the openUrl action calls openExternalUrl immediately instead of storing pendingUrl", expectFail: [openUrl-confirm-shows-host] }
  - { id: M4, file: web/packages/ui/src/hooks/use-plugin-tab-reaper.ts, change: "the effect no longer calls closeTab", expectFail: [TestPanelClosesOnDisable_TabRestorable] }
  - { id: M5, file: web/packages/ui/src/hooks/use-plugin-panels.ts, change: "plugin.panels is called with {ui: 0}", expectFail: [TestPanelsQuery_ReportsUiLevel] }
  - { id: M6, file: web/packages/ui/src/components/plugin-view/plugin-view.tsx, change: "the markdown img override is removed so images render as img elements", expectFail: [remote-image-not-fetched] }
  - { id: M7, file: web/packages/ui/src/components/plugin-view/safe-svg.tsx, change: "ALLOWED_ELEMENTS gains script and ALLOWED_ATTRS gains onload", expectFail: [svg_icon_is_rebuilt_from_an_allowlist] }
  - { id: M8, file: web/packages/ui/src/components/plugin-view/plugin-view.tsx, change: "the code node renders its text through dangerouslySetInnerHTML", expectFail: [no_raw_html_sinks_in_the_plugin_renderer_directories] }
  - { id: M9, file: web/packages/ui/src/lib/query/event-bridge.ts, change: "the plugin.event case invalidates nothing", expectFail: [TestPlugin_Event_InvalidatesQuery_NoPerPluginStore] }
  - { id: M10, file: web/packages/ui/src/App.tsx, change: "ShellCommands registers an empty list for plugins:panels (pluginPanelCommands is [])", expectFail: [TestSidebarAndPalette_ContributionsRegister] }
commands:
  - { id: C1, run: "task lint", timeoutSec: 900 }
  - { id: C2, run: "task test", timeoutSec: 1500 }
limits: { budgetUsd: 2, maxMinutes: 45 }
writeScope:
  paths: []
---

# PC2 validation contract

The web renderer draws untrusted plugin data. The validator should judge the diff against AC-W1..W5, run each named test, apply each mutation (one at a time, restore after) and confirm the named test fails, and look for any path by which plugin data reaches the DOM as HTML, a remote fetch, or an action that sends a message without the user pressing send. Frames (light/dark, 1280/390) are in /tmp/pc2-frames and are reviewed by the Lead.
