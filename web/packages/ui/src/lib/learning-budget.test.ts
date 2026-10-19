import { describe, expect, it } from "vitest";

import { budgetState, formatTokens } from "@/lib/learning-budget";
import type { LearningWorkspaceBudget } from "@/lib/learning-types";

const base: LearningWorkspaceBudget = { windowHours: 24, usedUsd: 0.04, usedTokens: 18_000, capUsd: 0.25, capTokens: 150_000, unreportedRuns: 0 };

describe("budgetState", () => {
  it("formats both values against their caps", () => {
    const s = budgetState(base);
    expect(s.costLabel).toBe("$0.04 of $0.25");
    expect(s.tokensLabel).toBe("18k of 150k tokens");
    expect(s.level).toBe("ok");
  });

  it("the used fraction is the larger of the two", () => {
    expect(budgetState({ ...base, usedUsd: 0.2, usedTokens: 15_000 }).usedFraction).toBeCloseTo(0.8);
    expect(budgetState({ ...base, usedUsd: 0.01, usedTokens: 120_000 }).usedFraction).toBeCloseTo(0.8);
  });

  it("warns from 80% of either cap and is exhausted from 100%", () => {
    expect(budgetState({ ...base, usedUsd: 0.1999 }).level).toBe("ok");
    expect(budgetState({ ...base, usedUsd: 0.2 }).level).toBe("warn");
    expect(budgetState({ ...base, usedTokens: 120_000 }).level).toBe("warn");
    expect(budgetState({ ...base, usedUsd: 0.25 }).level).toBe("exhausted");
    expect(budgetState({ ...base, usedTokens: 150_000 }).level).toBe("exhausted");
  });

  it("clamps the fraction to 0..1", () => {
    expect(budgetState({ ...base, usedUsd: 5 }).usedFraction).toBe(1);
    expect(budgetState({ ...base, usedUsd: -1, usedTokens: -5 }).usedFraction).toBe(0);
    expect(budgetState({ ...base, usedUsd: Number.NaN, usedTokens: 0 }).usedFraction).toBe(0);
  });

  it("a zero cap means no limit and never divides by zero", () => {
    const s = budgetState({ ...base, capUsd: 0, capTokens: 0, usedUsd: 3, usedTokens: 9_000_000 });
    expect(s.usedFraction).toBe(0);
    expect(s.level).toBe("ok");
    expect(s.costFraction).toBe(0);
    expect(s.costLabel).toBe("$3.00");
    expect(s.tokensLabel).toBe("9M tokens");
  });

  it("one capped dimension still drives the level when the other is unlimited", () => {
    expect(budgetState({ ...base, capTokens: 0, usedUsd: 0.25 }).level).toBe("exhausted");
  });
});

describe("formatTokens", () => {
  it("abbreviates thousands and millions", () => {
    expect(formatTokens(0)).toBe("0");
    expect(formatTokens(950)).toBe("950");
    expect(formatTokens(1_500)).toBe("1.5k");
    expect(formatTokens(18_000)).toBe("18k");
    expect(formatTokens(149_600)).toBe("150k");
    expect(formatTokens(1_250_000)).toBe("1.3M");
  });
});
