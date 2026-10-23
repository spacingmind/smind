import { describe, expect, it, vi } from 'vitest';
import { createThumbLoader, shouldFetchThumb, thumbKey } from '../thumbLoader';

const att = { id: 'x', label: 'p.jpg', sizeLabel: '5 KB', kind: 'image', thumbnail: true, runId: 'r1' };

describe('thumbLoader', () => {
  it('cache key is runId:id', () => expect(thumbKey('r1', 'x')).toBe('r1:x'));

  it('fetch is not attempted when thumbnail is false, for files, or with a local uri', () => {
    expect(shouldFetchThumb({ ...att, thumbnail: false })).toBe(false);
    expect(shouldFetchThumb({ ...att, kind: 'file' })).toBe(false);
    expect(shouldFetchThumb({ ...att, uri: 'blob:1' })).toBe(false);
    expect(shouldFetchThumb({ ...att, runId: undefined })).toBe(false);
    expect(shouldFetchThumb(att)).toBe(true);
  });

  it('fetches once per key, one at a time, and returns a data uri', async () => {
    let active = 0;
    let maxActive = 0;
    const fetchOne = vi.fn(async () => {
      active++;
      maxActive = Math.max(maxActive, active);
      await new Promise((r) => setTimeout(r, 5));
      active--;
      return { mimeType: 'image/png', data: 'AAAA' };
    });
    const loader = createThumbLoader(fetchOne);
    const [a, b, a2] = await Promise.all([loader.load('r1', 'x'), loader.load('r1', 'y'), loader.load('r1', 'x')]);
    expect(a).toBe('data:image/png;base64,AAAA');
    expect(a2).toBe(a);
    expect(b).toBeDefined();
    expect(fetchOne).toHaveBeenCalledTimes(2);
    expect(maxActive).toBe(1);
  });

  it('a failed fetch stays a pill and is not retried', async () => {
    const fetchOne = vi.fn(async () => {
      throw new Error('too big');
    });
    const loader = createThumbLoader(fetchOne);
    expect(await loader.load('r1', 'x')).toBeUndefined();
    expect(await loader.load('r1', 'x')).toBeUndefined();
    expect(fetchOne).toHaveBeenCalledTimes(1);
  });
});
