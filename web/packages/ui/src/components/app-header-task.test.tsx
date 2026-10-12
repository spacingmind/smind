// AH-AC3: the header title's hover/focus card.
import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { HeaderTaskTitle } from "@/components/app-header-task";
import type { Task } from "@/lib/types";

const TASK: Task = {
  ID: 7,
  WorkspaceID: 1,
  SpaceID: null,
  Title: "Fix the bug",
  Status: "active",
  WorktreePath: "/home/dev/repo-wt",
  Branch: "fix-bug",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
  ArchivedAt: null,
};

describe("HeaderTaskTitle", () => {
  it("AH-AC3 hover card shows branch, status, last activity", async () => {
    render(<HeaderTaskTitle task={TASK} />);
    expect(screen.queryByTestId("app-header-task-card")).not.toBeInTheDocument();

    // Radix HoverCard opens on keyboard focus without any timer.
    await act(async () => {
      screen.getByTestId("app-header-title").focus();
    });
    const card = await screen.findByTestId("app-header-task-card");
    expect(card).toHaveTextContent("Last activity");
    expect(screen.getByTestId("app-header-task-branch")).toHaveTextContent("fix-bug");
    expect(screen.getByTestId("app-header-task-worktree")).toHaveTextContent("/home/dev/repo-wt");
    expect(screen.getByTestId("app-header-task-status")).toHaveTextContent(/active/i);
    expect(screen.getByTestId("app-header-task-activity").textContent).not.toBe("");
    fireEvent.blur(screen.getByTestId("app-header-title"));
  });
});
