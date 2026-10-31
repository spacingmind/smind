// The sidebar's "Started by automation" filter: every group mode lists only automation tasks while it is on.
import { act } from "react";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import { renderWithClientProp as render } from "@/test/render-with-daemon";
import { afterEach, describe, expect, it } from "vitest";

import { AppSidebar } from "@/components/app-sidebar";
import { SidebarProvider } from "@/components/ui/sidebar";
import { FakeWsClient } from "@/test/fake-ws-client";
import { reloadAllPreferenceStores } from "@/stores/create-preference-store";
import type { Task, Workspace } from "@/lib/types";

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

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

async function mountSidebar(tasks: Task[]) {
  const client = new FakeWsClient();
  render(
    <SidebarProvider>
      <AppSidebar client={client as never} />
    </SidebarProvider>,
  );
  client.nth("workspace.list", 0).resolve([WORKSPACE]);
  await flush();
  client.nth("space.list", 0).resolve([]);
  client.nth("task.list", 0).resolve(tasks);
  await flush();
  await screen.findAllByTestId("sidebar-task-row");
}

const titles = () => screen.queryAllByTestId("task-title").map((el) => el.textContent);

afterEach(() => {
  cleanup();
  window.localStorage.clear();
  reloadAllPreferenceStores();
});

const TASKS = [
  makeTask(1, "By hand"),
  makeTask(2, "By cron", { automationId: 9, automationName: "Nightly" }),
  makeTask(3, "Also by hand"),
];

describe("sidebar automation filter", () => {
  it("filter shows only automation tasks", async () => {
    await mountSidebar(TASKS);
    expect(titles()).toEqual(expect.arrayContaining(["By hand", "By cron", "Also by hand"]));

    const toggle = screen.getByTestId("sidebar-automation-filter");
    expect(toggle).toHaveAttribute("aria-pressed", "false");
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-pressed", "true");
    expect(titles()).toEqual(["By cron"]);

    fireEvent.click(toggle);
    expect(titles()).toHaveLength(3);
  });

  it("filter honours status grouping", async () => {
    await mountSidebar(TASKS);
    fireEvent.click(screen.getByTestId("sidebar-group-mode-status"));
    fireEvent.click(screen.getByTestId("sidebar-automation-filter"));
    expect(titles()).toEqual(["By cron"]);
  });

  it("an automation child of a filtered-out parent still shows as a root", async () => {
    await mountSidebar([makeTask(1, "Parent"), makeTask(2, "Child", { ParentTaskID: 1, automationId: 9 })]);
    fireEvent.click(screen.getByTestId("sidebar-automation-filter"));
    expect(titles()).toEqual(["Child"]);
  });

  it("says so when nothing matches", async () => {
    await mountSidebar([makeTask(1, "By hand")]);
    fireEvent.click(screen.getByTestId("sidebar-automation-filter"));
    expect(screen.getByText("No automation tasks")).toBeInTheDocument();
    expect(screen.queryByTestId("sidebar-workspace-row")).not.toBeInTheDocument();
    expect(screen.queryByTestId("task-title")).not.toBeInTheDocument();
  });
});
