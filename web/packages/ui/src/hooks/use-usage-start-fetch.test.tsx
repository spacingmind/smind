import { act } from "@testing-library/react";
import type { ReactElement } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useUsageReports } from "@/hooks/use-usage-reports";
import { useUsageStartFetch } from "@/hooks/use-usage-start-fetch";
import { createQueryClient } from "@/lib/query/query-client";
import { queryKeys } from "@/lib/query-keys";
import { ConnectionClosedError, RpcError } from "@/lib/ws-client";
import type { UsageQueryData } from "@/lib/account-usage/types";
import { claudeAccount, codexAccount, usageResult } from "@/test/account-usage-fixtures";
import { renderHookWithDaemon, renderWithDaemon } from "@/test/render-with-daemon";
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

  it("quota_start_fetch_and_a_touching_surface_mounted_together_send_one_touch_in_either_order", async () => {
    function StartFetchFirst() {
      useUsageStartFetch();
      useUsageReports({ touch: true });
      return null;
    }
    function SurfaceFirst() {
      useUsageReports({ touch: true });
      useUsageStartFetch();
      return null;
    }
    function FooterSummary() {
      useUsageReports({ touch: true });
      return null;
    }
    function StartFetch() {
      useUsageStartFetch();
      return null;
    }
    // The app's own shape: the sidebar summary is a child of the shell that sends the start fetch,
    // so the footer's effects run first.
    function FooterThenStartFetch() {
      return (
        <>
          <FooterSummary />
          <StartFetch />
        </>
      );
    }
    const variants: [string, () => ReactElement | null][] = [
      ["the start fetch hook is called first", StartFetchFirst],
      ["the touching surface hook is called first", SurfaceFirst],
      ["the footer component is mounted first", FooterThenStartFetch],
    ];
    for (const [order, Component] of variants) {
      // Mounted with an active window: whichever sender runs second must see the first one's
      // claim and send nothing.
      visibility = "visible";
      focused = true;
      const active = new FakeUsageDaemon([claudeAccount()]);
      renderWithDaemon(<Component />, active, { queryClient: createQueryClient() });
      await flush();
      expect(active.requests, order).toEqual([{ mode: "touch" }]);
      await advance(60_000);
      expect(active.count("touch"), order).toBe(1);

      // Mounted while the window is hidden, then activated: whichever listener fires first on
      // the activation, the two senders still share the one touch.
      visibility = "hidden";
      focused = false;
      const hidden = new FakeUsageDaemon([claudeAccount()]);
      renderWithDaemon(<Component />, hidden, { queryClient: createQueryClient() });
      await flush();
      expect(hidden.count("touch"), order).toBe(0);
      visibility = "visible";
      focused = true;
      act(() => {
        document.dispatchEvent(new Event("visibilitychange"));
        window.dispatchEvent(new Event("focus"));
      });
      await flush();
      await advance(60_000);
      expect(hidden.count("touch"), order).toBe(1);
      expect(hidden.count("refresh"), order).toBe(0);
    }
  });

  it("quota_start_fetch_is_retried_only_if_the_first_never_left", async () => {
    // A touch that never left the machine (the socket was already closed): the next connection of
    // the same app start still sends the one request, and only that one.
    const neverLeft = new FakeUsageDaemon([claudeAccount()]);
    neverLeft.failWith = new ConnectionClosedError("wsclient: connection closed");
    const reconnected = new FakeUsageDaemon([claudeAccount()]);
    const first = mountStart(neverLeft);
    await flush();
    expect(neverLeft.requests).toHaveLength(1);

    first.rerender({ daemon: reconnected });
    await flush();
    expect(reconnected.requests).toEqual([{ mode: "touch" }]);

    await advance(TEN_MINUTES);
    first.rerender();
    first.unmount();
    const remounted = mountStart(reconnected, first.queryClient);
    await flush();
    await advance(TEN_MINUTES);
    remounted.rerender();
    await flush();
    expect(neverLeft.requests).toHaveLength(1);
    expect(reconnected.requests).toHaveLength(1);

    // Any other failure leaves the one-shot mark set: no second request, ever.
    const failing = new FakeUsageDaemon([claudeAccount()]);
    failing.failWith = new Error("boom");
    const next = new FakeUsageDaemon([claudeAccount()]);
    const errored = mountStart(failing);
    await flush();
    expect(failing.requests).toHaveLength(1);

    errored.rerender({ daemon: next });
    await flush();
    await advance(TEN_MINUTES);
    errored.unmount();
    mountStart(next, errored.queryClient);
    await flush();
    await advance(TEN_MINUTES);
    expect(failing.requests).toHaveLength(1);
    expect(next.requests).toHaveLength(0);
  });
});
