import { act, fireEvent, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { AppSidebar } from "@/components/app-sidebar";
import { BRANCH_GONE_SENTENCE } from "@/components/archived-tasks";
import { SidebarProvider } from "@/components/ui/sidebar";
import type { Task, Workspace } from "@/lib/types";
import { RpcError } from "@/lib/ws-client";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp as render } from "@/test/render-with-daemon";

const WORKSPACE: Workspace = {
  ID: 1,
  Path: "/tmp/ws",
  Title: "My Workspace",
  RoutingPolicy: "",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
};

function task(id: number, title: string, over: Partial<Task> = {}): Task {
  return {
    ID: id,
    WorkspaceID: 1,
    SpaceID: null,
    Title: title,
    Status: "active",
    WorktreePath: null,
    Branch: `smind/task-${id}`,
    CreatedAt: "2024-01-01T00:00:00Z",
    UpdatedAt: "2024-01-01T00:00:00Z",
    ArchivedAt: null,
    ...over,
  };
}

const KEPT = task(5, "Kept branch task", { ArchivedAt: new Date(Date.now() - 3 * 3_600_000).toISOString() });
const GONE = task(6, "Deleted branch task", { ArchivedAt: new Date(Date.now() - 2 * 86_400_000).toISOString(), BranchDeletedAt: "2026-10-20T00:00:00Z" });

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 6; i++) await Promise.resolve();
  });
}

/** The nth archived `task.list`; `index` -1 is the latest (an invalidation supersedes an unanswered re-read). */
function archivedCall(client: FakeWsClient, index = 0) {
  const calls = client.calls.filter((c) => c.method === "task.list" && (c.params as { archived?: boolean }).archived);
  const call = calls.at(index);
  if (!call) throw new Error(`no archived task.list #${index} (have ${calls.length})`);
  return call;
}

async function mount(archived: Task[], active: Task[] = []) {
  const client = new FakeWsClient();
  render(
    <SidebarProvider>
      <AppSidebar client={client as never} selectedTaskId={null} />
    </SidebarProvider>,
  );
  client.nth("workspace.list", 0).resolve([WORKSPACE]);
  await flush();
  client.nth("space.list", 0).resolve([]);
  client.calls.find((c) => c.method === "task.list" && !(c.params as { archived?: boolean }).archived)!.resolve(active);
  await flush();
  expect(archivedCall(client).params).toEqual({ workspaceId: 1, archived: true });
  archivedCall(client).resolve(archived);
  await flush();
  return client;
}

describe("Archived view (ADR-0039 §9)", () => {
  it("lists archived tasks with Restore", async () => {
    const client = await mount([KEPT, GONE]);
    // Closed by default, with the count.
    const header = screen.getByTestId("sidebar-archived-header");
    expect(header).toHaveTextContent("Archived (2)");
    expect(header).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByTestId("archived-task-list")).not.toBeInTheDocument();

    fireEvent.click(header);
    const rows = screen.getAllByTestId("archived-task-row");
    expect(rows).toHaveLength(2);
    expect(within(rows[0]).getByTestId("archived-task-title")).toHaveTextContent("Kept branch task");
    expect(within(rows[0]).getByTestId("archived-task-meta")).toHaveTextContent("3h ago · branch kept");
    expect(within(rows[1]).getByTestId("archived-task-meta")).toHaveTextContent("2d ago · branch deleted");

    const restore = within(rows[0]).getByTestId("archived-task-restore");
    expect(restore).toHaveTextContent("Restore");
    expect(restore).toBeEnabled();
    fireEvent.click(restore);
    await flush();
    expect(client.nth("task.restore").params).toEqual({ taskId: 5 });
  });

  it("shows nothing when the workspace has no archived tasks", async () => {
    await mount([]);
    expect(screen.queryByTestId("sidebar-archived")).not.toBeInTheDocument();
  });

  it("disables Restore with the branch_gone sentence", async () => {
    const client = await mount([GONE]);
    fireEvent.click(screen.getByTestId("sidebar-archived-header"));
    const restore = screen.getByTestId("archived-task-restore");
    expect(restore).toBeDisabled();
    expect(restore).toHaveAttribute("title", BRANCH_GONE_SENTENCE);
    expect(restore).toHaveAccessibleDescription(BRANCH_GONE_SENTENCE);
    expect(client.calls.some((c) => c.method === "task.restore")).toBe(false);
  });

  it("shows a typed restore error as one sentence", async () => {
    const client = await mount([KEPT]);
    fireEvent.click(screen.getByTestId("sidebar-archived-header"));
    fireEvent.click(screen.getByTestId("archived-task-restore"));
    await flush();
    await act(async () => {
      client.nth("task.restore").reject(new RpcError("git [worktree add /x]: exit status 128", "branch_gone"));
    });
    await flush();
    const error = screen.getByTestId("archived-task-error");
    expect(error).toHaveTextContent(BRANCH_GONE_SENTENCE);
    expect(error.textContent).not.toMatch(/exit status|worktree add/);
  });

  it("restores a task into the tree live", async () => {
    const client = await mount([KEPT]);
    expect(screen.queryByText("Kept branch task")).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId("sidebar-archived-header"));
    fireEvent.click(screen.getByTestId("archived-task-restore"));
    await flush();

    const restored = { ...KEPT, ArchivedAt: null, WorktreePath: "/tmp/wt" };
    await act(async () => {
      client.nth("task.restore").resolve(restored);
      // The daemon publishes the restored task as it does a created one.
      client.pushNotification("task.created", { task: restored });
    });
    await flush();

    // In the tree from the event alone: the acting client's own tree re-read is still pending.
    expect(await screen.findByTestId("sidebar-task-row")).toHaveTextContent("Kept branch task");
    expect(client.calls.filter((c) => c.method === "workspace.list")).toHaveLength(2);
    // And off the Archived list once its re-read lands.
    await act(async () => archivedCall(client, -1).resolve([]));
    await flush();
    expect(screen.queryByTestId("sidebar-archived")).not.toBeInTheDocument();
  });

  it("moves a task into the Archived list when it is archived", async () => {
    const client = await mount([], [task(8, "About to archive")]);
    expect(await screen.findByTestId("sidebar-task-row")).toBeInTheDocument();
    const archived = { ...task(8, "About to archive"), ArchivedAt: new Date().toISOString() };
    await act(async () => client.pushNotification("task.archived", { task: archived }));
    await flush();
    await act(async () => archivedCall(client, -1).resolve([archived]));
    await flush();
    expect(screen.queryByTestId("sidebar-task-row")).not.toBeInTheDocument();
    expect(screen.getByTestId("sidebar-archived-header")).toHaveTextContent("Archived (1)");
  });
});
