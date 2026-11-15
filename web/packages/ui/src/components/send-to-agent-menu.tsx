import { useState } from "react";

import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Icons } from "@/lib/icons";

/** One thing the "Send to agent" menu can put into the focused chat's composer. `detail` is the size (and line range) shown up front. */
export type SendEntry = {
  id: string;
  label: string;
  detail?: string;
  disabled?: boolean;
  onSelect: () => void;
};

/**
 * The "Send to agent" affordance (DT3-C AC10): a small ghost button whose menu lists what a pane can
 * add to the agent's chat. Entries are recomputed by `getEntries` every time the menu *opens*, so the
 * shown sizes are read at that moment, never stale; opening alone queues nothing -- only choosing an
 * entry calls its `onSelect`.
 */
export function SendToAgentMenu({ getEntries, testId }: { getEntries: () => SendEntry[]; testId?: string }) {
  const [entries, setEntries] = useState<SendEntry[]>([]);

  return (
    <DropdownMenu
      onOpenChange={(open) => {
        if (open) setEntries(getEntries());
      }}
    >
      <DropdownMenuTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          aria-label="Send to agent"
          title="Send to agent"
          data-testid={testId ?? "send-to-agent"}
          className="max-sm:min-h-11"
        >
          <Icons.send aria-hidden />
          <span>Send to agent</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {entries.map((entry) => (
          <DropdownMenuItem
            key={entry.id}
            data-testid={`send-entry-${entry.id}`}
            disabled={entry.disabled}
            onSelect={() => entry.onSelect()}
          >
            <span className="min-w-0 flex-1 truncate">{entry.label}</span>
            {entry.detail !== undefined && (
              <span className="ml-3 shrink-0 text-ui-sm text-foreground-muted">{entry.detail}</span>
            )}
          </DropdownMenuItem>
        ))}
        <p role="presentation" className="px-2 pt-1 pb-0.5 text-ui-sm text-foreground-muted">
          Adds to the agent's chat. Nothing is sent until you send.
        </p>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
