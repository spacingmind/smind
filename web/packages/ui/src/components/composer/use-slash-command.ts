import { useEffect, useMemo, useState, type KeyboardEvent } from "react";

import { SLASH_LISTBOX_ID, slashOptionId } from "@/components/composer/slash-command-popover";
import { usePluginCommands } from "@/hooks/use-plugin-contributions";
import {
  applySlashReplacement,
  buildSlashCommands,
  findActiveSlash,
  rankSlashCommands,
  BUILTIN_SLASH_COMMANDS,
  type BuiltinSlashCommand,
  type SlashCommand,
} from "@/lib/slash-commands";

/**
 * The composer's `/` command state, shaped like `useFileMention`: the active token at the caret, the ranked rows,
 * the highlighted row and the key rules. Picking a row only reports the new text and caret to `onAccept`; nothing
 * here can send. With no commands (no plugin commands and no built-ins) or no match the list is not open, so the
 * composer behaves exactly as it did before commands existed.
 */
export function useSlashCommand({
  text,
  caret,
  enabled,
  onAccept,
  builtins = BUILTIN_SLASH_COMMANDS,
}: {
  text: string;
  caret: number;
  enabled: boolean;
  onAccept: (command: SlashCommand, next: { text: string; cursor: number }) => void;
  builtins?: readonly BuiltinSlashCommand[];
}) {
  const pluginCommands = usePluginCommands();
  const all = useMemo(() => buildSlashCommands(builtins, pluginCommands), [builtins, pluginCommands]);
  const slash = useMemo(() => (enabled && all.length > 0 ? findActiveSlash({ text, cursorIndex: caret }) : null), [enabled, all, text, caret]);
  const rows = useMemo(() => (slash ? rankSlashCommands(all, slash.query) : []), [all, slash]);
  // Esc closes the list for this token only; typing a new `/` (or leaving it) re-arms it.
  const [dismissed, setDismissed] = useState(false);
  useEffect(() => {
    if (!slash && dismissed) setDismissed(false);
  }, [slash, dismissed]);
  const open = slash !== null && rows.length > 0 && !dismissed;

  const [active, setActive] = useState(0);
  const query = slash?.query;
  useEffect(() => setActive(0), [query]);
  const activeIndex = Math.min(active, Math.max(rows.length - 1, 0));

  function pick(command: SlashCommand) {
    if (!slash) return;
    onAccept(command, applySlashReplacement({ text, slash, command }));
  }

  /** Returns true when the list consumed the key; everything else falls through to the composer. */
  function interceptKey(e: KeyboardEvent<HTMLTextAreaElement>): boolean {
    if (!open) return false;
    if (e.key === "Escape") {
      e.preventDefault();
      setDismissed(true);
      return true;
    }
    if (e.metaKey || e.ctrlKey || e.altKey) return false;
    const composing = (e.nativeEvent as unknown as { isComposing?: boolean }).isComposing;
    if (composing) return false;
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      const step = e.key === "ArrowDown" ? 1 : -1;
      setActive((activeIndex + step + rows.length) % rows.length);
      return true;
    }
    if ((e.key === "Enter" && !e.shiftKey) || (e.key === "Tab" && !e.shiftKey)) {
      e.preventDefault();
      pick(rows[activeIndex]!);
      return true;
    }
    return false;
  }

  return {
    open,
    rows,
    activeIndex,
    setActive,
    pick,
    interceptKey,
    textareaProps: {
      role: "combobox",
      "aria-autocomplete": "list" as const,
      "aria-expanded": open,
      "aria-controls": open ? SLASH_LISTBOX_ID : undefined,
      "aria-activedescendant": open ? slashOptionId(activeIndex) : undefined,
    },
  };
}
