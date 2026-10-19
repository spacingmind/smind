import { ProviderIcon } from "@/components/provider-icon";
import { ICON_SIZE } from "@/lib/icons";
import { cn } from "@/lib/utils";

/**
 * The provider's mark by `provider` id (either vocabulary), or a generic
 * gauge for anything else (AC39: unknown providers never throw). Marks
 * inherit `currentColor`.
 */
export function UsageSourceIcon({ provider, className }: { provider: string; className?: string }) {
  return <ProviderIcon provider={provider} testId={null} fallback="usage" className={cn(ICON_SIZE.md, "shrink-0 text-muted-foreground", className)} />;
}
