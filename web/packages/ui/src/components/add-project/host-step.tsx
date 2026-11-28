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
    // One host: a compact read-only label (an icon, the friendly name, the address muted).
    return (
      <p
        data-testid="add-project-host-chip"
        className="inline-flex w-fit max-w-full items-center gap-1.5 justify-self-start rounded-full border border-border bg-muted px-2.5 py-1 text-ui-sm text-foreground"
      >
        <Icons.host aria-hidden className="size-3.5 shrink-0 text-foreground-muted" />
        <span className="min-w-0 truncate font-medium">{hosts.current.name}</span>
        {hosts.current.address && <span className="min-w-0 truncate text-foreground-muted">{hosts.current.address}</span>}
      </p>
    );
  }

  return (
    <div data-testid="add-project-hosts" className="overflow-hidden rounded-xl border border-border bg-card">
      <div
        data-testid="add-project-host-current"
        aria-current="true"
        className="flex items-center gap-2 bg-accent px-3 py-2.5"
      >
        <Icons.host className="size-4 shrink-0 text-foreground" />
        <span className="min-w-0 truncate text-ui-base font-medium text-foreground">{hosts.current.name}</span>
        {hosts.current.address && (
          <span className="min-w-0 truncate text-ui-sm text-foreground-muted">{hosts.current.address}</span>
        )}
        <span className="ml-auto inline-flex shrink-0 items-center gap-1 text-ui-sm font-medium text-foreground">
          <Icons.check aria-hidden className="size-3.5" />
          Selected
        </span>
      </div>
      {hosts.saved.map((saved) => (
        <div
          key={saved.id}
          data-testid={`add-project-host-saved-${saved.id}`}
          className="flex items-center gap-2 border-t border-border px-3 py-2.5 text-foreground-muted"
        >
          <Icons.host className="size-4 shrink-0" />
          <span className="min-w-0 truncate text-ui-base">{saved.label}</span>
          {saved.address && <span className="min-w-0 truncate text-ui-sm">{saved.address}</span>}
          <span className="ml-auto shrink-0 text-ui-sm">Switch it in Settings → Connections</span>
        </div>
      ))}
      {hosts.fleet.map((host) => (
        <div
          key={host.hostId}
          data-testid={`add-project-host-fleet-${host.hostId}`}
          aria-disabled="true"
          className="flex items-center gap-2 border-t border-border px-3 py-2.5 text-foreground-muted opacity-80"
        >
          <Icons.host className="size-4 shrink-0" />
          <span className="min-w-0 truncate text-ui-base">{host.name}</span>
          <span className="ml-auto max-w-[60%] text-right text-ui-sm">{host.reason}</span>
        </div>
      ))}
    </div>
  );
}
