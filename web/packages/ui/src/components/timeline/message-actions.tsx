import { useEffect, useRef, useState, type ComponentProps, type ReactNode } from "react";
import { Check, Copy } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

/**
 * Tailwind needs the full class strings at build time, so each named hover
 * group is spelled out rather than interpolated.
 */
const REVEAL_BY_GROUP = {
  prompt: "group-hover/prompt:opacity-100",
  run: "group-hover/answer:opacity-100",
} as const;

/**
 * The per-message action bar (ZCode `MessageActions`): hidden until the row
 * is hovered or something inside it has focus, always visible on touch.
 * Hidden by opacity only, so the buttons stay in the tab order and focusing
 * one reveals the bar.
 */
export function MessageActions({
  group,
  className,
  children,
  ...props
}: { group: keyof typeof REVEAL_BY_GROUP } & ComponentProps<"div">) {
  return (
    <TooltipProvider>
      <div
        className={cn(
          "flex items-center gap-0.5 opacity-0 transition-opacity has-[:focus-visible]:opacity-100 has-[[aria-expanded=true]]:opacity-100 [@media(hover:none)]:opacity-100",
          REVEAL_BY_GROUP[group],
          className,
        )}
        {...props}
      >
        {children}
      </div>
    </TooltipProvider>
  );
}

export const MESSAGE_ACTION_CLASS = "text-foreground-muted hover:text-foreground";

/** Icon-only ghost button with a tooltip; `label` is both the tooltip and the accessible name. */
export function MessageAction({
  label,
  icon,
  className,
  ...props
}: { label: string; icon: ReactNode } & Omit<ComponentProps<typeof Button>, "children" | "aria-label">) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          aria-label={label}
          className={cn(MESSAGE_ACTION_CLASS, className)}
          {...props}
        >
          {icon}
        </Button>
      </TooltipTrigger>
      <TooltipContent side="bottom">{label}</TooltipContent>
    </Tooltip>
  );
}

/**
 * Writes `getText()` to the clipboard and shows a check for ~1.5 s. The
 * clipboard can be absent (no secure context, jsdom) or rejected; either way
 * the button stays silent rather than throwing into the transcript.
 */
export function useCopyFeedback(): { copied: boolean; copy: (text: string) => Promise<void> } {
  const [copied, setCopied] = useState(false);
  const timer = useRef<number>(undefined);
  useEffect(() => () => window.clearTimeout(timer.current), []);

  async function copy(text: string) {
    // No clipboard (a non-secure origin such as plain http on a LAN address) means nothing was
    // copied, so no confirmation either.
    if (!navigator.clipboard?.writeText) return;
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      window.clearTimeout(timer.current);
      timer.current = window.setTimeout(() => setCopied(false), 1500);
    } catch {
      // Nothing useful to say; the text is still selectable by hand.
    }
  }
  return { copied, copy };
}

export function CopyAction({
  getText,
  label = "Copy",
  ...props
}: { getText: () => string; label?: string } & Omit<ComponentProps<typeof Button>, "children" | "aria-label" | "onClick">) {
  const { copied, copy } = useCopyFeedback();
  return (
    <MessageAction
      label={copied ? "Copied" : label}
      icon={copied ? <Check aria-hidden /> : <Copy aria-hidden />}
      onClick={() => void copy(getText())}
      {...props}
    />
  );
}
