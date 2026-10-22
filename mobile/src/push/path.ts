// The open task's id from the router pathname (ADR-0040 section 5). Pure.

/** The id for exactly `/task/<digits>` (optional trailing slash); null for any other path. */
export function openTaskIdFromPath(pathname: string | null | undefined): number | null {
  if (typeof pathname !== 'string') return null;
  const m = /^\/task\/(\d+)\/?$/.exec(pathname);
  if (m === null) return null;
  const n = Number(m[1]);
  return Number.isSafeInteger(n) && n > 0 ? n : null;
}
