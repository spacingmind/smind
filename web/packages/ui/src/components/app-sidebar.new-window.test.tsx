// Desktop build: the task row's menu offers "Open in new window", which asks the shell for the task's window through
// desktop.windowOpenTask with the task id only; a refusal (the window cap) is shown to the user.
import { act } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { AppSidebar } from "@/components/app-sidebar";
import { SidebarProvider } from "@/components/ui/sidebar";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp as render } from "@/test/render-with-daemon";
import { taskTrigger } from "@/test/sidebar-helpers";
import type { Task, Workspace } from "@/lib/types";

const platform = vi.hoisted(() => ({ windowOpenTask: vi.fn() }));
const toasts = vi.hoisted(() => ({ toast: vi.fn() }));
vi.mock("@/lib/platform", async (orig) => {
  const real = await orig<typeof import("@/lib/platform")>();
  return { ...real, isDesktop: true, desktop: { ...real.desktop, windowOpenTask: platform.windowOpenTask } };
});
vi.mock("@/components/ui/toast", () => ({ toast: toasts.toast }));
vi.mock("@/hooks/use-local-path-actions", () => ({ useLocalPathActions: () => null }));

const WORKSPACE: Workspace = { ID: 1, Path: "/tmp/ws", Title: "My Workspace", RoutingPolicy: "", CreatedAt: "2024-01-01T00:00:00Z", UpdatedAt: "2024-01-01T00:00:00Z" };
const TASK: Task = { ID: 42, WorkspaceID: 1, SpaceID: null, Title: "Fix the bug", Status: "active", WorktreePath: null, Branch: "fix-bug", CreatedAt: "2024-01-01T00:00:00Z", UpdatedAt: "2024-01-01T00:00:00Z", ArchivedAt: null };

const flush = () => act(async () => { for (let i = 0; i < 6; i++) await Promise.resolve(); });

async function mount() {
  const client = new FakeWsClient();
  render(
    <SidebarProvider>
      <AppSidebar client={client as never} selectedTaskId={null} />
    </SidebarProvider>,
  );
  client.nth("workspace.list", 0).resolve([WORKSPACE]);
  await flush();
  client.nth("space.list", 0).resolve([]);
  client.nth("task.list", 0).resolve([TASK]);
  await flush();
}

beforeEach(() => {
  platform.windowOpenTask.mockReset();
  toasts.toast.mockReset();
});

describe("Open in new window", () => {
  it("asks the shell for that task's window with the task id", async () => {
    platform.windowOpenTask.mockResolvedValue("task-1");
    await mount();
    fireEvent.pointerDown(taskTrigger(TASK.ID), { button: 0, ctrlKey: false });
    fireEvent.click(await screen.findByTestId("sidebar-task-open-in-new-window-action"));
    await waitFor(() => expect(platform.windowOpenTask).toHaveBeenCalledWith(TASK.ID));
    expect(platform.windowOpenTask).toHaveBeenCalledTimes(1);
    expect(toasts.toast).not.toHaveBeenCalled();
  });

  it("shows the shell's refusal (the window cap) as an error toast", async () => {
    platform.windowOpenTask.mockRejectedValue(new Error("too many task windows open (8); close one first"));
    await mount();
    fireEvent.pointerDown(taskTrigger(TASK.ID), { button: 0, ctrlKey: false });
    fireEvent.click(await screen.findByTestId("sidebar-task-open-in-new-window-action"));
    await waitFor(() => expect(toasts.toast).toHaveBeenCalled());
    expect(toasts.toast.mock.calls[0]![0]).toMatchObject({ variant: "error", description: "too many task windows open (8); close one first" });
  });
});
