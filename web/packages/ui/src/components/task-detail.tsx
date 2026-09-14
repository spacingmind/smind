import { useCallback, useRef, useState } from "react";
import { ArrowDown } from "lucide-react";

import { Composer } from "@/components/composer/composer";
import { useDetailLevel } from "@/components/timeline/detail-level";
import { RunTimeline } from "@/components/timeline/run-timeline";
import { useAutoFollow } from "@/components/timeline/use-auto-follow";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { InlineSpinner } from "@/components/ui/inline-spinner";
import { PaneHeader } from "@/components/ui/pane-header";
import { useRunTimeline, type RunEntry } from "@/hooks/use-run-timeline";
import type { ConnectionStatus } from "@/lib/reconnect";
import type { Task } from "@/lib/types";
import type { WsClientLike } from "@/lib/ws-client";

/**
 * The main-content pane for a selected task: identity header, a chat-log
 * timeline of its runs (history plus any still-live streaming), and the
 * composer that starts new runs via run.start + run.attach (never
 * task.prompt -- see the hook this delegates to for why). All data comes
 * from useRunTimeline; this component is presentation plus the wiring
 * that tells the composer which run (if any) is currently live.
 */
export function TaskDetailPane({
  client,
  task,
  connectionStatus = "connected",
  onOpenFile,
}: {
  client: WsClientLike | null;
  task: Task;
  /** Real-time connection status from App.tsx -- lets an active run.attach subscription visibly reflect a break instead of silently freezing on stale "live" output. Defaults to "connected" so every existing caller/test not wired up to App.tsx's status keeps behaving exactly as before. */
  connectionStatus?: ConnectionStatus;
  /** Opens a worktree-relative path as a file tab. Optional: without it, a tool-call card naming a file simply isn't click-through. */
  onOpenFile?: (path: string) => void;
}) {
  const { runs, error, submitPrompt, stopRun, respondPermission } = useRunTimeline(client, task.ID);

  // Every run currently holding an unanswered permission request -- in
  // practice at most one (a task has one active run at a time), but this
  // stays a list so nothing here assumes that. Rendered in a dock pinned
  // between the scrolling log and the prompt form (see below) rather than
  // inline per-run, so it can't be scrolled out of view while log chunks
  // keep streaming in above/below it.
  const pendingRuns = runs?.filter((run) => run.pendingPermission) ?? [];

  // The task's live run, if any. A task has at most one at a time, so the
  // first match is the one the composer's Stop and queue drain act on.
  const runningRunId = runs?.find((run) => run.status === "running")?.id ?? null;

  // Auto-follow keys off the total item count across every run: that is
  // the one number that changes whenever anything is appended anywhere in
  // the transcript, and it's O(runs) rather than O(items) to compute.
  const itemCount = runs?.reduce((total, run) => total + run.items.length, 0) ?? 0;
  const follow = useAutoFollow<HTMLDivElement>(itemCount);

  const [detailLevel, setDetailLevel] = useDetailLevel();

  // App.tsx re-creates its openFileTab closure on every render, which
  // would defeat TimelineRow's memo if passed straight through. Pinning
  // it behind a ref gives every row a callback whose identity never
  // changes while still calling the current one.
  const openFileRef = useRef(onOpenFile);
  openFileRef.current = onOpenFile;
  const openFile = useCallback((path: string) => openFileRef.current?.(path), []);

  return (
    <div className="flex h-full flex-col">
      <PaneHeader
        title={task.Title}
        subtitle={
          <>
            <span className="uppercase">{task.Status}</span>
            {task.Branch && <span className="truncate">{task.Branch}</span>}
          </>
        }
        actions={
          <Button
            type="button"
            variant="ghost"
            size="xs"
            data-testid="detail-level-toggle"
            aria-pressed={detailLevel === "overview"}
            onClick={() => setDetailLevel(detailLevel === "overview" ? "detailed" : "overview")}
          >
            {detailLevel === "overview" ? "Overview" : "Detailed"}
          </Button>
        }
      />

      {connectionStatus === "reconnecting" && (
        <Alert
          testId="connection-banner"
          variant="warning"
          className="rounded-none border-x-0 border-t-0"
          description="Connection lost -- reconnecting to daemon…"
        />
      )}

      <div className="relative flex-1 min-h-0">
        <div
          ref={follow.ref}
          onScroll={follow.onScroll}
          data-testid="run-log-scroll"
          data-following={follow.following}
          className="h-full overflow-y-auto px-4 py-3"
        >
          {error && <Alert variant="error" description={error} />}
          {!error && runs === null && <InlineSpinner label="Loading runs…" />}
          {!error && runs !== null && runs.length === 0 && (
            <EmptyState title="No runs yet" description="Send a prompt to start one" />
          )}
          {runs !== null && runs.length > 0 && (
            <ul className="space-y-4">
              {runs.map((run) => (
                <RunTimeline
                  key={run.id}
                  run={run}
                  detailLevel={detailLevel}
                  worktreePath={task.WorktreePath ?? undefined}
                  onOpenFile={openFile}
                />
              ))}
            </ul>
          )}
        </div>

        {!follow.following && (
          <Button
            type="button"
            variant="outline"
            size="sm"
            data-testid="jump-to-latest"
            onClick={follow.jumpToLatest}
            className="absolute bottom-3 left-1/2 -translate-x-1/2 shadow-sm"
          >
            <ArrowDown className="size-3" />
            Jump to latest
          </Button>
        )}
      </div>

      {/*
       * The pending-permission dock: a sibling of the scrolling log above,
       * not a descendant of it, so it stays pinned in place (like the
       * prompt form right below it) no matter how far the log has
       * scrolled or how much new output streams in. See the plan's Item 3.
       */}
      {pendingRuns.length > 0 && (
        <div data-testid="pending-permission-dock" className="shrink-0 border-t bg-background px-4 py-2">
          {pendingRuns.map((run) => (
            <PendingPermissionView
              key={run.id}
              runId={run.id}
              pending={run.pendingPermission!}
              onRespond={respondPermission}
            />
          ))}
        </div>
      )}

      <Composer
        client={client}
        taskId={task.ID}
        connected={client !== null && connectionStatus === "connected"}
        runningRunId={runningRunId}
        onSubmit={submitPrompt}
        onStop={stopRun}
      />
    </div>
  );
}

