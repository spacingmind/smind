/**
 * The one gate for a link that comes from data (the PR panel's `details.url` / `check.url`): http(s) only, so a
 * `javascript:`, `data:` or `vbscript:` value never reaches an `href`. Returns the normalised URL, or null.
 */
export function safeHttpUrl(value: string | null | undefined): string | null {
  if (typeof value !== "string" || value === "") return null;
  try {
    const url = new URL(value);
    return url.protocol === "https:" || url.protocol === "http:" ? url.href : null;
  } catch {
    return null;
  }
}
