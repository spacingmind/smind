import { act } from "@testing-library/react";
import type { QueryClient } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useUsageReports } from "@/hooks/use-usage-reports";
import { createQueryClient } from "@/lib/query/query-client";
import { ConnectionClosedError } from "@/lib/ws-client";
import { renderHookWithDaemon } from "@/test/render-with-daemon";
import { claudeAccount, codexAccount } from "@/test/account-usage-fixtures";
import { FakeUsageDaemon } from "@/test/usage-test-client";

const TTL_MS = 300_000;

let visibility: "visible" | "hidden" = "visible";
let focused = true;

function setWindow(next: { visible?: boolean; focused?: boolean }): void {
  if (next.visible !== undefined) visibility = next.visible ? "visible" : "hidden";
  if (next.focused !== undefined) focused = next.focused;
  act(() => {
    document.dispatchEvent(new Event("visibilitychange"));
    window.dispatchEvent(new Event(focused ? "focus" : "blur"));
  });
}

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

function mount(daemon: FakeUsageDaemon, touch: boolean, queryClient?: QueryClient) {
  return renderHookWithDaemon(({ touch }) => useUsageReports({ touch }), daemon, {
    initialProps: { touch },
    queryClient,
  });
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

describe("call discipline (AC54)", () => {
  it("opening_modal_sends_one_touch", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    mount(daemon, true);
    await flush();
    expect(daemon.modes()).toEqual(["touch"]);
  });

  it("a surface that does not touch lists once as cached and never sends touch or refresh", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    const { rerender } = mount(daemon, false);
    await flush();
    rerender({ props: { touch: false } });
    await advance(TTL_MS * 3);
    expect(daemon.modes()).toEqual(["cached"]);
  });

  it("footer_touches_at_ttl_cadence_only_while_visible_and_focused", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    mount(daemon, true);
    await flush();
    expect(daemon.count("touch")).toBe(1);

    await advance(TTL_MS - 1_000);
    expect(daemon.count("touch")).toBe(1);
    await advance(1_000);
    expect(daemon.count("touch")).toBe(2);
    await advance(TTL_MS);
    expect(daemon.count("touch")).toBe(3);
    expect(daemon.count("refresh")).toBe(0);
    expect(daemon.count("cached")).toBe(0);
  });

  it("hidden_or_blurred_window_pauses_the_timer_and_sends_nothing", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    mount(daemon, true);
    await flush();
    expect(daemon.count("touch")).toBe(1);

    setWindow({ visible: false });
    await advance(TTL_MS * 4);
    expect(daemon.count("touch")).toBe(1);

    // Visible again but still blurred: not active, so still nothing.
    setWindow({ focused: false });
    setWindow({ visible: true });
    await advance(TTL_MS * 4);
    expect(daemon.count("touch")).toBe(1);
  });

  it("becoming_visible_touches_only_if_last_report_older_than_ttl", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    mount(daemon, true);
    await flush();
    setWindow({ focused: false });

    // Back inside the TTL: no touch on return, the remainder is waited out.
    await advance(60_000);
    setWindow({ focused: true });
    await flush();
    expect(daemon.count("touch")).toBe(1);
    await advance(TTL_MS - 60_000);
    expect(daemon.count("touch")).toBe(2);

    // Away for longer than the TTL: exactly one touch on return, no burst for the missed ticks.
    setWindow({ visible: false });
    await advance(TTL_MS * 5);
    setWindow({ visible: true });
    await flush();
    expect(daemon.count("touch")).toBe(3);
  });

  it("a surface opened in a hidden window lists as cached, then touches when the window becomes active", async () => {
    setWindow({ visible: false });
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    mount(daemon, true);
    await flush();
    expect(daemon.modes()).toEqual(["cached"]);
    setWindow({ visible: true });
    await flush();
    expect(daemon.modes()).toEqual(["cached", "touch"]);
  });

  it("closing and reopening within the TTL sends no second touch", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    const queryClient = createQueryClient();
    const first = mount(daemon, true, queryClient);
    await flush();
    first.unmount();
    await advance(10_000);
    mount(daemon, true, queryClient);
    await flush();
    expect(daemon.count("touch")).toBe(1);
  });

  it("reconnect_relists_with_cached_mode", async () => {
    const first = new FakeUsageDaemon([claudeAccount()]);
    const second = new FakeUsageDaemon([claudeAccount()]);
    const { rerender } = renderHookWithDaemon(() => useUsageReports({ touch: false }), first, { initialProps: null });
    await flush();
    rerender({ daemon: second });
    await flush();
    expect(first.modes()).toEqual(["cached"]);
    expect(second.modes()).toEqual(["cached"]);
  });

  it("a reconnect to the same daemon keeps the reports on screen while it re-reads cached", async () => {
    const first = new FakeUsageDaemon([claudeAccount()]);
    const second = new FakeUsageDaemon([claudeAccount()]);
    const { result, rerender } = renderHookWithDaemon(() => useUsageReports({ touch: false }), first, { initialProps: null });
    await flush();
    expect(result.current.snapshot.reports).toHaveLength(1);

    rerender({ daemon: second });
    // No loading flash: the cache survived the transport swap and shows until the re-read lands.
    expect(result.current.snapshot.phase).toBe("ready");
    expect(result.current.snapshot.reports).toHaveLength(1);
    await flush();
    expect(second.modes()).toEqual(["cached"]);
    expect(second.count("touch") + second.count("refresh")).toBe(0);
  });

  it("a reconnect never sends a touch by itself, even when the TTL has already elapsed", async () => {
    const first = new FakeUsageDaemon([claudeAccount()]);
    const second = new FakeUsageDaemon([claudeAccount()]);
    const { rerender } = renderHookWithDaemon(() => useUsageReports({ touch: true }), first, { initialProps: null });
    await flush();
    expect(first.modes()).toEqual(["touch"]);

    // The clock jumps past the TTL without a timer firing (a sleeping laptop), then the socket is swapped.
    vi.setSystemTime(Date.now() + TTL_MS + 5_000);
    rerender({ daemon: second });
    await flush();
    expect(second.modes()).toEqual(["cached"]);

    // The cadence resumes on its own timer, once.
    await advance(TTL_MS);
    expect(second.modes()).toEqual(["cached", "touch"]);
  });

  it("a touch that never left a closed socket does not burn the TTL", async () => {
    const first = new FakeUsageDaemon([claudeAccount()]);
    first.failWith = new ConnectionClosedError("wsclient: connection closed");
    const { rerender } = renderHookWithDaemon(() => useUsageReports({ touch: true }), first, { initialProps: null });
    await flush();
    const second = new FakeUsageDaemon([claudeAccount()]);
    rerender({ daemon: second });
    await flush();
    // The first touch failed at once on a dead socket; the TTL clock was not started by it, so the
    // next window event on the live connection may touch.
    setWindow({ focused: false });
    setWindow({ focused: true });
    await flush();
    expect(second.count("touch")).toBe(1);
  });

  it("two touching surfaces mounting together send one touch", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    renderHookWithDaemon(
      () => {
        useUsageReports({ touch: true });
        useUsageReports({ touch: true });
      },
      daemon,
      { initialProps: null },
    );
    await flush();
    expect(daemon.modes()).toEqual(["touch"]);
  });

  it("a reconnect while a touching surface is open re-lists as cached and keeps counting the TTL", async () => {
    const first = new FakeUsageDaemon([claudeAccount()]);
    const second = new FakeUsageDaemon([claudeAccount()]);
    const { rerender } = renderHookWithDaemon(() => useUsageReports({ touch: true }), first, { initialProps: null });
    await flush();
    expect(first.modes()).toEqual(["touch"]);

    await advance(60_000);
    rerender({ daemon: second });
    await flush();
    expect(second.modes()).toEqual(["cached"]);

    // The TTL runs from the first touch (240 s left), not from the reconnect.
    await advance(TTL_MS - 60_000 - 1_000);
    expect(second.modes()).toEqual(["cached"]);
    await advance(1_000);
    expect(second.modes()).toEqual(["cached", "touch"]);
  });

  it("tells_the_user_to_update_a_daemon_without_account_usage (and never asks again)", async () => {
    const daemon = new FakeUsageDaemon([]);
    daemon.makeOld();
    const { result } = mount(daemon, true);
    await flush();
    expect(result.current.snapshot.supportsUsage).toBe(false);
    await advance(TTL_MS * 3);
    expect(daemon.requests).toHaveLength(1);
    await act(async () => {
      await result.current.refreshAll();
    });
    expect(daemon.requests).toHaveLength(1);
  });

  it("polling disabled: one list, no further touches", async () => {
    const daemon = new FakeUsageDaemon([]);
    daemon.enabled = false;
    const { result } = mount(daemon, true);
    await flush();
    await advance(TTL_MS * 3);
    expect(result.current.snapshot.enabled).toBe(false);
    expect(daemon.requests).toHaveLength(1);
  });
});

