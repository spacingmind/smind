import { afterEach, describe, expect, it, vi } from "vitest";

// platform.ts reads import.meta.env.VITE_SMIND_DESKTOP into a top-level
// const at module load, so each scenario stubs the env var and then
// re-imports the module fresh (vi.resetModules) rather than mutating a
// single already-loaded instance.
afterEach(() => {
  vi.unstubAllEnvs();
  vi.resetModules();
  vi.restoreAllMocks();
  vi.doUnmock("@tauri-apps/api/core");
  vi.doUnmock("@tauri-apps/api/event");
});

describe("platform in a non-desktop build", () => {
  it("isDesktop is false and every desktop method rejects", async () => {
    vi.stubEnv("VITE_SMIND_DESKTOP", "");
    const { isDesktop, desktop } = await import("@/lib/platform");

    expect(isDesktop).toBe(false);
    await expect(desktop.listConnections()).rejects.toThrow(/unavailable/i);
    await expect(desktop.getCurrentConnection()).rejects.toThrow(/unavailable/i);
    await expect(desktop.addConnection("x", "http://example.com")).rejects.toThrow(/unavailable/i);
    await expect(desktop.removeConnection("id")).rejects.toThrow(/unavailable/i);
    await expect(desktop.selectConnection("id")).rejects.toThrow(/unavailable/i);
    await expect(desktop.openExternal("https://example.com")).rejects.toThrow(/unavailable/i);
    await expect(desktop.daemonStatus()).rejects.toThrow(/unavailable/i);
    await expect(desktop.daemonInstall()).rejects.toThrow(/unavailable/i);
    await expect(desktop.daemonUpdate()).rejects.toThrow(/unavailable/i);
    await expect(desktop.daemonRestart()).rejects.toThrow(/unavailable/i);
    await expect(desktop.takeOverDaemon()).rejects.toThrow(/unavailable/i);
    await expect(desktop.connectionVersion("local")).rejects.toThrow(/unavailable/i);
    expect(() => desktop.onDaemonProgress(() => {})()).not.toThrow();
  });
});

describe("platform in a desktop build", () => {
  it("isDesktop is true and each method calls the matching tauri command", async () => {
    vi.stubEnv("VITE_SMIND_DESKTOP", "1");
    const invoke = vi.fn(async (cmd: string, args?: Record<string, unknown>) => {
      switch (cmd) {
        case "connections_list":
          return [{ id: "local", kind: "local", label: "Local", baseUrl: "http://127.0.0.1:4648" }];
        case "connections_add":
          return { id: "url:example", kind: "url", label: args?.label, baseUrl: args?.url };
        case "connections_remove":
          return undefined;
        case "connections_select":
          return { id: args?.id, kind: "url", label: "Tunnel", baseUrl: "http://example.com:9000" };
        case "connections_get_current":
          return { id: "local", kind: "local", label: "Local", baseUrl: "http://127.0.0.1:4648" };
        case "open_external":
          return undefined;
        case "daemon_status":
        case "daemon_install":
        case "daemon_update":
        case "daemon_restart":
        case "take_over_daemon":
          return { platform: "macos", reachable: true, daemonVersion: "0.7.0", appVersion: "0.7.0", comparison: "same", managedState: "managed", pid: 123, installedPath: "/tmp/smind", logPath: "/tmp/smind.log" };
        case "connection_version":
          return { reachable: true, daemonVersion: "0.6.0", appVersion: "0.7.0", comparison: "older" };
        default:
          throw new Error(`unexpected invoke command ${cmd}`);
      }
    });
    vi.doMock("@tauri-apps/api/core", () => ({ invoke }));

    const { isDesktop, desktop } = await import("@/lib/platform");
    expect(isDesktop).toBe(true);

    const list = await desktop.listConnections();
    expect(list).toEqual([{ id: "local", kind: "local", label: "Local", baseUrl: "http://127.0.0.1:4648" }]);
    expect(invoke).toHaveBeenCalledWith("connections_list");

    const added = await desktop.addConnection("Tunnel", "http://example.com:9000");
    expect(added).toEqual({ id: "url:example", kind: "url", label: "Tunnel", baseUrl: "http://example.com:9000" });
    expect(invoke).toHaveBeenCalledWith("connections_add", { label: "Tunnel", url: "http://example.com:9000" });

    await desktop.removeConnection("url:example");
    expect(invoke).toHaveBeenCalledWith("connections_remove", { id: "url:example" });

    await desktop.selectConnection("url:example");
    expect(invoke).toHaveBeenCalledWith("connections_select", { id: "url:example" });

    const current = await desktop.getCurrentConnection();
    expect(current.id).toBe("local");
    expect(invoke).toHaveBeenCalledWith("connections_get_current");

    await desktop.openExternal("https://example.com");
    expect(invoke).toHaveBeenCalledWith("open_external", { url: "https://example.com" });

    const status = await desktop.daemonStatus();
    expect(status.managedState).toBe("managed");
    expect(invoke).toHaveBeenCalledWith("daemon_status");
    await desktop.daemonInstall();
    expect(invoke).toHaveBeenCalledWith("daemon_install");
    await desktop.daemonUpdate();
    expect(invoke).toHaveBeenCalledWith("daemon_update");
    await desktop.daemonRestart();
    expect(invoke).toHaveBeenCalledWith("daemon_restart");
    await desktop.takeOverDaemon();
    expect(invoke).toHaveBeenCalledWith("take_over_daemon");
    const versionInfo = await desktop.connectionVersion("url:example");
    expect(versionInfo.comparison).toBe("older");
    expect(invoke).toHaveBeenCalledWith("connection_version", { id: "url:example" });
  });

  it("onDaemonProgress subscribes and forwards events, unsubscribing on cleanup", async () => {
    vi.stubEnv("VITE_SMIND_DESKTOP", "1");
    const unlisten = vi.fn();
    const listen = vi.fn(async (_event: string, _handler: (e: { payload: unknown }) => void) => unlisten);
    vi.doMock("@tauri-apps/api/event", () => ({ listen }));

    const { desktop } = await import("@/lib/platform");
    const cb = vi.fn();
    const unsubscribe = desktop.onDaemonProgress(cb);
    await vi.waitFor(() => expect(listen).toHaveBeenCalledWith("daemon-progress", expect.any(Function)));
    const handler = listen.mock.calls[0][1];
    handler({ payload: { stage: "downloading", message: "Downloading…" } });
    expect(cb).toHaveBeenCalledWith({ stage: "downloading", message: "Downloading…" });

    unsubscribe();
    expect(unlisten).toHaveBeenCalled();
  });
});
