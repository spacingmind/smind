/**
 * The desktop platform surface (ADR-0013 AC6): a single `isDesktop` /
 * `desktop` API pair components use instead of touching
 * `window.__TAURI__`/`@tauri-apps/api` directly. `isDesktop` is the
 * build-time `VITE_SMIND_DESKTOP` flag -- set only by
 * `bun run build:desktop` (`desktop/src-tauri/tauri.conf.json`'s
 * `beforeBuildCommand`/`beforeDevCommand`), never by the daemon-embedded
 * build's own `bun run build` -- so `desktop` is a real, working API in
 * a desktop build and an always-rejecting stub everywhere else. Nothing
 * outside this file imports `@tauri-apps/api`.
 */

/** A saved daemon connection (AC4's Rust-side `Registry`/`Connection` shape, mirrored here). */
export interface Connection {
  id: string;
  kind: "local" | "url";
  label: string;
  baseUrl: string;
}

/** The app-managed local daemon's status (ADR-0013 part D2), mirroring the Rust `DaemonStatus` DTO. */
export interface DaemonStatus {
  platform: "wsl2" | "macos" | "unsupported";
  reachable: boolean;
  daemonVersion: string | null;
  appVersion: string;
  comparison: "older" | "same" | "newer" | "unknown";
  managedState: "notRunning" | "managed" | "unmanaged";
  pid: number | null;
  installedPath: string | null;
  logPath: string | null;
  /** Whether the managed binary exists on disk right now -- distinct from `managedState`, since a bare take-over (no prior Install/Update) can be "managed" with nothing installed. Restart is disabled unless this is true. */
  binaryInstalled: boolean;
}

/** A progress update emitted while `daemonInstall`/`daemonUpdate`/`daemonRestart` run. */
export interface DaemonProgress {
  stage: "downloading" | "verifying" | "installing" | "starting" | "stopping";
  message: string;
}

/** Version skew for *any* saved connection (not just local) -- the loopback proxy doesn't forward `/healthz`, so this probes directly. */
export interface ConnectionVersionInfo {
  reachable: boolean;
  daemonVersion: string | null;
  appVersion: string;
  comparison: "older" | "same" | "newer" | "unknown";
}

/** The commands `capabilities/proxy.json` exposes to the bundled UI (AC5/ADR-0013 part D2), one method per command. */
export interface DesktopApi {
  listConnections(): Promise<Connection[]>;
  addConnection(label: string, url: string): Promise<Connection>;
  removeConnection(id: string): Promise<void>;
  selectConnection(id: string): Promise<Connection>;
  getCurrentConnection(): Promise<Connection>;
  /** Opens an http(s) URL in the OS's default browser -- Rust validates the scheme (AC5); this is the only escape hatch a bundled-UI link needs, since in-window navigation is restricted to the proxy origin (AC3). */
  openExternal(url: string): Promise<void>;
  /** ADR-0013 part D2: install/update/restart/take-over the app-managed local daemon. */
  daemonStatus(): Promise<DaemonStatus>;
  daemonInstall(): Promise<DaemonStatus>;
  daemonUpdate(): Promise<DaemonStatus>;
  daemonRestart(): Promise<DaemonStatus>;
  takeOverDaemon(): Promise<DaemonStatus>;
  connectionVersion(id: string): Promise<ConnectionVersionInfo>;
  /** Subscribes to progress events during a long-running daemon operation; returns an unsubscribe function. */
  onDaemonProgress(cb: (progress: DaemonProgress) => void): () => void;
}

/** The host OS of a desktop build, read from the webview's user agent (WKWebView / WebView2 / WebKitGTK name their platform). */
export type DesktopOS = "macos" | "windows" | "linux";

/** The main window's observable state, for the drawn caption buttons and the header's traffic-light inset. */
export interface WindowState {
  maximized: boolean;
  fullscreen: boolean;
}

/**
 * Window controls for the drawn chrome (desktop-native-feel D1). Backed by
 * Tauri core `core:window:allow-*` permissions scoped to exactly these
 * operations in `capabilities/proxy.json`. `close` raises the window's
 * close-request, which the shell turns into hide-to-tray -- it never
 * quits or destroys.
 */
export interface DesktopWindowApi {
  minimize(): Promise<void>;
  toggleMaximize(): Promise<void>;
  close(): Promise<void>;
  /** Tells the shell the UI has painted its first frame, so it can show the window it created hidden (D2.1). */
  notifyPainted(): Promise<void>;
  /** Mirrors the UI theme into the native window (vibrancy/chrome appearance) and remembers it for the next launch's pre-paint background. */
  setTheme(preference: "light" | "dark" | "system", resolved: "light" | "dark"): Promise<void>;
  /** Calls `cb` with the current state straight away and again whenever it changes (maximize, restore, snap, fullscreen); returns an unsubscribe function. */
  onStateChange(cb: (state: WindowState) => void): () => void;
}

/** True only in a build made with `VITE_SMIND_DESKTOP=1`. */
export const isDesktop: boolean = import.meta.env.VITE_SMIND_DESKTOP === "1";

/** Maps a user-agent string to a desktop OS; unknown agents are treated as Linux (the only platform whose webview names no vendor OS reliably). */
export function detectDesktopOS(userAgent: string): DesktopOS {
  if (/Macintosh|Mac OS X/i.test(userAgent)) return "macos";
  if (/Windows/i.test(userAgent)) return "windows";
  return "linux";
}

