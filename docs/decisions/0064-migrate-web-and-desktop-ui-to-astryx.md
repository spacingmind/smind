# 0064: Migrate the web and desktop UI to Astryx

## Status

**Accepted for the direction — 2026-11-28.** The Human decided to move all of the web UI and the desktop UI to Astryx. This record does not argue that again. It fixes the strategy, the build and theme approach, the risks and the order of work. The four sub-choices in [Open sub-decisions](#open-sub-decisions-for-the-human) are still open; each has a recommendation, and the waves that depend on one wait for the Human.

Nothing is built yet. The first code is the Wave 0 spike, and it starts only on the Human's go.

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

5. **Build.** Start on the prebuilt-stylesheet path with Tailwind 4 kept and layered (layer order as in the Astryx example, with our theme in `astryx-theme`). Move to compiling Astryx from source with its Vite plugin only if Wave 0 shows a size or theming reason. React 19 is already met. Versions of every `@astryxdesign/*` package and `@stylexjs/stylex` are pinned exactly.

6. **Tailwind stays during the migration.** It is the layout and spacing glue for screens that are not moved yet. Whether it stays afterwards is open sub-decision 1.

7. **Wave 0 spike, then go or no-go.** Before any wave: the theme plus three primitives (Button, Input, Switch) behind their wrappers, in a branch. Measure and write down:
   - **Bundle size**: gzip size of the production JS and CSS from `bun run build` in `web/packages/ui`, before and after, same command and same machine. The budget is set by the spike; the starting guess is +15% over the current total, and a number above it is a no-go unless the Human agrees.
   - **Theme flash**: first paint in the real desktop window (not only a browser), cold start, light and dark, a screen recording or frame sequence of the first 500 ms.
   - **Build time**: cold and warm `vite build`, and dev server start.
   - **Webview support**: the build runs in the macOS webview and in the Windows webview with cascade layers working (see Risks).
   - **Focus rule**: opening a menu, dialog or popover with the mouse shows no focus ring (the B3 rule); a keyboard open shows one.
   The spike ends with a written go or no-go in the plan file. On no-go, this record is amended, not silently dropped.

8. **First small item after Wave 0: opt-in "done" feedback.** A calm check mark that draws itself in about 400 to 600 ms when a task succeeds or when a pull request that sMind opened is merged. Rules: off by default; one switch, Settings, Appearance, "Celebrate finished work"; disabled under the system reduced-motion setting and under the in-app reduced-motion setting; the state change itself (the status text, the badge) always shows, with or without the effect; no sound; one shared animation, not one per screen. Frames at 0, 25, 50, 75 and 100 percent, light and dark, plus the reduced-motion frame.

9. **Queue position.** The code waves start after the P0 batch and the non-git workspace work ([ADR-0063](0063-workspaces-without-a-git-worktree.md), still proposed) and **before** the P1 batch and the create-project steps C2 to C5, so new UI lands on Astryx instead of being moved twice. The "done" feedback item follows Wave 0.

10. **Calm by default.** Motion stays quiet. Nothing in the migration adds effects that the current UI does not have, except the opt-in item above.

### Wave order (recommended, see sub-decision 3)

| Wave | Content |
|---|---|
| 0 | Spike: theme, Button, Input, Switch, measurements, go or no-go |
| 1 | Theme and tokens in the app; Button, Input, Select, Checkbox, Switch, Dialog and their wrappers |
| 2 | Settings (the modal and its sections), forms, alerts, banners |
| 3 | Command palette, sidebar and side nav, toasts, sheets and drawers, menus, tooltips, popovers |
| 4 | Typeaheads and tables; desktop shell chrome |
| 5 | Task chat (only if sub-decision 2 says adopt), then removal of `radix-ui` and `class-variance-authority`, the final drift test |

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
