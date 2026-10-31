import { Fragment, useEffect, useId, useRef, useState } from "react";

import { Checkbox } from "@/components/ui/checkbox";
import { describePrError } from "@/components/git/pr-error";
import { PrErrorLine } from "@/components/git/pr-error-line";
import {
  conflictData,
  nextStep,
  pushTargetNote,
  plural,
  pushSuccessLine,
  resolveConflictPrompt,
  syncCard,
  updateSuccessLine,
  type SyncCard,
  type SyncMode,
} from "@/components/git/sync-model";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { usePushTask, useUpdateFromBase } from "@/hooks/use-pr-actions";
import { useSendToFocusedChat } from "@/hooks/use-send-to-focused-chat";
import { useTaskGit } from "@/hooks/use-task-git";
import { Icons } from "@/lib/icons";
import type { UpdateConflictErrorData } from "@/lib/types";

/** The dialog title, also the menu and header label: "Publish branch" while the branch has no upstream. */
export function syncTitle(mode: SyncMode, published: boolean, base: string): string {
  if (mode === "push") return published ? "Push" : "Publish branch";
  return base ? `Update from ${base}` : "Update from base";
}

const MAX_VISIBLE_FILES = 10;

/**
 * Push / Publish branch and Update from base (ADR-0039 §3-§6, G7): one confirm card per action with the facts that
 * decide it, then in flight → result line, a typed error, or (update only) the conflict state, which names the files
 * and offers "Ask the agent to resolve". The worktree is never aborted and there is no force push anywhere.
 */
