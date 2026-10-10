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
  vi.doUnmock("@tauri-apps/api/window");
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
    await expect(desktop.editorsList()).rejects.toThrow(/unavailable/i);
    await expect(desktop.openInEditor("vscode", "/tmp/a")).rejects.toThrow(/unavailable/i);
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
          return { platform: "macos", reachable: true, daemonVersion: "0.7.0", appVersion: "0.7.0", comparison: "same", managedState: "managed", pid: 123, installedPath: "/tmp/smind", logPath: "/tmp/smind.log", binaryInstalled: true };
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

describe("editors (desktop-native-feel D4)", () => {
  it("editorsList and openInEditor call the matching tauri commands with camelCase args", async () => {
    vi.stubEnv("VITE_SMIND_DESKTOP", "1");
    const editors = [
      { id: "file-manager", label: "Finder", kind: "fileManager" },
      { id: "vscode", label: "VS Code", kind: "editor" },
    ];
    const invoke = vi.fn(async (cmd: string) => (cmd === "editors_list" ? editors : undefined));
    vi.doMock("@tauri-apps/api/core", () => ({ invoke }));

    const { desktop } = await import("@/lib/platform");
    await expect(desktop.editorsList()).resolves.toEqual(editors);
    expect(invoke).toHaveBeenCalledWith("editors_list");
    await desktop.openInEditor("vscode", "/tmp/a/main.go");
    expect(invoke).toHaveBeenCalledWith("open_in_editor", { editorId: "vscode", path: "/tmp/a/main.go" });
  });

  it("openInEditor turns Tauri's plain-string rejection into an Error with that message", async () => {
    vi.stubEnv("VITE_SMIND_DESKTOP", "1");
    const invoke = vi.fn(() => Promise.reject("path does not exist: /tmp/gone"));
    vi.doMock("@tauri-apps/api/core", () => ({ invoke }));

    const { desktop } = await import("@/lib/platform");
    await expect(desktop.openInEditor("vscode", "/tmp/gone")).rejects.toThrow("path does not exist: /tmp/gone");
  });
});

describe("window controls (desktop-native-feel D1)", () => {
  it("platform-window-controls-stub: in a non-desktop build the window API rejects and reports no OS", async () => {
    vi.stubEnv("VITE_SMIND_DESKTOP", "");
    const { desktopWindow, desktopOS } = await import("@/lib/platform");

    expect(desktopOS).toBeNull();
    await expect(desktopWindow.minimize()).rejects.toThrow(/unavailable/i);
    await expect(desktopWindow.toggleMaximize()).rejects.toThrow(/unavailable/i);
    await expect(desktopWindow.close()).rejects.toThrow(/unavailable/i);
    await expect(desktopWindow.notifyPainted()).rejects.toThrow(/unavailable/i);
    await expect(desktopWindow.setTheme("system", "dark")).rejects.toThrow(/unavailable/i);
    const cb = vi.fn();
    expect(() => desktopWindow.onStateChange(cb)()).not.toThrow();
    expect(cb).not.toHaveBeenCalled();
  });

  it("detectDesktopOS reads the webview user agents of all three platforms", async () => {
    vi.stubEnv("VITE_SMIND_DESKTOP", "");
    const { detectDesktopOS } = await import("@/lib/platform");
    expect(detectDesktopOS("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15")).toBe("macos");
    expect(detectDesktopOS("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Edg/130")).toBe("windows");
    expect(detectDesktopOS("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/605.1.15")).toBe("linux");
  });

  it("in a desktop build each method maps to exactly one Tauri window call or command", async () => {
    vi.stubEnv("VITE_SMIND_DESKTOP", "1");
    const win = {
      minimize: vi.fn(async () => {}),
      toggleMaximize: vi.fn(async () => {}),
      close: vi.fn(async () => {}),
      destroy: vi.fn(async () => {}),
      isMaximized: vi.fn(async () => true),
      isFullscreen: vi.fn(async () => false),
      onResized: vi.fn(async (_h: () => void) => () => {}),
    };
    const invoke = vi.fn(async () => undefined);
    vi.doMock("@tauri-apps/api/window", () => ({ getCurrentWindow: () => win }));
    vi.doMock("@tauri-apps/api/core", () => ({ invoke }));
    const { desktopWindow } = await import("@/lib/platform");

    await desktopWindow.minimize();
    await desktopWindow.toggleMaximize();
    await desktopWindow.close();
    expect(win.minimize).toHaveBeenCalledTimes(1);
    expect(win.toggleMaximize).toHaveBeenCalledTimes(1);
    expect(win.close).toHaveBeenCalledTimes(1);
    expect(win.destroy).not.toHaveBeenCalled();

    await desktopWindow.notifyPainted();
    expect(invoke).toHaveBeenCalledWith("window_ready");
    await desktopWindow.setTheme("system", "dark");
    expect(invoke).toHaveBeenCalledWith("window_set_theme", { preference: "system", resolved: "dark" });
  });

  it("onStateChange reads the state immediately, re-reads on resize, and unsubscribes", async () => {
    vi.stubEnv("VITE_SMIND_DESKTOP", "1");
    let maximized = false;
    const unlisten = vi.fn();
    let onResize: () => void = () => {};
    const win = {
      isMaximized: vi.fn(async () => maximized),
      isFullscreen: vi.fn(async () => false),
      onResized: vi.fn(async (h: () => void) => {
        onResize = h;
        return unlisten;
      }),
    };
    vi.doMock("@tauri-apps/api/window", () => ({ getCurrentWindow: () => win }));
    vi.doMock("@tauri-apps/api/core", () => ({ invoke: vi.fn() }));
    const { desktopWindow } = await import("@/lib/platform");

    const cb = vi.fn();
    const stop = desktopWindow.onStateChange(cb);
    await vi.waitFor(() => expect(cb).toHaveBeenCalledWith({ maximized: false, fullscreen: false }));
    await vi.waitFor(() => expect(win.onResized).toHaveBeenCalled());

    maximized = true;
    onResize();
    await vi.waitFor(() => expect(cb).toHaveBeenLastCalledWith({ maximized: true, fullscreen: false }));

    stop();
    expect(unlisten).toHaveBeenCalled();
  });
});
