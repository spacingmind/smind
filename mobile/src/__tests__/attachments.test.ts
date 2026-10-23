// attachments.test.ts pins the pure attachment pipeline (ADR-0038): the relay
// budget on the exact request, the downscale ladder with a fake manipulator
// whose output size depends on quality, refusals, daemon error wording and the
// timeline view helpers.

import { describe, expect, it } from 'vitest';
import type { AttachmentMeta, AttachmentsInfo } from '../api';
import {
  MAX_LONG_EDGE,
  PendingAttachment,
  Pick,
  RELAY_MAX_REQUEST_BYTES,
  addPick,
  attachmentErrorMessage,
  canThumbnail,
  capabilities,
  checkSend,
  fits,
  humanSize,
  prepareFile,
  prepareImage,
  requestBytes,
  runStartParams,
  timelineAttachmentView,
} from '../attachments';

const base = { taskId: 7, provider: 'glm', prompt: 'hello', whenBusy: 'queue' };

function b64(len: number): string {
  return 'A'.repeat(Math.floor(len / 4) * 4);
}

function pending(name: string, dataLen: number, kind: 'image' | 'file' = 'file'): PendingAttachment {
  const data = b64(dataLen);
  return { id: name, kind, name, mimeType: kind === 'image' ? 'image/jpeg' : 'application/pdf', data, size: (data.length * 3) / 4 };
}

/** A manipulator whose base64 length is quality * scale, recording every call. */
function fakeManipulator(scale: number) {
  const calls: Array<{ uri: string; maxLongEdge: number; quality: number }> = [];
  return {
    calls,
    manipulate: async (uri: string, opts: { maxLongEdge: number; quality: number }) => {
      calls.push({ uri, ...opts });
      return { uri: `${uri}#jpg`, width: 1568, height: 1000, base64: b64(opts.quality * scale) };
    },
  };
}

function deps(readLen: number, scale = 0) {
  const m = fakeManipulator(scale);
  return { m, d: { manipulate: m.manipulate, readBase64: async () => b64(readLen) } };
}

const ctx = { base, existing: [] as PendingAttachment[] };
const png = (over: Partial<Pick> = {}): Pick => ({ uri: 'file:///p.png', name: 'p.png', mimeType: 'image/png', width: 800, height: 600, kind: 'image', ...over });

