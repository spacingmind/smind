import { useEffect, useMemo, useState, type KeyboardEvent } from "react";

import { MENTION_LISTBOX_ID, mentionOptionId } from "@/components/composer/file-mention-popover";
import { useTaskPaths } from "@/hooks/use-task-paths";
import { applyFileMentionReplacement, findActiveFileMention, rankPaths } from "@/lib/file-mention";

/**
 * The composer's `@` mention state: the active token at the caret, the ranked paths, the highlighted row and the key
 * rules. The path index is only fetched while a mention is open (ADR-0038 §6); `onAccept` receives the new text and
 * caret so the composer can apply both and remember the pick.
 */
export function useFileMention({
  text,
  caret,
  enabled,
  taskId,
  onAccept,
}: {
  text: string;
  caret: number;
  enabled: boolean;
  taskId: number | null;
  onAccept: (path: string, next: { text: string; cursor: number }) => void;
}) {
  const mention = useMemo(() => (enabled ? findActiveFileMention({ text, cursorIndex: caret }) : null), [enabled, text, caret]);
  // Esc closes the popover for this `@` only; typing a new one (or leaving this one) re-arms it.
  const [dismissedAt, setDismissedAt] = useState<number | null>(null);
  useEffect(() => {
    if (!mention && dismissedAt !== null) setDismissedAt(null);
  }, [mention, dismissedAt]);
  const open = mention !== null && dismissedAt !== mention.start;

  const { paths, loading } = useTaskPaths(taskId, open);
  const rows = useMemo(() => (open ? rankPaths(paths, mention.query) : []), [open, paths, mention?.query]);

  const [active, setActive] = useState(0);
  const query = mention?.query;
  useEffect(() => setActive(0), [query]);
  const activeIndex = Math.min(active, Math.max(rows.length - 1, 0));

  /** After a failed send put the text back: its trailing `@token` must not pop the list up again over the error. */
  function dismissForText(restored: string) {
    const m = findActiveFileMention({ text: restored, cursorIndex: caret });
    if (m) setDismissedAt(m.start);
  }

  function pick(path: string) {
    if (!mention) return;
    onAccept(path, applyFileMentionReplacement({ text, mention, path }));
  }

  /** Returns true when the popover consumed the key; everything else falls through to the composer. */
  function interceptKey(e: KeyboardEvent<HTMLTextAreaElement>): boolean {
    if (!open || !mention) return false;
    if (e.key === "Escape") {
      e.preventDefault();
      setDismissedAt(mention.start);
      return true;
    }
    if (rows.length === 0 || e.metaKey || e.ctrlKey || e.altKey) return false;
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
      pick(rows[activeIndex]!.path);
      return true;
    }
    return false;
  }

  return {
    open,
    rows,
    activeIndex,
    loading,
    setActive,
    pick,
    dismissForText,
    interceptKey,
    textareaProps: {
      role: "combobox",
      "aria-autocomplete": "list" as const,
      "aria-expanded": open,
      "aria-controls": open ? MENTION_LISTBOX_ID : undefined,
      "aria-activedescendant": open && rows.length > 0 ? mentionOptionId(activeIndex) : undefined,
    },
  };
}
