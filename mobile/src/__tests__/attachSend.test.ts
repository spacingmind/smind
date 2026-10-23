import { describe, expect, it, vi } from 'vitest';
import { attachmentPayloads, sendWithAttachments } from '../attachSend';
import { PendingAttachment, RunStartBase } from '../attachments';
import { DaemonError } from '../relay/RelayConnection';

const base: RunStartBase = { taskId: 1, provider: 'p', prompt: 'hello', whenBusy: 'queue' };
const att = (over: Partial<PendingAttachment> = {}): PendingAttachment => ({ id: 'a1', kind: 'file', name: 'a.txt', mimeType: 'text/plain', data: 'aGk=', size: 2, ...over });

describe('sendWithAttachments', () => {
  it('send is refused inline when the request no longer fits', async () => {
    const send = vi.fn();
    const big = att({ data: 'A'.repeat(1_048_000), size: 786_000, name: 'big.bin' });
    const out = await sendWithAttachments({ ...base, prompt: 'x'.repeat(2000) }, [big], send);
    expect(out.kind).toBe('refused');
    expect(out.kind === 'refused' && out.notice).toContain('big.bin');
    expect(send).not.toHaveBeenCalled();
  });

  it('failed send keeps the tray and draft: the outcome is failed, never sent', async () => {
    const out = await sendWithAttachments(base, [att()], async () => {
      throw new Error('relay down');
    });
    expect(out).toEqual({ kind: 'failed', message: 'relay down', attachment: false });
  });

  it('daemon attachment error shows inline text', async () => {
    const out = await sendWithAttachments(base, [att(), att({ id: 'a2', name: 'pic.png' })], async () => {
      throw new DaemonError('attachment 2: mime mismatch', 'mime_mismatch');
    });
    expect(out).toEqual({ kind: 'failed', message: 'pic.png is not the kind of image its type claims.', attachment: true });
  });

  it('sends and returns the result when it fits', async () => {
    expect(await sendWithAttachments(base, [att()], async () => 7)).toEqual({ kind: 'sent', result: 7 });
  });

  it('payloads carry only kind, name, mimeType and data', () => {
    expect(attachmentPayloads([att({ uri: 'blob:x', width: 3 })])).toEqual([{ kind: 'file', name: 'a.txt', mimeType: 'text/plain', data: 'aGk=' }]);
  });
});
