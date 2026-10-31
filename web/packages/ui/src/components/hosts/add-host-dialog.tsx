import { useEffect, useState } from "react";

import { formatSas } from "@/components/hosts/grant-summary";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { FormError } from "@/components/ui/form-error";
import { Input } from "@/components/ui/input";
import { useFleetHostActions } from "@/hooks/use-fleet-hosts";
import type { FleetHost } from "@/lib/fleet-types";

const ALIAS_RE = /^[a-z][a-z0-9-]{0,31}$/;

function messageOf(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/**
 * Adding a host from an invite another host created (ADR-0048). This side only starts the pairing: the
 * result is the code to confirm on the OTHER host, where a person approves. Nothing in this dialog can
 * approve anything, by design (S9).
 */
export function AddHostDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const actions = useFleetHostActions();
  const [invite, setInvite] = useState("");
  const [alias, setAlias] = useState("");
  const [cleartext, setCleartext] = useState(false);
  const [added, setAdded] = useState<FleetHost | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (open) return;
    setInvite("");
    setAlias("");
    setCleartext(false);
    setAdded(null);
    setError(null);
  }, [open]);

  const aliasInvalid = alias !== "" && !ALIAS_RE.test(alias);
  const canSubmit = invite.trim() !== "" && !aliasInvalid && !pending;

  async function submit() {
    setPending(true);
    setError(null);
    try {
      setAdded(await actions.add(invite.trim(), alias || undefined, cleartext || undefined));
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
      <DialogContent data-testid="add-host-dialog">
        {added ? (
          <>
            <DialogHeader className="text-left">
              <DialogTitle>{`Confirm this code on ${added.alias}`}</DialogTitle>
              <DialogDescription asChild>
                <div className="flex flex-col gap-3">
                  {added.sas ? (
                    <p
                      data-testid="add-sas"
                      className="text-center font-mono text-3xl font-semibold tracking-widest tabular-nums text-foreground"
                    >
                      {formatSas(added.sas)}
                    </p>
                  ) : (
                    <p data-testid="add-no-sas">Waiting for the pairing code…</p>
                  )}
                  <p>Nothing is trusted until a person approves on that host. This dialog cannot approve for you.</p>
                </div>
              </DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button type="button" data-testid="add-done" onClick={() => onOpenChange(false)}>
                Done
              </Button>
            </DialogFooter>
          </>
        ) : (
          <>
            <DialogHeader className="text-left">
              <DialogTitle>Add a host</DialogTitle>
              <DialogDescription>Paste the invite created on the other host.</DialogDescription>
            </DialogHeader>
            <textarea
              aria-label="Invite"
              data-testid="add-invite"
              className="h-24 w-full resize-none rounded-lg border border-input bg-transparent px-2.5 py-1 font-mono text-ui-base outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
              placeholder="smind-host://…"
              disabled={pending}
              value={invite}
              onChange={(e) => setInvite(e.target.value)}
            />
            <div className="flex flex-col gap-1">
              <Input
                aria-label="Alias (optional)"
                placeholder="Alias (optional)"
                data-testid="add-alias"
                autoComplete="off"
                disabled={pending}
                value={alias}
                onChange={(e) => setAlias(e.target.value)}
              />
              <FormError
                message={aliasInvalid ? "Use lowercase letters, digits and dashes, starting with a letter" : null}
                size="sm"
                testId="add-alias-error"
                silent
              />
            </div>
            <label className="flex items-start gap-2 text-ui-sm text-foreground">
              <Checkbox
                className="mt-0.5"
                data-testid="add-cleartext"
                checked={cleartext}
                disabled={pending}
                onCheckedChange={(on) => setCleartext(on === true)}
              />
              <span className="flex flex-col">
                <span>Allow cleartext</span>
                <span className="text-muted-foreground">Prompts and results are readable on that network.</span>
              </span>
            </label>
            <FormError message={error} testId="add-error" />
            <DialogFooter>
              <Button type="button" disabled={!canSubmit} data-testid="add-submit" onClick={() => void submit()}>
                {pending ? "Adding…" : "Add host"}
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
