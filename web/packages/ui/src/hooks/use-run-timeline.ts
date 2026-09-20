import { useCallback, useEffect, useRef, useState, type Dispatch, type SetStateAction } from "react";

import type { WsClientLike } from "@/lib/ws-client";
import type {
  ApprovalPolicy,
  PermissionQuestion,
  PermissionRequestEventParams,
  PermissionResolutionReason,
  PermissionResolvedEventParams,
  Provider,
  RunAttachResult,
  RunLogEvent,
  RunLogsResult,
  RunSetApprovalPolicyResult,
  RunStartResult,
  RunStatusValue,
  RunSummary,
  ThinkingLevel,
  ToolCallStatus,
} from "@/lib/types";

/** A still-unanswered permission request on a run, as shown by the detail pane. */
export interface PendingPermission {
  requestId: string;
  summary: string;
  options: { id: string; label: string; kind: string }[];
  /** See PermissionQuestion's doc comment in lib/types.ts -- present only for a question-form-shaped request. */
  questions?: PermissionQuestion[];
  /** Present only for a plan-review-shaped request. */
  plan?: string;
}

/**
 * One rendered row of a run's transcript. The wire's event stream is a
 * flat sequence of small deltas (ADR 0008); this is the coalesced view of
 * it -- consecutive text chunks of the same role become one item, and a
 * tool call is one item however many events describe it.
 *
 * `id` is assigned once, at the moment the item is first appended, and
 * never changes. That is what lets the renderer memoize a row: a new
 * chunk produces a new array with a new *last* item and every earlier
 * item at its original object identity, so React re-renders exactly one
 * row instead of the whole transcript.
 */
export type TimelineItem = TimelineTextItem | TimelineToolCallItem | TimelinePermissionItem | TimelineUnknownItem;

/** Assistant output, the human's own turn, or the model's reasoning -- three roles, one shape. */
export interface TimelineTextItem {
  kind: "assistant" | "user" | "thinking";
  id: string;
  text: string;
}

/** One tool call, merged across every event carrying its toolCallId. */
export interface TimelineToolCallItem {
  kind: "tool_call";
  id: string;
  toolCallId: string;
  toolName?: string;
  title?: string;
  status: ToolCallStatus;
  input?: unknown;
  result?: unknown;
}

/**
 * A resolved permission request, rendered as a small marker row so the
 * transcript keeps a visible trace of *how* it was resolved -- a person
 * clicking an option, an auto-safe policy deciding for them, or the
 * request timing out unanswered (ui-redesign-parity.md's Validation
 * note). There is deliberately no "pending" counterpart item: while a
 * request is unanswered it lives only in `RunEntry.pendingPermission`
 * (the dock), and this item is created once, at the moment
 * "permission_resolved" arrives.
 */
export interface TimelinePermissionItem {
  kind: "permission";
  id: string;
  requestId: string;
  optionId: string;
  /** Absent for an older server payload with no reason field -- renders with no reason badge, not a crash. */
  reason?: PermissionResolutionReason;
}

/**
 * An event whose `type` this client does not know. The daemon's event
 * enum is append-only (ADR 0008), so a newer daemon talking to an older
 * tab is a supported state, not a bug -- it renders as a labelled
 * placeholder rather than throwing or silently vanishing.
 */
export interface TimelineUnknownItem {
  kind: "unknown";
  id: string;
  eventType: string;
}

/** The wire event names that carry a text delta, and the row role each becomes. */
const TEXT_EVENT_KINDS: Record<string, TimelineTextItem["kind"]> = {
  chunk: "assistant",
  user_message: "user",
  thinking: "thinking",
};

/**
 * Event names that are not transcript rows at all. "done" only drives run
 * status, and "permission_request" only drives the pending-permission
 * dock (RunEntry.pendingPermission) -- neither ever becomes an item.
 * "permission_resolved" is deliberately *not* in this set: it becomes a
 * TimelinePermissionItem, handled by its own branch below.
 */
const NON_ITEM_EVENTS = new Set(["done", "permission_request"]);

/**
 * Appends one wire event to a run's item list, returning a new array.
 *
 * Every branch preserves the object identity of items it did not touch
 * (see TimelineItem's doc comment for why that matters), and no branch
 * ever throws: a malformed event -- missing text, missing toolCallId, an
 * unrecognized type -- degrades to either "ignore" or "render a fallback
 * row", because a transcript that crashes on one bad event loses the
 * whole run's history with it.
 */
