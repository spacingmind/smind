import { describe, expect, it } from "vitest";
import { buildFailoverIndex } from "@/lib/run-failover";

describe("buildFailoverIndex", () => {
  it("links a failed run to its successor (with chat and provider) and back", () => {
    const idx = buildFailoverIndex([
      { ID: "r1", ChatID: 10, Provider: "claude-native" },
      { ID: "r2", ChatID: 11, Provider: "glm", FailoverOf: "r1" },
    ]);
    expect(idx.successors.get("r1")).toEqual({ runId: "r2", chatId: 11, provider: "glm" });
    expect(idx.continues.get("r2")).toBe("r1");
    expect(idx.successors.has("r2")).toBe(false);
    expect(idx.continues.has("r1")).toBe(false);
  });

  it("is empty when nothing failed over", () => {
    const idx = buildFailoverIndex([{ ID: "r1", ChatID: 10, Provider: "glm", FailoverOf: "" }]);
    expect(idx.successors.size).toBe(0);
  });
});
