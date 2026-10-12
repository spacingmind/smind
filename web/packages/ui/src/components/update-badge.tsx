import { Download } from "lucide-react";

import { pendingUpdate, type AppUpdateState } from "@/lib/app-update";
import { isDesktop } from "@/lib/platform";
import { cn } from "@/lib/utils";

/**
 * ZCode's "update available" badge (ADR-0023 U4 follow-up): a small green
 * download button in the header's left zone while an update waits for a
 * decision -- the prompt is open, or was dismissed with Later. Click puts
 * the prompt back. Renders nothing in a browser build, when up to date, and
 * while an install is running.
 */
export function UpdateBadge({
  state,
  onOpen,
  className,
}: {
  state: AppUpdateState;
  onOpen: () => void;
  className?: string;
}) {
  const info = isDesktop ? pendingUpdate(state) : null;
  if (!info) return null;
  const label = `Update available: smind v${info.version}`;
  return (
    <button
      type="button"
      data-testid="update-badge"
      title={label}
      aria-label={label}
      onClick={onOpen}
      className={cn(
        "inline-flex size-6 shrink-0 items-center justify-center rounded-md bg-success/15 text-success outline-none transition-colors hover:bg-success/25 focus-visible:ring-2 focus-visible:ring-ring",
        className,
      )}
    >
      <Download className="size-3.5" aria-hidden />
    </button>
  );
}
