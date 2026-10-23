// attachSend.ts: the decision logic of a send that may carry attachments,
// kept out of TaskDetailScreen so it is testable without React: the inline
// re-check before the daemon is called, and how a failure is reported. The
// screen keeps the draft and the tray on anything but 'sent'.

import { AttachmentPayload } from './api';
import { attachmentErrorMessage, checkSend, PendingAttachment, RunStartBase } from './attachments';

const ATTACHMENT_CODES = new Set([
  'attachment_too_large',
  'too_many_attachments',
  'unsupported_image_type',
  'mime_mismatch',
  'invalid_base64',
  'invalid_attachment',
  'outside_workspace',
  'not_found',
]);

/** The wire payload: kind, name, mimeType and data only -- never the local uri or size. */
export function attachmentPayloads(pending: PendingAttachment[]): AttachmentPayload[] {
  return pending.map((a) => ({ kind: a.kind, name: a.name, mimeType: a.mimeType, data: a.data }));
}

export type SendOutcome<T> =
  | { kind: 'refused'; notice: string }
  | { kind: 'sent'; result: T }
  | { kind: 'failed'; message: string; /** true: show `message` as the attachment notice; false: as "Couldn't send: ..." */ attachment: boolean };

/** Re-checks the exact request, then sends; the daemon is not called when it no longer fits. */
export async function sendWithAttachments<T>(base: RunStartBase, pending: PendingAttachment[], send: () => Promise<T>): Promise<SendOutcome<T>> {
  const notice = checkSend(base, pending);
  if (notice !== null) return { kind: 'refused', notice };
  try {
    return { kind: 'sent', result: await send() };
  } catch (err) {
    const e = err as { code?: string; message?: string };
    if (pending.length > 0 && e.code !== undefined && ATTACHMENT_CODES.has(e.code)) {
      return { kind: 'failed', message: attachmentErrorMessage(e, pending.map((a) => a.name)), attachment: true };
    }
    return { kind: 'failed', message: err instanceof Error ? err.message : String(err), attachment: false };
  }
}