/** The host OS in a desktop build, `null` in a browser build. */
export const desktopOS: DesktopOS | null =
  isDesktop && typeof navigator !== "undefined" ? detectDesktopOS(navigator.userAgent) : null;

function unavailable(): DesktopApi {
  const reject = <T>(): Promise<T> =>
    Promise.reject(new Error("smind: the desktop connection API is unavailable in this build"));
  return {
    listConnections: () => reject(),
    addConnection: () => reject(),
    removeConnection: () => reject(),
    selectConnection: () => reject(),
    getCurrentConnection: () => reject(),
    openExternal: () => reject(),
    daemonStatus: () => reject(),
    daemonInstall: () => reject(),
    daemonUpdate: () => reject(),
    daemonRestart: () => reject(),
    takeOverDaemon: () => reject(),
    connectionVersion: () => reject(),
    onDaemonProgress: () => () => {},
  };
}

function unavailableWindow(): DesktopWindowApi {
  const reject = (): Promise<void> =>
    Promise.reject(new Error("smind: the desktop window API is unavailable in this build"));
  return {
    minimize: reject,
    toggleMaximize: reject,
    close: reject,
    notifyPainted: reject,
    setTheme: reject,
    onStateChange: () => () => {},
  };
}

/** Imported dynamically, and only from inside the `isDesktop` branch below, so a non-desktop build never needs `@tauri-apps/api` at runtime. */
async function loadInvoke() {
  const { invoke } = await import("@tauri-apps/api/core");
  return invoke;
}

function realDesktopApi(): DesktopApi {
  return {
    async listConnections() {
      const invoke = await loadInvoke();
      return invoke<Connection[]>("connections_list");
    },
    async addConnection(label, url) {
      const invoke = await loadInvoke();
      return invoke<Connection>("connections_add", { label, url });
    },
    async removeConnection(id) {
      const invoke = await loadInvoke();
      await invoke("connections_remove", { id });
    },
    async selectConnection(id) {
      const invoke = await loadInvoke();
      return invoke<Connection>("connections_select", { id });
    },
    async getCurrentConnection() {
      const invoke = await loadInvoke();
      return invoke<Connection>("connections_get_current");
    },
    async openExternal(url) {
      const invoke = await loadInvoke();
      await invoke("open_external", { url });
    },
    async daemonStatus() {
      const invoke = await loadInvoke();
      return invoke<DaemonStatus>("daemon_status");
    },
    async daemonInstall() {
      const invoke = await loadInvoke();
      return invoke<DaemonStatus>("daemon_install");
    },
    async daemonUpdate() {
      const invoke = await loadInvoke();
      return invoke<DaemonStatus>("daemon_update");
    },
    async daemonRestart() {
      const invoke = await loadInvoke();
      return invoke<DaemonStatus>("daemon_restart");
    },
    async takeOverDaemon() {
      const invoke = await loadInvoke();
      return invoke<DaemonStatus>("take_over_daemon");
    },
    async connectionVersion(id) {
      const invoke = await loadInvoke();
      return invoke<ConnectionVersionInfo>("connection_version", { id });
    },
    onDaemonProgress(cb) {
      let unlisten: (() => void) | null = null;
      let cancelled = false;
      import("@tauri-apps/api/event").then(({ listen }) =>
        listen<DaemonProgress>("daemon-progress", (event) => cb(event.payload)).then((fn) => {
          if (cancelled) {
            fn();
          } else {
            unlisten = fn;
          }
        }),
      );
      return () => {
        cancelled = true;
        unlisten?.();
      };
    },
  };
}

function realWindowApi(): DesktopWindowApi {
  // One import and one handle, shared by every call below.
  let current: Promise<import("@tauri-apps/api/window").Window> | null = null;
  function currentWindow() {
    current ??= import("@tauri-apps/api/window").then(({ getCurrentWindow }) => getCurrentWindow());
    return current;
  }
  return {
    async minimize() {
      await (await currentWindow()).minimize();
    },
    async toggleMaximize() {
      await (await currentWindow()).toggleMaximize();
    },
    async close() {
      await (await currentWindow()).close();
    },
    async notifyPainted() {
      const invoke = await loadInvoke();
      await invoke("window_ready");
    },
    async setTheme(preference, resolved) {
      const invoke = await loadInvoke();
      await invoke("window_set_theme", { preference, resolved });
    },
    onStateChange(cb) {
      let cancelled = false;
      let unlisten: (() => void) | null = null;
      let queued = false;
      const read = async () => {
        const win = await currentWindow();
        const [maximized, fullscreen] = await Promise.all([win.isMaximized(), win.isFullscreen()]);
        if (!cancelled) cb({ maximized, fullscreen });
      };
      // A drag-resize fires `resized` every frame; coalesce to one read per frame.
      const schedule = () => {
        if (queued) return;
        queued = true;
        requestAnimationFrame(() => {
          queued = false;
          void read().catch(() => {});
        });
      };
      currentWindow()
        .then((win) => win.onResized(schedule))
        .then((fn) => {
          if (cancelled) fn();
          else unlisten = fn;
        })
        .catch(() => {});
      void read().catch(() => {});
      return () => {
        cancelled = true;
        unlisten?.();
      };
    },
  };
}

/** The desktop API: a working implementation when `isDesktop`, an always-rejecting stub otherwise. */
export const desktop: DesktopApi = isDesktop ? realDesktopApi() : unavailable();

/** The window-control API (D1): real when `isDesktop`, an always-rejecting stub otherwise. */
export const desktopWindow: DesktopWindowApi = isDesktop ? realWindowApi() : unavailableWindow();