export function appendTimelineEvent(items: TimelineItem[], event: RunLogEvent): TimelineItem[] {
  const type = event.type;

  if (NON_ITEM_EVENTS.has(type)) return items;

  const textKind = TEXT_EVENT_KINDS[type];
  if (textKind) {
    const text = event.text ?? "";
    if (text === "") return items;
    const last = items[items.length - 1];
    if (last && last.kind === textKind) {
      // Coalesce into the run of text already in progress -- only the
      // last item gets a new object.
      const merged: TimelineTextItem = { ...last, text: last.text + text };
      return [...items.slice(0, -1), merged];
    }
    return [...items, { kind: textKind, id: `${textKind}-${items.length}`, text }];
  }

  if (type === "tool_call") {
    const toolCallId = event.toolCallId;
    if (!toolCallId) return items;
    const index = items.findIndex((item) => item.kind === "tool_call" && item.toolCallId === toolCallId);
    if (index === -1) {
      return [
        ...items,
        {
          kind: "tool_call",
          id: `tool-${items.length}`,
          toolCallId,
          toolName: event.toolName,
          title: event.title,
          // ACP's initial tool_call may omit status entirely; a call
          // that has been announced but not completed is running.
          status: event.status ?? "running",
          input: event.input,
          result: event.result,
        },
      ];
    }
    // Merge, never replace: a completion event legitimately omits
    // toolName/title/input, and those mean "unchanged", not "cleared".
    const existing = items[index] as TimelineToolCallItem;
    const merged: TimelineToolCallItem = {
      ...existing,
      toolName: event.toolName ?? existing.toolName,
      title: event.title ?? existing.title,
      status: event.status ?? existing.status,
      input: event.input ?? existing.input,
      result: event.result ?? existing.result,
    };
    const next = items.slice();
    next[index] = merged;
    return next;
  }

  if (type === "permission_resolved") {
    return [
      ...items,
      {
        kind: "permission",
        id: `permission-${items.length}`,
        requestId: event.requestId ?? "",
        optionId: event.optionId ?? "",
        reason: event.reason,
      },
    ];
  }

  return [...items, { kind: "unknown", id: `unknown-${items.length}`, eventType: type }];
}

/** Folds a whole backfilled event batch (run.logs) into the same item list the live stream builds. */
export function buildTimeline(events: RunLogEvent[]): TimelineItem[] {
  return events.reduce<TimelineItem[]>(appendTimelineEvent, []);
}

/** One run in a task's timeline, as rendered by the detail pane. */
export interface RunEntry {
  id: string;
  provider: Provider;
  prompt: string;
  status: RunStatusValue;
  startedAt: string;
  /** When the run reached a terminal state, if known -- drives the turn footer's elapsed time. */
  finishedAt?: string;
  /** The run's transcript: backfilled history plus, for a run still streaming, live events folded in as they arrive. */
  items: TimelineItem[];
  stopReason?: string;
  err?: string;
  /**
   * The run's approval policy -- "manual" (the default) if the run was
   * started without ever touching the composer's selector, or whatever it
   * was live-switched to since (see setApprovalPolicy below and
   * docs/plans/active/mid-run-approval-and-retry-effort.md's Item A).
   */
  approvalPolicy: ApprovalPolicy;
  /**
   * The run's Claude-only thinking level, "" (unset) for every other
   * provider or an untouched selector -- immutable for the run's lifetime
   * (unlike approvalPolicy, there is no live thinking-level switch; see
   * retryWithHigherEffort below for how a *new* run gets a higher tier).
   */
  thinkingLevel: ThinkingLevel;
  /**
   * The run's most recent still-unanswered permission request, or
   * undefined if none is currently pending. Set when a "permission_request"
   * event arrives (live, or replayed from run.attach's backfill for a
   * request nobody has answered yet) and cleared when the matching
   * "permission_resolved" event arrives -- from *any* connection, not just
   * one originating from this tab's own respondPermission call, since
   * another connection could answer first.
   */
  pendingPermission?: PendingPermission;
}

