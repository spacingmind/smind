import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

// Source pins on App.tsx: the hook knows tasks, not workspaces, so App is where a task's workspace is looked up (D3).
describe("split ratios wiring (source pins on App.tsx)", () => {
  const app = readFileSync(resolve(__dirname, "../App.tsx"), "utf8");

  it("App hands useTaskTabs the per-workspace ratio port, read and write both keyed by the task's own workspace", () => {
    expect(app).toContain("useTaskTabs({");
    expect(app).toContain("splitRatios: {");
    expect(app).toContain("workspaceId === undefined ? undefined : readSplitRatios(workspaceId)");
    expect(app).toContain("if (workspaceId !== undefined) rememberSplitRatio(workspaceId, direction, firstFraction);");
    expect(app).toContain('allTasks.find((t) => t.ID === taskId)?.WorkspaceID');
    expect(app).toContain('import { readSplitRatios, rememberSplitRatio } from "@/lib/split-ratios";');
  });
});