describe('prepareImage', () => {
  it('fits as is', async () => {
    const { m, d } = deps(4000);
    const r = await prepareImage(png(), d, ctx);
    expect(r.ok).toBe(true);
    if (!r.ok) return;
    expect(m.calls).toEqual([]);
    expect(r.attachment).toMatchObject({ kind: 'image', name: 'p.png', mimeType: 'image/png', width: 800, height: 600, size: 3000, uri: 'file:///p.png' });
    expect(r.attachment.data).toBe(b64(4000));
  });

  it('oversized image is downscaled to <= 1568 and fits', async () => {
    // 4000 px wide: over the long edge, so it is re-encoded. q0.8 -> 1.28M chars (too big), q0.7 -> 1.12M (too big), q0.6 -> 960k (fits).
    const { m, d } = deps(100, 1_600_000);
    const r = await prepareImage(png({ name: 'big.png', width: 4000, height: 3000 }), d, ctx);
    expect(r.ok).toBe(true);
    if (!r.ok) return;
    expect(m.calls.map((c) => c.quality)).toEqual([0.8, 0.7, 0.6]);
    expect(m.calls.every((c) => c.maxLongEdge === MAX_LONG_EDGE)).toBe(true);
    expect(r.attachment.mimeType).toBe('image/jpeg');
    expect(r.attachment.name).toBe('big.jpg');
    expect(Math.max(r.attachment.width!, r.attachment.height!)).toBeLessThanOrEqual(1568);
    expect(fits(base, [r.attachment])).toBe(true);
  });

  it('oversized image whose bytes do not fit is also re-encoded even when small in pixels', async () => {
    const { m, d } = deps(1_500_000, 1_000_000);
    const r = await prepareImage(png(), d, ctx);
    expect(r.ok).toBe(true);
    expect(m.calls.map((c) => c.quality)).toEqual([0.8]);
  });

  it('downscale cannot fit is refused', async () => {
    const { m, d } = deps(100, 4_000_000);
    const r = await prepareImage(png({ name: 'huge.png', width: 9000, height: 9000 }), d, ctx);
    expect(r).toEqual({ ok: false, message: 'huge.png is too large to send from the phone even after shrinking; try a smaller image' });
    expect(m.calls.map((c) => c.quality)).toEqual([0.8, 0.7, 0.6, 0.5, 0.4]);
  });

  it('heic is re-encoded to jpeg', async () => {
    const { m, d } = deps(100, 400_000);
    const r = await prepareImage(png({ name: 'IMG_0001.HEIC', uri: 'file:///i.heic', mimeType: 'image/heic', width: 1000, height: 800 }), d, ctx);
    expect(r.ok).toBe(true);
    if (!r.ok) return;
    expect(m.calls).toEqual([{ uri: 'file:///i.heic', maxLongEdge: 1568, quality: 0.8 }]);
    expect(r.attachment.mimeType).toBe('image/jpeg');
    expect(r.attachment.name).toBe('IMG_0001.jpg');
  });

  it('unknown dimensions are re-encoded rather than passed through', async () => {
    const { m, d } = deps(100, 400_000);
    const r = await prepareImage(png({ width: undefined, height: undefined }), d, ctx);
    expect(r.ok).toBe(true);
    expect(m.calls).toHaveLength(1);
  });

  it('counts the attachments already in the tray', async () => {
    const { d } = deps(500_000);
    const existing = [pending('a.pdf', 500_000)];
    const r = await prepareImage(png(), d, { base, existing });
    // 2 x ~500k base64 fits one frame; a third passes through only if it still fits, else it is re-encoded (empty here) -- so check the passthrough bytes.
    expect(r.ok && r.attachment.data).toBe(b64(500_000));
    const f = await prepareFile({ uri: 'u', name: 'c.pdf', kind: 'file' }, d, { base, existing: [...existing, pending('b.pdf', 500_000)] });
    expect(f.ok).toBe(false);
  });
});

describe('prepareFile', () => {
  it('oversized file is refused with message', async () => {
    // 3.2M base64 chars = 2.4 MB decoded.
    const r = await prepareFile({ uri: 'file:///r.pdf', name: 'report.pdf', mimeType: 'application/pdf', kind: 'file' }, { readBase64: async () => b64(3_200_000) }, ctx);
    expect(r).toEqual({ ok: false, message: 'report.pdf is 2.4 MB; over the relay a prompt can carry about 780 KB' });
  });

  it('a small file is sent as is', async () => {
    const r = await prepareFile({ uri: 'file:///n.txt', name: 'n.txt', kind: 'file' }, { readBase64: async () => 'aGVsbG8=' }, ctx);
    expect(r.ok).toBe(true);
    if (!r.ok) return;
    expect(r.attachment).toMatchObject({ kind: 'file', name: 'n.txt', mimeType: 'application/octet-stream', data: 'aGVsbG8=', size: 5 });
  });
});

describe('addPick', () => {
  it('eleventh attachment is refused', async () => {
    const existing = Array.from({ length: 10 }, (_, i) => pending(`f${i}.txt`, 40));
    const { m, d } = deps(40);
    const r = await addPick({ uri: 'u', name: 'x.txt', kind: 'file' }, d, { base, existing, max: 10 });
    expect(r).toEqual({ ok: false, message: 'You can attach up to 10 files to one prompt.' });
    expect(m.calls).toEqual([]);
  });

  it('dispatches by kind', async () => {
    const { m, d } = deps(400);
    const f = await addPick({ uri: 'u', name: 'x.txt', kind: 'file' }, d, { ...ctx, max: 10 });
    const i = await addPick(png(), d, { ...ctx, max: 10 });
    expect(f.ok && f.attachment.kind).toBe('file');
    expect(i.ok && i.attachment.kind).toBe('image');
    expect(m.calls).toEqual([]);
  });
});