/**
 * A pending permission request card, rendered in the dock pinned above the
 * prompt form (see TaskDetailPane): what's being requested, plus one
 * button per option. Clicking a button calls run.respondPermission (via
 * onRespond); this component never clears the pending state itself on
 * click -- the parent's pendingPermission prop disappearing (once a
 * "permission_resolved" event arrives, from this tab's own click or
 * another connection entirely) is what unmounts it, so both cases are
 * handled identically.
 */
function PendingPermissionView({
  runId,
  pending,
  onRespond,
}: {
  runId: string;
  pending: NonNullable<RunEntry["pendingPermission"]>;
  onRespond: (runId: string, requestId: string, optionId: string) => Promise<void>;
}) {
  const [respondingTo, setRespondingTo] = useState<string | null>(null);
  const [respondError, setRespondError] = useState<string | null>(null);

  async function handleClick(optionId: string) {
    setRespondingTo(optionId);
    setRespondError(null);
    try {
      await onRespond(runId, pending.requestId, optionId);
    } catch (err) {
      setRespondError(err instanceof Error ? err.message : String(err));
      setRespondingTo(null);
    }
    // On success, leave the buttons disabled: the run's own subscription
    // observes the matching "permission_resolved" event and this component
    // unmounts (pendingPermission clears) shortly -- no need to reset here.
  }

  return (
    <div className="mt-2">
      <Alert
        testId="pending-permission"
        variant="warning"
        title={pending.summary}
        description={respondError ? `respond failed: ${respondError}` : undefined}
      >
        {pending.options.map((option, index) => (
          <Button
            key={option.id}
            type="button"
            variant="outline"
            size="sm"
            className="h-6 px-2 text-xs"
            disabled={respondingTo !== null}
            data-testid={`chat-permission-option-${index}`}
            onClick={() => handleClick(option.id)}
          >
            {option.label}
          </Button>
        ))}
      </Alert>
    </div>
  );
}