interface TimelineState {
  /** null while the initial run.list fetch for the current task is in flight. */
  runs: RunEntry[] | null;
  error: string | null;
  /**
   * Starts a new run (run.start) and immediately begins streaming it
   * (run.attach) into the timeline. approvalPolicy is omitted from the
   * run.start payload entirely when it's "manual" -- the backend's default
   * when the field is absent (internal/wsapi/handlers.go) -- so a run
   * started without ever touching the approval-policy selector sends the
   * exact same payload as before that selector existed. thinkingLevel is
   * the same story: omitted whenever it's "" (unset -- every non-Claude
   * provider, and Claude before the selector is touched), so a run started
   * without ever touching the thinking-level selector sends the exact same
   * payload as before that selector existed either.
   */
  submitPrompt: (
    provider: Provider,
    prompt: string,
    approvalPolicy?: ApprovalPolicy,
    thinkingLevel?: ThinkingLevel,
  ) => Promise<void>;
  /**
   * Actually stops runId server-side (run.stop) -- unlike task switch/
   * unmount, which only ever detach. The run's own active run.attach
   * subscription (if any) observes the stop as its terminal response, same
   * as any other subscriber, and updates its status the normal way; this
   * function's caller doesn't need to patch state itself.
   */
  stopRun: (runId: string) => Promise<void>;
  /**
   * Answers runId's pending permission request requestId with optionId
   * (run.respondPermission), from this connection. Never clears
   * pendingPermission itself: the subsequent "permission_resolved" event --
   * which arrives here the same way regardless of whether this call or a
   * different connection answered first -- is what does that, so both
   * cases are handled by the exact same code path.
   */
  respondPermission: (runId: string, requestId: string, optionId: string) => Promise<void>;
  /**
   * Switches runId's live approvalPolicy between "manual" and "auto-safe"
   * (run.setApprovalPolicy) -- see Item A. Only meaningful while the run is
   * still running; the caller is expected to have already gated the
   * control that invokes this on that (see task-detail.tsx).
   */
  setApprovalPolicy: (runId: string, policy: ApprovalPolicy) => Promise<void>;
  /**
   * Starts a new run for the same task as `run`, one thinking tier above
   * `run.thinkingLevel` (see nextThinkingTier), with the same prompt,
   * provider, and approvalPolicy -- Item B's "Retry with higher effort". A
   * no-op if there's no higher tier to try (the caller is expected to have
   * already gated the button that invokes this on canRetryWithHigherEffort).
   */
  retryWithHigherEffort: (run: RunEntry) => Promise<void>;
}

/**
 * Claude's thinking tiers in order, "" (unspecified) deliberately excluded:
 * per the plan doc's Acceptance Criteria, "Retry with higher effort" only
 * ever offers a *known* higher tier to try (off -> standard -> extended),
 * not a guess at what an unset selector's "next" tier would even mean.
 */
const NEXT_THINKING_TIER: Partial<Record<ThinkingLevel, ThinkingLevel>> = {
  off: "standard",
  standard: "extended",
};

/** The next thinking tier up from level, or null if there isn't one (already "extended", or level is "" / unrecognized). */
export function nextThinkingTier(level: ThinkingLevel): ThinkingLevel | null {
  return NEXT_THINKING_TIER[level] ?? null;
}

/**
 * Whether run's failure state should offer "Retry with higher effort"
 * (Item B): a Claude-native run, ended in error, with a higher thinking
 * tier left to try. A successful run, a non-Claude provider, or a run
 * already at "extended" never qualifies.
 */
export function canRetryWithHigherEffort(run: RunEntry): boolean {
  return run.status === "error" && run.provider === "claude-native" && nextThinkingTier(run.thinkingLevel) !== null;
}

/** Tracks one task-selection's lifetime: guards async continuations from a superseded selection, and lets an active run.attach be aborted (detached, not stopped) on task switch or unmount. */
interface Session {
  cancelled: boolean;
  controller: AbortController;
}

function patch(setRuns: Dispatch<SetStateAction<RunEntry[] | null>>, id: string, changes: Partial<RunEntry>): void {
  setRuns((prev) => (prev ? prev.map((r) => (r.id === id ? { ...r, ...changes } : r)) : prev));
}

function appendEvent(setRuns: Dispatch<SetStateAction<RunEntry[] | null>>, id: string, event: RunLogEvent): void {
  setRuns((prev) =>
    prev
      ? prev.map((r) => {
          if (r.id !== id) return r;
          const items = appendTimelineEvent(r.items, event);
          // Identical array means the event changed nothing (an empty
          // chunk, a "done") -- returning the same run object keeps the
          // whole list's identity stable and skips the re-render.
          return items === r.items ? r : { ...r, items };
        })
      : prev,
  );
}

function setPendingPermission(
  setRuns: Dispatch<SetStateAction<RunEntry[] | null>>,
  runId: string,
  params: PermissionRequestEventParams,
): void {
  patch(setRuns, runId, {
    pendingPermission: {
      requestId: params.requestId,
      summary: params.summary,
      options: params.options,
      questions: params.questions,
      plan: params.plan,
    },
  });
}

