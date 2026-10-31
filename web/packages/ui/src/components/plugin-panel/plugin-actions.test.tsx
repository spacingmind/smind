import { act, fireEvent, render, screen } from "@testing-library/react";
import type { QueryClient } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";

import { PluginActionsContext, type PluginActions } from "@/components/plugin-panel/plugin-actions-context";
import { PluginPanelPane } from "@/components/plugin-panel/plugin-panel-pane";
import { clearToasts } from "@/components/ui/toast";
import { queryKeys } from "@/lib/query-keys";
import { DaemonQueryProvider } from "@/lib/query/daemon-query-provider";
import { createQueryClient } from "@/lib/query/query-client";
import { openExternalUrl } from "@/lib/external-link";
import { useComposerInsertStore } from "@/stores/composer-insert-store";
import { FakeWsClient } from "@/test/fake-ws-client";
import "@/test/query-scheduler";

vi.mock("@/lib/external-link", () => ({ openExternalUrl: vi.fn() }));

/** The daemon's actions-all fixture document: one button per action kind. */
const ACTIONS_DOC = {
  ui: 1,
  root: {
    type: "stack",
    children: [
      { type: "button", label: "Refresh", action: { kind: "rpc", rpc: "refresh", params: { depth: 2 } } },
      { type: "button", label: "Ask", action: { kind: "prompt", text: "Why did the suite slow down?" } },
      { type: "button", label: "Command", action: { kind: "command", name: "acme-review:refresh" } },
      { type: "button", label: "Panel", action: { kind: "openPanel", panel: "overview", context: { task: "current" } } },
      { type: "button", label: "Open", action: { kind: "openUrl", url: "https://Example.com:8443/a?x=1", host: "example.com:8443" } },
    ],
  },
};

const FORM_DOC = {
  ui: 1,
  root: {
    type: "form",
    id: "report",
    fields: [
      { name: "title", label: "Title", required: true, type: "text" },
      { name: "notify", label: "Notify", type: "toggle" },
    ],
    submit: { label: "File report", action: { kind: "rpc", rpc: "refresh", params: { form: "report" } } },
  },
};

function docWith(children: unknown) {
  return { ui: 1, root: { type: "stack", children } };
}

/** Renders the pane around a view document (same harness as plugin-panel-pane.test.tsx). */
function renderPane({
  doc = ACTIONS_DOC,
  actions,
  queryClient = createQueryClient(),
}: { doc?: unknown; actions?: PluginActions; queryClient?: QueryClient } = {}) {
  const client = new FakeWsClient();
  const provided: PluginActions = actions ?? { openPanel: vi.fn() };
  render(
    <DaemonQueryProvider queryClient={queryClient} client={client as never} events={null}>
      <PluginActionsContext.Provider value={provided}>
        <PluginPanelPane pluginId="acme" panelId="overview" />
      </PluginActionsContext.Provider>
    </DaemonQueryProvider>,
  );
  client.nth("plugin.view", 0).resolve({ view: doc });
  return { client, queryClient };
}

/** The composer insert store needs a mounted composer for an insert to land. */
function mountComposer(): () => void {
  return useComposerInsertStore.getState().register("7:1");
}

async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 8; i++) await Promise.resolve();
  });
}

async function click(el: HTMLElement): Promise<void> {
  fireEvent.click(el);
  await flush();
}

afterEach(() => {
  useComposerInsertStore.setState({ mounted: [], target: null, request: null });
  clearToasts();
  vi.mocked(openExternalUrl).mockClear();
});

