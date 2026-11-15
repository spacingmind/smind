import type { ActionId } from "@/keyboard/actions";
import { useKeyboard } from "@/keyboard/keyboard-provider";
import { formatChord } from "@/keyboard/shortcut-string";

/**
 * The live shortcut for `action`, formatted for this platform ("⌘B"), or null when the action has none bound. It reads
 * the resolved bindings, so a shortcut the user customised or removed in Settings is shown (or dropped) as such.
 */
export function useShortcutLabel(action: ActionId | undefined): string | null {
  const { bindings, isMac } = useKeyboard();
  const combo = action === undefined ? undefined : bindings.find((b) => b.action === action)?.effectiveCombo;
  return combo ? formatChord(combo, isMac) : null;
}

/** "Settings" and "⌘," -> "Settings (⌘,)"; without a shortcut the bare label. */
export function withShortcut(label: string, shortcut: string | null): string {
  return shortcut ? `${label} (${shortcut})` : label;
}
