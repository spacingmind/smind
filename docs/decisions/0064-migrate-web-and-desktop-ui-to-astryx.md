# 0064: Migrate the web and desktop UI to Astryx

## Status

**Accepted for the direction — 2026-11-28.** The Human decided to move all of the web UI and the desktop UI to Astryx. This record does not argue that again. It fixes the strategy, the build and theme approach, the risks and the order of work. The four sub-choices in [Open sub-decisions](#open-sub-decisions-for-the-human) are still open; each has a recommendation, and the waves that depend on one wait for the Human.

**Wave 0 (the spike) is built and measured: see [Wave 0 results](#wave-0-results-2026-11-28) for the numbers and the go or no-go.** Steps 5, 7 and 9 of the Decision were amended by what the spike found; the amendments are marked "Wave 0".

Builds on [ADR-0013](0013-desktop-bundled-ui.md) (the desktop shell bundles the same web UI), [ADR-0051](0051-web-demo-mode-and-app-spacingmind-com.md) (the demo build) and the token rules in `docs/design.md`.

## Context

**What Astryx is.** A React DOM design system, MIT licensed, from Meta (`@astryxdesign/core`, 0.7.x at the time of writing; read at commit 2fb8581 of its repository). Components are styled with StyleX and shipped inside CSS cascade layers (`reset`, `astryx-base`, `astryx-theme`). A theme is a set of tokens built with `defineTheme`, with light and dark values side by side, and it can extend a base theme. It needs React 19 or later and `@stylexjs/stylex` ^0.19 as peers. Its core package has the pieces sMind hand-builds today: command palette, toast, drawer, bottom sheet, typeahead, table, side nav, dialog, menus, and a set of chat components (layout, message list, message, composer with tokens and drafts, send button, tool calls, system message).

**How you use it with Vite.** The repository has two examples. One compiles the library from source with its Vite plugin (`@astryxdesign/build/vite`), which gives library and app styles separate class prefixes and separate layers. The other imports the prebuilt stylesheets and layers them with Tailwind 4 in one CSS file (`@layer reset, theme, base, astryx-base, astryx-theme, components, utilities`). The second path is the lower-risk start for us, because we already run Tailwind 4 with `@tailwindcss/vite`.

**Where sMind is.**

- `web/packages/ui/src/components/ui/` holds 59 primitives (shadcn-style: Radix wrappers with `class-variance-authority` and Tailwind). 21 files import `radix-ui`, 4 use `class-variance-authority`, and about 288 files import from `components/ui/`.
- Tokens are the reference-derived set in `docs/design.md`, declared in `web/packages/ui/src/index.css` (`:root` and `.dark`, Tailwind `@theme inline`).
- Theme mode is a `dark` class on `<html>`, set by a script in `web/packages/ui/index.html` before first paint, and by `web/packages/ui/src/lib/theme.ts` afterwards.
- Hand-built pieces that Astryx also ships: the command palette, toasts, the sheet, typeaheads (mentions, pickers), tables and the sidebar.
- The desktop shell loads the same UI build (`build:desktop`). The demo build (`build:demo`) is the same UI against a scripted in-browser daemon.
- The vite version is `^7.1` here; the Astryx Vite example uses 8.x. Whether the plugin works on our version is a Wave 0 question, not a known fact.

**Out of scope: mobile.** The Expo app stays as it is. `@astryxdesign/native` is published only under a canary tag, is marked experimental, and has no React Native binding we could use.

## Decision

1. **Scope.** The migration covers `web/packages/ui` (`components/ui/` first, then the screens), the desktop shell, the demo build, and the e2e and frames harness that checks them. Mobile is unchanged.

2. **Strategy: wrappers first.** Re-implement each `components/ui/*` primitive on top of Astryx and keep its props and exports, so call sites do not change. When a wrapper is a thin pass-through, replace it with direct Astryx use in the files that call it, one family at a time. Then adopt Astryx for the hand-built pieces: command palette, toast, drawer and sheet, typeahead, table, side nav. The chat pieces follow the sub-decision below.

3. **Waves, not a big bang.** Each wave is one or a few PRs and ends with frames: the real app window, light and dark, at the default size and at the minimum size, for every state the wave touched. A wave is not done until a person has compared every light and dark pair. Once a wave lands, a drift test forbids the old way for what that wave moved (`radix-ui` imports and hand-rolled copies of the primitive). When the last wave lands, `radix-ui` and `class-variance-authority` leave `package.json`.

4. **Theme.** One custom sMind theme, written with `defineTheme`, extending the neutral Astryx theme, filled from our reference-derived tokens in `docs/design.md`. It has light and dark values, and the app keeps its three modes (light, dark, follow the system). The theme is **built ahead of time** into a stylesheet that ships with the app. It is not compiled in the browser, so there is no style flash while the desktop webview starts. The inline script in `index.html` keeps setting the mode before first paint.

5. **Build.** *Wave 0 amendment:* compile Astryx from source with its Vite plugin (`@astryxdesign/build/vite`, `rootDir` set to the workspace root), with Tailwind 4 kept and layered. The prebuilt-stylesheet path was tried first and rejected: it ships the whole library's CSS (+52 kB gzip) before one component uses it. Components are imported only through deep entry points (`@astryxdesign/core/Button`, never the barrel), or the whole library's CSS comes back. The layer order is declared **inline, first in `<head>` of `index.html`**, as well as at the top of `index.css`: Astryx injects its own `@layer reset, astryx-base, astryx-theme;` at runtime (its `Theme` and `CodeBlock` modules), and the first declaration of a layer name wins, so without the inline line Tailwind's preflight outranks every Astryx style. A test pins it. React 19 is already met. Versions of every `@astryxdesign/*` package and `@stylexjs/stylex` are pinned exactly.

6. **Tailwind stays during the migration.** It is the layout and spacing glue for screens that are not moved yet. Whether it stays afterwards is open sub-decision 1.

7. **Wave 0 spike, then go or no-go.** Before any wave: the theme plus three primitives (Button, Input, Switch) behind their wrappers, in a branch. Measure and write down:
   - **Bundle size**: gzip size of the production JS and CSS from `bun run build` in `web/packages/ui`, before and after, same command and same machine. The budget is set by the spike; the starting guess is +15% over the current total, and a number above it is a no-go unless the Human agrees.
   - **Theme flash**: first paint in the real desktop window (not only a browser), cold start, light and dark, a screen recording or frame sequence of the first 500 ms.
   - **Build time**: cold and warm `vite build`, and dev server start.
   - **Webview support**: the build runs in the macOS webview and in the Windows webview with cascade layers working (see Risks).
   - **Focus rule**: opening a menu, dialog or popover with the mouse shows no focus ring (the B3 rule); a keyboard open shows one.
   The spike ends with a written go or no-go in the plan file. On no-go, this record is amended, not silently dropped.

8. **First small item after Wave 0: opt-in "done" feedback.** A calm check mark that draws itself in about 400 to 600 ms when a task succeeds or when a pull request that sMind opened is merged. Rules: off by default; one switch, Settings, Appearance, "Celebrate finished work"; disabled under the system reduced-motion setting and under the in-app reduced-motion setting; the state change itself (the status text, the badge) always shows, with or without the effect; no sound; one shared animation, not one per screen. Frames at 0, 25, 50, 75 and 100 percent, light and dark, plus the reduced-motion frame.

9. **Size gate (Human decision, 2026-11-28).** The demo size limits (`web/packages/ui/scripts/check-demo-build.mjs`: initial JS 1,150,000 B gzip, initial CSS 35,840 B gzip) are **raised temporarily** to Wave 0's measured numbers plus a small margin (JS 1,290,000 B, CSS 55,296 B), because radix and Astryx coexist until the last wave. The script and file names do not change. The old limits stay in the script as `FINAL_BUDGET`. It is a ratchet: each later wave keeps or lowers the numbers in the same PR, never raises them. The final wave (radix and cva removed) sets the limits back to `FINAL_BUDGET`, or justifies an exception here.

10. **Queue position.** The code waves start after the P0 batch and the non-git workspace work ([ADR-0063](0063-workspaces-without-a-git-worktree.md), still proposed) and **before** the P1 batch and the create-project steps C2 to C5, so new UI lands on Astryx instead of being moved twice. The "done" feedback item follows Wave 0.

11. **Calm by default.** Motion stays quiet. Nothing in the migration adds effects that the current UI does not have, except the opt-in item above.

### Wave order (recommended, see sub-decision 3)

| Wave | Content |
|---|---|
| 0 | Spike: theme, Button, Input, Switch, measurements, go or no-go |
| 1 | Theme and tokens in the app; Button, Input, Select, Checkbox, Switch, Dialog and their wrappers |
| 2 | Settings (the modal and its sections), forms, alerts, banners |
| 3 | Command palette, sidebar and side nav, toasts, sheets and drawers, menus, tooltips, popovers |
| 4 | Typeaheads and tables; desktop shell chrome |
| 5 | Task chat (only if sub-decision 2 says adopt), then removal of `radix-ui` and `class-variance-authority`, the final drift test |

## Wave 0 results (2026-11-28)

What was built: pinned deps (`@astryxdesign/core`, `theme-neutral`, `build`, `cli` 0.7.17, `@stylexjs/stylex` 0.19.2), the source-compiled build, the sMind theme built ahead of time (`src/theme/smind-theme.ts` to `smind-theme.css`), the layer order, and Button, Input, Switch and Dialog on Astryx behind the existing wrappers with the same props. Plan: `docs/plans/active/astryx-wave0.md`.

**Measurements** (`vite build` of `web/packages/ui`, same machine, gzip level as vite prints; JS is the main chunk, CSS is every stylesheet in `assets/`):

| State | CSS raw / gzip | JS raw / gzip | Build, warm |
|---|---|---|---|
| Before (develop) | 176.2 kB / 28.9 kB | 3,684.6 kB / 1,117.4 kB | 3.8 s |
| Prebuilt Astryx CSS, no component used | 501.6 kB / 81.1 kB | 3,684.8 kB / 1,117.4 kB | 3.9 s |
| Source build, no component used | 203.6 kB / 33.2 kB | 3,684.8 kB / 1,117.4 kB | 4.6 s |
| Source build, four primitives (final) | 320.5 kB / 51.5 kB | 4,128.0 kB / 1,262.4 kB | 9 to 14 s |

The change from before to the final state is CSS +22.5 kB gzip (+78%) and JS +145.0 kB gzip (+13%): +14.6% of the two together. The demo check reads 1,259,181 B initial JS and 51,051 B initial CSS (old limits 1,150,000 and 35,840), so the gate was raised (Decision 9). Most of the JS is shared infrastructure that the first components pull in (layers, positioning, i18n), so the cost of the next components is expected to be lower; that is not measured yet. Warm build time is 2.4 to 3.6 times the old one; fixing it is a separate small item.

**Checks that passed:** `tsc -b`; the web unit suite (552 files, 6,012 tests, after the wrappers were fixed); the Chromium e2e suite on the final head (249 passed, 56 skipped, 0 failed); the demo build and its check; the desktop build; the dev server starts; Vite 7.3.6 works with the plugin; vitest needs no plugin.

**Real window** (macOS 27.2.1 audit app, REAL-WINDOW frames, light and dark, 1280x800 and the 800x520 minimum, before and after, in `~/.smind/dogfood/astryx-wave0/frames`): cascade layers, nested layers, `@scope` and `light-dark()` all work in the macOS webview. First paint in dark mode shows no light flash at about 80 ms per frame, same as before. The Windows webview was not run; from the browser compatibility tables its Chromium engine supports layers (99), `@property` (85), `@scope` (118) and `light-dark()` (123).

**What the spike found**

1. The layer order bug above: every Astryx style lost to Tailwind's preflight until the order was declared in `index.html`. Unit tests could not see it; only a real page did.
2. Wrappers are not drop-in at the DOM level. The first swap broke 47 unit tests: duplicate live regions, a label that renders twice, focus and Escape behaviour, `data-slot`, `aria-busy`, class names pinned by tests, a dialog structure that the call sites' class names assume. Each was fixed in the wrapper or its test. Input needed the most workarounds (Astryx wants a label and a controlled value, and takes no class name for the `<input>` element). Dialog needed its sizing classes routed to the right element.
3. jsdom is not enough: the first real-window pass showed unstyled buttons, a dialog stuck in a corner, an icon above its label, a heavy focus ring and a dialog that is 130 px short as a small-window sheet. All were fixed and re-checked. Real-window frames stay a hard gate for every wave.
4. **Browser floor.** `light-dark()` needs Safari 17.5 (macOS 14.5) and Chromium 123. Tailwind 4 alone needs Safari 16.4. The supported macOS and Windows webview versions must be written down before Wave 1.
5. One difference is open: in the Add project dialog the primary button looks greyed in the first moments (before, it was solid). Functional e2e passes. It needs a look in Wave 1.

**Staffing log (truthful).** Codex (`peer-codex`) was tried first for every Wave 0 step. Its tool host failed to start (a macOS Gatekeeper prompt on the Codex code-mode host, not a usage limit): three tasks, six runs, no shell command ever ran. GLM was then rate-limited until 20:24. So Wave 0's code was written by `peer-sonnet` (build wiring, theme, wrappers, test fixes, visual fixes, the source-build experiment). After the prompt was cleared, a smoke run on `peer-codex` worked end to end (shell, edit, tests, commit) and its test was kept. `peer-opencode` (GLM 5.4) failed at once with an authentication error from the OpenCode ACP session (no Z.AI credential); `peer-glm` (5.3) did the same smoke step in 40 s.

### Go or no-go

**GO to Wave 1, with these conditions** (each needs the Human's yes):

1. Accept the size ratchet (Decision 9): about +14.6% gzip for the first four primitives, falling back to the old limits by the last wave.
2. Accept the browser floor in finding 4, or change the theme tokens to avoid `light-dark()`.
3. Change the strategy for Input and Dialog: after Wave 1 they are migrated at their call sites directly, not through a wrapper. Button and Switch keep their wrappers for now.
4. Real-window light and dark frames (default and minimum size) are a merge gate for every wave, as stated.
5. Warm build time is brought back near 3.8 s in its own small PR.

A no-go would have come from the prebuilt path (size) or from the layer problem; both have a fix. The migration is feasible. It is not cheap: expect each wave to need a frame loop, not only tests.

## Risks

- **0.x API churn.** Astryx is pre-1.0; names and props can change between minors. Pin exact versions. A bump is its own PR with the full frame check. Never use a canary or experimental API.
- **StyleX build complexity.** The compile-from-source path adds a Babel step and prefix and layer rules. The prebuilt path avoids most of it. Wave 0 measures both only if the first one fails.
- **Accessibility regressions.** Astryx has its own focus handling: it follows the keyboard or pointer using the browser's `:focus-visible` plus a shared "last input" state. Our B3 rule (no focus ring when a menu or dialog opens by pointer) must hold after the swap. Every wrapper keeps its current keyboard tests; focus is part of each wave's frames.
- **Redoing B2 and B3.** The Settings modal and the Browser settings were just built on our primitives. Wave 2 moves them. Their tests are the safety net, and the frames must match the intent, not necessarily the pixels.
- **Webview CSS support.** Cascade layers need Safari 15.4 or later (the macOS webview) and Chromium 99 or later (the Windows webview). `@property` needs Safari 16.4 or later. Our minimum supported OS versions must be checked against these in Wave 0 on a real window; this record does not claim they are met. If a target is too old, that is a no-go for that platform, found early.
- **Two styling systems at once.** During the migration both Tailwind utilities and Astryx styles are on screen. The layer order makes Tailwind utilities win on purpose, so a wrapper can still be adjusted from a call site. A screen must not mix the two for the same property.
- **Bundle size and startup.** A design system can add a lot of JS and CSS. The budget in step 7 covers it; the demo build has its own size check (`web/packages/ui/scripts/check-demo-build.mjs`) which must still pass.
- **Mobile drift.** Mobile keeps its own look. Shared tokens stay in `docs/design.md`; the web theme file is generated from or checked against them, so the two do not drift.

## Open sub-decisions for the Human

1. **Keep Tailwind after the migration, or phase it out?** Recommendation: keep it for layout and spacing in screens, phase out only its component styling (the shadcn token layer). Removing it entirely is a second large change with little gain.
2. **Adopt the Astryx chat components for the task chat, or keep ours?** Recommendation: decide after Wave 4 with the spike's numbers. Our chat has behaviour Astryx may not cover (run timeline, permission cards, queued messages, stream resume). Default is keep ours and adopt only the parts that fit (composer, scroll button).
3. **Wave order.** Recommendation: the table above (tokens, theme and the core inputs first, then Settings, then palette, sidebar and toasts, then chat last).
4. **Canary and experimental APIs.** Recommendation: never use them. Pin stable releases only.

## Agent-UI pattern checklist

Each wave checks the screens it touches against this list. The words are generic on purpose.

- **Approval card.** Shows a risk badge, a preview of what will happen, and the approve and deny shortcuts.
- **Permission request.** States who is asking, why, and exactly what it will do.
- **One streamed answer, every state.** Streaming, stopped, continue from the exact point, failed, retry from that point, done. The layout does not shift between states.
- **Worst-case data.** Long names, empty lists, huge numbers and many items are checked in every component, not only the happy path.
- **Focus ring only for the keyboard.** A pointer open or click does not draw one.
- **Reduced motion.** Effects turn off; state changes still show.

Candidate future components, **not** part of this migration, listed so they are not lost: a context-window meter; a per-run trace and cost waterfall; an "autonomy dial", one control that maps to the permission mode and the saved grants.

## Alternatives considered

- **Stay on shadcn-style primitives.** Nothing changes and nothing breaks, but we keep maintaining 59 primitives and a hand-built palette, toast, sheet and table. The Human decided against it.
- **A big-bang rewrite of every screen.** Faster on paper; it breaks frames review, mixes bugs from the library with bugs from the rewrite, and blocks other work. Passed over for wrappers first.
- **Use Astryx on new screens only.** Cheap, but the app would carry two component systems for a long time and look uneven. Passed over, except that it is the fallback if Wave 0 is a no-go for some primitives.
- **Compile Astryx from source from day one.** Gives the finest theming control, but adds the most build risk. Kept as the second step.
- **Include mobile.** Not possible today: the native package is canary-only and has no React Native binding. Revisit only if that changes, in its own record.

## Rationale

Wrappers first keeps about 288 call sites untouched, so each wave is a small, reviewable diff with frames. A build-time theme removes the one thing a webview shows badly, a flash of the wrong style at start. Keeping Tailwind layered means screens can move one at a time. Pinning exact versions and gating every bump with frames answers the 0.x risk. The spike comes first because the three things that could stop the whole plan (size, webview support, focus behaviour) are cheap to measure and expensive to find late.