describe('send budget', () => {
  it('size is rechecked at send', () => {
    const att = [pending('a.pdf', 1_000_000)];
    expect(checkSend(base, att)).toBeNull();
    const grown = { ...base, prompt: 'x'.repeat(60_000) };
    expect(checkSend(grown, att)).toBe(
      'This prompt is too large to send: a.pdf (750 KB) is the largest attachment; over the relay a prompt can carry about 780 KB. Remove an attachment or shorten the prompt.',
    );
  });

  it('request size is exact', () => {
    const params = { taskId: 1, provider: 'glm', prompt: 'héllo 日本語 🎉' };
    expect(requestBytes(params)).toBe(Buffer.byteLength(JSON.stringify(params), 'utf8'));
    expect(requestBytes(params)).toBeGreaterThan(JSON.stringify(params).length);
    // The request fits at exactly the limit and not one byte over.
    const att = pending('a.bin', 1_000_000);
    const overhead = requestBytes(runStartParams({ ...base, prompt: '' }, [att]));
    const room = RELAY_MAX_REQUEST_BYTES - overhead;
    expect(fits({ ...base, prompt: 'x'.repeat(room) }, [att])).toBe(true);
    expect(fits({ ...base, prompt: 'x'.repeat(room + 1) }, [att])).toBe(false);
    // Multi-byte characters count their bytes: 'é' is 2 bytes.
    expect(fits({ ...base, prompt: 'é'.repeat(Math.floor(room / 2) + 1) }, [att])).toBe(false);
  });

  it('runStartParams matches the api wire', () => {
    expect(runStartParams({ ...base, chatId: 3, model: 'm', permissionMode: 'p' }, [])).toEqual({
      taskId: 7, provider: 'glm', prompt: 'hello', whenBusy: 'queue', chatId: 3, model: 'm', permissionMode: 'p',
    });
    const a = pending('a.pdf', 8);
    expect(runStartParams(base, [a]).attachments).toEqual([{ kind: 'file', name: 'a.pdf', mimeType: 'application/pdf', data: a.data }]);
  });

  it('humanSize uses decimal units', () => {
    expect(humanSize(512)).toBe('512 B');
    expect(humanSize(780_000)).toBe('780 KB');
    expect(humanSize(2_400_000)).toBe('2.4 MB');
    expect(humanSize(999_999)).toBe('1.0 MB');
  });
});

