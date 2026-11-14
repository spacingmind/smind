import { useState } from "react";

import { SettingsGroup } from "@/components/settings/settings-layout";
import { registerSettingsSection, type SettingsSectionContext } from "@/components/settings/settings-registry";
import { COMPACT_TOUCH_BUTTON_CLASS } from "@/components/permission/permission-option-button";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { FormError } from "@/components/ui/form-error";
import { LoadingRows } from "@/components/ui/loading-rows";
import { StatusBadge, type StatusBadgeStatus } from "@/components/ui/status-badge";
import { useConfirmAction } from "@/hooks/use-confirm-action";
import { useLastDefined } from "@/hooks/use-last-defined";
import { usePermissionGrants, useRevokeGrant } from "@/hooks/use-permission-grants";
import { grantClassLabel, grantRevokeCopy, grantStatusHint, grantStatusLabel } from "@/lib/grant-copy";
import { Icons } from "@/lib/icons";
import { formatRelativeTime } from "@/lib/relative-time";
import type { SavedGrant } from "@/lib/types";

const STATUS_TIER: Record<string, StatusBadgeStatus> = { active: "success", expired: "neutral", void: "warning", revoked: "neutral" };

/** "in 12d", "in 3h", or "expired" / "3d ago" for a time that has passed. */
function expiryText(g: SavedGrant): string {
  const at = new Date(g.expiresAt).getTime();
  if (Number.isNaN(at)) return g.expiresAt;
  const diff = at - Date.now();
  if (diff <= 0) return `Expired ${formatRelativeTime(g.expiresAt)}`;
  const days = Math.floor(diff / 86_400_000);
  if (days >= 1) return `Expires in ${days}d`;
  const hours = Math.max(1, Math.floor(diff / 3_600_000));
  return `Expires in ${hours}h`;
}

/**
 * Settings -> Saved grants (saved grants, docs/plans/active/saved-grants.md): every "always allow in this
 * project" decision, with the exact command, project, class, expiry and use count, and a Revoke for any
 * that is not already revoked. A grant is created only on a permission card, by a human; this page can
 * only read and revoke. Commands render as plain text.
 */
function SavedGrantsSection(_ctx: SettingsSectionContext) {
  const grants = usePermissionGrants();
  const revoke = useRevokeGrant();
  const [revokeId, setRevokeId] = useState<number | null>(null);
  const action = useConfirmAction(revokeId !== null);
  const target = useLastDefined(grants.data?.find((g) => g.id === revokeId) ?? null);

  return (
    <div className="flex min-w-0 flex-col gap-6" data-testid="settings-section-saved-grants">
      {grants.isPending ? (
        <LoadingRows />
      ) : grants.isError ? (
        <FormError message="Could not load the saved grants." testId="grants-error" />
      ) : (grants.data ?? []).length === 0 ? (
        <SettingsGroup title="Saved grants">
          <p className="px-3 py-3 text-ui-sm text-foreground-muted" data-testid="grants-empty">
            Nothing saved. When a permission card offers &quot;Always allow in this project&quot;, the commands you save appear here, and you can revoke them.
          </p>
        </SettingsGroup>
      ) : (
        <div className="flex min-w-0 flex-col gap-3" data-testid="grants-list">
          {(grants.data ?? []).map((g) => (
            <div key={g.id} data-testid={`grant-${g.id}`} data-grant-status={g.status} className="flex min-w-0 flex-col gap-2 rounded-xl border bg-card p-3">
              <div className="flex min-w-0 flex-wrap items-center gap-2">
                <StatusBadge status={STATUS_TIER[g.status] ?? "neutral"} data-testid={`grant-${g.id}-status`}>
                  {grantStatusLabel(g.status)}
                </StatusBadge>
                <StatusBadge status={g.class === "readonly" ? "neutral" : "warning"} data-testid={`grant-${g.id}-class`}>
                  {grantClassLabel(g.class)}
                </StatusBadge>
                <span className="text-ui-sm text-foreground-muted">{expiryText(g)}</span>
              </div>
              <div data-testid={`grant-${g.id}-command`} className="min-w-0 font-mono text-ui-sm whitespace-pre-wrap break-all text-foreground">
                {g.command}
              </div>
              <div className="min-w-0 text-ui-sm text-foreground-muted whitespace-pre-wrap break-all">{`Project folder: ${g.folder || "unknown"}`}</div>
              {g.class !== "readonly" && (
                <div className="text-ui-sm text-foreground-muted">{g.runnerOptIn ? "You allowed this to run the project's code (the agent can change it)." : "No project-code opt-in: this is never applied."}</div>
              )}
              <div className="text-ui-sm text-foreground-muted">
                {`Used ${g.useCount} ${g.useCount === 1 ? "time" : "times"}${g.lastUsedAt ? `, last ${formatRelativeTime(g.lastUsedAt)}` : ""}. Saved ${formatRelativeTime(g.createdAt)}.`}
              </div>
              {grantStatusHint(g) && <div className="text-ui-sm text-foreground-muted">{grantStatusHint(g)}</div>}
              {g.status !== "revoked" && (
                <div>
                  <Button type="button" variant="outline" size="sm" className={COMPACT_TOUCH_BUTTON_CLASS} data-testid={`grant-${g.id}-revoke`} onClick={() => setRevokeId(g.id)}>
                    Revoke
                  </Button>
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      <ConfirmDialog
        open={revokeId !== null}
        onOpenChange={(open) => {
          if (open) return;
          setRevokeId(null);
          action.reset();
        }}
        title="Revoke this grant?"
        description={<p data-testid="grant-revoke-copy">{target ? grantRevokeCopy(target) : ""}</p>}
        confirmLabel="Revoke"
        pendingLabel="Revoking…"
        pending={action.pending}
        error={action.error}
        confirmTestId="grant-revoke-confirm"
        cancelTestId="grant-revoke-cancel"
        errorTestId="grant-revoke-error"
        onConfirm={async () => {
          if (revokeId === null) return;
          if (await action.run(() => revoke(revokeId))) setRevokeId(null);
        }}
      />
    </div>
  );
}

registerSettingsSection({
  id: "saved-grants",
  label: "Saved grants",
  groupLabel: "Profiles & providers",
  description: "Commands you chose to always allow in a project. Revoke any of them.",
  order: 196,
  icon: <Icons.permission aria-hidden className="size-3.5" />,
  render: (ctx) => <SavedGrantsSection {...ctx} />,
});
