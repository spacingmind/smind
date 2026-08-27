import { useCallback, useEffect, useRef, useState, type Dispatch, type SetStateAction } from "react";

import type { WsClientLike } from "@/lib/ws-client";
import type {
  Provider,
  RunAttachResult,
  RunChunkEventParams,
  RunLogEvent,
  RunLogsResult,
  RunStartResult,
  RunStatusValue,
  RunSummary,
} from "@/lib/types";

/** One run in a task's timeline, as rendered by the detail pane. */
export interface RunEntry {
  id: string;
  provider: Provider;
  prompt: string;
  status: RunStatusValue;
  startedAt: string;
  /** Collected text -- backfilled history plus, for a run still streaming, live chunks appended as they arrive. */
  text: string;
  stopReason?: string;
  err?: string;
}

interface TimelineState {
  /** null while the initial run.list fetch for the current task is in flight. */
  runs: RunEntry[] | null;
  error: string | null;
  /** Starts a new run (run.start) and immediately begins streaming it (run.attach) into the timeline. */
  submitPrompt: (provider: Provider, prompt: string) => Promise<void>;
  /**
   * Actually stops runId server-side (run.stop) -- unlike task switch/
   * unmount, which only ever detach. The run's own active run.attach
   * subscription (if any) observes the stop as its terminal response, same
   * as any other subscriber, and updates its status the normal way; this
   * function's caller doesn't need to patch state itself.
   */
  stopRun: (runId: string) => Promise<void>;
}

/** Tracks one task-selection's lifetime: guards async continuations from a superseded selection, and lets an active run.attach be aborted (detached, not stopped) on task switch or unmount. */
interface Session {
  cancelled: boolean;
  controller: AbortController;
}

function collectText(events: RunLogEvent[]): string {
  return events
    .filter((e) => e.type === "chunk")
    .map((e) => e.text ?? "")
    .join("");
}

function patch(setRuns: Dispatch<SetStateAction<RunEntry[] | null>>, id: string, changes: Partial<RunEntry>): void {
  setRuns((prev) => (prev ? prev.map((r) => (r.id === id ? { ...r, ...changes } : r)) : prev));
}

function appendChunk(setRuns: Dispatch<SetStateAction<RunEntry[] | null>>, id: string, text: string): void {
  setRuns((prev) => (prev ? prev.map((r) => (r.id === id ? { ...r, text: r.text + text } : r)) : prev));
}

/**
 * Subscribes runId's remaining live output into the timeline via
 * run.attach: backfill of everything emitted so far, then the live tail
 * (see internal/wsapi/handlers.go's handleRunAttach). session.controller's
 * signal is what detaches this subscription on task switch/unmount --
 * never run.stop, matching the "switching tasks/closing the tab only
 * detaches" requirement in docs/plans/active/web-ui-task-detail.md.
 *
 * On successful completion, the server's own terminal result carries the
 * authoritative stopReason. On failure, if it wasn't our own abort (i.e.
 * the run itself ended in error/stopped), a best-effort run.logs call
 * fetches the authoritative terminal status -- the live text already
 * accumulated via streamed chunks needs no re-fetching, since attach
 * forwards every chunk event regardless of how the run ends.
 */
function streamRun(client: WsClientLike, session: Session, setRuns: Dispatch<SetStateAction<RunEntry[] | null>>, runId: string): void {
  client
    .callStream<RunAttachResult>(
      "run.attach",
      { runId },
      (event, params) => {
        if (session.cancelled) return;
        if (event === "chunk") {
          appendChunk(setRuns, runId, (params as RunChunkEventParams).text);
        }
      },
      { signal: session.controller.signal },
    )
    .then((result) => {
      if (session.cancelled) return;
      patch(setRuns, runId, { status: "done", stopReason: result.stopReason });
    })
    .catch(() => {
      if (session.cancelled) return;
      client
        .call<RunLogsResult>("run.logs", { runId })
        .then((logs) => {
          if (session.cancelled) return;
          patch(setRuns, runId, { status: logs.status, stopReason: logs.stopReason, err: logs.err });
        })
        .catch(() => {
          // Best-effort finalize only -- the timeline already shows
          // everything streamed before the run ended.
        });
    });
}

