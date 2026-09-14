/**
 * The keyboard action vocabulary: the closed set of things a shortcut can
 * ask the app to do, and the focus scopes that decide whether it's allowed
 * to right now.
 *
 * An action id is the *contract between a binding and whoever performs it*.
 * Bindings (`keyboard/shortcuts.ts`) map key combos to action ids and know
 * nothing about React; handlers (`keyboard/keyboard-provider.tsx`'s
 * `useActionHandler`) map action ids to behavior and know nothing about
 * keys. That indirection is what lets Track B's composer claim
 * `composer.focus` and `run.interrupt`, or Track C's panes claim
 * `tab.close`, without any of them editing the binding table -- and what
 * lets a binding be rebound without touching a single handler.
 *
 * Modelled on `refs/paseo/packages/app/src/keyboard/actions.ts`, scaled
 * down: smind has no browser tab, no voice/dictation, and (today) no
 * multi-directional pane grid, so the ids below are the subset that maps
 * onto surfaces smind actually has.
 */

/** Every action a binding may target. Adding one means adding a binding *and* a handler somewhere. */
export type ActionId =
  | "palette.open"
  | "shortcuts.help"
  | "sidebar.toggle"
  | "theme.cycle"
  | "composer.focus"
  | "run.interrupt"
  | "tab.close"
  | "tab.jump"
  | "task.prev"
  | "task.next";

/**
 * Where keyboard focus is, as far as shortcut routing cares.
 *
 * `editable` and `terminal` are the two that gate most bindings: typing
 * `?` into the composer must insert a `?`, not open the shortcuts dialog,
 * and `Ctrl+B` inside xterm belongs to whatever's running in the shell.
 * `modal` means a dialog owns the keyboard outright.
 */
export type FocusScope = "editable" | "terminal" | "modal" | "other";

/** What a matched binding hands its handler. `null` for most; the digit for a `Digit`-wildcard binding. */
export type ActionPayload = { digit: number } | null;

/** A handler's signature: it receives the matched binding's payload and returns nothing. */
export type ActionHandler = (payload: ActionPayload) => void;
