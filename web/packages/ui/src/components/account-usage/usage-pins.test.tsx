import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { UsageSidebarItem } from "@/components/account-usage/usage-sidebar-item";
import { UsageModalRoot } from "@/components/account-usage/usage-modal";
import { UsageWindowBar } from "@/components/account-usage/usage-window-bar";
import { listSettingsSections } from "@/components/settings/settings-registry";
import "@/components/settings/usage-section";
import { SidebarProvider } from "@/components/ui/sidebar";
import { TooltipProvider } from "@/components/ui/tooltip";
import { closeUsageModal, openUsageModal } from "@/hooks/use-usage-modal";
import { STORAGE_KEYS } from "@/lib/storage";
import { FIXTURE_NOW, claudeAccount, codexAccount } from "@/test/account-usage-fixtures";
import { FakeUsageDaemon } from "@/test/usage-test-client";

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

const stored = () => JSON.parse(window.localStorage.getItem(STORAGE_KEYS.usage)!);

beforeEach(() => {
  window.localStorage.clear();
  vi.spyOn(document, "hasFocus").mockReturnValue(true);
});
afterEach(() => {
  act(() => closeUsageModal());
  vi.restoreAllMocks();
});

describe("UsageWindowBar pinning", () => {
  const win = { id: "five_hour", label: "Session", usedPct: 31, resetsAt: new Date(Date.now() + 2 * 3_600_000 + 60_000).toISOString() };

  it("without pin it is a plain bar: no checkbox, no glyph", () => {
    render(<UsageWindowBar window={win} displayAs="used" />);
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
    expect(screen.queryByTestId("usage-pin-glyph-unpinned")).not.toBeInTheDocument();
    expect(screen.getByRole("progressbar", { name: "Session" })).toBeInTheDocument();
  });

  it("with pin the whole row is a checkbox named 'Pin <name> <window>, <value> · <reset>' with its checked state", () => {
    const onToggle = vi.fn();
    const { rerender } = render(<UsageWindowBar window={win} displayAs="used" pin={{ pinned: false, pinLabel: "Pin Work Max Session", onToggle }} />);
    const box = screen.getByRole("checkbox", { name: "Pin Work Max Session, 31% · resets 2h" });
    expect(box).not.toBeChecked();
    expect(within(box).getByTestId("usage-pin-glyph-unpinned")).toBeInTheDocument();
    fireEvent.click(box);
    expect(onToggle).toHaveBeenCalledTimes(1);

    rerender(<UsageWindowBar window={win} displayAs="used" pin={{ pinned: true, pinLabel: "Pin Work Max Session", onToggle }} />);
    expect(screen.getByRole("checkbox", { name: "Pin Work Max Session, 31% · resets 2h" })).toBeChecked();
    expect(screen.getByTestId("usage-pin-glyph-pinned")).toBeInTheDocument();
  });

  it("a pinned row carries the pinned surface; an unpinned row previews it at half strength on hover", () => {
    const pin = { pinLabel: "Pin x", onToggle: () => {} };
    const { rerender } = render(<UsageWindowBar window={win} displayAs="used" pin={{ ...pin, pinned: false }} />);
    expect(screen.getByRole("checkbox").className).toContain("hover:bg-muted/50");
    expect(screen.getByRole("checkbox").className).not.toMatch(/(^| )bg-muted( |$)/);
    rerender(<UsageWindowBar window={win} displayAs="used" pin={{ ...pin, pinned: true }} />);
    expect(screen.getByRole("checkbox").className).toMatch(/(^| )bg-muted( |$)/);
    expect(screen.getByRole("checkbox").className).not.toContain("hover:bg-muted");
  });

  it("the checkbox is a native button (focusable; the browser fires click on Space and Enter)", () => {
    const onToggle = vi.fn();
    render(<UsageWindowBar window={win} displayAs="used" pin={{ pinned: false, pinLabel: "Pin x", onToggle }} />);
    const box = screen.getByRole("checkbox");
    expect(box.tagName).toBe("BUTTON");
    box.focus();
    expect(box).toHaveFocus();
  });
});

