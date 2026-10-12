import { useEffect, useRef, useState, type ReactNode } from "react";

import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { useCanHover } from "@/hooks/use-can-hover";

const PILL_CLASS =
  "flex shrink-0 items-center gap-1 truncate rounded-full px-2 py-0.5 text-ui-sm text-foreground-muted transition-colors hover:bg-hover hover:text-foreground";

/** The pill's outline: kept for header pills, dropped inside the transcript (messages are borderless). */
const PILL_BORDER_CLASS = "border";

const OPEN_DELAY_MS = 200;
const CLOSE_DELAY_MS = 150;

/** How the card got opened decides what closes it and whether it takes focus. */
type OpenedBy = "hover" | "focus" | "click";

/**
 * A usage pill and the card it opens (ADR-0028 PR-E). One Popover everywhere,
 * so a pointer-capability change never swaps primitives under an open card:
 *
 * - hover-capable pointers: the card opens on pointer hover or keyboard focus
 *   after a short delay, stays open while the pointer is over the pill or the
 *   card, and closes on leave / blur; Escape closes it. Hover and focus opens
 *   never steal focus.
 * - a click or tap (any device; Enter/Space on the pill too) toggles it as the
 *   plain popover the pill always was: focus moves into the card, so a keyboard
 *   user can Tab to "Open Usage"; it stays until dismissed.
 * - `(hover: none)` touch devices have no hover, so a tap is the only way in.
 */
export function UsagePillCard({
  testId,
  label,
  pill,
  contentTestId,
  borderless = false,
  children,
}: {
  testId: string;
  /** The pill's accessible name. */
  label: string;
  /** What the pill shows (an icon and a one-line summary). */
  pill: ReactNode;
  contentTestId: string;
  /** Omit the pill outline (the run footer sits inside the borderless transcript). */
  borderless?: boolean;
  /** The card's content; `close` dismisses the card without handing focus back (the "Open Usage" link uses it). */
  children: (close: () => void) => ReactNode;
}) {
  const canHover = useCanHover();
  const [open, setOpen] = useState(false);
  const openedBy = useRef<OpenedBy>("click");
  const restoreFocus = useRef(true);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const clearTimer = () => {
    if (timer.current !== null) clearTimeout(timer.current);
    timer.current = null;
  };
  useEffect(() => clearTimer, []);

  const schedule = (by: OpenedBy, next: boolean) => {
    clearTimer();
    timer.current = setTimeout(
      () => {
        timer.current = null;
        if (next) {
          if (!open) openedBy.current = by;
          setOpen(true);
        } else if (openedBy.current !== "click") {
          setOpen(false);
        }
      },
      next ? OPEN_DELAY_MS : CLOSE_DELAY_MS,
    );
  };

  // The pointer that stopped hovering also stops being able to: nothing pending outlives the capability.
  useEffect(() => {
    if (!canHover) clearTimer();
  }, [canHover]);

  const close = () => {
    clearTimer();
    restoreFocus.current = false;
    setOpen(false);
  };
  const onOpenChange = (next: boolean) => {
    clearTimer();
    if (next) openedBy.current = "click";
    restoreFocus.current = true;
    setOpen(next);
  };
  const touch = (e: { pointerType: string }) => e.pointerType === "touch" || e.pointerType === "pen";

  return (
    <Popover open={open} onOpenChange={onOpenChange}>
      <PopoverTrigger
        type="button"
        data-testid={testId}
        aria-label={label}
        className={borderless ? PILL_CLASS : `${PILL_CLASS} ${PILL_BORDER_CLASS}`}
        onPointerEnter={(e) => canHover && !touch(e) && schedule("hover", true)}
        onPointerLeave={(e) => canHover && !touch(e) && schedule("hover", false)}
        onFocus={() => canHover && schedule("focus", true)}
        onBlur={() => {
          if (canHover && openedBy.current === "focus") schedule("focus", false);
        }}
      >
        {pill}
      </PopoverTrigger>
      <PopoverContent
        data-testid={contentTestId}
        align="start"
        className="w-64 p-3"
        onOpenAutoFocus={(e) => {
          if (openedBy.current !== "click") e.preventDefault();
        }}
        onCloseAutoFocus={(e) => {
          if (openedBy.current !== "click" || !restoreFocus.current) e.preventDefault();
        }}
        onPointerEnter={() => canHover && clearTimer()}
        onPointerLeave={(e) => canHover && !touch(e) && openedBy.current === "hover" && schedule("hover", false)}
      >
        {children(close)}
      </PopoverContent>
    </Popover>
  );
}
