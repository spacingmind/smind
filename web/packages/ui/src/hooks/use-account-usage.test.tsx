import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useAccountUsage } from "@/hooks/use-account-usage";
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

function mount(daemon: FakeUsageDaemon, touch: boolean) {
  return renderHook(({ touch }) => useAccountUsage(daemon.client, daemon.events, { touch }), {
    initialProps: { touch },
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
    rerender({ touch: false });
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
    const first = mount(daemon, true);
    await flush();
    first.unmount();
    await advance(10_000);
    mount(daemon, true);
    await flush();
    expect(daemon.count("touch")).toBe(1);
  });

  it("reconnect_relists_with_cached_mode", async () => {
    const first = new FakeUsageDaemon([claudeAccount()]);
    const second = new FakeUsageDaemon([claudeAccount()]);
    const { rerender } = renderHook(
      ({ d }) => useAccountUsage(d.client, d.events, { touch: false }),
      { initialProps: { d: first } },
    );
    await flush();
    rerender({ d: second });
    await flush();
    expect(first.modes()).toEqual(["cached"]);
    expect(second.modes()).toEqual(["cached"]);
  });

  it("a reconnect while a touching surface is open re-lists as cached and keeps counting the TTL", async () => {
    const first = new FakeUsageDaemon([claudeAccount()]);
    const second = new FakeUsageDaemon([claudeAccount()]);
    const { rerender } = renderHook(
      ({ d }) => useAccountUsage(d.client, d.events, { touch: true }),
      { initialProps: { d: first } },
    );
    await flush();
    expect(first.modes()).toEqual(["touch"]);

    await advance(60_000);
    rerender({ d: second });
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
      await result.current.store?.refreshAll();
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
      await result.current.store!.refreshOne(2);
    });
    expect(daemon.requests.at(-1)).toEqual({ accountIds: [2], mode: "refresh" });
    expect(daemon.count("refresh")).toBe(1);
  });

  it("refresh all sends one refresh without ids", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount(), codexAccount()]);
    const { result } = mount(daemon, false);
    await flush();
    await act(async () => {
      await result.current.store!.refreshAll();
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
      await result.current.store!.refreshOne(1);
    });
    expect(result.current.snapshot.failed.has(1)).toBe(true);
    expect(result.current.snapshot.reports[0]?.report?.status).toBe("available");
    expect(result.current.snapshot.refreshing.has(1)).toBe(false);
  });

  it("is pending while in flight and records when it was pressed", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    const { result } = mount(daemon, false);
    await flush();
    let done: Promise<void>;
    act(() => {
      done = result.current.store!.refreshOne(1);
    });
    expect(result.current.snapshot.refreshing.has(1)).toBe(true);
    expect(result.current.snapshot.pressedAt[1]).toBeTypeOf("number");
    await act(async () => {
      await done;
    });
    expect(result.current.snapshot.refreshing.has(1)).toBe(false);
  });
});

describe("live events", () => {
  it("account_usage_event_upserts_without_refetch_and_removal_drops_the_card", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount(), codexAccount()]);
    const { result } = mount(daemon, false);
    await flush();
    const requests = daemon.requests.length;

    const fresher = claudeAccount({ report: { ...claudeAccount().report!, fetchedAt: "2026-10-12T12:05:00Z", windows: [{ id: "five_hour", label: "5-hour", usedPct: 80, remainingPct: 20 }] } });
    act(() => daemon.emit("account.usage", { usage: fresher }));
    expect(result.current.snapshot.reports.find((r) => r.accountId === 1)?.report?.windows?.[0]?.usedPct).toBe(80);

    act(() => daemon.emit("account.removed", { id: 2 }));
    expect(result.current.snapshot.reports.map((r) => r.accountId)).toEqual([1]);
    expect(daemon.requests).toHaveLength(requests);
  });

  it("ignores an event for an account it does not know", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    const { result } = mount(daemon, false);
    await flush();
    act(() => daemon.emit("account.usage", { usage: codexAccount() }));
    expect(result.current.snapshot.reports.map((r) => r.accountId)).toEqual([1]);
  });

  it("older_fetchedat_never_replaces_newer (a refresh racing an event)", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    const { result } = mount(daemon, false);
    await flush();
    const newer = claudeAccount({ report: { ...claudeAccount().report!, fetchedAt: "2026-10-12T12:30:00Z", planLabel: "newer" } });
    const older = claudeAccount({ report: { ...claudeAccount().report!, fetchedAt: "2026-10-12T12:10:00Z", planLabel: "older" } });
    act(() => daemon.emit("account.usage", { usage: newer }));
    act(() => daemon.emit("account.usage", { usage: older }));
    expect(result.current.snapshot.reports[0]?.report?.planLabel).toBe("newer");
  });

  it("garbage event payloads are ignored", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    const { result } = mount(daemon, false);
    await flush();
    act(() => {
      daemon.emit("account.usage", null);
      daemon.emit("account.usage", { usage: 3 });
      daemon.emit("account.removed", { id: "x" });
    });
    expect(result.current.snapshot.reports).toHaveLength(1);
  });
});