/**
 * Loads taskId's run history and keeps it live. run.list has no
 * server-side filter (see internal/wsapi/handlers.go's handleRunList), so
 * filtering to this task and sorting chronologically happens here. Any run
 * still `running` when the pane opens is followed via streamRun
 * (backfill + live tail) regardless of which connection started it --
 * that cross-connection reattach is this feature's whole point. Already-
 * terminal runs get their full history via a one-shot run.logs.
 *
 * Switching taskId (or unmounting) aborts any in-flight run.attach
 * subscriptions and guards every async continuation via a per-selection
 * Session so a stale fetch from a superseded taskId can never clobber the
 * current selection's view -- the same `cancelled`-flag pattern
 * app-sidebar.tsx's useWorkspaceTree already established, extended here to
 * also cover live subscriptions (not just one-shot fetches).
 */
export function useRunTimeline(client: WsClientLike | null, taskId: number | null): TimelineState {
  const [runs, setRuns] = useState<RunEntry[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const sessionRef = useRef<Session | null>(null);

  useEffect(() => {
    setRuns(null);
    setError(null);

    if (!client || taskId === null) {
      sessionRef.current = null;
      return;
    }

    const session: Session = { cancelled: false, controller: new AbortController() };
    sessionRef.current = session;

    client
      .call<RunSummary[]>("run.list")
      .then((list) => {
        if (session.cancelled) return;

        const mine = [...list].filter((r) => r.TaskID === taskId).sort((a, b) => a.StartedAt.localeCompare(b.StartedAt));

        const initial: RunEntry[] = mine.map((r) => ({
          id: r.ID,
          provider: r.Provider,
          prompt: r.Prompt,
          status: r.Status,
          startedAt: r.StartedAt,
          text: "",
          stopReason: r.StopReason || undefined,
          err: r.Err || undefined,
        }));
        setRuns(initial);

        for (const r of mine) {
          if (r.Status === "running") {
            streamRun(client, session, setRuns, r.ID);
          } else {
            client
              .call<RunLogsResult>("run.logs", { runId: r.ID })
              .then((logs) => {
                if (session.cancelled) return;
                patch(setRuns, r.ID, {
                  text: collectText(logs.events),
                  status: logs.status,
                  stopReason: logs.stopReason,
                  err: logs.err,
                });
              })
              .catch((err: unknown) => {
                if (!session.cancelled) setError(err instanceof Error ? err.message : String(err));
              });
          }
        }
      })
      .catch((err: unknown) => {
        if (!session.cancelled) setError(err instanceof Error ? err.message : String(err));
      });

    return () => {
      session.cancelled = true;
      session.controller.abort();
      if (sessionRef.current === session) sessionRef.current = null;
    };
  }, [client, taskId]);

  const submitPrompt = useCallback(
    async (provider: Provider, prompt: string) => {
      const session = sessionRef.current;
      if (!client || taskId === null || !session) {
        throw new Error("no task selected");
      }

      const { runId } = await client.call<RunStartResult>("run.start", { taskId, provider, prompt });
      if (session.cancelled) return;

      const entry: RunEntry = {
        id: runId,
        provider,
        prompt,
        status: "running",
        startedAt: new Date().toISOString(),
        text: "",
      };
      setRuns((prev) => (prev ? [...prev, entry] : [entry]));

      streamRun(client, session, setRuns, runId);
    },
    [client, taskId],
  );

  const stopRun = useCallback(
    async (runId: string) => {
      if (!client) throw new Error("not connected");
      await client.call("run.stop", { runId });
    },
    [client],
  );

  return { runs, error, submitPrompt, stopRun };
}
