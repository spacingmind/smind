import { act } from "react";
import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { listSettingsSections } from "@/components/settings/settings-registry";
import "@/components/settings/mcp-servers-section";
import type { DaemonEvents } from "@/hooks/use-daemon-events";
import type { McpServer } from "@/lib/types";
import { FakeWsClient } from "@/test/fake-ws-client";

// The daemon redacts every env/headers value to this literal; the UI must
// never expect or render anything else for an existing key.
const REDACTED = "[redacted]";

const STDIO_SERVER: McpServer = {
  id: 1,
  name: "playwright",
  transport: "stdio",
  command: "npx",
  args: ["-y", "@playwright/mcp@latest", "--headless"],
  env: { PLAYWRIGHT_API_KEY: REDACTED },
  url: "",
  headers: {},
  enabled: true,
  createdAt: "2026-10-10T00:00:00Z",
  updatedAt: "2026-10-10T00:00:00Z",
};

const HTTP_SERVER: McpServer = {
  id: 2,
  name: "remote",
  transport: "http",
  command: "",
  args: [],
  env: {},
  url: "https://example.com/mcp",
  headers: { Authorization: REDACTED },
  enabled: true,
  createdAt: "2026-10-10T00:00:00Z",
  updatedAt: "2026-10-10T00:00:00Z",
};

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

/** A minimal DaemonEvents fake: records listeners per topic and lets a test fire one directly. */
function fakeDaemonEvents(): DaemonEvents & { fire(topic: string, payload: unknown): void } {
  const listeners = new Map<string, Set<(payload: unknown) => void>>();
  return {
    subscribe(topic, listener) {
      let set = listeners.get(topic);
      if (!set) {
        set = new Set();
        listeners.set(topic, set);
      }
      set.add(listener);
      return () => set!.delete(listener);
    },
    fire(topic, payload) {
      for (const fn of listeners.get(topic) ?? []) fn(payload);
    },
  };
}

function renderSection(client: FakeWsClient, events?: DaemonEvents) {
  const section = listSettingsSections().find((s) => s.id === "mcp-servers");
  if (!section) throw new Error("mcp-servers section did not register");
  render(<>{section.render({ client: client as never, events })}</>);
}

