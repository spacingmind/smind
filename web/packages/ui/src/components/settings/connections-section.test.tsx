import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

const LOCAL = { id: "local", kind: "local" as const, label: "Local", baseUrl: "http://127.0.0.1:4648" };
const TUNNEL = { id: "url:example", kind: "url" as const, label: "Tunnel", baseUrl: "http://example.com:9000" };

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });
}

afterEach(() => {
  vi.resetModules();
  vi.restoreAllMocks();
  vi.doUnmock("@/lib/platform");
});

describe("connections-section registration", () => {
  it("does not register the Connections settings section in a non-desktop build", async () => {
    vi.doMock("@/lib/platform", () => ({
      isDesktop: false,
      desktop: {
        listConnections: vi.fn(),
        getCurrentConnection: vi.fn(),
        addConnection: vi.fn(),
        removeConnection: vi.fn(),
        selectConnection: vi.fn(),
      },
    }));

    const { listSettingsSections } = await import("@/components/settings/settings-registry");
    await import("@/components/settings/connections-section");

    expect(listSettingsSections().some((s) => s.id === "connections")).toBe(false);
  });
});

describe("connections-section (desktop build)", () => {
  async function renderSection(desktopApi: {
    listConnections: () => Promise<unknown>;
    getCurrentConnection: () => Promise<unknown>;
    addConnection?: (label: string, url: string) => Promise<unknown>;
    removeConnection?: (id: string) => Promise<unknown>;
    selectConnection?: (id: string) => Promise<unknown>;
  }) {
    vi.doMock("@/lib/platform", () => ({
      isDesktop: true,
      desktop: {
        addConnection: vi.fn(),
        removeConnection: vi.fn(),
        selectConnection: vi.fn(),
        ...desktopApi,
      },
    }));

    const { listSettingsSections } = await import("@/components/settings/settings-registry");
    await import("@/components/settings/connections-section");
    const section = listSettingsSections().find((s) => s.id === "connections");
    if (!section) throw new Error("connections section did not register");
    render(<>{section.render({ client: null })}</>);
    await flush();
    const platform = await import("@/lib/platform");
    return platform.desktop;
  }

  it("registers and lists connections, marking the current one", async () => {
    await renderSection({
      listConnections: () => Promise.resolve([LOCAL, TUNNEL]),
      getCurrentConnection: () => Promise.resolve(LOCAL),
    });

    expect(screen.getByTestId("connection-row-local")).toBeInTheDocument();
    expect(screen.getByTestId("connection-row-url:example")).toBeInTheDocument();
    expect(screen.getByTestId("connection-current-local")).toBeInTheDocument();
    // The local entry can never be removed (AC4); a url entry can.
    expect(screen.queryByTestId("connection-remove-local")).not.toBeInTheDocument();
    expect(screen.getByTestId("connection-remove-url:example")).toBeInTheDocument();
  });

  it("Switch calls selectConnection with the row's id", async () => {
    const desktop = await renderSection({
      listConnections: () => Promise.resolve([LOCAL, TUNNEL]),
      getCurrentConnection: () => Promise.resolve(LOCAL),
    });

    fireEvent.click(screen.getByTestId("connection-select-url:example"));
    await flush();

    expect(desktop.selectConnection).toHaveBeenCalledWith("url:example");
  });

  it("Remove calls removeConnection with the row's id", async () => {
    const desktop = await renderSection({
      listConnections: () => Promise.resolve([LOCAL, TUNNEL]),
      getCurrentConnection: () => Promise.resolve(LOCAL),
    });

    fireEvent.click(screen.getByTestId("connection-remove-url:example"));
    await flush();

    expect(desktop.removeConnection).toHaveBeenCalledWith("url:example");
  });

  it("Add submits the label and URL fields to addConnection", async () => {
    const desktop = await renderSection({
      listConnections: () => Promise.resolve([LOCAL]),
      getCurrentConnection: () => Promise.resolve(LOCAL),
    });

    fireEvent.change(screen.getByTestId("connection-add-label"), { target: { value: "Tunnel" } });
    fireEvent.change(screen.getByTestId("connection-add-url"), { target: { value: "http://example.com:9000" } });
    fireEvent.click(screen.getByTestId("connection-add-submit"));
    await flush();

    expect(desktop.addConnection).toHaveBeenCalledWith("Tunnel", "http://example.com:9000");
  });

  it("shows an error message when addConnection rejects", async () => {
    await renderSection({
      listConnections: () => Promise.resolve([LOCAL]),
      getCurrentConnection: () => Promise.resolve(LOCAL),
      addConnection: () => Promise.reject(new Error("invalid URL")),
    });

    fireEvent.change(screen.getByTestId("connection-add-url"), { target: { value: "not a url" } });
    fireEvent.click(screen.getByTestId("connection-add-submit"));
    await flush();

    expect(screen.getByTestId("connection-error")).toHaveTextContent("invalid URL");
  });
});
