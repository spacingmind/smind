import { useEffect, useState } from "react";

import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { FormError } from "@/components/ui/form-error";
import { Input } from "@/components/ui/input";
import { useFleetHostActions } from "@/hooks/use-fleet-hosts";
import { diffLabels, MAX_HOST_LABELS, rowsFromLabels, validateLabelRows, type LabelRow } from "@/lib/fleet-labels";
import type { FleetHost } from "@/lib/fleet-types";
import { Icons } from "@/lib/icons";

function messageOf(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/**
 * Editing a host's routing labels (ADR-0048 F5): up to 16 `key: value` tags this host keeps for the peer and uses to
 * pick one by tag (`smind host select --labels`). They are never sent to the peer. Edits start from the stored labels and
 * are dropped on close; Save sends only the difference through `fleet.host.labels`, whose own validation has the last word.
 */
export function EditLabelsDialog({
  host,
  open,
  onOpenChange,
}: {
  host: FleetHost;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const actions = useFleetHostActions();
  const [rows, setRows] = useState<LabelRow[]>(() => rowsFromLabels(host.labels));
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (open) return;
    setError(null);
    setRows(rowsFromLabels(host.labels));
  }, [open, host.labels]);

  const checked = validateLabelRows(rows);
  const change = diffLabels(host.labels, rows);
  const changed = Object.keys(change.set).length > 0 || change.remove.length > 0;

  function update(index: number, patch: Partial<LabelRow>) {
    setRows((prev) => prev.map((r, i) => (i === index ? { ...r, ...patch } : r)));
  }

  async function save() {
    setPending(true);
    setError(null);
    try {
      await actions.setLabels(host.hostId, change);
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
      <DialogContent data-testid="edit-labels-dialog">
        <DialogHeader className="text-left">
          <DialogTitle>{`Labels for ${host.alias}`}</DialogTitle>
          <DialogDescription>
            Tags you set to pick this host by what it is, for example os: windows. They stay on this host and are never sent to {host.alias}.
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-2" data-testid="edit-labels-rows">
          {rows.length === 0 && (
            <p className="text-ui-sm text-foreground-muted" data-testid="edit-labels-empty">
              No labels yet.
            </p>
          )}
          {rows.map((row, i) => {
            const errs = checked.rows[i];
            const touched = row.key !== "" || row.value !== "";
            return (
              <div key={i} className="flex flex-col gap-1" data-testid={`edit-labels-row-${i}`}>
                <div className="flex items-center gap-2">
                  <Input
                    aria-label={`Label ${i + 1} key`}
                    aria-invalid={touched && errs.key !== null}
                    data-testid={`edit-labels-key-${i}`}
                    placeholder="key"
                    value={row.key}
                    disabled={pending}
                    autoComplete="off"
                    spellCheck={false}
                    onChange={(e) => update(i, { key: e.target.value })}
                  />
                  <Input
                    aria-label={`Label ${i + 1} value`}
                    aria-invalid={touched && errs.value !== null}
                    data-testid={`edit-labels-value-${i}`}
                    placeholder="value"
                    value={row.value}
                    disabled={pending}
                    autoComplete="off"
                    spellCheck={false}
                    onChange={(e) => update(i, { value: e.target.value })}
                  />
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-xs"
                    aria-label={`Remove label ${i + 1}`}
                    data-testid={`edit-labels-remove-${i}`}
                    disabled={pending}
                    onClick={() => setRows((prev) => prev.filter((_, j) => j !== i))}
                  >
                    <Icons.close aria-hidden />
                  </Button>
                </div>
                {touched && (errs.key || errs.value) && (
                  <FormError message={errs.key ?? errs.value} size="sm" silent testId={`edit-labels-row-error-${i}`} />
                )}
              </div>
            );
          })}
        </div>

        <div className="flex items-center justify-between gap-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            data-testid="edit-labels-add"
            disabled={pending || rows.length >= MAX_HOST_LABELS}
            onClick={() => setRows((prev) => [...prev, { key: "", value: "" }])}
          >
            <Icons.add aria-hidden />
            Add label
          </Button>
          <span className="text-ui-sm text-foreground-muted" data-testid="edit-labels-count">
            {rows.length} of {MAX_HOST_LABELS}
          </span>
        </div>

        <FormError message={error} testId="edit-labels-error" />
        <DialogFooter>
          <Button
            type="button"
            disabled={pending || !checked.ok || !changed}
            data-testid="edit-labels-save"
            onClick={() => void save()}
          >
            {pending ? "Saving…" : "Save"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
