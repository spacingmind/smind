import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";

import { AppSidebar } from "@/components/app-sidebar";
import { SidebarProvider } from "@/components/ui/sidebar";
import { DaemonQueryProvider } from "@/lib/query/daemon-query-provider";
import { createQueryClient } from "@/lib/query/query-client";
import { queryKeys } from "@/lib/query-keys";
import type { PluginPanelsResult } from "@/lib/plugin-panel-types";
import { FakeWsClient } from "@/test/fake-ws-client";
import "@/test/query-scheduler";

/** Seeds the panels cache the way the event bridge's `plugin.updated` refresh does. */
function mountPanels(result: PluginPanelsResult, onOpenPluginPanel = vi.fn()) {
  const queryClient = createQueryClient();
  queryClient.setQueryData(queryKeys.pluginPanels(), result);
  const client = new FakeWsClient();
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <DaemonQueryProvider queryClient={queryClient} client={client as never} events={null}>
        <SidebarProvider>{children}</SidebarProvider>
      </DaemonQueryProvider>
    );
  }
  return {
    onOpenPluginPanel,
    view: render(
      <AppSidebar
        client={client as never}
        selectedTaskId={null}
        onSelectTask={vi.fn()}
        onOpenSettings={vi.fn()}
        onOpenPluginPanel={onOpenPluginPanel}
      />,
      { wrapper: Wrapper },
    ),
  };
}

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 4; i++) await Promise.resolve();
  });
}

describe("AppSidebar plugins group", () => {
  it("TestSidebarPlugins_HiddenWithoutItems: no group at all when the daemon offers no sidebar entries", async () => {
    mountPanels({
      panels: [{ plugin: "acme", id: "usage", title: "Acme usage", where: "task", ui: 1 }],
      sidebar: [],
    });
    await flush();

    expect(screen.queryByTestId("sidebar-plugin-item")).toBeNull();
    expect(screen.queryByText("Plugins")).toBeNull();
  });

  it("TestSidebarPlugins_RowOpensPage: a sidebar entry renders its title and opens its panel's page", async () => {
    const { onOpenPluginPanel } = mountPanels({
      panels: [{ plugin: "acme", id: "about", title: "About", where: "sidebar", ui: 1 }],
      sidebar: [{ plugin: "acme", id: "about", title: "About", opens: "about" }],
    });
    await flush();

    const row = screen.getByTestId("sidebar-plugin-item");
    expect(row).toHaveTextContent("About");
    expect(row).toHaveAttribute("data-plugin", "acme");
    expect(row).toHaveAttribute("data-panel", "about");

    fireEvent.click(row);
    expect(onOpenPluginPanel).toHaveBeenCalledWith("acme", "about");
  });

  it("sidebar row uses the item's icon", async () => {
    const withIcon = mountPanels({
      panels: [{ plugin: "acme", id: "about", title: "About", where: "sidebar", ui: 1 }],
      sidebar: [{ plugin: "acme", id: "about", title: "About", opens: "about", icon: "Hand" }],
    });
    await flush();

    const svg = withIcon.view.container.querySelector('[data-testid="sidebar-plugin-item"] svg');
    expect(svg?.getAttribute("class")).toContain("lucide-hand");
    withIcon.view.unmount();

    mountPanels({
      panels: [{ plugin: "acme", id: "about", title: "About", where: "sidebar", ui: 1 }],
      sidebar: [{ plugin: "acme", id: "about", title: "About", opens: "about" }],
    });
    await flush();

    const fallback = screen.getByTestId("sidebar-plugin-item").querySelector("svg");
    expect(fallback?.getAttribute("class")).toContain("lucide-puzzle");
  });
});
