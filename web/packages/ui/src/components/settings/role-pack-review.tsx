import { useState } from "react";

import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { FormError } from "@/components/ui/form-error";
import { useApplyRolePack } from "@/hooks/use-role-pack";
import { fieldLabel, outcomeText, stateLabel } from "@/lib/role-pack-text";
import type { RolePackCheck, RolePackField, RolePackSeat } from "@/lib/role-pack-types";
import { cn } from "@/lib/utils";

/** The same rule as the daemon: an untouched seat's stale prompt field is written by default. */
function writtenByDefault(seat: RolePackSeat, f: RolePackField): boolean {
  return seat.untouched && f.kind === "prompt" && f.state === "stale";
}

function DiffText({ diff }: { diff: string }) {
  const lines = diff === "" ? [] : diff.split("\n");
  return (
    <div className="max-h-64 overflow-auto rounded-md border border-border bg-muted/40 p-2 font-mono text-ui-xs" data-testid="role-pack-diff">
      {lines.map((line, i) => {
        const kind = line.startsWith("+") ? "add" : line.startsWith("-") ? "del" : "ctx";
        return (
          <div
            key={i}
            data-diff={kind}
            className={cn("whitespace-pre-wrap break-words", kind === "add" ? "text-success" : kind === "del" ? "text-destructive" : "text-foreground-muted")}
          >
            {line === "" ? " " : line}
          </div>
        );
      })}
    </div>
  );
}

function Badge({ children, tone }: { children: string; tone?: "success" | "warning" | "muted" }) {
  return (
    <span
      className={cn(
        "rounded-full border px-2 py-0.5 text-ui-xs",
        tone === "success" ? "border-success/40 text-success" : tone === "warning" ? "border-warning/40 text-warning" : "border-border text-foreground-muted",
      )}
    >
      {children}
    </span>
  );
}

/** The review: what would change, per seat and field, and the only buttons that ever call apply. */
export function RolePackReviewDialog({
  open,
  onOpenChange,
  check,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  check: RolePackCheck;
}) {
  const apply = useApplyRolePack();
  const [confirm, setConfirm] = useState<{ seat: string; field: RolePackField } | null>(null);

  const seats = check.seats.filter((s) => s.status === "ok" && s.fields.some((f) => f.state !== "current"));
  const defaultCount = seats.reduce((n, s) => n + s.fields.filter((f) => writtenByDefault(s, f)).length, 0);
  const pending = apply.isPending;

  const send = (take: string[]) => apply.mutate({ take });

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent data-testid="role-pack-review" className="flex max-h-[85vh] w-[calc(100vw-2rem)] max-w-3xl flex-col gap-3">
          <DialogHeader className="text-left">
            <DialogTitle>Role prompt updates (pack v{check.packVersion})</DialogTitle>
            <DialogDescription>
              Nothing is changed until you click Apply or Take. sMind saves a backup of every profile it changes first.
            </DialogDescription>
          </DialogHeader>
          <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto">
            {seats.map((seat) => (
              <div key={seat.name} className="flex flex-col gap-2" data-testid={`role-pack-seat-${seat.name}`}>
                <div className="flex items-center gap-2">
                  <h3 className="text-ui-base font-medium">{seat.name}</h3>
                  {!seat.untouched && <span className="text-ui-xs text-foreground-muted">edited by you</span>}
                </div>
                {seat.fields
                  .filter((f) => f.state !== "current")
                  .map((f) => (
                    <div key={f.name} className="flex flex-col gap-1.5" data-testid={`role-pack-field-${seat.name}-${f.name}`}>
                      <div className="flex items-center gap-2">
                        <span className="text-ui-sm font-medium">{fieldLabel(f.name)}</span>
                        <Badge tone={f.state === "stale" ? "success" : "warning"}>{stateLabel(f.state)}</Badge>
                        {f.kind === "capability" && <Badge>capability</Badge>}
                        {!writtenByDefault(seat, f) && (
                          <Button
                            type="button"
                            size="sm"
                            variant="outline"
                            className="ml-auto"
                            disabled={pending}
                            data-testid={`role-pack-take-${seat.name}-${f.name}`}
                            onClick={() => (f.kind === "capability" ? setConfirm({ seat: seat.name, field: f }) : send([`${seat.name}.${f.name}`]))}
                          >
                            Take
                          </Button>
                        )}
                      </div>
                      {f.kind === "capability" ? (
                        <p className="text-ui-sm text-foreground-muted">
                          Stored: {f.stored || "(empty)"} -&gt; Pack: {f.want || "(empty)"}
                        </p>
                      ) : (
                        <DiffText diff={f.diff} />
                      )}
                    </div>
                  ))}
              </div>
            ))}
          </div>
          <div className="flex shrink-0 flex-col gap-1">
            {apply.data && (
              <div data-testid="role-pack-result" className="flex flex-col gap-1 text-ui-sm">
                {apply.data.results.map((r) => (
                  <p key={`${r.seat}-${r.outcome}`}>{outcomeText(r)}</p>
                ))}
                {apply.data.backupDir !== "" && (
                  <p data-testid="role-pack-backup" className="break-all text-foreground-muted">
                    Backup saved to {apply.data.backupDir}
                  </p>
                )}
              </div>
            )}
            <FormError message={apply.error ? apply.error.message : null} testId="role-pack-apply-error" />
          </div>
          <DialogFooter className="items-center">
            {defaultCount === 0 && (
              <span className="mr-auto text-ui-sm text-foreground-muted">Nothing to apply by default; take fields one by one below.</span>
            )}
            <Button type="button" variant="outline" disabled={pending} data-testid="role-pack-close" onClick={() => onOpenChange(false)}>
              Close
            </Button>
            <Button type="button" disabled={pending || defaultCount === 0} data-testid="role-pack-apply" onClick={() => send([])}>
              {pending ? "Applying..." : `Apply ${defaultCount} ${defaultCount === 1 ? "update" : "updates"}`}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <ConfirmDialog
        open={confirm !== null}
        onOpenChange={(o) => {
          if (!o) setConfirm(null);
        }}
        testId="role-pack-capability-confirm"
        destructive={false}
        title={confirm ? `Change ${fieldLabel(confirm.field.name)} of ${confirm.seat}?` : ""}
        description={
          confirm
            ? `${fieldLabel(confirm.field.name)} changes what this seat may do. Stored: ${confirm.field.stored || "(empty)"} -> Pack: ${confirm.field.want || "(empty)"}.`
            : ""
        }
        confirmLabel="Take"
        pending={pending}
        onConfirm={() => {
          if (confirm) send([`${confirm.seat}.${confirm.field.name}`]);
          setConfirm(null);
        }}
      />
    </>
  );
}
