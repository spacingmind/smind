// The pinned "Needs you" section (docs/plans/active/home-and-sidebar-ui.md PR 2,
// AC9/AC10): the needs-attention bucket's tasks listed above the workspaces in
// the default view, without leaving the tree.
import { fireEvent, screen, within } from "@testing-library/react";
import { renderWithClientProp as render } from "@/test/render-with-daemon";
import { afterEach, describe, expect, it, vi } from "vitest";

import { AppSidebar } from "@/components/app-sidebar";
import { SidebarProvider } from "@/components/ui/sidebar";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { AttentionReason, TaskAttention } from "@/hooks/use-task-attention";
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
  Branch: null,
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
  ArchivedAt: null,
};

const PERMISSION_ATTENTION: TaskAttention = new Map([[TASK.ID, new Set<AttentionReason>(["permission"])]]);
const FINISHED_ATTENTION: TaskAttention = new Map([[TASK.ID, new Set<AttentionReason>(["finished"])]]);

async function flush(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}

/** Drives the workspace.list -> {space.list, task.list} sequence useWorkspaceTree makes for a single workspace. */
async function resolveWorkspaceTree(client: FakeWsClient, workspace: Workspace, spaces: Space[], tasks: Task[]): Promise<void> {
  client.nth("workspace.list", 0).resolve([workspace]);
  await flush();
  client.nth("space.list", 0).resolve(spaces);
  client.nth("task.list", 0).resolve(tasks);
  await flush();
}

async function mountSidebar(props: Partial<Parameters<typeof AppSidebar>[0]> = {}) {
  const client = new FakeWsClient();
  const utils = render(
    <SidebarProvider>
      <AppSidebar client={client as never} selectedTaskId={null} {...props} />
    </SidebarProvider>,
  );
  await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);
  await screen.findByTestId("sidebar-workspace-row");
  return { client, ...utils };
}

afterEach(() => {
  window.localStorage.clear();
});

describe("pinned Needs you section (AC9)", () => {
  it("pinned-needs-you-appears-only-when-a-task-needs-the-person", async () => {
    const bare = await mountSidebar();
    expect(screen.queryByTestId("sidebar-needs-you-section")).not.toBeInTheDocument();
    bare.unmount();

    await mountSidebar({ attention: PERMISSION_ATTENTION });
    const section = await screen.findByTestId("sidebar-needs-you-section");
    expect(within(section).getByTestId("sidebar-needs-you-section-header")).toHaveTextContent("Needs you");
    expect(within(section).getByText("Fix the bug")).toBeInTheDocument();

    // The status view keeps its own bucket instead of the pinned section.
    fireEvent.click(screen.getByTestId("sidebar-group-mode-status"));
    expect(screen.queryByTestId("sidebar-needs-you-section")).not.toBeInTheDocument();

    // Back in the tree view, a search hides it too (the results are flat).
    fireEvent.click(screen.getByTestId("sidebar-group-mode-tree"));
    await screen.findByTestId("sidebar-needs-you-section");
    fireEvent.click(screen.getByTestId("sidebar-search-toggle"));
    fireEvent.change(screen.getByTestId("sidebar-search-input"), { target: { value: "fix" } });
    expect(screen.queryByTestId("sidebar-needs-you-section")).not.toBeInTheDocument();
  });

  it("pinned-section-hidden-while-searching", async () => {
    await mountSidebar({ attention: PERMISSION_ATTENTION });
    await screen.findByTestId("sidebar-needs-you-section");

    fireEvent.click(screen.getByTestId("sidebar-search-toggle"));
    fireEvent.change(screen.getByTestId("sidebar-search-input"), { target: { value: "fix" } });

    expect(screen.queryByTestId("sidebar-needs-you-section")).not.toBeInTheDocument();
    fireEvent.change(screen.getByTestId("sidebar-search-input"), { target: { value: "" } });
    expect(await screen.findByTestId("sidebar-needs-you-section")).toBeInTheDocument();
  });

  it("pinned-needs-you-excludes-tasks-that-only-finished", async () => {
    // An unseen finish is not blocked on the person: no pinned section.
    const finished = await mountSidebar({ attention: FINISHED_ATTENTION });
    expect(await screen.findByText("Fix the bug")).toBeInTheDocument();
    expect(screen.queryByTestId("sidebar-needs-you-section")).not.toBeInTheDocument();
    finished.unmount();

    // A permission ask is: the section appears.
    await mountSidebar({ attention: PERMISSION_ATTENTION });
    expect(await screen.findByTestId("sidebar-needs-you-section")).toBeInTheDocument();
  });

  it("pinned-needs-you-rows-select-the-same-task", async () => {
    const onSelectTask = vi.fn();
    await mountSidebar({ attention: PERMISSION_ATTENTION, onSelectTask });

    const section = await screen.findByTestId("sidebar-needs-you-section");
    fireEvent.click(within(section).getByTestId("sidebar-task-row"));

    expect(onSelectTask).toHaveBeenCalledWith(TASK);
  });

  it("task-stays-under-its-workspace-too", async () => {
    await mountSidebar({ attention: PERMISSION_ATTENTION });

    const section = await screen.findByTestId("sidebar-needs-you-section");
    const workspaceLi = screen.getByTestId("sidebar-workspace-row").closest("li")!;
    const rows = screen
      .getAllByTestId("sidebar-task-item")
      .filter((el) => el.dataset.taskId === String(TASK.ID));
    expect(rows).toHaveLength(2);
    expect(rows.some((row) => section.contains(row))).toBe(true);
    expect(rows.some((row) => workspaceLi.contains(row))).toBe(true);
  });
});
