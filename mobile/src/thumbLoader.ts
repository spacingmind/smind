// thumbLoader.ts: lazy, cached, one-at-a-time thumbnail fetches for the
// timeline's attachment pills (ADR-0038). Pure: the fetch is injected. A
// failed fetch is cached as "no thumbnail" so the pill stays a pill and the
// daemon is not asked again.

import { LineAttachment } from './runTimeline';

export function thumbKey(runId: string, id: string): string {
  return `${runId}:${id}`;
}

/** Only an image the daemon said would fit the frame, served by a run, with no local copy. */
export function shouldFetchThumb(a: LineAttachment): boolean {
  return a.kind === 'image' && a.thumbnail && a.uri === undefined && a.runId !== undefined;
}

export interface ThumbLoader {
  /** A data: uri, or undefined when the fetch failed. */
  load(runId: string, id: string): Promise<string | undefined>;
}

export function createThumbLoader(fetchOne: (runId: string, id: string) => Promise<{ mimeType: string; data: string }>): ThumbLoader {
  const cache = new Map<string, Promise<string | undefined>>();
  let tail: Promise<unknown> = Promise.resolve();
  return {
    load(runId, id) {
      const key = thumbKey(runId, id);
      const hit = cache.get(key);
      if (hit) return hit;
      const p = tail.then(async () => {
        try {
          const r = await fetchOne(runId, id);
          return `data:${r.mimeType};base64,${r.data}`;
        } catch {
          return undefined;
        }
      });
      tail = p; // never rejects: the chain keeps going after a failure
      cache.set(key, p);
      return p;
    },
  };
}
