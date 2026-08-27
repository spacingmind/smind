import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { AppSidebar } from "@/components/app-sidebar";
import { SidebarProvider } from "@/components/ui/sidebar";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { Space, Task, Workspace } from "@/lib/types";

const WORKSPACE: Workspace = {
  ID: 1,
  Path: "/tmp/ws",
  Title: "My Workspace",
  RoutingPolicy: "",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
};

const TASK: Task = {
  ID: 42,
  WorkspaceID: 1,
  SpaceID: null,
  Title: "Fix the bug",
  Status: "active",
  WorktreePath: null,
  Branch: "fix-bug",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
  ArchivedAt: null,
};

const SPACE_A: Space = {
  ID: 10,
  WorkspaceID: 1,
  Title: "Space A",
  EnvData: "",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
};

const SPACE_B: Space = {
  ID: 11,
  WorkspaceID: 1,
  Title: "Space B",
  EnvData: "",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
};

const TASK_IN_SPACE_A: Task = {
  ID: 100,
  WorkspaceID: 1,
  SpaceID: 10,
  Title: "Task in Space A",
  Status: "active",
  WorktreePath: null,
  Branch: null,
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
  ArchivedAt: null,
};

const TASK_IN_SPACE_B: Task = {
  ID: 101,
  WorkspaceID: 1,
  SpaceID: 11,
  Title: "Task in Space B",
  Status: "active",
  WorktreePath: null,
  Branch: null,
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
  ArchivedAt: null,
};

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

/** Drives the workspace.list -> {space.list, task.list} sequence useWorkspaceTree makes for a single workspace. */
async function resolveWorkspaceTree(client: FakeWsClient, workspace: Workspace, spaces: Space[], tasks: Task[]): Promise<void> {
  client.nth("workspace.list", 0).resolve([workspace]);
  await flush();
  client.nth("space.list", 0).resolve(spaces);
  client.nth("task.list", 0).resolve(tasks);
  await flush();
}

describe("AppSidebar", () => {
  it("clicking a task row invokes onSelectTask with that task", async () => {
    // AppSidebar's client prop is typed WsClient, not WsClientLike, since
    // it needs .close() elsewhere in the app -- FakeWsClient only
    // implements the call/callStream surface useWorkspaceTree actually
    // uses, so it's cast through unknown here rather than widening the
    // component's real prop type just for this test.
    const client = new FakeWsClient();
    const onSelectTask = vi.fn();

    render(
      <SidebarProvider>
        <AppSidebar client={client as never} selectedTaskId={null} onSelectTask={onSelectTask} />
      </SidebarProvider>,
    );

    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);

    const row = await screen.findByText("Fix the bug");
    fireEvent.click(row);

    expect(onSelectTask).toHaveBeenCalledTimes(1);
    expect(onSelectTask).toHaveBeenCalledWith(TASK);
  });

  it("a workspace with zero spaces renders its tasks flat, same as before space grouping existed", async () => {
    const client = new FakeWsClient();

    render(
      <SidebarProvider>
        <AppSidebar client={client as never} selectedTaskId={null} />
      </SidebarProvider>,
    );

    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);

    await screen.findByText("Fix the bug");
    // No space-grouping UI should appear when the workspace has no spaces.
    expect(screen.queryByText("Ungrouped")).not.toBeInTheDocument();
    expect(screen.queryByText("Space A")).not.toBeInTheDocument();
  });

  it("a workspace with two spaces plus an ungrouped task renders all three groupings, nothing dropped", async () => {
    const client = new FakeWsClient();

    render(
      <SidebarProvider>
        <AppSidebar client={client as never} selectedTaskId={null} />
      </SidebarProvider>,
    );

    await resolveWorkspaceTree(client, WORKSPACE, [SPACE_A, SPACE_B], [TASK_IN_SPACE_A, TASK_IN_SPACE_B, TASK]);

    // Both spaces are visible as their own groups.
    await screen.findByText("Space A");
    await screen.findByText("Space B");
    // Each space's own task is visible.
    await screen.findByText("Task in Space A");
    await screen.findByText("Task in Space B");
    // The ungrouped task is visible too, under an explicit bucket.
    await screen.findByText("Ungrouped");
    await screen.findByText("Fix the bug");
  });

  it("selecting a task inside a space invokes onSelectTask with the same Task shape as an ungrouped one", async () => {
    const client = new FakeWsClient();
    const onSelectTask = vi.fn();

    render(
      <SidebarProvider>
        <AppSidebar client={client as never} selectedTaskId={null} onSelectTask={onSelectTask} />
      </SidebarProvider>,
    );

    await resolveWorkspaceTree(client, WORKSPACE, [SPACE_A, SPACE_B], [TASK_IN_SPACE_A, TASK_IN_SPACE_B, TASK]);

    const row = await screen.findByText("Task in Space A");
    fireEvent.click(row);

    expect(onSelectTask).toHaveBeenCalledTimes(1);
    expect(onSelectTask).toHaveBeenCalledWith(TASK_IN_SPACE_A);
  });
});
