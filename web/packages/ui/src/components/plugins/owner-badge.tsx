import { StatusBadge } from "@/components/ui/status-badge";
import { Icons } from "@/lib/icons";
import { openPluginDetail } from "@/lib/plugin-focus";
import { cn } from "@/lib/utils";

/** The tooltip on a plugin-owned row's disabled controls and on its badge. */
export function ownedTooltip(owner: string): string {
  return `Owned by plugin ${owner}: manage it in Settings → Plugins`;
}

/**
 * "from <plugin>": the one marker for a row a plugin contributed (MCP server, agent, skill; ADR-0042, AC15).
 * Renders nothing for a row nobody owns. By default it is a button that opens that plugin's detail in Settings ->
 * Plugins; pass `link={false}` where it sits inside another interactive row (a picker option), where a nested
 * button would be invalid.
 */
export function OwnerBadge({
  owner,
  link = true,
  className,
  testId,
}: {
  owner: string | null | undefined;
  link?: boolean;
  className?: string;
  testId?: string;
}) {
  if (!owner) return null;
  const pill = (
    <StatusBadge status="neutral" className={cn("max-w-40 gap-1 px-1.5 py-0", className)} data-owner={owner}>
      <Icons.plugin aria-hidden className="size-3 shrink-0" />
      <span className="truncate">from {owner}</span>
    </StatusBadge>
  );
  if (!link)
    return (
      <span data-testid={testId} title={ownedTooltip(owner)} className="shrink-0">
        {pill}
      </span>
    );
  return (
    <button
      type="button"
      data-testid={testId}
      title={ownedTooltip(owner)}
      aria-label={`from plugin ${owner}: open its details in Settings → Plugins`}
      className="shrink-0 rounded-full outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
      onClick={(e) => {
        e.stopPropagation();
        openPluginDetail(owner);
      }}
    >
      {pill}
    </button>
  );
}
