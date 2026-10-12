import { useLayoutEffect, useRef, useState, type ComponentPropsWithoutRef } from "react";

import { cn } from "@/lib/utils";

/**
 * The mask ZCode's TaskTitleOverflowText applies (components/TaskTitleOverflowText.tsx):
 * the last 1.5rem fades to transparent. `black` here is the mask's alpha
 * channel, not a painted colour.
 */
const OVERFLOW_FADE =
  "[mask-image:linear-gradient(to_right,black_calc(100%_-_1.5rem),transparent)] [-webkit-mask-image:linear-gradient(to_right,black_calc(100%_-_1.5rem),transparent)]";

/**
 * Single-line text that fades out at its right edge when it does not fit,
 * instead of ending in a hard ellipsis. The fade is only applied while the
 * text actually overflows -- measured, and re-measured on resize -- so a title
 * that fits is never dimmed. (ZCode also scrolls the overflowing title as a
 * marquee on hover; that is deliberately not ported.)
 *
 * The element is a flex item that may shrink (`min-w-0 flex-1`), which is what
 * gives it a width to overflow against.
 */
export function FadeText({ className, children, ...props }: ComponentPropsWithoutRef<"span">) {
  const ref = useRef<HTMLSpanElement>(null);
  const [overflowing, setOverflowing] = useState(false);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const measure = () => setOverflowing(el.scrollWidth > el.clientWidth);
    measure();
    if (typeof ResizeObserver === "undefined") {
      window.addEventListener("resize", measure);
      return () => window.removeEventListener("resize", measure);
    }
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, [children]);

  return (
    <span
      ref={ref}
      data-overflowing={overflowing ? "true" : undefined}
      className={cn("min-w-0 flex-1 overflow-hidden whitespace-nowrap", overflowing && OVERFLOW_FADE, className)}
      {...props}
    >
      {children}
    </span>
  );
}
