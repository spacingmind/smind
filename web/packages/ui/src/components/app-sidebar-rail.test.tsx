// Collapsed 48px sidebar rail: each workspace row is a monogram badge with a
// status corner dot, a selected state, a name tooltip, and a click that opens
// the sidebar and reveals that workspace.
import { act } from "react";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import { renderWithClientProp as render } from "@/test/render-with-daemon";
import { afterEach, describe, expect, it, vi } from "vitest";

import { AppSidebar } from "@/components/app-sidebar";
import { SidebarProvider } from "@/components/ui/sidebar";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { TaskAttention, TaskRunStatus } from "@/hooks/use-task-attention";
import type { Task, Workspace } from "@/lib/types";

function makeWorkspace(id: number, title: string): Workspace {
  return {
    ID: id,
    Path: `/tmp/ws${id}`,
    Title: title,
    RoutingPolicy: "",
    CreatedAt: "2024-01-01T00:00:00Z",
    UpdatedAt: "2024-01-01T00:00:00Z",
  };
}

function makeTask(id: number, workspaceId: number): Task {
  return {
    ID: id,
    WorkspaceID: workspaceId,
    SpaceID: null,
    Title: `Task ${id}`,
    Status: "active",
    WorktreePath: null,
    Branch: null,
    CreatedAt: "2024-01-01T00:00:00Z",
    UpdatedAt: "2024-01-01T00:00:00Z",
    ArchivedAt: null,
  };
}

const WORKSPACES = [makeWorkspace(1, "alpha"), makeWorkspace(2, "my-app")];
const TASKS = [makeTask(11, 1), makeTask(21, 2)];

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

function tree(open: boolean, onOpenChange: (open: boolean) => void, props: Partial<Parameters<typeof AppSidebar>[0]>, client: FakeWsClient) {
  return (
    <SidebarProvider open={open} onOpenChange={onOpenChange}>
      <AppSidebar client={client as never} {...props} />
    </SidebarProvider>
  );
}

async function mount(open: boolean, props: Partial<Parameters<typeof AppSidebar>[0]> = {}) {
  const client = new FakeWsClient();
  const onOpenChange = vi.fn();
  const utils = render(tree(open, onOpenChange, props, client));
  client.nth("workspace.list", 0).resolve(WORKSPACES);
  await flush();
  for (const [i, ws] of WORKSPACES.entries()) {
    client.nth("space.list", i).resolve([]);
    client.nth("task.list", i).resolve(TASKS.filter((t) => t.WorkspaceID === ws.ID));
  }
  await flush();
  await screen.findAllByTestId("sidebar-workspace-row");
  return {
    onOpenChange,
    rerenderOpen: (next: boolean) => utils.rerender(tree(next, onOpenChange, props, client)),
  };
}

function row(id: number): HTMLElement {
  return document.querySelector(`[data-testid="sidebar-workspace-row"][data-workspace-id="${id}"]`) as HTMLElement;
}

afterEach(() => {
  cleanup();
  window.localStorage.clear();
});

describe("collapsed rail workspace rows", () => {
  it("renders a distinct monogram per workspace, hidden from assistive tech", async () => {
    await mount(false);

    const monograms = screen.getAllByTestId("rail-monogram");
    expect(monograms.map((m) => m.textContent)).toEqual(["Al", "MA"]);
    expect(monograms[0].firstElementChild).toHaveAttribute("aria-hidden", "true");
    expect(row(1)).toHaveAttribute("aria-label", "alpha");
    expect(row(2)).toHaveAttribute("aria-label", "my-app");
    expect(row(1)).toHaveAttribute("aria-expanded", "true");
  });

  it("shows the aggregate status as a corner dot only for workspaces that have one", async () => {
    const attention: TaskAttention = new Map([[21, new Set(["permission" as const])]]);
    const runStatus: TaskRunStatus = new Map([[11, "running" as never]]);
    await mount(false, { attention, runStatus });

    const dots = screen.getAllByTestId("rail-aggregate");
    expect(dots.map((d) => d.getAttribute("data-aggregate"))).toEqual(["running", "warning"]);
    expect(row(1).querySelector('[data-testid="rail-aggregate"]')).toHaveAttribute("data-aggregate", "running");
  });

  it("renders no dot for an idle workspace", async () => {
    await mount(false);
    expect(screen.queryAllByTestId("rail-aggregate")).toHaveLength(0);
  });

  it("marks the workspace containing the selected task", async () => {
    await mount(false, { selectedTaskId: 21 });

    const [first, second] = screen.getAllByTestId("rail-monogram");
    expect(first).not.toHaveAttribute("data-selected");
    expect(second).toHaveAttribute("data-selected", "true");
  });

  it("clicking a row in the rail opens the sidebar and keeps that workspace expanded", async () => {
    const { onOpenChange } = await mount(false);

    fireEvent.click(row(1));

    expect(onOpenChange).toHaveBeenCalledWith(true);
    expect(row(1)).toHaveAttribute("aria-expanded", "true");
  });

  it("clicking a collapsed workspace in the rail also expands it", async () => {
    const { onOpenChange, rerenderOpen } = await mount(true);
    fireEvent.click(row(1)); // expanded sidebar: toggles closed
    expect(row(1)).toHaveAttribute("aria-expanded", "false");
    expect(onOpenChange).not.toHaveBeenCalled();

    rerenderOpen(false);
    fireEvent.click(row(1));

    expect(onOpenChange).toHaveBeenCalledWith(true);
    expect(row(1)).toHaveAttribute("aria-expanded", "true");
  });

  it("in the expanded sidebar a click toggles and never touches the sidebar open state", async () => {
    const { onOpenChange } = await mount(true);

    fireEvent.click(row(2));
    expect(row(2)).toHaveAttribute("aria-expanded", "false");
    fireEvent.click(row(2));
    expect(row(2)).toHaveAttribute("aria-expanded", "true");
    expect(onOpenChange).not.toHaveBeenCalled();
  });

  it("does not render the name tooltip in the expanded sidebar", async () => {
    await mount(true);
    fireEvent.focus(row(1));
    expect(screen.queryByRole("tooltip")).toBeNull();
  });

  it("hides the list scrollbar in the rail (no width reserved or flashed)", () => {
    // jsdom has no layout, so guard the class contract instead.
    // eslint-disable-next-line @typescript-eslint/no-require-imports
    const src = require("node:fs").readFileSync(require("node:path").join(__dirname, "app-sidebar.tsx"), "utf8") as string;
    expect(src).toContain("group-data-[collapsible=icon]:[&_[data-slot=scroll-area-scrollbar]]:hidden");
  });
});
