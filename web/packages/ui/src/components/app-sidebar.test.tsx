import { act } from "react";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { renderWithClientProp as render } from "@/test/render-with-daemon";
import { afterEach, describe, expect, it, vi } from "vitest";

import { AppSidebar } from "@/components/app-sidebar";
import { PaletteProvider, useRegisteredCommands } from "@/palette/palette-provider";
import { SidebarProvider } from "@/components/ui/sidebar";
import { FakeWsClient } from "@/test/fake-ws-client";
import { gitItem, openHoverCard, revealWorkspaceActions, taskItem, taskTrigger } from "@/test/sidebar-helpers";
import type { AttentionReason, TaskAttention } from "@/hooks/use-task-attention";
import type { LocalPathActions } from "@/hooks/use-local-path-actions";
import type { AgentProfile, Space, Task, TaskGit, Workspace } from "@/lib/types";

let mockLocalPathActions: LocalPathActions | null = null;
vi.mock("@/hooks/use-local-path-actions", () => ({
  useLocalPathActions: () => mockLocalPathActions,
}));

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
  describe("workspace row context menu", () => {
    afterEach(() => {
      mockLocalPathActions = null;
    });

    async function mountTree(client: FakeWsClient) {
      render(
        <SidebarProvider>
          <AppSidebar client={client as never} selectedTaskId={null} />
        </SidebarProvider>,
      );
      await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);
      return await screen.findByTestId("sidebar-workspace-row");
    }

    async function labels(container: HTMLElement): Promise<string[]> {
      return within(container)
        .getAllByRole("menuitem")
        .map((el) => el.textContent ?? "");
    }

    it.each([
      ["with localPathActions", true],
      ["without localPathActions", false],
    ])("workspace row context menu mirrors the actions menu (%s)", async (_name, local) => {
      mockLocalPathActions = local
        ? {
            revealLabel: "Reveal in Finder",
            editors: [{ id: "vscode", label: "VS Code", kind: "editor" }] as never,
            reveal: vi.fn(),
            open: vi.fn(),
          }
        : null;
      const client = new FakeWsClient();
      const row = await mountTree(client);

      fireEvent.pointerDown(await revealWorkspaceActions(), { button: 0, ctrlKey: false });
      const dropdownLabels = await labels(await screen.findByRole("menu"));
      fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" });
      await flush();

      fireEvent.contextMenu(row);
      const contextLabels = await labels(await screen.findByTestId("sidebar-workspace-context-menu"));

      expect(contextLabels).toEqual(dropdownLabels);
      expect(contextLabels.includes("Reveal in Finder")).toBe(local);
      expect(contextLabels.includes("Open in VS Code")).toBe(local);
    });

    it("right click on a workspace row does not toggle expansion", async () => {
      const client = new FakeWsClient();
      const row = await mountTree(client);
      const before = screen.queryAllByTestId("sidebar-task-row").length;

      fireEvent.contextMenu(row);
      await screen.findByTestId("sidebar-workspace-context-menu");
      expect(screen.queryAllByTestId("sidebar-task-row")).toHaveLength(before);

      fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" });
      await flush();
      fireEvent.click(row);
      expect(screen.queryAllByTestId("sidebar-task-row")).not.toHaveLength(before);
    });

    it("workspace context menu Delete workspace opens the delete flow", async () => {
      const client = new FakeWsClient();
      const row = await mountTree(client);

      fireEvent.contextMenu(row);
      const menu = await screen.findByTestId("sidebar-workspace-context-menu");
      fireEvent.click(within(menu).getByText("Delete workspace"));

      expect(await screen.findByText("Delete workspace?")).toBeInTheDocument();
    });
  });

  it("sidebar logo sits on a theme-token tile in light theme and is untiled in dark", () => {
    const client = new FakeWsClient();
    render(
      <SidebarProvider>
        <AppSidebar client={client as never} selectedTaskId={null} />
      </SidebarProvider>,
    );

    const logo = screen.getByTestId("sidebar-logo");
    expect(logo.className).toContain("bg-foreground");
    expect(logo.className).toContain("dark:bg-transparent");
    expect(logo.className).toContain("dark:size-5");
    expect(logo.querySelector("img")).not.toBeNull();
  });

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

  describe("task row layout stability (ui-redesign-parity Item 2)", () => {
    it("the status slot is present at the same fixed width whether or not the task has attention", async () => {
      const client = new FakeWsClient();
      const attention: TaskAttention = new Map([[TASK.ID, new Set<AttentionReason>(["error"])]]);

      const { rerender } = render(
        <SidebarProvider>
          <AppSidebar client={client as never} selectedTaskId={null} attention={new Map()} />
        </SidebarProvider>,
      );
      await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);

      const row = await screen.findByTestId("sidebar-task-row");
      const slotWithoutAttention = row.querySelector('[data-testid="task-status-slot"]');
      expect(slotWithoutAttention).toBeInTheDocument();
      expect(slotWithoutAttention?.querySelector('[data-testid="task-attention"]')).not.toBeInTheDocument();
      const classNameWithoutAttention = slotWithoutAttention?.className;

      rerender(
        <SidebarProvider>
          <AppSidebar client={client as never} selectedTaskId={null} attention={attention} />
        </SidebarProvider>,
      );

      const slotWithAttention = screen.getByTestId("task-status-slot");
      // Same reserved-space wrapper, same width classes -- only the dot
      // *inside* it (queried separately, and absent above) is what
      // changes, so the title after it never shifts.
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

      const runSlotBefore = (await screen.findByTestId("task-status-slot")).className;
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
      expect(screen.getByTestId("task-status-slot").className).toBe(runSlotBefore);
      expect(screen.getAllByTestId("row-aggregate-slot")[0]!.className).toBe(aggregateSlotBefore);
      expect(screen.getByTestId("task-run-status")).toBeInTheDocument();
      expect(screen.getAllByTestId("row-aggregate").length).toBeGreaterThan(0);
    });
  });

  describe("unread marker (AC2)", () => {
    it("renders the unread marker only for a task id in the `unread` set", async () => {
      const client = new FakeWsClient();

      render(
        <SidebarProvider>
          <AppSidebar client={client as never} selectedTaskId={null} unread={new Set([TASK.ID])} />
        </SidebarProvider>,
      );
      await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);

      const row = await screen.findByTestId("sidebar-task-row");
      expect(row.querySelector('[data-testid="task-unread-marker"]')).toBeInTheDocument();
    });

    it("renders no unread marker for a task id absent from `unread`, but keeps the slot's width reserved", async () => {
      const client = new FakeWsClient();

      const { rerender } = render(
        <SidebarProvider>
          <AppSidebar client={client as never} selectedTaskId={null} unread={new Set()} />
        </SidebarProvider>,
      );
      await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);

      const slotBefore = (await screen.findByTestId("task-status-slot")).className;
      expect(screen.queryByTestId("task-unread-marker")).not.toBeInTheDocument();

      rerender(
        <SidebarProvider>
          <AppSidebar client={client as never} selectedTaskId={null} unread={new Set([TASK.ID])} />
        </SidebarProvider>,
      );

      expect(screen.getByTestId("task-status-slot").className).toBe(slotBefore);
      expect(screen.getByTestId("task-unread-marker")).toBeInTheDocument();
    });

    it("the task menu's Mark unread action calls onMarkUnread with that task's id", async () => {
      const client = new FakeWsClient();
      const onMarkUnread = vi.fn();

      render(
        <SidebarProvider>
          <AppSidebar client={client as never} selectedTaskId={null} onMarkUnread={onMarkUnread} />
        </SidebarProvider>,
      );
      await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);

      await screen.findByTestId("sidebar-task-row");
      const taskMenu = taskTrigger(TASK.ID);
      fireEvent.pointerDown(taskMenu, { button: 0, ctrlKey: false });
      fireEvent.click(await screen.findByTestId("sidebar-task-mark-unread-action"));

      expect(onMarkUnread).toHaveBeenCalledWith(TASK.ID);
    });
  });

  describe("pinned section (AC4)", () => {
    afterEach(() => {
      window.localStorage.clear();
    });

    it("pinning a task from its menu shows it in a Pinned section above the tree", async () => {
      const client = new FakeWsClient();

      render(
        <SidebarProvider>
          <AppSidebar client={client as never} selectedTaskId={null} />
        </SidebarProvider>,
      );
      await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);

      expect(screen.queryByTestId("sidebar-pinned-section")).not.toBeInTheDocument();

      fireEvent.pointerDown(taskTrigger(TASK.ID), { button: 0, ctrlKey: false });
      fireEvent.click(await screen.findByTestId("sidebar-task-pin-action"));

      const pinned = await screen.findByTestId("sidebar-pinned-section");
      expect(within(pinned).getByText("Fix the bug")).toBeInTheDocument();
    });

    it("unpinning removes it from the Pinned section, and the action label toggles", async () => {
      const client = new FakeWsClient();

      render(
        <SidebarProvider>
          <AppSidebar client={client as never} selectedTaskId={null} />
        </SidebarProvider>,
      );
      await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);

      fireEvent.pointerDown(taskTrigger(TASK.ID), { button: 0, ctrlKey: false });
      fireEvent.click(await screen.findByTestId("sidebar-task-pin-action"));
      await screen.findByTestId("sidebar-pinned-section");

      fireEvent.pointerDown(taskTrigger(TASK.ID), { button: 0, ctrlKey: false });
      expect(await screen.findByTestId("sidebar-task-pin-action")).toHaveTextContent("Unpin");
      fireEvent.click(screen.getByTestId("sidebar-task-pin-action"));

      await waitFor(() => expect(screen.queryByTestId("sidebar-pinned-section")).not.toBeInTheDocument());
    });

    it("persists the pinned set across a remount", async () => {
      const client = new FakeWsClient();

      const { unmount } = render(
        <SidebarProvider>
          <AppSidebar client={client as never} selectedTaskId={null} />
        </SidebarProvider>,
      );
      await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);
      fireEvent.pointerDown(taskTrigger(TASK.ID), { button: 0, ctrlKey: false });
      fireEvent.click(await screen.findByTestId("sidebar-task-pin-action"));
      await screen.findByTestId("sidebar-pinned-section");
      unmount();

      const client2 = new FakeWsClient();
      render(
        <SidebarProvider>
          <AppSidebar client={client2 as never} selectedTaskId={null} />
        </SidebarProvider>,
      );
      await resolveWorkspaceTree(client2, WORKSPACE, [], [TASK]);

      expect(await screen.findByTestId("sidebar-pinned-section")).toBeInTheDocument();
    });

    it("collapsing the Pinned section hides its rows without unpinning", async () => {
      const client = new FakeWsClient();

      render(
        <SidebarProvider>
          <AppSidebar client={client as never} selectedTaskId={null} />
        </SidebarProvider>,
      );
      await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);
      fireEvent.pointerDown(taskTrigger(TASK.ID), { button: 0, ctrlKey: false });
      fireEvent.click(await screen.findByTestId("sidebar-task-pin-action"));
      await screen.findByTestId("sidebar-pinned-section");

      fireEvent.click(screen.getByTestId("sidebar-pinned-section-header"));

      const pinned = screen.getByTestId("sidebar-pinned-section");
      // Still pinned (the section itself remains), but its rows are gone.
      expect(within(pinned).queryByText("Fix the bug")).not.toBeInTheDocument();
    });
  });

  describe("task hover card (AC5)", () => {
    it("shows branch, diff stat and last activity for the hovered task, using only already-fetched data", async () => {
      const client = new FakeWsClient();

      render(
        <SidebarProvider>
          <AppSidebar client={client as never} selectedTaskId={null} />
        </SidebarProvider>,
      );
      await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);
      await screen.findByText("Fix the bug");

      client.nth("task.gitList", 0).resolve({
        items: [gitItem(TASK.ID, "feat/fix-bug", 2, 5, 1)],
      });
      await flush();

      const row = screen.getByTestId("sidebar-task-row");
      vi.useFakeTimers();
      try {
        fireEvent.pointerEnter(row, { pointerId: 1, pointerType: "mouse" });
        act(() => {
          vi.advanceTimersByTime(500);
        });
      } finally {
        vi.useRealTimers();
      }

      const card = await screen.findByTestId("task-hover-card");
      expect(within(card).getByText("feat/fix-bug")).toBeInTheDocument();
      expect(within(card).getByTestId("git-tokens")).toHaveTextContent("+5 −1");
      expect(within(card).getByTestId("task-hover-card-last-activity")).toBeInTheDocument();
    });
  });

  describe("group-by-status view toggle (AC6)", () => {
    afterEach(() => {
      window.localStorage.clear();
    });

    it("defaults to the tree view -- no status groups, the workspace row is present", async () => {
      const client = new FakeWsClient();
      render(
        <SidebarProvider>
          <AppSidebar client={client as never} selectedTaskId={null} />
        </SidebarProvider>,
      );
      await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);

      expect(await screen.findByTestId("sidebar-workspace-row")).toBeInTheDocument();
      expect(screen.queryByTestId("sidebar-status-group")).not.toBeInTheDocument();
    });

    it("toggling shows status groups instead of the workspace tree, and running task lands under Running", async () => {
      const client = new FakeWsClient();
      render(
        <SidebarProvider>
          <AppSidebar
            client={client as never}
            selectedTaskId={null}
            runStatus={new Map([[TASK.ID, "running"]])}
          />
        </SidebarProvider>,
      );
      await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);
      await screen.findByTestId("sidebar-workspace-row");

      fireEvent.click(screen.getByTestId("sidebar-group-mode-status"));

      expect(screen.queryByTestId("sidebar-workspace-row")).not.toBeInTheDocument();
      const groups = screen.getAllByTestId("sidebar-status-group");
      expect(groups).toHaveLength(1);
      expect(groups[0]).toHaveAttribute("data-status-group", "running");
      expect(within(groups[0]!).getByText("Fix the bug")).toBeInTheDocument();
    });

    it("collapsing a status group's header hides its rows", async () => {
      const client = new FakeWsClient();
      render(
        <SidebarProvider>
          <AppSidebar client={client as never} selectedTaskId={null} />
        </SidebarProvider>,
      );
      await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);
      await screen.findByTestId("sidebar-workspace-row");
      fireEvent.click(screen.getByTestId("sidebar-group-mode-status"));

      const group = await screen.findByTestId("sidebar-status-group");
      expect(within(group).getByText("Fix the bug")).toBeInTheDocument();

      fireEvent.click(within(group).getByTestId("sidebar-status-group-header"));
      expect(within(group).queryByText("Fix the bug")).not.toBeInTheDocument();
    });

    it("persists the chosen view across a remount", async () => {
      const client = new FakeWsClient();
      const { unmount } = render(
        <SidebarProvider>
          <AppSidebar client={client as never} selectedTaskId={null} />
        </SidebarProvider>,
      );
      await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);
      await screen.findByTestId("sidebar-workspace-row");
      fireEvent.click(screen.getByTestId("sidebar-group-mode-status"));
      await screen.findByTestId("sidebar-status-group");
      unmount();

      const client2 = new FakeWsClient();
      render(
        <SidebarProvider>
          <AppSidebar client={client2 as never} selectedTaskId={null} />
        </SidebarProvider>,
      );
      await resolveWorkspaceTree(client2, WORKSPACE, [], [TASK]);

      expect(await screen.findByTestId("sidebar-status-group")).toBeInTheDocument();
      expect(screen.queryByTestId("sidebar-workspace-row")).not.toBeInTheDocument();
    });
  });
});

