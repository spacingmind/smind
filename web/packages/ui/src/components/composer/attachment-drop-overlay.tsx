import { cn } from "@/lib/utils";

/** The "Drop to attach" label over the task pane while files are dragged in. Opacity-only: no layout shift. */
export function AttachmentDropOverlay({ active }: { active: boolean }) {
  return (
    <div
      aria-hidden={!active}
      data-testid="attachment-drop-overlay"
      data-active={active ? "true" : "false"}
      className={cn(
        "pointer-events-none absolute inset-0 z-20 flex items-center justify-center bg-background/70 transition-opacity duration-(--duration-menu)",
        active ? "opacity-100" : "opacity-0",
      )}
    >
      <span className="rounded-lg border border-input-border-focused bg-popover px-4 py-2 text-ui-base text-foreground shadow-md">Drop to attach</span>
    </div>
  );
}
