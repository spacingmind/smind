import { act } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useUsageReports } from "@/hooks/use-usage-reports";
import { useUsageStartFetch } from "@/hooks/use-usage-start-fetch";
import { createQueryClient } from "@/lib/query/query-client";
import { queryKeys } from "@/lib/query-keys";
import { RpcError } from "@/lib/ws-client";
import type { UsageQueryData } from "@/lib/account-usage/types";
import { claudeAccount, codexAccount, usageResult } from "@/test/account-usage-fixtures";
import { renderHookWithDaemon } from "@/test/render-with-daemon";
import { FakeUsageDaemon } from "@/test/usage-test-client";

const TEN_MINUTES = 10 * 60_000;

let visibility: "visible" | "hidden" = "visible";
let focused = true;

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

async function advance(ms: number): Promise<void> {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
}

function mountStart(daemon: FakeUsageDaemon, queryClient = createQueryClient()) {
  const view = renderHookWithDaemon(() => useUsageStartFetch(), daemon, { initialProps: null, queryClient });
  return { ...view, queryClient };
}

beforeEach(() => {
  vi.useFakeTimers();
  visibility = "visible";
  focused = true;
  vi.spyOn(document, "visibilityState", "get").mockImplementation(() => visibility);
  vi.spyOn(document, "hasFocus").mockImplementation(() => focused);
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("start fetch (ADR-0028 section 3 amendment)", () => {
  it("quota_start_fetch_sends_one_touch_at_app_start", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    mountStart(daemon);
    await flush();
    expect(daemon.requests).toEqual([{ mode: "touch" }]);
    expect(daemon.requests).toHaveLength(1);
    expect(daemon.requests[0]).not.toHaveProperty("accountIds");

    await advance(TEN_MINUTES);
    expect(daemon.requests).toHaveLength(1);
  });

  it("quota_start_fetch_is_not_repeated_on_remount_rerender_or_reconnect", async () => {
    const first = new FakeUsageDaemon([claudeAccount()]);
    const second = new FakeUsageDaemon([claudeAccount()]);
    const { rerender, unmount, queryClient } = mountStart(first);
    await flush();
    expect(first.requests).toHaveLength(1);

    rerender();
    await flush();
    // A reconnect: a new client object, same QueryClient.
    rerender({ daemon: second });
    await flush();
    unmount();
    mountStart(second, queryClient);
    await flush();
    await advance(TEN_MINUTES);

    expect(first.requests).toHaveLength(1);
    expect(second.requests).toHaveLength(0);
    expect(first.count("cached") + second.count("cached")).toBe(0);

    // A failed first touch leaves the cache cold, so only the one-shot guard keeps it from repeating.
    const failing = new FakeUsageDaemon([claudeAccount()]);
    failing.failWith = new Error("boom");
    const next = new FakeUsageDaemon([claudeAccount()]);
    const cold = mountStart(failing);
    await flush();
    expect(failing.requests).toHaveLength(1);
    cold.rerender();
    cold.rerender({ daemon: next });
    await flush();
    cold.unmount();
    mountStart(next, cold.queryClient);
    await flush();
    await advance(TEN_MINUTES);
    expect(failing.requests).toHaveLength(1);
    expect(next.requests).toHaveLength(0);
  });

  it("quota_start_fetch_skipped_when_the_cache_is_warm", async () => {
    const earlier = new FakeUsageDaemon([claudeAccount()]);
    const queryClient = createQueryClient();
    // A usage surface fills the cache with a touch inside the TTL (its own, not the start fetch).
    const surface = renderHookWithDaemon(() => useUsageReports({ touch: true }), earlier, {
      initialProps: null,
      queryClient,
    });
    await flush();
    expect(earlier.count("touch")).toBe(1);
    expect(queryClient.getQueryData<UsageQueryData>(queryKeys.accountUsage())?.reports).toHaveLength(1);
    surface.unmount();

    const daemon = new FakeUsageDaemon([claudeAccount()]);
    mountStart(daemon, queryClient);
    await flush();
    expect(daemon.requests).toHaveLength(0);
  });

  it("quota_start_fetch_skipped_while_a_touch_is_in_flight", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    let release!: () => void;
    daemon.gate = new Promise<void>((resolve) => (release = resolve));
    const queryClient = createQueryClient();
    renderHookWithDaemon(() => useUsageReports({ touch: true }), daemon, { initialProps: null, queryClient });
    await flush();
    expect(daemon.requests).toHaveLength(1);

    mountStart(daemon, queryClient);
    await flush();
    expect(daemon.requests).toHaveLength(1);
    release();
    await flush();
    expect(daemon.requests).toHaveLength(1);
  });

  it("quota_start_fetch_waits_for_an_active_window_then_fires_once", async () => {
    visibility = "hidden";
    focused = false;
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    mountStart(daemon);
    await flush();
    expect(daemon.requests).toHaveLength(0);

    // Visible but not focused yet: still waiting.
    visibility = "visible";
    act(() => {
      document.dispatchEvent(new Event("visibilitychange"));
    });
    await flush();
    expect(daemon.requests).toHaveLength(0);

    focused = true;
    act(() => {
      window.dispatchEvent(new Event("focus"));
      document.dispatchEvent(new Event("visibilitychange"));
    });
    await flush();
    expect(daemon.requests).toEqual([{ mode: "touch" }]);

    act(() => {
      window.dispatchEvent(new Event("focus"));
      document.dispatchEvent(new Event("visibilitychange"));
    });
    await flush();
    expect(daemon.requests).toHaveLength(1);
  });

  it("quota_start_fetch_is_one_request_for_all_accounts", async () => {
    const daemon = new FakeUsageDaemon([
      claudeAccount({ accountId: 1 }),
      codexAccount({ accountId: 2 }),
      claudeAccount({ accountId: 3 }),
    ]);
    mountStart(daemon);
    await flush();
    expect(daemon.requests).toHaveLength(1);
    expect(daemon.requests[0]?.accountIds).toBeUndefined();
  });

  it("quota_start_fetch_stops_when_usage_is_disabled_or_unsupported", async () => {
    const disabled = new FakeUsageDaemon([]);
    disabled.enabled = false;
    const a = mountStart(disabled);
    await flush();
    expect(disabled.requests).toHaveLength(1);
    a.rerender();
    await advance(TEN_MINUTES);
    expect(disabled.requests).toHaveLength(1);
    expect(a.queryClient.getQueryData<UsageQueryData>(queryKeys.accountUsage())?.enabled).toBe(false);

    const old = new FakeUsageDaemon([claudeAccount()]);
    old.failWith = new RpcError("method not found: account.usage");
    const b = mountStart(old);
    await flush();
    expect(old.requests).toHaveLength(1);
    expect(b.queryClient.getQueryData<UsageQueryData>(queryKeys.accountUsage())?.supportsUsage).toBe(false);
    b.rerender();
    await advance(TEN_MINUTES);
    expect(old.requests).toHaveLength(1);
  });

  it("quota_start_fetch_never_sends_refresh_or_cached", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount(), codexAccount()]);
    const second = new FakeUsageDaemon([claudeAccount()]);
    const { rerender, queryClient, unmount } = mountStart(daemon);
    await flush();
    await advance(TEN_MINUTES);
    rerender({ daemon: second });
    await advance(TEN_MINUTES);
    unmount();
    mountStart(second, queryClient);
    await advance(TEN_MINUTES);
    const all = [...daemon.requests, ...second.requests];
    expect(all.length).toBeGreaterThan(0);
    expect(all.every((r) => r.mode === "touch")).toBe(true);
    expect(usageResult([]).reports).toHaveLength(0);
  });

  it("quota_start_fetch_does_not_make_a_usage_surface_send_a_second_touch_within_the_ttl", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    const { queryClient } = mountStart(daemon);
    await flush();
    expect(daemon.count("touch")).toBe(1);

    renderHookWithDaemon(() => useUsageReports({ touch: true }), daemon, { initialProps: null, queryClient });
    await flush();
    await advance(60_000);
    expect(daemon.count("touch")).toBe(1);
    expect(daemon.count("cached")).toBe(0);
    expect(daemon.count("refresh")).toBe(0);
  });
});
