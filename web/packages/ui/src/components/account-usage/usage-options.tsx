
import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { SegmentedControl } from "@/components/ui/segmented-control";
import type { UsageDisplayAs } from "@/lib/account-usage/format";
import { cn } from "@/lib/utils";
import { Icons } from "@/lib/icons";

const DISPLAY_AS_OPTIONS: { value: UsageDisplayAs; label: string }[] = [
  { value: "used", label: "Used" },
  { value: "remaining", label: "Remaining" },
];

/**
 * The shared usage preferences (Paseo's UsageOptions): "Show summary in
 * sidebar" (default off) and Percentages Used / Remaining. Opens from a
 * cog in the modal's title row, and from a visible labelled **Settings**
 * button in Settings -> Usage (`labelled`), where Paseo's cog was hard to
 * find. The Popover keeps to the viewport on narrow screens.
 */
export function UsageOptions({
  displayAs,
  onDisplayAs,
  inSidebar,
  onInSidebar,
  labelled = false,
}: {
  displayAs: UsageDisplayAs;
  onDisplayAs: (value: UsageDisplayAs) => void;
  inSidebar: boolean;
  onInSidebar: (on: boolean) => void;
  labelled?: boolean;
}) {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size={labelled ? "sm" : "icon-sm"}
          aria-label="Usage settings"
          data-testid="usage-options-menu"
          className={cn(!labelled && "text-muted-foreground")}
        >
          <Icons.settings aria-hidden className="size-3.5" />
          {labelled && "Settings"}
        </Button>
      </PopoverTrigger>
      <PopoverContent
        align="end"
        data-testid="usage-options-surface"
        className="flex w-[min(20rem,calc(100vw-2rem))] flex-col gap-3"
      >
        <div className="flex items-start justify-between gap-3">
          <div className="flex min-w-0 flex-col gap-0.5">
            <span className="text-ui-base text-foreground">Show summary in sidebar</span>
            <span className="text-ui-sm text-muted-foreground">Pin plan usage to the sidebar footer.</span>
          </div>
          <button
            type="button"
            role="switch"
            aria-checked={inSidebar}
            aria-label="Show summary in sidebar"
            data-testid="usage-show-in-sidebar"
            onClick={() => onInSidebar(!inSidebar)}
            className={cn(
              "h-5 w-9 shrink-0 rounded-full border border-input-border transition-colors",
              inSidebar ? "bg-primary" : "bg-transparent",
            )}
          >
            <span
              className={cn(
                "block size-3.5 rounded-full bg-background shadow transition-transform",
                inSidebar ? "translate-x-4" : "translate-x-0.5",
              )}
            />
          </button>
        </div>
        <div className="flex items-center justify-between gap-3">
          <span className="text-ui-base text-foreground">Percentages</span>
          <SegmentedControl
            label="Percentages"
            options={DISPLAY_AS_OPTIONS}
            value={displayAs}
            onChange={onDisplayAs}
          />
        </div>
      </PopoverContent>
    </Popover>
  );
}
