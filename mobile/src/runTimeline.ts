// runTimeline.ts turns run.logs' batched history plus run.attach's live
// stream into one flat list of renderable lines -- Item 3's honest
// rendering per the plan: role-prefixed text lines and a tool-call
// name+status line are enough (the web UI's full per-tool fidelity is
// explicitly not required). Wire shapes are ADR 0008's, as implemented by
// internal/wsapi/handlers.go's runLogEvent/attachAndStream.

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
   */
  toolCall?: { toolName: string; title: string; status: string };
}

interface RunLogEvent {
  type: string;
  text?: string;
  stopReason?: string;
  toolName?: string;
  title?: string;
  status?: string;
}

/** Render one run.logs entry into zero or more lines. */
function renderLogEvent(ev: RunLogEvent, idx: number): TimelineLine[] {
  switch (ev.type) {
    case 'user_message':
      return [{ key: `e${idx}`, role: 'user', text: ev.text ?? '' }];
    case 'chunk':
      return [{ key: `e${idx}`, role: 'assistant', text: ev.text ?? '' }];
    case 'thinking':
      return [{ key: `e${idx}`, role: 'assistant (thinking)', text: ev.text ?? '' }];
    case 'tool_call':
      return [
        {
          key: `e${idx}`,
          role: '',
          text: `tool: ${ev.toolName ?? '?'}${ev.title ? ` — ${ev.title}` : ''}${ev.status ? ` [${ev.status}]` : ''}`,
          toolCall: { toolName: ev.toolName ?? '?', title: ev.title ?? '', status: ev.status ?? '' },
        },
      ];
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
export function timelineFromLogs(logsResult: { events?: RunLogEvent[] }): TimelineLine[] {
  return (logsResult.events ?? []).flatMap(renderLogEvent);
}

/** One live run.attach event (event name + params) appended to the timeline. */
export function lineFromAttachEvent(event: string, params: unknown, seq: number): TimelineLine[] {
  const p = (params ?? {}) as RunLogEvent & { text?: string };
  const asLog: Record<string, RunLogEvent> = {
    chunk: { type: 'chunk', text: p.text },
    user_message: { type: 'user_message', text: p.text },
    thinking: { type: 'thinking', text: p.text },
    tool_call: { type: 'tool_call', toolName: p.toolName, title: p.title, status: p.status },
  };
  const mapped = asLog[event];
  if (mapped) return renderLogEvent(mapped, seq);
  if (event === 'done') return renderLogEvent({ type: 'done', stopReason: (p as { stopReason?: string }).stopReason }, seq);
  return [];
}
