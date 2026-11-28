---
v: 1
milestone: "ADR-0064 Wave 0: Astryx theme, build wiring and four primitives behind the wrappers"
base: origin/develop
planRef: docs/plans/active/astryx-wave0.md
acs:
  - id: AC1
    text: "Astryx core, the theme package and stylex are pinned to exact versions in web/packages/ui/package.json."
    covers: { tests: [astryx_deps_are_exactly_pinned], frames: [] }
  - id: AC2
    text: "The CSS entry declares one layer order for Astryx and Tailwind before any import that emits rules."
    covers: { tests: [astryx_layer_order_is_declared_first, astryx_layer_order_is_declared_in_index_html_before_any_stylesheet], frames: [] }
  - id: AC3
    text: "Typecheck, the full unit suite and the demo build check pass."
    covers: { tests: [], frames: [], commands: [C1, C2, C3] }
  - id: AC4
    text: "The sMind theme has light and dark values for every token and its stylesheet is built, not compiled at runtime."
    covers: { tests: [smind_theme_covers_light_and_dark_for_every_token, smind_theme_stylesheet_is_built_not_compiled_at_runtime], frames: [] }
  - id: AC5
    text: "The pre-paint script sets the attributes the Astryx theme needs."
    covers: { tests: [prepaint_script_sets_the_astryx_theme_attributes], frames: [] }
  - id: AC6
    text: "Button, Input, Switch and Dialog keep their props and exports; no call site changed."
    covers: { tests: [button_keeps_variants_sizes_and_data_attributes, input_forwards_props_and_aria_invalid, switch_toggles_and_keeps_data_state, no_call_site_changed_for_the_four_wrappers], frames: [] }
  - id: AC7
    text: "asChild, Escape and overlay close work; a pointer open of a dialog shows no focus ring."
    covers: { tests: [button_as_child_renders_the_child_element, dialog_pointer_open_shows_no_focus_ring, dialog_escape_and_overlay_close_it], frames: [] }
tests:
  - { name: astryx_deps_are_exactly_pinned, run: "cd web/packages/ui && bunx vitest run src/lib/astryx-build.test.ts -t astryx_deps_are_exactly_pinned", timeoutSec: 900 }
  - { name: astryx_layer_order_is_declared_first, run: "cd web/packages/ui && bunx vitest run src/lib/astryx-build.test.ts -t astryx_layer_order_is_declared_first", timeoutSec: 900 }
  - { name: astryx_layer_order_is_declared_in_index_html_before_any_stylesheet, run: "cd web/packages/ui && bunx vitest run src/lib/astryx-build.test.ts -t astryx_layer_order_is_declared_in_index_html_before_any_stylesheet", timeoutSec: 900 }
  - { name: smind_theme_covers_light_and_dark_for_every_token, run: "cd web/packages/ui && bunx vitest run src/theme/smind-theme.test.ts -t smind_theme_covers_light_and_dark_for_every_token", timeoutSec: 900 }
  - { name: smind_theme_stylesheet_is_built_not_compiled_at_runtime, run: "cd web/packages/ui && bunx vitest run src/theme/smind-theme.test.ts -t smind_theme_stylesheet_is_built_not_compiled_at_runtime", timeoutSec: 900 }
  - { name: prepaint_script_sets_the_astryx_theme_attributes, run: "cd web/packages/ui && bunx vitest run src/theme/smind-theme.test.ts -t prepaint_script_sets_the_astryx_theme_attributes", timeoutSec: 900 }
  - { name: button_keeps_variants_sizes_and_data_attributes, run: "cd web/packages/ui && bunx vitest run src/components/ui/button.test.tsx -t button_keeps_variants_sizes_and_data_attributes", timeoutSec: 900 }
  - { name: button_as_child_renders_the_child_element, run: "cd web/packages/ui && bunx vitest run src/components/ui/button.test.tsx -t button_as_child_renders_the_child_element", timeoutSec: 900 }
  - { name: input_forwards_props_and_aria_invalid, run: "cd web/packages/ui && bunx vitest run src/components/ui/input.test.tsx -t input_forwards_props_and_aria_invalid", timeoutSec: 900 }
  - { name: switch_toggles_and_keeps_data_state, run: "cd web/packages/ui && bunx vitest run src/components/ui/switch.test.tsx -t switch_toggles_and_keeps_data_state", timeoutSec: 900 }
  - { name: dialog_pointer_open_shows_no_focus_ring, run: "cd web/packages/ui && bunx vitest run src/components/ui/dialog.test.tsx -t dialog_pointer_open_shows_no_focus_ring", timeoutSec: 900 }
  - { name: dialog_escape_and_overlay_close_it, run: "cd web/packages/ui && bunx vitest run src/components/ui/dialog.test.tsx -t dialog_escape_and_overlay_close_it", timeoutSec: 900 }
  - { name: no_call_site_changed_for_the_four_wrappers, run: "cd web/packages/ui && bunx vitest run src/lib/astryx-wave0-scope.test.ts -t no_call_site_changed_for_the_four_wrappers", timeoutSec: 900 }
mutations:
  - { id: M1, file: web/packages/ui/package.json, change: "the stylex version gets a ^ prefix", expectFail: [astryx_deps_are_exactly_pinned] }
  - { id: M2, file: web/packages/ui/src/index.css, change: "the layer order line moves below the first @import that emits rules", expectFail: [astryx_layer_order_is_declared_first] }
  - { id: M3, file: web/packages/ui/src/components/ui/dialog.tsx, change: "the dialog focuses its content with a visible ring on pointer open", expectFail: [dialog_pointer_open_shows_no_focus_ring] }
  - { id: M4, file: web/packages/ui/src/components/ui/switch.tsx, change: "data-state is no longer set on the switch root", expectFail: [switch_toggles_and_keeps_data_state] }
  - { id: M5, file: web/packages/ui/index.html, change: "the inline @layer order style is removed from <head>", expectFail: [astryx_layer_order_is_declared_in_index_html_before_any_stylesheet] }
frames: []
commands:
  - { id: C1, run: "cd web/packages/ui && bunx tsc -b", timeoutSec: 900 }
  - { id: C2, run: "cd web/packages/ui && bun run test", timeoutSec: 1800 }
  - { id: C3, run: "cd web/packages/ui && bun run build:demo && rm -rf dist-demo", timeoutSec: 900 }
  - { id: C4, run: "go test ./internal/hygiene/ -count=1", timeoutSec: 900 }
  - { id: C5, run: "cd web/packages/ui && node --test scripts/check-demo-build.test.mjs", timeoutSec: 900 }
limits: { budgetUsd: 3, maxMinutes: 60 }
---

# Astryx Wave 0 validation contract

Spec: `docs/plans/active/astryx-wave0.md`. Web only. Frames (REAL-WINDOW, light and dark, default and minimum size) are captured by the Lead, not the validator; the validator checks the tests, mutations and commands.