/**
 * Minimal DaemonEvents stub: records listeners per topic, lets the test fire them (same shape as
 * diff-viewer-pane.test.tsx's). `fire` also pushes the notification down the client, as the daemon would: the
 * workspace tree reads it through the query-cache event bridge (renderWithClientProp mounts the real
 * `useDaemonEvents`), while the status overrides still listen on the `events` prop.
 */
function makeEventsStub(client: FakeWsClient) {
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
        client.pushNotification(topic, payload);
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
    const stub = makeEventsStub(client);

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
    const stub = makeEventsStub(client);

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
    const stub = makeEventsStub(client);

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
    await waitFor(() => expect(screen.queryByText("My Workspace")).not.toBeInTheDocument());
    expect(screen.queryByText("Fix the bug")).not.toBeInTheDocument();
  });

  it("an event for a workspace this client does not hold leaves the tree alone", async () => {
    const client = new FakeWsClient();
    const stub = makeEventsStub(client);

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
    const stub = makeEventsStub(client);

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

  it("event.dropped clears a live task.status override, so it can't shadow the freshly-refetched task.Status forever", async () => {
    // ADR 0005's queue is per-connection: a drop can just as well have
    // swallowed the task.status that set this override, and there is no
    // corresponding refetch of "just this task's status" to correct it --
    // only clearing the override (and letting the tree refetch supply the
    // real value) removes the stale shadow.
    const client = new FakeWsClient();
    const stub = makeEventsStub(client);

    render(
      <SidebarProvider>
        <AppSidebar client={client as never} selectedTaskId={null} events={stub.events} />
      </SidebarProvider>,
    );
    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);

    stub.fire("task.status", { taskId: TASK.ID, status: "overridden" });
    // The lifecycle status lives in the row's hover card (sidebar-polish S4).
    expect(within(await openHoverCard(TASK.ID)).getByTestId("sidebar-task-status")).toHaveTextContent("overridden");

    // event.dropped also re-triggers useWorkspaceTree's own refetch
    // (a separate, already-covered concern), which briefly unmounts every
    // row while the fetch is in flight -- resolve this second round trip
    // (index 1) the same way the initial mount's (index 0) was, so the
    // row (and its now-cleared override) comes back.
    stub.fire("event.dropped", { count: 1 });
    await flush();
    client.nth("workspace.list", 1).resolve([WORKSPACE]);
    await flush();
    client.nth("space.list", 1).resolve([]);
    client.nth("task.list", 1).resolve([TASK]);
    await flush();

    expect(within(await openHoverCard(TASK.ID)).getByTestId("sidebar-task-status")).toHaveTextContent(TASK.Status);
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

  it("rate limited task shows the warning dot and hint", async () => {
    const reset = new Date(2030, 0, 1, 17, 35).toISOString();
    const client = renderSidebar({
      runStatus: new Map([[TASK.ID, "error"]]),
      rateLimit: new Map([[TASK.ID, { window: "5h", resetAt: reset }]]),
    });
    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);

    const dot = await screen.findByTestId("task-run-status");
    expect(dot).toHaveAttribute("data-status", "warning");
    expect(dot).toHaveAttribute("data-run-status", "error");
    expect(dot.getAttribute("aria-label")).toMatch(/^latest run error, Rate limited · limit has reset$|^latest run error, Rate limited · resets /);
    expect(screen.getByTestId("sidebar-task-row").getAttribute("aria-label")).toContain("Rate limited · ");
  });

  it("rate limited task with an unseen failure shows a warning attention dot", async () => {
    const client = renderSidebar({
      runStatus: new Map([[TASK.ID, "error"]]),
      rateLimit: new Map([[TASK.ID, {}]]),
      attention: new Map([[TASK.ID, new Set<AttentionReason>(["error"])]]),
    });
    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);

    const dot = await screen.findByTestId("task-attention");
    expect(dot).toHaveAttribute("data-status", "warning");
    expect(dot.getAttribute("aria-label")).toContain("Rate limited · reset time unknown");
  });

  it("plain error task still shows the error dot", async () => {
    const client = renderSidebar({ runStatus: new Map([[TASK.ID, "error"]]) });
    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);

    const dot = await screen.findByTestId("task-run-status");
    expect(dot).toHaveAttribute("data-status", "danger");
    expect(dot).toHaveAttribute("aria-label", "latest run error");
    expect(screen.getByTestId("sidebar-task-row").getAttribute("aria-label")).not.toContain("Rate limited");
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

  it("a task row shows its diff stat on the row and its branch in the hover card once task.gitList reports them", async () => {
    const client = renderSidebar();
    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);
    await screen.findByText("Fix the bug");

    client.nth("task.gitList", 0).resolve({
      items: [gitItem(TASK.ID, "feat/fix-bug", 3, 12, 4)],
    });
    await flush();

    const diffstat = screen.getByTestId("sidebar-task-diffstat");
    expect(diffstat).toHaveTextContent("+12");
    expect(diffstat).toHaveTextContent("-4");
    // The branch is no longer printed on the row...
    expect(screen.getByTestId("sidebar-task-row").textContent).not.toContain("feat/fix-bug");
    // ...it is in the hover card and the row's accessible name.
    expect(within(await openHoverCard(TASK.ID)).getByTestId("sidebar-task-branch")).toHaveTextContent("feat/fix-bug");
    expect(screen.getByTestId("sidebar-task-row")).toHaveAccessibleName(/branch feat\/fix-bug/);
  });

  it("a task the daemon reports no git state for shows no diff stat, and its lifecycle status is in the hover card, never a fabricated zero", async () => {
    const client = renderSidebar();
    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);
    await screen.findByText("Fix the bug");

    client.nth("task.gitList", 0).resolve({ items: [] });
    await flush();

    expect(screen.queryByTestId("sidebar-task-diffstat")).not.toBeInTheDocument();
    const card = await openHoverCard(TASK.ID);
    expect(within(card).getByTestId("sidebar-task-status")).toHaveTextContent(TASK.Status);
  });

  it("it shows the diff stat from task.gitList and nothing when diff is absent", async () => {
    const client = renderSidebar();
    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);
    await screen.findByText("Fix the bug");

    client.nth("task.gitList", 0).resolve({ items: [{ ...gitItem(TASK.ID, "feat/fix-bug", 1, 8, 2), diff: undefined }] });
    await flush();
    expect(screen.queryByTestId("sidebar-task-diffstat")).not.toBeInTheDocument();

    act(() => client.pushNotification("task.git", { taskId: TASK.ID, git: gitItem(TASK.ID, "feat/fix-bug", 1, 8, 2) }));
    await flush();
    expect(screen.getByTestId("sidebar-task-diffstat")).toHaveTextContent("+8 -2");
  });

  it("the hover card shows `branch → base` and the git tokens", async () => {
    const client = renderSidebar();
    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);
    await screen.findByText("Fix the bug");

    client.nth("task.gitList", 0).resolve({
      items: [gitItem(TASK.ID, "feat/fix-bug", 3, 12, 4, { base: { ref: "develop", known: true } })],
    });
    await flush();

    const card = await openHoverCard(TASK.ID);
    expect(within(card).getByTestId("sidebar-task-branch")).toHaveTextContent("feat/fix-bug → develop");
    expect(within(card).getByTestId("git-tokens")).toHaveTextContent("+12");
  });

  it("sidebar-git-glyph mount: it renders at most one glyph and one dot in a row", async () => {
    const client = renderSidebar();
    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);
    await screen.findByText("Fix the bug");

    client.nth("task.gitList", 0).resolve({
      items: [
        gitItem(TASK.ID, "feat/fix-bug", 1, 1, 0, {
          pr: {
            number: 7,
            repo: "o/r",
            url: "https://github.com/o/r/pull/7",
            title: "t",
            state: "open",
            draft: false,
            headRef: "h",
            headSha: "s",
            baseRef: "develop",
            fromFork: false,
            mergeable: "mergeable",
            reviewDecision: "none",
            fetchedAt: "2026-10-22T00:00:00Z",
            stale: "",
            refreshAvailableAt: "",
          },
          base: { ref: "develop", known: true },
          conflict: { state: "conflicts", files: ["a"], checkedAt: "2026-10-22T00:00:00Z" },
          origin: { published: false },
          commits: { sinceFork: 2 },
        }),
      ],
    });
    await flush();

    const row = screen.getByTestId("sidebar-task-row");
    expect(within(row).getAllByTestId("sidebar-git-glyph")).toHaveLength(1);
    expect(within(row).getAllByRole("img", { name: /Pull request #7/ })).toHaveLength(1);
    expect(within(row).getAllByTestId("sidebar-git-dot")).toHaveLength(1);
  });

  it("a clean task shows no diff stat -- zero changed files is not a count worth printing", async () => {
    const client = renderSidebar();
    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);
    await screen.findByText("Fix the bug");

    client.nth("task.gitList", 0).resolve({
      items: [gitItem(TASK.ID, "feat/clean", 0, 0, 0)],
    });
    await flush();

    expect(screen.queryByTestId("sidebar-task-diffstat")).not.toBeInTheDocument();
    expect(within(await openHoverCard(TASK.ID)).getByTestId("sidebar-task-branch")).toHaveTextContent("feat/clean");
  });

  it("the row is one fixed-height line whether or not a stat has arrived: no second meta line", async () => {
    const client = renderSidebar();
    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);

    const itemBefore = (await screen.findByTestId("sidebar-task-item")).className;
    expect(screen.queryByTestId("sidebar-task-meta")).not.toBeInTheDocument();

    client.nth("task.gitList", 0).resolve({
      items: [gitItem(TASK.ID, "feat/fix-bug", 3, 12, 4)],
    });
    await flush();

    // Same height class before and after -- only the right cluster's content
    // changed, so no row below it moves when a stat lands.
    expect(screen.getByTestId("sidebar-task-item").className).toBe(itemBefore);
    expect(itemBefore).toContain("h-8");
    expect(screen.queryByTestId("sidebar-task-meta")).not.toBeInTheDocument();
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

describe("AppSidebar settings entry point (ui-redesign-parity Item 13)", () => {
  it("the settings button asks the shell to open the settings view (Item 4 moved the screen out of the sidebar)", async () => {
    const onOpenSettings = vi.fn();
    const client = new FakeWsClient();
    render(
      <SidebarProvider>
        <AppSidebar client={client as never} selectedTaskId={null} onOpenSettings={onOpenSettings} />
      </SidebarProvider>,
    );
    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);

    expect(onOpenSettings).not.toHaveBeenCalled();
    fireEvent.click(screen.getByTestId("sidebar-settings-button"));
    expect(onOpenSettings).toHaveBeenCalledTimes(1);
  });

  it("the footer Host popover (not a header gear) is the entry to Providers", async () => {
    const onOpenSettings = vi.fn();
    const client = new FakeWsClient();
    render(
      <SidebarProvider>
        <AppSidebar client={client as never} selectedTaskId={null} onOpenSettings={onOpenSettings} />
      </SidebarProvider>,
    );
    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);

    expect(screen.getByTestId("sidebar-settings-button")).toBeInTheDocument();
    // Providers/Agents live in the footer Host popover (sidebar-footer plan D2).
    fireEvent.click(screen.getByTestId("sidebar-footer-host"));
    expect(screen.getByTestId("sidebar-footer-host-providers")).toBeInTheDocument();
  });
});

