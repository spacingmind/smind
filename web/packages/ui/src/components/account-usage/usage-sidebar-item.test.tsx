import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { UsageSidebarItem } from "@/components/account-usage/usage-sidebar-item";
import { SidebarProvider } from "@/components/ui/sidebar";
import { TooltipProvider } from "@/components/ui/tooltip";
import { STORAGE_KEYS } from "@/lib/storage";
import type { UsageWindow } from "@/lib/account-usage/types";
import { claudeAccount, codexAccount, problemAccount } from "@/test/account-usage-fixtures";
import { FakeUsageDaemon } from "@/test/usage-test-client";

const openUsageModal = vi.fn();
vi.mock("@/hooks/use-usage-modal", () => ({ openUsageModal: () => openUsageModal() }));

/** Records every observed element so a test can report widths for the line and its two measuring copies. */
let observers: { callback: ResizeObserverCallback; target: Element | null }[] = [];

class FakeResizeObserver {
  private entry: { callback: ResizeObserverCallback; target: Element | null };
  constructor(callback: ResizeObserverCallback) {
    this.entry = { callback, target: null };
    observers.push(this.entry);
  }
  observe(target: Element) {
    this.entry.target = target;
  }
  unobserve() {}
  disconnect() {
    this.entry.target = null;
  }
}

/** Reports the widths of the line, the labels copy and the percents copy (the three live observers, in DOM order). */
function measure(widths: { available: number; labels: number; percents: number }): void {
  const live = observers.filter((o) => o.target?.isConnected);
  expect(live).toHaveLength(3);
  const values = [widths.available, widths.labels, widths.percents];
  act(() => {
    live.forEach((o, i) => o.callback([{ contentRect: { width: values[i] } } as ResizeObserverEntry], {} as ResizeObserver));
  });
}

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

function setPrefs(prefs: Record<string, unknown>): void {
  window.localStorage.setItem(STORAGE_KEYS.usage, JSON.stringify({ displayAs: "used", accountKey: null, inSidebar: true, pins: null, ...prefs }));
}

async function renderItem(daemon: FakeUsageDaemon, { open = true }: { open?: boolean } = {}) {
  const view = render(
    <SidebarProvider defaultOpen={open}>
      <TooltipProvider>
        <UsageSidebarItem client={daemon.client} events={daemon.events} />
      </TooltipProvider>
    </SidebarProvider>,
  );
  await flush();
  return view;
}

const row = () => screen.getByTestId("sidebar-usage-summary");

beforeEach(() => {
  window.localStorage.clear();
  observers = [];
  vi.stubGlobal("ResizeObserver", FakeResizeObserver);
  vi.spyOn(document, "hasFocus").mockReturnValue(true);
  setPrefs({});
});

