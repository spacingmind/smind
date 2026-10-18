import type { ActionId } from "@/keyboard/actions";
import { CONNECTION_CHANGED_EVENT } from "@/lib/local-paths";

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
  kind: "local" | "url" | "relay";
  label: string;
  baseUrl: string;
}

/** What the relay-link confirmation screen shows (Rust `RelayPreview`): every field is non-secret -- the admission secret and key bytes never reach the webview. */
export interface RelayPreview {
  /** The relay's `host:port`, as written in the link. */
  relayAddress: string;
  relayHostId: string;
  /** The ends of the daemon's public key in groups of four, to compare with what the daemon prints. */
  keyFingerprint: string;
  /** Whether the link pins the relay's TLS certificate (false = CA-verified). */
  pinned: boolean;
  /** "changedConnection": a saved connection has this relay host id but a different key or relay. "alreadySaved": identical, adding refreshes it. */
  kind: "newHost" | "alreadySaved" | "changedConnection";
}

/**
 * The live state of the selected relay connection (Rust `RelayStatus`). All fields are non-secret: `deviceId` is this install's own device id (what
 * `smind relay devices approve` takes), `code`/`message` are the daemon's refusal.
 */
export interface RelayStatus {
  state:
    | "connecting"
    | "connected"
    | "reconnecting"
    | "awaitingApproval"
    | "revoked"
    | "refused"
    | "passwordRequired"
    | "incorrectPassword"
    | "passwordLocked";
  deviceId?: string;
  code?: string;
  message?: string;
  /** `passwordLocked`: seconds the daemon asks this device to wait before another try. */
  retryAfterSecs?: number;
}

/** The longest pairing link the UI will send to Rust (Rust enforces the same limit). */
export const MAX_RELAY_LINK_LENGTH = 4096;

/** True when `input` is a relay pairing link (an `offer` parameter in the URL fragment) -- never a plain daemon URL, even though it parses as one. */
export function isRelayPairingLink(input: string): boolean {
  const hash = input.trim().indexOf("#");
  if (hash < 0) return false;
  return new URLSearchParams(input.trim().slice(hash + 1)).has("offer");
}

/** The app-managed local daemon's status (ADR-0013 part D2), mirroring the Rust `DaemonStatus` DTO. */
export interface DaemonStatus {
  platform: "windows" | "macos" | "unsupported";
  reachable: boolean;
  daemonVersion: string | null;
  appVersion: string;
  comparison: "older" | "same" | "newer" | "unknown";
  managedState: "notRunning" | "managed" | "unmanaged";
  /** `/healthz` `managedBy` ("systemd" | "launchd" | "task"): an OS service manager owns this daemon, so the app never takes it over. Absent/null when none does. */
  managedBy?: string | null;
  pid: number | null;
  installedPath: string | null;
  logPath: string | null;
  /** Whether the managed binary exists on disk right now -- distinct from `managedState`, since a bare take-over (no prior Install/Update) can be "managed" with nothing installed. Restart is disabled unless this is true. */
  binaryInstalled: boolean;
  /** Windows only (AC6): an unmanaged port owner that is not an identifiable smind.exe -- typically the WSL relay forwarding a WSL daemon's port. Take over is hidden and an explanation is shown instead. */
  wslDaemonSuspected: boolean;
  /** Windows only (AC8): whether Git for Windows was found (null elsewhere). Install/start work without it. */
  gitFound: boolean | null;
  /** Set when a prerequisite is missing (AC8), e.g. Git for Windows. */
  prerequisiteMessage: string | null;
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
  /** `/healthz` `managedBy` when the daemon reports one; absent/null otherwise (and for a relay connection, which has no `/healthz`). */
  managedBy?: string | null;
}

