import { describe, expect, it } from "vitest";

import { supportsThinking } from "@/lib/thinking-levels";

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
