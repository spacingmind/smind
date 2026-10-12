import { act } from "react";
import { render, screen, fireEvent } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

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

function baseStatus(overrides: Partial<Record<string, unknown>> = {}) {
  return {
    platform: "macos",
    reachable: true,
    daemonVersion: "0.7.0",
    appVersion: "0.7.0",
    comparison: "same",
    managedState: "managed",
    pid: 123,
    installedPath: "/tmp/smind/bin/smind",
    logPath: "/tmp/smind/smind.log",
    binaryInstalled: true,
    wslDaemonSuspected: false,
    gitFound: null,
    prerequisiteMessage: null,
    ...overrides,
  };
}

async function renderSection(desktopApi: Record<string, unknown>) {
  vi.doMock("@/lib/platform", () => ({
    isDesktop: true,
    desktop: {
      onDaemonProgress: () => () => {},
      daemonInstall: vi.fn(),
      daemonUpdate: vi.fn(),
      daemonRestart: vi.fn(),
      takeOverDaemon: vi.fn(),
      ...desktopApi,
    },
  }));
  const { listSettingsSections } = await import("@/components/settings/settings-registry");
  await import("@/components/settings/daemon-section");
  const section = listSettingsSections().find((s) => s.id === "daemon");
  if (!section) throw new Error("daemon section did not register");
  render(<>{section.render({ client: null })}</>);
  await flush();
  const platform = await import("@/lib/platform");
  return platform.desktop;
}

describe("daemon-section registration", () => {
  it("does not register in a non-desktop build", async () => {
    vi.doMock("@/lib/platform", () => ({ isDesktop: false, desktop: {} }));
    const { listSettingsSections } = await import("@/components/settings/settings-registry");
    await import("@/components/settings/daemon-section");
    expect(listSettingsSections().some((s) => s.id === "daemon")).toBe(false);
  });
});