/** Opens the footer Host popover (Providers/Agents/health rows live inside it). */
function openHost(): void {
  fireEvent.click(screen.getByTestId("sidebar-footer-host"));
}

describe("AppSidebar footer Host popover and agent palette entries (run-config IA)", () => {
  const PROFILE: AgentProfile = {
    ID: 7,
    Name: "Quick Fixes",
    Provider: "claude-native",
    PermissionMode: "", AutoAccept: false,
    ThinkingLevel: "standard",
    Notes: "",
    CreatedAt: "2024-01-01T00:00:00Z",
    UpdatedAt: "2024-01-01T00:00:00Z",
  };

  it("Host popover: Providers row deep-links to Settings -> Providers; Agents row deep-links to Settings -> Agents", async () => {
    const onOpenSettings = vi.fn();
    const client = new FakeWsClient();
    render(
      <PaletteProvider>
        <SidebarProvider>
          <AppSidebar client={client as never} selectedTaskId={null} onOpenSettings={onOpenSettings} />
        </SidebarProvider>
      </PaletteProvider>,
    );
    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);
    await flush();

    // ADR-0015's real Providers section, via the direct onOpenSettings
    // callback -- this footer row has direct prop access, so it doesn't
    // need the window-event indirection the composer's toolbar (outside
    // the sidebar's tree) uses for the same "land on a specific settings
    // section" problem.
    openHost();
    fireEvent.click(screen.getByTestId("sidebar-footer-host-providers"));
    expect(onOpenSettings).toHaveBeenCalledWith("providers");

    openHost();
    fireEvent.click(screen.getByTestId("sidebar-footer-host-agents"));
    expect(onOpenSettings).toHaveBeenCalledWith("agents");
  });

  it("Host popover Agents row shows the profile count", async () => {
    const client = new FakeWsClient();
    render(
      <SidebarProvider>
        <AppSidebar client={client as never} selectedTaskId={null} />
      </SidebarProvider>,
    );
    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);
    client.nth("profile.list", 0).resolve([PROFILE]);
    await flush();

    openHost();
    expect(screen.getByTestId("sidebar-footer-host-agents")).toHaveTextContent("1");
  });

  it("Host popover Providers row shows a health dot + healthy count from provider.test, tested once (not per render)", async () => {
    const client = new FakeWsClient();
    const { rerender } = render(
      <SidebarProvider>
        <AppSidebar client={client as never} selectedTaskId={null} />
      </SidebarProvider>,
    );
    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);
    await flush();

    // No dot/count until provider.list answers.
    openHost();
    expect(screen.queryByTestId("sidebar-footer-providers-health-dot")).not.toBeInTheDocument();

    client.nth("provider.list", 0).resolve({
      providers: [
        { id: "claude-native", label: "Claude Code" },
        { id: "glm", label: "GLM", kind: "cli" },
      ],
    });
    await flush();

    expect(client.nth("provider.test", 0).params).toEqual({ provider: "claude-native" });
    expect(client.nth("provider.test", 1).params).toEqual({ provider: "glm" });

    await act(async () => {
      client.nth("provider.test", 0).resolve({ ok: true, detail: "ok" });
      client.nth("provider.test", 1).resolve({ ok: false, detail: "no binary" });
    });

    const dot = screen.getByTestId("sidebar-footer-providers-health-dot");
    expect(dot).toHaveAttribute("data-status", "warning");
    expect(screen.getByTestId("sidebar-footer-host-providers")).toHaveTextContent("1 healthy");

    // Re-rendering the same client doesn't re-test.
    rerender(
      <SidebarProvider>
        <AppSidebar client={client as never} selectedTaskId={null} />
      </SidebarProvider>,
    );
    await flush();
    expect(client.calls.filter((c) => c.method === "provider.test")).toHaveLength(2);
  });

  it("registers Settings/Agents palette entries and Use agent: <name> per profile", async () => {
    const onOpenSettings = vi.fn();
    const client = new FakeWsClient();
    const useAgentListener = vi.fn();
    const openSettingsListener = vi.fn();
    window.addEventListener("smind:use-agent", useAgentListener);
    window.addEventListener("smind:open-settings", openSettingsListener);
    // useRegisteredCommands is a hook; a probe component inside the same
    // PaletteProvider captures the registry where the test can read it.
    let commands: ReturnType<typeof useRegisteredCommands> = [];
    function Probe() {
      commands = useRegisteredCommands();
      return null;
    }
    render(
      <PaletteProvider>
        <Probe />
        <SidebarProvider>
          <AppSidebar client={client as never} selectedTaskId={null} onOpenSettings={onOpenSettings} />
        </SidebarProvider>
      </PaletteProvider>,
    );
    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);
    client.nth("profile.list", 0).resolve([PROFILE]);
    await flush();
    const byTitle = new Map(commands.map((c) => [c.title, c]));
    expect(byTitle.has("Settings: Agents")).toBe(true);
    expect(byTitle.has("Settings: Providers")).toBe(true);
    expect(byTitle.has("New agent…")).toBe(true);
    expect(byTitle.has("Use agent: Quick Fixes")).toBe(true);
    // The Connection group of Settings is desktop-only (this file runs as a web build).
    expect(byTitle.has("Settings: Daemon server")).toBe(false);
    expect(byTitle.has("Settings: Daemon")).toBe(false);

    // The Agents entries deep-link via the window event (this composer-
    // toolbar-shared channel); Settings -> Providers (ADR-0015) is now a
    // real section, reached via the direct onOpenSettings(sectionId)
    // callback this command already has prop access to.
    byTitle.get("Settings: Agents")!.run();
    expect(openSettingsListener).toHaveBeenCalledTimes(1);
    expect((openSettingsListener.mock.calls[0]![0] as CustomEvent).detail).toEqual({ sectionId: "agents" });
    byTitle.get("New agent…")!.run();
    expect(openSettingsListener).toHaveBeenCalledTimes(2);
    expect((openSettingsListener.mock.calls[1]![0] as CustomEvent).detail).toEqual({ sectionId: "agents" });

    byTitle.get("Settings: Providers")!.run();
    expect(onOpenSettings).toHaveBeenCalledWith("providers");

    byTitle.get("Use agent: Quick Fixes")!.run();
    expect(useAgentListener).toHaveBeenCalledTimes(1);
    expect((useAgentListener.mock.calls[0]![0] as CustomEvent).detail).toEqual(PROFILE);
    window.removeEventListener("smind:use-agent", useAgentListener);
    window.removeEventListener("smind:open-settings", openSettingsListener);
  });
});

