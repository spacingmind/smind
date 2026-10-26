// A client-side port of internal/automation/cron.go: the five-field grammar, UTC, the standard "day-of-month OR
// day-of-week when both are restricted" rule, and the 5-minute minimum cadence. It only drives the form's live
// preview and early rejection; the daemon stays the authority and its own answer is shown for a saved automation.

export const MIN_CRON_INTERVAL_MS = 5 * 60_000;

const MACROS: Record<string, string> = { "@hourly": "0 * * * *", "@daily": "0 0 * * *", "@weekly": "0 0 * * 0" };
const MONTHS: Record<string, number> = { jan: 1, feb: 2, mar: 3, apr: 4, may: 5, jun: 6, jul: 7, aug: 8, sep: 9, oct: 10, nov: 11, dec: 12 };
const DAYS: Record<string, number> = { sun: 0, mon: 1, tue: 2, wed: 3, thu: 4, fri: 5, sat: 6 };

export interface ParsedCron {
  spec: string;
  min: bigint;
  hour: bigint;
  dom: bigint;
  mon: bigint;
  dow: bigint;
  domStar: boolean;
  dowStar: boolean;
}

export type CronParse = { ok: true; cron: ParsedCron } | { ok: false; error: string };

const bit = (v: number): bigint => 1n << BigInt(v);
const has = (set: bigint, v: number): boolean => (set & bit(v)) !== 0n;

function value(s: string, names: Record<string, number> | null, what: string): number {
  const named = names?.[s.toLowerCase()];
  if (named !== undefined) return named;
  if (!/^\d+$/.test(s)) throw new Error(`cron ${what}: "${s}" is not a number`);
  return Number(s);
}

function field(s: string, lo: number, hi: number, names: Record<string, number> | null, what: string): { set: bigint; star: boolean } {
  let set = 0n;
  for (const part of s.split(",")) {
    if (part === "") throw new Error(`cron ${what} "${s}": empty list item`);
    let rng = part;
    let step = 1;
    const slash = part.indexOf("/");
    if (slash >= 0) {
      const raw = part.slice(slash + 1);
      step = /^\d+$/.test(raw) ? Number(raw) : 0;
      if (step < 1) throw new Error(`cron ${what} "${s}": bad step`);
      rng = part.slice(0, slash);
    }
    let from = lo;
    let to = hi;
    if (rng === "*") {
      // the whole range
    } else if (rng.includes("-")) {
      const [a, b] = rng.split("-", 2);
      from = value(a, names, what);
      to = value(b ?? "", names, what);
    } else {
      from = to = value(rng, names, what);
      if (slash >= 0) to = hi; // "5/15" means 5-max/15, standard cron
    }
    if (from < lo || to > hi || from > to) throw new Error(`cron ${what} "${s}": out of range ${lo}-${hi}`);
    for (let v = from; v <= to; v += step) set |= bit(v);
  }
  return { set, star: s === "*" };
}

function dayMatches(c: ParsedCron, t: Date): boolean {
  const domOk = has(c.dom, t.getUTCDate());
  const dowOk = has(c.dow, t.getUTCDay());
  if (c.domStar && c.dowStar) return true;
  if (c.domStar) return dowOk;
  if (c.dowStar) return domOk;
  return domOk || dowOk;
}

/** The first instant strictly after `after` (UTC, minute resolution) matching the spec; null if none within 8 years. */
export function nextCronRun(c: ParsedCron, after: Date): Date | null {
  const t = new Date(Math.floor(after.getTime() / 60_000) * 60_000 + 60_000);
  const limit = Date.UTC(t.getUTCFullYear() + 8, t.getUTCMonth(), t.getUTCDate());
  while (t.getTime() < limit) {
    if (!has(c.mon, t.getUTCMonth() + 1)) {
      t.setTime(Date.UTC(t.getUTCFullYear(), t.getUTCMonth() + 1, 1));
      continue;
    }
    if (!dayMatches(c, t)) {
      t.setTime(Date.UTC(t.getUTCFullYear(), t.getUTCMonth(), t.getUTCDate() + 1));
      continue;
    }
    if (!has(c.hour, t.getUTCHours())) {
      t.setTime(Date.UTC(t.getUTCFullYear(), t.getUTCMonth(), t.getUTCDate(), t.getUTCHours() + 1));
      continue;
    }
    if (!has(c.min, t.getUTCMinutes())) {
      t.setTime(t.getTime() + 60_000);
      continue;
    }
    return t;
  }
  return null;
}

/** The smallest gap between two consecutive matches, scanning two years from a leap year's start (as the daemon does). */
function minGapMs(c: ParsedCron): number {
  const start = new Date(Date.UTC(2024, 0, 1));
  let prev = nextCronRun(c, new Date(start.getTime() - 60_000));
  if (!prev) return Infinity;
  const end = Date.UTC(2026, 0, 1);
  let gap = Infinity;
  for (let n = 0; n < 200_000; n++) {
    const next = nextCronRun(c, prev);
    if (!next || next.getTime() > end) break;
    gap = Math.min(gap, next.getTime() - prev.getTime());
    if (gap < MIN_CRON_INTERVAL_MS) return gap;
    prev = next;
  }
  return gap;
}

export function parseCron(input: string): CronParse {
  let spec = input.trim();
  spec = MACROS[spec] ?? spec;
  const f = spec.split(/\s+/).filter(Boolean);
  if (f.length !== 5) return { ok: false, error: `want 5 fields (minute hour day-of-month month day-of-week), got ${spec === "" ? 0 : f.length}` };
  try {
    const min = field(f[0], 0, 59, null, "minute");
    const hour = field(f[1], 0, 23, null, "hour");
    const dom = field(f[2], 1, 31, null, "day-of-month");
    const mon = field(f[3], 1, 12, MONTHS, "month");
    const dow = field(f[4], 0, 7, DAYS, "day-of-week");
    const sunday = has(dow.set, 7) ? (dow.set & ~bit(7)) | 1n : dow.set; // 7 is Sunday too
    const cron: ParsedCron = { spec: f.join(" "), min: min.set, hour: hour.set, dom: dom.set, mon: mon.set, dow: sunday, domStar: dom.star, dowStar: dow.star };
    const gap = minGapMs(cron);
    if (gap < MIN_CRON_INTERVAL_MS) {
      return { ok: false, error: `can fire every ${Math.round(gap / 60_000)} min: the minimum cadence is 5 min` };
    }
    return { ok: true, cron };
  } catch (err) {
    return { ok: false, error: err instanceof Error ? err.message : String(err) };
  }
}

/** The next `count` fire times after `from`, or the parse error. */
export function nextCronRuns(spec: string, count: number, from: Date = new Date()): { ok: true; runs: Date[] } | { ok: false; error: string } {
  const parsed = parseCron(spec);
  if (!parsed.ok) return parsed;
  const runs: Date[] = [];
  let t = from;
  for (let i = 0; i < count; i++) {
    const next = nextCronRun(parsed.cron, t);
    if (!next) break;
    runs.push(next);
    t = next;
  }
  return { ok: true, runs };
}