afterEach(() => {
  openUsageModal.mockClear();
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("UsageSidebarItem", () => {
  it("pinned_windows_show_in_footer: each account's logo then its summary windows as percent + short label", async () => {
    await renderItem(new FakeUsageDaemon([claudeAccount(), codexAccount()]));
    expect(within(row()).getAllByTestId("sidebar-usage-source")).toHaveLength(2);
    const windows = within(row()).getAllByTestId("sidebar-usage-pinned-window");
    expect(windows.map((w) => w.textContent)).toEqual(["31% 5h", "54% wk", "7% 5h", "12% wk"]);
    expect(row()).toHaveAccessibleName("Usage: Claude 5-hour 31% used, Claude Weekly 54% used, Codex 5-hour 7% used, Codex Weekly 12% used");
  });

  it("pins replace the defaults, grouped by provider in report order", async () => {
    setPrefs({ pins: [{ provider: "openai", windowId: "weekly" }, { provider: "anthropic", windowId: "weekly" }] });
    await renderItem(new FakeUsageDaemon([claudeAccount(), codexAccount()]));
    expect(within(row()).getAllByTestId("sidebar-usage-pinned-window").map((w) => w.textContent)).toEqual(["54% wk", "12% wk"]);
  });

  it("an explicit empty selection stays empty: the row reads plain Usage", async () => {
    setPrefs({ pins: [] });
    await renderItem(new FakeUsageDaemon([claudeAccount()]));
    expect(row()).toHaveAttribute("data-layout", "plain");
    expect(row()).toHaveTextContent("Usage");
    expect(within(row()).queryByTestId("sidebar-usage-pinned-window")).not.toBeInTheDocument();
  });

  it("with no summary data (only unavailable and error accounts) the row reads plain Usage", async () => {
    await renderItem(new FakeUsageDaemon([problemAccount("expired"), problemAccount("error", { accountId: 3, key: "anthropic-3" })]));
    expect(row()).toHaveAttribute("data-layout", "plain");
    expect(row()).toHaveAccessibleName("Usage");
  });

  it("unavailable and error accounts are left out beside healthy ones", async () => {
    await renderItem(new FakeUsageDaemon([problemAccount("rejected"), codexAccount()]));
    expect(within(row()).getAllByTestId("sidebar-usage-source")).toHaveLength(1);
    expect(within(row()).getAllByTestId("sidebar-usage-pinned-window").map((w) => w.textContent)).toEqual(["7% 5h", "12% wk"]);
  });

  it("follows the account filter and falls back to all when that account is gone", async () => {
    setPrefs({ accountKey: "openai-2" });
    const first = await renderItem(new FakeUsageDaemon([claudeAccount(), codexAccount()]));
    expect(within(row()).getAllByTestId("sidebar-usage-pinned-window").map((w) => w.textContent)).toEqual(["7% 5h", "12% wk"]);
    first.unmount();
    setPrefs({ accountKey: "anthropic-99" });
    await renderItem(new FakeUsageDaemon([claudeAccount(), codexAccount()]));
    expect(within(row()).getAllByTestId("sidebar-usage-pinned-window")).toHaveLength(4);
  });

  it("colours a window by its tone; healthy meters stay neutral and a danger meter is coloured", async () => {
    const hot: UsageWindow[] = [
      { id: "five_hour", label: "5-hour", shortLabel: "5h", summary: true, usedPct: 20 },
      { id: "weekly", label: "Weekly", shortLabel: "wk", summary: true, usedPct: 96 },
      { id: "monthly", label: "Monthly", shortLabel: "mo", summary: true, usedPct: 75 },
    ];
    const account = claudeAccount();
    await renderItem(new FakeUsageDaemon([{ ...account, report: { ...account.report!, windows: hot } }]));
    measure({ available: 600, labels: 200, percents: 120 });
    const [ok, danger, warning] = within(row()).getAllByTestId("sidebar-usage-pinned-window");
    expect(ok).toHaveClass("text-foreground");
    expect(danger).toHaveClass("text-destructive");
    expect(warning).toHaveClass("text-warning");
    const fills = Array.from(row().querySelectorAll<HTMLElement>("[data-testid=sidebar-usage-meter] [data-slot=usage-meter-fill]"));
    expect(fills.map((f) => f.className.match(/bg-[\w/-]+/)?.[0])).toEqual(["bg-foreground/25", "bg-destructive", "bg-warning"]);
  });

  it("remaining mode shows the share left", async () => {
    setPrefs({ displayAs: "remaining" });
    await renderItem(new FakeUsageDaemon([claudeAccount()]));
    expect(within(row()).getAllByTestId("sidebar-usage-pinned-window").map((w) => w.textContent)).toEqual(["69% 5h", "46% wk"]);
    expect(row().getAttribute("aria-label")).toContain("69% left");
  });

  it("a window with an empty short label shows its percent alone", async () => {
    const account = claudeAccount();
    const windows: UsageWindow[] = [{ id: "rolling", label: "Rolling", shortLabel: "", summary: true, usedPct: 21 }];
    await renderItem(new FakeUsageDaemon([{ ...account, report: { ...account.report!, windows } }]));
    expect(within(row()).getByTestId("sidebar-usage-pinned-window").textContent).toBe("21%");
  });

  it("layout: labels until measured, then meters, labels or percents by the room there is, never mixed", async () => {
    await renderItem(new FakeUsageDaemon([claudeAccount()]));
    expect(row()).toHaveAttribute("data-layout", "labels");
    expect(within(row()).queryByTestId("sidebar-usage-meter")).not.toBeInTheDocument();

    measure({ available: 220, labels: 140, percents: 90 });
    expect(row()).toHaveAttribute("data-layout", "meters");
    expect(within(row()).getAllByTestId("sidebar-usage-meter")).toHaveLength(2);

    measure({ available: 160, labels: 140, percents: 90 });
    expect(row()).toHaveAttribute("data-layout", "labels");

    measure({ available: 120, labels: 140, percents: 90 });
    expect(row()).toHaveAttribute("data-layout", "percents");
    expect(within(row()).getAllByTestId("sidebar-usage-pinned-window").map((w) => w.textContent)).toEqual(["31%", "54%"]);
    // the measuring copies are hidden from assistive tech and carry no test ids
    expect(row().querySelector("[aria-hidden=true] [data-testid]")).toBeNull();
  });

  it("clicking the row opens the Usage modal", async () => {
    await renderItem(new FakeUsageDaemon([claudeAccount()]));
    fireEvent.click(row());
    expect(openUsageModal).toHaveBeenCalledTimes(1);
  });

  it("the plain row opens the modal too", async () => {
    await renderItem(new FakeUsageDaemon([problemAccount("expired")]));
    fireEvent.click(row());
    expect(openUsageModal).toHaveBeenCalledTimes(1);
  });

  it("renders nothing, and sends nothing, on the icon-collapsed rail", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    await renderItem(daemon, { open: false });
    expect(screen.queryByTestId("sidebar-usage-summary")).not.toBeInTheDocument();
    expect(daemon.requests).toHaveLength(0);
  });

  it("renders nothing when polling is disabled or the daemon lacks account.usage", async () => {
    const disabled = new FakeUsageDaemon([claudeAccount()]);
    disabled.enabled = false;
    const first = await renderItem(disabled);
    expect(screen.queryByTestId("sidebar-usage-summary")).not.toBeInTheDocument();
    first.unmount();

    const old = new FakeUsageDaemon([claudeAccount()]);
    old.makeOld();
    await renderItem(old);
    expect(screen.queryByTestId("sidebar-usage-summary")).not.toBeInTheDocument();
  });

  it("tolerates a reshaped report (null windows, missing fields) without throwing", async () => {
    const account = claudeAccount();
    const reshaped = { ...account, report: { ...account.report!, windows: [null, 4, { id: "weekly", usedPct: 40, summary: true }] } };
    await renderItem(new FakeUsageDaemon([reshaped as never]));
    expect(within(row()).getByTestId("sidebar-usage-pinned-window").textContent).toBe("40% weekly");
  });
});

describe("call budget (AC54)", () => {
  async function advance(ms: number): Promise<void> {
    await act(async () => {
      await vi.advanceTimersByTimeAsync(ms);
    });
  }

  it("a visible summary sends one touch, then at most one per ttl while the window is active", async () => {
    vi.useFakeTimers();
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    await renderItem(daemon);
    expect(daemon.modes()).toEqual(["touch"]);
    await advance(299_000);
    expect(daemon.modes()).toEqual(["touch"]);
    await advance(2_000);
    expect(daemon.modes()).toEqual(["touch", "touch"]);
    await advance(300_000);
    expect(daemon.modes()).toEqual(["touch", "touch", "touch"]);
    expect(daemon.count("refresh")).toBe(0);
  });

  it("a hidden or blurred window sends nothing and reads cached; becoming active touches only after the ttl", async () => {
    vi.useFakeTimers();
    vi.spyOn(document, "hasFocus").mockReturnValue(false);
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    await renderItem(daemon);
    expect(daemon.modes()).toEqual(["cached"]);
    await advance(10 * 300_000);
    expect(daemon.modes()).toEqual(["cached"]);

    vi.spyOn(document, "hasFocus").mockReturnValue(true);
    act(() => void window.dispatchEvent(new Event("focus")));
    await advance(0);
    expect(daemon.modes()).toEqual(["cached", "touch"]);
    // refocusing inside the ttl adds nothing
    act(() => void window.dispatchEvent(new Event("blur")));
    act(() => void window.dispatchEvent(new Event("focus")));
    await advance(1_000);
    expect(daemon.modes()).toEqual(["cached", "touch"]);
  });

  it("a live account.usage event updates the row without another request", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    await renderItem(daemon);
    const before = daemon.requests.length;
    const fresh = claudeAccount();
    const usage = {
      ...fresh,
      report: { ...fresh.report!, fetchedAt: "2026-10-13T00:00:00Z", windows: [{ id: "five_hour", label: "5-hour", shortLabel: "5h", summary: true, usedPct: 88 }] },
    };
    act(() => daemon.emit("account.usage", { usage }));
    expect(within(row()).getByTestId("sidebar-usage-pinned-window").textContent).toBe("88% 5h");
    expect(daemon.requests).toHaveLength(before);
  });
});