describe("pins in the modal and Settings", () => {
  // The fixtures' reset times are fixed relative to FIXTURE_NOW; pin only
  // Date (timers stay real so the async flushes keep working), otherwise the
  // rows read "resetting now" once the wall clock passes them.
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(FIXTURE_NOW);
  });
  afterEach(() => vi.useRealTimers());

  async function openModal(daemon: FakeUsageDaemon) {
    render(<UsageModalRoot client={daemon.client} events={daemon.events} />);
    act(() => openUsageModal());
    await flush();
  }

  it("with the summary off, window rows are plain bars (no checkboxes) in the modal and Settings", async () => {
    setPrefs({ inSidebar: false });
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    await openModal(daemon);
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  });

  it("summary_in_sidebar_switch: turning it on makes every window row a pin checkbox, off makes them plain again", async () => {
    setPrefs({ inSidebar: false });
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    await openModal(daemon);
    fireEvent.click(screen.getByTestId("usage-options-menu"));
    await flush();
    fireEvent.click(screen.getByRole("switch", { name: "Show summary in sidebar" }));
    await flush();
    expect(screen.getAllByRole("checkbox")).toHaveLength(2);
    fireEvent.click(screen.getByRole("switch", { name: "Show summary in sidebar" }));
    await flush();
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
  });

  it("the default windows read as pinned; the first toggle snapshots the defaults then removes only that window", async () => {
    setPrefs({});
    const daemon = new FakeUsageDaemon([claudeAccount(), codexAccount()]);
    await openModal(daemon);
    const names = screen.getAllByRole("checkbox").map((c) => c.getAttribute("aria-label"));
    expect(names[0]).toMatch(/^Pin Claude 5-hour, 31% · resets/);
    expect(screen.getAllByRole("checkbox").every((c) => c.getAttribute("aria-checked") === "true")).toBe(true);
    expect(stored().pins).toBeNull();

    fireEvent.click(screen.getByRole("checkbox", { name: /^Pin Claude Weekly/ }));
    expect(stored().pins).toEqual([
      { provider: "anthropic", windowId: "five_hour" },
      { provider: "openai", windowId: "five_hour" },
      { provider: "openai", windowId: "weekly" },
    ]);
    expect(screen.getByRole("checkbox", { name: /^Pin Claude Weekly/ })).not.toBeChecked();
  });

  it("filtered to one account, the first toggle keeps every other provider's default pins", async () => {
    setPrefs({ accountKey: "anthropic-1" });
    const daemon = new FakeUsageDaemon([claudeAccount(), codexAccount()]);
    await openModal(daemon);
    expect(screen.queryByTestId("usage-report-2")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("checkbox", { name: /^Pin Claude Weekly/ }));
    expect(stored().pins).toEqual([
      { provider: "anthropic", windowId: "five_hour" },
      { provider: "openai", windowId: "five_hour" },
      { provider: "openai", windowId: "weekly" },
    ]);
  });

  it("a pin matches the window on every account of the provider", async () => {
    setPrefs({ pins: [] });
    const second = claudeAccount({ accountId: 3, key: "anthropic-3", displayName: "Work Max" });
    const daemon = new FakeUsageDaemon([claudeAccount(), second]);
    await openModal(daemon);
    fireEvent.click(screen.getByRole("checkbox", { name: /^Pin Claude Weekly/ }));
    expect(screen.getByRole("checkbox", { name: /^Pin Work Max Weekly/ })).toBeChecked();
    expect(stored().pins).toEqual([{ provider: "anthropic", windowId: "weekly" }]);
  });

  it("an explicit empty selection persists and does not fall back to defaults", async () => {
    setPrefs({ pins: [{ provider: "anthropic", windowId: "weekly" }] });
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    await openModal(daemon);
    fireEvent.click(screen.getByRole("checkbox", { name: /^Pin Claude Weekly/ }));
    expect(stored().pins).toEqual([]);
    expect(screen.getAllByRole("checkbox").every((c) => c.getAttribute("aria-checked") === "false")).toBe(true);
  });

  it("Used/Remaining switches the rows without touching pins", async () => {
    setPrefs({ pins: [{ provider: "anthropic", windowId: "weekly" }] });
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    await openModal(daemon);
    fireEvent.click(screen.getByTestId("usage-options-menu"));
    await flush();
    fireEvent.click(screen.getByRole("radio", { name: "Remaining" }));
    await flush();
    expect(stored()).toMatchObject({ displayAs: "remaining", pins: [{ provider: "anthropic", windowId: "weekly" }] });
    expect(screen.getByRole("checkbox", { name: /^Pin Claude Weekly, 46% left/ })).toBeChecked();
  });

  it("Settings -> Usage pins the same way and the footer follows live", async () => {
    setPrefs({ pins: [] });
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    const section = listSettingsSections().find((s) => s.id === "usage")!;
    render(
      <SidebarProvider>
        <TooltipProvider>
          <UsageSidebarItem client={daemon.client} events={daemon.events} />
        </TooltipProvider>
        {section.render({ client: daemon.client, events: daemon.events })}
      </SidebarProvider>,
    );
    await flush();
    expect(screen.getByTestId("sidebar-usage-summary")).toHaveAttribute("data-layout", "plain");
    fireEvent.click(screen.getByRole("checkbox", { name: /^Pin Claude Weekly/ }));
    expect(within(screen.getByTestId("sidebar-usage-summary")).getByTestId("sidebar-usage-pinned-window").textContent).toBe("54% wk");
  });

  it("the footer and the modal it opens share one ttl clock: together still one touch per ttl", async () => {
    vi.useFakeTimers();
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    render(
      <SidebarProvider>
        <TooltipProvider>
          <UsageSidebarItem client={daemon.client} events={daemon.events} />
        </TooltipProvider>
        <UsageModalRoot client={daemon.client} events={daemon.events} />
      </SidebarProvider>,
    );
    await act(async () => void (await vi.advanceTimersByTimeAsync(0)));
    expect(daemon.modes()).toEqual(["touch"]);
    fireEvent.click(screen.getByTestId("sidebar-usage-summary"));
    await act(async () => void (await vi.advanceTimersByTimeAsync(0)));
    expect(screen.getByTestId("usage-modal")).toBeInTheDocument();
    expect(daemon.modes()).toEqual(["touch"]);
    await act(async () => void (await vi.advanceTimersByTimeAsync(301_000)));
    expect(daemon.modes()).toEqual(["touch", "touch"]);
    vi.useRealTimers();
  });
});