describe("plugin panel actions", () => {
  it("prompt-action-inserts-not-sends", async () => {
    const unregister = mountComposer();
    const { client } = renderPane();
    await flush();

    await click(screen.getByRole("button", { name: "Ask" }));

    expect(useComposerInsertStore.getState().request?.text).toBe("Why did the suite slow down?");
    // Inserting text touched nothing but reads: no send, no plugin rpc.
    const senders = client.calls
      .map((c) => c.method)
      .filter((m) => /^(task\.|run\.|chat\.)/.test(m) || m === "plugin.call");
    expect(senders).toEqual([]);
    expect([...new Set(client.calls.map((c) => c.method))].sort()).toEqual(["plugin.commands", "plugin.panels", "plugin.view"]);
    unregister();
  });

  it("openUrl-confirm-shows-host", async () => {
    renderPane();
    await flush();

    await click(screen.getByRole("button", { name: "Open" }));
    const dialog = screen.getByTestId("plugin-openurl-confirm");
    expect(dialog.textContent).toContain("example.com:8443");
    expect(dialog.textContent).toContain("acme");
    expect(dialog.querySelector("code")?.textContent).toBe("https://Example.com:8443/a?x=1");
    expect(openExternalUrl).not.toHaveBeenCalled();

    await click(screen.getByTestId("plugin-openurl-confirm-cancel"));
    expect(openExternalUrl).not.toHaveBeenCalled();
    expect(screen.queryByTestId("plugin-openurl-confirm")).toBeNull();

    await click(screen.getByRole("button", { name: "Open" }));
    await click(screen.getByTestId("plugin-openurl-confirm-confirm"));
    expect(openExternalUrl).toHaveBeenCalledTimes(1);
    expect(openExternalUrl).toHaveBeenCalledWith("https://Example.com:8443/a?x=1");
    expect(screen.queryByTestId("plugin-openurl-confirm")).toBeNull();
  });

  it("openUrl-ignores-non-http-schemes", async () => {
    renderPane({
      doc: docWith([
        { type: "button", label: "Js", action: { kind: "openUrl", url: "javascript:alert(1)", host: "example.com" } },
        { type: "button", label: "File", action: { kind: "openUrl", url: "file:///etc/passwd", host: "example.com" } },
        { type: "button", label: "Creds", action: { kind: "openUrl", url: "https://user:pw@example.com/", host: "example.com" } },
      ]),
    });
    await flush();

    for (const label of ["Js", "File", "Creds"]) {
      await click(screen.getByRole("button", { name: label }));
      expect(screen.queryByTestId("plugin-openurl-confirm")).toBeNull();
      expect(screen.queryByRole("dialog")).toBeNull();
    }
    expect(openExternalUrl).not.toHaveBeenCalled();
  });

  it("openUrl-host-comes-from-the-url", async () => {
    renderPane({
      doc: docWith([
        { type: "button", label: "Open", action: { kind: "openUrl", url: "https://real.example/x", host: "fake.example" } },
      ]),
    });
    await flush();

    await click(screen.getByRole("button", { name: "Open" }));
    const dialog = screen.getByTestId("plugin-openurl-confirm");
    expect(dialog.textContent).toContain("real.example");
    expect(dialog.textContent).not.toContain("fake.example");
  });

  it("rpc action calls plugin.call and refreshes the panel", async () => {
    const { client } = renderPane();
    await flush();

    await click(screen.getByRole("button", { name: "Refresh" }));
    expect(client.nth("plugin.call", 0).params).toEqual({ pluginId: "acme", rpc: "refresh", params: { depth: 2 } });

    client.nth("plugin.call", 0).resolve({});
    await flush();
    expect(client.calls.filter((c) => c.method === "plugin.view")).toHaveLength(2);
  });

  it("form submit carries the field values", async () => {
    const { client } = renderPane({ doc: FORM_DOC });
    await flush();

    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Hello" } });
    await click(screen.getByRole("button", { name: "File report" }));
    expect(client.nth("plugin.call", 0).params).toEqual({
      pluginId: "acme",
      rpc: "refresh",
      params: { form: "report", values: { title: "Hello", notify: false } },
    });
  });

  it("openPanel hands the plugin, panel and context to the app's actions", async () => {
    const openPanel = vi.fn();
    renderPane({ actions: { openPanel } });
    await flush();

    await click(screen.getByRole("button", { name: "Panel" }));
    expect(openPanel).toHaveBeenCalledTimes(1);
    expect(openPanel).toHaveBeenCalledWith("acme", "overview", { task: "current" });
  });

  it("command action inserts the command's prompt text", async () => {
    const unregister = mountComposer();
    const queryClient = createQueryClient();
    queryClient.setQueryData(queryKeys.pluginCommands(), [
      { plugin: "acme-review", name: "refresh", qualified: "acme-review:refresh", prompt: "Refresh now" },
    ]);
    const { client } = renderPane({ queryClient });
    await flush();

    await click(screen.getByRole("button", { name: "Command" }));
    expect(useComposerInsertStore.getState().request?.text).toBe("Refresh now");
    expect(client.calls.filter((c) => c.method === "plugin.call")).toHaveLength(0);
    unregister();
  });

  it("command action with an rpc asks the plugin for its prompt, then inserts it", async () => {
    const unregister = mountComposer();
    const queryClient = createQueryClient();
    queryClient.setQueryData(queryKeys.pluginCommands(), [
      { plugin: "acme-review", name: "refresh", qualified: "acme-review:refresh", rpc: "refresh-prompt" },
    ]);
    const { client } = renderPane({ queryClient });
    await flush();

    await click(screen.getByRole("button", { name: "Command" }));
    expect(client.nth("plugin.call", 0).params).toEqual({
      pluginId: "acme-review",
      rpc: "refresh-prompt",
      params: { command: "refresh", args: "" },
    });

    client.nth("plugin.call", 0).resolve({ prompt: "From the plugin" });
    await flush();
    expect(useComposerInsertStore.getState().request?.text).toBe("From the plugin");
    // The rpc-backed command never refreshed the panel (no plugin.view re-read) and never sent.
    expect(client.calls.filter((c) => c.method === "plugin.view")).toHaveLength(1);
    expect(client.calls.map((c) => c.method).some((m) => /^(task\.|run\.|chat\.)/.test(m))).toBe(false);
    unregister();
  });

  it("command action with an unknown name toasts and inserts nothing", async () => {
    const unregister = mountComposer();
    const { client } = renderPane({
      doc: docWith([{ type: "button", label: "Ghost", action: { kind: "command", name: "nope:x" } }]),
    });
    await flush();

    await click(screen.getByRole("button", { name: "Ghost" }));
    expect(useComposerInsertStore.getState().request).toBeNull();
    expect(client.calls.map((c) => c.method).some((m) => m === "plugin.call")).toBe(false);
    unregister();
  });

  it("an action whose kind is none of the five is ignored", async () => {
    const { client } = renderPane({
      doc: docWith([
        { type: "button", label: "Odd", action: { kind: "detonate", url: "https://evil.example" } },
        { type: "button", label: "No rpc", action: { kind: "rpc" } },
      ]),
    });
    await flush();

    await click(screen.getByRole("button", { name: "Odd" }));
    await click(screen.getByRole("button", { name: "No rpc" }));
    expect([...new Set(client.calls.map((c) => c.method))].sort()).toEqual(["plugin.commands", "plugin.panels", "plugin.view"]);
    expect(screen.queryByTestId("plugin-openurl-confirm")).toBeNull();
  });
});
