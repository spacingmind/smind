import { useEffect, useState } from "react";

import { GrantEditor } from "@/components/hosts/grant-editor";
import { formatSas } from "@/components/hosts/grant-summary";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { FormError } from "@/components/ui/form-error";
import { Input } from "@/components/ui/input";
import { useFleetHostActions } from "@/hooks/use-fleet-hosts";
import { grantNeedsWriteWorkspace, isValidSas, normalizeSas, type FleetGrant, type FleetHost } from "@/lib/fleet-types";

function messageOf(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/**
 * Approving a pairing (ADR-0048 S9): the human compares the code the other host shows with this one and
 * TYPES it. Approve stays disabled until the typed digits equal the pending SAS, so a pairing someone else
 * started cannot be approved by a reflex click; nothing here fills the field or enables Approve for the
 * user. The grant starts from the host's proposed inbound grant and is edited before it is sent.
 */
export function ApproveHostDialog({
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
  const [typed, setTyped] = useState("");
  const [grant, setGrant] = useState<FleetGrant>(host.inbound.grant);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Whatever closes the dialog, the typed code and edits do not survive into the next review.
  useEffect(() => {
    if (open) return;
    setTyped("");
    setError(null);
    setGrant(host.inbound.grant);
  }, [open, host.inbound.grant]);

  const canApprove =
    !!host.sas && isValidSas(typed) && normalizeSas(typed) === host.sas && !grantNeedsWriteWorkspace(grant) && !pending;

  async function run(action: () => Promise<unknown>) {
    setPending(true);
    setError(null);
    try {
      await action();
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
      <DialogContent data-testid="approve-host-dialog">
        <DialogHeader className="text-left">
          <DialogTitle>{`Approve ${host.alias}?`}</DialogTitle>
          <DialogDescription asChild>
            <div className="flex flex-col gap-3">
              {host.sas ? (
                <>
                  <p
                    data-testid="approve-sas"
                    className="text-center font-mono text-3xl font-semibold tracking-widest tabular-nums text-foreground"
                  >
                    {formatSas(host.sas)}
                  </p>
                  <p>
                    Check that {host.alias} shows the same code. Only approve if it matches and you started this pairing.
                  </p>
                </>
              ) : (
                <p data-testid="approve-no-sas">No pairing code is available. Reject and pair again.</p>
              )}
            </div>
          </DialogDescription>
        </DialogHeader>
        <Input
          data-testid="approve-sas-input"
          aria-label={`Type the code shown on ${host.alias}`}
          placeholder="Type the code"
          inputMode="numeric"
          autoComplete="off"
          disabled={pending}
          value={typed}
          onChange={(e) => setTyped(e.target.value)}
        />
        <GrantEditor grant={grant} onChange={setGrant} workspaces={workspaces} disabled={pending} />
        <FormError message={error} testId="approve-error" />
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            disabled={pending}
            data-testid="approve-reject"
            onClick={() => void run(() => actions.reject(host.hostId))}
          >
            Reject
          </Button>
          <Button
            type="button"
            disabled={!canApprove}
            data-testid="approve-confirm"
            onClick={() => void run(() => actions.approve(host.hostId, typed, grant))}
          >
            {pending ? "Approving…" : "Approve"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
