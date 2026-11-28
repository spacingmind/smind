# Astryx Wave 0 spike

Size: L (7) — scope 2, PRs 1, risk 1, decisions 1, verification 2 (real window)
Decision record: [ADR-0064](../../decisions/0064-migrate-web-and-desktop-ui-to-astryx.md). Contract: `astryx-wave0.contract.md`.

Wave 0 is the spike before any migration wave: the sMind theme, the build wiring and four primitives (Button, Input, Switch, Dialog) on Astryx behind the existing `components/ui` wrappers. It ends with measurements and a written go or no-go (section Validation). One PR carries the ADR and this code.

## Acceptance criteria

- **AC1 Pinned deps.** `@astryxdesign/core`, the theme package(s) and `@stylexjs/stylex` are in `web/packages/ui/package.json` at exact versions (no `^`, no `~`). The lockfile is updated.
- **AC2 Build wiring.** One documented path (prebuilt stylesheets or the Astryx Vite plugin, chosen as in the Astryx Vite + Tailwind example, reason written in the plan). Tailwind 4 stays. The CSS entry declares one layer order that places Astryx layers and Tailwind layers, with a comment.
- **AC3 All build targets work.** `vite` dev, the daemon-embedded build, `build:desktop`, `build:demo` (its size check passes), `vitest`, and `tsc -b` all pass.
- **AC4 Theme.** A sMind theme built with `defineTheme` (extends the neutral theme), light and dark values for every token in `docs/design.md` that Astryx has a slot for, built at build time into a stylesheet. No runtime theme compile.
- **AC5 No flash.** The pre-paint script in `index.html` sets everything the Astryx theme needs (mode and theme attributes) before first paint, in sync with `src/lib/theme.ts`; light, dark and system all work.
- **AC6 Four primitives.** `button.tsx`, `input.tsx`, `switch.tsx`, `dialog.tsx` render Astryx components. Their props and exports are unchanged; no call site changes.
- **AC7 Behaviours kept.** No focus ring when a dialog opens by pointer; the ring only for the keyboard; Escape and overlay close; `asChild` on Button; `data-slot`, `data-variant`, `data-size` attributes kept; reduced motion respected.
- **AC8 Measurements.** Bundle size (gzip and raw, JS and CSS) before and after; first paint and theme flash in the real window; build time; cascade layer and `@property` support in the macOS webview (measured) and the Windows webview (cited).
- **AC9 Frames.** REAL-WINDOW frames, light and dark, default and minimum size, before and after, of Settings, a dialog, a form input, a switch; every pair opened and compared.
- **AC10 Go or no-go** written in the ADR and in this plan, with the numbers.

## Named tests

| Test | File | AC |
|---|---|---|
| `astryx_deps_are_exactly_pinned` | `web/packages/ui/src/lib/astryx-build.test.ts` | AC1 |
| `astryx_layer_order_is_declared_first` | `web/packages/ui/src/lib/astryx-build.test.ts` | AC2 |
| `smind_theme_covers_light_and_dark_for_every_token` | `web/packages/ui/src/theme/smind-theme.test.ts` | AC4 |
| `smind_theme_stylesheet_is_built_not_compiled_at_runtime` | `web/packages/ui/src/theme/smind-theme.test.ts` | AC4 |
| `prepaint_script_sets_the_astryx_theme_attributes` | `web/packages/ui/src/theme/smind-theme.test.ts` | AC5 |
| `button_keeps_variants_sizes_and_data_attributes` | `web/packages/ui/src/components/ui/button.test.tsx` | AC6, AC7 |
| `button_as_child_renders_the_child_element` | `web/packages/ui/src/components/ui/button.test.tsx` | AC7 |
| `input_forwards_props_and_aria_invalid` | `web/packages/ui/src/components/ui/input.test.tsx` | AC6 |
| `switch_toggles_and_keeps_data_state` | `web/packages/ui/src/components/ui/switch.test.tsx` | AC6, AC7 |
| `dialog_pointer_open_shows_no_focus_ring` | `web/packages/ui/src/components/ui/dialog.test.tsx` | AC7 |
| `dialog_escape_and_overlay_close_it` | `web/packages/ui/src/components/ui/dialog.test.tsx` | AC7 |
| `no_call_site_changed_for_the_four_wrappers` | `web/packages/ui/src/lib/astryx-wave0-scope.test.ts` | AC6 |

## Decisions

- Pinned version is the latest published release (0.7.17), not the repository head (0.7.18).
- Write scopes: Peer 1 build (package.json, lockfile, vite config, CSS entry); Peer 2 theme files only; Peer 3 the four wrappers and their tests. Lane A does not touch `components/ui` or the build.
- Coding is done by Codex peers; fall back to GLM then Sonnet on a usage limit, logged below.

## Progress

- Spec and contract committed first. Peers (Codex tried first, tool host failed to start: Gatekeeper prompt, not a usage limit; GLM limited until 20:24): Peer 1 deps and wiring, Peer 2 theme, Peer 3 wrappers, Peer 4 class-pinned tests and harness, Peer 5 visual parity, Peer 6 three residual defects, experiment 783 source-compiled build: all `peer-sonnet`. Smoke runs: `peer-codex` (793, worked), `peer-glm` (795, worked), `peer-opencode` (794, authentication error).
- Lead: the plugin switch, the layer-order fix in `index.html`, the size gate, frames, measurements.

## Validation

See ADR-0064, section "Wave 0 results": the measurements, the findings and the go or no-go. AC1 to AC7 are covered by the named tests (all pass); AC8 measurements and AC9 frames are in the ADR section and `~/.smind/dogfood/astryx-wave0/frames`; AC10 is the go or no-go there.
