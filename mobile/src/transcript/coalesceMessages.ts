// coalesceMessages.ts turns the flat TimelineLine[] (runTimeline.ts: one line
// per streamed chunk) plus the permission board and the not-finished footer
// into the TranscriptItem[] the task transcript list renders. Pure: no
// react-native import, no mutation of its inputs.
//
// Rules: consecutive 'assistant' lines merge into ONE item (text = chunks
// concatenated in order, key = the FIRST line's key, so a streaming append
// re-renders only that item); consecutive 'assistant (thinking)' lines merge
// the same way; a tool-call line, a user line or a meta line breaks a run.
// Permissions (key `perm-<requestId>`) and the footer (key `footer`) are
// appended after the lines, in that order.

import type { PermissionRequestState } from '../permissionRequests';
import type { TimelineLine } from '../runTimeline';

export type TranscriptItem =
  | { type: 'user'; key: string; text: string }
  | { type: 'assistant'; key: string; text: string }
  | { type: 'thinking'; key: string; text: string }
  | { type: 'tool'; key: string; toolName: string; title: string; status: string }
  | { type: 'meta'; key: string; text: string }
  | { type: 'permission'; key: string; request: PermissionRequestState }
  | { type: 'footer'; key: string; sentence: string };

export type TranscriptItemType = TranscriptItem['type'];

const THINKING_ROLE = 'assistant (thinking)';

export function buildTranscript(
  lines: readonly TimelineLine[],
  permissions: readonly PermissionRequestState[] = [],
  footer: { sentence: string } | null = null,
): TranscriptItem[] {
  const items: TranscriptItem[] = [];
  for (const line of lines) {
    if (line.toolCall) {
      items.push({ type: 'tool', key: line.key, ...line.toolCall });
      continue;
    }
    const last = items[items.length - 1];
    if (line.role === 'assistant' || line.role === THINKING_ROLE) {
      const type = line.role === 'assistant' ? 'assistant' : 'thinking';
      if (last && last.type === type) last.text += line.text;
      else items.push({ type, key: line.key, text: line.text });
    } else if (line.role === 'user') {
      items.push({ type: 'user', key: line.key, text: line.text });
    } else {
      items.push({ type: 'meta', key: line.key, text: line.text });
    }
  }
  for (const request of permissions) items.push({ type: 'permission', key: `perm-${request.requestId}`, request });
  if (footer) items.push({ type: 'footer', key: 'footer', sentence: footer.sentence });
  return items;
}
