import { useState, type ReactNode } from "react";

import { SettingsEmptyCard, SettingsGroup, SettingsList, SettingsListRow, SettingsRow } from "@/components/settings/settings-layout";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { FormError } from "@/components/ui/form-error";
import { Input } from "@/components/ui/input";
import { LoadingRows } from "@/components/ui/loading-rows";
import { useRelayDeviceActions, useRelayDevices } from "@/hooks/use-relay-devices";
import { useViaRelay } from "@/hooks/use-via-relay";
import { daemonSentence } from "@/lib/daemon-sentence";
import { Icons } from "@/lib/icons";
import { deviceName, relayDeviceUnavailable, shortDeviceId, type RelayDevice, type RelayDeviceState } from "@/lib/relay-devices";
import { formatRelativeTime } from "@/lib/relative-time";

const STATE_LABEL: Record<RelayDeviceState, string> = {
  pending: "Pending",
  approved: "Approved",
  revoked: "Revoked",
};

function messageOf(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/**
 * The "Paired devices" group of Settings -> Daemon (B6 D1/D2): the relay-paired devices and the pairing window, from
 * the local-only `relay.device.*` RPCs. Over a relay session the call is refused ("forbidden") and the group becomes
 * the "manage them on the daemon's machine" copy -- with no call made at all when the connection kind is already known
 * to be relay; a daemon without the relay bridge says "Relay is not set up on this daemon." One list read on mount, one
 * after every mutation, one per Refresh click, never on a timer. The public key `device_pub` the wire carries is never
 * rendered or logged: a device is named by its label and its short device id only.
 */
export function RelayDevicesGroup() {
  const { viaRelay, ready } = useViaRelay();
  const query = useRelayDevices(ready && !viaRelay);
  const actions = useRelayDeviceActions();
  const data = query.data ?? null;

  const errorKind = query.isError ? relayDeviceUnavailable(query.error) : null;
  const readOnly = viaRelay || errorKind === "relay";

  const [confirming, setConfirming] = useState<{ kind: "approve" | "revoke"; device: RelayDevice } | null>(null);
  const [renaming, setRenaming] = useState<RelayDevice | null>(null);
  const [label, setLabel] = useState("");
  const [labelError, setLabelError] = useState<string | null>(null);

  function openConfirm(kind: "approve" | "revoke", device: RelayDevice) {
    (kind === "approve" ? actions.approve : actions.revoke).reset();
    setConfirming({ kind, device });
  }

  function openRename(device: RelayDevice) {
    actions.rename.reset();
    setRenaming(device);
    setLabel(device.label);
    setLabelError(null);
  }

  function saveRename() {
    if (!renaming) return;
    const trimmed = label.trim();
    if (trimmed === "") {
      setLabelError("Give the device a name.");
      return;
    }
    setLabelError(null);
    actions.rename.mutate({ id: renaming.device_id, label: trimmed }, { onSuccess: () => setRenaming(null) });
  }

  const renameError = labelError ?? (actions.rename.error ? daemonSentence(actions.rename.error.message) : null);
  const confirmPending = confirming?.kind === "approve" ? actions.approve.isPending : actions.revoke.isPending;
  const confirmError = confirming?.kind === "approve" ? actions.approve.error : actions.revoke.error;
  const confirmName = confirming ? deviceName(confirming.device) : "";

  let body: ReactNode;
  if (readOnly) {
    body = (
      <p data-testid="relay-devices-read-only" className="px-4 py-3 text-ui-base text-muted-foreground">
        Manage paired devices on the daemon&apos;s machine.
      </p>
    );
  } else if (errorKind === "no-bridge") {
    body = (
      <p data-testid="relay-devices-no-bridge" className="px-4 py-3 text-ui-base text-muted-foreground">
        Relay is not set up on this daemon.
      </p>
    );
  } else if (query.isError) {
    body = (
      <div className="px-4 py-3">
        <Alert variant="error" testId="relay-devices-error" description={daemonSentence(messageOf(query.error))} />
      </div>
    );
  } else if (!data) {
    body = <LoadingRows className="rounded-none border-0" />;
  } else {
    const devices = data.devices;
    body = (
      <>
        <SettingsRow
          label="Pairing window"
          control={
            data.window === null ? (
              <Button
                type="button"
                variant="ghost"
                size="sm"
                data-testid="relay-window-open"
                disabled={actions.openWindow.isPending}
                onClick={() => actions.openWindow.mutate()}
              >
                Open pairing window
              </Button>
            ) : (
              <span data-testid="relay-window-state" className="text-ui-sm text-foreground-muted">
                Open until {new Date(data.window.expires_at).toLocaleTimeString()}, {data.window.remaining} device(s) left
              </span>
            )
          }
        />
        {devices.length === 0 ? (
          <SettingsEmptyCard
            testId="relay-devices-empty"
            title="No devices are paired yet"
            description="Open a pairing window, then add this daemon on your phone."
          />
        ) : (
          <SettingsList data-testid="relay-devices-list" className="rounded-none border-x-0 border-b-0 border-t">
            {devices.map((d) => (
              <SettingsListRow key={d.device_id} data-testid={`relay-device-${d.device_id}`} className="flex flex-col gap-2">
                <div className="flex items-center justify-between gap-2">
                  <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                    <span className="flex items-center gap-2">
                      <span className="truncate text-ui-base font-medium text-foreground">{deviceName(d)}</span>
                      <span className="shrink-0 font-mono text-ui-sm text-foreground-muted">{shortDeviceId(d.device_id)}</span>
                      <span
                        data-testid={`relay-device-state-${d.device_id}`}
                        className="shrink-0 rounded-full border border-border px-1.5 text-ui-sm text-muted-foreground"
                      >
                        {STATE_LABEL[d.state]}
                      </span>
                    </span>
                    <span className="truncate text-ui-sm text-foreground-muted">{deviceMeta(d)}</span>
                  </div>
                  <div className="flex shrink-0 items-center gap-1">
                    {d.state === "pending" && (
                      <Button
                        type="button"
                        variant="quiet"
                        size="sm"
                        data-testid={`relay-device-approve-${d.device_id}`}
                        onClick={() => openConfirm("approve", d)}
                      >
                        Approve
                      </Button>
                    )}
                    {d.state === "approved" && (
                      <Button
                        type="button"
                        variant="quiet"
                        size="sm"
                        data-testid={`relay-device-revoke-${d.device_id}`}
                        onClick={() => openConfirm("revoke", d)}
                      >
                        Revoke
                      </Button>
                    )}
                    <Button
                      type="button"
                      variant="quiet"
                      size="sm"
                      data-testid={`relay-device-rename-${d.device_id}`}
                      onClick={() => openRename(d)}
                    >
                      Rename
                    </Button>
                  </div>
                </div>
              </SettingsListRow>
            ))}
          </SettingsList>
        )}
      </>
    );
  }

  return (
    <div className="flex flex-col gap-2" data-testid="relay-devices-group">
      <SettingsGroup
        title="Paired devices"
        description="Phones and other devices paired through the relay."
        actions={
          readOnly ? undefined : (
            <Button type="button" variant="ghost" size="sm" data-testid="relay-devices-refresh" onClick={() => void query.refetch()}>
              <Icons.refresh aria-hidden className={query.isFetching ? "size-3.5 animate-spin motion-reduce:animate-none" : "size-3.5"} /> Refresh
            </Button>
          )
        }
      >
        {body}
      </SettingsGroup>

      <ConfirmDialog
        open={confirming !== null}
        onOpenChange={(open) => {
          if (open) return;
          setConfirming(null);
        }}
        title={`${confirming?.kind === "approve" ? "Approve" : "Revoke"} ${confirmName}?`}
        description={
          confirming
            ? confirming.kind === "approve"
              ? `${confirmName} (${shortDeviceId(confirming.device.device_id)}) will be allowed to connect to this daemon.`
              : `${confirmName} (${shortDeviceId(confirming.device.device_id)}) will no longer be allowed to connect.`
            : ""
        }
        confirmLabel={confirming?.kind === "approve" ? "Approve" : "Revoke"}
        pendingLabel={confirming?.kind === "approve" ? "Approving…" : "Revoking…"}
        destructive={confirming?.kind !== "approve"}
        pending={confirmPending}
        error={confirmError ? daemonSentence(confirmError.message) : null}
        testId="relay-confirm-dialog"
        onConfirm={() => {
          if (!confirming) return;
          const id = confirming.device.device_id;
          if (confirming.kind === "approve") {
            actions.approve.mutate(id, { onSuccess: () => setConfirming(null) });
          } else {
            actions.revoke.mutate(id, { onSuccess: () => setConfirming(null) });
          }
        }}
      />

      <Dialog
        open={renaming !== null}
        onOpenChange={(open) => {
          if (!open) setRenaming(null);
        }}
      >
        <DialogContent data-testid="relay-rename-dialog">
          <DialogHeader className="text-left">
            <DialogTitle>Rename {renaming ? deviceName(renaming) : ""}</DialogTitle>
            <DialogDescription>The name this display shows; the device keeps its id.</DialogDescription>
          </DialogHeader>
          <Input
            data-testid="relay-rename-input"
            aria-label="Device name"
            value={label}
            autoComplete="off"
            onChange={(e) => setLabel(e.target.value)}
          />
          <FormError message={renameError} testId="relay-rename-error" />
          <DialogFooter>
            <Button type="button" variant="outline" size="sm" data-testid="relay-rename-cancel" onClick={() => setRenaming(null)}>
              Cancel
            </Button>
            <Button type="button" size="sm" data-testid="relay-rename-save" disabled={actions.rename.isPending} onClick={saveRename}>
              {actions.rename.isPending ? "Saving…" : "Save"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

/** The muted line under a device's name: its live connection, its last seen age, and push when the daemon knows it. */
function deviceMeta(d: RelayDevice): string {
  const parts: string[] = [];
  if (d.connected) parts.push("Connected now");
  parts.push(`Last seen ${formatRelativeTime(d.last_seen)}`);
  if (typeof d.push === "boolean") parts.push(d.push ? "Mobile push on" : "Mobile push off");
  return parts.join(" · ");
}
