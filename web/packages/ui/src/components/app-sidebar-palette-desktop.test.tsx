import { act } from "react";
import { renderWithClientProp as render } from "@/test/render-with-daemon";
import { afterEach, describe, expect, it, vi } from "vitest";

import { FakeWsClient } from "@/test/fake-ws-client";

// isDesktop is a module-load constant, so each test mocks @/lib/platform
// BEFORE dynamically importing AppSidebar (and everything that shares its
// module graph), the same way app-sidebar-footer.test.tsx's desktop test does.
async function mountDesktopSidebar() {
  vi.resetModules();
  vi.doMock("@/lib/platform", async (importOriginal) => ({
    ...(await importOriginal<typeof import("@/lib/platform")>()),
    isDesktop: true,
    desktopOS: "macos",
    desktop: { openExternal: vi.fn(() => Promise.resolve()) },
  }));
  vi.doMock("@/hooks/use-local-path-actions", () => ({
    useLocalPathActions: () => null,
  }));
  const { AppSidebar } = await import("@/components/app-sidebar");
  const { PaletteProvider, useRegisteredCommands } = await import("@/palette/palette-provider");
  const { SidebarProvider } = await import("@/components/ui/sidebar");

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
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
  return commands;
}

describe("AppSidebar palette entries in a desktop build", () => {
  afterEach(() => {
    vi.resetModules();
    vi.restoreAllMocks();
    vi.doUnmock("@/lib/platform");
    vi.doUnmock("@/hooks/use-local-path-actions");
  });

  it("registers Settings: Daemon server and Settings: Daemon in a desktop build", async () => {
    const commands = await mountDesktopSidebar();
    const byTitle = new Map(commands.map((c) => [c.title, c]));

    expect(byTitle.get("Settings: Daemon server")?.group).toBe("Settings");
    expect(byTitle.get("Settings: Daemon")?.group).toBe("Settings");
    expect(byTitle.has("Settings: Agents")).toBe(true);
    expect(byTitle.has("Settings: Providers")).toBe(true);
  });

  it("Settings: Daemon server opens the connections section and Settings: Daemon opens the daemon section", async () => {
    const commands = await mountDesktopSidebar();
    const byTitle = new Map(commands.map((c) => [c.title, c]));
    const listener = vi.fn();
    window.addEventListener("smind:open-settings", listener);
    try {
      byTitle.get("Settings: Daemon server")!.run();
      byTitle.get("Settings: Daemon")!.run();
      expect(listener).toHaveBeenCalledTimes(2);
      expect((listener.mock.calls[0]![0] as CustomEvent).detail).toEqual({ sectionId: "connections" });
      expect((listener.mock.calls[1]![0] as CustomEvent).detail).toEqual({ sectionId: "daemon" });
    } finally {
      window.removeEventListener("smind:open-settings", listener);
    }
  });
});
