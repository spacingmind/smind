import { useState, type ReactNode } from "react";

import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";

/**
 * A confirm step for a destructive learning action. `requireText` adds a typed-confirmation field
 * (e.g. the workspace title) that must match exactly before the action is enabled. The caller owns
 * the mutation: pass its `pending` and `error`, close with `onOpenChange(false)` on success.
 */
export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel,
  pendingLabel,
  pending,
  error,
  requireText,
  destructive = true,
  onConfirm,
  testId = "learning-confirm",
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: ReactNode;
  confirmLabel: string;
  pendingLabel?: string;
  pending: boolean;
  error?: string | null;
  requireText?: string;
  /** False for a reversible step (restore a version): the confirm button is the primary one, not red. */
  destructive?: boolean;
  onConfirm: () => void;
  testId?: string;
}) {
  const [typed, setTyped] = useState("");
  const blocked = requireText !== undefined && typed !== requireText;
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) setTyped("");
        onOpenChange(next);
      }}
    >
      <DialogContent data-testid={testId}>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription asChild>
            <div>{description}</div>
          </DialogDescription>
        </DialogHeader>
        {requireText !== undefined && (
          <label className="flex flex-col gap-1 text-ui-sm text-foreground-muted">
            Type <span className="font-medium text-foreground">{requireText}</span> to confirm
            <Input
              data-testid={`${testId}-input`}
              value={typed}
              autoComplete="off"
              onChange={(e) => setTyped(e.target.value)}
            />
          </label>
        )}
        {error && (
          <p role="alert" data-testid={`${testId}-error`} className="text-ui-base text-destructive">
            {error}
          </p>
        )}
        <DialogFooter>
          <Button type="button" variant="outline" disabled={pending} onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button type="button" variant={destructive ? "destructive" : "default"} disabled={pending || blocked} data-testid={`${testId}-confirm`} onClick={onConfirm}>
            {pending ? (pendingLabel ?? confirmLabel) : confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
