import { QueryClientContext } from "@tanstack/react-query";
import { useContext } from "react";

import { useInboxCount } from "@/hooks/use-inbox";
import { cn } from "@/lib/utils";

function Count({ className }: { className?: string }) {
  const { data } = useInboxCount();
  const unseen = data?.unseen ?? 0;
  if (unseen === 0) return null;
  return (
    <span
      role="img"
      data-testid="inbox-badge"
      aria-label={`${unseen} unseen inbox item${unseen === 1 ? "" : "s"}`}
      className={cn(
        "pointer-events-none flex h-4 min-w-4 items-center justify-center rounded-full bg-primary px-1 text-ui-xs leading-none font-medium text-primary-foreground",
        className,
      )}
    >
      {unseen > 99 ? "99+" : unseen}
    </span>
  );
}

/**
 * The sidebar's inbox count: the number of unseen items, with every pending permission always counted (ADR-0045).
 * Hosts rendered outside a daemon connection (tests) show nothing.
 */
export function InboxBadge({ className }: { className?: string }) {
  const queryClient = useContext(QueryClientContext);
  return queryClient ? <Count className={className} /> : null;
}
