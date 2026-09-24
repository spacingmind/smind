import {
  forwardRef,
  useCallback,
  useImperativeHandle,
  useRef,
  useState,
  type KeyboardEvent as ReactKeyboardEvent,
} from "react";
import { ArrowDown, ArrowUp, ChevronDown, ChevronRight, X } from "lucide-react";

import { Button } from "@/components/ui/button";

/**
 * The one Find bar every pane (chat, file, terminal) renders -- same input,
 * match-count status, prev/next, close, and an optional replace row, per
 * `docs/plans/active/web-find.md`'s Decisions. Ported from Paseo's
 * `pane-find/index.tsx` behavior (same keys, same layout shape), rewritten
 * for plain React DOM + Tailwind instead of React Native + unistyles --
 * smind has no cross-platform text-input abstraction to route through.
 */

export interface FindBarReplace {
  value: string;
  onChange(value: string): void;
  onReplace(): void;
  onReplaceAll(): void;
}

export interface FindBarHandle {
  /** Focuses the query input and selects its current text, so typing replaces it -- what re-pressing the shortcut while already open should do. */
  focus(): void;
}

export interface FindBarProps {
  query: string;
  /** The "i/N" (or "No matches" / "Searching…") text shown beside the input. */
  status: string;
  /** Whether prev/next/replace are actionable -- false while there are no matches. */
  canNavigate: boolean;
  onQueryChange(query: string): void;
  onNext(): void;
  onPrevious(): void;
  onClose(): void;
  /** Present only for a surface with a replace command (the file editor); absent hides the whole row, including its toggle. */
  replace?: FindBarReplace;
}

/**
 * Enter = next, Shift+Enter = previous, Escape = close -- the one keyboard
 * contract every Find surface shares (`docs/plans/active/web-find.md`'s
 * Decisions). Shared between the query and replacement fields so Enter
 * inside either one still navigates instead of doing nothing.
 */
function useFindBarKeyDown(onClose: () => void, onNext: () => void, onPrevious: () => void) {
  return useCallback(
    (event: ReactKeyboardEvent<HTMLInputElement>) => {
      if (event.key === "Escape") {
        event.preventDefault();
        onClose();
      } else if (event.key === "Enter") {
        event.preventDefault();
        if (event.shiftKey) onPrevious();
        else onNext();
      }
    },
    [onClose, onNext, onPrevious],
  );
}

export const FindBar = forwardRef<FindBarHandle, FindBarProps>(function FindBar(
  { query, status, canNavigate, onQueryChange, onNext, onPrevious, onClose, replace },
  ref,
) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [replaceExpanded, setReplaceExpanded] = useState(false);

  useImperativeHandle(
    ref,
    () => ({
      focus() {
        const input = inputRef.current;
        if (!input) return;
        input.focus();
        input.select();
      },
    }),
    [],
  );

  const onKeyDown = useFindBarKeyDown(onClose, onNext, onPrevious);

  return (
    <div
      data-testid="find-bar"
      className="flex w-[340px] max-w-full flex-col gap-1 rounded-lg border bg-surface-1 p-1.5 shadow-md"
    >
      <div className="flex items-center gap-1">
        {replace && (
          <Button
            type="button"
            variant="ghost"
            size="icon-xs"
            aria-label={replaceExpanded ? "Hide replace" : "Show replace"}
            aria-expanded={replaceExpanded}
            data-testid="find-toggle-replace"
            onClick={() => setReplaceExpanded((expanded) => !expanded)}
          >
            {replaceExpanded ? <ChevronDown /> : <ChevronRight />}
          </Button>
        )}
        <div className="flex h-7 min-w-0 flex-1 items-center gap-2 rounded-md bg-surface-2 px-2">
          <input
            ref={inputRef}
            autoFocus
            value={query}
            onChange={(event) => onQueryChange(event.target.value)}
            onKeyDown={onKeyDown}
            placeholder="Find"
            aria-label="Find"
            autoCapitalize="off"
            autoCorrect="off"
            spellCheck={false}
            data-testid="find-input"
            className="h-full min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-foreground-muted"
          />
          <span
            className="shrink-0 text-xs text-foreground-muted"
            role="status"
            aria-live="polite"
            data-testid="find-status"
          >
            {status}
          </span>
        </div>
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          aria-label="Previous match"
          disabled={!canNavigate}
          onClick={onPrevious}
        >
          <ArrowUp />
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          aria-label="Next match"
          disabled={!canNavigate}
          onClick={onNext}
        >
          <ArrowDown />
        </Button>
        <Button type="button" variant="ghost" size="icon-xs" aria-label="Close find" onClick={onClose}>
          <X />
        </Button>
      </div>
      {replace && replaceExpanded && (
        <div className="flex items-center gap-1">
          {/* Empty gutter the width of the toggle button above, so the replacement field lines up under the query field rather than the toggle. */}
          <div className="w-6 shrink-0" />
          <input
            value={replace.value}
            onChange={(event) => replace.onChange(event.target.value)}
            onKeyDown={onKeyDown}
            placeholder="Replace"
            aria-label="Replace"
            autoCapitalize="off"
            autoCorrect="off"
            spellCheck={false}
            data-testid="find-replace-input"
            className="h-7 min-w-0 flex-1 rounded-md bg-surface-2 px-2 text-sm outline-none placeholder:text-foreground-muted"
          />
          <Button
            type="button"
            variant="ghost"
            size="xs"
            disabled={!canNavigate}
            data-testid="find-replace"
            onClick={replace.onReplace}
          >
            Replace
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="xs"
            disabled={!canNavigate}
            data-testid="find-replace-all"
            onClick={replace.onReplaceAll}
          >
            Replace all
          </Button>
        </div>
      )}
    </div>
  );
});
