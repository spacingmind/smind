import { useEffect, useState, type ReactNode } from "react";

import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { FormError } from "@/components/ui/form-error";
import { Input } from "@/components/ui/input";

/**
 * The one confirm step for a destructive (or consequential) action: title, a description that says what
 * will happen, an inline error slot for the daemon's refusal, Cancel + the confirm button. `requireText`
 * adds a typed-confirmation field (e.g. the workspace title) that must match exactly before the action is
 * enabled. The caller owns the mutation: pass its `pending` and `error` (see `useConfirmAction`) and close
 * with `onOpenChange(false)` on success -- a failure stays open with the message visible.
 *
 * Test ids default to `${testId}-confirm` / `-cancel` / `-error` / `-input`; pass `confirmTestId` etc. to
 * keep an existing surface's ids.
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
  testId = "confirm-dialog",
  confirmTestId,
  cancelTestId,
  errorTestId,
  className,
  children,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: ReactNode;
  description: ReactNode;
  confirmLabel: ReactNode;
  pendingLabel?: ReactNode;
  pending: boolean;
  error?: string | null;
  requireText?: string;
  /** False for a reversible step (restore a version): the confirm button is the primary one, not red. */
  destructive?: boolean;
  onConfirm: () => void;
  testId?: string;
  confirmTestId?: string;
  cancelTestId?: string;
  errorTestId?: string;
  /** For the dialog box itself, e.g. top-anchoring one whose body loads after it opens. */
  className?: string;
  /** Controls between the description and the error (a checkbox the confirm depends on). */
  children?: ReactNode;
}) {
  const [typed, setTyped] = useState("");
  const blocked = requireText !== undefined && typed !== requireText;
  // Whatever closes the dialog (Cancel, Esc, the overlay, the X, the parent after success), the typed
  // confirmation does not survive into the next time it opens.
  useEffect(() => {
    if (!open) setTyped("");
  }, [open]);
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        // An action in flight keeps the dialog open: closing it would hide a failure the user is waiting on.
        if (!next && pending) return;
        onOpenChange(next);
      }}
    >
      <DialogContent data-testid={testId} className={className}>
        <DialogHeader className="text-left">
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
        {children}
        <FormError message={error} testId={errorTestId ?? `${testId}-error`} />
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            disabled={pending}
            data-testid={cancelTestId ?? `${testId}-cancel`}
            onClick={() => onOpenChange(false)}
          >
            Cancel
          </Button>
          <Button
            type="button"
            variant={destructive ? "destructive" : "default"}
            disabled={pending || blocked}
            data-testid={confirmTestId ?? `${testId}-confirm`}
            onClick={onConfirm}
          >
            {pending ? (pendingLabel ?? confirmLabel) : confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
