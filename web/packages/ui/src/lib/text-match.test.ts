import { describe, expect, it } from "vitest";
import { compareMatchScores, scoreMatch, scoreTextFields } from "./text-match";

describe("text match", () => {
  it("exact beats whole word beats prefix", () => {
    expect(scoreMatch("opus", "opus")).toEqual({ tier: 0, offset: 0 });
    expect(scoreMatch("opus", "opus 4.5")).toEqual({ tier: 1, offset: 0 });
    expect(scoreMatch("opus", "opusair")).toEqual({ tier: 2, offset: 0 });
    expect(scoreMatch("opus", "claude opus")).toEqual({ tier: 1, offset: 7 });
    expect(scoreMatch("pus", "opus")).toEqual({ tier: 4, offset: 1 });

    const exact = scoreTextFields("opus", ["opus"]);
    const wholeWord = scoreTextFields("opus", ["opus 4.5"]);
    const prefix = scoreTextFields("opus", ["opusair"]);
    expect(exact).not.toBeNull();
    expect(wholeWord).not.toBeNull();
    expect(prefix).not.toBeNull();
    expect(compareMatchScores(exact!, wholeWord!)).toBeLessThan(0);
    expect(compareMatchScores(wholeWord!, prefix!)).toBeLessThan(0);
  });

  it("multi-token query needs every token", () => {
    expect(scoreTextFields("claude opus", ["Claude Opus 5"])).not.toBeNull();
    expect(scoreTextFields("claude opus", ["Claude Sonnet 5"])).toBeNull();
    // Tokens may land in different fields.
    expect(scoreTextFields("claude fast", ["Claude Opus", "fast tool use"])).not.toBeNull();
    expect(scoreTextFields("claude missing", ["Claude Opus", "other"])).toBeNull();
  });

  it("subsequence is per word", () => {
    expect(scoreMatch("pasbab", "paseo-babysit")).toEqual({ tier: 5, offset: 0, spread: 9 });
    expect(scoreMatch("labdes", "Label as Design")).toBeNull();
    expect(scoreMatch("odes", "Label as Design")).toBeNull();
  });

  it("ties keep stable order", () => {
    const a = { tier: 2, offset: 0, spread: 3 };
    const b = { tier: 2, offset: 0, spread: 3 };
    expect(compareMatchScores(a, b)).toBe(0);
    expect(compareMatchScores({ tier: 2, offset: 0 }, { tier: 2, offset: 4 })).toBeLessThan(0);
    expect(
      compareMatchScores({ tier: 5, offset: 0, spread: 2 }, { tier: 5, offset: 0, spread: 6 }),
    ).toBeLessThan(0);
  });
});
