import { cn } from "@/lib/utils";

/**
 * The "Drop to attach" cue over the task pane while files are dragged in (ZCode's drop-zone language): a barely
 * tinted layer with a dashed outline inset from the pane's edge and a small label chip. It must not wash the content
 * out: the pane stays readable under it, since the user is looking for where to drop. Opacity-only (no layout shift);
 * it never takes pointer events, so the drop still lands on the pane.
 */
export function AttachmentDropOverlay({ active }: { active: boolean }) {
  return (
    <div
      aria-hidden={!active}
      data-testid="attachment-drop-overlay"
      data-active={active ? "true" : "false"}
      className={cn(
        "pointer-events-none absolute inset-0 z-20 flex items-center justify-center bg-primary/[0.04] transition-opacity duration-(--duration-menu)",
        active ? "opacity-100" : "opacity-0",
      )}
    >
      <div
        data-testid="attachment-drop-outline"
        className="absolute inset-2 rounded-2xl border-2 border-dashed border-input-border-focused"
      />
      <span className="relative rounded-lg border border-input-border-focused bg-popover px-4 py-2 text-ui-base text-foreground shadow-md">
        Drop to attach
      </span>
    </div>
  );
}
