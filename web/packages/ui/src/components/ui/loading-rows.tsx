import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

/**
 * The shared loading state for a list that has not arrived yet: a few skeleton rows in the list's own card
 * shell (so the layout does not jump when the data lands), announced as one polite status ("Loading…").
 * Replaces the bare "Loading…" paragraphs the Settings sections used.
 */
export function LoadingRows({ rows = 3, label = "Loading…", className }: { rows?: number; label?: string; className?: string }) {
  return (
    <div
      role="status"
      aria-busy="true"
      data-slot="loading-rows"
      className={cn("flex flex-col gap-2 rounded-xl border bg-card p-3", className)}
    >
      <span className="sr-only">{label}</span>
      {Array.from({ length: rows }, (_, i) => (
        <Skeleton key={i} aria-hidden className="h-9 w-full" />
      ))}
    </div>
  );
}
