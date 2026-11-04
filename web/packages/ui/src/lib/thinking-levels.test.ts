import { describe, expect, it } from "vitest";

import { defaultEffortLabel, effortLabel, reasoningLevels, supportsThinking, THINKING_LEVELS } from "@/lib/thinking-levels";

describe("supportsThinking", () => {
  it("is true only for claude-native", () => {
    expect(supportsThinking("claude-native")).toBe(true);
    expect(supportsThinking("glm")).toBe(false);
    expect(supportsThinking("kimi")).toBe(false);
    expect(supportsThinking("codex-native")).toBe(false);
  });

  it("supportsThinking is true for a claude-native instance with thinking supported", () => {
    const providers = [{ id: "inst", runner: "claude-native" as const, extends: "claude-native", thinking: "supported" as const }];
    expect(supportsThinking("inst", providers)).toBe(true);
  });

  it("supportsThinking is false when the daemon says thinking is hidden", () => {
    const providers = [{ id: "inst", runner: "claude-native" as const, extends: "claude-native", thinking: "hidden" as const }];
    expect(supportsThinking("inst", providers)).toBe(false);
  });

  it("supportsThinking keeps the runner rule for providers without the field", () => {
    const providers = [{ id: "inst", runner: "claude-native" as const }, { id: "x", runner: "acp" as const }];
    expect(supportsThinking("inst", providers)).toBe(true);
    expect(supportsThinking("x", providers)).toBe(false);
  });
});

describe("reasoningLevels", () => {
  const providers = [
    { id: "codex-native", runner: "codex-native" as const, models: [
      { id: "a", label: "A", default: true, defaultEffort: "medium", efforts: [{ id: "low", description: "Fast" }, { id: "medium" }, { id: "xhigh" }] },
      { id: "b", label: "B", defaultEffort: "high", efforts: [{ id: "high" }] },
      { id: "c", label: "C" },
    ] },
    { id: "glm", runner: "acp" as const },
  ];

  it("lists the selected model's efforts with the default marked", () => {
    const a = reasoningLevels("codex-native", "a", providers)!;
    expect(a.map((l) => l.id)).toEqual(["low", "medium", "xhigh"]);
    expect(a.map((l) => l.label)).toEqual(["Low", "Medium", "Extra high"]);
    expect(a[0]!.help).toBe("Fast");
    expect(a[1]!.help).toBe("(default)");
    expect(reasoningLevels("codex-native", "b", providers)!.map((l) => l.id)).toEqual(["high"]);
  });

  it("falls back to the provider's default model when none is given", () => {
    expect(reasoningLevels("codex-native", "", providers)!.map((l) => l.id)).toEqual(["low", "medium", "xhigh"]);
  });

  it("is empty for a model with no efforts or an unknown model", () => {
    expect(reasoningLevels("codex-native", "c", providers)).toEqual([]);
    expect(reasoningLevels("codex-native", "nope", providers)).toEqual([]);
    expect(reasoningLevels("codex-native", "a", undefined)).toEqual([]);
  });

  it("keeps Claude's vocabulary and omits other providers", () => {
    expect(reasoningLevels("claude-native", "", providers)).toBe(THINKING_LEVELS);
    expect(reasoningLevels("glm", "", providers)).toBeNull();
  });

  it("labels the default and unknown ids", () => {
    expect(defaultEffortLabel("codex-native", "a", providers)).toBe("Default (Medium)");
    expect(defaultEffortLabel("codex-native", "c", providers)).toBe("Default");
    expect(effortLabel("custom")).toBe("Custom");
  });
});
