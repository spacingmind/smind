import { ForgeSettingsLink } from "@/components/git/forge-settings-link";
import { Icons } from "@/lib/icons";
import type { PRDetails } from "@/lib/types";

/** The one line under the header when the numbers are not fresh; `not_focused` is never shown. */
export function PRNotice({ details }: { details: Pick<PRDetails, "stale" | "refreshAvailableAt"> }) {
  const { stale, refreshAvailableAt } = details;
  let text = "";
  let settings = false;
  switch (stale) {
    case "budget": {
      const at = Date.parse(refreshAvailableAt);
      const when = Number.isNaN(at) ? "" : ` Next refresh ${new Date(at).toLocaleTimeString([], { hour: "numeric", minute: "2-digit" })}.`;
      text = `GitHub refresh limit reached; showing the last data.${when}`;
      break;
    }
    case "backoff":
    case "rate_limited":
      text = "GitHub is rate limiting smind; showing the last data.";
      break;
    case "unauthenticated":
      text = "Not signed in to GitHub on this machine.";
      settings = true;
      break;
    case "unavailable":
      text = "Couldn't reach GitHub; showing the last data.";
      break;
    default:
      return null;
  }
  return (
    <div
      role="status"
      data-testid="pr-notice"
      className="flex flex-wrap items-center gap-x-2 gap-y-1 border-b bg-surface px-4 py-1.5 text-ui-sm text-foreground-muted"
    >
      <Icons.info aria-hidden className="size-3.5 shrink-0" />
      <span className="min-w-0">{text}</span>
      {settings && <ForgeSettingsLink />}
    </div>
  );
}