/**
 * Clears runId's pendingPermission, but only if it's still the request
 * being resolved -- guarding against an out-of-order/duplicate
 * "permission_resolved" event clearing a *later* pending request that
 * happened to arrive first (defensive; the server only ever resolves one
 * request at a time per internal/runs.Registry's own invariants, but the
 * UI shouldn't rely on wire-ordering guarantees it hasn't verified).
 */
function clearPendingPermission(
  setRuns: Dispatch<SetStateAction<RunEntry[] | null>>,
  runId: string,
  params: PermissionResolvedEventParams,
): void {
  setRuns((prev) =>
    prev
      ? prev.map((r) => (r.id === runId && r.pendingPermission?.requestId === params.requestId ? { ...r, pendingPermission: undefined } : r))
      : prev,
  );
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
        if (event === "permission_request") {
          setPendingPermission(setRuns, runId, params as PermissionRequestEventParams);
          return;
        }
        if (event === "permission_resolved") {
          clearPendingPermission(setRuns, runId, params as PermissionResolvedEventParams);
          // Also falls through to appendEvent below, so the resolution
          // still lands its own TimelinePermissionItem -- clearing the
          // dock and recording the trace are two separate effects of the
          // same event, not a fork in how it's handled.
        }
        // Everything else goes through the same reducer the backfill
        // does, so a live event and a replayed one can't diverge. The
        // streamed shape is the batched one minus its `type` field
        // (internal/wsapi/handlers.go emits the same params either way),
        // so re-attaching the name is the whole translation.
        appendEvent(setRuns, runId, { ...(params as object), type: event } as RunLogEvent);
      },
      { signal: session.controller.signal },
    )
    .then((result) => {
      if (session.cancelled) return;
      patch(setRuns, runId, { status: "done", stopReason: result.stopReason, finishedAt: new Date().toISOString() });
    })
    .catch(() => {
      if (session.cancelled) return;
      client
        .call<RunLogsResult>("run.logs", { runId })
        .then((logs) => {
          if (session.cancelled) return;
          patch(setRuns, runId, {
            status: logs.status,
            stopReason: logs.stopReason,
            err: logs.err,
            finishedAt: new Date().toISOString(),
          });
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
          finishedAt: r.FinishedAt ?? undefined,
          items: [],
          stopReason: r.StopReason || undefined,
          err: r.Err || undefined,
          approvalPolicy: r.ApprovalPolicy,
          thinkingLevel: r.ThinkingLevel,
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
                  items: buildTimeline(logs.events),
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
    async (provider: Provider, prompt: string, approvalPolicy?: ApprovalPolicy, thinkingLevel?: ThinkingLevel) => {
      const session = sessionRef.current;
      if (!client || taskId === null || !session) {
        throw new Error("no task selected");
      }

      const { runId } = await client.call<RunStartResult>("run.start", {
        taskId,
        provider,
        prompt,
        ...(approvalPolicy && approvalPolicy !== "manual" ? { approvalPolicy } : {}),
        ...(thinkingLevel ? { thinkingLevel } : {}),
      });
      if (session.cancelled) return;

      const entry: RunEntry = {
        id: runId,
        provider,
        prompt,
        status: "running",
        startedAt: new Date().toISOString(),
        items: [],
        approvalPolicy: approvalPolicy ?? "manual",
        thinkingLevel: thinkingLevel ?? "",
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

  const respondPermission = useCallback(
    async (runId: string, requestId: string, optionId: string) => {
      if (!client) throw new Error("not connected");
      await client.call("run.respondPermission", { runId, requestId, optionId });
    },
    [client],
  );

  const setApprovalPolicy = useCallback(
    async (runId: string, policy: ApprovalPolicy) => {
      if (!client) throw new Error("not connected");
      const result = await client.call<RunSetApprovalPolicyResult>("run.setApprovalPolicy", { runId, policy });
      patch(setRuns, runId, { approvalPolicy: result.approvalPolicy });
    },
    [client],
  );

  const retryWithHigherEffort = useCallback(
    async (run: RunEntry) => {
      const tier = nextThinkingTier(run.thinkingLevel);
      if (!tier) return;
      await submitPrompt(run.provider, run.prompt, run.approvalPolicy, tier);
    },
    [submitPrompt],
  );

  return { runs, error, submitPrompt, stopRun, respondPermission, setApprovalPolicy, retryWithHigherEffort };
}
