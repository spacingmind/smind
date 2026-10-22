import { useId, type MouseEvent } from "react";

import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Icons } from "@/lib/icons";
import { cn } from "@/lib/utils";

/**
 * The icon button behind every "Add to chat" entry point (diff file header, hunk header, Files row). It reveals on
 * hover/focus of a `group` ancestor and stays visible below `md` (touch has no hover). A `disabledReason` keeps it in
 * place but inert, with the reason in the tooltip and for screen readers.
 */
export function AddToChatButton({
  label,
  tooltip = label,
  onAdd,
  disabledReason,
  className,
  testId,
}: {
  /** aria-label. */
  label: string;
  tooltip?: string;
  onAdd: () => void;
  disabledReason?: string;
  className?: string;
  testId?: string;
}) {
  const reasonId = useId();
  const stop = (e: MouseEvent) => e.stopPropagation();
  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          {/* A disabled button swallows pointer events: the span keeps the tooltip reachable. */}
          <span
            className="inline-flex shrink-0"
            onClick={stop}
            onKeyDown={(e) => e.stopPropagation()}
          >
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              className={cn(
                "rounded-md text-foreground-subtle opacity-0 group-hover:opacity-100 group-focus-within:opacity-100 focus-visible:opacity-100 max-md:opacity-100",
                className,
              )}
              aria-label={label}
              aria-describedby={disabledReason ? reasonId : undefined}
              disabled={disabledReason !== undefined}
              onClick={onAdd}
              data-testid={testId}
            >
              <Icons.mention aria-hidden="true" />
            </Button>
            {disabledReason && (
              <span id={reasonId} className="sr-only">
                {disabledReason}
              </span>
            )}
          </span>
        </TooltipTrigger>
        <TooltipContent>{disabledReason ?? tooltip}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
