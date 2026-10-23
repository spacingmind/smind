import { describe, expect, it } from "vitest";
import type { RunSummary } from "@/lib/types";
import { selectObservedModel } from "./observed-model";

function run(over: Partial<RunSummary> & { model?: string }): RunSummary {
  const { model, ...rest } = over;
  return {
    ID: "r",
    TaskID: 1,
    ChatID: 1,
    Provider: "claude",
    StartedAt: "2026-10-23T10:00:00Z",
    usage: model === undefined ? undefined : { source: "acp", model },
    ...rest,
  } as RunSummary;
}

describe("selectObservedModel", () => {
  it("selectObservedModel prefers the chat's newest run", () => {
    const runs = [
      run({ ChatID: 1, model: "old", StartedAt: "2026-10-23T09:00:00Z" }),
      run({ ChatID: 1, model: "newest-in-chat", StartedAt: "2026-10-23T10:00:00Z" }),
      run({ ChatID: 2, model: "newer-elsewhere", StartedAt: "2026-10-23T11:00:00Z" }),
    ];
    expect(selectObservedModel(runs, { chatId: 1, provider: "claude" })).toBe("newest-in-chat");
  });

  it("selectObservedModel falls back to the newest run of the same provider", () => {
    const runs = [
      run({ ChatID: 2, model: "older", StartedAt: "2026-10-23T09:00:00Z" }),
      run({ ChatID: 3, model: "newer", StartedAt: "2026-10-23T11:00:00Z" }),
      run({ ChatID: 4, Provider: "glm", model: "other-provider", StartedAt: "2026-10-23T12:00:00Z" }),
    ];
    expect(selectObservedModel(runs, { chatId: 1, provider: "claude" })).toBe("newer");
    expect(selectObservedModel(runs, { chatId: null, provider: "claude" })).toBe("newer");
  });

  it("selectObservedModel ignores runs without usage.model", () => {
    const runs = [
      run({ ChatID: 1, StartedAt: "2026-10-23T12:00:00Z" }),
      run({ ChatID: 1, model: "", StartedAt: "2026-10-23T12:00:00Z" }),
      run({ ChatID: 9, Provider: "glm", model: "codex-model" }),
    ];
    expect(selectObservedModel(runs, { chatId: 1, provider: "claude" })).toBeUndefined();
    expect(selectObservedModel(undefined, { chatId: 1, provider: "claude" })).toBeUndefined();
  });
});
