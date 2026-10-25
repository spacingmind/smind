import { act } from "react";
import { fireEvent, screen, within } from "@testing-library/react";
import { renderWithClientProp as render } from "@/test/render-with-daemon";
import { describe, expect, it } from "vitest";

import { listSettingsSections } from "@/components/settings/settings-registry";
import "@/components/settings/usage-section";
import { useWorkspaceTree } from "@/hooks/use-workspace-tree";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { UsageSummaryRow, Workspace } from "@/lib/types";

const WORKSPACES: Workspace[] = [
  { ID: 1, Path: "/tmp/a", Title: "Alpha", RoutingPolicy: "", CreatedAt: "", UpdatedAt: "" },
];

const TASKS = [{ ID: 11, WorkspaceID: 1, SpaceID: null, Title: "Fix login", Status: "done", WorktreePath: null, Branch: null, CreatedAt: "", UpdatedAt: "", ArchivedAt: null }];

function runRow(overrides: Partial<UsageSummaryRow> = {}): UsageSummaryRow {
  return {
    scope: "runs",
    key: "11",
    count: 3,
    inputTokens: 12345,
    outputTokens: 6789,
    cacheReadTokens: 1000,
    cacheWriteTokens: 500,
    reasoningTokens: 0,
    costUsd: 0.0423,
    ...overrides,
  };
}

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

function renderSection(client: FakeWsClient) {
  const section = listSettingsSections().find((s) => s.id === "usage");
  if (!section) throw new Error("usage section did not register");
  render(<>{section.render({ client: client as never, events: null })}</>);
}

