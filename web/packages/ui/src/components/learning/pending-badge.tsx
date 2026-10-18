import { QueryClientContext } from "@tanstack/react-query";
import { useContext } from "react";

import { usePendingProposalCount } from "@/hooks/use-learning";
import { cn } from "@/lib/utils";

function Count({ className }: { className?: string }) {
  const count = usePendingProposalCount();
  if (count === 0) return null;
  return (
    <span
      role="img"
      data-testid="learning-pending-badge"
      aria-label={`${count} pending skill proposal${count === 1 ? "" : "s"}`}
      className={cn("size-1.5 shrink-0 rounded-full bg-foreground", className)}
    />
  );
}

/**
 * A quiet dot when any workspace has skill proposals waiting (ADR-0026 AC20). It reads the shared
 * status entry, so it costs nothing extra; hosts rendered outside a daemon connection (tests,
 * storybook-style shells) simply show nothing.
 */
export function PendingProposalBadge({ className }: { className?: string }) {
  const queryClient = useContext(QueryClientContext);
  return queryClient ? <Count className={className} /> : null;
}
