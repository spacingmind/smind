import { describe, expect, it } from "vitest";

import { nextCronRuns, parseCron } from "@/lib/cron-next";

const FROM = new Date("2026-10-27T10:07:30Z");
const iso = (spec: string, n = 3): string[] => {
  const r = nextCronRuns(spec, n, FROM);
  if (!r.ok) throw new Error(r.error);
  return r.runs.map((d) => d.toISOString());
};

describe("cron next runs", () => {
  it("steps: */5 fires on the next five-minute marks", () => {
    expect(iso("*/5 * * * *")).toEqual(["2026-10-27T10:10:00.000Z", "2026-10-27T10:15:00.000Z", "2026-10-27T10:20:00.000Z"]);
  });

  it("lists and ranges", () => {
    expect(iso("0,30 9-10 * * *")).toEqual(["2026-10-27T10:30:00.000Z", "2026-10-28T09:00:00.000Z", "2026-10-28T09:30:00.000Z"]);
  });

  it("an offset step: 5/15 means 5-59/15", () => {
    expect(iso("5/15 11 * * *", 4)).toEqual(["2026-10-27T11:05:00.000Z", "2026-10-27T11:20:00.000Z", "2026-10-27T11:35:00.000Z", "2026-10-27T11:50:00.000Z"]);
  });

  it("macros and names: @daily, mon-fri and a month name", () => {
    expect(iso("@daily", 1)).toEqual(["2026-10-28T00:00:00.000Z"]);
    // 2026-10-27 is a Tuesday; mon-fri at 08:00 -> Wed, Thu, Fri
    expect(iso("0 8 * * mon-fri")).toEqual(["2026-10-28T08:00:00.000Z", "2026-10-29T08:00:00.000Z", "2026-10-30T08:00:00.000Z"]);
    expect(iso("0 0 1 jan *", 1)).toEqual(["2027-01-01T00:00:00.000Z"]);
  });

  it("day-of-month OR day-of-week when both are restricted", () => {
    // the 1st OR any Friday: Fri 30 Oct comes before Sun 1 Nov
    expect(iso("0 6 1 * fri")).toEqual(["2026-10-30T06:00:00.000Z", "2026-11-01T06:00:00.000Z", "2026-11-06T06:00:00.000Z"]);
    // one restricted field alone is an AND with the star: only the 1st
    expect(iso("0 6 1 * *", 2)).toEqual(["2026-11-01T06:00:00.000Z", "2026-12-01T06:00:00.000Z"]);
  });

  it("7 is Sunday", () => {
    expect(iso("0 0 * * 7", 1)).toEqual(["2026-11-01T00:00:00.000Z"]);
  });

  it("rejects a cadence under five minutes", () => {
    for (const spec of ["* * * * *", "*/4 * * * *", "0,3 * * * *", "1-4 * * * *"]) {
      const r = parseCron(spec);
      expect(r.ok, spec).toBe(false);
      if (!r.ok) expect(r.error).toContain("minimum cadence");
    }
    expect(parseCron("*/5 * * * *").ok).toBe(true);
  });

  it("rejects malformed specs", () => {
    expect(parseCron("* * *").ok).toBe(false);
    expect(parseCron("").ok).toBe(false);
    expect(parseCron("61 * * * *").ok).toBe(false);
    expect(parseCron("a * * * *").ok).toBe(false);
    expect(parseCron("*/0 * * * *").ok).toBe(false);
    expect(parseCron("0 0 * * 9").ok).toBe(false);
  });
});
