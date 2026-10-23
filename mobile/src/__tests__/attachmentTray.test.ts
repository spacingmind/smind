import { describe, expect, it } from 'vitest';
import { attachMenuItems, trayPills } from '../attachmentTray';
import { PendingAttachment } from '../attachments';

const base: PendingAttachment = { id: 'a', kind: 'file', name: 'notes.txt', mimeType: 'text/plain', data: '', size: 2400 };

describe('attachmentTray', () => {
  it('tray pills show name and size', () => {
    const pills = trayPills([base, { ...base, id: 'b', kind: 'image', name: 'p.jpg', size: 512, uri: 'blob:x' }]);
    expect(pills).toEqual([
      { id: 'a', label: 'notes.txt', sizeLabel: '2 KB', kind: 'file' },
      { id: 'b', label: 'p.jpg', sizeLabel: '512 B', kind: 'image', thumbUri: 'blob:x' },
    ]);
  });

  it('attach menu follows daemon kinds', () => {
    expect(attachMenuItems(undefined).map((i) => i.key)).toEqual(['photo', 'file']);
    expect(attachMenuItems({ kinds: ['image', 'file', 'file_ref'] } as never).map((i) => i.key)).toEqual(['photo', 'file']);
    expect(attachMenuItems({ kinds: ['file'] } as never).map((i) => i.key)).toEqual(['file']);
    expect(attachMenuItems({ kinds: ['file_ref'] } as never)).toEqual([]);
  });
});
