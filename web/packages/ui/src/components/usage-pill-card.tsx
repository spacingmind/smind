import { useState, type ReactNode } from "react";

import { HoverCard, HoverCardContent, HoverCardTrigger } from "@/components/ui/hover-card";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { useCanHover } from "@/hooks/use-can-hover";

const PILL_CLASS =
  "flex shrink-0 items-center gap-1 truncate rounded-full border px-2 py-0.5 text-ui-sm text-foreground-muted transition-colors hover:bg-hover hover:text-foreground";

/**
 * A usage pill and the card it opens (ADR-0028 PR-E): a hover card where the
 * pointer can hover -- it opens on pointer hover or keyboard focus after a
 * short delay, stays open while the pointer is over it, and Escape closes
 * it -- and the existing tap-to-open popover on touch devices, with the same
 * content. A click opens the hover card too, so the pill still works as the
 * button it always was.
 */
export function UsagePillCard({
  testId,
  label,
  pill,
  contentTestId,
  children,
}: {
  testId: string;
  /** The pill's accessible name. */
  label: string;
  /** What the pill shows (an icon and a one-line summary). */
  pill: ReactNode;
  contentTestId: string;
  /** The card's content; `close` dismisses the card (the "Open Usage" link uses it). */
  children: (close: () => void) => ReactNode;
}) {
  const canHover = useCanHover();
  const [open, setOpen] = useState(false);
  const close = () => setOpen(false);
  const triggerProps = { "data-testid": testId, "aria-label": label, className: PILL_CLASS, children: pill } as const;
  const contentProps = { "data-testid": contentTestId, align: "start", className: "w-64 p-3" } as const;

  if (!canHover) {
    return (
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger type="button" {...triggerProps} />
        <PopoverContent {...contentProps}>{children(close)}</PopoverContent>
      </Popover>
    );
  }
  return (
    <HoverCard open={open} onOpenChange={setOpen} openDelay={200} closeDelay={150}>
      <HoverCardTrigger asChild>
        <button type="button" {...triggerProps} onClick={() => setOpen(true)} />
      </HoverCardTrigger>
      <HoverCardContent {...contentProps}>{children(close)}</HoverCardContent>
    </HoverCard>
  );
}
