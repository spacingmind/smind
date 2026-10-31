import { useEffect, useState } from "react";
import { Icons } from "@/lib/icons";

import { Button } from "@/components/ui/button";
import { NoticeBanner } from "@/components/ui/notice-banner";
import { StatusBadge } from "@/components/ui/status-badge";
import { useRetrySetup } from "@/hooks/use-retry-setup";
import { cn } from "@/lib/utils";
import type { SetupStatus, Task } from "@/lib/types";
import { getWorkspaceScripts, retryTaskSetup } from "@/lib/worktree-setup";
import type { WsClientLike } from "@/lib/ws-client";

const LABELS: Record<Exclude<SetupStatus, "">, string> = {
  running: "Setup running",
  ok: "Setup done",
  failed: "Setup failed",
  blocked: "Setup blocked",
};

/**
 * A task's worktree setup state (ADR-0024). Renders nothing when the task
 * never had a setup script (SetupStatus ""). `compact` is the icon-only
 * form for the sidebar row; the labelled pill goes in the task header.
 */
export function SetupBadge({ status, compact = false, className }: { status: SetupStatus | undefined; compact?: boolean; className?: string }) {
  if (!status) return null;
  const label = LABELS[status];
  const icon =
    status === "running" ? (
      <Icons.spinner className="size-3 shrink-0 animate-spin" aria-hidden="true" />
    ) : status === "ok" ? (
      <Icons.check className="size-3 shrink-0" aria-hidden="true" />
    ) : status === "blocked" ? (
      <Icons.lock className="size-3 shrink-0" aria-hidden="true" />
    ) : (
      <Icons.warning className="size-3 shrink-0" aria-hidden="true" />
    );
  if (compact) {
    return (
      <span
        data-testid="task-setup-badge"
        data-setup-status={status}
        role="img"
        aria-label={label}
        title={label}
        className={cn(
          "flex shrink-0 items-center",
          className,
          status === "failed" ? "text-destructive" : status === "running" || status === "blocked" ? "text-warning" : "text-success/70",
        )}
      >
        {icon}
      </span>
    );
  }
  return (
    <StatusBadge
      status={status === "failed" ? "danger" : status === "running" ? "running" : status === "blocked" ? "warning" : "success"}
      data-testid="task-setup-badge"
      data-setup-status={status}
    >
      {icon}
      {label}
    </StatusBadge>
  );
}

/** The task's allocated SMIND_PORT as a small `:<port>` chip; nothing when no port is set. */
export function PortChip({ port }: { port: number | null | undefined }) {
  if (port == null) return null;
  return (
    <span
      data-testid="task-port-chip"
      title={`SMIND_PORT ${port}`}
      className="rounded-md border bg-surface px-1.5 py-0.5 font-mono text-ui-xs text-foreground-muted"
    >
      :{port}
    </span>
  );
}

/**
 * The strip under the task header for a task that has run setup: the
 * failure reason when it failed, a "Show log" toggle for the output tail,
 * and "Retry setup". Retry only appears for a finished (ok/failed) setup
 * whose workspace still has a script -- hasScript starts unknown and the
 * button stays hidden until workspace.get confirms it.
 */
