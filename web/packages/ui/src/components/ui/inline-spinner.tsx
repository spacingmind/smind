import { Loader2 } from "lucide-react";

import { cn } from "@/lib/utils";

/**
 * The shared inline loading indicator (ui-redesign-parity plan, Item 2):
 * refs/paseo/docs/design.md §11 -- "loading is inline by default", a
 * small spinner next to the thing it relates to, not a page-level
 * takeover. `label`, if given, is rendered as visible muted text next to
 * the spinner (the common case here -- "Loading runs…" etc.); omit it for
 * a bare spinner next to something that already has its own label (a
 * button's in-flight state, say).
 */
export function InlineSpinner({ label, className }: { label?: string; className?: string }) {
  return (
    <span className={cn("inline-flex items-center gap-1.5 text-sm text-foreground-muted", className)} role="status">
      <Loader2 className="size-3.5 shrink-0 animate-spin" aria-hidden="true" />
      {label}
    </span>
  );
}
