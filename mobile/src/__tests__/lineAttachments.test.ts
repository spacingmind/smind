import { describe, expect, it } from 'vitest';
import { lineAttachmentsFromPending, lineAttachmentsFromRun } from '../lineAttachments';
import { withRunPrompt } from '../promptLine';
import { buildTranscript } from '../transcript/coalesceMessages';
import { TimelineLine } from '../runTimeline';

describe('line attachments', () => {
  it('a run row carries its attachment metadata onto the user line', () => {
    const atts = lineAttachmentsFromRun({ ID: 'r1', attachments: [{ id: 'x', kind: 'image', name: 'p.jpg', size: 5000 }, { id: 'y', kind: 'file', name: 'n.txt', size: 12 }] });
    expect(atts).toEqual([
      { id: 'x', label: 'p.jpg', sizeLabel: '5 KB', kind: 'image', thumbnail: true, runId: 'r1' },
      { id: 'y', label: 'n.txt', sizeLabel: '12 B', kind: 'file', thumbnail: false, runId: 'r1' },
    ]);
    const lines = withRunPrompt([], 'look', atts);
    expect(lines[0]?.attachments).toBe(atts);
    expect(buildTranscript(lines)[0]).toMatchObject({ type: 'user', attachments: atts });
  });

  it('a run without attachments leaves the line unchanged', () => {
    expect(lineAttachmentsFromRun({ ID: 'r1' })).toBeUndefined();
    const line = withRunPrompt([], 'look')[0] as TimelineLine;
    expect('attachments' in line).toBe(false);
  });

  it('pending attachments become local, never-fetched pills', () => {
    expect(lineAttachmentsFromPending([{ id: 'a', kind: 'image', name: 'p.jpg', mimeType: 'image/jpeg', data: '', size: 900, uri: 'blob:1' }])).toEqual([
      { id: 'a', label: 'p.jpg', sizeLabel: '900 B', kind: 'image', thumbnail: false, uri: 'blob:1' },
    ]);
    expect(lineAttachmentsFromPending([])).toBeUndefined();
  });
});
