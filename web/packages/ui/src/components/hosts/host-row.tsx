import { grantSummary, formatSas } from "@/components/hosts/grant-summary";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { FormError } from "@/components/ui/form-error";
import { StatusBadge } from "@/components/ui/status-badge";
import { Switch } from "@/components/ui/switch";
import { linkLabel, linkTone, type FleetHost } from "@/lib/fleet-types";
import { Icons } from "@/lib/icons";

/**
 * One paired host in Settings -> Hosts: alias and label, the link state (or, while pairing, where the
 * ceremony stands), both directions as plain on/off lines with the grant under the inbound one, the SAS
 * to compare while waiting, and the row's actions. The reverse switch only opens the parent's
 * confirmation when turned on; turning it off is immediate. No key, secret or address of the host is
 * ever rendered here -- the wire view (ADR-0048 section 8) carries none and this row shows none.
 */
export function HostRow({
  host,
  busy,
  error,
  onReview,
  onEditGrant,
  onRevoke,
  onRemove,
  onEnableReverse,
  onDisableReverse,
}: {
  host: FleetHost;
  /** A row action is in flight (its switch is locked); the confirms carry their own pending state. */
  busy: boolean;
  error: string | null;
  onReview: (host: FleetHost) => void;
  onEditGrant: (host: FleetHost) => void;
  onRevoke: (host: FleetHost) => void;
  onRemove: (host: FleetHost) => void;
  /** Turn the reverse direction on: opens the narrower-grant confirmation in the parent. */
  onEnableReverse: (host: FleetHost) => void;
  /** Turn the reverse direction off: immediate (it only closes this host's door). */
  onDisableReverse: (host: FleetHost) => void;
}) {
  const alias = host.alias;
  return (
    <li data-testid={`host-row-${alias}`} className="flex flex-col gap-2 rounded-lg px-2 py-1.5 hover:bg-hover">
      <div className="flex items-start gap-2">
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <span className="truncate text-ui-base font-medium text-foreground">{alias}</span>
            {host.label && <span className="shrink-0 truncate text-ui-sm text-muted-foreground">{host.label}</span>}
            {host.state === "pending" ? (
              <StatusBadge status="neutral" data-testid={`host-pending-${alias}`}>
                {host.pendingApproval ? "Awaiting your approval" : `Waiting for approval on ${alias}`}
              </StatusBadge>
            ) : host.state === "revoked" ? (
              <StatusBadge status="neutral" data-testid={`host-link-${alias}`}>
                Revoked
              </StatusBadge>
            ) : (
              <StatusBadge status={linkTone(host.link)} data-testid={`host-link-${alias}`}>
                {linkLabel(host.link)}
              </StatusBadge>
            )}
          </span>
          <p className="text-ui-sm text-muted-foreground">
            You can use {alias}: {host.outboundEnabled ? "on" : "off"}
          </p>
          <p className="text-ui-sm text-muted-foreground">
            {alias} can use this host: {host.inbound.enabled ? "on" : "off"}
          </p>
          {host.inbound.enabled && (
            <p className="text-ui-sm text-muted-foreground" data-testid={`host-grant-${alias}`}>
              {grantSummary(host.inbound.grant)}
            </p>
          )}
          {host.state === "pending" && host.sas && !host.pendingApproval && (
            <p className="text-ui-sm text-foreground" data-testid={`host-sas-${alias}`}>
              Confirm this code on {alias}: <span className="font-medium tabular-nums">{formatSas(host.sas)}</span>
            </p>
          )}
          {host.link === "refused" && (
            <Alert
              variant="error"
              testId={`host-refused-${alias}`}
              description={host.linkGuidance || host.linkCode || "The link was refused"}
            />
          )}
          {error && <FormError message={error} size="sm" testId={`host-row-error-${alias}`} />}
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {host.pendingApproval && (
            <Button type="button" size="sm" data-testid={`host-review-${alias}`} onClick={() => onReview(host)}>
              Review pairing
            </Button>
          )}
          {host.state === "approved" && (
            <label className="flex items-center gap-2 text-ui-sm text-foreground">
              <Switch
                aria-label={`Let ${alias} use this host`}
                data-testid={`host-inbound-${alias}`}
                checked={host.inbound.enabled}
                disabled={busy}
                onCheckedChange={(checked) => (checked ? onEnableReverse(host) : onDisableReverse(host))}
              />
              <span>Let {alias} use this host</span>
            </label>
          )}
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                type="button"
                variant="ghost"
                size="icon-xs"
                aria-label={`Actions for ${alias}`}
                data-testid={`host-menu-${alias}`}
                className="shrink-0"
              >
                <Icons.more aria-hidden />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              {host.state === "approved" && host.inbound.enabled && (
                <DropdownMenuItem data-testid={`host-edit-grant-${alias}`} onSelect={() => onEditGrant(host)}>
                  Edit access
                </DropdownMenuItem>
              )}
              {host.state === "approved" && (
                <DropdownMenuItem data-testid={`host-revoke-${alias}`} onSelect={() => onRevoke(host)}>
                  Revoke
                </DropdownMenuItem>
              )}
              <DropdownMenuItem data-testid={`host-remove-${alias}`} onSelect={() => onRemove(host)}>
                Remove
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>
    </li>
  );
}
