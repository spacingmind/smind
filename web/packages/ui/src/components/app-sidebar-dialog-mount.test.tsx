import { act } from "react";
import { screen } from "@testing-library/react";
import { renderWithClientProp as render } from "@/test/render-with-daemon";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { AppSidebar } from "@/components/app-sidebar";
import { SidebarProvider } from "@/components/ui/sidebar";
import { PaletteProvider, useRegisteredCommands } from "@/palette/palette-provider";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { Workspace } from "@/lib/types";

const WORKSPACE: Workspace = {
  ID: 1,
  Path: "/tmp/ws",
  Title: "My Workspace",
  RoutingPolicy: "",
  CreatedAt: "2024-01-01T00:00:00Z",
  UpdatedAt: "2024-01-01T00:00:00Z",
};

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

/** Forces useIsMobile() true: below 768px the sidebar is a closed Sheet. */
function stubCompactViewport() {
  Object.defineProperty(window, "innerWidth", { writable: true, configurable: true, value: 390 });
  window.matchMedia = vi.fn().mockImplementation(
    (query: string) =>
      ({
        matches: true,
        media: query,
        onchange: null,
        addEventListener: () => {},
        removeEventListener: () => {},
        addListener: () => {},
        removeListener: () => {},
        dispatchEvent: () => false,
      }) as unknown as MediaQueryList,
  );
}

async function mountCompact() {
  const client = new FakeWsClient();
  let commands: ReturnType<typeof useRegisteredCommands> = [];
  function Probe() {
    commands = useRegisteredCommands();
    return null;
  }
  render(
    <PaletteProvider>
      <Probe />
      <SidebarProvider>
        <AppSidebar client={client as never} selectedTaskId={null} />
      </SidebarProvider>
    </PaletteProvider>,
  );
  client.nth("workspace.list", 0).resolve([WORKSPACE]);
  await flush();
  client.nth("space.list", 0).resolve([]);
  client.nth("task.list", 0).resolve([]);
  await flush();
  return () => commands;
}

describe("AppSidebar CRUD dialogs in the compact layout (UAT A-01)", () => {
  beforeEach(stubCompactViewport);
  afterEach(() => {
    Object.defineProperty(window, "innerWidth", { writable: true, configurable: true, value: 1024 });
  });

  it("palette 'New task' opens the dialog while the drawer is closed", async () => {
    const commands = await mountCompact();
    const cmd = commands().find((c) => c.title === "New task");
    expect(cmd).toBeDefined();
    act(() => cmd!.run());
    expect(await screen.findByRole("dialog", { name: "New task" })).toBeInTheDocument();
  });

  it("palette 'New workspace' opens the dialog while the drawer is closed", async () => {
    const commands = await mountCompact();
    const cmd = commands().find((c) => c.title === "New workspace");
    expect(cmd).toBeDefined();
    act(() => cmd!.run());
    expect(await screen.findByRole("dialog", { name: "New workspace" })).toBeInTheDocument();
  });
});
