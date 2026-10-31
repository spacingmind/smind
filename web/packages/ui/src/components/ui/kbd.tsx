import { cn } from "@/lib/utils";

/**
 * Renders a formatted combo as key caps.
 *
 * On mac `formatCombo` returns "⌘⌥T" (no separator, matching Apple's own
 * rendering); elsewhere "Ctrl+Alt+T". Splitting the mac form back into caps
 * would mean re-parsing glyphs, so mac gets one cap and the rest get one per
 * key -- which is what each platform's conventions look like anyway.
 */
export function Kbd({ keys, className }: { keys: string; className?: string }) {
  const parts = keys.includes("+") ? keys.split("+") : [keys];
  return (
    <span data-slot="kbd" className={cn("inline-flex items-center gap-1", className)}>
      {parts.map((part, i) => (
        <kbd
          key={`${part}-${i}`}
          className="rounded-md border border-border bg-muted px-1.5 py-0.5 font-mono text-ui-xs text-foreground-muted"
        >
          {part}
        </kbd>
      ))}
    </span>
  );
}
