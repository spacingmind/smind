import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
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

  describe("notifications toggle", () => {
    /** jsdom has no Notification API -- installs a minimal fake so the toggle's "default" (clickable) state is reachable at all; without it useNotificationPermission reports "unsupported" and the button stays disabled, which a separate test below covers directly. */
    function installFakeNotification(initialPermission: NotificationPermission) {
      const requestPermission = vi
        .fn<() => Promise<NotificationPermission>>()
        .mockResolvedValue("granted");
      class FakeNotification {
        static permission: NotificationPermission = initialPermission;
        static requestPermission = requestPermission;
      }
      vi.stubGlobal("Notification", FakeNotification);
      return requestPermission;
    }

    afterEach(() => {
      vi.unstubAllGlobals();
    });

    it("never requests Notification permission on mount -- only an explicit click on the toggle does", async () => {
      const requestPermission = installFakeNotification("default");
      const client = new FakeWsClient();

      render(
        <SidebarProvider>
          <AppSidebar client={client as never} selectedTaskId={null} />
        </SidebarProvider>,
      );
      await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);

      expect(requestPermission).not.toHaveBeenCalled();

      fireEvent.click(screen.getByTestId("notifications-toggle"));
      await flush();

      expect(requestPermission).toHaveBeenCalledTimes(1);
    });

    it("without a Notification API at all, the toggle renders disabled instead of throwing", async () => {
      const client = new FakeWsClient();

      render(
        <SidebarProvider>
          <AppSidebar client={client as never} selectedTaskId={null} />
        </SidebarProvider>,
      );
      await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);

      const button = screen.getByTestId("notifications-toggle");
      expect(button).toBeDisabled();
      expect(() => fireEvent.click(button)).not.toThrow();
    });
  });

  describe("task row layout stability (ui-redesign-parity Item 2)", () => {
    it("the attention-dot slot is present at the same fixed width whether or not the task has attention", async () => {
      const client = new FakeWsClient();
      const attention: TaskAttention = new Map([[TASK.ID, new Set<AttentionReason>(["error"])]]);

      const { rerender } = render(
        <SidebarProvider>
          <AppSidebar client={client as never} selectedTaskId={null} attention={new Map()} />
        </SidebarProvider>,
      );
      await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);

      const row = await screen.findByTestId("sidebar-task-row");
      const slotWithoutAttention = row.querySelector('[data-testid="task-attention-slot"]');
      expect(slotWithoutAttention).toBeInTheDocument();
      expect(slotWithoutAttention?.querySelector('[data-testid="task-attention"]')).not.toBeInTheDocument();
      const classNameWithoutAttention = slotWithoutAttention?.className;

      rerender(
        <SidebarProvider>
          <AppSidebar client={client as never} selectedTaskId={null} attention={attention} />
        </SidebarProvider>,
      );

      const slotWithAttention = screen.getByTestId("task-attention-slot");
      // Same reserved-space wrapper, same width classes -- only the dot
      // *inside* it (queried separately, and absent above) is what
      // changes, so the sidebar-task-status label after it never shifts.
      expect(slotWithAttention.className).toBe(classNameWithoutAttention);
      expect(slotWithAttention.querySelector('[data-testid="task-attention"]')).toBeInTheDocument();
    });
  });
});

/** Minimal DaemonEvents stub: records listeners per topic, lets the test fire them (same shape as diff-viewer-pane.test.tsx's). */
function makeEventsStub() {
  const listeners = new Map<string, Set<(payload: unknown) => void>>();
  return {
    events: {
      subscribe(topic: string, listener: (payload: unknown) => void): () => void {
        let set = listeners.get(topic);
        if (!set) {
          set = new Set();
          listeners.set(topic, set);
        }
        set.add(listener);
        return () => set!.delete(listener);
      },
    },
    fire(topic: string, payload: unknown): void {
      act(() => {
        for (const l of listeners.get(topic) ?? []) l(payload);
      });
    },
  };
}

