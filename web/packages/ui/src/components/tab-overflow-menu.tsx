import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { TabLabel, type TabEntry } from "@/components/tab-registry";
import { Icons } from "@/lib/icons";
import { disambiguatedParts } from "@/lib/tab-scroll-edges";
import { cn } from "@/lib/utils";

/**
 * "More tabs": lists every tab of a pane when its strip overflows, so a tab scrolled out of sight is one click away
 * (the strip scrolls the chosen tab into view when it becomes active). Rendered only while the strip scrolls.
 */
export function TabOverflowMenu({
  tabs,
  activeKey,
  onActivate,
}: {
  tabs: readonly TabEntry[];
  activeKey: string | null;
  onActivate: (key: string) => void;
}) {
  // Several chats are all called "Chat": the repeats are numbered by the same function the strip's chips use.
  const parts = disambiguatedParts(tabs);
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label="More tabs"
          data-testid="tab-overflow-button"
          className="size-7 shrink-0 text-foreground-muted hover:text-foreground"
        >
          <Icons.chevronDown aria-hidden className="size-4" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" data-testid="tab-overflow-menu" className="max-h-80 min-w-48 max-w-72 overflow-y-auto">
        {tabs.map((entry) => (
          <DropdownMenuItem
            key={entry.key}
            data-testid="tab-overflow-item"
            data-tab-key={entry.key}
            aria-current={entry.key === activeKey ? "true" : undefined}
            className={cn("gap-1.5", entry.key === activeKey && "bg-selected font-medium")}
            onSelect={() => onActivate(entry.key)}
            // A long list opens at the top: bring the current tab into view so its marker is never off-screen.
            ref={(el) => {
              if (el && entry.key === activeKey) el.scrollIntoView?.({ block: "nearest" });
            }}
          >
            <TabLabel entry={entry} number={parts.get(entry.key)?.n} />
            {entry.key === activeKey && <Icons.check aria-hidden data-testid="tab-overflow-current" className="ml-auto size-3.5 shrink-0" />}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
