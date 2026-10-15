import { act } from "react";
import { cleanup, render, screen, fireEvent, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

const LOCAL = { id: "local", kind: "local" as const, label: "Local", baseUrl: "http://127.0.0.1:4648" };
const TUNNEL = { id: "url:example", kind: "url" as const, label: "Tunnel", baseUrl: "http://example.com:9000" };

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

afterEach(() => {
  vi.resetModules();
  vi.restoreAllMocks();
  vi.doUnmock("@/lib/platform");
});

function mockPlatform(overrides: {
  current: unknown;
  connectionVersion: () => Promise<unknown>;
  daemonStatus?: () => Promise<unknown>;
  daemonUpdate?: () => Promise<unknown>;
}) {
  vi.doMock("@/lib/platform", () => ({
    isDesktop: true,
    desktop: {
      getCurrentConnection: () => Promise.resolve(overrides.current),
      connectionVersion: overrides.connectionVersion,
      daemonStatus: overrides.daemonStatus ?? (() => Promise.resolve({ managedState: "notRunning" })),
      daemonUpdate: overrides.daemonUpdate ?? vi.fn(),
      onDaemonProgress: () => () => {},
    },
  }));
}

describe("DesktopDaemonBanner", () => {
  it("renders nothing when not desktop", async () => {
    vi.doMock("@/lib/platform", () => ({ isDesktop: false, desktop: {} }));
    const { DesktopDaemonBanner } = await import("@/components/desktop-daemon-banner");
    const { container } = render(<DesktopDaemonBanner />);
    await flush();
    expect(container).toBeEmptyDOMElement();
  });

  it("renders nothing when up to date", async () => {
    mockPlatform({
      current: LOCAL,
      connectionVersion: () => Promise.resolve({ reachable: true, daemonVersion: "0.7.0", appVersion: "0.7.0", comparison: "same" }),
    });
    const { DesktopDaemonBanner } = await import("@/components/desktop-daemon-banner");
    const { container } = render(<DesktopDaemonBanner />);
    await flush();
    expect(container).toBeEmptyDOMElement();
  });

  it("shows an actionable banner for the managed local connection", async () => {
    mockPlatform({
      current: LOCAL,
      connectionVersion: () => Promise.resolve({ reachable: true, daemonVersion: "0.6.0", appVersion: "0.7.0", comparison: "older" }),
      daemonStatus: () => Promise.resolve({ managedState: "managed", comparison: "older" }),
    });
    const { DesktopDaemonBanner } = await import("@/components/desktop-daemon-banner");
    render(<DesktopDaemonBanner />);
    await waitFor(() => expect(screen.getByTestId("desktop-daemon-banner")).toBeInTheDocument());
    expect(screen.getByTestId("desktop-daemon-banner")).toHaveTextContent("Daemon v0.6.0 is older than this app (v0.7.0)");
    expect(screen.getByTestId("desktop-daemon-banner-update")).toBeInTheDocument();
  });

  it("shows a plain notice, no button, for local-but-unmanaged", async () => {
    mockPlatform({
      current: LOCAL,
      connectionVersion: () => Promise.resolve({ reachable: true, daemonVersion: "0.6.0", appVersion: "0.7.0", comparison: "older" }),
      daemonStatus: () => Promise.resolve({ managedState: "unmanaged", comparison: "older" }),
    });
    const { DesktopDaemonBanner } = await import("@/components/desktop-daemon-banner");
    render(<DesktopDaemonBanner />);
    await waitFor(() => expect(screen.getByTestId("desktop-daemon-banner")).toBeInTheDocument());
    expect(screen.queryByTestId("desktop-daemon-banner-update")).not.toBeInTheDocument();
  });

  it("shows a plain notice, no button, for a url connection", async () => {
    mockPlatform({
      current: TUNNEL,
      connectionVersion: () => Promise.resolve({ reachable: true, daemonVersion: "0.6.0", appVersion: "0.7.0", comparison: "older" }),
    });
    const { DesktopDaemonBanner } = await import("@/components/desktop-daemon-banner");
    render(<DesktopDaemonBanner />);
    await waitFor(() => expect(screen.getByTestId("desktop-daemon-banner")).toBeInTheDocument());
    expect(screen.queryByTestId("desktop-daemon-banner-update")).not.toBeInTheDocument();
  });

  it("fetches connectionVersion/daemonStatus exactly once on mount, never polling", async () => {
    // Same reasoning as daemon-section's equivalent test: this banner is
    // mounted for the whole app session, so it must not re-poll on a
    // timer -- each daemon_status/connection_version call can spawn a
    // console-flashing child process on Windows.
    const connectionVersion = vi.fn(() => Promise.resolve({ reachable: true, daemonVersion: "0.6.0", appVersion: "0.7.0", comparison: "older" }));
    const daemonStatus = vi.fn(() => Promise.resolve({ managedState: "managed", comparison: "older" }));
    mockPlatform({ current: LOCAL, connectionVersion, daemonStatus });
    const { DesktopDaemonBanner } = await import("@/components/desktop-daemon-banner");
    render(<DesktopDaemonBanner />);
    await waitFor(() => expect(screen.getByTestId("desktop-daemon-banner")).toBeInTheDocument());
    await flush();
    await flush();
    expect(connectionVersion).toHaveBeenCalledTimes(1);
    expect(daemonStatus).toHaveBeenCalledTimes(1);
  });

  describe("M5 — update when idle", () => {
    async function mount(props: { runsLoaded?: boolean; runningRuns?: number } = {}) {
      const daemonUpdate = vi.fn(() => Promise.resolve({ managedState: "managed" }));
      mockPlatform({
        current: LOCAL,
        connectionVersion: () => Promise.resolve({ reachable: true, daemonVersion: "0.6.0", appVersion: "0.7.0", comparison: "older" }),
        daemonStatus: () => Promise.resolve({ managedState: "managed", comparison: "older" }),
        daemonUpdate,
      });
      const { DesktopDaemonBanner } = await import("@/components/desktop-daemon-banner");
      render(<DesktopDaemonBanner runsLoaded={props.runsLoaded ?? true} runningRuns={props.runningRuns ?? 0} />);
      await waitFor(() => expect(screen.getByTestId("desktop-daemon-banner")).toBeInTheDocument());
      await flush();
      return daemonUpdate;
    }

    it("daemon-update-not-before-runs-loaded", async () => {
      const daemonUpdate = await mount({ runsLoaded: false, runningRuns: 0 });
      expect(daemonUpdate).not.toHaveBeenCalled();
    });

    it("daemon-update-auto-when-idle", async () => {
      const daemonUpdate = await mount({ runsLoaded: true, runningRuns: 0 });
      expect(daemonUpdate).toHaveBeenCalledTimes(1);
      // A re-render doesn't call it again (once per key per session).
      await act(async () => {
        await Promise.resolve();
      });
      expect(daemonUpdate).toHaveBeenCalledTimes(1);
    });

    it("daemon-update-deferred-while-running", async () => {
      const daemonUpdate = await mount({ runsLoaded: true, runningRuns: 2 });
      expect(daemonUpdate).not.toHaveBeenCalled();
      const banner = screen.getByTestId("desktop-daemon-banner");
      expect(banner).toHaveTextContent("2 agents running");
      expect(banner).toHaveTextContent("when they finish");
    });

    it("daemon-update-fires-when-last-run-finishes", async () => {
      const daemonUpdate = vi.fn(() => Promise.resolve({ managedState: "managed" }));
      mockPlatform({
        current: LOCAL,
        connectionVersion: () => Promise.resolve({ reachable: true, daemonVersion: "0.6.0", appVersion: "0.7.0", comparison: "older" }),
        daemonStatus: () => Promise.resolve({ managedState: "managed", comparison: "older" }),
        daemonUpdate,
      });
      const { DesktopDaemonBanner } = await import("@/components/desktop-daemon-banner");
      const view = render(<DesktopDaemonBanner runsLoaded={true} runningRuns={2} />);
      await waitFor(() => expect(screen.getByTestId("desktop-daemon-banner")).toBeInTheDocument());
      await flush();
      expect(daemonUpdate).not.toHaveBeenCalled();

      view.rerender(<DesktopDaemonBanner runsLoaded={true} runningRuns={1} />);
      await flush();
      expect(daemonUpdate).not.toHaveBeenCalled();

      view.rerender(<DesktopDaemonBanner runsLoaded={true} runningRuns={0} />);
      await flush();
      expect(daemonUpdate).toHaveBeenCalledTimes(1);
    });

    it("daemon-update-now-requires-confirm", async () => {
      const daemonUpdate = await mount({ runsLoaded: true, runningRuns: 1 });
      expect(daemonUpdate).not.toHaveBeenCalled();

      fireEvent.click(screen.getByTestId("desktop-daemon-banner-update-now"));
      await flush();
      expect(screen.getByTestId("desktop-daemon-banner-confirm")).toHaveTextContent("Running agent work will be interrupted");
      expect(daemonUpdate).not.toHaveBeenCalled();

      fireEvent.click(screen.getByText("Cancel"));
      await flush();
      expect(daemonUpdate).not.toHaveBeenCalled();

      fireEvent.click(screen.getByTestId("desktop-daemon-banner-update-now"));
      await flush();
      fireEvent.click(screen.getByTestId("desktop-daemon-banner-confirm-update"));
      await flush();
      expect(daemonUpdate).toHaveBeenCalledTimes(1);
    });

    it("daemon-update-not-now-disarms", async () => {
      const daemonUpdate = vi.fn(() => Promise.resolve({ managedState: "managed" }));
      mockPlatform({
        current: LOCAL,
        connectionVersion: () => Promise.resolve({ reachable: true, daemonVersion: "0.6.0", appVersion: "0.7.0", comparison: "older" }),
        daemonStatus: () => Promise.resolve({ managedState: "managed", comparison: "older" }),
        daemonUpdate,
      });
      const { DesktopDaemonBanner } = await import("@/components/desktop-daemon-banner");
      const view = render(<DesktopDaemonBanner runsLoaded={true} runningRuns={2} />);
      await waitFor(() => expect(screen.getByTestId("desktop-daemon-banner")).toBeInTheDocument());
      await flush();

      fireEvent.click(screen.getByTestId("desktop-daemon-banner-not-now"));
      await flush();
      expect(screen.getByTestId("desktop-daemon-banner-update")).toBeInTheDocument();

      // The count drops to 0: no automatic call, the manual button stays.
      view.rerender(<DesktopDaemonBanner runsLoaded={true} runningRuns={0} />);
      await flush();
      expect(daemonUpdate).not.toHaveBeenCalled();
      expect(screen.getByTestId("desktop-daemon-banner-update")).toBeInTheDocument();
    });

    it("daemon-update-failure-no-loop", async () => {
      const daemonUpdate = vi.fn(() => Promise.reject(new Error("disk full")));
      mockPlatform({
        current: LOCAL,
        connectionVersion: () => Promise.resolve({ reachable: true, daemonVersion: "0.6.0", appVersion: "0.7.0", comparison: "older" }),
        daemonStatus: () => Promise.resolve({ managedState: "managed", comparison: "older" }),
        daemonUpdate,
      });
      const { DesktopDaemonBanner } = await import("@/components/desktop-daemon-banner");
      render(<DesktopDaemonBanner runsLoaded={true} runningRuns={0} />);
      await waitFor(() => expect(daemonUpdate).toHaveBeenCalledTimes(1));
      await flush();
      await flush();
      // No automatic second call.
      expect(daemonUpdate).toHaveBeenCalledTimes(1);
      expect(screen.getByTestId("desktop-daemon-banner-error")).toHaveTextContent("disk full");

      fireEvent.click(screen.getByTestId("desktop-daemon-banner-retry"));
      await flush();
      expect(daemonUpdate).toHaveBeenCalledTimes(2);
    });

    it("daemon-update-unmanaged-untouched", async () => {
      const unmanagedUpdate = vi.fn();
      mockPlatform({
        current: LOCAL,
        connectionVersion: () => Promise.resolve({ reachable: true, daemonVersion: "0.6.0", appVersion: "0.7.0", comparison: "older" }),
        daemonStatus: () => Promise.resolve({ managedState: "unmanaged", comparison: "older" }),
        daemonUpdate: unmanagedUpdate,
      });
      const { DesktopDaemonBanner } = await import("@/components/desktop-daemon-banner");
      render(<DesktopDaemonBanner runsLoaded={true} runningRuns={0} />);
      await waitFor(() => expect(screen.getByTestId("desktop-daemon-banner")).toBeInTheDocument());
      await flush();

      const urlUpdate = vi.fn();
      mockPlatform({
        current: TUNNEL,
        connectionVersion: () => Promise.resolve({ reachable: true, daemonVersion: "0.6.0", appVersion: "0.7.0", comparison: "older" }),
        daemonUpdate: urlUpdate,
      });
      const { DesktopDaemonBanner: Banner2 } = await import("@/components/desktop-daemon-banner");
      render(<Banner2 runsLoaded={true} runningRuns={0} />);
      await waitFor(() => expect(screen.getAllByTestId("desktop-daemon-banner")).toHaveLength(2));
      await flush();

      expect(unmanagedUpdate).not.toHaveBeenCalled();
      expect(urlUpdate).not.toHaveBeenCalled();
      for (const b of screen.getAllByTestId("desktop-daemon-banner")) {
        expect(within(b).queryByTestId("desktop-daemon-banner-update")).not.toBeInTheDocument();
      }
    });
  });

  it("Update & restart calls daemonUpdate", async () => {
    const daemonUpdate = vi.fn(() => Promise.resolve({ managedState: "managed" }));
    mockPlatform({
      current: LOCAL,
      connectionVersion: () => Promise.resolve({ reachable: true, daemonVersion: "0.6.0", appVersion: "0.7.0", comparison: "older" }),
      daemonStatus: () => Promise.resolve({ managedState: "managed", comparison: "older" }),
      daemonUpdate,
    });
    const { DesktopDaemonBanner } = await import("@/components/desktop-daemon-banner");
    render(<DesktopDaemonBanner />);
    await waitFor(() => expect(screen.getByTestId("desktop-daemon-banner-update")).toBeInTheDocument());

    // runsLoaded defaults to false: run state unknown, so it asks first.
    fireEvent.click(screen.getByTestId("desktop-daemon-banner-update"));
    await flush();
    expect(daemonUpdate).not.toHaveBeenCalled();
    expect(screen.getByTestId("desktop-daemon-banner-confirm")).toHaveTextContent(/may be running/);
    fireEvent.click(screen.getByTestId("desktop-daemon-banner-confirm-update"));
    await flush();

    expect(daemonUpdate).toHaveBeenCalled();
  });

  it("daemon-update-manual-confirms-while-runs-unknown", async () => {
    const daemonUpdate = vi.fn(() => Promise.resolve({ managedState: "managed" }));
    mockPlatform({
      current: LOCAL,
      connectionVersion: () => Promise.resolve({ reachable: true, daemonVersion: "0.6.0", appVersion: "0.7.0", comparison: "older" }),
      daemonStatus: () => Promise.resolve({ managedState: "managed", comparison: "older" }),
      daemonUpdate,
    });
    const { DesktopDaemonBanner } = await import("@/components/desktop-daemon-banner");
    render(<DesktopDaemonBanner runsLoaded={false} runningRuns={0} />);
    await waitFor(() => expect(screen.getByTestId("desktop-daemon-banner-update")).toBeInTheDocument());
    fireEvent.click(screen.getByTestId("desktop-daemon-banner-update"));
    await flush();
    expect(daemonUpdate).not.toHaveBeenCalled();
    expect(screen.getByTestId("desktop-daemon-banner-confirm")).toBeInTheDocument();
  });
});

describe("ADR-0031 P10 — the older-daemon notice for daemons the app does not manage", () => {
  const RELAY = { id: "relay:homelab@relay.example.test:7400", kind: "relay" as const, label: "Relay (homelab)", baseUrl: "relay://homelab@relay.example.test:7400" };
  const older = (daemonVersion: string, extra: Record<string, unknown> = {}) => () =>
    Promise.resolve({ reachable: true, daemonVersion, appVersion: "0.12.0", comparison: "older", ...extra });
  const relayProbe = () => Promise.resolve({ reachable: false, daemonVersion: null, appVersion: "0.12.0", comparison: "unknown" });

  afterEach(() => {
    window.localStorage.clear();
  });

  async function mount(client: { call: ReturnType<typeof vi.fn> } | null = null) {
    const { DesktopDaemonBanner } = await import("@/components/desktop-daemon-banner");
    // The client prop is a WsClient; only `call` is used.
    render(<DesktopDaemonBanner client={client as never} />);
  }
  const commandTexts = () => screen.queryAllByTestId("desktop-daemon-banner-command").map((el) => el.textContent);

  it("shows smind update with a Copy button for a url connection whose daemon has the command", async () => {
    mockPlatform({ current: TUNNEL, connectionVersion: older("0.12.0") });
    await mount();
    await waitFor(() => expect(screen.getByTestId("desktop-daemon-banner")).toBeInTheDocument());
    expect(commandTexts()).toEqual(["smind update"]);
    expect(screen.getByTestId("desktop-daemon-banner-copy")).toHaveTextContent("Copy");
    expect(screen.queryByTestId("desktop-daemon-banner-update")).not.toBeInTheDocument();
  });

  it("shows the installer one-liners for a daemon older than the update command", async () => {
    mockPlatform({ current: TUNNEL, connectionVersion: older("0.11.1") });
    await mount();
    await waitFor(() => expect(screen.getByTestId("desktop-daemon-banner")).toBeInTheDocument());
    expect(commandTexts()).toEqual(["curl -fsSL https://spacingmind.com/install.sh | sh", "irm https://spacingmind.com/install.ps1 | iex"]);
    expect(screen.getAllByTestId("desktop-daemon-banner-copy")).toHaveLength(2);
  });

  it("copies exactly the displayed command to the clipboard and confirms without moving focus", async () => {
    const writeText = vi.fn(() => Promise.resolve());
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    mockPlatform({ current: TUNNEL, connectionVersion: older("0.11.1") });
    await mount();
    await waitFor(() => expect(screen.getByTestId("desktop-daemon-banner")).toBeInTheDocument());

    const [shCopy, psCopy] = screen.getAllByTestId("desktop-daemon-banner-copy");
    fireEvent.click(psCopy);
    await flush();
    expect(writeText).toHaveBeenCalledTimes(1);
    expect(writeText).toHaveBeenCalledWith("irm https://spacingmind.com/install.ps1 | iex");
    expect(psCopy).toHaveTextContent("Copied");
    expect(shCopy).toHaveTextContent("Copy");
    expect(shCopy).not.toHaveTextContent("Copied");
    // The button confirms in place: nothing else grabbed focus.
    expect(document.activeElement === document.body || document.activeElement === psCopy).toBe(true);
  });

  it("shows the notice for a relay connection using daemon.info", async () => {
    const call = vi.fn(() => Promise.resolve({ version: "0.11.1", commit: "abc1234", managedBy: "systemd" }));
    mockPlatform({ current: RELAY, connectionVersion: relayProbe });
    await mount({ call });
    await waitFor(() => expect(screen.getByTestId("desktop-daemon-banner")).toBeInTheDocument());
    expect(call).toHaveBeenCalledWith("daemon.info");
    const banner = screen.getByTestId("desktop-daemon-banner");
    expect(banner).toHaveTextContent("Daemon v0.11.1 is older than this app (v0.12.0)");
    expect(banner).toHaveTextContent("managed by systemd");
    expect(commandTexts()).toHaveLength(2); // 0.11.1 predates smind update
  });

  it("stays quiet for a relay daemon that reports the app's version or newer", async () => {
    for (const version of ["0.12.0", "0.13.0"]) {
      vi.resetModules();
      mockPlatform({ current: RELAY, connectionVersion: relayProbe });
      const call = vi.fn(() => Promise.resolve({ version }));
      const { DesktopDaemonBanner } = await import("@/components/desktop-daemon-banner");
      const { container, unmount } = render(<DesktopDaemonBanner client={{ call } as never} />);
      await flush();
      await flush();
      expect(container).toBeEmptyDOMElement();
      unmount();
    }
  });

  it("shows version unknown plus installer commands when a relay daemon lacks daemon.info", async () => {
    const call = vi.fn(() => Promise.reject(new Error("method not found: daemon.info")));
    mockPlatform({ current: RELAY, connectionVersion: relayProbe });
    await mount({ call });
    await waitFor(() => expect(screen.getByTestId("desktop-daemon-banner")).toBeInTheDocument());
    const banner = screen.getByTestId("desktop-daemon-banner");
    expect(banner).toHaveTextContent("Daemon version unknown");
    expect(banner).not.toHaveTextContent(/up to date/i);
    expect(commandTexts()).toEqual(["curl -fsSL https://spacingmind.com/install.sh | sh", "irm https://spacingmind.com/install.ps1 | iex"]);
  });

  it("says nothing about a relay connection whose daemon.info failed for another reason", async () => {
    const call = vi.fn(() => Promise.reject(new Error("connection closed")));
    mockPlatform({ current: RELAY, connectionVersion: relayProbe });
    const { DesktopDaemonBanner } = await import("@/components/desktop-daemon-banner");
    const { container } = render(<DesktopDaemonBanner client={{ call } as never} />);
    await flush();
    await flush();
    expect(container).toBeEmptyDOMElement();
  });

  it("keeps Update & restart for the managed local daemon and shows no copy block", async () => {
    mockPlatform({
      current: LOCAL,
      connectionVersion: older("0.11.1"),
      daemonStatus: () => Promise.resolve({ managedState: "managed", comparison: "older" }),
    });
    await mount();
    await waitFor(() => expect(screen.getByTestId("desktop-daemon-banner-update")).toBeInTheDocument());
    expect(commandTexts()).toEqual([]);
    expect(screen.queryByTestId("desktop-daemon-banner-copy")).not.toBeInTheDocument();
    expect(screen.queryByTestId("desktop-daemon-banner-dismiss")).not.toBeInTheDocument();
  });

  it("shows managed-by-service with the copy block for a service-managed local daemon", async () => {
    mockPlatform({
      current: LOCAL,
      connectionVersion: older("0.12.0", { managedBy: "launchd" }),
      daemonStatus: () => Promise.resolve({ managedState: "unmanaged", managedBy: "launchd", comparison: "older" }),
    });
    await mount();
    await waitFor(() => expect(screen.getByTestId("desktop-daemon-banner")).toBeInTheDocument());
    expect(screen.getByTestId("desktop-daemon-banner")).toHaveTextContent("managed by launchd");
    expect(commandTexts()).toEqual(["smind update"]);
    expect(screen.queryByTestId("desktop-daemon-banner-update")).not.toBeInTheDocument();
  });

  it("is dismissable per version: a dismissed notice stays gone, a new daemon version brings it back", async () => {
    mockPlatform({ current: TUNNEL, connectionVersion: older("0.11.1") });
    await mount();
    await waitFor(() => expect(screen.getByTestId("desktop-daemon-banner")).toBeInTheDocument());
    fireEvent.click(screen.getByTestId("desktop-daemon-banner-dismiss"));
    await waitFor(() => expect(screen.queryByTestId("desktop-daemon-banner")).not.toBeInTheDocument());

    // A fresh mount (next app start): still dismissed for 0.11.1 -> 0.12.0 ...
    cleanup();
    vi.resetModules();
    mockPlatform({ current: TUNNEL, connectionVersion: older("0.11.1") });
    await mount();
    await flush();
    await flush();
    expect(screen.queryByTestId("desktop-daemon-banner")).not.toBeInTheDocument();

    // ... but a different daemon version is a new notice.
    cleanup();
    vi.resetModules();
    mockPlatform({ current: TUNNEL, connectionVersion: older("0.11.2") });
    await mount();
    await waitFor(() => expect(screen.getByTestId("desktop-daemon-banner")).toBeInTheDocument());
  });
});
