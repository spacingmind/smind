# Composer redesign — ZCode card, Paseo interaction

**Ask (2026-10-12):** the chat composer should look like ZCode's input card and
interact like Paseo's. Reference screenshots (local, gitignored):
`tmp/reference/{zcode,paseo}-composer.png`; current look:
`tmp/reference/smind-current-shell-dark.png`.

**Sources ported from:** `refs/zcode/packages/ui/src/v4/ConversationComposer.tsx`
(send/stop button pair, `rounded-2xl` shell, `ArrowUp`/`Square` icons) and
`refs/zcode/DESIGN.md` (input tokens, `rounded-2xl` main-input exception);
`refs/paseo/packages/app/src/composer/{agent-controls/control.tsx,
input/input.tsx,pill-styles.ts,diff-stat-pill.tsx}` (icon+value trigger with
an icon-only collapse, focus hint shown only when web ∧ unfocused ∧ empty,
chip row above the card). Paseo is React Native, so this is a port of the
*behaviour and proportions*, not the code.

## Findings that shape the scope

- `composer.focus` (Mod+L, plus Mod+Shift+L for terminals) is **already in the
  keyboard registry** (`keyboard/shortcuts.ts`) but **nothing claims it** —
  `grep useActionHandler("composer.focus"` is empty. The composer claims it
  itself (no `App.tsx` edit, which PR #297 is touching).
- The diff-stat pill (`task.diff` via `useTaskDiff`) **already exists**
  (`DiffStatPill`) — no new RPC. Status chips reuse it.
- No `@file` mention, `/command` or skill picker exists in the composer
  (`grep` over `web/packages/ui/src` finds none), so the placeholder must not
  advertise them.
- `ModeInfo.autoApproves` (ADR-0019, served by `provider.list`) is the
  existing "dangerous mode" signal — the warning tint keys off it plus
  `autoAccept` for ACP providers. Mode behaviour does not change.

## Decisions

1. **No `+`, mic, voice or lightning buttons.** There is no attachment/voice
   feature to back them (follow-ups below). The agent-profile picker stays as
   the leftmost pill (only rendered when profiles exist, as today).
2. **Pill order (Paseo):** agent profile · provider · model · thinking ·
   permission (+ ACP Auto-accept toggle) … right edge: ACP live config options,
   then the send cluster. Icons (lucide): `Bot` profile, `SquareTerminal`
   provider, `Cpu` model, `Brain` thinking, `ShieldCheck` permission
   (`ShieldAlert` + `text-warning` when it auto-approves).
3. **Radix `Select` stays** for provider/permission/thinking/model (keeps the
   `combobox` a11y, keyboard behaviour, aria-labels and test ids); only the
   trigger changes — leading icon + label + small chevron, chrome-less.
4. **Narrow widths (`< sm`, 640px):** label and chevron hide, trigger becomes a
   44×44 icon-only button (Item 21's touch target); `md:` and up the dense
   `h-7` sizing. Accessible names are the existing `aria-label`s, so icon-only
   is still labelled.
5. **Send cluster (ZCode):** idle → one icon button (`ArrowUp`, label "Send",
   disabled grey when empty). Running → `Stop` (`Square`, always shown), plus —
   only when the draft has text — `Send now` (`Zap`, interrupt) and `Queue`
   (`ArrowUp`, label "Queue"). Queue/interrupt semantics (ADR-0021 §9),
   Escape-to-stop and Cmd/Ctrl+Enter are unchanged.
6. **Status chips above the card (Paseo):** a "Working" chip (pulsing dot) while
   a run is live; the existing diff-stat chip (`+adds −dels`, compact `k`
   formatting, click opens Diff). Row renders nothing when neither applies.
   Queued-prompt rows stay above the chips' row (server state, unchanged).
7. **Focus hint (Paseo):** faint right-aligned `⌘L` / `Ctrl+L` text inside the
   card's top-right, visible only when the textarea is unfocused and empty;
   label comes from the *effective* binding of `composer-focus` (respects
   user rebinding; hidden if unbound).
8. **Placeholder:** unchanged strings from `composerPlaceholder()` — honest
   about what exists. Text area is 2 rows tall at rest, autogrows to
   `MAX_COMPOSER_HEIGHT`, then scrolls.

## Acceptance criteria

- **AC1 Card.** One `rounded-2xl` card; no divider between textarea and
  toolbar; textarea ≥ 2 rows at rest, grows to the cap then scrolls.
- **AC2 Pills.** Provider, model, thinking, permission (and profile when
  profiles exist) render as icon + label + chevron pills; every existing
  aria-label/test id and behaviour is preserved.
- **AC3 Warning tint.** The permission pill is warning-tinted (`ShieldAlert`)
  iff the effective mode `autoApproves` or `autoAccept` is on; otherwise normal.
- **AC4 Narrow.** Below `sm` pills collapse to icon-only 44px buttons without
  wrapping; `md:` and up are dense.
- **AC5 Send/Stop.** Send is an icon-only button disabled when empty; running
  state shows Stop always, Queue/Send-now with text; queue/interrupt/Escape
  behaviour unchanged.
- **AC6 Chips.** "Working" chip while running; diff-stat chip when there are
  changes and a Diff tab; nothing otherwise.
- **AC7 Focus.** `composer.focus` focuses the textarea; hint shows the
  effective combo only when unfocused ∧ empty ∧ bound.
- **AC8 Honest copy / no dead buttons.** Placeholder never mentions
  `@`/`/commands`/skills; no control without a working handler.
- **AC9 No regression.** All pre-existing composer tests pass (edited only for
  markup); `task test`, `task lint`, web typecheck/lint/test green.
- **AC10 Visual.** Light+dark screenshots at 1440×900 and 390×844 for: empty,
  typed multi-line, open model + permission pickers, running (stop + chips),
  queued prompt, bypass tint — compared side by side with the references.

## Test scenarios (named)

`run-config-toolbar.test.tsx` (new) / `composer.test.tsx`:

- `pills render an icon, the label and a chevron` (AC2)
- `pills keep their aria-labels and test ids` (AC2)
- `permission pill is warning-tinted for an autoApproves mode and plain otherwise` (AC3)
- `permission pill is warning-tinted while Auto-accept is on` (AC3)
- `pill labels and chevrons carry the below-sm hide classes and 44px touch size` (AC4)
- `composer card has no divider and the textarea is two rows` (AC1)
- `send is icon-only, labelled Send, disabled while empty` (AC5)
- `running shows Stop always, Queue and Send now only with text` (AC5)
- `Working chip shows while a run is live and hides when idle` (AC6)
- `diff-stat chip compact-formats thousands and opens the diff` (AC6)
- `composer.focus action focuses the textarea` (AC7)
- `focus hint shows the effective combo when empty and unfocused, hides on focus or text` (AC7)
- `focus hint follows a rebound shortcut and hides when unbound` (AC7)
- `placeholder does not advertise unsupported features` (AC8)

## Progress

- [x] Spec committed
- [ ] Step 1 — toolbar pills (`run-config-toolbar.tsx`)
- [ ] Step 2 — card shell, send cluster, chips, focus hint (`composer.tsx`,
      `prompt-textarea.tsx`, `composer-status-chips.tsx`)
- [ ] Step 3 — screenshot pass + fixes

## Peer log

_(filled in as Peers report)_

## Validation

_(filled in when every AC is confirmed)_

## Follow-ups (not in this PR — no backing feature)

- `+` attach button (needs an attachments feature; paste/drag image first).
- Mic / voice buttons (needs a dictation pipeline).
- Lightning/fast-mode toggle (needs a provider fast-mode setting).
- `@file` mentions and `/command` / skill menus in the prompt (then the
  placeholder may advertise them).
