import { Gauge } from "lucide-react";

import { cn } from "@/lib/utils";

/** Anthropic's mark, drawn in `currentColor`. */
function ClaudeMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="currentColor" aria-hidden className={className}>
      <path d="M13.8 3h-3.6l6.2 18h3.6L13.8 3Zm-9.8 18h3.7L9.6 15.6 7.8 10.3 4 21Z" />
    </svg>
  );
}

/** OpenAI's mark (a six-petal knot), drawn in `currentColor`. */
function OpenAIMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.6} strokeLinejoin="round" aria-hidden className={className}>
      <path d="M12 3.2a4.3 4.3 0 0 1 4.2 5.3 4.3 4.3 0 0 1 2.1 7.9 4.3 4.3 0 0 1-6.3 3.4 4.3 4.3 0 0 1-6.3-3.4A4.3 4.3 0 0 1 7.8 8.5 4.3 4.3 0 0 1 12 3.2Z" />
      <path d="M12 3.2v6.4l5.5 3.2M7.8 8.5l5.5 3.2v6.4M18.3 16.4l-5.5-3.2L7.8 16" />
    </svg>
  );
}

/**
 * The provider's mark by `provider` id, or a generic gauge for anything
 * else (AC39: unknown providers never throw). Marks inherit `currentColor`.
 */
export function UsageSourceIcon({ provider, className }: { provider: string; className?: string }) {
  const cls = cn("size-4 shrink-0 text-muted-foreground", className);
  switch (provider) {
    case "anthropic":
      return <ClaudeMark className={cls} />;
    case "openai":
      return <OpenAIMark className={cls} />;
    default:
      return <Gauge aria-hidden className={cls} />;
  }
}
