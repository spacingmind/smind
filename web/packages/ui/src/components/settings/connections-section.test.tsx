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
    vi.doMock("@/lib/platform", async (orig) => ({
      ...(await orig<typeof import("@/lib/platform")>()),
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
    vi.doMock("@/lib/platform", async (orig) => ({
      ...(await orig<typeof import("@/lib/platform")>()),
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

describe("Add via relay link (desktop build)", () => {
  const RELAY = { id: "relay:ws-1@relay.test:7400", kind: "relay" as const, label: "Home", baseUrl: "relay://ws-1@relay.test:7400" };
  const LINK = "https://spacingmind.sh/pair#offer=c2VjcmV0LXBheWxvYWQ";
  const preview = (kind: "newHost" | "alreadySaved" | "changedConnection" = "newHost") => ({
    relayAddress: "relay.test:7401",
    relayHostId: "ws-1",
    keyFingerprint: "ERER ERER \u2026 RERE RERE",
    pinned: true,
    kind,
  });

  async function renderRelay(overrides: Record<string, unknown> = {}) {
    const api = {
      listConnections: vi.fn(() => Promise.resolve([LOCAL])),
      getCurrentConnection: vi.fn(() => Promise.resolve(LOCAL)),
      addConnection: vi.fn(),
      removeConnection: vi.fn(),
      selectConnection: vi.fn(() => Promise.resolve(RELAY)),
      previewRelayLink: vi.fn(() => Promise.resolve(preview())),
      addRelayConnection: vi.fn(() => Promise.resolve(RELAY)),
      ...overrides,
    };
    vi.doMock("@/lib/platform", async (orig) => ({
      ...(await orig<typeof import("@/lib/platform")>()),
      isDesktop: true,
      desktop: api,
    }));
    const { listSettingsSections } = await import("@/components/settings/settings-registry");
    await import("@/components/settings/connections-section");
    const section = listSettingsSections().find((s) => s.id === "connections");
    if (!section) throw new Error("connections section did not register");
    const view = render(<>{section.render({ client: null })}</>);
    await flush();
    return { api, view, section };
  }

  async function paste(value: string) {
    fireEvent.change(screen.getByTestId("relay-link-input"), { target: { value } });
    fireEvent.click(screen.getByTestId("relay-link-review"));
    await flush();
  }

  it("masks the pasted link and does not autocomplete it", async () => {
    await renderRelay();
    const input = screen.getByTestId("relay-link-input");
    expect(input).toHaveAttribute("type", "password");
    expect(input).toHaveAttribute("autocomplete", "off");
    expect(screen.getByTestId("relay-link-warning")).toHaveTextContent(/like a password/i);
  });

  it("previews a pasted relay link and shows relay host, address and fingerprint without the secret", async () => {
    const { api } = await renderRelay();
    await paste(LINK);

    expect(api.previewRelayLink).toHaveBeenCalledWith(LINK);
    expect(screen.getByTestId("relay-link-address")).toHaveTextContent("relay.test:7401");
    expect(screen.getByTestId("relay-link-host-id")).toHaveTextContent("ws-1");
    expect(screen.getByTestId("relay-link-fingerprint")).toHaveTextContent("ERER ERER");
    expect(screen.getByTestId("relay-link-trust")).toHaveTextContent(/pinned/i);
    expect(screen.getByTestId("relay-link-confirm").textContent).not.toContain("c2VjcmV0");
    expect(screen.queryByTestId("relay-link-input")).not.toBeInTheDocument();
  });

  it("adds the connection only after Add is pressed, then selects it and refreshes", async () => {
    const { api } = await renderRelay();
    fireEvent.change(screen.getByTestId("relay-link-label-input"), { target: { value: "Home" } });
    await paste(LINK);
    expect(api.addRelayConnection).not.toHaveBeenCalled();
    expect(screen.getByTestId("relay-link-label")).toHaveTextContent("Home");

    fireEvent.click(screen.getByTestId("relay-link-add"));
    await flush();

    expect(api.addRelayConnection).toHaveBeenCalledWith("Home", LINK);
    expect(api.selectConnection).toHaveBeenCalledWith(RELAY.id);
    expect(api.listConnections).toHaveBeenCalledTimes(2);
    // Back to a clean paste step with the link gone.
    expect(screen.getByTestId("relay-link-input")).toHaveValue("");
  });

  it("Cancel adds and saves nothing and clears the link", async () => {
    const { api } = await renderRelay();
    await paste(LINK);
    fireEvent.click(screen.getByTestId("relay-link-cancel"));
    await flush();

    expect(api.addRelayConnection).not.toHaveBeenCalled();
    expect(api.selectConnection).not.toHaveBeenCalled();
    expect(screen.getByTestId("relay-link-input")).toHaveValue("");
  });

  it("warns and defaults to Cancel when the same relay host returns with a different key or relay", async () => {
    await renderRelay({ previewRelayLink: vi.fn(() => Promise.resolve(preview("changedConnection"))) });
    await paste(LINK);

    expect(screen.getByTestId("relay-link-changed")).toHaveTextContent(/key or relay changed/i);
    expect(screen.getByTestId("relay-link-cancel")).toHaveFocus();
    expect(screen.getByTestId("relay-link-add")).toHaveTextContent("Add anyway");
  });

  it("shows the Rust error inline for an invalid link and clears the link", async () => {
    await renderRelay({
      previewRelayLink: vi.fn(() => Promise.reject(new Error("pairing: invalid offer: unsupported offer version 2"))),
    });
    await paste(LINK);

    expect(screen.getByTestId("relay-link-error")).toHaveTextContent("unsupported offer version 2");
    expect(screen.getByTestId("relay-link-error")).toHaveAttribute("role", "alert");
    expect(screen.getByTestId("relay-link-input")).toHaveValue("");
  });

  it("rejects an oversized link without sending it to Rust", async () => {
    const { api } = await renderRelay();
    await paste("https://x/pair#offer=" + "A".repeat(5000));

    expect(api.previewRelayLink).not.toHaveBeenCalled();
    expect(screen.getByTestId("relay-link-error")).toHaveTextContent(/longer than 4096/);
    expect(screen.getByTestId("relay-link-input")).toHaveValue("");
  });

  it("clears the link and shows the error when saving fails", async () => {
    const { api } = await renderRelay({ addRelayConnection: vi.fn(() => Promise.reject(new Error("disk full"))) });
    await paste(LINK);
    fireEvent.click(screen.getByTestId("relay-link-add"));
    await flush();

    expect(screen.getByTestId("relay-link-error")).toHaveTextContent("disk full");
    expect(screen.getByTestId("relay-link-input")).toHaveValue("");
    expect(api.selectConnection).not.toHaveBeenCalled();
  });

  it("keeps the connection and says so when switching to it fails", async () => {
    const { api } = await renderRelay({ selectConnection: vi.fn(() => Promise.reject(new Error("watcher down"))) });
    await paste(LINK);
    fireEvent.click(screen.getByTestId("relay-link-add"));
    await flush();

    expect(api.addRelayConnection).toHaveBeenCalled();
    expect(screen.getByTestId("relay-link-error")).toHaveTextContent(/Added "Home", but switching to it failed: watcher down/);
  });

  it("locks the input while a review is in flight so Add saves exactly the previewed link", async () => {
    let resolvePreview!: (p: ReturnType<typeof preview>) => void;
    const { api } = await renderRelay({
      previewRelayLink: vi.fn(() => new Promise((r) => (resolvePreview = r))),
    });
    fireEvent.change(screen.getByTestId("relay-link-input"), { target: { value: LINK } });
    fireEvent.click(screen.getByTestId("relay-link-review"));
    await flush();
    expect(screen.getByTestId("relay-link-input")).toBeDisabled();

    await act(async () => resolvePreview(preview()));
    fireEvent.click(screen.getByTestId("relay-link-add"));
    await flush();
    expect(api.addRelayConnection).toHaveBeenCalledWith("", LINK);
  });

  it("drops the pasted link on unmount", async () => {
    const { view, section } = await renderRelay();
    fireEvent.change(screen.getByTestId("relay-link-input"), { target: { value: LINK } });
    view.unmount();
    render(<>{section.render({ client: null })}</>);
    await flush();
    expect(screen.getByTestId("relay-link-input")).toHaveValue("");
  });

  it("routes a relay link pasted into the URL form to the relay flow and never saves a URL", async () => {
    const { api } = await renderRelay();
    fireEvent.change(screen.getByTestId("connection-add-url"), { target: { value: LINK } });
    fireEvent.click(screen.getByTestId("connection-add-submit"));
    await flush();

    expect(api.addConnection).not.toHaveBeenCalled();
    expect(api.previewRelayLink).toHaveBeenCalledWith(LINK);
    expect(screen.getByTestId("connection-relay-handover")).toHaveTextContent(/relay pairing link/i);
    expect(screen.getByTestId("relay-link-confirm")).toBeInTheDocument();
    expect(screen.getByTestId("connection-add-url")).toHaveValue("");
    expect(screen.queryByTestId("connection-error")).not.toBeInTheDocument();
  });
});