export function SyncDialog({
  taskId,
  mode,
  open,
  onOpenChange,
}: {
  taskId: number;
  mode: SyncMode;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const pending = useRef(false);
  return (
    <Dialog open={open} onOpenChange={(next) => (next || !pending.current) && onOpenChange(next)}>
      <DialogContent
        className="top-[10vh] max-h-[calc(90dvh-1rem)] translate-y-0 gap-3 overflow-y-auto p-5 sm:max-w-md"
        data-testid="sync-dialog"
        // The primary action takes focus, so Enter confirms and the first Tab doesn't start on Cancel.
        onOpenAutoFocus={(e) => {
          e.preventDefault();
          (e.currentTarget as HTMLElement | null)?.querySelector<HTMLElement>('[data-testid="sync-submit"]')?.focus();
        }}
      >
        <SyncForm taskId={taskId} mode={mode} onClose={() => onOpenChange(false)} onPending={(p) => (pending.current = p)} />
      </DialogContent>
    </Dialog>
  );
}

/** Mounted only while the dialog is open, so every open starts from the idle card. */
function SyncForm({ taskId, mode, onClose, onPending }: { taskId: number; mode: SyncMode; onClose: () => void; onPending: (pending: boolean) => void }) {
  const git = useTaskGit(taskId);
  const push = usePushTask();
  const update = useUpdateFromBase();
  const sendToChat = useSendToFocusedChat(taskId);
  const [rebase, setRebase] = useState(false);
  const [successLine, setSuccessLine] = useState<string | null>(null);
  // The card as it was when the call started: the `task.git` event that follows must not rewrite it under the result.
  const [frozen, setFrozen] = useState<SyncCard | null>(null);
  const [asking, setAsking] = useState(false);
  const [askError, setAskError] = useState<string | null>(null);
  const rebaseId = useId();

  const card = frozen ?? syncCard(mode, git);
  const mutation = mode === "push" ? push : update;
  const inFlight = mutation.isPending || asking;
  const title = syncTitle(mode, card.published, card.base);
  const conflict: UpdateConflictErrorData | null =
    mode === "update" && update.isError && (update.error as { code?: string }).code === "conflict"
      ? conflictData((update.error as { data?: unknown }).data)
      : null;
  const error = mutation.error && !conflict ? describePrError(mutation.error, mode === "push" ? "push" : "sync") : null;
  const action = mode === "push" ? git?.actions?.push : git?.actions?.updateFromBase;
  // `up_to_date` is advisory in `actions` (the RPC fetches and answers), so only other refusals block the button.
  const blocked = !!action && !action.enabled && action.code !== "up_to_date";
  const done = successLine !== null;
  const showRebaseWarning = mode === "update" && rebase && card.published;

  // A disabled button gives up focus; hand it back to the primary once the call settles into a result or a conflict.
  const primaryRef = useRef<HTMLButtonElement>(null);
  const settled = done || conflict !== null;
  useEffect(() => {
    if (settled) primaryRef.current?.focus();
  }, [settled]);

  const run = () => {
    if (inFlight || done || blocked) return;
    setFrozen(card);
    onPending(true);
    const settle = () => onPending(false);
    if (mode === "push") {
      push.mutate(
        { taskId },
        {
          onSuccess: (result) => {
            settle();
            setSuccessLine(pushSuccessLine(result));
          },
          onError: settle,
        },
      );
      return;
    }
    update.mutate(
      { taskId, ...(rebase ? { strategy: "rebase" as const } : {}) },
      {
        onSuccess: (result) => {
          settle();
          setSuccessLine(updateSuccessLine(result, card));
        },
        onError: settle,
      },
    );
  };

  const askAgent = async () => {
    if (!conflict) return;
    setAsking(true);
    setAskError(null);
    onPending(true);
    try {
      await sendToChat(resolveConflictPrompt(conflict));
      onPending(false);
      onClose();
    } catch (err) {
      onPending(false);
      setAskError(err instanceof Error ? err.message : String(err));
    } finally {
      setAsking(false);
    }
  };

  return (
    <form
      className="grid gap-3"
      onSubmit={(e) => {
        e.preventDefault();
        run();
      }}
    >
      <DialogHeader>
        <DialogTitle>{title}</DialogTitle>
        <DialogDescription className="sr-only">
          {mode === "push" ? "Review what will be sent to origin, then push." : "Review what will be brought in from the base branch, then update."}
        </DialogDescription>
      </DialogHeader>

      <dl className="grid grid-cols-[4.5rem_1fr] gap-x-4 gap-y-1.5 rounded-lg border border-input-border bg-surface px-3 py-2.5 text-ui-base" data-testid="sync-card">
        <Row label="Branch" testId="sync-row-branch" mono>
          <Icons.branch aria-hidden className="mr-1.5 inline size-3.5 text-foreground-muted" />
          <Breakable text={card.branch || "—"} />
        </Row>
        <Row label={mode === "push" ? "Remote" : "Base"} testId="sync-row-remote" mono>
          {mode === "push" ? (
            card.pushTo ? (
              <Breakable text={`${card.pushTo.remote}/${card.pushTo.branch}`} />
            ) : card.published ? (
              <Breakable text={`origin/${card.branch}`} />
            ) : (
              "origin (new upstream)"
            )
          ) : (
            card.remote || "—"
          )}
          {mode === "push" && pushTargetNote(card) && (
            <p className="font-sans text-ui-sm text-foreground-muted" data-testid="sync-remote-note">
              {pushTargetNote(card)}
            </p>
          )}
        </Row>
        <Row label="Ahead" testId="sync-row-ahead">
          {card.ahead === undefined ? "—" : plural(card.ahead, "commit")}
        </Row>
        <Row label="Behind" testId="sync-row-behind">
          {card.behind === undefined ? "—" : plural(card.behind, "commit")}
        </Row>
        {!done && !conflict && (
          <Row label="Next step" testId="sync-row-next">
            <Breakable text={nextStep(mode, card, rebase)} />
          </Row>
        )}
      </dl>

      {mode === "update" && !done && !conflict && (
        <div className="grid gap-1.5">
          <label htmlFor={rebaseId} className="flex items-center gap-2 text-ui-base">
            <Checkbox id={rebaseId} checked={rebase} disabled={inFlight} onCheckedChange={(on) => setRebase(on === true)} data-testid="sync-rebase" />
            <span>Rebase instead of merge</span>
          </label>
          {showRebaseWarning && (
            <Alert
              variant="warning"
              description="smind can't force-push; a rebased published branch needs a manual push --force-with-lease."
              testId="sync-rebase-warning"
            />
          )}
        </div>
      )}

      {/* The button carries the visible "Pushing…"; this is the same state for assistive tech. */}
      {inFlight && !conflict && (
        <p className="sr-only" role="status" data-testid="sync-progress">
          {asking ? "Sending…" : mode === "push" ? "Pushing…" : "Updating…"}
        </p>
      )}

      {successLine && (
        <p className="flex items-center gap-1.5 text-ui-base text-success" role="status" data-testid="sync-result">
          <Icons.success aria-hidden className="size-3.5 shrink-0" />
          {successLine}
        </p>
      )}

      {error && <PrErrorLine error={error} testId="sync-error" onNavigate={onClose} />}

      {conflict && (
        <div className="grid gap-2" data-testid="sync-conflict">
          <PrErrorLine error={describePrError(update.error, "sync")} testId="sync-conflict-message" />
          <ConflictFiles conflict={conflict} />
          {askError && <p className="text-ui-sm text-destructive" role="alert" data-testid="sync-ask-error">{askError}</p>}
        </div>
      )}

      {/* One primary button for every state, so the focused element survives the swap (a remounted one drops focus to the dialog). */}
      <DialogFooter className="gap-2">
        {!done && (
          <Button type="button" variant="ghost" onClick={onClose} disabled={asking || (inFlight && !conflict)} data-testid={conflict ? "sync-close" : "sync-cancel"}>
            {conflict ? "Close" : "Cancel"}
          </Button>
        )}
        <Button
          ref={primaryRef}
          type={conflict || done ? "button" : "submit"}
          disabled={conflict ? asking : done ? false : inFlight || blocked}
          onClick={conflict ? () => void askAgent() : done ? onClose : undefined}
          data-testid={conflict ? "sync-ask-agent" : done ? "sync-close" : "sync-submit"}
        >
          {conflict ? (
            asking ? (
              <>
                <Icons.spinner aria-hidden className="size-3.5 animate-spin" /> Sending…
              </>
            ) : (
              "Ask the agent to resolve"
            )
          ) : done ? (
            "Close"
          ) : inFlight ? (
            <>
              <Icons.spinner aria-hidden className="size-3.5 animate-spin" /> {mode === "push" ? "Pushing…" : "Updating…"}
            </>
          ) : mode === "push" ? (
            card.published ? (
              "Push"
            ) : (
              "Publish branch"
            )
          ) : rebase ? (
            "Rebase branch"
          ) : (
            "Update branch"
          )}
        </Button>
      </DialogFooter>
    </form>
  );
}

function Row({ label, testId, mono, children }: { label: string; testId: string; mono?: boolean; children: React.ReactNode }) {
  return (
    <div className="contents" data-testid={testId}>
      <dt className="text-ui-sm leading-5 text-foreground-muted">{label}</dt>
      <dd className={mono ? "min-w-0 font-mono text-ui-sm leading-5 [overflow-wrap:anywhere]" : "min-w-0 leading-5 [overflow-wrap:anywhere]"}>{children}</dd>
    </div>
  );
}

/** Text that may wrap after `/` and `-` (a branch name) without a zero-width character landing in a copy. */
function Breakable({ text }: { text: string }) {
  const parts = text.split(/(?<=[/-])/);
  return (
    <>
      {parts.map((part, i) => (
        <Fragment key={i}>
          {i > 0 && <wbr />}
          {part}
        </Fragment>
      ))}
    </>
  );
}

function ConflictFiles({ conflict }: { conflict: UpdateConflictErrorData }) {
  const visible = conflict.files.slice(0, MAX_VISIBLE_FILES);
  const hidden = conflict.files.length - visible.length;
  return (
    <div className="grid gap-1">
      <p className="text-ui-sm text-foreground-muted">Conflicting files</p>
      <ul className="grid max-h-48 gap-0.5 overflow-y-auto rounded-lg border border-input-border bg-surface px-3 py-2 font-mono text-ui-sm" data-testid="sync-conflict-files">
        {visible.map((file) => (
          <li key={file} className="[overflow-wrap:anywhere]">
            {file}
          </li>
        ))}
        {(hidden > 0 || conflict.filesTruncated) && (
          <li className="font-sans text-foreground-muted" data-testid="sync-conflict-more">
            {hidden > 0 ? `and ${hidden} more` : "and more"}
          </li>
        )}
      </ul>
    </div>
  );
}