describe("AppSidebar collapsed rail (dogfood Item 1)", () => {
  /** Renders with the sidebar icon-collapsed -- SidebarProvider's controlled `open=false` plus collapsible="icon" is exactly what the rail toggle produces. */
  function renderCollapsed() {
    const client = new FakeWsClient();
    render(
      <SidebarProvider open={false}>
        <AppSidebar client={client as never} selectedTaskId={null} />
      </SidebarProvider>,
    );
    return document.querySelector("[data-slot='sidebar']") as HTMLElement;
  }

  it("collapsed mode renders the footer actions as a vertical icon stack and no header actions", () => {
    renderCollapsed();

    const actions = screen.getByTestId("sidebar-footer-actions");
    // Vertical stack (flex-col) under the icon-collapsed variant; jsdom cannot compute CSS, so classes are asserted.
    expect(actions.className).toContain("group-data-[collapsible=icon]:flex-col");
    expect(screen.getByTestId("sidebar-expanded-header").className).toContain("group-data-[collapsible=icon]:hidden");
    expect(screen.queryByTestId("sidebar-collapsed-header-actions")).not.toBeInTheDocument();
    expect(within(actions).getByTestId("theme-toggle-trigger")).toBeInTheDocument();
    expect(within(actions).getByTestId("sidebar-settings-button")).toBeInTheDocument();
    expect(within(actions).getByTestId("sidebar-footer-host")).toBeInTheDocument();
  });

  it("the collapsed footer's settings button still opens settings", () => {
    const onOpenSettings = vi.fn();
    const client = new FakeWsClient();
    render(
      <SidebarProvider open={false}>
        <AppSidebar client={client as never} selectedTaskId={null} onOpenSettings={onOpenSettings} />
      </SidebarProvider>,
    );

    fireEvent.click(screen.getByTestId("sidebar-settings-button"));
    expect(onOpenSettings).toHaveBeenCalledTimes(1);
  });

  it("expanded mode renders the header row with the footer actions horizontal", async () => {
    const client = new FakeWsClient();
    render(
      <SidebarProvider>
        <AppSidebar client={client as never} selectedTaskId={null} />
      </SidebarProvider>,
    );

    expect(screen.getByTestId("sidebar-expanded-header").className.split(" ")).not.toContain("hidden");
    expect(screen.getByTestId("sidebar-footer-actions").className.split(" ")).not.toContain("flex-col");
    expect(screen.getByTestId("sidebar-settings-button")).toBeInTheDocument();
  });
});

