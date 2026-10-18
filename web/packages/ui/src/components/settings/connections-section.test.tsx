import { act } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

const LOCAL = { id: "local", kind: "local" as const, label: "Local", baseUrl: "http://127.0.0.1:4648" };
const SETTINGS = { onboardingCompleted: true, manageLocalDaemon: true };
const STATUS = { platform: "macos", reachable: false, managedState: "notRunning", localService: null };
const PROBE_OK = { reachable: true, version: "0.12.0", service: "smind", authRequired: false, loopback: false, error: null };

/** Defaults every desktop mock here needs now that the section also reads app settings and daemon status. */
function baseApi() {
  return {
    appSettingsGet: vi.fn(() => Promise.resolve(SETTINGS)),
    appSettingsSet: vi.fn((patch: object) => Promise.resolve({ ...SETTINGS, ...patch })),
    daemonStatus: vi.fn(() => Promise.resolve(STATUS)),
    probeConnection: vi.fn(() => Promise.resolve(PROBE_OK)),
    addSshConnection: vi.fn(),
  };
}

async function openAdd(method: "url" | "ssh" | "relay") {
  fireEvent.click(screen.getByTestId("connection-add-menu"));
  fireEvent.click(screen.getByTestId(`connection-add-menu-${method}`));
  await flush();
}

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
    addConnection?: (label: string, url: string, password?: string) => Promise<unknown>;
    daemonStatus?: () => Promise<unknown>;
    appSettingsGet?: () => Promise<unknown>;
    appSettingsSet?: (patch: object) => Promise<unknown>;
    removeConnection?: (id: string) => Promise<unknown>;
    selectConnection?: (id: string) => Promise<unknown>;
    relayStatus?: () => Promise<unknown>;
  }) {
    vi.doMock("@/lib/platform", async (orig) => ({
      ...(await orig<typeof import("@/lib/platform")>()),
      isDesktop: true,
      desktop: {
        ...baseApi(),
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

  it("shows the live relay state, and what to run, under the current relay connection only", async () => {
    const RELAY = { id: "relay:homelab@relay.example.test:7400", kind: "relay" as const, label: "Homelab", baseUrl: "relay:homelab@relay.example.test:7400" };
    const relayStatus = vi.fn().mockResolvedValue({ state: "awaitingApproval", deviceId: "abcdefghijklmnopqrstuvwxyz" });
    await renderSection({
      listConnections: () => Promise.resolve([LOCAL, RELAY]),
      getCurrentConnection: () => Promise.resolve(RELAY),
      relayStatus,
    });
    await flush();

    const row = screen.getByTestId(`connection-row-${RELAY.id}`);
    expect(row).toHaveTextContent("Waiting for approval on the daemon");
    expect(row).toHaveTextContent("smind relay devices approve abcdefghijklmnopqrstuvwxyz");
    // A different connection's row has no relay state of its own.
    expect(screen.getByTestId("connection-row-local")).not.toHaveTextContent("Waiting for approval");
  });

  it("does not ask for a relay state when the current connection is not a relay one", async () => {
    const relayStatus = vi.fn().mockResolvedValue({ state: "connected" });
    await renderSection({
      listConnections: () => Promise.resolve([LOCAL]),
      getCurrentConnection: () => Promise.resolve(LOCAL),
      relayStatus,
    });
    await flush();
    expect(relayStatus).not.toHaveBeenCalled();
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

  it("Add connection menu offers URL, SSH and relay link and reveals the matching form", async () => {
    await renderSection({
      listConnections: () => Promise.resolve([LOCAL]),
      getCurrentConnection: () => Promise.resolve(LOCAL),
    });

    // No add form is visible until a method is picked.
    expect(screen.queryByTestId("connection-add-url")).not.toBeInTheDocument();
    expect(screen.queryByTestId("relay-link-input")).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId("connection-add-menu"));
    expect(screen.getAllByRole("menuitem").map((el) => el.textContent)).toEqual([
      "Connect to a daemon by URL",
      "Connect over SSH",
      "Pair via relay link",
    ]);
    fireEvent.click(screen.getByTestId("connection-add-menu-ssh"));
    await flush();
    expect(screen.getByTestId("ssh-form")).toBeInTheDocument();
    fireEvent.click(screen.getByTestId("connection-add-close"));
    expect(screen.queryByTestId("ssh-form")).not.toBeInTheDocument();
  });

  it("Add probes the URL, then submits label and URL to addConnection", async () => {
    const desktop = await renderSection({
      listConnections: () => Promise.resolve([LOCAL]),
      getCurrentConnection: () => Promise.resolve(LOCAL),
      addConnection: vi.fn(() => Promise.resolve(TUNNEL)),
    });
    await openAdd("url");

    fireEvent.change(screen.getByTestId("connection-add-label"), { target: { value: "Tunnel" } });
    fireEvent.change(screen.getByTestId("connection-add-url"), { target: { value: "http://example.com:9000" } });
    fireEvent.click(screen.getByTestId("connection-add-submit"));
    await flush();
    expect(desktop.probeConnection).toHaveBeenCalledWith("http://example.com:9000");
    expect(desktop.addConnection).not.toHaveBeenCalled();
    fireEvent.click(screen.getByTestId("connection-add-submit"));
    await flush();

    expect(desktop.addConnection).toHaveBeenCalledWith("Tunnel", "http://example.com:9000", undefined);
    // The panel closes and the list refreshes; nothing is selected by adding.
    expect(screen.queryByTestId("connection-add-url")).not.toBeInTheDocument();
    expect(desktop.selectConnection).not.toHaveBeenCalled();
  });

  it("shows an error message when addConnection rejects", async () => {
    await renderSection({
      listConnections: () => Promise.resolve([LOCAL]),
      getCurrentConnection: () => Promise.resolve(LOCAL),
      addConnection: () => Promise.reject(new Error("daemon refused the login")),
    });
    await openAdd("url");

    fireEvent.change(screen.getByTestId("connection-add-url"), { target: { value: "http://example.com:9000" } });
    fireEvent.click(screen.getByTestId("connection-add-submit"));
    await flush();
    fireEvent.click(screen.getByTestId("connection-add-submit"));
    await flush();

    expect(screen.getByTestId("connection-error")).toHaveTextContent("daemon refused the login");
  });

  it("rejects text that is not an http(s) URL before probing", async () => {
    const desktop = await renderSection({
      listConnections: () => Promise.resolve([LOCAL]),
      getCurrentConnection: () => Promise.resolve(LOCAL),
    });
    await openAdd("url");

    fireEvent.change(screen.getByTestId("connection-add-url"), { target: { value: "not a url" } });
    fireEvent.click(screen.getByTestId("connection-add-submit"));
    await flush();

    expect(screen.getByTestId("connection-error")).toHaveTextContent(/http/);
    expect(desktop.probeConnection).not.toHaveBeenCalled();
  });

  it("lists ssh connections with an SSH tag", async () => {
    const SSH = { id: "ssh:homelab", kind: "ssh" as const, label: "homelab", baseUrl: "ssh://me@homelab.lan:2222?daemonPort=4648" };
    await renderSection({
      listConnections: () => Promise.resolve([LOCAL, SSH]),
      getCurrentConnection: () => Promise.resolve(LOCAL),
    });
    expect(screen.getByTestId("connection-row-ssh:homelab")).toHaveTextContent("homelab");
    expect(screen.getByTestId("connection-kind-ssh:homelab")).toHaveTextContent("SSH");
    // The stored target is shown (not a tunnel URL), the daemon port as a second detail.
    expect(screen.getByTestId("connection-row-ssh:homelab")).toHaveTextContent("ssh://me@homelab.lan:2222");
    expect(screen.getByTestId("connection-row-ssh:homelab")).toHaveTextContent("daemon port 4648");
    expect(screen.getByTestId("connection-row-ssh:homelab")).not.toHaveTextContent("127.0.0.1");
    expect(screen.getByTestId("connection-remove-ssh:homelab")).toBeInTheDocument();
  });

  it("shows Rust's text inline when switching to an ssh connection fails", async () => {
    const SSH = { id: "ssh:homelab", kind: "ssh" as const, label: "homelab", baseUrl: "ssh://me@homelab.lan:2222?daemonPort=4648" };
    await renderSection({
      listConnections: () => Promise.resolve([LOCAL, SSH]),
      getCurrentConnection: () => Promise.resolve(LOCAL),
      selectConnection: () => Promise.reject("ssh tunnel is not running: Connection refused"),
    });
    fireEvent.click(screen.getByTestId("connection-select-ssh:homelab"));
    await flush();
    expect(screen.getByTestId("connection-list-error")).toHaveTextContent("Connection refused");
    expect(screen.getByTestId("connection-row-ssh:homelab")).toBeInTheDocument();
  });

  describe("Manage this computer's daemon", () => {
  const MANAGED_RUNNING = { ...STATUS, managedState: "managed", reachable: true, localService: "smind" };

  async function renderManage(status: object, settings = SETTINGS) {
    const order: string[] = [];
    const desktop = await renderSection({
      listConnections: () => Promise.resolve([LOCAL]),
      getCurrentConnection: () => Promise.resolve(LOCAL),
      appSettingsGet: () => Promise.resolve(settings),
      appSettingsSet: vi.fn((patch: object) => {
        order.push("appSettingsSet");
        return Promise.resolve({ ...settings, ...patch });
      }),
      daemonStatus: () => Promise.resolve(status),
    });
    for (const m of ["daemonInstall", "daemonUpdate", "daemonRestart", "takeOverDaemon"] as const) {
      (desktop as unknown as Record<string, unknown>)[m] = vi.fn(() => {
        order.push(m);
        return Promise.resolve(status);
      });
    }
    return { desktop, order };
  }

  it("Manage this computer's daemon toggle confirms before disabling while the app owns a running daemon", async () => {
    const { desktop } = await renderManage(MANAGED_RUNNING);
    const toggle = screen.getByTestId("connection-manage-toggle");
    expect(toggle).toHaveAttribute("aria-checked", "true");

    fireEvent.click(toggle);
    await flush();
    expect(screen.getByTestId("connection-manage-confirm")).toBeInTheDocument();
    expect(desktop.appSettingsSet).not.toHaveBeenCalled();
    expect(toggle).toHaveAttribute("aria-checked", "true");

    // Cancel keeps everything as it was.
    fireEvent.click(screen.getByTestId("connection-manage-confirm-cancel"));
    await flush();
    expect(screen.queryByTestId("connection-manage-confirm")).not.toBeInTheDocument();
    expect(desktop.appSettingsSet).not.toHaveBeenCalled();

    fireEvent.click(toggle);
    await flush();
    fireEvent.click(screen.getByTestId("connection-manage-confirm-yes"));
    await flush();
    expect(desktop.appSettingsSet).toHaveBeenCalledWith({ manageLocalDaemon: false });
    expect(screen.getByTestId("connection-manage-toggle")).toHaveAttribute("aria-checked", "false");
  });

  it("persists the setting before stopping", async () => {
    const { desktop, order } = await renderManage(MANAGED_RUNNING);
    fireEvent.click(screen.getByTestId("connection-manage-toggle"));
    await flush();
    fireEvent.click(screen.getByTestId("connection-manage-confirm-yes"));
    await flush();

    // The setting is the first (and only) thing written, and no daemon action ever runs from here.
    expect(order).toEqual(["appSettingsSet"]);
    const d = desktop as unknown as Record<string, { mock: { calls: unknown[] } }>;
    for (const m of ["daemonInstall", "daemonUpdate", "daemonRestart", "takeOverDaemon"]) expect(d[m].mock.calls).toHaveLength(0);
  });

  it("turns off without a confirm when the app does not own a running daemon, and turning on just persists", async () => {
    const { desktop } = await renderManage(STATUS);
    fireEvent.click(screen.getByTestId("connection-manage-toggle"));
    await flush();
    expect(screen.queryByTestId("connection-manage-confirm")).not.toBeInTheDocument();
    expect(desktop.appSettingsSet).toHaveBeenLastCalledWith({ manageLocalDaemon: false });

    fireEvent.click(screen.getByTestId("connection-manage-toggle"));
    await flush();
    expect(desktop.appSettingsSet).toHaveBeenLastCalledWith({ manageLocalDaemon: true });
    expect(screen.getByTestId("connection-manage-toggle")).toHaveAttribute("aria-checked", "true");
  });

  it("hides the toggle where the app cannot manage a daemon at all", async () => {
    await renderManage({ ...STATUS, platform: "unsupported" });
    expect(screen.queryByTestId("connection-manage-toggle")).not.toBeInTheDocument();
  });

  it("Run setup again leaves saved connections untouched", async () => {
    const seen = vi.fn();
    window.addEventListener("smind:run-setup", seen);
    const { desktop } = await renderManage(STATUS);
    const remove = vi.spyOn(desktop, "removeConnection");

    expect(screen.getByTestId("connection-run-setup")).toHaveTextContent("Run setup again");
    fireEvent.click(screen.getByTestId("connection-run-setup"));
    await flush();
    window.removeEventListener("smind:run-setup", seen);

    expect(seen).toHaveBeenCalledTimes(1);
    expect(remove).not.toHaveBeenCalled();
    expect(desktop.selectConnection).not.toHaveBeenCalled();
    expect(desktop.appSettingsSet).not.toHaveBeenCalled();
    expect(screen.getByTestId("connection-row-local")).toBeInTheDocument();
  });

  it("says Finish setup while onboarding is still pending", async () => {
    await renderManage(STATUS, { onboardingCompleted: false, manageLocalDaemon: false });
    expect(screen.getByTestId("connection-run-setup")).toHaveTextContent("Finish setup");
  });
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
      ...baseApi(),
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
    await openAdd("relay");
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
    expect(screen.getByTestId("relay-link-trust")).toHaveTextContent(/pinned fingerprint/i);
    expect(screen.getByTestId("relay-link-confirm").textContent).not.toContain("c2VjcmV0");
    expect(screen.queryByTestId("relay-link-input")).not.toBeInTheDocument();
  });

  it("says a CA-mode relay is verified by a public certificate for its host name", async () => {
    await renderRelay({
      previewRelayLink: vi.fn(() => Promise.resolve({ ...preview(), relayAddress: "relay.example.org:7401", pinned: false })),
    });
    await paste(LINK);

    expect(screen.getByTestId("relay-link-trust")).toHaveTextContent("Verified by a public certificate");
    expect(screen.getByTestId("relay-link-trust")).not.toHaveTextContent(/pinned/i);
    expect(screen.getByTestId("relay-link-trust-detail")).toHaveTextContent("valid for relay.example.org");
    expect(screen.getByTestId("relay-link-trust-detail")).toHaveTextContent("self-signed certificate is refused");
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
    await openAdd("relay");
    expect(screen.getByTestId("relay-link-input")).toHaveValue("");
  });

  it("routes a relay link pasted into the URL form to the relay flow and never saves a URL", async () => {
    const { api } = await renderRelay();
    await openAdd("url");
    fireEvent.change(screen.getByTestId("connection-add-url"), { target: { value: LINK } });
    fireEvent.click(screen.getByTestId("connection-add-submit"));
    await flush();

    expect(api.addConnection).not.toHaveBeenCalled();
    expect(api.previewRelayLink).toHaveBeenCalledWith(LINK);
    expect(screen.getByTestId("connection-relay-handover")).toHaveTextContent(/relay pairing link/i);
    expect(screen.getByTestId("relay-link-confirm")).toBeInTheDocument();
    // The URL form is gone (the link moved to the relay flow, nothing is left in it).
    expect(screen.queryByTestId("connection-add-url")).not.toBeInTheDocument();
    expect(screen.queryByTestId("connection-error")).not.toBeInTheDocument();
  });
});

