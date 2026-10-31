import { formatSas, grantParts } from "@/components/hosts/grant-summary";
import { HostActivity } from "@/components/hosts/host-activity";
import { SasCode } from "@/components/hosts/sas-code";
import { SettingsGroup, SettingsRow } from "@/components/settings/settings-layout";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { FormError } from "@/components/ui/form-error";
import { StatusBadge } from "@/components/ui/status-badge";
import { Switch } from "@/components/ui/switch";
import { linkLabel, linkTone, type FleetHost } from "@/lib/fleet-types";
import { Icons } from "@/lib/icons";

/**
 * One paired host in Settings -> Hosts as a card of divided rows: a header (alias, label, the link state
 * or, while pairing, where the ceremony stands, and the actions), one row per direction -- the inbound one
 * carries the grant chips and, once approved, the reverse switch -- and, while pairing, the review button
 * and the SAS to compare. The reverse switch only opens the parent's confirmation when turned on; turning
 * it off is immediate. No key, secret or address of the host is ever rendered here -- the wire view
 * (ADR-0048 section 8) carries none and this row shows none.
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
    <li data-testid={`host-row-${alias}`}>
      <SettingsGroup>
        <SettingsRow
          testId={`host-header-${alias}`}
          className="flex-row items-center justify-between gap-3"
          label={
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
          }
          control={
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
          }
        />
        <SettingsRow
          testId={`host-dir-out-${alias}`}
          className="flex-row items-center justify-between gap-3"
          label={`This host → ${alias}`}
          description="Use its workspaces and agents from here"
          control={
            <StatusBadge status={host.outboundEnabled ? "success" : "neutral"} data-testid={`host-out-state-${alias}`}>
              {host.outboundEnabled ? "On" : "Off"}
            </StatusBadge>
          }
        />
        <SettingsRow
          testId={`host-dir-in-${alias}`}
          className="flex-row items-center justify-between gap-3"
          label={`${alias} → this host`}
          description={
            host.inbound.enabled ? (
              <span data-testid={`host-grant-${alias}`} className="mt-1 flex flex-wrap gap-1">
                {grantParts(host.inbound.grant).map((p) => (
                  <span key={p} className="whitespace-nowrap rounded-md bg-muted px-1.5 py-0.5 text-ui-xs text-foreground-muted">
                    {p}
                  </span>
                ))}
              </span>
            ) : undefined
          }
          control={
            host.state === "approved" ? (
              <Switch
                aria-label={`Let ${alias} use this host`}
                data-testid={`host-inbound-${alias}`}
                checked={host.inbound.enabled}
                disabled={busy}
                onCheckedChange={(checked) => (checked ? onEnableReverse(host) : onDisableReverse(host))}
              />
            ) : (
              <StatusBadge status={host.inbound.enabled ? "success" : "neutral"} data-testid={`host-in-state-${alias}`}>
                {host.inbound.enabled ? "On" : "Off"}
              </StatusBadge>
            )
          }
        />
        {host.pendingApproval && (
          <SettingsRow
            testId={`host-review-row-${alias}`}
            label="Approve this pairing"
            description="Compare the code, then approve or reject."
            control={
              <Button type="button" size="sm" data-testid={`host-review-${alias}`} onClick={() => onReview(host)}>
                Review pairing
              </Button>
            }
          />
        )}
        {host.state === "pending" && host.sas && !host.pendingApproval && (
          <SettingsRow
            testId={`host-sas-${alias}`}
            label={`Confirm this code on ${alias}`}
            description="Compare it with the code that host shows."
            control={
              <SasCode
                code={formatSas(host.sas)}
                digits={host.sas}
                testId={`host-sas-code-${alias}`}
                copyTestId={`host-sas-copy-${alias}`}
              />
            }
          />
        )}
        {(host.state === "approved" || host.state === "revoked") && <HostActivity host={host} />}
        {host.link === "refused" && (
          <div className="px-4 py-3">
            <Alert
              variant="error"
              testId={`host-refused-${alias}`}
              description={host.linkGuidance || host.linkCode || "The link was refused"}
            />
          </div>
        )}
        {error && (
          <div className="px-4 py-3">
            <FormError message={error} size="sm" testId={`host-row-error-${alias}`} />
          </div>
        )}
      </SettingsGroup>
    </li>
  );
}