/**
 * Live screenshots showed sidebar rows rendering the *tail* of a task title
 * ("e verify commands)") instead of a truncation, at both desktop and
 * tablet widths. The title span had carried `min-w-0 truncate` the whole
 * time: the defect was an ancestor that could not be narrower than its own
 * content, so the clip never had a width to happen against and the row
 * overflowed the scroll viewport horizontally instead.
 *
 * jsdom implements no layout and loads no Tailwind, so `scrollWidth` and
 * `getComputedStyle` are uniformly zero/empty here -- measuring the
 * truncation itself is not possible in this suite. What *is* checkable, and
 * what actually regressed, is the constraint chain: assert it structurally,
 * one link at a time, from the title up to the viewport.
 */
describe("AppSidebar long-title truncation", () => {
  const LONG_TITLE =
    "Item 7a: make the sidebar and side-pane layout resizable, then live-test it at four widths (include verify commands)";

  const LONG_TITLE_TASK: Task = { ...TASK, ID: 77, Title: LONG_TITLE };

  it("leaves no shrink-to-fit box between a task title and the scroll viewport", async () => {
    const client = new FakeWsClient();
    const { container } = render(
      <SidebarProvider>
        <AppSidebar client={client as never} selectedTaskId={null} />
      </SidebarProvider>,
    );
    await resolveWorkspaceTree(client, WORKSPACE, [], [LONG_TITLE_TASK]);

    const title = await screen.findByText(LONG_TITLE);
    // Fade-out overflow (sidebar-polish S7), not a hard ellipsis: it clips
    // (overflow-hidden) and may shrink (min-w-0) like the old truncate did.
    expect(title.className).toContain("overflow-hidden");
    expect(title.className).toContain("min-w-0");
    expect(title.className).not.toContain("truncate");

    const viewport = container.querySelector('[data-slot="scroll-area-viewport"]');
    expect(viewport).not.toBeNull();

    // The root cause: Radix's Viewport hardcodes `display: table` on the
    // wrapper it puts around its children -- a shrink-to-fit box that grows
    // to the widest row instead of taking the viewport's width. It is an
    // inline style, so only an `!important` rule displaces it; see
    // components/ui/scroll-area.tsx.
    expect(viewport?.className).toContain("[&>div]:block!");

    // ...and every flex box between the two has to be allowed to shrink
    // below its content, or one of them silently re-breaks the chain.
    for (let el = title.parentElement; el && el !== viewport; el = el.parentElement) {
      if (!/(^|\s)flex(\s|$)/.test(el.className)) continue;
      expect(
        /(^|\s)(min-w-0|overflow-hidden)(\s|$)/.test(el.className),
        `<${el.tagName.toLowerCase()} class="${el.className}"> sits between the task title and the scroll viewport, so it needs min-w-0 (or overflow-hidden) for the title's truncate to have a width to clip against`,
      ).toBe(true);
    }
  });
});

