import { useEffect, useState } from "react";

import { GrantEditor } from "@/components/hosts/grant-editor";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { FormError } from "@/components/ui/form-error";
import { useFleetHostActions } from "@/hooks/use-fleet-hosts";
import { grantNeedsWriteWorkspace, type FleetGrant, type FleetHost } from "@/lib/fleet-types";

function messageOf(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/** Changing what an approved host may do here (ADR-0048 section 1); edits start from its current inbound grant and are dropped on close. */
export function EditGrantDialog({
  host,
  workspaces,
  open,
  onOpenChange,
}: {
  host: FleetHost;
  workspaces: { id: number; title: string }[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const actions = useFleetHostActions();
  const [grant, setGrant] = useState<FleetGrant>(host.inbound.grant);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (open) return;
    setError(null);
    setGrant(host.inbound.grant);
  }, [open, host.inbound.grant]);

  async function save() {
    setPending(true);
    setError(null);
    try {
      await actions.setGrant(host.hostId, grant);
      onOpenChange(false);
    } catch (err) {
      setError(messageOf(err));
    } finally {
      setPending(false);
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next && pending) return;
        onOpenChange(next);
      }}
    >
      <DialogContent data-testid="edit-grant-dialog">
        <DialogHeader className="text-left">
          <DialogTitle>{`Access for ${host.alias}`}</DialogTitle>
        </DialogHeader>
        <GrantEditor grant={grant} onChange={setGrant} workspaces={workspaces} disabled={pending} />
        <FormError message={error} testId="edit-grant-error" />
        <DialogFooter>
          <Button
            type="button"
            disabled={pending || grantNeedsWriteWorkspace(grant)}
            data-testid="edit-grant-save"
            onClick={() => void save()}
          >
            {pending ? "Saving…" : "Save"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
