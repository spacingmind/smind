# PA.5 — Mobile: read-only plugins, prompt commands, themes

Parent: [plugins.md](plugins.md) (AC15 mobile part, AC30 lint note), [ADR-0042](../../decisions/0042-plugin-system.md).
Size: M (5) — scope 1 (mobile only, one dev-tooling hook), PRs 1, risk 1 (relay-readable data rendered on a phone), decisions 1, verification 1 (390 px web export, no device).

## Acceptance criteria

- AM1 Plugins list (read-only), reached from the Connection sheet ("Plugins"): off state "Plugins are off on this daemon. Turn them on in config.yaml on the computer."; empty state; rows with name, version, source kind chip, status chip (Enabled / Disabled / Pending update / Error), last error line, link warning line for `link` sources. Refetch on `plugin.updated`.
- AM2 Detail (read-only): the consent summary as returned (capabilities, MCP servers, escalating profiles flagged, skills, contributions with "from <plugin>" owner labels, conflicts). No enable/disable/install/remove/update/reload control; the screen says "Manage plugins on the computer: Settings -> Plugins". The client calls only `plugin.list/get/commands/themes`.
- AM3 Prompt commands: `/` at the start of the composer shows a compact suggestion list above it ("from <plugin>"); selecting inserts the prompt and never sends; built-ins win a bare name, the first plugin by id wins between plugins; no plugin commands means the composer is unchanged.
- AM4 Themes: plugin themes selectable on the Plugins screen. One explicit token-to-key mapping table with a completeness test; strict value validation as web; oklch converted; fall back to the default when the plugin is disabled or removed; the choice persists.
- AM5 Safety: a source scan bans `eval` / `new Function` in `mobile/src`; plugin text renders as plain `<Text>`; no remote image from plugin data. Hostile strings render as text.
- AM6 Docs: `public-docs/mobile-and-relay.md` paragraph; plan Progress/Validation; ADR-0042 dated note.

## Test scenarios (vitest, `cd mobile && npm test`)

- `pluginsList.test.ts`: off / empty / list / error / link warning / status chip precedence.
- `pluginDetail.test.ts`: the view model has no action; `pluginsReadOnly.test.ts`: the api module and the screens call only the four read RPCs (client mock).
- `pluginSlash.test.ts`: `InsertsNeverSends`, `BuiltinWinsCollision`, `FirstPluginByIdWinsBetweenPlugins`, `NoPluginCommands_ComposerUnchanged`, `findActiveSlash`, replacement.
- `pluginTheme.test.ts`: mapping completeness (every `AppTheme` colour key mapped or listed not themeable; every mapped token is canonical), hostile values rejected (`url(`, `var(`, `calc(`, `;`, quotes, lookalikes, over 64 bytes), oklch/hsl/rgb conversion, per-scheme application, fallback when the theme vanishes.
- `pluginSafety.test.ts`: no `eval`/`new Function` in `mobile/src`; hostile fixture strings reach `<Text>` unchanged.
- `pluginUpdates.test.ts`: refetch on `plugin.updated`.
- Mutations to run: (1) select sends; (2) a mutating RPC in the detail; (3) `url(` accepted; (4) built-in loses the collision.

## Decisions

- No daemon change. Wire: `plugin.list` (`{globalEnabled, plugins[]}`), `plugin.get {id}`, `plugin.commands`, `plugin.themes`, event `plugin.updated {id, globalEnabled}`; all relay-readable.
- oklch: implemented (a 20-line OKLab conversion), not rejected: web's own tokens are oklch and a plugin author writes one set for both apps.
- Theme persistence: only the selected `<plugin>:<id>` is stored (expo-secure-store, already linked: no dev-client rebuild; memory on web), never token values. Tokens are re-read from `plugin.themes` after connecting, so a removed plugin can never leave stale colours; until then the default applies.
- No state management library exists on mobile (screens use `conn.call` + `useState`); a small `usePluginData` hook follows that.
- Entry point: the Connection sheet (mobile has no settings screen).
- Dev tooling: `devharness` gets `plugin-install/link/enable/reload` commands (a second `plugins.Manager` on the same store and home) so the 390 px frames use a real daemon API with real plugins; not product code.

## Progress

- [ ] spec  [ ] data layer  [ ] slash  [ ] screens  [ ] themes  [ ] safety  [ ] harness + frames  [ ] docs

## Validation

(filled at the end)
