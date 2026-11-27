import { useProjectHosts } from "@/hooks/use-project-hosts";
import { Icons } from "@/lib/icons";

/**
 * The "Add project" dialog's Where step (C1 D1). One host collapses to a chip
 * naming it; several list the current connection (selected), the other saved
 * connections (with where to switch them -- the dialog itself never switches
 * the app's connection) and the fleet hosts (disabled, with the reason). Every
 * row is a plain element on purpose: nothing in this step is clickable, so
 * nothing that looks live is dead.
 */
export function HostStep() {
  const hosts = useProjectHosts();

  if (hosts.single) {
    return (
      <p
        data-testid="add-project-host-chip"
        className="inline-flex items-center gap-1.5 rounded-md border bg-surface px-2 py-1 text-ui-sm text-foreground-muted"
      >
        On: {hosts.current.name}
      </p>
    );
  }

  return (
    <div data-testid="add-project-hosts" className="overflow-hidden rounded-xl border border-border bg-card">
      <div data-testid="add-project-host-current" className="flex items-center gap-2 px-3 py-2.5">
        <Icons.host className="size-4 shrink-0 text-foreground-muted" />
        <span className="min-w-0 truncate text-ui-base font-medium text-foreground">{hosts.current.name}</span>
        <span className="ml-auto shrink-0 text-ui-sm text-foreground-muted">This connection</span>
      </div>
      {hosts.saved.map((saved) => (
        <div
          key={saved.id}
          data-testid={`add-project-host-saved-${saved.id}`}
          className="flex items-center gap-2 border-t border-border px-3 py-2.5 text-foreground-muted"
        >
          <Icons.host className="size-4 shrink-0" />
          <span className="min-w-0 truncate text-ui-base">{saved.label}</span>
          <span className="ml-auto shrink-0 text-ui-sm">Switch the connection in Settings → Connections</span>
        </div>
      ))}
      {hosts.fleet.map((host) => (
        <div
          key={host.hostId}
          data-testid={`add-project-host-fleet-${host.hostId}`}
          aria-disabled="true"
          className="flex items-center gap-2 border-t border-border px-3 py-2.5 text-foreground-muted"
        >
          <Icons.host className="size-4 shrink-0" />
          <span className="min-w-0 truncate text-ui-base">{host.name}</span>
          <span className="ml-auto shrink-0 text-ui-sm">{host.reason}</span>
        </div>
      ))}
    </div>
  );
}