/**
 * ADR 0009's lifecycle topics, from this tab's point of view: every event
 * here stands in for a mutation made somewhere else -- a second browser
 * tab, or the CLI, both of which drive the same daemon over the same /ws
 * -- so the assertion in each case is that the tree changes with no
 * further RPC.
 */
describe("AppSidebar lifecycle events (ui-redesign-parity Item 16)", () => {
  it("a task created in another client appears without a reload and without a refetch", async () => {
    const client = new FakeWsClient();
    const stub = makeEventsStub();

    render(
      <SidebarProvider>
        <AppSidebar client={client as never} selectedTaskId={null} events={stub.events} />
      </SidebarProvider>,
    );

    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);
    await screen.findByText("Fix the bug");
    const callsBefore = client.calls.length;

    const other: Task = { ...TASK, ID: 43, Title: "Created in the other tab" };
    stub.fire("task.created", { task: other });

    expect(await screen.findByText("Created in the other tab")).toBeInTheDocument();
    // The splice is the whole point: no second workspace.list/task.list.
    expect(client.calls.length).toBe(callsBefore);
  });

  it("the acting client's own refresh and the matching event converge on one row, not two", async () => {
    // ADR 0009: an event is not ordered against the RPC response that
    // caused it, so this client can see task.created for a row its own
    // refetch is about to return (or already returned).
    const client = new FakeWsClient();
    const stub = makeEventsStub();

    render(
      <SidebarProvider>
        <AppSidebar client={client as never} selectedTaskId={null} events={stub.events} />
      </SidebarProvider>,
    );

    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);

    const created: Task = { ...TASK, ID: 43, Title: "Created here" };
    stub.fire("task.created", { task: created });
    // ...and now the local refresh()-driven refetch lands, carrying it too.
    stub.fire("task.created", { task: created });

    expect(await screen.findAllByText("Created here")).toHaveLength(1);
  });

  it("an archived task leaves the tree, and a deleted workspace takes its subtree with it", async () => {
    const client = new FakeWsClient();
    const stub = makeEventsStub();

    render(
      <SidebarProvider>
        <AppSidebar client={client as never} selectedTaskId={null} events={stub.events} />
      </SidebarProvider>,
    );

    await resolveWorkspaceTree(client, WORKSPACE, [SPACE_A], [TASK_IN_SPACE_A, TASK]);
    await screen.findByText("Task in Space A");

    stub.fire("task.archived", { task: TASK_IN_SPACE_A });
    await screen.findByText("Fix the bug");
    expect(screen.queryByText("Task in Space A")).not.toBeInTheDocument();

    stub.fire("workspace.deleted", { id: WORKSPACE.ID });
    expect(screen.queryByText("My Workspace")).not.toBeInTheDocument();
    expect(screen.queryByText("Fix the bug")).not.toBeInTheDocument();
  });

  it("an event for a workspace this client does not hold leaves the tree alone", async () => {
    const client = new FakeWsClient();
    const stub = makeEventsStub();

    render(
      <SidebarProvider>
        <AppSidebar client={client as never} selectedTaskId={null} events={stub.events} />
      </SidebarProvider>,
    );

    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);
    await screen.findByText("Fix the bug");

    stub.fire("task.created", { task: { ...TASK, ID: 44, WorkspaceID: 999, Title: "Elsewhere" } });

    expect(screen.queryByText("Elsewhere")).not.toBeInTheDocument();
    expect(screen.getByText("Fix the bug")).toBeInTheDocument();
  });

  it("event.dropped refetches the tree, since a dropped event may have been a create or a delete", async () => {
    const client = new FakeWsClient();
    const stub = makeEventsStub();

    render(
      <SidebarProvider>
        <AppSidebar client={client as never} selectedTaskId={null} events={stub.events} />
      </SidebarProvider>,
    );

    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);
    expect(client.calls.filter((c) => c.method === "workspace.list")).toHaveLength(1);

    stub.fire("event.dropped", { count: 3 });
    await flush();

    expect(client.calls.filter((c) => c.method === "workspace.list")).toHaveLength(2);
  });
});
