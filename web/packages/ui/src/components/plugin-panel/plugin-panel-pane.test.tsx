import { act, fireEvent, render, screen } from "@testing-library/react";
import type { QueryClient } from "@tanstack/react-query";
import { describe, expect, it } from "vitest";

import { PluginPanelPane } from "@/components/plugin-panel/plugin-panel-pane";
import { DaemonQueryProvider } from "@/lib/query/daemon-query-provider";
import { createQueryClient } from "@/lib/query/query-client";
import type { PluginPanel, PluginPanelsResult } from "@/lib/plugin-panel-types";
import { RpcError } from "@/lib/ws-client";
import { FakeWsClient } from "@/test/fake-ws-client";
import "@/test/query-scheduler";

const HELLO = { ui: 1, root: { type: "heading", text: "Hi" } };
const PANELS: PluginPanelsResult = {
  panels: [{ plugin: "acme", id: "overview", title: "Acme overview", where: "task", ui: 1 } satisfies PluginPanel],
  sidebar: [],
};

/** The daemon-provider wrapper the pane needs (same harness as use-plugin-panels.test.tsx). */
function renderPane(props: { taskId?: number; bare?: boolean; queryClient?: QueryClient; client?: FakeWsClient } = {}) {
  const client = props.client ?? new FakeWsClient();
  const queryClient = props.queryClient ?? createQueryClient();
  render(
    <DaemonQueryProvider queryClient={queryClient} client={client as never} events={null}>
      <PluginPanelPane pluginId="acme" panelId="overview" taskId={props.taskId} bare={props.bare} />
    </DaemonQueryProvider>,
  );
  return { client, queryClient };
}

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 8; i++) await Promise.resolve();
  });
}

describe("PluginPanelPane", () => {
  it("shows loading, then the panel title and the rendered view", async () => {
    const { client } = renderPane();
    expect(screen.getByTestId("plugin-panel-loading")).toBeTruthy();
    expect(screen.getByTestId("plugin-panel-pane").getAttribute("data-plugin")).toBe("acme");
    expect(screen.getByTestId("plugin-panel-pane").getAttribute("data-panel")).toBe("overview");

    client.nth("plugin.panels", 0).resolve(PANELS);
    client.nth("plugin.view", 0).resolve({ view: HELLO });
    await flush();

    // The header names the panel (from plugin.panels) with the plugin id, not the raw ids.
    expect(screen.getByTestId("plugin-panel-header")).toHaveTextContent("Acme overview");
    expect(screen.getByTestId("plugin-panel-header")).toHaveTextContent("acme");
    expect(screen.queryByTestId("plugin-panel-loading")).toBeNull();
    expect(screen.getByTestId("plugin-view")).toBeTruthy();
    expect(screen.getByRole("heading", { name: "Hi" })).toBeTruthy();
  });

  it("header separates the title from the plugin id", async () => {
    const { client } = renderPane();
    client.nth("plugin.panels", 0).resolve(PANELS);
    client.nth("plugin.view", 0).resolve({ view: HELLO });
    await flush();

    const title = screen.getByText("Acme overview");
    const id = screen.getByText("acme");
    expect(title).not.toBe(id);
    // Both sit inside the same flex wrapper (the gap between them), inside the header.
    expect(title.parentElement).toBe(id.parentElement);
    expect(screen.getByTestId("plugin-panel-header")).toContainElement(title);
    expect(screen.getByTestId("plugin-panel-header")).toContainElement(id);
  });

  it("bare pane shows only the plugin id in its header", async () => {
    const { client } = renderPane({ bare: true });
    client.nth("plugin.panels", 0).resolve(PANELS);
    client.nth("plugin.view", 0).resolve({ view: HELLO });
    await flush();

    const header = screen.getByTestId("plugin-panel-header");
    expect(header).toHaveTextContent("acme");
    expect(header).not.toHaveTextContent("Acme overview");
    expect(screen.getByTestId("plugin-panel-refresh")).toBeInTheDocument();
  });

  it("asks plugin.view with the task context only when there is one", async () => {
    const withTask = renderPane({ taskId: 7 });
    expect(withTask.client.nth("plugin.view", 0).params).toEqual({
      pluginId: "acme",
      panelId: "overview",
      ui: 1,
      context: { taskId: 7 },
    });

    const withoutTask = renderPane();
    const params = withoutTask.client.nth("plugin.view", 0).params as { context?: unknown };
    expect(params).toEqual({ pluginId: "acme", panelId: "overview", ui: 1 });
    expect(params.context).toBeUndefined();
  });

  it("a view_invalid failure shows the daemon's reason and Retry refetches", async () => {
    const { client } = renderPane();
    client.nth("plugin.view", 0).reject(new RpcError("root.children[1].text: want a string", "view_invalid"));
    await flush();

    const error = screen.getByTestId("plugin-panel-error");
    expect(error).toHaveTextContent("The plugin sent an invalid panel.");
    expect(error).toHaveTextContent("root.children[1].text: want a string");

    fireEvent.click(screen.getByTestId("plugin-panel-error-retry"));
    client.nth("plugin.view", 1).resolve({ view: HELLO });
    await flush();
    expect(screen.getByTestId("plugin-view")).toBeTruthy();
    expect(screen.queryByTestId("plugin-panel-error")).toBeNull();
  });

  it.each([
    ["ui_level_too_low", "This panel needs a newer app version."],
    ["plugin_unavailable", "The plugin is not running."],
    ["unknown_panel", "This panel is no longer available."],
  ])("a %s failure reads as its human sentence", async (code, sentence) => {
    const { client } = renderPane();
    client.nth("plugin.view", 0).reject(new RpcError(code, code));
    await flush();
    expect(screen.getByTestId("plugin-panel-error")).toHaveTextContent(sentence);
  });

  it("the refresh button re-calls plugin.view, also for a task-scoped view", async () => {
    const { client } = renderPane({ taskId: 7 });
    client.nth("plugin.view", 0).resolve({ view: HELLO });
    await flush();
    expect(screen.getByRole("heading", { name: "Hi" })).toBeTruthy();

    fireEvent.click(screen.getByTestId("plugin-panel-refresh"));
    await flush();
    expect(client.calls.filter((c) => c.method === "plugin.view")).toHaveLength(2);
    expect(client.nth("plugin.view", 1).params).toEqual({
      pluginId: "acme",
      panelId: "overview",
      ui: 1,
      context: { taskId: 7 },
    });

    client.nth("plugin.view", 1).resolve({ view: { ui: 1, root: { type: "heading", text: "Second" } } });
    await flush();
    expect(screen.getByRole("heading", { name: "Second" })).toBeTruthy();
  });
});