describe('daemon errors and capabilities', () => {
  const names = ['a.png', 'b.pdf'];
  it('daemon error code maps to inline text', () => {
    expect(attachmentErrorMessage({ code: 'attachment_too_large', index: 1 }, names)).toBe('b.pdf is too large for the daemon to accept.');
    expect(attachmentErrorMessage({ code: 'too_many_attachments' }, names)).toBe('Too many attachments on one prompt.');
    expect(attachmentErrorMessage({ code: 'unsupported_image_type', index: 0 }, names)).toBe('a.png is an image type the daemon does not accept.');
    expect(attachmentErrorMessage({ code: 'mime_mismatch', index: 0 }, names)).toBe('a.png is not the kind of image its type claims.');
    expect(attachmentErrorMessage({ code: 'invalid_base64', index: 1 }, names)).toBe('b.pdf could not be read; try attaching it again.');
    expect(attachmentErrorMessage({ code: 'invalid_attachment', index: 1 }, names)).toBe('b.pdf was rejected by the daemon.');
    expect(attachmentErrorMessage({ code: 'outside_workspace', index: 0 }, names)).toBe('a.png is outside this workspace.');
    expect(attachmentErrorMessage({ code: 'not_found', index: 0 }, names)).toBe('a.png was not found.');
    // Out-of-range index and no index: generic subject.
    expect(attachmentErrorMessage({ code: 'mime_mismatch', index: 5 }, names)).toBe('An attachment is not the kind of image its type claims.');
    expect(attachmentErrorMessage({ code: 'mime_mismatch' }, names)).toBe('An attachment is not the kind of image its type claims.');
    // The wire carries no index field; the daemon's message starts "attachment N:".
    expect(attachmentErrorMessage({ code: 'mime_mismatch', message: 'attachment 2: bytes are not image/png' }, names)).toBe('b.pdf is not the kind of image its type claims.');
    // Unknown code.
    expect(attachmentErrorMessage({ code: 'weird', message: 'boom' }, names)).toBe('boom');
    expect(attachmentErrorMessage({}, names)).toBe('Could not send the attachments.');
  });

  it('older daemon without kinds hides affordance or limits it', () => {
    const info = (kinds?: string[], maxCount = 10): AttachmentsInfo => ({ maxCount, maxImageBytes: 1, maxFileBytes: 1, maxTotalBytes: 1, maxMessageBytes: 1, images: [], kinds });
    expect(capabilities(undefined)).toEqual({ image: true, file: true, maxCount: 10 });
    expect(capabilities(info(undefined))).toEqual({ image: true, file: true, maxCount: 10 });
    expect(capabilities(info(['file_ref', 'pr']))).toEqual({ image: false, file: false, maxCount: 10 });
    expect(capabilities(info(['file'], 4))).toEqual({ image: false, file: true, maxCount: 4 });
    expect(capabilities(info(['image', 'file']))).toEqual({ image: true, file: true, maxCount: 10 });
  });
});

describe('timeline', () => {
  it('thumbnail only when the fetch fits one frame', () => {
    const img = (size?: number, name = 'a.png'): AttachmentMeta => ({ id: '1', kind: 'image', name, mimeType: 'image/png', size });
    expect(canThumbnail(img(500_000))).toBe(true);
    expect(canThumbnail(img(undefined))).toBe(false);
    const est = (n: number) => (((n + 2) / 3) | 0) * 4 + (5 + 9) * 6 + 256;
    let edge = 800_000;
    while (est(edge + 1) <= RELAY_MAX_REQUEST_BYTES) edge++;
    expect(est(edge)).toBeLessThanOrEqual(RELAY_MAX_REQUEST_BYTES);
    expect(canThumbnail(img(edge))).toBe(true);
    expect(canThumbnail(img(edge + 1))).toBe(false);
    expect(canThumbnail({ id: '2', kind: 'file', name: 'a.pdf', size: 10 })).toBe(false);
  });

  it('timeline view lists name and size', () => {
    const meta: AttachmentMeta[] = [
      { id: '1', kind: 'image', name: 'shot.jpg', mimeType: 'image/jpeg', size: 2_400_000 },
      { id: '2', kind: 'file', name: 'n.txt', mimeType: 'text/plain', size: 512 },
      { id: '3', kind: 'file_ref', path: 'src/a.go' },
      { id: '4', kind: 'pr', number: 42, title: 'Fix' },
    ];
    expect(timelineAttachmentView(meta)).toEqual([
      { id: '1', label: 'shot.jpg', sizeLabel: '2.4 MB', kind: 'image', thumbnail: false },
      { id: '2', label: 'n.txt', sizeLabel: '512 B', kind: 'file', thumbnail: false },
      { id: '3', label: 'src/a.go', sizeLabel: '', kind: 'file_ref', thumbnail: false },
      { id: '4', label: '#42', sizeLabel: '', kind: 'pr', thumbnail: false },
    ]);
    expect(timelineAttachmentView(undefined)).toEqual([]);
  });
});
