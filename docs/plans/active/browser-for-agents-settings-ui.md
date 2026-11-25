# Browser for agents: the Settings section (PR B3)

Governing: ADR-0061 section 12 (no browser UI on mobile; the settings are local-only), AC14 to AC16 of `desktop-browser-for-agents.md`, ADR-0032 (shared query cache), ADR-0056 (what the switch does and does not protect against). The daemon side is merged: `browser.settings.get`, `browser.settings.set` (`enabled`, `allowAdd`, `allowRemove`, `allowHosts`, `profileMode`) and `browser.data.clear` are human-only and local-only, the switch is off by default, and the settings view carries `killSwitch`, `available`, `chromeInstalled`, `installStep`, `note` and the `providers` reach list. This PR is the web UI over them. Size: M (6). HIGH-RISK-lite: it flips a security switch, so a foreground two-reviewer pass runs before the Steward's.

## Where it lives

A new Settings section `browser` ("Browser"), registered in the existing settings registry (`components/settings/settings-registry.ts`) in the group "Profiles & providers" at order 197, next to Permission policy (195) and Saved grants (196): the same place as every other per-workspace or permission section, no new surface. Like "Skills (learned)" it opens with a workspace picker (`Select`) and shows the chosen workspace's settings. Data goes through the shared query cache (`queryKeys.browserSettings(workspaceId)`), never component state; the three RPCs are the only way to change anything.

## What the section shows (top to bottom)

1. **Workspace picker** (shared workspace tree query, the first workspace by default).
2. **Daemon state banners** (text, never colour alone), each with its own test id:
   - kill switch on: "The browser is disabled by the daemon's configuration (`browser.disabled` or `SMIND_BROWSER_DISABLED`). It is off for every workspace and the switch below cannot turn it on." The switch is disabled.
   - no browser installed (`chromeInstalled` false): "No browser installed" with the daemon's `installStep` verbatim. The switch stays usable (the setting is stored) but the banner says tools are not offered until one is installed.
   - not available in this daemon version (`available` false): the existing "not yet offered" wording.
3. **The switch** row: "Let agents use a browser in this workspace" (default off, stated), a `Switch` with an accessible name and a visible "On"/"Off" text, `aria-busy` while the call runs. Turning it on or off calls `browser.settings.set {workspaceId, enabled}`; the daemon's refusal text, if any, is shown as-is under the row.
4. **The residual text next to the switch** (always visible, not behind a disclosure), three short paragraphs that mirror `public-docs/browser.md`:
   - (a) the unauthenticated-local limit: "A local program that talks to the daemon without saying who it is counts as you, and so does anything inside the agent's own process tree. Keep the daemon token private." (the proxy and run-socket limit);
   - (b) the ADR-0056 shell limit: "The permission cards protect against the model, not against a shell-capable agent that attacks the daemon: it could answer a card through the daemon's websocket. Do not let an agent you do not trust run shell commands.";
   - (c) the Windows and provider statements from the reach table: "On Windows the browser is not offered." plus the providers list below.
