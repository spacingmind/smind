import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";

import { PluginPage } from "@/components/plugin-panel/plugin-page";
import { DaemonQueryProvider } from "@/lib/query/daemon-query-provider";
import { createQueryClient } from "@/lib/query/query-client";
import { queryKeys } from "@/lib/query-keys";
import type { PluginPanel, PluginPanelsResult } from "@/lib/plugin-panel-types";
import { FakeWsClient } from "@/test/fake-ws-client";
import "@/test/query-scheduler";

const ABOUT: PluginPanel = { plugin: "acme", id: "about", title: "About Acme", where: "sidebar", ui: 1 };

function seedPanels(result: PluginPanelsResult) {
  const queryClient = createQueryClient();
  queryClient.setQueryData(queryKeys.pluginPanels(), result);
  return queryClient;
}

function mount(queryClient: ReturnType<typeof createQueryClient>, onNavigateBack = vi.fn()) {
  const client = new FakeWsClient();
  function Wrapper({ children }: { children: ReactNode }) {
    return (
      <DaemonQueryProvider queryClient={queryClient} client={client as never} events={null}>
        {children}
      </DaemonQueryProvider>
    );
  }
  return render(<PluginPage pluginId="acme" panelId="about" onNavigateBack={onNavigateBack} />, { wrapper: Wrapper });
}

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 4; i++) await Promise.resolve();
  });
}

describe("PluginPage", () => {
  it("renders the panel's title and the pane stub for a known panel", async () => {
    mount(seedPanels({ panels: [ABOUT], sidebar: [] }));
    await flush();

    expect(screen.getByTestId("plugin-page")).toBeInTheDocument();
    expect(screen.getByTestId("plugin-page-title")).toHaveTextContent("About Acme");
    const pane = screen.getByTestId("plugin-panel-pane");
    expect(pane).toHaveAttribute("data-plugin", "acme");
    expect(pane).toHaveAttribute("data-panel", "about");
  });

  it("falls back to the panel id while the list has not loaded, and the back button navigates back", async () => {
    const onNavigateBack = vi.fn();
    // No seeded cache entry: the query is pending, so the title is the panel id.
    mount(createQueryClient(), onNavigateBack);
    await flush();

    expect(screen.getByTestId("plugin-page-title")).toHaveTextContent("about");
    expect(onNavigateBack).not.toHaveBeenCalled();

    fireEvent.click(screen.getByTestId("plugin-page-back"));
    expect(onNavigateBack).toHaveBeenCalledTimes(1);
  });

  it("TestPanelDisabledWhileOpen_NavigatesBackOnce: a loaded list without the panel walks the page back, once", async () => {
    const onNavigateBack = vi.fn();
    const { rerender } = mount(seedPanels({ panels: [], sidebar: [] }), onNavigateBack);
    await flush();

    expect(onNavigateBack).toHaveBeenCalledTimes(1);
    // A re-render while still gone does not navigate again.
    rerender(<PluginPage pluginId="acme" panelId="about" onNavigateBack={onNavigateBack} />);
    await flush();
    expect(onNavigateBack).toHaveBeenCalledTimes(1);
  });
});
