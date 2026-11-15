import { describe, expect, it } from "vitest";

import { isTaskWindowLabel } from "@/lib/task-window";

describe("isTaskWindowLabel", () => {
  it.each(["task-1", "task-2", "task-8"])("%s is a task window", (label) => {
    expect(isTaskWindowLabel(label)).toBe(true);
  });
  it.each(["main", "task-0", "task-9", "task-10", "task-01", "task-", "Task-1", "xtask-1", "task-1 ", "", undefined, null, 1])("%s is not", (label) => {
    expect(isTaskWindowLabel(label)).toBe(false);
  });
});
