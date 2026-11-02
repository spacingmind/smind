// runTimeline.ts turns run.logs' batched history plus run.attach's live
// stream into one flat list of renderable lines -- Item 3's honest
// rendering per the plan: role-prefixed text lines and a tool-call
// name+status line are enough (the web UI's full per-tool fidelity is
// explicitly not required). Wire shapes are ADR 0008's, as implemented by
// internal/wsapi/handlers.go's runLogEvent/attachAndStream.

/** A sent prompt's attachment as the timeline shows it: metadata only, plus a local uri on an optimistic line and the run id a lazy thumbnail fetch needs. */
export interface LineAttachment {
  id: string;
  label: string;
  sizeLabel: string;
  kind: string;
  /** run.attachment would fit the frame (attachments.canThumbnail). */
  thumbnail: boolean;
  /** Local image uri: set on the optimistic line, so no fetch is needed. */
  uri?: string;
  /** The run whose run.attachment serves this item. */
  runId?: string;
}

/** One elided heredoc of a tool call or permission detail: the marker's index and the body's byte count. */
export interface HeredocRef {
  index: number;
  bytes: number;
}

/** One rendered timeline line. */
export interface TimelineLine {
  key: string;
  /** e.g. "user", "assistant", "assistant (thinking)" -- empty for tool/status lines. */
  role: string;
  text: string;
  /**
   * Present only for tool-call lines: the real 3-state status
   * (internal/taskrunner/event.go's ToolStatusRunning/Success/Failure --
   * no "queued" or "pending" state exists) plus name/title, so the UI
   * can render a collapsed row with a status icon instead of parsing
   * `text`. Item 3 (mobile-ui-polish plan)'s reason for this field.
   * toolCallId/heredocs/runId (heredoc reveal) feed the
   * run.revealToolHeredoc toggles; all three optional because older
   * events carry none.
   */
  toolCall?: { toolName: string; title: string; status: string; toolCallId?: string; heredocs?: HeredocRef[]; runId?: string };
  /** User lines only: the prompt's attachments (ADR-0038), shown read-only under the message. */
  attachments?: LineAttachment[];
}

interface RunLogEvent {
  type: string;
  text?: string;
  stopReason?: string;
  toolName?: string;
  title?: string;
  status?: string;
  /** Elided heredoc refs (wsapi toRunToolCallParams) -- the reveal toggle's feed. */
  toolCallId?: string;
  heredocs?: HeredocRef[];
}

/** Render one run.logs entry into zero or more lines. */
function renderLogEvent(ev: RunLogEvent, idx: number, runId?: string): TimelineLine[] {
  switch (ev.type) {
    case 'user_message':
      return [{ key: `e${idx}`, role: 'user', text: ev.text ?? '' }];
    case 'chunk':
      return [{ key: `e${idx}`, role: 'assistant', text: ev.text ?? '' }];
    case 'thinking':
      return [{ key: `e${idx}`, role: 'assistant (thinking)', text: ev.text ?? '' }];
    case 'tool_call': {
      const toolCall: NonNullable<TimelineLine['toolCall']> = {
        toolName: ev.toolName ?? '?',
        title: ev.title ?? '',
        status: ev.status ?? '',
      };
      if (ev.toolCallId) toolCall.toolCallId = ev.toolCallId;
      if (ev.heredocs) toolCall.heredocs = ev.heredocs;
      if (runId) toolCall.runId = runId;
      return [
        {
          key: `e${idx}`,
          role: '',
          text: `tool: ${ev.toolName ?? '?'}${ev.title ? ` — ${ev.title}` : ''}${ev.status ? ` [${ev.status}]` : ''}`,
          toolCall,
        },
      ];
    }
    case 'done':
      return [{ key: `e${idx}`, role: '', text: `done (${ev.stopReason ?? ''})` }];
    // permission_request/permission_resolved render through
    // PermissionBoard (Milestone 3 Item 2) -- summary plus tappable
    // option buttons while pending, resolved state after -- not as text
    // lines here, which would double-render them.
    default:
      return [];
  }
}

/** The full history from a run.logs result: {runId, status, events: [...]}. */
export function timelineFromLogs(logsResult: { runId?: string; events?: RunLogEvent[] }): TimelineLine[] {
  const out: TimelineLine[] = [];
  (logsResult.events ?? []).forEach((ev, i) => out.push(...renderLogEvent(ev, i, logsResult.runId)));
  return out;
}

/** One live run.attach event (event name + params) appended to the timeline. */
export function lineFromAttachEvent(event: string, params: unknown, seq: number, runId?: string): TimelineLine[] {
  const p = (params ?? {}) as RunLogEvent & { text?: string };
  const asLog: Record<string, RunLogEvent> = {
    chunk: { type: 'chunk', text: p.text },
    user_message: { type: 'user_message', text: p.text },
    thinking: { type: 'thinking', text: p.text },
    tool_call: { type: 'tool_call', toolName: p.toolName, title: p.title, status: p.status, toolCallId: p.toolCallId, heredocs: p.heredocs },
  };
  const mapped = asLog[event];
  if (mapped) return renderLogEvent(mapped, seq, runId);
  if (event === 'done') return renderLogEvent({ type: 'done', stopReason: (p as { stopReason?: string }).stopReason }, seq, runId);
  return [];
}
