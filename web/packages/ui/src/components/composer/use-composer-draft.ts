import { useCallback, useRef, useState } from "react";

/**
 * localStorage key for one task's unsent composer text. Namespaced per
 * task id so switching tasks swaps drafts rather than sharing one buffer
 * (ui-redesign-parity Item 10's "draft persistence per task"), following
 * the `smind:` prefix hooks/use-sidebar-width.ts already established.
 */
export function draftStorageKey(taskId: number): string {
  return `smind:composer-draft:${taskId}`;
}

function readDraft(taskId: number | null): string {
  if (taskId === null || typeof window === "undefined") return "";
  try {
    return window.localStorage.getItem(draftStorageKey(taskId)) ?? "";
  } catch {
    // Storage can throw (private browsing, disabled storage) -- an
    // unreadable draft is an empty composer, not a crashed pane.
    return "";
  }
}

function writeDraft(taskId: number | null, value: string): void {
  if (taskId === null || typeof window === "undefined") return;
  try {
    if (value === "") window.localStorage.removeItem(draftStorageKey(taskId));
    else window.localStorage.setItem(draftStorageKey(taskId), value);
  } catch {
    // Best-effort persistence -- a write failure must not stop the text
    // from being typed and sent in this session.
  }
}

export interface ComposerDraft {
  value: string;
  setValue: (next: string) => void;
  /** Drops the draft entirely (submitted, so there's nothing left unsent). */
  clear: () => void;
}

/**
 * The composer's text for taskId, persisted to localStorage on every
 * keystroke so it survives both a task switch (this component stays
 * mounted and the prop changes) and a reload/remount (App.tsx keys the
 * tab strip by task id, so the pane really does remount).
 *
 * The taskId change is handled by the render-phase "adjust state when a
 * prop changes" pattern rather than an effect: reading the new task's
 * draft during the same render that saw the new id means the textarea
 * never paints one frame of the *previous* task's text, which an effect
 * would allow.
 */
export function useComposerDraft(taskId: number | null): ComposerDraft {
  const [value, setValueState] = useState<string>(() => readDraft(taskId));
  const lastTaskId = useRef<number | null>(taskId);

  if (lastTaskId.current !== taskId) {
    lastTaskId.current = taskId;
    setValueState(readDraft(taskId));
  }

  const setValue = useCallback(
    (next: string) => {
      setValueState(next);
      writeDraft(taskId, next);
    },
    [taskId],
  );

  const clear = useCallback(() => {
    setValueState("");
    writeDraft(taskId, "");
  }, [taskId]);

  return { value, setValue, clear };
}
