import { useCallback, useRef } from "react";
import { ArrowDown } from "lucide-react";

import { Composer } from "@/components/composer/composer";
import { PermissionCard } from "@/components/permission/permission-card";
import { useDetailLevel } from "@/components/timeline/detail-level";
import { RunTimeline } from "@/components/timeline/run-timeline";
import { useAutoFollow } from "@/components/timeline/use-auto-follow";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { InlineSpinner } from "@/components/ui/inline-spinner";
import { PaneHeader } from "@/components/ui/pane-header";
import { useRunTimeline } from "@/hooks/use-run-timeline";
import { useTaskDiff } from "@/hooks/use-task-diff";
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
  onOpenDiffTab,
}: {
  client: WsClientLike | null;
  task: Task;
  /** Real-time connection status from App.tsx -- lets an active run.attach subscription visibly reflect a break instead of silently freezing on stale "live" output. Defaults to "connected" so every existing caller/test not wired up to App.tsx's status keeps behaving exactly as before. */
  connectionStatus?: ConnectionStatus;
  /** Opens a worktree-relative path as a file tab. Optional: without it, a tool-call card naming a file simply isn't click-through. */
  onOpenFile?: (path: string) => void;
  /** Brings the task's Diff tab forward -- the composer's diff-stat pill's click target. Optional: without it (no tab strip above) the pill is omitted. */
  onOpenDiffTab?: () => void;
}) {
  const { runs, error, submitPrompt, stopRun, respondPermission } = useRunTimeline(client, task.ID);
  // The composer's diff-stat pill (web-ui-dogfood-polish Item 5) reads the
  // same task.diff this is -- the same hook the diff pane itself uses, so
  // the pill and the pane's header stat can't disagree and no extra RPC
  // is introduced. It stays mounted even when the Diff tab isn't (the
  // fetch is cheap and the refresh signal is identical).
  const wholeDiff = useTaskDiff(client, task, undefined);

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

  const composerTextareaRef = useRef<HTMLTextAreaElement | null>(null);
  const focusComposer = useCallback(() => composerTextareaRef.current?.focus(), []);

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
          {/*
           * Dogfood Item 2: the chat timeline is a reading column, not a
           * pane -- cap it (and center it) on wide screens instead of
           * stretching line length edge-to-edge. The wrapper lives
           * *inside* the scroll container (so it scrolls with the log)
           * and only the chat tab gets it: files/diff/terminal render
           * their own full-width roots.
           */}
          <div data-testid="run-log-column" className="mx-auto max-w-3xl">
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
       * scrolled or how much new output streams in (see the plan's Item 3)
       * -- and aligned to the same reading column as that log (dogfood
       * Item 2): mx-auto with the same max-w-3xl keeps a permission card
       * sitting at the bottom of the log visually continuous with it on
       * wide screens.
       */}
      {pendingRuns.length > 0 && (
        <div data-testid="pending-permission-dock" className="mx-auto w-full max-w-3xl shrink-0 border-t bg-background px-4 py-2">
          {pendingRuns.map((run) => (
            <PermissionCard
              key={run.id}
              runId={run.id}
              pending={run.pendingPermission!}
              onRespond={respondPermission}
              onChat={focusComposer}
            />
          ))}
        </div>
      )}

      <Composer
        client={client}
        taskId={task.ID}
        connected={client !== null && connectionStatus === "connected"}
        runningRunId={runningRunId}
        diffStat={wholeDiff.stat}
        onOpenDiff={onOpenDiffTab}
        onSubmit={submitPrompt}
        onStop={stopRun}
        textareaRef={composerTextareaRef}
      />
    </div>
  );
}