describe("refresh", () => {
  it("refresh_is_sent_only_from_a_button_press, one account at a time", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount(), codexAccount()]);
    const { result } = mount(daemon, true);
    await flush();
    await advance(TTL_MS * 2);
    expect(daemon.count("refresh")).toBe(0);

    await act(async () => {
      await result.current.refreshOne(2);
    });
    expect(daemon.requests.at(-1)).toEqual({ accountIds: [2], mode: "refresh" });
    expect(daemon.count("refresh")).toBe(1);
  });

  it("refresh all sends one refresh without ids", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount(), codexAccount()]);
    const { result } = mount(daemon, false);
    await flush();
    await act(async () => {
      await result.current.refreshAll();
    });
    expect(daemon.requests.at(-1)).toEqual({ mode: "refresh" });
    expect(daemon.count("refresh")).toBe(1);
  });

  it("refresh_failure_keeps_previous_report_and_marks_the_account_failed", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    const { result } = mount(daemon, false);
    await flush();
    daemon.failWith = new Error("boom");
    await act(async () => {
      await result.current.refreshOne(1);
    });
    expect(result.current.snapshot.failed.has(1)).toBe(true);
    expect(result.current.snapshot.reports[0]?.report?.status).toBe("available");
    expect(result.current.snapshot.refreshing.has(1)).toBe(false);
  });

  it("is pending while in flight and records when it was pressed", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    const { result } = mount(daemon, false);
    await flush();
    let release!: () => void;
    daemon.gate = new Promise<void>((resolve) => (release = resolve));
    let done!: Promise<void>;
    await act(async () => {
      done = result.current.refreshOne(1);
    });
    expect(result.current.snapshot.refreshing.has(1)).toBe(true);
    expect(result.current.snapshot.refreshingAll).toBe(false);
    expect(result.current.snapshot.pressedAt[1]).toBeTypeOf("number");
    await act(async () => {
      release();
      await done;
    });
    expect(result.current.snapshot.refreshing.has(1)).toBe(false);
  });

  it("refresh all marks every account refreshing and stays per connection", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount(), codexAccount()]);
    const { result } = mount(daemon, false);
    await flush();
    let release!: () => void;
    daemon.gate = new Promise<void>((resolve) => (release = resolve));
    let done!: Promise<void>;
    await act(async () => {
      done = result.current.refreshAll();
    });
    expect(result.current.snapshot.refreshingAll).toBe(true);
    expect([...result.current.snapshot.refreshing].sort()).toEqual([1, 2]);
    await act(async () => {
      release();
      await done;
    });
    expect(result.current.snapshot.refreshingAll).toBe(false);
  });

  it("a newer report clears a failed refresh", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    const { result } = mount(daemon, false);
    await flush();
    daemon.failWith = new Error("boom");
    await act(async () => {
      await result.current.refreshOne(1);
    });
    expect(result.current.snapshot.failed.has(1)).toBe(true);
    const fresher = claudeAccount({ report: { ...claudeAccount().report!, fetchedAt: "2026-10-12T12:05:00Z" } });
    await act(async () => daemon.emit("account.usage", { usage: fresher }));
    expect(result.current.snapshot.failed.has(1)).toBe(false);
  });
});

