import { act } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { AppSidebar } from "@/components/app-sidebar";
import { FirstRunCards } from "@/components/home/first-run-cards";
import { GetStartedCard } from "@/components/home/get-started-card";
import { HomeLanding } from "@/components/home/home-landing";
import { SidebarProvider } from "@/components/ui/sidebar";
import { NEW_WORKSPACE_EVENT, requestNewWorkspace } from "@/lib/app-events";
import { PaletteProvider, useRegisteredCommands } from "@/palette/palette-provider";
import { FakeWsClient } from "@/test/fake-ws-client";
import { renderWithClientProp } from "@/test/render-with-daemon";
import type { ReactElement } from "react";
import type { RenderResult } from "@testing-library/react";
import type { Workspace } from "@/lib/types";

const WORKSPACE: Workspace = {
  ID: 1,
  Path: "/tmp/repo",
  Title: "My Workspace",
  RoutingPolicy: "",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
};

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 5; i++) await Promise.resolve();
  });
}

/**
 * Mounts the sidebar, the one surface that owns the Add project dialog, with
 * its workspace list answered. `extra` renders beside it (a home component),
 * given the same fake client the sidebar got.
 */
async function mountSidebar(workspaces: Workspace[], extra?: (client: FakeWsClient) => ReactElement): Promise<RenderResult> {
  const client = new FakeWsClient();
  const view = renderWithClientProp(
    <>
      <SidebarProvider>
        <AppSidebar client={client as never} selectedTaskId={null} />
      </SidebarProvider>
      {extra?.(client)}
    </>,
  );
  client.nth("workspace.list", 0).resolve(workspaces);
  await flush();
  return view;
}

/** What every entry point must do: the new modal is up, the old "New workspace" form is not. */
async function expectAddProjectModal(): Promise<void> {
  expect(await screen.findByTestId("add-project-dialog")).toBeInTheDocument();
  expect(screen.getByRole("dialog", { name: "Add project" })).toBeInTheDocument();
  expect(screen.queryByRole("dialog", { name: "New workspace" })).not.toBeInTheDocument();
}

/** Escape closes the modal, so the next entry point starts from a clean tree. */
async function closeWithEscape(): Promise<void> {
  fireEvent.keyDown(screen.getByTestId("add-project-dialog"), { key: "Escape" });
  await waitFor(() => expect(screen.queryByTestId("add-project-dialog")).not.toBeInTheDocument());
}

/** One sub-step per entry point, so a failure names the entry point that broke. */
async function entryPoint(name: string, run: () => Promise<void> | void): Promise<void> {
  try {
    await run();
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    throw new Error(`entry point ${name}: ${message}`, { cause: err });
  }
}

describe("Add project entry points", () => {
  it("add_project_all_six_entry_points_open_the_new_modal", async () => {
    // 1. The sidebar's "+" while projects exist: the footer's Add workspace button
    //    (the header "+" that testid sidebar-new-workspace-button named was removed in B1).
    await entryPoint("the sidebar '+' with projects present (sidebar-footer-add-workspace)", async () => {
      const view = await mountSidebar([WORKSPACE]);
      fireEvent.click(screen.getByTestId("sidebar-footer-add-workspace"));
      await expectAddProjectModal();
      await closeWithEscape();
      view.unmount();
    });

    // 2. The sidebar's empty state: its "+" and its "Add project" button.
    await entryPoint("the sidebar empty state (sidebar-empty-add-workspace, sidebar-empty-new-workspace-button)", async () => {
      const view = await mountSidebar([]);
      fireEvent.click(screen.getByTestId("sidebar-empty-add-workspace"));
      await expectAddProjectModal();
      await closeWithEscape();
      fireEvent.click(screen.getByTestId("sidebar-empty-new-workspace-button"));
      await expectAddProjectModal();
      await closeWithEscape();
      view.unmount();
    });

    // 3. The home landing's project menu "Add project": App.tsx wires onAddProject
    //    to requestNewWorkspace, which dispatches the event the sidebar listens for.
    await entryPoint("the home landing 'Add project' (home-project-add)", async () => {
      const onAddProject = vi.fn(requestNewWorkspace);
      const view = await mountSidebar([WORKSPACE], (client) => (
        <HomeLanding client={client} connected workspaces={[WORKSPACE]} onCreated={vi.fn()} onAddProject={onAddProject} />
      ));
      fireEvent.click(screen.getByTestId("home-project-chip"));
      fireEvent.click(screen.getByTestId("home-project-add"));
      expect(onAddProject).toHaveBeenCalledTimes(1);
      await expectAddProjectModal();
      await closeWithEscape();
      view.unmount();
    });

    // 4. The two home cards' "Add a project" steps, both wired to the same event.
    await entryPoint("the first-run cards and Get started card 'Add a project'", async () => {
      const firstRun = vi.fn(requestNewWorkspace);
      const cards = await mountSidebar([], () => (
        <FirstRunCards providers={[]} health={{}} rpcFailed={new Set()} ensure={vi.fn()} onAddProject={firstRun} />
      ));
      fireEvent.click(screen.getByTestId("first-run-add-project"));
      expect(firstRun).toHaveBeenCalledTimes(1);
      await expectAddProjectModal();
      await closeWithEscape();
      cards.unmount();

      const getStarted = vi.fn(requestNewWorkspace);
      const view = await mountSidebar([], () => (
        <GetStartedCard workspaces={[]} providers={[]} health={{}} rpcFailed={new Set()} ensure={vi.fn()} onAddProject={getStarted} />
      ));
      fireEvent.click(screen.getByTestId("get-started-add-project"));
      expect(getStarted).toHaveBeenCalledTimes(1);
      await expectAddProjectModal();
      await closeWithEscape();
      view.unmount();
    });

    // 5. The command palette's "New workspace" (id new-workspace), registered by the sidebar.
    await entryPoint("the command palette 'New workspace' (id new-workspace)", async () => {
      const client = new FakeWsClient();
      let commands: ReturnType<typeof useRegisteredCommands> = [];
      function Probe() {
        commands = useRegisteredCommands();
        return null;
      }
      const registeredCommands = () => commands;
      const view = renderWithClientProp(
        <PaletteProvider>
          <Probe />
          <SidebarProvider>
            <AppSidebar client={client as never} selectedTaskId={null} />
          </SidebarProvider>
        </PaletteProvider>,
      );
      client.nth("workspace.list", 0).resolve([]);
      await flush();
      const cmd = registeredCommands().find((c) => c.id === "new-workspace");
      expect(cmd).toBeDefined();
      act(() => cmd!.run());
      await expectAddProjectModal();
      await closeWithEscape();
      view.unmount();
    });

    // 6. The smind:new-workspace window event itself, dispatched raw.
    await entryPoint("the smind:new-workspace window event", async () => {
      const view = await mountSidebar([]);
      act(() => {
        window.dispatchEvent(new Event(NEW_WORKSPACE_EVENT));
      });
      await expectAddProjectModal();
      await closeWithEscape();
      view.unmount();
    });
  });
});
