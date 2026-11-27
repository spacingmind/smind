import { useState } from "react";

import { SettingsEmptyCard, SettingsGroup, SettingsRow } from "@/components/settings/settings-layout";
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

/** "Expires in 12d" / "Expires in 3h" for a grant that is active; any other status shows its reason instead (a void, expired or revoked grant never shows a countdown that suggests it still works). */
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
 * Settings -> Saved grants (saved grants, docs/plans/completed/saved-grants.md): every "always allow in this
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
        <SettingsEmptyCard
          title="No saved grants"
          description="Nothing saved. When a permission card offers &quot;Always allow in this project&quot;, the commands you save appear here, and you can revoke them."
          testId="grants-empty"
        />
      ) : (
        <SettingsGroup testId="grants-list">
          {(grants.data ?? []).map((g) => (
            <SettingsRow
              key={g.id}
              testId={`grant-${g.id}`}
              label={
                <span data-testid={`grant-${g.id}-command`} className="min-w-0 font-mono text-ui-sm whitespace-pre-wrap break-all text-foreground">
                  {g.command}
                </span>
              }
              description={
                <>
                  <span className="flex flex-wrap items-center gap-2">
                    <StatusBadge status={STATUS_TIER[g.status] ?? "neutral"} data-testid={`grant-${g.id}-status`}>
                      {grantStatusLabel(g.status)}
                    </StatusBadge>
                    <StatusBadge status={g.class === "readonly" ? "neutral" : "warning"} data-testid={`grant-${g.id}-class`}>
                      {grantClassLabel(g.class)}
                    </StatusBadge>
                    {g.status === "active" && (
                      <span data-testid={`grant-${g.id}-expiry`} className="text-ui-sm text-foreground-muted">
                        {expiryText(g)}
                      </span>
                    )}
                  </span>
                  <span className="block whitespace-pre-wrap break-all">{`Project folder: ${g.folder || "unknown"}`}</span>
                  {g.class !== "readonly" && (
                    <span className="block">{g.runnerOptIn ? "You allowed this to run the project's code (the agent can change it)." : "No project-code opt-in: this is never applied."}</span>
                  )}
                  <span className="block">
                    {`Used ${g.useCount} ${g.useCount === 1 ? "time" : "times"}${g.lastUsedAt ? `, last ${formatRelativeTime(g.lastUsedAt)}` : ""}. Saved ${formatRelativeTime(g.createdAt)}.`}
                  </span>
                  {grantStatusHint(g) && <span className="block">{grantStatusHint(g)}</span>}
                </>
              }
              control={
                g.status !== "revoked" ? (
                  <Button type="button" variant="outline" size="sm" className={COMPACT_TOUCH_BUTTON_CLASS} data-testid={`grant-${g.id}-revoke`} onClick={() => setRevokeId(g.id)}>
                    Revoke
                  </Button>
                ) : undefined
              }
            />
          ))}
        </SettingsGroup>
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
  groupLabel: "Safety & usage",
  description: "Commands you chose to always allow in a project. Revoke any of them.",
  order: 191,
  icon: <Icons.permission aria-hidden className="size-3.5" />,
  render: (ctx) => <SavedGrantsSection {...ctx} />,
});
