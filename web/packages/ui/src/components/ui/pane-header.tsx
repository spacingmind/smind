import type { ReactNode } from "react";

import { cn } from "@/lib/utils";

/**
 * The shared pane header bar (ui-redesign-parity plan, Item 2): a title
 * on the left, an optional right-aligned action slot, one bottom border,
 * one padding scale. Every pane used to hand-roll its own
 * `flex items-center justify-between border-b px-{3,4} py-{2,3}` div with
 * drifting padding (docs/research/uiux-audit.md §2.4) -- this is the one
 * implementation, adopted by task-detail, file-editor-pane,
 * diff-viewer-pane and terminal-pane (see docs/design.md's canonical-
 * surface table).
 */
export function PaneHeader({
  title,
  subtitle,
  actions,
  className,
  testId,
}: {
  /** The pane's title -- usually a short string, but a ReactNode so a pane can compose in a dirty-marker or similar inline element (see file-editor-pane.tsx). */
  title: ReactNode;
  /** An optional line under the title (task-detail's status/branch row). */
  subtitle?: ReactNode;
  /** The right-aligned action slot -- buttons, toggles, status text. */
  actions?: ReactNode;
  className?: string;
  testId?: string;
}) {
  return (
    <div
      data-testid={testId}
      data-slot="pane-header"
      className={cn("flex items-center justify-between gap-2 border-b px-4 py-2.5", className)}
    >
      <div className="min-w-0">
        <h2 className="truncate text-panel-title">{title}</h2>
        {subtitle && <div className="mt-0.5 flex items-center gap-2 text-xs text-foreground-muted">{subtitle}</div>}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
    </div>
  );
}
