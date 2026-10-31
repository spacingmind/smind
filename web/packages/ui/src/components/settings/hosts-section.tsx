import { useState } from "react";

import { AddHostDialog } from "@/components/hosts/add-host-dialog";
import { ApproveHostDialog } from "@/components/hosts/approve-host-dialog";
import { EditGrantDialog } from "@/components/hosts/edit-grant-dialog";
import { HostRow } from "@/components/hosts/host-row";
import { InviteHostDialog } from "@/components/hosts/invite-host-dialog";
import { registerSettingsSection, type SettingsSectionContext } from "@/components/settings/settings-registry";
import { SettingsEmptyCard } from "@/components/settings/settings-layout";
import { SettingsSectionTitle } from "@/components/settings/settings-title";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { LoadingRows } from "@/components/ui/loading-rows";
import { useConfirmAction } from "@/hooks/use-confirm-action";
import { useFleetHostActions, useFleetHosts } from "@/hooks/use-fleet-hosts";
import { useLastDefined } from "@/hooks/use-last-defined";
import { useWorkspaceTree } from "@/hooks/use-workspace-tree";
import { REVERSE_GRANT_COPY } from "@/lib/fleet-types";
import { Icons } from "@/lib/icons";

function messageOf(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/**
 * Settings -> Hosts (ADR-0048 AC11): every paired host with its link state and both directions. Turning
 * the reverse direction on is the one consequential toggle, so it confirms with the narrower default
 * grant spelled out first; turning it off, revoking and removing each say what they do before they act.
 * A daemon with fleet disabled answers method-not-found, which the hook reads as `off`: a quiet state
 * with no actions, not an error. Live through `fleet.hosts.changed` (the event bridge refetches the list).
 */
function HostsSection(_props: SettingsSectionContext) {
  const query = useFleetHosts();
  const actions = useFleetHostActions();
  const data = query.data ?? null;
  const hosts = data?.hosts ?? null;

  const [busy, setBusy] = useState<string | null>(null);
  const [rowError, setRowError] = useState<{ id: string; message: string } | null>(null);

  // --- pairing review: typed SAS + grant, kept mounted while it closes
  const [reviewId, setReviewId] = useState<string | null>(null);
  const reviewTarget = useLastDefined(hosts?.find((h) => h.hostId === reviewId) ?? null);
  const { workspaces: tree } = useWorkspaceTree();
  const workspaces = (tree ?? []).map((w) => ({ id: w.ID, title: w.Title }));

  // --- edit access: the grant of an approved, inbound-enabled host
  const [editId, setEditId] = useState<string | null>(null);
  const editTarget = useLastDefined(hosts?.find((h) => h.hostId === editId) ?? null);

  // --- invite / add
  const [inviteOpen, setInviteOpen] = useState(false);
  const [addOpen, setAddOpen] = useState(false);

  // --- reverse: turned on only behind the narrower-grant confirmation
  const [reverseId, setReverseId] = useState<string | null>(null);
  const reverseAction = useConfirmAction(reverseId !== null);
  const reverseTarget = useLastDefined(hosts?.find((h) => h.hostId === reverseId) ?? null);

  // --- revoke / remove
  const [revokeId, setRevokeId] = useState<string | null>(null);
  const revokeAction = useConfirmAction(revokeId !== null);
  const revokeTarget = useLastDefined(hosts?.find((h) => h.hostId === revokeId) ?? null);

  const [removeId, setRemoveId] = useState<string | null>(null);
  const removeAction = useConfirmAction(removeId !== null);
  const removeTarget = useLastDefined(hosts?.find((h) => h.hostId === removeId) ?? null);

  /** A row action with no confirmation of its own: turning the reverse direction off only closes this host's door. */
  async function quick(hostId: string, run: () => Promise<unknown>) {
    setBusy(hostId);
    setRowError(null);
    try {
      await run();
    } catch (err) {
      setRowError({ id: hostId, message: messageOf(err) });
    } finally {
      setBusy(null);
    }
  }

  const headerActions =
    data && !data.off ? (
      <div className="flex items-center gap-2">
        <Button type="button" size="sm" variant="outline" data-testid="hosts-add" onClick={() => setAddOpen(true)}>
          Add a host
        </Button>
        <Button type="button" size="sm" data-testid="hosts-invite" onClick={() => setInviteOpen(true)}>
          Invite a host
        </Button>
      </div>
    ) : undefined;

  return (
    <div className="flex flex-col gap-6" data-testid="settings-section-hosts">
      <section className="flex flex-col gap-2">
        <SettingsSectionTitle title="Hosts" actions={headerActions} />

        {!data ? (
          query.isError ? (
            <div className="flex items-center gap-2">
              <Alert variant="error" testId="hosts-load-error" description={messageOf(query.error)} />
              <Button type="button" variant="ghost" size="sm" data-testid="hosts-load-retry" onClick={() => void query.refetch()}>
                Retry
              </Button>
            </div>
          ) : (
            <LoadingRows />
          )
        ) : data.off ? (
          <SettingsEmptyCard
            testId="hosts-off"
            title="Fleet is off"
            description="Set fleet.enabled: true in config.yaml and restart smind to connect other hosts."
          />
        ) : data.hosts.length === 0 ? (
          <SettingsEmptyCard
            testId="hosts-empty"
            title="No hosts paired"
            description="Create an invite here, then run smind host add on the other host."
          />
        ) : (
          <ul className="flex flex-col gap-3" data-testid="hosts-list">
            {data.hosts.map((host) => (
              <HostRow
                key={host.hostId}
                host={host}
                busy={busy === host.hostId}
                error={rowError?.id === host.hostId ? rowError.message : null}
                onReview={(h) => setReviewId(h.hostId)}
                onEditGrant={(h) => setEditId(h.hostId)}
                onRevoke={(h) => {
                  revokeAction.reset();
                  setRevokeId(h.hostId);
                }}
                onRemove={(h) => {
                  removeAction.reset();
                  setRemoveId(h.hostId);
                }}
                onEnableReverse={(h) => {
                  reverseAction.reset();
                  setReverseId(h.hostId);
                }}
                onDisableReverse={(h) => void quick(h.hostId, () => actions.disableReverse(h.hostId))}
              />
            ))}
          </ul>
        )}
      </section>

      {reviewTarget && (
        <ApproveHostDialog
          host={reviewTarget}
          workspaces={workspaces}
          open={reviewId !== null}
          onOpenChange={(open) => {
            if (!open) setReviewId(null);
          }}
        />
      )}

      {editTarget && (
        <EditGrantDialog
          host={editTarget}
          workspaces={workspaces}
          open={editId !== null}
          onOpenChange={(open) => {
            if (!open) setEditId(null);
          }}
        />
      )}

      <InviteHostDialog open={inviteOpen} onOpenChange={setInviteOpen} />
      <AddHostDialog open={addOpen} onOpenChange={setAddOpen} />

      <ConfirmDialog
        open={reverseId !== null}
        onOpenChange={(open) => {
          if (open) return;
          setReverseId(null);
          reverseAction.reset();
        }}
        title={`Let ${reverseTarget?.alias ?? ""} use this host?`}
        description={<p data-testid="host-reverse-copy">{REVERSE_GRANT_COPY}</p>}
        confirmLabel="Turn on"
        pendingLabel="Turning on…"
        destructive={false}
        pending={reverseAction.pending}
        error={reverseAction.error}
        testId="host-reverse-dialog"
        onConfirm={() => {
          if (!reverseTarget) return;
          const hostId = reverseTarget.hostId;
          void reverseAction
            .run(() => actions.enableReverse(hostId))
            .then((done) => {
              if (done) setReverseId(null);
            });
        }}
      />

      <ConfirmDialog
        open={revokeId !== null}
        onOpenChange={(open) => {
          if (open) return;
          setRevokeId(null);
          revokeAction.reset();
        }}
        title={`Revoke ${revokeTarget?.alias ?? ""}?`}
        description="Its live connection closes now and it can no longer call this host. You can pair it again later."
        confirmLabel="Revoke"
        pendingLabel="Revoking…"
        pending={revokeAction.pending}
        error={revokeAction.error}
        testId="host-revoke-dialog"
        onConfirm={() => {
          if (!revokeTarget) return;
          const hostId = revokeTarget.hostId;
          void revokeAction
            .run(() => actions.revoke(hostId))
            .then((done) => {
              if (done) setRevokeId(null);
            });
        }}
      />

      <ConfirmDialog
        open={removeId !== null}
        onOpenChange={(open) => {
          if (open) return;
          setRemoveId(null);
          removeAction.reset();
        }}
        title={`Remove ${removeTarget?.alias ?? ""}?`}
        description="The pairing is deleted on this host. The other host keeps its record until you remove it there too."
        confirmLabel="Remove"
        pendingLabel="Removing…"
        pending={removeAction.pending}
        error={removeAction.error}
        testId="host-remove-dialog"
        onConfirm={() => {
          if (!removeTarget) return;
          const hostId = removeTarget.hostId;
          void removeAction
            .run(() => actions.remove(hostId))
            .then((done) => {
              if (done) setRemoveId(null);
            });
        }}
      />
    </div>
  );
}

registerSettingsSection({
  id: "hosts",
  label: "Hosts",
  description: "Other smind hosts paired with this one.",
  order: 140,
  icon: <Icons.host aria-hidden className="size-3.5" />,
  render: (ctx) => <HostsSection {...ctx} />,
});
