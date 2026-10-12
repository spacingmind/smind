import { describe, expect, it, vi } from "vitest";
import {
  buildOutlinePrompts,
  createActivePromptPublisher,
  promptPreview,
  promptTickMagnification,
  resolveActiveRunIndex,
  stabilizeOutlinePrompts,
} from "./chat-outline";

describe("chat outline model", () => {
  it("promptPreview takes the first non-empty line, trimmed and truncated", () => {
    expect(promptPreview("\n  \n  fix the bug  \nsecond line")).toBe("fix the bug");
    expect(promptPreview("")).toBe("");
    const long = promptPreview("x".repeat(300));
    expect(long.length).toBe(120);
    expect(long.endsWith("…")).toBe(true);
  });

  it("buildOutlinePrompts maps runs to {id, preview} in order", () => {
    expect(
      buildOutlinePrompts([
        { id: "a", prompt: "one\ntwo" },
        { id: "b", prompt: "\nthree" },
      ]),
    ).toEqual([
      { id: "a", preview: "one" },
      { id: "b", preview: "three" },
    ]);
  });

  it("stable outline identity: same ids and previews return the previous array", () => {
    const first = buildOutlinePrompts([{ id: "a", prompt: "one" }]);
    const same = buildOutlinePrompts([{ id: "a", prompt: "one" }]);
    expect(stabilizeOutlinePrompts(first, same)).toBe(first);
    const changed = buildOutlinePrompts([{ id: "a", prompt: "two" }]);
    expect(stabilizeOutlinePrompts(first, changed)).toBe(changed);
    const longer = buildOutlinePrompts([
      { id: "a", prompt: "one" },
      { id: "b", prompt: "x" },
    ]);
    expect(stabilizeOutlinePrompts(first, longer)).toBe(longer);
  });

  it("resolveActiveRunIndex picks the last run at or above the anchor", () => {
    const tops = [0, 100, 300, 600];
    expect(resolveActiveRunIndex(tops, 24)).toBe(0);
    expect(resolveActiveRunIndex(tops, 100)).toBe(1);
    expect(resolveActiveRunIndex(tops, 350)).toBe(2);
    expect(resolveActiveRunIndex(tops, -50)).toBe(0);
    expect(resolveActiveRunIndex([], 10)).toBe(-1);
  });

  it("resolveActiveRunIndex picks the last run when scrolled to the bottom", () => {
    expect(resolveActiveRunIndex([0, 100, 300, 600], 24, true)).toBe(3);
  });

  it("promptTickMagnification falls off to zero at the radius", () => {
    expect(promptTickMagnification(0)).toBe(1);
    expect(promptTickMagnification(3)).toBe(0);
    expect(promptTickMagnification(-5)).toBe(0);
    expect(promptTickMagnification(1)).toBeCloseTo(promptTickMagnification(-1));
    expect(promptTickMagnification(1)).toBeGreaterThan(promptTickMagnification(2));
  });

  it("active prompt publisher notifies only on change", () => {
    const publisher = createActivePromptPublisher();
    const listener = vi.fn();
    const unsubscribe = publisher.subscribe(listener);
    publisher.publish("a");
    publisher.publish("a");
    expect(listener).toHaveBeenCalledTimes(1);
    expect(publisher.getActiveId()).toBe("a");
    publisher.publish(null);
    expect(listener).toHaveBeenCalledTimes(2);
    unsubscribe();
    publisher.publish("b");
    expect(listener).toHaveBeenCalledTimes(2);
  });
});
