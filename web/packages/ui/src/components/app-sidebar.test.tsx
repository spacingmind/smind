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

    it("the run-status and aggregate slots are equally reserved, before any run exists", async () => {
      const client = new FakeWsClient();

      const { rerender } = render(
        <SidebarProvider>
          <AppSidebar client={client as never} selectedTaskId={null} runStatus={new Map()} />
        </SidebarProvider>,
      );
      await resolveWorkspaceTree(client, WORKSPACE, [SPACE_A], [TASK_IN_SPACE_A]);

      const runSlotBefore = (await screen.findByTestId("task-run-status-slot")).className;
      const aggregateSlotBefore = screen.getAllByTestId("row-aggregate-slot")[0]!.className;
      expect(screen.queryByTestId("task-run-status")).not.toBeInTheDocument();
      expect(screen.queryByTestId("row-aggregate")).not.toBeInTheDocument();

      rerender(
        <SidebarProvider>
          <AppSidebar
            client={client as never}
            selectedTaskId={null}
            runStatus={new Map([[TASK_IN_SPACE_A.ID, "running" as const]])}
          />
        </SidebarProvider>,
      );

      // The dots arrive; the slots holding them do not change width.
      expect(screen.getByTestId("task-run-status-slot").className).toBe(runSlotBefore);
      expect(screen.getAllByTestId("row-aggregate-slot")[0]!.className).toBe(aggregateSlotBefore);
      expect(screen.getByTestId("task-run-status")).toBeInTheDocument();
      expect(screen.getAllByTestId("row-aggregate").length).toBeGreaterThan(0);
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

/**
 * Item 12's sidebar signal: the row state a user reads without opening a
 * task. Every assertion goes through a testid or a data-* attribute, never
 * a colour -- a design-token change must not be able to fail these.
 */
describe("AppSidebar signal (ui-redesign-parity Item 12)", () => {
  function renderSidebar(props: Partial<Parameters<typeof AppSidebar>[0]> = {}) {
    const client = new FakeWsClient();
    render(
      <SidebarProvider>
        <AppSidebar client={client as never} selectedTaskId={null} {...props} />
      </SidebarProvider>,
    );
    return client;
  }

  it("a task with a running run shows the running dot", async () => {
    const client = renderSidebar({ runStatus: new Map([[TASK.ID, "running"]]) });
    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);

    expect(await screen.findByTestId("task-run-status")).toHaveAttribute("data-run-status", "running");
  });

  it("a task that has never run shows no run dot, rather than an idle one", async () => {
    const client = renderSidebar({ runStatus: new Map() });
    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);

    await screen.findByTestId("sidebar-task-row");
    expect(screen.queryByTestId("task-run-status")).not.toBeInTheDocument();
  });

  it("error, finished and permission attention each render a distinguishable marker", async () => {
    const seen = new Map<AttentionReason, string>();

    for (const reason of ["error", "finished", "permission"] as AttentionReason[]) {
      const client = new FakeWsClient();
      const { unmount } = render(
        <SidebarProvider>
          <AppSidebar
            client={client as never}
            selectedTaskId={null}
            attention={new Map([[TASK.ID, new Set<AttentionReason>([reason])]])}
          />
        </SidebarProvider>,
      );
      await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);

      const dot = await screen.findByTestId("task-attention");
      expect(dot).toHaveAttribute("data-attention-reason", reason);
      seen.set(reason, dot.getAttribute("data-status")!);
      unmount();
    }

    // Three reasons, three variants -- they used to share one.
    expect(new Set(seen.values()).size).toBe(3);
  });

  it("a workspace whose task has an error shows the aggregate marker, and its space row does too", async () => {
    const client = renderSidebar({ attention: new Map([[TASK_IN_SPACE_A.ID, new Set<AttentionReason>(["error"])]]) });
    await resolveWorkspaceTree(client, WORKSPACE, [SPACE_A, SPACE_B], [TASK_IN_SPACE_A, TASK_IN_SPACE_B]);

    await screen.findByText("Task in Space A");
    const workspaceAggregate = screen.getByTestId("sidebar-workspace-row").querySelector('[data-testid="row-aggregate"]');
    expect(workspaceAggregate).toHaveAttribute("data-aggregate", "danger");

    const spaceRows = screen.getAllByTestId("sidebar-space-row");
    expect(spaceRows[0]!.querySelector('[data-testid="row-aggregate"]')).toHaveAttribute("data-aggregate", "danger");
    // Space B holds no errored task, so it says nothing.
    expect(spaceRows[1]!.querySelector('[data-testid="row-aggregate"]')).not.toBeInTheDocument();
  });

  it("an idle workspace carries no aggregate dot at all", async () => {
    const client = renderSidebar();
    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);

    await screen.findByText("Fix the bug");
    expect(screen.getByTestId("sidebar-workspace-row").querySelector('[data-testid="row-aggregate"]')).not.toBeInTheDocument();
  });

  it("a task row shows its branch and diff stat once task.stats reports them", async () => {
    const client = renderSidebar();
    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);
    await screen.findByText("Fix the bug");

    client.nth("task.stats", 0).resolve({
      stats: [{ taskId: TASK.ID, branch: "feat/fix-bug", filesChanged: 3, insertions: 12, deletions: 4 }],
    });
    await flush();

    expect(screen.getByTestId("sidebar-task-branch")).toHaveTextContent("feat/fix-bug");
    const diffstat = screen.getByTestId("sidebar-task-diffstat");
    expect(diffstat).toHaveTextContent("3f");
    expect(diffstat).toHaveTextContent("+12");
    expect(diffstat).toHaveTextContent("-4");
  });

  it("a task the daemon reports no stat for falls back to its lifecycle status, never a fabricated zero", async () => {
    const client = renderSidebar();
    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);
    await screen.findByText("Fix the bug");

    client.nth("task.stats", 0).resolve({ stats: [] });
    await flush();

    expect(screen.queryByTestId("sidebar-task-diffstat")).not.toBeInTheDocument();
    expect(screen.queryByTestId("sidebar-task-branch")).not.toBeInTheDocument();
    expect(screen.getByTestId("sidebar-task-status")).toHaveTextContent(TASK.Status);
  });

  it("a clean task shows its branch but no diff stat -- zero changed files is not a count worth printing", async () => {
    const client = renderSidebar();
    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);
    await screen.findByText("Fix the bug");

    client.nth("task.stats", 0).resolve({
      stats: [{ taskId: TASK.ID, branch: "feat/clean", filesChanged: 0, insertions: 0, deletions: 0 }],
    });
    await flush();

    expect(screen.getByTestId("sidebar-task-branch")).toHaveTextContent("feat/clean");
    expect(screen.queryByTestId("sidebar-task-diffstat")).not.toBeInTheDocument();
  });

  it("the meta line is present at a fixed height whether or not a stat has arrived", async () => {
    const client = renderSidebar();
    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);

    const metaBefore = await screen.findByTestId("sidebar-task-meta");
    const classNameBefore = metaBefore.className;

    client.nth("task.stats", 0).resolve({
      stats: [{ taskId: TASK.ID, branch: "feat/fix-bug", filesChanged: 3, insertions: 12, deletions: 4 }],
    });
    await flush();

    // Same reserved line, same height class -- only its contents changed,
    // so no row below it moves when a stat lands.
    expect(screen.getByTestId("sidebar-task-meta").className).toBe(classNameBefore);
  });

  it("selecting a task still invokes onSelectTask with the whole Task, dots or no dots", async () => {
    const onSelectTask = vi.fn();
    const client = new FakeWsClient();
    render(
      <SidebarProvider>
        <AppSidebar
          client={client as never}
          selectedTaskId={null}
          onSelectTask={onSelectTask}
          attention={new Map([[TASK.ID, new Set<AttentionReason>(["permission"])]])}
          runStatus={new Map([[TASK.ID, "running"]])}
        />
      </SidebarProvider>,
    );
    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);

    fireEvent.click(await screen.findByText("Fix the bug"));
    expect(onSelectTask).toHaveBeenCalledWith(TASK);
  });
});

