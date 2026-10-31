/**
 * Host labels (ADR-0048 F5): operator routing tags kept on this host's row for each peer, never sent to the
 * peer. The daemon validates them (`internal/fleet/registry/labels.go`); these mirror its rules so the editor
 * can say what is wrong before it sends, and `fleet-labels.test.ts` pins them against the same table the Go
 * test uses. The daemon stays the authority: a refusal it returns is shown as it is.
 */
export const MAX_HOST_LABELS = 16;

export const LABEL_KEY_RE = /^[a-z][a-z0-9._-]{0,31}$/;
export const LABEL_VALUE_RE = /^[a-z0-9][a-z0-9._+-]{0,31}$/;
const SECRET_WORDS = ["secret", "token", "passw", "apikey", "credential"];

export const LABEL_KEY_HINT = "Start with a lowercase letter; up to 32 of a-z, 0-9, . _ -";
export const LABEL_VALUE_HINT = "Up to 32 of a-z, 0-9, . _ + -";

export function labelKeyError(key: string): string | null {
  if (key === "") return "Enter a key";
  if (!LABEL_KEY_RE.test(key)) return LABEL_KEY_HINT;
  const lower = key.toLowerCase();
  if (SECRET_WORDS.some((w) => lower.includes(w))) return "A key that looks like a secret is not allowed";
  return null;
}

export function labelValueError(value: string): string | null {
  if (value === "") return "Enter a value";
  if (!LABEL_VALUE_RE.test(value)) return LABEL_VALUE_HINT;
  return null;
}

export interface LabelRow {
  key: string;
  value: string;
}

export interface LabelRowErrors {
  key: string | null;
  value: string | null;
}

/** Per-row errors for an edited list (a duplicate key counts against the later row), plus the list-level one (too many). */
export function validateLabelRows(rows: LabelRow[]): { rows: LabelRowErrors[]; list: string | null; ok: boolean } {
  const seen = new Set<string>();
  const out = rows.map((r) => {
    let key = labelKeyError(r.key);
    if (!key && seen.has(r.key)) key = "Duplicate key";
    seen.add(r.key);
    return { key, value: labelValueError(r.value) };
  });
  const list = rows.length > MAX_HOST_LABELS ? `At most ${MAX_HOST_LABELS} labels` : null;
  return { rows: out, list, ok: list === null && out.every((e) => e.key === null && e.value === null) };
}

export function rowsFromLabels(labels: Record<string, string> | undefined): LabelRow[] {
  return Object.entries(labels ?? {})
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([key, value]) => ({ key, value }));
}

/** What to send to `fleet.host.labels` to turn `before` into `rows`: changed or new pairs in `set`, dropped keys in `remove`. */
export function diffLabels(before: Record<string, string> | undefined, rows: LabelRow[]): { set: Record<string, string>; remove: string[] } {
  const prev = before ?? {};
  const set: Record<string, string> = {};
  const now = new Set<string>();
  for (const r of rows) {
    now.add(r.key);
    if (prev[r.key] !== r.value) set[r.key] = r.value;
  }
  const remove = Object.keys(prev)
    .filter((k) => !now.has(k))
    .sort();
  return { set, remove };
}