describe("daemon-section (desktop build)", () => {
  it("shows status/version/managed and offers Restart when managed and up to date", async () => {
    await renderSection({ daemonStatus: () => Promise.resolve(baseStatus()) });

    expect(screen.getByTestId("daemon-reachable")).toHaveTextContent("Running");
    expect(screen.getByTestId("daemon-version")).toHaveTextContent("0.7.0");
    expect(screen.getByTestId("daemon-managed-state")).toHaveTextContent("Managed by this app");
    expect(screen.getByTestId("daemon-log-path")).toHaveTextContent("/tmp/smind/smind.log");
    expect(screen.getByTestId("daemon-restart")).toBeInTheDocument();
    expect(screen.queryByTestId("daemon-update")).not.toBeInTheDocument();
    expect(screen.queryByTestId("daemon-install")).not.toBeInTheDocument();
  });

  it("offers Update & restart when managed and older", async () => {
    await renderSection({ daemonStatus: () => Promise.resolve(baseStatus({ comparison: "older", daemonVersion: "0.6.0" })) });
    expect(screen.getByTestId("daemon-update")).toBeInTheDocument();
    expect(screen.getByTestId("daemon-restart")).toBeInTheDocument();
  });

  it("disables Restart (but not Update) after a bare take-over with no managed binary installed", async () => {
    // The exact scenario the binary_installed precondition guards
    // against: take-over records the adopted pid as "managed" before any
    // Install/Update ever ran, so there is no managed binary on disk yet.
    const daemonRestart = vi.fn();
    await renderSection({
      daemonStatus: () => Promise.resolve(baseStatus({ binaryInstalled: false, comparison: "older", daemonVersion: "0.6.0" })),
      daemonRestart,
    });

    const restart = screen.getByTestId("daemon-restart");
    expect(restart).toBeInTheDocument();
    expect(restart).toBeDisabled();
    expect(screen.getByTestId("daemon-update")).not.toBeDisabled();

    fireEvent.click(restart);
    await flush();
    expect(daemonRestart).not.toHaveBeenCalled();
  });

  it("offers Install when not running", async () => {
    await renderSection({ daemonStatus: () => Promise.resolve(baseStatus({ managedState: "notRunning", reachable: false, daemonVersion: null })) });
    expect(screen.getByTestId("daemon-reachable")).toHaveTextContent("Not running");
    expect(screen.getByTestId("daemon-install")).toBeInTheDocument();
    expect(screen.queryByTestId("daemon-restart")).not.toBeInTheDocument();
  });

  it("offers take-over with a confirmation step when unmanaged, and never restart/install", async () => {
    const takeOverDaemon = vi.fn(() => Promise.resolve(baseStatus()));
    await renderSection({ daemonStatus: () => Promise.resolve(baseStatus({ managedState: "unmanaged" })), takeOverDaemon });

    expect(screen.getByTestId("daemon-managed-state")).toHaveTextContent("not managed by the app");
    expect(screen.queryByTestId("daemon-restart")).not.toBeInTheDocument();
    expect(screen.queryByTestId("daemon-install")).not.toBeInTheDocument();
    expect(screen.getByTestId("daemon-take-over")).toBeInTheDocument();
    expect(screen.queryByTestId("daemon-take-over-confirm")).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId("daemon-take-over"));
    await flush();
    expect(screen.getByTestId("daemon-take-over-confirm")).toBeInTheDocument();
    expect(takeOverDaemon).not.toHaveBeenCalled();

    fireEvent.click(screen.getByTestId("daemon-take-over-confirm"));
    await flush();
    expect(takeOverDaemon).toHaveBeenCalled();
  });

  it("cancelling take-over confirmation never calls takeOverDaemon", async () => {
    const takeOverDaemon = vi.fn();
    await renderSection({ daemonStatus: () => Promise.resolve(baseStatus({ managedState: "unmanaged" })), takeOverDaemon });

    fireEvent.click(screen.getByTestId("daemon-take-over"));
    await flush();
    fireEvent.click(screen.getByTestId("daemon-take-over-cancel"));
    await flush();

    expect(screen.getByTestId("daemon-take-over")).toBeInTheDocument();
    expect(takeOverDaemon).not.toHaveBeenCalled();
  });

  it("shows the unsupported message and no action buttons on an unsupported platform", async () => {
    await renderSection({ daemonStatus: () => Promise.resolve(baseStatus({ platform: "unsupported", managedState: "notRunning", reachable: false })) });

    const unsupported = screen.getByTestId("daemon-unsupported");
    expect(unsupported).toBeInTheDocument();
    // Unsupported now means "not Windows and not macOS" -- the message
    // must not point at WSL2 any more.
    expect(unsupported).toHaveTextContent("Windows and macOS only");
    expect(unsupported.textContent).not.toMatch(/WSL2/);
    expect(screen.queryByTestId("daemon-install")).not.toBeInTheDocument();
    expect(screen.queryByTestId("daemon-restart")).not.toBeInTheDocument();
  });

  it("shows Windows paths as returned and the prerequisite notice when git is missing", async () => {
    await renderSection({
      daemonStatus: () =>
        Promise.resolve(
          baseStatus({
            platform: "windows",
            installedPath: "C:\\Users\\test\\AppData\\Local\\smind\\bin\\smind.exe",
            logPath: "C:\\Users\\test\\AppData\\Local\\smind\\smind.log",
            gitFound: false,
            prerequisiteMessage: "Git for Windows was not found. Worktrees need git, and setup scripts need its sh. Install Git for Windows and restart smind.",
          }),
        ),
    });

    expect(screen.getByTestId("daemon-log-path")).toHaveTextContent("C:\\Users\\test\\AppData\\Local\\smind\\smind.log");
    expect(screen.getByTestId("daemon-prerequisite")).toHaveTextContent("Git for Windows was not found");
    expect(screen.queryByTestId("daemon-wsl-notice")).not.toBeInTheDocument();
  });

  it("hides take over and explains the situation when a WSL daemon is suspected", async () => {
    const takeOverDaemon = vi.fn();
    await renderSection({
      daemonStatus: () => Promise.resolve(baseStatus({ platform: "windows", managedState: "unmanaged", wslDaemonSuspected: true })),
      takeOverDaemon,
    });

    const notice = screen.getByTestId("daemon-wsl-notice");
    expect(notice).toHaveTextContent("A daemon that this app did not start is answering on this port");
    expect(notice).toHaveTextContent("not migrated");
    expect(screen.queryByTestId("daemon-take-over")).not.toBeInTheDocument();
    expect(screen.queryByTestId("daemon-take-over-confirm")).not.toBeInTheDocument();
    expect(takeOverDaemon).not.toHaveBeenCalled();
  });

  it("still offers take over for an unmanaged native smind.exe on Windows", async () => {
    await renderSection({
      daemonStatus: () => Promise.resolve(baseStatus({ platform: "windows", managedState: "unmanaged", wslDaemonSuspected: false })),
    });

    expect(screen.getByTestId("daemon-take-over")).toBeInTheDocument();
  });

  it("a progress event updates the shown stage text while an action is busy", async () => {
    const state: { progressCb: ((p: { stage: string; message: string }) => void) | undefined; resolveInstall: (() => void) | undefined } = {
      progressCb: undefined,
      resolveInstall: undefined,
    };
    vi.doMock("@/lib/platform", () => ({
      isDesktop: true,
      desktop: {
        daemonStatus: () => Promise.resolve(baseStatus({ managedState: "notRunning", reachable: false })),
        onDaemonProgress: (cb: (p: { stage: string; message: string }) => void) => {
          state.progressCb = cb;
          return () => {};
        },
        daemonInstall: () => new Promise<unknown>((resolve) => { state.resolveInstall = () => resolve(baseStatus()); }),
      },
    }));
    const { listSettingsSections } = await import("@/components/settings/settings-registry");
    await import("@/components/settings/daemon-section");
    const section = listSettingsSections().find((s) => s.id === "daemon");
    render(<>{section?.render({ client: null })}</>);
    await flush();

    fireEvent.click(screen.getByTestId("daemon-install"));
    await flush();
    state.progressCb?.({ stage: "downloading", message: "Downloading the daemon release…" });
    await flush();

    expect(screen.getByTestId("daemon-progress")).toHaveTextContent("Downloading the daemon release…");
    state.resolveInstall?.();
    await flush();
  });

  it("fetches daemon_status exactly once on mount, never polling", async () => {
    // Each daemon_status call probes the daemon and the port owner
    // (daemon_manager.rs), so a polling loop here would keep churning;
    // Settings sections mount once and stay mounted, so extra time
    // passing (simulated by repeated flushes) must never trigger a
    // second fetch.
    const daemonStatus = vi.fn(() => Promise.resolve(baseStatus()));
    await renderSection({ daemonStatus });
    await flush();
    await flush();
    await flush();
    expect(daemonStatus).toHaveBeenCalledTimes(1);
  });

  it("surfaces an install error verbatim", async () => {
    await renderSection({
      daemonStatus: () => Promise.resolve(baseStatus({ managedState: "notRunning", reachable: false })),
      daemonInstall: () => Promise.reject(new Error("no published release for version")),
    });

    fireEvent.click(screen.getByTestId("daemon-install"));
    await flush();

    expect(screen.getByTestId("daemon-error")).toHaveTextContent("no published release for version");
  });

  it("re-reads the live status after a failed restart instead of keeping the stale Running state", async () => {
    const daemonStatus = vi
      .fn()
      .mockResolvedValueOnce(baseStatus())
      .mockResolvedValue(baseStatus({ reachable: false, managedState: "notRunning", pid: null }));
    await renderSection({ daemonStatus, daemonRestart: vi.fn().mockRejectedValue(new Error("daemon failed to start after retry: boom")) });
    expect(screen.getByTestId("daemon-reachable")).toHaveTextContent("Running");

    fireEvent.click(screen.getByTestId("daemon-restart"));
    await flush();
    await flush();

    expect(daemonStatus).toHaveBeenCalledTimes(2);
    expect(screen.getByTestId("daemon-reachable")).toHaveTextContent("Not running");
  });
});
