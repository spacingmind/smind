import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { renderWithClientProp as render } from "@/test/render-with-daemon";
import { describe, expect, it, vi } from "vitest";

import { AppSidebar } from "@/components/app-sidebar";
import { PaletteProvider, usePalette } from "@/palette/palette-provider";
import { SidebarProvider } from "@/components/ui/sidebar";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { Task, Workspace } from "@/lib/types";

const WORKSPACE: Workspace = {
  ID: 1,
  Path: "/tmp/ws",
  Title: "My Workspace",
  RoutingPolicy: "",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
};

const WORKSPACE_2: Workspace = {
  ...WORKSPACE,
  ID: 2,
  Path: "/tmp/ws2",
  Title: "Second Workspace",
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

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

async function resolveWorkspaceTree(
  client: FakeWsClient,
  workspace: Workspace,
  tasks: Task[] = [TASK],
): Promise<void> {
  client.nth("workspace.list", 0).resolve([workspace]);
  await flush();
  client.nth("space.list", 0).resolve([]);
  client.nth("task.list", 0).resolve(tasks);
  await flush();
}

/** Two workspaces: the tree fetches space.list/task.list per workspace, so the second workspace's calls are index 1. */
async function resolveTwoWorkspaceTree(client: FakeWsClient): Promise<void> {
  client.nth("workspace.list", 0).resolve([WORKSPACE, WORKSPACE_2]);
  await flush();
  client.nth("space.list", 0).resolve([]);
  client.nth("task.list", 0).resolve([TASK]);
  client.nth("space.list", 1).resolve([]);
  client.nth("task.list", 1).resolve([]);
  await flush();
}

/** Displays the palette context's open flag, so a test can see the sidebar opened it. */
function PaletteOpenProbe() {
  const { open } = usePalette();
  return <span data-testid="palette-open">{String(open)}</span>;
}

function mount(client: FakeWsClient, onOpenSettings = vi.fn(), paletteProbe = false, onOpenAutomations = vi.fn()) {
  return render(
    <PaletteProvider>
      <SidebarProvider>
        <AppSidebar
          client={client as never}
          selectedTaskId={null}
          onSelectTask={vi.fn()}
          onOpenSettings={onOpenSettings}
          onOpenAutomations={onOpenAutomations}
        />
        {paletteProbe && <PaletteOpenProbe />}
      </SidebarProvider>
    </PaletteProvider>,
  );
}

describe("AppSidebar quick actions", () => {
  it("New task opens the task dialog for the only workspace", async () => {
    const client = new FakeWsClient();
    const onOpenSettings = vi.fn();
    const onOpenAutomations = vi.fn();
    mount(client, onOpenSettings, false, onOpenAutomations);
    await resolveWorkspaceTree(client, WORKSPACE);

    fireEvent.click(screen.getByTestId("sidebar-quick-new-task"));
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toHaveTextContent("New task");

    fireEvent.click(screen.getByTestId("sidebar-quick-automations"));
    expect(onOpenAutomations).toHaveBeenCalledTimes(1);
    expect(onOpenSettings).not.toHaveBeenCalled();
  });

  it("New task with several workspaces and no selection opens the palette", async () => {
    const client = new FakeWsClient();
    mount(client, vi.fn(), true);
    await resolveTwoWorkspaceTree(client);

    fireEvent.click(screen.getByTestId("sidebar-quick-new-task"));
    await waitFor(() => expect(screen.getByTestId("palette-open")).toHaveTextContent("true"));
  });

  it("quick actions are absent with no workspaces", async () => {
    const client = new FakeWsClient();
    mount(client);
    client.nth("workspace.list", 0).resolve([]);
    await flush();

    expect(screen.queryByTestId("sidebar-quick-actions")).toBeNull();
  });

  it("tree header reads Task list", async () => {
    const client = new FakeWsClient();
    mount(client);
    await resolveWorkspaceTree(client, WORKSPACE);

    expect(screen.getByText("Task list")).toBeInTheDocument();
    expect(screen.queryByText("Workspaces")).toBeNull();
  });
});