/** A check result from `app_update_check` (ADR-0023 interface contract): no updater on this platform, nothing newer, or an update Rust has verified and now holds for `appUpdateInstall`. */
export type AppUpdateCheck =
  | { kind: "unsupported" }
  | { kind: "upToDate"; currentVersion: string }
  | { kind: "available"; version: string; currentVersion: string; notes: string | null; date: string | null };

/** A progress event emitted while `app_update_install` runs. */
export type AppUpdateProgress =
  | { stage: "downloading"; downloaded: number; total: number | null }
  | { stage: "installing" };

/** One entry of the Rust-side editor allowlist (`editors_list`): the id `openInEditor` takes, and what to call it in the UI. */
export interface EditorInfo {
  id: string;
  label: string;
  /** "fileManager" is Finder / File Explorer / the XDG file manager (reveal); "editor" is a code editor (open). */
  kind: "fileManager" | "editor";
}

/**
 * The native-only app-menu actions `app_menu_action` accepts (a fixed Rust-side
 * allowlist in `menu.rs`); renderer actions go through the keyboard registry instead.
 */
export type NativeMenuActionId =
  | "reload"
  | "zoom-in"
  | "zoom-out"
  | "zoom-reset"
  | "fullscreen"
  | "minimize"
  | "hide"
  | "about"
  | "log-folder"
  | "github"
  | "check-updates"
  | "quit";