describe("live events", () => {
  it("account_usage_event_upserts_without_refetch_and_removal_drops_the_card", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount(), codexAccount()]);
    const { result } = mount(daemon, false);
    await flush();
    const requests = daemon.requests.length;

    const fresher = claudeAccount({ report: { ...claudeAccount().report!, fetchedAt: "2026-10-12T12:05:00Z", windows: [{ id: "five_hour", label: "5-hour", usedPct: 80, remainingPct: 20 }] } });
    await act(async () => daemon.emit("account.usage", { usage: fresher }));
    expect(result.current.snapshot.reports.find((r) => r.accountId === 1)?.report?.windows?.[0]?.usedPct).toBe(80);

    await act(async () => daemon.emit("account.removed", { id: 2 }));
    expect(result.current.snapshot.reports.map((r) => r.accountId)).toEqual([1]);
    expect(daemon.requests).toHaveLength(requests);
  });

  it("an event for an account nothing knows settles with one cached read, never a touch (no ghost accounts)", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    const { result } = mount(daemon, false);
    await flush();
    await act(async () => daemon.emit("account.usage", { usage: codexAccount() }));
    await flush();
    // The daemon's own list (one account) wins: a report for an unknown account is not resurrected from the event alone.
    expect(result.current.snapshot.reports.map((r) => r.accountId)).toEqual([1]);
    expect(daemon.modes()).toEqual(["cached", "cached"]);
  });

  it("a new account appears in plan usage without reload (ADR-0032 AC8)", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    const { result } = mount(daemon, false);
    await flush();
    daemon.reports = [claudeAccount(), codexAccount()];
    await act(async () => daemon.emit("account.updated", { account: { id: 2, provider: "openai", label: "codex", credentialType: "oauth", createdAt: "", updatedAt: "" } }));
    await flush();
    expect(result.current.snapshot.reports.map((r) => r.accountId)).toEqual([1, 2]);
    expect(daemon.count("touch") + daemon.count("refresh")).toBe(0);
  });

  it("older_fetchedat_never_replaces_newer (a refresh racing an event)", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    const { result } = mount(daemon, false);
    await flush();
    const newer = claudeAccount({ report: { ...claudeAccount().report!, fetchedAt: "2026-10-12T12:30:00Z", planLabel: "newer" } });
    const older = claudeAccount({ report: { ...claudeAccount().report!, fetchedAt: "2026-10-12T12:10:00Z", planLabel: "older" } });
    await act(async () => daemon.emit("account.usage", { usage: newer }));
    await act(async () => daemon.emit("account.usage", { usage: older }));
    expect(result.current.snapshot.reports[0]?.report?.planLabel).toBe("newer");
  });

  it("garbage event payloads are ignored", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    const { result } = mount(daemon, false);
    await flush();
    await act(async () => {
      daemon.emit("account.usage", null);
      daemon.emit("account.usage", { usage: 3 });
      daemon.emit("account.removed", { id: "x" });
    });
    expect(result.current.snapshot.reports).toHaveLength(1);
  });
});
