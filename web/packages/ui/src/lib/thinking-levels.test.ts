import { describe, expect, it } from "vitest";

import { supportsThinking } from "@/lib/thinking-levels";

describe("supportsThinking", () => {
  it("is true only for claude-native", () => {
    expect(supportsThinking("claude-native")).toBe(true);
    expect(supportsThinking("glm")).toBe(false);
    expect(supportsThinking("kimi")).toBe(false);
    expect(supportsThinking("codex-native")).toBe(false);
  });
});