export function SetupDetails({
  client,
  task,
  onTask,
}: {
  client: WsClientLike | null;
  task: Task;
  /** Receives the task task.retrySetup returns (back in "running"). */
  onTask: (task: Task) => void;
}) {
  const status = task.SetupStatus ?? "";
  const finished = status === "ok" || status === "failed";
  const [open, setOpen] = useState(false);
  const [hasScript, setHasScript] = useState(false);
  const [retrying, setRetrying] = useState(false);
  const [retryError, setRetryError] = useState<string | null>(null);

  useEffect(() => {
    if (!client || !finished) return;
    let cancelled = false;
    getWorkspaceScripts(client, task.WorkspaceID)
      .then((ws) => {
        if (!cancelled) setHasScript(ws.HasSetupScript ?? ws.SetupScript !== "");
      })
      .catch((err) => console.error("workspace.get failed, hiding Retry setup", err));
    return () => {
      cancelled = true;
    };
  }, [client, finished, task.WorkspaceID]);

  if (!status) return null;
  const log = (task.SetupLog ?? "").trim();
  const error = task.SetupError ?? "";
  // The strip carries no information for a clean setup with no output and
  // nothing to retry.
  if (status === "running" && !log) return null;

  const retry = async () => {
    if (!client) return;
    setRetrying(true);
    setRetryError(null);
    try {
      onTask(await retryTaskSetup(client, task.ID));
      setOpen(false);
    } catch (err) {
      setRetryError(err instanceof Error ? err.message : String(err));
    } finally {
      setRetrying(false);
    }
  };

  return (
    <div data-testid="task-setup-details" className="border-b bg-surface px-4 py-2 text-ui-sm">
      <div className="flex items-center gap-2">
        {status === "failed" && error ? (
          <p data-testid="task-setup-error" className="min-w-0 flex-1 truncate text-destructive" title={error}>
            {error}
          </p>
        ) : (
          <span className="min-w-0 flex-1 truncate text-foreground-muted">
            {status === "running" ? "Setup output" : status === "blocked" ? "Setup has not run" : "Setup finished"}
          </span>
        )}
        {log && (
          <Button
            type="button"
            variant="ghost"
            size="xs"
            data-testid="task-setup-log-toggle"
            aria-expanded={open}
            aria-controls={`task-${task.ID}-setup-log`}
            onClick={() => setOpen((v) => !v)}
          >
            <Icons.chevronRight className={cn("size-3 transition-transform", open && "rotate-90")} />
            {open ? "Hide log" : "Show log"}
          </Button>
        )}
        {finished && hasScript && (
          <Button type="button" variant="outline" size="xs" data-testid="task-setup-retry" disabled={retrying} onClick={() => void retry()}>
            <Icons.retry className={cn("size-3", retrying && "animate-spin")} />
            Retry setup
          </Button>
        )}
      </div>
      {retryError && (
        <p role="alert" className="mt-1.5 text-destructive">
          {retryError}
        </p>
      )}
      {open && log && (
        <pre
          id={`task-${task.ID}-setup-log`}
          data-testid="task-setup-log"
          tabIndex={0}
          className="mt-2 max-h-56 overflow-auto rounded-lg border bg-background p-2.5 font-mono text-ui-sm leading-relaxed whitespace-pre-wrap text-foreground-muted"
        >
          {log}
        </pre>
      )}
    </div>
  );
}

/**
 * The task view's notice for a task started from a fork PR (ADR-0039 §7): its setup script is blocked until the user
 * runs it. Keyed on `UntrustedSource`, not on `SetupStatus`: a workspace without a setup script has an empty status
 * but its task is still untrusted. "Run setup" clears the flag daemon-side, so the banner goes with the next task.updated.
 */
export function UntrustedBanner({ task, onTask }: { task: Task; onTask: (task: Task) => void }) {
  const retry = useRetrySetup();
  if (!task.UntrustedSource) return null;
  return (
    <NoticeBanner
      tone="warning"
      testId="task-untrusted-banner"
      actions={
        <>
          <Button
            type="button"
            variant="outline"
            size="xs"
            data-testid="task-untrusted-run-setup"
            disabled={retry.isPending}
            onClick={() => retry.mutate(task.ID, { onSuccess: onTask })}
          >
            <Icons.play className="size-3" />
            Run setup
          </Button>
          {retry.isError && (
            <span role="alert" className="text-ui-sm text-destructive">
              {retry.error.message}
            </span>
          )}
        </>
      }
    >
      <span className="font-medium">From a fork — setup scripts are blocked until you run them.</span>{" "}
      <span className="text-foreground-muted">Review the code first.</span>
    </NoticeBanner>
  );
}
