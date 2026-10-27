import { useEffect, useId, useState } from "react";

import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { ReadinessPanel } from "@/components/readiness-panel";
import { InlineSpinner } from "@/components/ui/inline-spinner";
import { Input } from "@/components/ui/input";
import type { Workspace } from "@/lib/types";
import {
  getWorkspaceDefaultBase,
  getWorkspaceScripts,
  setWorkspaceDefaultBase,
  setWorkspaceScripts,
} from "@/lib/worktree-setup";
import type { WsClientLike } from "@/lib/ws-client";

const SCRIPT_TEXTAREA_CLASS =
  "h-32 w-full resize-y rounded-lg border border-input-border bg-input px-2.5 py-2 font-mono text-ui-base leading-relaxed transition-colors outline-none placeholder:text-foreground-subtlest hover:border-input-border-hover focus-visible:border-input-border-focused focus-visible:bg-input-focused disabled:cursor-not-allowed disabled:opacity-50";

/**
 * The workspace's worktree settings: the default base branch (ADR-0039)
 * and the setup/teardown script editor (ADR-0024). The script text is only
 * ever returned by workspace.get, so the dialog loads it fresh on open
 * rather than reading it off the sidebar's workspace list. Saving sends
 * both script fields: an empty string clears a script, which is how
 * workspace.setScripts distinguishes "clear" from "keep".
 */
export function WorkspaceScriptsDialog({
  client,
  workspace,
  open,
  onOpenChange,
  onSaved,
}: {
  client: WsClientLike | null;
  workspace: Workspace;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Called after a successful save so the sidebar can refresh the Has*Script flags. */
  onSaved: () => void;
}) {
  const [setup, setSetup] = useState("");
  const [teardown, setTeardown] = useState("");
  const [defaultBase, setDefaultBase] = useState("");
  const [baseInfo, setBaseInfo] = useState<{ ref: string; detected: boolean; override: string } | null>(null);
  const [loading, setLoading] = useState(true);
  // A failed load leaves the textareas empty and saving then would clear
  // both scripts, so Save stays disabled until a load succeeded.
  const [loaded, setLoaded] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const id = useId();

  useEffect(() => {
    if (!open || !client) return;
    let cancelled = false;
    setLoading(true);
    setLoaded(false);
    setError(null);
    Promise.all([getWorkspaceScripts(client, workspace.ID), getWorkspaceDefaultBase(client, workspace.ID)])
      .then(([ws, base]) => {
        if (cancelled) return;
        setSetup(ws.SetupScript ?? "");
        setTeardown(ws.TeardownScript ?? "");
        setBaseInfo(base);
        setDefaultBase(base.override);
        setLoaded(true);
        setLoading(false);
      })
      .catch((err) => {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : String(err));
        setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [open, client, workspace.ID]);

  const submit = async () => {
    if (!client) return;
    setPending(true);
    setError(null);
    try {
      const trimmed = defaultBase.trim();
      if (baseInfo && trimmed !== baseInfo.override) {
        await setWorkspaceDefaultBase(client, workspace.ID, trimmed);
      }
      await setWorkspaceScripts(client, workspace.ID, { setupScript: setup, teardownScript: teardown });
      onSaved();
      onOpenChange(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setPending(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>Worktree settings</DialogTitle>
          <DialogDescription>
            {workspace.Title || workspace.Path} — the branch new tasks are measured against and open pull requests into, and
            the scripts that run in each new task&apos;s worktree with SMIND_WORKTREE_PATH, SMIND_SOURCE_CHECKOUT_PATH,
            SMIND_BRANCH, SMIND_PORT. Scripts are authored by you only — agents and repositories cannot change them.
          </DialogDescription>
        </DialogHeader>
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            void submit();
          }}
        >
          {loading ? (
            <InlineSpinner label="Loading scripts…" />
          ) : (
            <>
              <div className="grid gap-1.5">
                <label htmlFor={`${id}-default-base`} className="text-ui-base font-medium">
                  Default base branch
                </label>
                <Input
                  id={`${id}-default-base`}
                  data-testid="workspace-default-base"
                  placeholder={baseInfo?.ref ?? ""}
                  value={defaultBase}
                  onChange={(e) => setDefaultBase(e.target.value)}
                  disabled={pending}
                  spellCheck={false}
                />
                {baseInfo && (
                  <p className="text-ui-sm text-muted-foreground">
                    {baseInfo.detected
                      ? `Auto-detected: ${baseInfo.ref}. Leave empty to keep auto-detecting.`
                      : `Override in use. Clear the field to go back to auto-detect (${baseInfo.ref}).`}
                  </p>
                )}
              </div>
              <div className="grid gap-1.5">
                <label htmlFor={`${id}-setup`} className="text-ui-base font-medium">
                  Setup script
                </label>
                <textarea
                  id={`${id}-setup`}
                  data-testid="workspace-setup-script"
                  className={SCRIPT_TEXTAREA_CLASS}
                  placeholder="npm ci && cp $SMIND_SOURCE_CHECKOUT_PATH/.env .env"
                  spellCheck={false}
                  value={setup}
                  onChange={(e) => setSetup(e.target.value)}
                  disabled={pending}
                />
              </div>
              <div className="grid gap-1.5">
                <label htmlFor={`${id}-teardown`} className="text-ui-base font-medium">
                  Teardown script
                </label>
                <textarea
                  id={`${id}-teardown`}
                  data-testid="workspace-teardown-script"
                  className={SCRIPT_TEXTAREA_CLASS}
                  placeholder="Runs before the worktree is removed (archive or delete)"
                  spellCheck={false}
                  value={teardown}
                  onChange={(e) => setTeardown(e.target.value)}
                  disabled={pending}
                />
              </div>
            </>
          )}
          {/* After the editor loads, so the panel does not appear above the fields and then jump below them. */}
          {!loading && <ReadinessPanel client={client} workspaceId={workspace.ID} />}
          {error && (
            <p role="alert" data-testid="workspace-scripts-error" className="text-ui-base text-destructive">
              {error}
            </p>
          )}
          <DialogFooter>
            <Button type="button" variant="quiet" onClick={() => onOpenChange(false)} data-testid="workspace-scripts-cancel">
              Cancel
            </Button>
            <Button type="submit" disabled={pending || !loaded} data-testid="workspace-scripts-save">
              {pending ? "Saving…" : "Save"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