/** The commands `capabilities/proxy.json` exposes to the bundled UI (AC5/ADR-0013 part D2), one method per command. */
export interface DesktopApi {
  listConnections(): Promise<Connection[]>;
  addConnection(label: string, url: string): Promise<Connection>;
  /** Validates a pasted relay pairing link in Rust and returns only its non-secret fields; saves nothing. Rejects with Rust's error text (which never contains the link). */
  previewRelayLink(pairingUrl: string): Promise<RelayPreview>;
  /** Saves the connection a relay pairing link describes (key material stays in Rust, `0600`). Rejects with Rust's error text. */
  addRelayConnection(label: string, pairingUrl: string): Promise<Connection>;
  removeConnection(id: string): Promise<void>;
  selectConnection(id: string): Promise<Connection>;
  getCurrentConnection(): Promise<Connection>;
  /** The live state of the selected relay connection (connected, awaiting approval on the daemon, revoked, ...); `null` when the selected connection is not a relay one. */
  relayStatus(): Promise<RelayStatus | null>;
  /**
   * Hands the relay connection `connectionId` (it must still be the selected one) the daemon's password (ADR-0031 P13c) once. Rust keeps it in memory only -- never on disk, in a
   * log, or returned to this UI -- and reconnects at once. Rejects with Rust's error text (which never contains the password).
   */
  relaySetPassword(connectionId: string, password: string): Promise<void>;
  /** Opens an http(s) URL in the OS's default browser -- Rust validates the scheme (AC5); this is the only escape hatch a bundled-UI link needs, since in-window navigation is restricted to the proxy origin (AC3). */
  openExternal(url: string): Promise<void>;
  /** Quits the app (`app_quit`). The renderer's Ctrl+Q on Windows/Linux, where the undecorated window has no menu to carry the accelerator. */
  quit(): Promise<void>;
  /** Runs a native-only app-menu item (`app_menu_action`) -- what the header app menu uses on Windows/Linux, where the native menu bar isn't drawn. */
  menuAction(id: NativeMenuActionId): Promise<void>;
  /** ADR-0013 part D2: install/update/restart/take-over the app-managed local daemon. */
  daemonStatus(): Promise<DaemonStatus>;
  daemonInstall(): Promise<DaemonStatus>;
  daemonUpdate(): Promise<DaemonStatus>;
  daemonRestart(): Promise<DaemonStatus>;
  takeOverDaemon(): Promise<DaemonStatus>;
  connectionVersion(id: string): Promise<ConnectionVersionInfo>;
  /** Subscribes to progress events during a long-running daemon operation; returns an unsubscribe function. */
  onDaemonProgress(cb: (progress: DaemonProgress) => void): () => void;
  /**
   * Subscribes to native menu actions (desktop-native-feel D3.3): the
   * app menu emits one `menu-action` event whose payload is the action
   * id, which the UI dispatches through the keyboard action registry.
   */
  onMenuAction(cb: (action: ActionId) => void): () => void;
  /**
   * The native window's theme -- on desktop the source of truth for the
   * "system" preference. On Windows the webview's `prefers-color-scheme`
   * follows a WebView2 profile setting the shell only updates on theme-change
   * events, so it can disagree with the OS; the window's own theme doesn't.
   */
  getSystemTheme(): Promise<"light" | "dark">;
  /** Calls `cb` when the native window theme changes (OS theme flip while following the system); returns an unsubscribe function. */
  onSystemThemeChanged(cb: (theme: "light" | "dark") => void): () => void;
  /** Detected installed editors plus the platform file manager (desktop-native-feel D4.2), from a fixed Rust-side allowlist. */
  editorsList(): Promise<EditorInfo[]>;
  /**
   * Opens `path` in the editor with id `editorId` from `editorsList`. The
   * path must be absolute and exist on this machine; Rust rejects
   * anything else. Rejects with an `Error` carrying Rust's human-readable
   * message, so it can be shown in a toast as-is.
   */
  openInEditor(editorId: string, path: string): Promise<void>;
  /** ADR-0023: asks Rust to check for an update (it holds any result for `appUpdateInstall`). */
  appUpdateCheck(): Promise<AppUpdateCheck>;
  /** Installs the update held by the last successful check; rejects with an Error carrying Rust's string. */
  appUpdateInstall(): Promise<void>;
  /** Subscribes to download/install progress during `appUpdateInstall`; returns an unsubscribe function. */
  onAppUpdateProgress(cb: (progress: AppUpdateProgress) => void): () => void;
  /** Subscribes to the Check for Updates… menu item; returns an unsubscribe function. */
  onAppUpdateCheckRequested(cb: () => void): () => void;
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
    previewRelayLink: () => reject(),
    addRelayConnection: () => reject(),
    removeConnection: () => reject(),
    selectConnection: () => reject(),
    getCurrentConnection: () => reject(),
    relayStatus: () => reject(),
    relaySetPassword: () => reject(),
    openExternal: () => reject(),
    quit: () => reject(),
    getSystemTheme: () => reject(),
    onSystemThemeChanged: () => () => {},
    menuAction: () => reject(),
    daemonStatus: () => reject(),
    daemonInstall: () => reject(),
    daemonUpdate: () => reject(),
    daemonRestart: () => reject(),
    takeOverDaemon: () => reject(),
    connectionVersion: () => reject(),
    onDaemonProgress: () => () => {},
    // The browser stub never fires: there is no native menu there.
    onMenuAction: () => () => {},
    editorsList: () => reject(),
    openInEditor: () => reject(),
    appUpdateCheck: () => reject(),
    appUpdateInstall: () => reject(),
    onAppUpdateProgress: () => () => {},
    // The browser stub never fires: there is no native menu there.
    onAppUpdateCheckRequested: () => () => {},
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

/** Tauri rejects an `invoke` with the bare string a Rust command returned as its `Err`; wrap it so callers can rely on `Error.message`. */
function asError(err: unknown): Error {
  return err instanceof Error ? err : new Error(String(err));
}

function announceConnectionChange(): void {
  if (typeof window !== "undefined") window.dispatchEvent(new Event(CONNECTION_CHANGED_EVENT));
}

function realDesktopApi(): DesktopApi {
  return {
    async listConnections() {
      const invoke = await loadInvoke();
      return invoke<Connection[]>("connections_list");
    },
    async addConnection(label, url) {
      const invoke = await loadInvoke();
      const conn = await invoke<Connection>("connections_add", { label, url });
      announceConnectionChange();
      return conn;
    },
    async previewRelayLink(pairingUrl) {
      const invoke = await loadInvoke();
      try {
        return await invoke<RelayPreview>("connections_preview_relay", { pairingUrl });
      } catch (err) {
        throw asError(err);
      }
    },
    async addRelayConnection(label, pairingUrl) {
      const invoke = await loadInvoke();
      try {
        const conn = await invoke<Connection>("connections_add_relay", { label, pairingUrl });
        announceConnectionChange();
        return conn;
      } catch (err) {
        throw asError(err);
      }
    },
    async removeConnection(id) {
      const invoke = await loadInvoke();
      await invoke("connections_remove", { id });
      announceConnectionChange();
    },
    async selectConnection(id) {
      const invoke = await loadInvoke();
      const conn = await invoke<Connection>("connections_select", { id });
      announceConnectionChange();
      return conn;
    },
    async getCurrentConnection() {
      const invoke = await loadInvoke();
      return invoke<Connection>("connections_get_current");
    },
    async relayStatus() {
      const invoke = await loadInvoke();
      return invoke<RelayStatus | null>("connections_relay_status");
    },
    async relaySetPassword(connectionId, password) {
      const invoke = await loadInvoke();
      try {
        await invoke("connections_relay_set_password", { id: connectionId, password });
      } catch (err) {
        throw asError(err);
      }
    },
    async openExternal(url) {
      const invoke = await loadInvoke();
      await invoke("open_external", { url });
    },
    async quit() {
      const invoke = await loadInvoke();
      await invoke("app_quit");
    },
    async getSystemTheme() {
      const { getCurrentWindow } = await import("@tauri-apps/api/window");
      return (await getCurrentWindow().theme()) === "dark" ? "dark" : "light";
    },
    onSystemThemeChanged(cb) {
      let unlisten: (() => void) | null = null;
      let cancelled = false;
      import("@tauri-apps/api/window")
        .then(({ getCurrentWindow }) =>
          getCurrentWindow().onThemeChanged((event) =>
            cb(event.payload === "dark" ? "dark" : "light"),
          ),
        )
        .then((fn) => {
          if (cancelled) fn();
          else unlisten = fn;
        })
        .catch(() => {});
      return () => {
        cancelled = true;
        unlisten?.();
      };
    },
    async menuAction(id) {
      const invoke = await loadInvoke();
      try {
        await invoke("app_menu_action", { id });
      } catch (err) {
        throw asError(err);
      }
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
    async editorsList() {
      const invoke = await loadInvoke();
      return invoke<EditorInfo[]>("editors_list");
    },
    async openInEditor(editorId, path) {
      const invoke = await loadInvoke();
      try {
        await invoke("open_in_editor", { editorId, path });
      } catch (err) {
        throw asError(err);
      }
    },
    async appUpdateCheck() {
      const invoke = await loadInvoke();
      return invoke<AppUpdateCheck>("app_update_check");
    },
    async appUpdateInstall() {
      const invoke = await loadInvoke();
      try {
        await invoke("app_update_install");
      } catch (err) {
        throw asError(err);
      }
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
    onMenuAction(cb) {
      let unlisten: (() => void) | null = null;
      let cancelled = false;
      import("@tauri-apps/api/event").then(({ listen }) =>
        listen<string>("menu-action", (event) => cb(event.payload as ActionId)).then((fn) => {
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
    onAppUpdateProgress(cb) {
      let unlisten: (() => void) | null = null;
      let cancelled = false;
      import("@tauri-apps/api/event").then(({ listen }) =>
        listen<AppUpdateProgress>("app-update-progress", (event) => cb(event.payload)).then((fn) => {
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
    onAppUpdateCheckRequested(cb) {
      let unlisten: (() => void) | null = null;
      let cancelled = false;
      import("@tauri-apps/api/event").then(({ listen }) =>
        listen("app-update-check-requested", () => cb()).then((fn) => {
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
