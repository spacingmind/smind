import { useEffect, useState } from "react";

import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { FormError } from "@/components/ui/form-error";
import { Input } from "@/components/ui/input";
import { useFleetHostActions } from "@/hooks/use-fleet-hosts";

const INVITE_WINDOW_MINUTES = 10;

function messageOf(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/** "in 10 min" for a future expiry (lib/relative-time only words the past); an unparseable stamp is shown as given. */
function expiresText(expiresAt: string): string {
  const ms = new Date(expiresAt).getTime() - Date.now();
  if (Number.isNaN(ms)) return expiresAt;
  if (ms <= 0) return "now";
  return `in ${Math.max(1, Math.round(ms / 60_000))} min`;
}

/**
 * Creating an invite (ADR-0048): a short-lived string the other host feeds to `smind host add`. It holds no
 * password and no provider secret, so showing and copying it is safe; it is dropped from state when the
 * dialog closes so a stale one is never left on screen.
 */
export function InviteHostDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const actions = useFleetHostActions();
  const [invite, setInvite] = useState<{ invite: string; expiresAt: string } | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (open) return;
    setInvite(null);
    setError(null);
  }, [open]);

  async function create() {
    setPending(true);
    setError(null);
    try {
      setInvite(await actions.invite(INVITE_WINDOW_MINUTES));
    } catch (err) {
      setError(messageOf(err));
    } finally {
      setPending(false);
    }
  }

  async function copy() {
    if (!invite) return;
    try {
      await navigator.clipboard.writeText(invite.invite);
    } catch {
      // Clipboard access can be denied; the invite stays selectable in the field.
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent data-testid="invite-host-dialog">
        <DialogHeader className="text-left">
          <DialogTitle>Invite a host</DialogTitle>
          <DialogDescription>
            Create an invite, then run smind host add on the other host within 10 minutes. The invite holds no password and no
            provider secret.
          </DialogDescription>
        </DialogHeader>
        {invite && (
          <div className="flex flex-col gap-2">
            <div className="flex items-center gap-2">
              <Input
                readOnly
                aria-label="Invite"
                data-testid="invite-value"
                className="min-w-0 flex-1 text-ellipsis font-mono"
                title={invite.invite}
                value={invite.invite}
                onFocus={(e) => e.currentTarget.select()}
              />
              <Button type="button" variant="outline" className="shrink-0" data-testid="invite-copy" onClick={() => void copy()}>
                Copy
              </Button>
            </div>
            <p className="text-ui-sm text-muted-foreground" data-testid="invite-expires">
              Expires {expiresText(invite.expiresAt)}
            </p>
          </div>
        )}
        <FormError message={error} testId="invite-error" />
        <DialogFooter>
          <Button type="button" disabled={pending} data-testid="invite-create" onClick={() => void create()}>
            {pending ? "Creating…" : invite ? "Create a new invite" : "Create invite"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
