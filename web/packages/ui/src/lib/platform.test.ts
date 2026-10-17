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
    await expect(desktop.addConnection("x", "http://example.com", "pw")).rejects.toThrow(/unavailable/i);
    await expect(desktop.probeConnection("http://example.com")).rejects.toThrow(/unavailable/i);
    await expect(desktop.addSshConnection("x", { host: "h", sshPort: null, daemonPort: 4648 })).rejects.toThrow(/unavailable/i);
    await expect(desktop.appSettingsGet()).rejects.toThrow(/unavailable/i);
    await expect(desktop.appSettingsSet({ onboardingCompleted: true })).rejects.toThrow(/unavailable/i);
    expect(() => desktop.onRunSetupRequested(() => {})()).not.toThrow();
    await expect(desktop.previewRelayLink("x")).rejects.toThrow(/unavailable/i);
    await expect(desktop.addRelayConnection("x", "y")).rejects.toThrow(/unavailable/i);
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
    await expect(desktop.appUpdateCheck()).rejects.toThrow(/unavailable/i);
    await expect(desktop.appUpdateInstall()).rejects.toThrow(/unavailable/i);
    expect(() => desktop.onAppUpdateProgress(() => {})()).not.toThrow();
    expect(() => desktop.onAppUpdateCheckRequested(() => {})()).not.toThrow();
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
          return { platform: "macos", reachable: true, daemonVersion: "0.7.0", appVersion: "0.7.0", comparison: "same", managedState: "managed", pid: 123, installedPath: "/tmp/smind", logPath: "/tmp/smind.log", binaryInstalled: true, localService: "smind", portOccupied: false, wslDaemonSuspected: false, gitFound: null, prerequisiteMessage: null };
        case "connection_version":
          return { reachable: true, daemonVersion: "0.6.0", appVersion: "0.7.0", comparison: "older" };
        case "app_update_check":
          return { kind: "upToDate", currentVersion: "0.7.0" };
        case "app_update_install":
          return undefined;
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
    expect(invoke).toHaveBeenCalledWith("connections_add", { label: "Tunnel", url: "http://example.com:9000", password: null });

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

    const updateCheck = await desktop.appUpdateCheck();
    expect(updateCheck).toEqual({ kind: "upToDate", currentVersion: "0.7.0" });
    expect(invoke).toHaveBeenCalledWith("app_update_check");
    await desktop.appUpdateInstall();
    expect(invoke).toHaveBeenCalledWith("app_update_install");
  });

  it("maps the relay-link methods to their commands, wraps Rust's string errors, and announces a saved connection", async () => {
    vi.stubEnv("VITE_SMIND_DESKTOP", "1");
    const preview = { relayAddress: "relay.test:7401", relayHostId: "ws-1", keyFingerprint: "ERER ERER … RERE RERE", pinned: true, kind: "newHost" };
    const conn = { id: "relay:ws-1@relay.test:7400", kind: "relay", label: "Home", baseUrl: "relay://ws-1@relay.test:7400" };
    const invoke = vi.fn(async (cmd: string) => {
      if (cmd === "connections_preview_relay") return preview;
      if (cmd === "connections_add_relay") return conn;
      throw "pairing: invalid offer: unsupported offer version 2";
    });
    vi.doMock("@tauri-apps/api/core", () => ({ invoke }));
    const { desktop } = await import("@/lib/platform");
    const { CONNECTION_CHANGED_EVENT } = await import("@/lib/local-paths");
    const changed = vi.fn();
    window.addEventListener(CONNECTION_CHANGED_EVENT, changed);

    expect(await desktop.previewRelayLink("https://x/pair#offer=abc")).toEqual(preview);
    expect(invoke).toHaveBeenCalledWith("connections_preview_relay", { pairingUrl: "https://x/pair#offer=abc" });
    expect(changed).not.toHaveBeenCalled();

    expect(await desktop.addRelayConnection("Home", "https://x/pair#offer=abc")).toEqual(conn);
    expect(invoke).toHaveBeenCalledWith("connections_add_relay", { label: "Home", pairingUrl: "https://x/pair#offer=abc" });
    expect(changed).toHaveBeenCalledTimes(1);

    invoke.mockRejectedValueOnce("pairing: invalid offer: unsupported offer version 2");
    await expect(desktop.previewRelayLink("bad")).rejects.toThrow("unsupported offer version 2");
    invoke.mockRejectedValueOnce("too long");
    await expect(desktop.addRelayConnection("", "bad")).rejects.toBeInstanceOf(Error);
    expect(changed).toHaveBeenCalledTimes(1);
    window.removeEventListener(CONNECTION_CHANGED_EVENT, changed);
  });

  it("maps the first-run methods to their commands: probe, password add, ssh add, settings get/set", async () => {
    vi.stubEnv("VITE_SMIND_DESKTOP", "1");
    const probe = { reachable: true, version: "0.12.0", service: "smind", authRequired: true, loopback: false, error: null };
    const ssh = { id: "ssh:homelab", kind: "ssh", label: "homelab", baseUrl: "http://127.0.0.1:50123" };
    const settings = { onboardingCompleted: true, manageLocalDaemon: false };
    const invoke = vi.fn(async (cmd: string) => {
      switch (cmd) {
        case "connections_probe":
          return probe;
        case "connections_add":
          return { id: "url:h", kind: "url", label: "h", baseUrl: "http://h:4648" };
        case "connections_add_ssh":
          return ssh;
        case "app_settings_get":
        case "app_settings_set":
          return settings;
        default:
          throw new Error(`unexpected invoke command ${cmd}`);
      }
    });
    vi.doMock("@tauri-apps/api/core", () => ({ invoke }));
    const { desktop } = await import("@/lib/platform");
    const { CONNECTION_CHANGED_EVENT } = await import("@/lib/local-paths");
    const changed = vi.fn();
    window.addEventListener(CONNECTION_CHANGED_EVENT, changed);

    expect(await desktop.probeConnection("http://h:4648")).toEqual(probe);
    expect(invoke).toHaveBeenCalledWith("connections_probe", { url: "http://h:4648" });
    expect(changed).not.toHaveBeenCalled();

    await desktop.addConnection("h", "http://h:4648", "hunter2");
    expect(invoke).toHaveBeenCalledWith("connections_add", { label: "h", url: "http://h:4648", password: "hunter2" });
    expect(changed).toHaveBeenCalledTimes(1);

    expect(await desktop.addSshConnection("homelab", { host: "homelab", sshPort: 2222, daemonPort: 4648 })).toEqual(ssh);
    expect(invoke).toHaveBeenCalledWith("connections_add_ssh", { label: "homelab", host: "homelab", sshPort: 2222, daemonPort: 4648 });
    await desktop.addSshConnection("homelab", { host: "homelab", sshPort: null, daemonPort: 4648 });
    expect(invoke).toHaveBeenLastCalledWith("connections_add_ssh", { label: "homelab", host: "homelab", sshPort: null, daemonPort: 4648 });
    expect(changed).toHaveBeenCalledTimes(3);

    expect(await desktop.appSettingsGet()).toEqual(settings);
    expect(invoke).toHaveBeenCalledWith("app_settings_get");
    expect(await desktop.appSettingsSet({ manageLocalDaemon: false })).toEqual(settings);
    expect(invoke).toHaveBeenCalledWith("app_settings_set", { patch: { manageLocalDaemon: false } });
    window.removeEventListener(CONNECTION_CHANGED_EVENT, changed);
  });

  it("wraps Rust's string errors from the first-run commands and announces nothing on failure", async () => {
    vi.stubEnv("VITE_SMIND_DESKTOP", "1");
    const invoke = vi.fn(async () => {
      throw "ssh: connect to host homelab port 22: Connection refused";
    });
    vi.doMock("@tauri-apps/api/core", () => ({ invoke }));
    const { desktop } = await import("@/lib/platform");
    const { CONNECTION_CHANGED_EVENT } = await import("@/lib/local-paths");
    const changed = vi.fn();
    window.addEventListener(CONNECTION_CHANGED_EVENT, changed);

    const target = { host: "homelab", sshPort: null, daemonPort: 4648 };
    await expect(desktop.addSshConnection("h", target)).rejects.toThrow("Connection refused");
    await expect(desktop.addSshConnection("h", target)).rejects.toBeInstanceOf(Error);
    await expect(desktop.probeConnection("http://h")).rejects.toBeInstanceOf(Error);
    await expect(desktop.appSettingsSet({ onboardingCompleted: true })).rejects.toBeInstanceOf(Error);
    await expect(desktop.addConnection("h", "http://h", "pw")).rejects.toBeInstanceOf(Error);
    expect(changed).not.toHaveBeenCalled();
    window.removeEventListener(CONNECTION_CHANGED_EVENT, changed);
  });

  it("onRunSetupRequested subscribes to the Help menu event and unsubscribes on cleanup", async () => {
    vi.stubEnv("VITE_SMIND_DESKTOP", "1");
    const unlisten = vi.fn();
    const listen = vi.fn(async (_event: string, _handler: (e: { payload: unknown }) => void) => unlisten);
    vi.doMock("@tauri-apps/api/event", () => ({ listen }));
    const { desktop } = await import("@/lib/platform");
    const cb = vi.fn();
    const unsubscribe = desktop.onRunSetupRequested(cb);
    await vi.waitFor(() => expect(listen).toHaveBeenCalledWith("run-setup-requested", expect.any(Function)));
    listen.mock.calls[0][1]({ payload: null });
    expect(cb).toHaveBeenCalledTimes(1);
    unsubscribe();
    await vi.waitFor(() => expect(unlisten).toHaveBeenCalled());
  });

  it("isRelayPairingLink recognises an offer fragment and nothing else", async () => {
    vi.stubEnv("VITE_SMIND_DESKTOP", "1");
    const { isRelayPairingLink } = await import("@/lib/platform");
    expect(isRelayPairingLink("https://spacingmind.sh/pair#offer=abc")).toBe(true);
    expect(isRelayPairingLink("  https://x/pair#v=1&offer=abc\n")).toBe(true);
    expect(isRelayPairingLink("http://example.com:9000")).toBe(false);
    expect(isRelayPairingLink("http://example.com:9000/#section")).toBe(false);
    expect(isRelayPairingLink("http://example.com/?offer=1")).toBe(false);
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

  it("onAppUpdateProgress and onAppUpdateCheckRequested subscribe to the update events", async () => {
    vi.stubEnv("VITE_SMIND_DESKTOP", "1");
    const unlisten = vi.fn();
    const listen = vi.fn(async (_event: string, _handler: (e: { payload: unknown }) => void) => unlisten);
    vi.doMock("@tauri-apps/api/event", () => ({ listen }));

    const { desktop } = await import("@/lib/platform");
    const cb = vi.fn();
    desktop.onAppUpdateProgress(cb);
    await vi.waitFor(() => expect(listen).toHaveBeenCalledWith("app-update-progress", expect.any(Function)));
    listen.mock.calls[0][1]({ payload: { stage: "downloading", downloaded: 10, total: null } });
    expect(cb).toHaveBeenCalledWith({ stage: "downloading", downloaded: 10, total: null });

    const requested = vi.fn();
    desktop.onAppUpdateCheckRequested(requested);
    await vi.waitFor(() => expect(listen).toHaveBeenCalledWith("app-update-check-requested", expect.any(Function)));
    listen.mock.calls[1][1]({ payload: null });
    expect(requested).toHaveBeenCalledTimes(1);

    expect(unlisten).not.toHaveBeenCalled();
  });

  it("appUpdateInstall turns Tauri's plain-string rejection into an Error with that message", async () => {
    vi.stubEnv("VITE_SMIND_DESKTOP", "1");
    const invoke = vi.fn(() => Promise.reject("signature verification failed"));
    vi.doMock("@tauri-apps/api/core", () => ({ invoke }));

    const { desktop } = await import("@/lib/platform");
    await expect(desktop.appUpdateInstall()).rejects.toThrow("signature verification failed");
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
