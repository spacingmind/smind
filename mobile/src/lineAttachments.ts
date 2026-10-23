// lineAttachments.ts: builds the attachment metadata a user timeline line
// carries (ADR-0038) -- from a run.list row (a sent prompt) or from the
// composer's pending list (the optimistic line). Pure.

import { RunSummary } from './api';
import { PendingAttachment, timelineAttachmentView } from './attachments';
import { trayPills } from './attachmentTray';
import { LineAttachment } from './runTimeline';

/** Undefined when the run carries none, so a line with no attachments stays shaped as before. */
export function lineAttachmentsFromRun(run: Pick<RunSummary, 'ID' | 'attachments'>): LineAttachment[] | undefined {
  const view = timelineAttachmentView(run.attachments);
  return view.length === 0 ? undefined : view.map((a) => ({ ...a, runId: run.ID }));
}

/** The optimistic line's pills: local thumbnails, never fetched. */
export function lineAttachmentsFromPending(pending: PendingAttachment[]): LineAttachment[] | undefined {
  if (pending.length === 0) return undefined;
  return trayPills(pending).map((p) => ({ id: p.id, label: p.label, sizeLabel: p.sizeLabel, kind: p.kind, thumbnail: false, uri: p.thumbUri }));
}
