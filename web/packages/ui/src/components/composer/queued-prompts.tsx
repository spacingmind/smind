import { Icons } from "@/lib/icons";

import { AttachmentChip } from "@/components/timeline/attachment-chip";
import { Button } from "@/components/ui/button";
import { useNow } from "@/hooks/use-now";
import { CHILD_EVENT_SOURCE, CHILD_UPDATES_LABEL, queueStartsLabel } from "@/lib/queue-hold";
import type { ChatQueueItem } from "@/lib/types";

/** "starts HH:MM" / "starts in 18s" for a held item; ticks each second only while mounted (held items only). */
function StartsLabel({ notBefore }: { notBefore: string }) {
  const now = useNow(1_000);
  const label = queueStartsLabel(notBefore, now);
  if (!label) return null;
  return (
    <span data-testid="queued-starts" className="shrink-0">
      {label}
    </span>
  );
}

/**
 * The daemon's per-chat prompt queue as rendered above the composer card
 * (ADR-0021 §9): one row per visible item, straight from useChatQueue's
 * server state -- never local. Queued rows show the prompt and a real
 * cancel button; an auto-cancelled row (its delivery failed server-side)
 * lingers for a few seconds as a "Not sent" notice with the server's
 * reason, and needs no affordance.
 */
export function QueuedPrompts({
  items,
  onCancel,
}: {
  items: ChatQueueItem[];
  onCancel: (itemId: number) => void;
}) {
  if (items.length === 0) return null;
  return (
    <ul data-testid="composer-queue" aria-label="Queued prompts" className="flex flex-col gap-1">
      {items.map((item) =>
        item.Status === "cancelled" ? (
          <li
            key={item.ID}
            role="status"
            data-testid="composer-queued-item"
            className="flex items-center gap-2 rounded-md bg-surface px-2 py-1 text-ui-sm text-foreground-muted"
          >
            <span className="shrink-0 uppercase text-destructive">Not sent</span>
            <span className="min-w-0 flex-1 truncate" title={item.Prompt}>
              {item.Prompt}
            </span>
            <span className="text-destructive">{item.CancelReason}</span>
          </li>
        ) : (
          <li
            key={item.ID}
            data-testid="composer-queued-item"
            className="flex items-center gap-2 rounded-md bg-surface px-2 py-1 text-ui-sm text-foreground-muted"
          >
            <span className="shrink-0 uppercase">{item.Priority > 0 ? "Next" : "Queued"}</span>
            {item.Source === CHILD_EVENT_SOURCE ? (
              <span className="flex min-w-0 flex-1 items-baseline gap-1.5">
                <span className="truncate">{CHILD_UPDATES_LABEL}</span>
                {item.NotBefore && <StartsLabel notBefore={item.NotBefore} />}
              </span>
            ) : (
              <span className="min-w-0 flex-1 truncate" title={item.Prompt}>
                {item.Prompt}
              </span>
            )}
            {item.Source !== CHILD_EVENT_SOURCE && item.NotBefore && <StartsLabel notBefore={item.NotBefore} />}
            {item.attachments && item.attachments.length > 0 && (
              <span data-testid="queued-attachments" className="flex shrink-0 items-center gap-1">
                {item.attachments.map((a) => (
                  <AttachmentChip key={a.id} attachment={a} compact />
                ))}
              </span>
            )}
            <Button
              type="button"
              variant="ghost"
              size="icon-xs"
              aria-label={`Cancel queued prompt: ${(item.Source === CHILD_EVENT_SOURCE ? CHILD_UPDATES_LABEL : item.Prompt).slice(0, 40)}`}
              onClick={() => onCancel(item.ID)}
            >
              <Icons.close aria-hidden className="size-3" />
            </Button>
          </li>
        ),
      )}
    </ul>
  );
}
