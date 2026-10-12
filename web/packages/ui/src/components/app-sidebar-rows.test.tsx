// Sidebar polish (docs/plans/active/sidebar-polish.md): row-level behavior of
// the task sidebar -- scoped actions (S1), keyboard (S2), the single status
// slot (S3), the single-line row (S4), container rows (S5), the task context
// menu (S6), title fade + per-row indentation (S7) and persisted expansion
// (S8). The older app-sidebar*.test.tsx files keep the pre-existing contracts.
import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { AppSidebar } from "@/components/app-sidebar";
import { SidebarProvider } from "@/components/ui/sidebar";
import { FakeWsClient } from "@/test/fake-ws-client";
import { hoverTask, taskTrigger, unhoverTask } from "@/test/sidebar-helpers";
import type { Space, Task, Workspace } from "@/lib/types";

const WORKSPACE: Workspace = {
  ID: 1,
  Path: "/tmp/ws",
  Title: "My Workspace",
  RoutingPolicy: "",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
};

function makeTask(id: number, title: string, extra: Partial<Task> = {}): Task {
  return {
    ID: id,
    WorkspaceID: 1,
    SpaceID: null,
    Title: title,
    Status: "active",
    WorktreePath: null,
    Branch: null,
    CreatedAt: "2024-01-01T00:00:00Z",
    UpdatedAt: "2024-01-01T00:00:00Z",
    ArchivedAt: null,
    ...extra,
  };
}

// A three-tier tree: root (supervisor) > leader > coder, plus a sibling leader.
const ROOT = makeTask(1, "Root task");
const LEADER = makeTask(2, "Leader task", { ParentTaskID: 1 });
const CODER = makeTask(3, "Coder task", { ParentTaskID: 2 });
const LEADER_B = makeTask(4, "Leader B", { ParentTaskID: 1 });
const TREE = [ROOT, LEADER, CODER, LEADER_B];

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

async function mountSidebar(
  tasks: Task[],
  props: Partial<Parameters<typeof AppSidebar>[0]> = {},
  spaces: Space[] = [],
) {
  const client = new FakeWsClient();
  const utils = render(
    <SidebarProvider>
      <AppSidebar client={client as never} {...props} />
    </SidebarProvider>,
  );
  client.nth("workspace.list", 0).resolve([WORKSPACE]);
  await flush();
  client.nth("space.list", 0).resolve(spaces);
  client.nth("task.list", 0).resolve(tasks);
  await flush();
  await screen.findAllByTestId("sidebar-task-row");
  return { client, ...utils };
}

function triggerCount(): number {
  return screen.queryAllByTestId("sidebar-task-actions-trigger").length;
}

afterEach(() => {
  window.localStorage.clear();
});

describe("task row actions (S1)", () => {
  it("hovering a task row mounts only that row's actions", async () => {
    await mountSidebar(TREE);
    expect(triggerCount()).toBe(0);

    hoverTask(LEADER.ID);

    const triggers = screen.getAllByTestId("sidebar-task-actions-trigger");
    expect(triggers).toHaveLength(1);
    expect(triggers[0]).toHaveAttribute("data-task-id", String(LEADER.ID));
  });

  it("hovering a child row does not mount its parent's or siblings' actions", async () => {
    await mountSidebar(TREE);

    hoverTask(CODER.ID);

    const owners = screen.getAllByTestId("sidebar-task-actions-trigger").map((t) => t.getAttribute("data-task-id"));
    // The coder's ancestors (leader, root) and its parent's sibling stay quiet.
    expect(owners).toEqual([String(CODER.ID)]);
  });

  it("hovering a parent row does not mount its descendants' actions", async () => {
    await mountSidebar(TREE);

    hoverTask(ROOT.ID);

    const owners = screen.getAllByTestId("sidebar-task-actions-trigger").map((t) => t.getAttribute("data-task-id"));
    expect(owners).toEqual([String(ROOT.ID)]);
  });

  it("leaving a row unmounts its actions", async () => {
    await mountSidebar(TREE);
    hoverTask(LEADER.ID);
    expect(triggerCount()).toBe(1);

    unhoverTask(LEADER.ID);

    expect(triggerCount()).toBe(0);
  });

  it("an open menu keeps its row's actions mounted after the pointer leaves", async () => {
    await mountSidebar(TREE);
    const trigger = taskTrigger(LEADER.ID);

    fireEvent.pointerDown(trigger, { button: 0, ctrlKey: false });
    await screen.findByTestId("sidebar-task-pin-action");
    unhoverTask(LEADER.ID);

    expect(screen.getByTestId("sidebar-task-actions-trigger")).toBeInTheDocument();
  });
});
