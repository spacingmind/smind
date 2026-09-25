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
  });
});
