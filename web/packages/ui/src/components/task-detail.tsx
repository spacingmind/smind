import { useState } from "react";

import { Composer } from "@/components/composer/composer";
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
}: {
  client: WsClientLike | null;
  task: Task;
  /** Real-time connection status from App.tsx -- lets an active run.attach subscription visibly reflect a break instead of silently freezing on stale "live" output. Defaults to "connected" so every existing caller/test not wired up to App.tsx's status keeps behaving exactly as before. */
  connectionStatus?: ConnectionStatus;
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
      />

      {connectionStatus === "reconnecting" && (
        <Alert
          testId="connection-banner"
          variant="warning"
          className="rounded-none border-x-0 border-t-0"
          description="Connection lost -- reconnecting to daemon…"
        />
      )}

      <div data-testid="run-log-scroll" className="flex-1 overflow-y-auto px-4 py-3">
        {error && <Alert variant="error" description={error} />}
        {!error && runs === null && <InlineSpinner label="Loading runs…" />}
        {!error && runs !== null && runs.length === 0 && (
          <EmptyState title="No runs yet" description="Send a prompt to start one" />
        )}
        {runs !== null && runs.length > 0 && (
          <ul className="space-y-4">
            {runs.map((run) => (
              <RunEntryView key={run.id} run={run} />
            ))}
          </ul>
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
 * One run in the task's log: its provider/status header, the prompt that
 * started it, and its collected output. Stopping a live run lives in the
 * composer now (ui-redesign-parity Item 10) rather than here -- one Stop
 * control per task, always in the same place, instead of one per run card
 * that scrolls away mid-run.
 */
function RunEntryView({ run }: { run: RunEntry }) {
  return (
    <li data-testid="run-entry" data-run-id={run.id} className="rounded-lg border p-3">
      <div className="flex items-center justify-between text-xs text-muted-foreground">
        <span>{run.provider}</span>
        <span className="uppercase" data-testid="run-status">
          {run.status}
        </span>
      </div>
      <p className="mt-1 text-sm font-medium">{run.prompt}</p>
      <pre className="mt-2 whitespace-pre-wrap text-sm" data-testid="run-text">
        {run.text}
      </pre>
      {run.err && <p className="mt-1 text-xs text-destructive">{run.err}</p>}
    </li>
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