/**
 * PR-U1: the sidebar tree view's task hierarchy -- children nested under
 * their parent, collapsible with an attention rollup on a collapsed
 * parent. Fixtures carry ParentTaskID exactly as the daemon wire does.
 */
describe("AppSidebar task tree (PR-U1)", () => {
  afterEach(() => {
    window.localStorage.clear();
  });

  const PARENT: Task = { ...TASK, ID: 1, Title: "Parent task" };
  const CHILD: Task = { ...TASK, ID: 2, Title: "Child task", ParentTaskID: 1 };
  const OTHER_CHILD: Task = { ...TASK, ID: 4, Title: "Other child", ParentTaskID: 1 };
  const GRANDCHILD: Task = { ...TASK, ID: 3, Title: "Grandchild task", ParentTaskID: 2 };

  async function mountTree(
    props: Partial<Parameters<typeof AppSidebar>[0]> = {},
    tasks: Task[] = [PARENT, CHILD, OTHER_CHILD, GRANDCHILD],
  ) {
    const client = new FakeWsClient();
    const utils = render(
      <SidebarProvider>
        <AppSidebar client={client as never} {...props} />
      </SidebarProvider>,
    );
    await resolveWorkspaceTree(client, WORKSPACE, [], tasks);
    await screen.findAllByTestId("sidebar-task-row");
    return { client, ...utils };
  }

  /** The row's wrapper: select button, collapse chevron and (when active) actions are siblings inside it. */
  function itemOf(id: number): HTMLElement {
    return rowOf(id).closest<HTMLElement>('[data-testid="sidebar-task-item"]')!;
  }

  function rowOf(id: number): HTMLElement {
    return screen.getAllByTestId("sidebar-task-row").find((r) => r.dataset.taskId === String(id))!;
  }

  it("renders a child nested under its parent", async () => {
    await mountTree();

    const childRow = rowOf(CHILD.ID);
    // Nested, not flat: the child's row lives inside the parent row's
    // own list item.
    expect(rowOf(PARENT.ID).closest("li")!.contains(childRow)).toBe(true);
    // The parent carries the chevron and a supervisor tier label with
    // its child count; the child a plain coder label, no chevron.
    expect(itemOf(PARENT.ID).querySelector('[data-testid="task-collapse-toggle"]')).toBeInTheDocument();
    // A child that is itself a parent (leader) carries a chevron; a leaf
    // grandchild does not.
    expect(itemOf(CHILD.ID).querySelector('[data-testid="task-collapse-toggle"]')).toBeInTheDocument();
    expect(itemOf(GRANDCHILD.ID).querySelector('[data-testid="task-collapse-toggle"]')).not.toBeInTheDocument();
  });

  it("collapsing a parent hides its subtree", async () => {
    await mountTree();

    fireEvent.click(itemOf(PARENT.ID).querySelector('[data-testid="task-collapse-toggle"]')!);

    expect(screen.queryByText("Child task")).not.toBeInTheDocument();
    expect(screen.queryByText("Grandchild task")).not.toBeInTheDocument();
    expect(screen.getByText("Parent task")).toBeInTheDocument();
    // And it persists: a remount rebuilds from localStorage collapsed.
  });

  it("collapsed parent shows the attention rollup of a descendant needing a permission", async () => {
    await mountTree({ attention: new Map([[CHILD.ID, new Set<AttentionReason>(["permission"])]]) });
    fireEvent.click(itemOf(PARENT.ID).querySelector('[data-testid="task-collapse-toggle"]')!);

    const rollup = rowOf(PARENT.ID).querySelector('[data-testid="task-rollup"]');
    expect(rollup).toBeInTheDocument();
    expect(rollup).toHaveAttribute("data-rollup-reason", "permission");
    expect(rollup).toHaveAttribute("aria-label", "1 descendant needs attention: a permission is waiting");
    expect(rollup).toHaveTextContent("1");
  });

  it("collapsed parent rollup shows the most urgent reason and a count", async () => {
    await mountTree({
      attention: new Map([
        [CHILD.ID, new Set<AttentionReason>(["permission"])],
        [GRANDCHILD.ID, new Set<AttentionReason>(["finished"])],
      ]),
      runStatus: new Map([[OTHER_CHILD.ID, "error" as const]]),
    });
    fireEvent.click(itemOf(PARENT.ID).querySelector('[data-testid="task-collapse-toggle"]')!);

    const rollup = rowOf(PARENT.ID).querySelector('[data-testid="task-rollup"]');
    expect(rollup).toBeInTheDocument();
    // error outranks permission and finished, and all three descendants count.
    expect(rollup).toHaveAttribute("data-rollup-reason", "error");
    expect(rollup).toHaveTextContent("3");
  });

  it("expanded parent shows no rollup", async () => {
    await mountTree({ attention: new Map([[CHILD.ID, new Set<AttentionReason>(["error"])]]) });

    expect(rowOf(PARENT.ID).querySelector('[data-testid="task-rollup"]')).not.toBeInTheDocument();
  });

  it("rollup appears live when run.status error arrives for a grandchild", async () => {
    const { rerender, client } = await mountTree({ runStatus: new Map() });
    fireEvent.click(itemOf(PARENT.ID).querySelector('[data-testid="task-collapse-toggle"]')!);
    expect(rowOf(PARENT.ID).querySelector('[data-testid="task-rollup"]')).not.toBeInTheDocument();

    rerender(
      <SidebarProvider>
        <AppSidebar
          client={client as never}
          selectedTaskId={null}
          runStatus={new Map([[GRANDCHILD.ID, "error" as const]])}
        />
      </SidebarProvider>,
    );

    const rollup = rowOf(PARENT.ID).querySelector('[data-testid="task-rollup"]');
    expect(rollup).toBeInTheDocument();
    expect(rollup).toHaveAttribute("data-rollup-reason", "error");
  });

  it("child created via task.created event nests live", async () => {
    const client = new FakeWsClient();
    const stub = makeEventsStub(client);
    render(
      <SidebarProvider>
        <AppSidebar client={client as never} selectedTaskId={null} events={stub.events} />
      </SidebarProvider>,
    );
    await resolveWorkspaceTree(client, WORKSPACE, [], [PARENT, CHILD]);
    await screen.findByText("Child task");

    stub.fire("task.created", { task: GRANDCHILD });

    // The splice lands through the query cache (one batched notification), so assert the settled nesting.
    await waitFor(() => expect(rowOf(CHILD.ID).closest("li")!.contains(rowOf(GRANDCHILD.ID))).toBe(true));
    // The new grandchild promoted its parent to leader.
    expect(rowOf(CHILD.ID)).toHaveAccessibleName("Child task, leader with 1 sub-task, branch fix-bug");
  });

  it("task.archived of a parent leaves its live child as a root row", async () => {
    const client = new FakeWsClient();
    const stub = makeEventsStub(client);
    render(
      <SidebarProvider>
        <AppSidebar client={client as never} selectedTaskId={null} events={stub.events} />
      </SidebarProvider>,
    );
    await resolveWorkspaceTree(client, WORKSPACE, [], [PARENT, CHILD]);
    await screen.findByText("Child task");

    stub.fire("task.archived", { task: PARENT });

    // The orphan child survives as a top-level row: present, and not
    // nested inside any other row's list item.
    const child = await waitFor(() => rowOf(CHILD.ID));
    expect(screen.queryByText("Parent task")).not.toBeInTheDocument();
    expect(screen.getAllByTestId("sidebar-task-row").map((r) => r.dataset.taskId)).toEqual(["2"]);
    expect(child.querySelector('[data-testid="task-tier"]')).not.toBeInTheDocument();
  });

  it("selecting a hidden child expands its ancestors", async () => {
    const { rerender, client } = await mountTree({ selectedTaskId: null }, [PARENT, CHILD, GRANDCHILD]);
    fireEvent.click(itemOf(PARENT.ID).querySelector('[data-testid="task-collapse-toggle"]')!);
    expect(screen.queryByText("Grandchild task")).not.toBeInTheDocument();

    rerender(
      <SidebarProvider>
        <AppSidebar client={client as never} selectedTaskId={GRANDCHILD.ID} />
      </SidebarProvider>,
    );

    expect(await screen.findByText("Grandchild task")).toBeInTheDocument();
    expect(rowOf(GRANDCHILD.ID).getAttribute("data-active")).not.toBeNull();
  });

  it("the tier is not printed on the row; it is in the hover card and the accessible name, and parents keep their child-count chip", async () => {
    await mountTree();

    const parentRow = rowOf(PARENT.ID);
    expect(parentRow.querySelector('[data-testid="task-tier"]')).not.toBeInTheDocument();
    expect(parentRow.textContent).not.toMatch(/supervisor/);
    expect(parentRow).toHaveAccessibleName("Parent task, supervisor with 2 sub-tasks, branch fix-bug");
    // The chip stays: it is the one hierarchy cue visible while collapsed.
    expect(within(parentRow).getByTestId("task-child-count")).toHaveTextContent("2");

    const card = await openHoverCard(PARENT.ID);
    expect(within(card).getByTestId("task-tier")).toHaveTextContent("supervisor · 2 sub-tasks");
  });

  it("siblings of a parent reserve the chevron slot so titles align", async () => {
    // OTHER_ROOT is a childless sibling of PARENT in the same list.
    const OTHER_ROOT: Task = { ...TASK, ID: 9, Title: "Lone root" };
    await mountTree({}, [PARENT, CHILD, OTHER_ROOT]);

    const lone = itemOf(OTHER_ROOT.ID);
    // No toggle button (it has nothing to collapse)...
    expect(lone.querySelector('[data-testid="task-collapse-toggle"]')).not.toBeInTheDocument();
    // ...but it shares its parent-bearing sibling's left padding, so both
    // titles start at one x (the parent's chevron lives in the gutter that
    // padding makes room for).
    expect(lone.style.paddingLeft).toBe(itemOf(PARENT.ID).style.paddingLeft);
    expect(itemOf(PARENT.ID).style.paddingLeft).toBe("12px");
  });

  it("lists without parents reserve no gutter, giving depth-0 titles the room", async () => {
    const LONE_A: Task = { ...TASK, ID: 20, Title: "Lone A" };
    const LONE_B: Task = { ...TASK, ID: 21, Title: "Lone B" };
    await mountTree({}, [LONE_A, LONE_B]);

    for (const row of screen.getAllByTestId("sidebar-task-row")) {
      expect(row.closest<HTMLElement>('[data-testid="sidebar-task-item"]')!.style.paddingLeft).toBe("0px");
    }
  });

  it("reconnect refetch rebuilds the same tree", async () => {
    const client = new FakeWsClient();
    const { rerender } = render(
      <SidebarProvider>
        <AppSidebar client={client as never} selectedTaskId={null} />
      </SidebarProvider>,
    );
    await resolveWorkspaceTree(client, WORKSPACE, [], [PARENT, CHILD, GRANDCHILD]);
    expect(rowOf(CHILD.ID).closest("li")!.contains(rowOf(GRANDCHILD.ID))).toBe(true);

    // A reconnect swaps the client. The tree stays on screen (same daemon: the cache is kept) and is
    // re-read in the background once the new connection's events.subscribe has been answered.
    const client2 = new FakeWsClient();
    rerender(
      <SidebarProvider>
        <AppSidebar client={client2 as never} selectedTaskId={null} />
      </SidebarProvider>,
    );
    expect(rowOf(CHILD.ID)).toBeInTheDocument();
    expect(client2.calls.some((c) => c.method === "workspace.list")).toBe(false);
    await act(async () => client2.nth("events.subscribe").resolve(null));
    await resolveWorkspaceTree(client2, WORKSPACE, [], [PARENT, CHILD, GRANDCHILD]);

    const child = await waitFor(() => rowOf(CHILD.ID));
    expect(rowOf(PARENT.ID).closest("li")!.contains(child)).toBe(true);
    expect(child.closest("li")!.contains(rowOf(GRANDCHILD.ID))).toBe(true);
  });
});

