import { useEffect, useRef } from "react";

/**
 * The input a tab's "Rename" swaps its trigger for (App.tsx's
 * DraggableTabTrigger, used for both terminal tabs and chat tabs). Its
 * own component/file rather than inline JSX so the focus-race fix below
 * is independently testable without needing a real ContextMenu/Radix
 * dismiss cycle in the test -- see tab-rename-input.test.tsx.
 */
export function TabRenameInput({
  title,
  onCommit,
  onCancel,
}: {
  /** The tab's current title -- the input's starting value. */
  title: string;
  /** Fires on blur, unless the edit was cancelled (Escape). Value is the input's current text, untrimmed -- the caller trims/no-ops on blank. */
  onCommit: (value: string) => void;
  /** Fires on Escape -- the caller exits edit mode without renaming. */
  onCancel: () => void;
}) {
  const inputRef = useRef<HTMLInputElement | null>(null);
  const cancelledRef = useRef(false);

  // Focusing takes an explicit effect, deferred one frame, rather than
  // the input's own `autoFocus`: whatever menu this input's trigger
  // click came from (App.tsx's ContextMenu, via Radix) still runs its
  // own dismiss-time focus restoration in the same commit this
  // component mounts in, and a same-tick `autoFocus` reliably loses
  // that race -- the browser observably refocuses whatever the menu
  // considers its trigger, and this input's own onBlur then reads that
  // as "the user clicked away," committing an unedited rename and
  // closing again before a real user can type anything. One
  // requestAnimationFrame is enough to land after that cleanup and win
  // the race regardless of what else grabs focus synchronously in
  // between (see this file's own test for the regression coverage --
  // jsdom can't reproduce Radix's real dismiss-focus timing, so that
  // test simulates the race directly instead: something else calling
  // .focus() synchronously right after this mounts).
  useEffect(() => {
    const raf = requestAnimationFrame(() => {
      inputRef.current?.focus();
      inputRef.current?.select();
    });
    return () => cancelAnimationFrame(raf);
  }, []);

  return (
    <input
      ref={inputRef}
      defaultValue={title}
      aria-label={`Rename ${title}`}
      data-testid="workspace-tab-rename-input"
      className="h-7 max-w-48 shrink-0 rounded border border-ring bg-transparent px-2 text-ui-base outline-none"
      onFocus={(e) => e.currentTarget.select()}
      onKeyDown={(e) => {
        e.stopPropagation();
        if (e.key === "Enter") {
          e.currentTarget.blur();
        } else if (e.key === "Escape") {
          cancelledRef.current = true;
          onCancel();
        }
      }}
      onBlur={(e) => {
        if (!cancelledRef.current) onCommit(e.currentTarget.value);
        cancelledRef.current = false;
      }}
    />
  );
}
