import { act } from "react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
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

const CREATED_TASK: Task = {
  ...TASK,
  ID: 43,
  Title: "Brand new task",
};

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

async function resolveWorkspaceTree(
  client: FakeWsClient,
  workspace: Workspace,
  spaces: Space[] = [],
  tasks: Task[] = [TASK],
): Promise<void> {
  client.nth("workspace.list", 0).resolve([workspace]);
  await flush();
  client.nth("space.list", 0).resolve(spaces);
  client.nth("task.list", 0).resolve(tasks);
  await flush();
}

/** Resolves the refresh fetch's workspace.list (index 1) with a workspace+tree, then its per-workspace list calls (index 1 each). */
async function resolveRefresh(client: FakeWsClient, workspace: Workspace, spaces: Space[], tasks: Task[]): Promise<void> {
  client.nth("workspace.list", 1).resolve([workspace]);
  await flush();
  await flush();
  client.nth("space.list", 1).resolve(spaces);
  client.nth("task.list", 1).resolve(tasks);
  await flush();
}

function mount(client: FakeWsClient, onSelectTask = vi.fn()) {
  render(
    <SidebarProvider>
      <AppSidebar client={client as never} selectedTaskId={null} onSelectTask={onSelectTask} />
    </SidebarProvider>,
  );
  return onSelectTask;
}

describe("AppSidebar CRUD", () => {
  it("renders the first-run guide with a New workspace button when there are no workspaces", async () => {
    const client = new FakeWsClient();
    mount(client);

    client.nth("workspace.list").resolve([]);
    await flush();

    expect(await screen.findByText("Welcome to smind")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "New workspace" })).toBeInTheDocument();
  });

  it("create-workspace dialog requires a path and shows a hint, without calling the RPC", async () => {
    const client = new FakeWsClient();
    mount(client);
    client.nth("workspace.list").resolve([]);
    await flush();

    fireEvent.click(await screen.findByRole("button", { name: "New workspace" }));
    fireEvent.click(await screen.findByRole("button", { name: "Create workspace" }));
    await flush();

    expect(await screen.findByText("Path is required.")).toBeInTheDocument();
    expect(client.calls.filter((c) => c.method === "workspace.create")).toHaveLength(0);
  });

  it("create-workspace submits workspace.create and refreshes the tree on success", async () => {
    const client = new FakeWsClient();
    mount(client);
    client.nth("workspace.list", 0).resolve([]);
    await flush();

    fireEvent.click(await screen.findByRole("button", { name: "New workspace" }));
    fireEvent.change(await screen.findByLabelText("Path"), { target: { value: "/tmp/repo" } });
    fireEvent.click(screen.getByRole("button", { name: "Create workspace" }));

    const create = await waitFor(() => client.nth("workspace.create"));
    expect(create.params).toEqual({ path: "/tmp/repo" });

    await act(async () => {
      create.resolve({ ...WORKSPACE, Path: "/tmp/repo", Title: "" });
    });
    // The initial tree had no workspaces, so this is the first workspace ever
    // shown: its space.list/task.list calls are index 0, not 1 (resolveRefresh
    // assumes a pre-existing workspace already triggered index-0 calls).
    client.nth("workspace.list", 1).resolve([{ ...WORKSPACE, Path: "/tmp/repo", Title: "" }]);
    await flush();
    await flush();
    client.nth("space.list", 0).resolve([]);
    client.nth("task.list", 0).resolve([]);
    await flush();

    expect(await screen.findByText("/tmp/repo")).toBeInTheDocument();
  });

  it("create-workspace surfaces the daemon's error inline (e.g. not a git repo)", async () => {
    const client = new FakeWsClient();
    mount(client);
    client.nth("workspace.list", 0).resolve([]);
    await flush();

    fireEvent.click(await screen.findByRole("button", { name: "New workspace" }));
    fireEvent.change(await screen.findByLabelText("Path"), { target: { value: "/tmp/repo" } });
    fireEvent.click(screen.getByRole("button", { name: "Create workspace" }));

    const create = await waitFor(() => client.nth("workspace.create"));
    await act(async () => {
      create.reject(new Error("workspace.create: not a git repository"));
    });
    await flush();

    expect(await screen.findByText(/not a git repository/)).toBeInTheDocument();
  });

  it("workspace row menu offers Add task and Add space; Add space calls space.create and refreshes", async () => {
    const client = new FakeWsClient();
    mount(client);
    await resolveWorkspaceTree(client, WORKSPACE);

    const menu = await screen.findByRole("button", { name: "Row actions" });
    fireEvent.pointerDown(menu, { button: 0, ctrlKey: false });
    await flush();
    fireEvent.click(await screen.findByText("Add space"));

    fireEvent.change(await screen.findByLabelText("Title"), { target: { value: "New Space" } });
    fireEvent.click(screen.getByRole("button", { name: "Create space" }));

    const create = await waitFor(() => client.nth("space.create"));
    expect(create.params).toEqual({ workspaceId: 1, title: "New Space" });

    await act(async () => {
      create.resolve(SPACE_A);
    });
    await resolveRefresh(client, WORKSPACE, [SPACE_A], [TASK]);

    expect(await screen.findByText("Space A")).toBeInTheDocument();
  });

  it("Add task from workspace level calls task.create without spaceId, selects the new task, and refreshes", async () => {
    const client = new FakeWsClient();
    const onSelectTask = mount(client, vi.fn());
    await resolveWorkspaceTree(client, WORKSPACE);

    const menu = await screen.findByRole("button", { name: "Row actions" });
    fireEvent.pointerDown(menu, { button: 0, ctrlKey: false });
    await flush();
    fireEvent.click(await screen.findByText("Add task"));

    fireEvent.change(await screen.findByLabelText("Title"), { target: { value: "Brand new task" } });
    fireEvent.click(screen.getByRole("button", { name: "Create task" }));

    const create = await waitFor(() => client.nth("task.create"));
    expect(create.params).toEqual({ workspaceId: 1, title: "Brand new task" });

    await act(async () => {
      create.resolve(CREATED_TASK);
    });
    expect(onSelectTask).toHaveBeenCalledWith(CREATED_TASK);
    await resolveRefresh(client, WORKSPACE, [], [TASK, CREATED_TASK]);

    expect(await screen.findByText("Brand new task")).toBeInTheDocument();
  });

  it("task row action opens the archive confirm; confirming calls task.archive and refreshes", async () => {
    const client = new FakeWsClient();
    mount(client);
    await resolveWorkspaceTree(client, WORKSPACE);

    const taskMenu = await screen.findByRole("button", { name: "Actions for Fix the bug" });
    fireEvent.pointerDown(taskMenu, { button: 0, ctrlKey: false });
    await flush();
    fireEvent.click(await screen.findByText("Archive task"));

    // Confirm copy mentions checkpointing, per the spec.
    expect(await screen.findByText(/checkpointed to the task branch/i)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Archive" }));

    const archive = await waitFor(() => client.nth("task.archive"));
    expect(archive.params).toEqual({ id: 42 });

    await act(async () => {
      archive.resolve(TASK);
    });
    await resolveRefresh(client, WORKSPACE, [], []);

    await waitFor(() => expect(screen.queryByText("Fix the bug")).not.toBeInTheDocument());
  });
});