describe("AppSidebar stage view", () => {
  const OTHER_WS: Workspace = { ...WORKSPACE, ID: 2, Path: "/tmp/other", Title: "Other WS" };
  const mk = (id: number, title: string, updatedAt: string): Task => ({ ...TASK, ID: id, Title: title, UpdatedAt: updatedAt });
  const READY_TASK = mk(1, "Ready task", "2026-10-01T00:00:00Z");
  const NEWER_READY = mk(2, "Newer ready", "2026-10-02T00:00:00Z");
  const DONE_TASK = mk(3, "Done task", "2026-10-01T00:00:00Z");
  const AHEAD = { commits: { aheadOfBase: 2 } } as Partial<TaskGit>;
  const MERGED = { pr: { state: "merged" } } as unknown as Partial<TaskGit>;

  function mount(client: FakeWsClient, props: Partial<Parameters<typeof AppSidebar>[0]> = {}) {
    return render(
      <SidebarProvider>
        <AppSidebar client={client as never} selectedTaskId={null} {...props} />
      </SidebarProvider>,
    );
  }

  async function mountStage(tasks: Task[], git: TaskGit[], props: Partial<Parameters<typeof AppSidebar>[0]> = {}) {
    const client = new FakeWsClient();
    const view = mount(client, props);
    await resolveWorkspaceTree(client, WORKSPACE, [], tasks);
    await screen.findByTestId("sidebar-group-mode-control");
    client.nth("task.gitList", 0).resolve({ items: git });
    await flush();
    fireEvent.click(screen.getByTestId("sidebar-group-mode-stage"));
    return { client, ...view };
  }

  const group = (key: string) => document.querySelector<HTMLElement>(`[data-testid="sidebar-stage-group"][data-stage-group="${key}"]`);
  const titlesIn = (el: HTMLElement) => within(el).queryAllByTestId("task-title").map((t) => t.textContent);

  it("defaults to the workspace tree", async () => {
    const client = new FakeWsClient();
    mount(client);
    await resolveWorkspaceTree(client, WORKSPACE, [], [TASK]);
    expect(await screen.findByTestId("sidebar-workspace-row")).toBeInTheDocument();
    expect(screen.queryByTestId("sidebar-stage-group")).not.toBeInTheDocument();
    expect(screen.getByTestId("sidebar-group-mode-tree")).toHaveAttribute("aria-checked", "true");
  });

  it("the Stage segment shows stage groups and the choice persists across a remount", async () => {
    const { unmount } = await mountStage([READY_TASK], [gitItem(1, "b", 1, 1, 1, AHEAD)]);
    expect(screen.queryByTestId("sidebar-workspace-row")).not.toBeInTheDocument();
    expect(group("ready")).toBeInTheDocument();
    expect(screen.getByTestId("sidebar-group-mode-stage")).toHaveAttribute("aria-checked", "true");
    unmount();

    const client2 = new FakeWsClient();
    mount(client2);
    await resolveWorkspaceTree(client2, WORKSPACE, [], [READY_TASK]);
    client2.nth("task.gitList", 0).resolve({ items: [gitItem(1, "b", 1, 1, 1, AHEAD)] });
    await flush();
    expect(await screen.findByTestId("sidebar-stage-group")).toBeInTheDocument();
    expect(screen.queryByTestId("sidebar-workspace-row")).not.toBeInTheDocument();
  });

  it("Done is collapsed by default with its count, expands on click, and the choice persists", async () => {
    const git = [gitItem(3, "d", 1, 1, 1, MERGED)];
    const { unmount } = await mountStage([DONE_TASK], git);
    const done = group("done")!;
    expect(within(done).getByTestId("stage-group-count")).toHaveTextContent("1");
    expect(within(done).getByTestId("sidebar-stage-group-header")).toHaveAttribute("aria-expanded", "false");
    expect(within(done).queryByText("Done task")).not.toBeInTheDocument();

    fireEvent.click(within(done).getByTestId("sidebar-stage-group-header"));
    expect(within(group("done")!).getByText("Done task")).toBeInTheDocument();
    unmount();

    const client2 = new FakeWsClient();
    mount(client2);
    await resolveWorkspaceTree(client2, WORKSPACE, [], [DONE_TASK]);
    client2.nth("task.gitList", 0).resolve({ items: git });
    await flush();
    expect(await screen.findByText("Done task")).toBeInTheDocument();
    expect(within(group("done")!).getByTestId("sidebar-stage-group-header")).toHaveAttribute("aria-expanded", "true");
  });

  it("other groups are expanded by default and collapse persists", async () => {
    await mountStage([READY_TASK], [gitItem(1, "b", 1, 1, 1, AHEAD)]);
    const header = within(group("ready")!).getByTestId("sidebar-stage-group-header");
    expect(header).toHaveAttribute("aria-expanded", "true");
    fireEvent.click(header);
    expect(within(group("ready")!).queryByText("Ready task")).not.toBeInTheDocument();
    expect(JSON.parse(window.localStorage.getItem("smind:sidebar-collapsed-nodes") ?? "[]")).toContain("stage:ready");
  });

  it("sorts a group by UpdatedAt descending", async () => {
    await mountStage(
      [READY_TASK, NEWER_READY],
      [gitItem(1, "a", 1, 1, 1, AHEAD), gitItem(2, "b", 1, 1, 1, AHEAD)],
    );
    expect(titlesIn(group("ready")!)).toEqual(["Newer ready", "Ready task"]);
  });

  it("each row shows its workspace name", async () => {
    const client = new FakeWsClient();
    mount(client);
    client.nth("workspace.list", 0).resolve([WORKSPACE, OTHER_WS]);
    await flush();
    client.nth("space.list", 0).resolve([]);
    client.nth("space.list", 1).resolve([]);
    client.nth("task.list", 0).resolve([READY_TASK]);
    client.nth("task.list", 1).resolve([{ ...NEWER_READY, WorkspaceID: 2 }]);
    await flush();
    client.nth("task.gitList", 0).resolve({ items: [gitItem(1, "a", 1, 1, 1, AHEAD)] });
    await flush();
    fireEvent.click(screen.getByTestId("sidebar-group-mode-stage"));
    const names = screen.getAllByTestId("task-workspace-name").map((n) => n.textContent);
    expect(names.sort()).toEqual(["My Workspace", "Other WS"]);
    expect(within(taskItem(1)).getByTestId("task-workspace-name")).toHaveTextContent("My Workspace");
  });

  it("a task moves Ready -> Working on a running run, and Ready -> Done on a merged PR", async () => {
    const { client, rerender } = await mountStage([READY_TASK], [gitItem(1, "a", 1, 1, 1, AHEAD)]);
    expect(within(group("ready")!).getByText("Ready task")).toBeInTheDocument();

    rerender(
      <SidebarProvider>
        <AppSidebar client={client as never} selectedTaskId={null} runStatus={new Map([[1, "running" as const]])} />
      </SidebarProvider>,
    );
    expect(group("ready")).toBeNull();
    expect(within(group("working")!).getByText("Ready task")).toBeInTheDocument();

    rerender(
      <SidebarProvider>
        <AppSidebar client={client as never} selectedTaskId={null} runStatus={new Map([[1, "done" as const]])} />
      </SidebarProvider>,
    );
    expect(within(group("ready")!).getByText("Ready task")).toBeInTheDocument();

    act(() => client.pushNotification("task.git", { taskId: 1, git: gitItem(1, "a", 1, 1, 1, MERGED) }));
    await flush();
    expect(group("ready")).toBeNull();
    expect(within(group("done")!).getByTestId("stage-group-count")).toHaveTextContent("1");
  });

  it("mounting in stage mode issues the same daemon calls as tree mode", async () => {
    const counts = async (mode: "tree" | "stage") => {
      window.localStorage.clear();
      if (mode === "stage") window.localStorage.setItem("smind:sidebar-group-mode", JSON.stringify("stage"));
      const client = new FakeWsClient();
      const view = mount(client);
      await resolveWorkspaceTree(client, WORKSPACE, [], [READY_TASK]);
      client.nth("task.gitList", 0).resolve({ items: [gitItem(1, "a", 1, 1, 1, AHEAD)] });
      await flush();
      await screen.findByText("Ready task");
      const byMethod: Record<string, number> = {};
      for (const c of client.calls) byMethod[c.method] = (byMethod[c.method] ?? 0) + 1;
      view.unmount();
      return byMethod;
    };
    const tree = await counts("tree");
    const stage = await counts("stage");
    expect(stage).toEqual(tree);
  });
});