/**
 * The collapsed-search field (ui-redesign-parity Item 12,
 * audit-deepseek-harness.md §3): a header toggle that expands into a
 * field; a non-blank query flattens the tree; an outside click collapses
 * only an empty query.
 */
describe("AppSidebar search (ui-redesign-parity Item 12)", () => {
  it("is collapsed by default and expands into a field on the toggle", async () => {
    const client = new FakeWsClient();
    render(
      <SidebarProvider>
        <AppSidebar client={client as never} selectedTaskId={null} />
      </SidebarProvider>,
    );
    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);

    expect(screen.queryByTestId("sidebar-search-input")).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId("sidebar-search-toggle"));
    expect(screen.getByTestId("sidebar-search-input")).toBeInTheDocument();
  });

  it("a non-blank query replaces the tree with a flat result list", async () => {
    const client = new FakeWsClient();
    render(
      <SidebarProvider>
        <AppSidebar client={client as never} selectedTaskId={null} />
      </SidebarProvider>,
    );
    await resolveWorkspaceTree(client, WORKSPACE, [SPACE_A, SPACE_B], [TASK_IN_SPACE_A, TASK_IN_SPACE_B, TASK]);
    await screen.findByText("Space A");

    fireEvent.click(screen.getByTestId("sidebar-search-toggle"));
    fireEvent.change(screen.getByTestId("sidebar-search-input"), { target: { value: "space a" } });

    // The grouping UI is gone; only the matching task remains, flat.
    expect(screen.queryByText("Space A")).not.toBeInTheDocument();
    expect(screen.queryByText("Space B")).not.toBeInTheDocument();
    expect(screen.getByText("Task in Space A")).toBeInTheDocument();
    expect(screen.queryByText("Task in Space B")).not.toBeInTheDocument();
    expect(screen.queryByText("Fix the bug")).not.toBeInTheDocument();
  });

  it("a query matching nothing shows an empty state, not a blank pane", async () => {
    const client = new FakeWsClient();
    render(
      <SidebarProvider>
        <AppSidebar client={client as never} selectedTaskId={null} />
      </SidebarProvider>,
    );
    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);

    fireEvent.click(screen.getByTestId("sidebar-search-toggle"));
    fireEvent.change(screen.getByTestId("sidebar-search-input"), { target: { value: "nothing matches this" } });

    expect(await screen.findByTestId("sidebar-search-empty")).toBeInTheDocument();
  });

  it("an outside click collapses the field while the query is empty", async () => {
    const client = new FakeWsClient();
    render(
      <div>
        <div data-testid="outside" />
        <SidebarProvider>
          <AppSidebar client={client as never} selectedTaskId={null} />
        </SidebarProvider>
      </div>,
    );
    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);

    fireEvent.click(screen.getByTestId("sidebar-search-toggle"));
    expect(screen.getByTestId("sidebar-search-input")).toBeInTheDocument();

    fireEvent.mouseDown(screen.getByTestId("outside"));
    expect(screen.queryByTestId("sidebar-search-input")).not.toBeInTheDocument();
  });

  it("an outside click does not collapse a field with a typed query", async () => {
    const client = new FakeWsClient();
    render(
      <div>
        <div data-testid="outside" />
        <SidebarProvider>
          <AppSidebar client={client as never} selectedTaskId={null} />
        </SidebarProvider>
      </div>,
    );
    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);

    fireEvent.click(screen.getByTestId("sidebar-search-toggle"));
    fireEvent.change(screen.getByTestId("sidebar-search-input"), { target: { value: "fix" } });

    fireEvent.mouseDown(screen.getByTestId("outside"));
    expect(screen.getByTestId("sidebar-search-input")).toBeInTheDocument();
    expect(screen.getByTestId("sidebar-search-input")).toHaveValue("fix");
  });

  it("clearing the query restores the tree with its previous expansion state", async () => {
    const client = new FakeWsClient();
    render(
      <SidebarProvider>
        <AppSidebar client={client as never} selectedTaskId={null} />
      </SidebarProvider>,
    );
    await resolveWorkspaceTree(client, WORKSPACE, [SPACE_A], [TASK_IN_SPACE_A]);
    await screen.findByText("Space A");

    // Collapse the workspace row before searching -- the state search
    // must leave untouched.
    fireEvent.click(screen.getByTestId("sidebar-workspace-row"));
    expect(screen.queryByText("Space A")).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId("sidebar-search-toggle"));
    fireEvent.change(screen.getByTestId("sidebar-search-input"), { target: { value: "space" } });
    expect(await screen.findByText("Task in Space A")).toBeInTheDocument();

    fireEvent.change(screen.getByTestId("sidebar-search-input"), { target: { value: "" } });

    // Back to the tree, still collapsed -- exactly as it was left.
    expect(screen.queryByText("Space A")).not.toBeInTheDocument();
    expect(screen.getByTestId("sidebar-workspace-row")).toBeInTheDocument();
  });

  it("selecting a task from search results still invokes onSelectTask with the whole Task", async () => {
    const onSelectTask = vi.fn();
    const client = new FakeWsClient();
    render(
      <SidebarProvider>
        <AppSidebar client={client as never} selectedTaskId={null} onSelectTask={onSelectTask} />
      </SidebarProvider>,
    );
    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);

    fireEvent.click(screen.getByTestId("sidebar-search-toggle"));
    fireEvent.change(screen.getByTestId("sidebar-search-input"), { target: { value: "fix" } });
    fireEvent.click(await screen.findByText("Fix the bug"));

    expect(onSelectTask).toHaveBeenCalledWith(TASK);
  });
});