describe("usage-section", () => {
  it("registers as the usage section in Agents & providers", () => {
    const section = listSettingsSections().find((s) => s.id === "usage");
    expect(section).toBeDefined();
    expect(section?.label).toBe("Usage");
    expect(section?.groupLabel).toBe("Agents & providers");
  });

  it("defaults to scope runs, groupBy task and a 7 day since", async () => {
    const client = new FakeWsClient();
    renderSection(client);
    const params = client.nth("usage.summary").params as Record<string, unknown>;
    expect(params.scope).toBe("runs");
    expect(params.groupBy).toBe("task");
    const since = new Date(params.since as string);
    const expected = Date.now() - 7 * 24 * 60 * 60 * 1000;
    expect(Math.abs(since.getTime() - expected)).toBeLessThan(60 * 1000);
    expect(params).not.toHaveProperty("until");
  });

  it("scope, groupBy and range round-trip into usage.summary params", async () => {
    const client = new FakeWsClient();
    renderSection(client);
    client.nth("usage.summary").resolve([]);
    await flush();

    fireEvent.click(screen.getByTestId("settings-usage-group-by-model"));
    await flush();
    let params = client.nth("usage.summary", 1).params as Record<string, unknown>;
    expect(params.groupBy).toBe("model");

    fireEvent.click(screen.getByTestId("settings-usage-range-today"));
    await flush();
    params = client.nth("usage.summary", 2).params as Record<string, unknown>;
    expect(params.range ?? undefined).toBeUndefined();
    const now = new Date();
    const midnight = new Date(now.getFullYear(), now.getMonth(), now.getDate());
    expect(new Date(params.since as string).getTime()).toBe(midnight.getTime());

    fireEvent.click(screen.getByTestId("settings-usage-scope-all"));
    await flush();
    params = client.nth("usage.summary", 3).params as Record<string, unknown>;
    expect(params.scope).toBe("all");
  });

  it("choosing proxy switches groupBy to model and disables the runs-only groupBys", async () => {
    const client = new FakeWsClient();
    renderSection(client);
    client.nth("usage.summary").resolve([]);
    await flush();

    fireEvent.click(screen.getByTestId("settings-usage-scope-proxy"));
    await flush();

    expect(client.calls.filter((c) => c.method === "usage.summary")).toHaveLength(2);
    const params = client.nth("usage.summary", 1).params as Record<string, unknown>;
    expect(params.scope).toBe("proxy");
    expect(params.groupBy).toBe("model");
    expect((screen.getByTestId("settings-usage-group-by-task") as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByTestId("settings-usage-group-by-provider") as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByTestId("settings-usage-group-by-workspace") as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByTestId("settings-usage-group-by-model") as HTMLButtonElement).disabled).toBe(false);
  });

  it("renders rows with thousands separators and costs", async () => {
    const client = new FakeWsClient();
    renderSection(client);
    client.nth("usage.summary").resolve([runRow()]);
    await flush();

    const row = screen.getByTestId("usage-row-runs-11");
    expect(within(row).getByTestId("usage-cell-inputTokens")).toHaveTextContent("12,345");
    expect(within(row).getByTestId("usage-cell-outputTokens")).toHaveTextContent("6,789");
    expect(within(row).getByTestId("usage-cell-costUsd")).toHaveTextContent("$0.042");
    expect(within(row).getByTestId("usage-cell-count")).toHaveTextContent("3");
    expect(within(row).getByTestId("usage-cell-cacheReadTokens")).toHaveTextContent("1,000");
  });

  it("shows an em dash for unreported tokens and null cost, never 0", async () => {
    const client = new FakeWsClient();
    renderSection(client);
    client.nth("usage.summary").resolve([
      runRow({ key: "1", unreported: ["reasoningTokens"], reasoningTokens: 0, costUsd: null }),
      runRow({ key: "2" }),
    ]);
    await flush();

    const unreportedRow = screen.getByTestId("usage-row-runs-1");
    expect(within(unreportedRow).getByTestId("usage-cell-reasoningTokens")).toHaveTextContent("—");
    expect(within(unreportedRow).getByTestId("usage-cell-costUsd")).toHaveTextContent("—");
    expect(within(unreportedRow).getByTestId("usage-cell-reasoningTokens")).not.toHaveTextContent("0");
    expect(within(unreportedRow).getByTestId("usage-cell-costUsd")).not.toHaveTextContent("0");

    const reportedRow = screen.getByTestId("usage-row-runs-2");
    expect(within(reportedRow).getByTestId("usage-cell-reasoningTokens")).toHaveTextContent("0");
  });

  it("resolves task and workspace keys to titles with an id fallback", async () => {
    const client = new FakeWsClient();
    renderSection(client);
    client.nth("usage.summary").resolve([runRow({ key: "11" }), runRow({ key: "999" })]);
    // The names come from the shared workspace tree: workspace.list + that workspace's space.list/task.list.
    client.nth("workspace.list").resolve(WORKSPACES);
    await flush();
    client.nth("space.list").resolve([]);
    client.nth("task.list").resolve(TASKS);
    await flush();

    expect(screen.getByTestId("usage-row-runs-11")).toHaveTextContent("Fix login");
    expect(screen.getByTestId("usage-row-runs-999")).toHaveTextContent("Task #999");
  });

  it("usage-section reads the shared workspace tree: with the sidebar's tree mounted too, each workspace's tasks are fetched once, not once more per consumer", async () => {
    const client = new FakeWsClient();
    const section = listSettingsSections().find((s) => s.id === "usage")!;
    function SidebarTree() {
      useWorkspaceTree(); // what AppSidebar mounts
      return null;
    }
    render(
      <>
        <SidebarTree />
        {section.render({ client: client as never, events: null })}
      </>,
    );
    client.nth("usage.summary").resolve([runRow({ key: "11" })]);
    client.nth("workspace.list").resolve([...WORKSPACES, { ...WORKSPACES[0]!, ID: 2, Title: "Beta" }]);
    await flush();
    for (const call of client.calls.filter((c) => c.method === "space.list")) call.resolve([]);
    for (const call of client.calls.filter((c) => c.method === "task.list")) call.resolve(TASKS);
    await flush();

    expect(screen.getByTestId("usage-row-runs-11")).toHaveTextContent("Fix login");
    // One tree for both consumers: 1 workspace.list, and one task.list per workspace (2) -- not a second
    // workspace.list + per-workspace task.list from the usage section.
    expect(client.calls.filter((c) => c.method === "workspace.list")).toHaveLength(1);
    expect(client.calls.filter((c) => c.method === "task.list")).toHaveLength(2);
  });

  it("shows an empty state when there are no rows", async () => {
    const client = new FakeWsClient();
    renderSection(client);
    client.nth("usage.summary").resolve([]);
    await flush();

    expect(screen.getByTestId("usage-empty")).toBeInTheDocument();
    expect(screen.getByText("No usage recorded in this range")).toBeInTheDocument();
  });

  it("shows an error when usage.summary fails", async () => {
    const client = new FakeWsClient();
    renderSection(client);
    client.nth("usage.summary").reject(new Error("bad combo"));
    await flush();

    expect(screen.getByTestId("usage-error")).toHaveTextContent("bad combo");
  });

  it("ignores a stale response after the selection changed", async () => {
    const client = new FakeWsClient();
    renderSection(client);
    // First request (runs/task) stays pending while the user changes groupBy.
    fireEvent.click(screen.getByTestId("settings-usage-group-by-model"));
    await flush();

    // Second request (runs/model) resolves first, the stale one later.
    client.nth("usage.summary", 1).resolve([runRow({ key: "gemini" })]);
    await flush();
    client.nth("usage.summary", 0).resolve([runRow({ key: "11", inputTokens: 999 })]);
    await flush();

    expect(screen.getByTestId("usage-row-runs-gemini")).toBeInTheDocument();
    expect(screen.queryByTestId("usage-row-runs-11")).not.toBeInTheDocument();
    expect(screen.getByTestId("usage-cell-inputTokens")).toHaveTextContent("12,345");
  });
  describe("subscription cost label", () => {
    const HINT = "Estimated at API list prices; subscription plans are not billed per token";
    const PROVIDERS = {
      providers: [
        { id: "claude-native", label: "Claude Code", credentialKind: "oauth" },
        { id: "glm", label: "GLM", kind: "cli" },
      ],
    };

    async function renderWithRows(rows: UsageSummaryRow[]) {
      const client = new FakeWsClient();
      renderSection(client);
      client.nth("usage.summary").resolve(rows);
      client.nth("provider.list").resolve(PROVIDERS);
      await flush();
      return client;
    }

    it("subscription provider row shows ≈ API price with tooltip", async () => {
      await renderWithRows([runRow({ providers: ["claude-native"] })]);
      const cost = screen.getByTestId("usage-cell-costUsd");
      expect(cost).toHaveTextContent("$0.042");
      const label = within(cost).getByText("≈ API price");
      expect(label).toHaveClass("text-foreground-muted");
      expect(label).toHaveAttribute("tabindex", "0");
      fireEvent.focus(label);
      expect(await screen.findByRole("tooltip")).toHaveTextContent(HINT);
    });

    it("API-key provider row shows plain cost", async () => {
      await renderWithRows([runRow({ providers: ["glm"] })]);
      const cost = screen.getByTestId("usage-cell-costUsd");
      expect(cost).toHaveTextContent("$0.042");
      expect(cost).not.toHaveTextContent("API price");
    });

    it("mixed row shows the label", async () => {
      await renderWithRows([runRow({ providers: ["glm", "claude-native"] })]);
      expect(screen.getByTestId("usage-cell-costUsd")).toHaveTextContent("≈ API price");
    });

    it("row without cost shows no label", async () => {
      await renderWithRows([runRow({ providers: ["claude-native"], costUsd: null })]);
      const cost = screen.getByTestId("usage-cell-costUsd");
      expect(cost).toHaveTextContent("—");
      expect(cost).not.toHaveTextContent("API price");
    });
  });
  describe("Space group-by", () => {
    const task = (ID: number, SpaceID: number | null) => ({ ...TASKS[0]!, ID, SpaceID, Title: `Task ${ID}` });
    const space = (ID: number, Title: string) => ({ ID, WorkspaceID: 1, Title, EnvData: "", CreatedAt: "", UpdatedAt: "" });

    // Backend: tasks 11 + 12, Web: task 13, task 14 is in no space, task 999 is not in the tree at all.
    async function renderSpaces(rows: UsageSummaryRow[]) {
      const client = new FakeWsClient();
      renderSection(client);
      client.nth("usage.summary").resolve(rows);
      client.nth("workspace.list").resolve(WORKSPACES);
      await flush();
      client.nth("space.list").resolve([space(1, "Backend"), space(2, "Web")]);
      client.nth("task.list").resolve([task(11, 1), task(12, 1), task(13, 2), task(14, null)]);
      await flush();
      return client;
    }

    it("usage-section Space group-by rolls task rows up by space", async () => {
      const client = await renderSpaces([
        runRow({ key: "11", count: 2, inputTokens: 100, costUsd: 0.5, unreported: ["reasoningTokens", "cacheReadTokens"], providers: ["glm"] }),
        runRow({ key: "12", count: 1, inputTokens: 50, costUsd: null, unreported: ["reasoningTokens"], providers: ["claude-native"] }),
        runRow({ key: "13", count: 4, inputTokens: 10, costUsd: 2 }),
        runRow({ key: "14", count: 1, inputTokens: 1, costUsd: 0.1 }),
        runRow({ key: "999", count: 1, inputTokens: 2, costUsd: 0.2 }),
      ]);
      fireEvent.click(screen.getByTestId("settings-usage-group-by-space"));
      await flush();

      // Space reuses the task rows: no second usage.summary, and groupBy never goes on the wire.
      expect(client.calls.filter((c) => c.method === "usage.summary")).toHaveLength(1);
      const body = screen.getAllByRole("row").slice(1);
      expect(body.map((r) => r.getAttribute("data-testid"))).toEqual([
        "usage-row-runs-space:2",
        "usage-row-runs-space:1",
        "usage-row-runs-space:none",
      ]);
      const backend = screen.getByTestId("usage-row-runs-space:1");
      expect(backend).toHaveTextContent("Backend");
      expect(within(backend).getByTestId("usage-cell-count")).toHaveTextContent("3");
      expect(within(backend).getByTestId("usage-cell-inputTokens")).toHaveTextContent("150");
      expect(within(backend).getByTestId("usage-cell-costUsd")).toHaveTextContent("$0.500");
      // reasoning is unreported in both rows, cached only in one.
      expect(within(backend).getByTestId("usage-cell-reasoningTokens")).toHaveTextContent("—");
      expect(within(backend).getByTestId("usage-cell-cacheReadTokens")).not.toHaveTextContent("—");
      expect(screen.getByTestId("usage-row-runs-space:2")).toHaveTextContent("Web");
      const none = screen.getByTestId("usage-row-runs-space:none");
      expect(none).toHaveTextContent("No space");
      expect(within(none).getByTestId("usage-cell-count")).toHaveTextContent("2");
      expect(within(none).getByTestId("usage-cell-costUsd")).toHaveTextContent("$0.300");
    });

    it("usage-section Space roll-up cost is em dash when no task reported cost", async () => {
      await renderSpaces([
        runRow({ key: "11", costUsd: null }),
        runRow({ key: "12", costUsd: null }),
        runRow({ key: "13", costUsd: 1 }),
      ]);
      fireEvent.click(screen.getByTestId("settings-usage-group-by-space"));
      await flush();

      expect(within(screen.getByTestId("usage-row-runs-space:1")).getByTestId("usage-cell-costUsd")).toHaveTextContent("—");
      expect(within(screen.getByTestId("usage-row-runs-space:2")).getByTestId("usage-cell-costUsd")).toHaveTextContent("$1.00");
    });

    it("usage-section Space option is disabled for proxy scope", async () => {
      const client = new FakeWsClient();
      renderSection(client);
      client.nth("usage.summary").resolve([]);
      await flush();
      expect((screen.getByTestId("settings-usage-group-by-space") as HTMLButtonElement).disabled).toBe(false);

      fireEvent.click(screen.getByTestId("settings-usage-scope-proxy"));
      await flush();
      expect((screen.getByTestId("settings-usage-group-by-space") as HTMLButtonElement).disabled).toBe(true);
    });
  });
});