5. **Allowlist editor** (see below), shown with the switch whatever its state (the list is kept while off).
6. **Live indicator**: when `sessionLive` is true: "A run is using the browser now" with an "End session" button (`browser.data.clear`, which ends the session and removes its profile). It is a polite status region; it appears and disappears without moving the rest of the page (reserved height or inline row, no layout jump in the frames).
7. **Profile**: "Ephemeral: a fresh profile that is removed when the session ends" (text only; persistent is not offered) and a "Clear browser data" button (the same RPC) with a confirm.
8. **Which agents get it**: the `providers` list from the settings view, grouped "Browser tools offered" (reach yes), "Offered, not guaranteed" (conditional) and "No browser for these agents" (no, with the daemon's reason), each entry with its label and, for an agent that gets the browser, the screenshots capability as text ("Screenshots: yes" / "no, text tools only" / "unknown, text tools only"); an agent without the browser shows only the daemon's reason (screenshots are moot). Built from the daemon's list, never a hard-coded one; the text for a provider without the browser is the daemon's `why`.

## The allowlist editor (`browser-allowlist-editor.tsx`)

Exact hosts only: a text input and an "Add" button (Enter submits), a list of the hosts each with a "Remove" button (icon + the text label as the accessible name "Remove <host>"), an empty state ("Only localhost is reachable"), a counter "N of 50" and the add disabled at 50 with the text "The list is full (50 hosts)". The shown note from the daemon (`note`, "a host name may only resolve to public addresses; to allow an intranet or local-network host, add its IP address instead; loopback is always allowed") is rendered under the input. Validation is the daemon's: the add calls `browser.settings.set {allowAdd:[host]}` and a refusal's text (for example "wildcards are not allowed") is shown under the input verbatim, the input keeps what was typed and gets `aria-invalid`; a successful add clears it. A removal calls `allowRemove:[host]`. Pending calls disable the control that started them and expose `aria-busy`. The editor is a controlled component (hosts, onAdd, onRemove, error, pending, max) so it is tested without a daemon.

## Daemon change (small, read-only)

`browser.settings.get/set` views gain `sessionLive` (bool: this workspace has a running browser session), from `Manager.Live(key)`. No new method, no new permission, no store change. The section polls the view every 3 s while it is mounted (the shared query's `refetchInterval`) so the indicator follows a run.

## Acceptance criteria

- AC1 The section is registered once, in "Profiles & providers" at order 197, reachable from the settings nav and the command palette entry for settings sections; it follows the existing section pattern (registry, `SettingsGroup`/`SettingsRow`, workspace `Select`).
- AC2 Default state (a workspace with no stored settings): the switch is Off with the text "Off", the allowlist is empty with the localhost-only text, no live indicator.
- AC3 Turning the switch on/off sends exactly `browser.settings.set {workspaceId, enabled}`; the view re-renders from the returned view; a daemon refusal is shown verbatim and the switch returns to its stored state.
- AC4 Add: a valid host is sent as `allowAdd:[host]` and appears; an invalid one shows the daemon's text under the input, keeps the typed value, sets `aria-invalid` and changes nothing. Remove sends `allowRemove:[host]`. The 50 cap disables add with its own text.
- AC5 Kill switch on: banner text, switch disabled, allowlist still editable (the setting is stored). Chrome missing: banner with the daemon's `installStep`. `available` false: the "not yet offered" text.
- AC6 The live indicator appears only when `sessionLive` is true and "End session" calls `browser.data.clear {workspaceId}`; "Clear browser data" asks for confirmation first.
- AC7 The residual text (a), (b), (c) is always visible and matches `public-docs/browser.md` (a test reads the doc and asserts the key sentences are present in both).
- AC8 The providers list renders from the daemon's `providers` (three groups, daemon `why` for a no), with screenshots as text; an empty list renders a neutral line, never a hard-coded one.
- AC9 Nothing in the section can change a setting except the three human-only RPCs; no call is made by merely rendering (the get is the only call on mount).
- AC10 Icons come from `lib/icons.ts` (new names added there and covered by its drift test); colours are tokens; state is always conveyed by text as well as colour; light and dark, 1280 and 390, no horizontal overflow at 390.
- AC11 The mobile app is untouched (ADR-0061 decision 12) and no shared string changes, so no mobile suite run.

## Named tests

Go (`internal/wsapi/browser_settings_test.go`, `internal/browser/session_test.go`): `TestBrowserSettingsViewReportsSessionLive`, `TestManagerLive`.

Web unit (vitest, `web/packages/ui/src/...`): `browser-allowlist-editor.test.tsx` (empty state, add by button and Enter, error shown verbatim with aria-invalid and the typed value kept, cap disables add, remove label, pending disables, no wildcards logic of its own), `browser-reach-list.test.tsx` (groups, daemon why, screenshots text, empty), `use-browser-settings.test.tsx` (get on mount only, set payloads exactly, clear payload, refetch interval, error text passed through), `browser-section.test.tsx` (AC2 to AC9, the doc/section sentence match, the icons registry), `settings-registry.test.ts` entry (id, group, order).

E2E (`web/e2e/specs/browser-settings.spec.ts`, real daemon, fake agent): `BS-01 the section is off by default and the switch persists`, `BS-02 an allowlist host can be added and removed, an invalid one shows the daemon's text`, `BS-03 the residual text and the providers list are visible`, `BS-04 narrow layout does not overflow`. The kill-switch, no-browser and live states are driven with `routeWebSocket` in the frames script (the real daemon cannot be put in those states from a spec without a model call or a different binary environment) and with unit tests.

## Mutations (committed files, each must fail a named test)

- The switch sends `enabled` inverted -> section test AC3.
- The add sends `allowHosts` instead of `allowAdd` -> hook test and e2e BS-02.
- The error text replaced by a generic one -> editor test.
- The cap not enforced -> editor test.
- The kill-switch banner removed or the switch left enabled -> section test AC5.
- The live indicator shown while `sessionLive` is false -> section test AC6.
- A residual paragraph removed -> the doc/section sentence test.
- A hard-coded provider list -> reach-list test with a custom list.
- A call on mount other than the get -> hook test AC9.

## Frames (not merged: `~/.smind/dogfood/browser-settings-ui/` with a README and a contact sheet)

Light and dark at web 1280 and 390 for every state: off, on-empty, on-with-hosts, invalid host (error), live indicator, kill switch on, Chrome missing. The script runs an isolated daemon on its own port (never :4648) with the fake agent, rewrites `browser.settings.get/set` answers per state with `page.routeWebSocket`, and asserts luminance (light vs dark), `scrollWidth <= clientWidth` at 390, no console errors, and that no run or model call happens. Every pair is opened and compared by hand.

## Decisions

1. Placement: the existing settings registry, group "Profiles & providers", order 197.
2. The live indicator needs one read-only daemon field (`sessionLive`); the alternative (watching run timeline notes) cannot tell a live session from an ended one.
3. "End session" and "Clear browser data" are the same RPC (profiles are ephemeral: ending the session removes the data); the confirm differs only in wording.
4. The section is local-only like the RPCs: over the relay (a paired phone) the daemon refuses and the section shows the refusal text; the mobile app has no browser UI.

## Not in this PR

Persistent profiles, a richer permission-card renderer (`Kind: "browser"` cards use the generic card), a per-run "browser" chip in the task header (AC15 of the parent spec: the section's indicator is the first step; the task-header chip is a follow-up), the desktop host, Windows.

## Progress

- [ ] Spec committed (this file)
- [ ] Daemon `sessionLive`
- [ ] Hook, editor, reach list, section, icons, registry
- [ ] Unit tests, e2e specs, full e2e matrix
- [ ] Frames, README, contact sheet, every pair opened
- [ ] Reviews, mutations

## Validation

(filled in when the work is done)
