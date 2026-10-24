import { describe, expect, it } from "vitest";

import { budgetState, parseBudgetInput } from "@/lib/task-budget";

describe("budgetState", () => {
  it("none: no cap", () => {
    expect(budgetState({})).toMatchObject({ kind: "none", label: "Set budget", partialLabel: "" });
    expect(budgetState({ budgetUsd: null })).toMatchObject({ kind: "none" });
  });

  it("under: spent / cap with the ratio", () => {
    const s = budgetState({ budgetUsd: 10, budgetSpentUsd: 4.2 });
    expect(s).toMatchObject({ kind: "under", label: "$4.20 / $10.00", partialLabel: "" });
    expect(s.ratio).toBeCloseTo(0.42);
  });

  it("a budget with no spend yet is under at $0.00", () => {
    expect(budgetState({ budgetUsd: 5 })).toMatchObject({ kind: "under", label: "$0.00 / $5.00" });
  });

  it("near starts exactly at 80%", () => {
    expect(budgetState({ budgetUsd: 10, budgetSpentUsd: 7.99 }).kind).toBe("under");
    expect(budgetState({ budgetUsd: 10, budgetSpentUsd: 8 }).kind).toBe("near");
    expect(budgetState({ budgetUsd: 10, budgetSpentUsd: 9.99 }).kind).toBe("near");
  });

  it("over: exactly at the cap is over", () => {
    expect(budgetState({ budgetUsd: 10, budgetSpentUsd: 10 })).toMatchObject({ kind: "over", label: "Over budget" });
    expect(budgetState({ budgetUsd: 10, budgetSpentUsd: 12.5 }).kind).toBe("over");
  });

  it("over follows the daemon's flag too", () => {
    expect(budgetState({ budgetUsd: 10, budgetSpentUsd: 9, overBudget: true }).kind).toBe("over");
  });

  it("partial: names the runs that reported no cost and marks the spend with +", () => {
    const s = budgetState({ budgetUsd: 10, budgetSpentUsd: 4.2, budgetPartial: true, budgetUnknownRuns: 3 });
    expect(s.partialLabel).toBe("$4.20+ · partial: 3 runs reported no cost");
    expect(s.label).toBe("$4.20+ / $10.00");
    expect(budgetState({ budgetUsd: 10, budgetSpentUsd: 1, budgetUnknownRuns: 1 }).partialLabel).toBe(
      "$1.00+ · partial: 1 run reported no cost",
    );
  });
});

describe("parseBudgetInput", () => {
  it.each([
    ["5", 5],
    ["5.5", 5.5],
    ["10.25", 10.25],
    [" 7.00 ", 7],
    ["$3", 3],
    [".5", 0.5],
  ])("accepts %j", (text, value) => {
    expect(parseBudgetInput(text)).toEqual({ ok: true, value });
  });

  it.each([
    ["", /Enter an amount/],
    ["   ", /Enter an amount/],
    ["0", /greater than 0/],
    ["0.00", /greater than 0/],
    ["-4", /greater than 0/],
    ["1.234", /2 decimal/],
    ["abc", /number like/],
    ["NaN", /number like/],
    ["Infinity", /number like/],
    ["1,5", /number like/],
    ["1e3", /number like/],
  ])("rejects %j", (text, error) => {
    const r = parseBudgetInput(text);
    expect(r.ok).toBe(false);
    if (!r.ok) expect(r.error).toMatch(error);
  });
});