describe("mcp-servers-section", () => {
  it("lists servers with transport badge and never renders secret values", async () => {
    const client = new FakeWsClient();
    renderSection(client);
    client.nth("mcp.list").resolve([STDIO_SERVER, HTTP_SERVER]);
    await flush();

    const stdioRow = screen.getByTestId("mcp-row-1");
    expect(within(stdioRow).getByText("playwright")).toBeInTheDocument();
    expect(within(stdioRow).getByTestId("mcp-transport-1")).toHaveTextContent("stdio");
    // Command + args summary, and the env key count -- never the value.
    expect(stdioRow).toHaveTextContent("npx -y @playwright/mcp@latest --headless");
    expect(stdioRow).toHaveTextContent("1 env var");
    expect(stdioRow).not.toHaveTextContent(REDACTED);
    expect(stdioRow.textContent).not.toContain("SUPERSECRET");

    const httpRow = screen.getByTestId("mcp-row-2");
    expect(within(httpRow).getByTestId("mcp-transport-2")).toHaveTextContent("http");
    expect(httpRow).toHaveTextContent("https://example.com/mcp");
    expect(httpRow).toHaveTextContent("1 header");
    expect(httpRow).not.toHaveTextContent(REDACTED);
  });

  it("renders empty state when no servers", async () => {
    const client = new FakeWsClient();
    renderSection(client);
    client.nth("mcp.list").resolve([]);
    await flush();

    expect(screen.getByTestId("mcp-empty-state")).toBeInTheDocument();
    expect(screen.getByText(/Give agents tools like a browser/)).toBeInTheDocument();
  });

  it("creates a stdio server via mcp.create with env as an object", async () => {
    const client = new FakeWsClient();
    renderSection(client);
    client.nth("mcp.list").resolve([]);
    await flush();

    fireEvent.click(screen.getByTestId("mcp-new-button"));
    await flush();
    fireEvent.change(screen.getByTestId("mcp-form-name"), { target: { value: "playwright" } });
    fireEvent.change(screen.getByTestId("mcp-form-command"), { target: { value: "npx" } });
    fireEvent.change(screen.getByTestId("mcp-form-args"), { target: { value: "-y\n@playwright/mcp@latest" } });
    fireEvent.click(screen.getByTestId("mcp-form-env-add"));
    fireEvent.change(screen.getByTestId("mcp-form-env-key-0"), { target: { value: "API_KEY" } });
    // The user types the secret; it lives only in the controlled input.
    fireEvent.change(screen.getByTestId("mcp-form-env-value-0"), { target: { value: "SUPERSECRET" } });
    fireEvent.click(screen.getByTestId("mcp-form-submit"));
    await flush();

    expect(client.nth("mcp.create").params).toEqual({
      name: "playwright",
      transport: "stdio",
      command: "npx",
      args: ["-y", "@playwright/mcp@latest"],
      env: { API_KEY: "SUPERSECRET" },
      url: "",
      headers: {},
    });

    // The daemon's answer redacts the value; the re-rendered row never
    // shows what was typed.
    client.nth("mcp.create").resolve({ ...STDIO_SERVER, env: { API_KEY: REDACTED } });
    await flush();
    expect(screen.getByTestId("mcp-row-1")).not.toHaveTextContent("SUPERSECRET");
    expect(screen.queryByTestId("mcp-new-form")).not.toBeInTheDocument();
  });

  it("creating http server sends url and headers and omits command", async () => {
    const client = new FakeWsClient();
    renderSection(client);
    client.nth("mcp.list").resolve([]);
    await flush();

    fireEvent.click(screen.getByTestId("mcp-new-button"));
    await flush();
    fireEvent.change(screen.getByTestId("mcp-form-name"), { target: { value: "remote" } });
    fireEvent.click(screen.getByTestId("mcp-form-transport"));
    fireEvent.click(await screen.findByRole("option", { name: "http" }));
    await flush();
    fireEvent.change(screen.getByTestId("mcp-form-url"), { target: { value: "https://example.com/mcp" } });
    fireEvent.click(screen.getByTestId("mcp-form-headers-add"));
    fireEvent.change(screen.getByTestId("mcp-form-headers-key-0"), { target: { value: "Authorization" } });
    fireEvent.change(screen.getByTestId("mcp-form-headers-value-0"), { target: { value: "Bearer tok" } });
    fireEvent.click(screen.getByTestId("mcp-form-submit"));
    await flush();

    expect(client.nth("mcp.create").params).toEqual({
      name: "remote",
      transport: "http",
      command: "",
      args: [],
      env: {},
      url: "https://example.com/mcp",
      headers: { Authorization: "Bearer tok" },
    });
  });

  it("validates required fields per transport before calling the daemon", async () => {
    const client = new FakeWsClient();
    renderSection(client);
    client.nth("mcp.list").resolve([]);
    await flush();

    fireEvent.click(screen.getByTestId("mcp-new-button"));
    await flush();
    // Empty name on stdio.
    fireEvent.click(screen.getByTestId("mcp-form-submit"));
    await flush();
    expect(screen.getByTestId("mcp-form-error")).toHaveTextContent("Name is required.");
    expect(client.calls.some((c) => c.method === "mcp.create")).toBe(false);

    // Name but no command.
    fireEvent.change(screen.getByTestId("mcp-form-name"), { target: { value: "x" } });
    fireEvent.click(screen.getByTestId("mcp-form-submit"));
    await flush();
    expect(screen.getByTestId("mcp-form-error")).toHaveTextContent("Command is required for stdio servers.");
    expect(client.calls.some((c) => c.method === "mcp.create")).toBe(false);

    // http with no url.
    fireEvent.click(screen.getByTestId("mcp-form-transport"));
    fireEvent.click(await screen.findByRole("option", { name: "http" }));
    await flush();
    fireEvent.click(screen.getByTestId("mcp-form-submit"));
    await flush();
    expect(screen.getByTestId("mcp-form-error")).toHaveTextContent("URL is required for http/sse servers.");
    expect(client.calls.some((c) => c.method === "mcp.create")).toBe(false);
  });

  it("toggling enabled calls mcp.setEnabled", async () => {
    const client = new FakeWsClient();
    renderSection(client);
    client.nth("mcp.list").resolve([STDIO_SERVER]);
    await flush();

    fireEvent.click(screen.getByTestId("mcp-enabled-1"));
    await flush();

    expect(client.nth("mcp.setEnabled").params).toEqual({ id: 1, enabled: false });
    client.nth("mcp.setEnabled").resolve({ ...STDIO_SERVER, enabled: false });
    await flush();

    const row = screen.getByTestId("mcp-row-1");
    expect((within(row).getByTestId("mcp-enabled-1") as HTMLInputElement).checked).toBe(false);
  });

  it("delete asks for confirmation then calls mcp.delete", async () => {
    const client = new FakeWsClient();
    renderSection(client);
    client.nth("mcp.list").resolve([STDIO_SERVER]);
    await flush();

    fireEvent.pointerDown(screen.getByTestId("mcp-menu-1"), { button: 0 });
    fireEvent.click(await screen.findByTestId("mcp-delete-1"));
    await flush();

    // The menu item only armed the inline confirmation -- nothing sent yet.
    expect(client.calls.some((c) => c.method === "mcp.delete")).toBe(false);
    expect(screen.getByTestId("mcp-delete-confirm-1")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("mcp-delete-confirm-1"));
    await flush();

    expect(client.nth("mcp.delete").params).toEqual({ id: 1 });
    client.nth("mcp.delete").resolve({});
    await flush();
    expect(screen.queryByTestId("mcp-row-1")).not.toBeInTheDocument();
  });

  it("edit keeps [redacted] placeholders when values untouched and sends new value when changed", async () => {
    const client = new FakeWsClient();
    renderSection(client);
    client.nth("mcp.list").resolve([STDIO_SERVER]);
    await flush();

    fireEvent.pointerDown(screen.getByTestId("mcp-menu-1"), { button: 0 });
    fireEvent.click(await screen.findByTestId("mcp-edit-1"));
    await flush();

    const valueInput = screen.getByTestId("mcp-edit-form-1-env-value-0") as HTMLInputElement;
    // Prefilled with the placeholder, masked.
    expect(valueInput).toHaveAttribute("type", "password");
    expect(valueInput.value).toBe(REDACTED);
    expect(screen.getByTestId("mcp-edit-form-1-name").closest("form")?.textContent ?? "").not.toContain("SUPERSECRET");

    // Untouched: submit sends the placeholder back ("keep the secret").
    fireEvent.click(screen.getByTestId("mcp-edit-form-1-submit"));
    await flush();
    expect(client.nth("mcp.update").params).toMatchObject({ id: 1, env: { PLAYWRIGHT_API_KEY: REDACTED } });

    // Retyped: the new value goes out instead.
    client.nth("mcp.update").resolve(STDIO_SERVER);
    await flush();
    fireEvent.pointerDown(screen.getByTestId("mcp-menu-1"), { button: 0 });
    fireEvent.click(await screen.findByTestId("mcp-edit-1"));
    await flush();
    fireEvent.change(screen.getByTestId("mcp-edit-form-1-env-value-0"), { target: { value: "SUPERSECRET2" } });
    fireEvent.click(screen.getByTestId("mcp-edit-form-1-submit"));
    await flush();
    expect(client.nth("mcp.update", 1).params).toMatchObject({ id: 1, env: { PLAYWRIGHT_API_KEY: "SUPERSECRET2" } });
  });

  it("shows the daemon's conflict error inline", async () => {
    const client = new FakeWsClient();
    renderSection(client);
    client.nth("mcp.list").resolve([]);
    await flush();

    fireEvent.click(screen.getByTestId("mcp-new-button"));
    await flush();
    fireEvent.change(screen.getByTestId("mcp-form-name"), { target: { value: "dupe" } });
    fireEvent.change(screen.getByTestId("mcp-form-command"), { target: { value: "npx" } });
    fireEvent.click(screen.getByTestId("mcp-form-submit"));
    await flush();

    client.nth("mcp.create").reject(new Error("mcp: name already exists"));
    await flush();

    expect(screen.getByTestId("mcp-form-error")).toHaveTextContent("name already exists");
    // The card stays open.
    expect(screen.getByTestId("mcp-new-form")).toBeInTheDocument();
  });

  it("live mcpServer.created/updated/deleted events update the list", async () => {
    const client = new FakeWsClient();
    const events = fakeDaemonEvents();
    renderSection(client, events);
    client.nth("mcp.list").resolve([]);
    await flush();
    expect(screen.getByTestId("mcp-empty-state")).toBeInTheDocument();

    act(() => {
      events.fire("mcpServer.created", { server: STDIO_SERVER });
    });
    await flush();
    expect(screen.getByTestId("mcp-row-1")).toBeInTheDocument();
    expect(screen.queryByTestId("mcp-empty-state")).not.toBeInTheDocument();

    act(() => {
      events.fire("mcpServer.updated", { server: { ...STDIO_SERVER, name: "pw" } });
    });
    await flush();
    expect(screen.getByText("pw")).toBeInTheDocument();
    expect(screen.queryByText("playwright")).not.toBeInTheDocument();

    act(() => {
      events.fire("mcpServer.deleted", { id: 1 });
    });
    await flush();
    expect(screen.queryByTestId("mcp-row-1")).not.toBeInTheDocument();
  });

  it("Playwright quick-fill populates the form", async () => {
    const client = new FakeWsClient();
    renderSection(client);
    client.nth("mcp.list").resolve([]);
    await flush();

    fireEvent.click(screen.getByTestId("mcp-new-button"));
    await flush();
    fireEvent.click(screen.getByTestId("mcp-form-playwright"));
    await flush();

    expect(screen.getByTestId("mcp-form-name")).toHaveValue("playwright");
    expect(screen.getByTestId("mcp-form-transport")).toHaveTextContent("stdio");
    expect(screen.getByTestId("mcp-form-command")).toHaveValue("npx");
    expect(screen.getByTestId("mcp-form-args")).toHaveValue("-y\n@playwright/mcp@latest\n--headless");
  });
});
