import { act } from "react";
import { waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import {
  BROWSER_SETTINGS_POLL_MS,
  useBrowserSettings,
  useClearBrowserData,
  useSetBrowserSettings,
} from "@/hooks/use-browser-settings";
import { RpcError } from "@/lib/ws-client";
import { browserDaemon, browserView } from "@/test/browser-test-client";
import { renderHookWithDaemon } from "@/test/render-with-daemon";

function methods(d: { calls: { method: string }[] }): string[] {
  return d.calls.map((c) => c.method);
}

describe("browser settings hooks", () => {
  it("reads browser.settings.get once on mount and nothing else", async () => {
    const d = browserDaemon((m) => (m === "browser.settings.get" ? browserView({ workspaceId: 7 }) : undefined));
    const { result } = renderHookWithDaemon(() => useBrowserSettings(7), d, { initialProps: undefined });
    await waitFor(() => expect(result.current.data?.workspaceId).toBe(7));
    expect(d.calls.filter((c) => c.method.startsWith("browser."))).toEqual([{ method: "browser.settings.get", params: { workspaceId: 7 } }]);
  });

  it("makes no call without a workspace", async () => {
    const d = browserDaemon(() => browserView());
    renderHookWithDaemon(() => useBrowserSettings(null), d, { initialProps: undefined });
    await act(async () => {
      await Promise.resolve();
    });
    expect(methods(d).filter((m) => m.startsWith("browser."))).toEqual([]);
  });

  it("normalises null lists", async () => {
    const d = browserDaemon(() => ({ ...browserView(), allowHosts: null, providers: null }));
    const { result } = renderHookWithDaemon(() => useBrowserSettings(1), d, { initialProps: undefined });
    await waitFor(() => expect(result.current.data).toBeDefined());
    expect(result.current.data?.allowHosts).toEqual([]);
    expect(result.current.data?.providers).toEqual([]);
  });

  it("re-reads the view every BROWSER_SETTINGS_POLL_MS while mounted, and never without a workspace", async () => {
    expect(BROWSER_SETTINGS_POLL_MS).toBe(3000);
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      const d = browserDaemon(() => browserView());
      renderHookWithDaemon(() => ({ on: useBrowserSettings(1), off: useBrowserSettings(null) }), d, { initialProps: undefined });
      const gets = () => d.calls.filter((c) => c.method === "browser.settings.get").length;
      await waitFor(() => expect(gets()).toBe(1));
      await act(async () => {
        await vi.advanceTimersByTimeAsync(BROWSER_SETTINGS_POLL_MS + 100);
      });
      await waitFor(() => expect(gets()).toBe(2));
      expect(d.calls.every((c) => c.method !== "browser.settings.get" || (c.params as { workspaceId: number }).workspaceId === 1)).toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });

  it("a poll still in flight when a change is answered cannot put the older view back", async () => {
    let release: (v: unknown) => void = () => {};
    let held = false;
    const d = browserDaemon((m, p) => {
      if (m === "browser.settings.get") {
        if (held) return new Promise((r) => (release = r)); // the slow poll answers with the OLD view
        return browserView({ enabled: false });
      }
      if (m === "browser.settings.set") return browserView({ enabled: (p as { enabled: boolean }).enabled });
      return undefined;
    });
    const { result } = renderHookWithDaemon(() => ({ get: useBrowserSettings(1), set: useSetBrowserSettings(1) }), d, { initialProps: undefined });
    await waitFor(() => expect(result.current.get.data?.enabled).toBe(false));
    held = true;
    await act(async () => {
      void result.current.get.refetch(); // a poll starts and hangs
      await Promise.resolve();
    });
    await act(async () => {
      await result.current.set.mutateAsync({ enabled: true });
    });
    await act(async () => {
      release(browserView({ enabled: false })); // the stale answer arrives after the change
      await Promise.resolve();
      await Promise.resolve();
    });
    expect(result.current.get.data?.enabled).toBe(true);
  });

  it("sends exactly the fields of one change and writes the answered view into the cache", async () => {
    const d = browserDaemon((m, p) => {
      if (m === "browser.settings.get") return browserView();
      if (m === "browser.settings.set") return browserView({ enabled: (p as { enabled?: boolean }).enabled === true, allowHosts: ["a.example"] });
      return undefined;
    });
    const { result } = renderHookWithDaemon(() => ({ get: useBrowserSettings(1), set: useSetBrowserSettings(1) }), d, { initialProps: undefined });
    await waitFor(() => expect(result.current.get.data).toBeDefined());
    await act(async () => {
      await result.current.set.mutateAsync({ enabled: true });
    });
    await act(async () => {
      await result.current.set.mutateAsync({ allowAdd: ["a.example"] });
    });
    await act(async () => {
      await result.current.set.mutateAsync({ allowRemove: ["a.example"] });
    });
    const sets = d.calls.filter((c) => c.method === "browser.settings.set").map((c) => c.params);
    expect(sets).toEqual([
      { workspaceId: 1, enabled: true },
      { workspaceId: 1, allowAdd: ["a.example"] },
      { workspaceId: 1, allowRemove: ["a.example"] },
    ]);
    expect(result.current.get.data?.allowHosts).toEqual(["a.example"]);
  });

  it("passes the daemon's refusal text through as the error", async () => {
    const d = browserDaemon((m) => {
      if (m === "browser.settings.get") return browserView();
      throw new RpcError("browser.settings.set: wildcards are not allowed", "invalid");
    });
    const { result } = renderHookWithDaemon(() => useSetBrowserSettings(1), d, { initialProps: undefined });
    await expect(result.current.mutateAsync({ allowAdd: ["*.example"] })).rejects.toThrow("wildcards are not allowed");
  });

  it("browser.data.clear sends the workspace and re-reads the view", async () => {
    const d = browserDaemon((m) => (m === "browser.settings.get" ? browserView() : {}));
    const { result } = renderHookWithDaemon(() => ({ get: useBrowserSettings(3), clear: useClearBrowserData(3) }), d, { initialProps: undefined });
    await waitFor(() => expect(result.current.get.data).toBeDefined());
    await act(async () => {
      await result.current.clear.mutateAsync();
    });
    expect(d.calls.find((c) => c.method === "browser.data.clear")?.params).toEqual({ workspaceId: 3 });
    await waitFor(() => expect(d.calls.filter((c) => c.method === "browser.settings.get").length).toBe(2));
  });
});
