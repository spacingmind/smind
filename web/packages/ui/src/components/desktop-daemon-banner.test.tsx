import { act } from "react";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
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

    fireEvent.click(screen.getByTestId("desktop-daemon-banner-update"));
    await flush();

    expect(daemonUpdate).toHaveBeenCalled();
  });
});
