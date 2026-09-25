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

/** The commands `capabilities/proxy.json` exposes to the bundled UI (AC5), one method per command. */
export interface DesktopApi {
  listConnections(): Promise<Connection[]>;
  addConnection(label: string, url: string): Promise<Connection>;
  removeConnection(id: string): Promise<void>;
  selectConnection(id: string): Promise<Connection>;
  getCurrentConnection(): Promise<Connection>;
  /** Opens an http(s) URL in the OS's default browser -- Rust validates the scheme (AC5); this is the only escape hatch a bundled-UI link needs, since in-window navigation is restricted to the proxy origin (AC3). */
  openExternal(url: string): Promise<void>;
}

/** True only in a build made with `VITE_SMIND_DESKTOP=1`. */
export const isDesktop: boolean = import.meta.env.VITE_SMIND_DESKTOP === "1";

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
  };
}

/** The desktop API: a working implementation when `isDesktop`, an always-rejecting stub otherwise. */
export const desktop: DesktopApi = isDesktop ? realDesktopApi() : unavailable();
