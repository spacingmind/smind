import { act, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { SettingsScreen } from "@/components/settings/settings-screen";
import { useUsageReports } from "@/hooks/use-usage-reports";
import { claudeAccount, codexAccount } from "@/test/account-usage-fixtures";
import { renderHookWithDaemon, renderWithDaemon } from "@/test/render-with-daemon";
import { FakeUsageDaemon } from "@/test/usage-test-client";

/**
 * ADR-0028 / ADR-0032 AC9: provider usage endpoints are rate-limited, so only an explicit surface may
 * send `touch` and only a button may send `refresh`. Everything below must leave both counts at zero.
 */
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

async function settle(ms = 0): Promise<void> {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
}

function expectNoProviderCalls(daemon: FakeUsageDaemon): void {
  expect(daemon.count("touch")).toBe(0);
  expect(daemon.count("refresh")).toBe(0);
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

describe("usage call budget", () => {
  it.each(["providers", "appearance", undefined])("opening settings performs zero usage refreshes (section %s)", async (sectionId) => {
    const daemon = new FakeUsageDaemon([claudeAccount(), codexAccount()]);
    renderWithDaemon(
      <SettingsScreen client={daemon.client} events={daemon.events} onNavigateBack={() => {}} initialSectionId={sectionId} />,
      daemon,
    );
    await settle();
    expect(screen.getByTestId("settings-screen")).toBeInTheDocument();
    expectNoProviderCalls(daemon);
    expect(daemon.count("cached")).toBeLessThanOrEqual(1);

    // Time passing, the window losing and regaining focus, and the tab hiding change nothing.
    await settle(TTL_MS * 3);
    setWindow({ focused: false });
    setWindow({ focused: true });
    setWindow({ visible: false });
    setWindow({ visible: true });
    await settle(TTL_MS);
    expectNoProviderCalls(daemon);
    expect(daemon.count("cached")).toBeLessThanOrEqual(1);
  });

  it("a reading surface never touches or refreshes across focus, blur, visibility, remount and events", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    const first = renderHookWithDaemon(() => useUsageReports({ touch: false }), daemon, { initialProps: null });
    await settle();
    expect(daemon.modes()).toEqual(["cached"]);

    setWindow({ focused: false });
    setWindow({ focused: true });
    setWindow({ visible: false });
    setWindow({ visible: true });
    await settle(TTL_MS * 3);

    // An account event and a re-render re-read nothing and send nothing.
    await act(async () => daemon.emit("account.updated", { account: { id: 1, label: "renamed" } }));
    await settle();
    first.rerender();
    await settle();
    expectNoProviderCalls(daemon);
    // account.updated invalidates the usage query: at most one more *cached* read, never a provider call.
    expect(daemon.modes().every((mode) => mode === "cached")).toBe(true);
    expect(daemon.count("cached")).toBeLessThanOrEqual(2);

    // Closing the surface sends nothing either.
    const requests = daemon.requests.length;
    first.unmount();
    expect(daemon.requests).toHaveLength(requests);
  });

  it("a second reading surface on the same cache sends no request", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    const first = renderHookWithDaemon(() => [useUsageReports(), useUsageReports()] as const, daemon, { initialProps: null });
    await settle();
    expect(daemon.modes()).toEqual(["cached"]);
    expect(first.result.current[1].snapshot.reports).toHaveLength(1);
  });

  it("a failed query is not retried and never falls back to touch or refresh", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    daemon.failWith = new Error("boom");
    const view = renderHookWithDaemon(() => useUsageReports(), daemon, { initialProps: null });
    await settle(TTL_MS);
    expect(daemon.requests).toHaveLength(1);
    expect(view.result.current.snapshot.phase).toBe("error");
    expect(view.result.current.snapshot.error).toBe("boom");
    setWindow({ focused: false });
    setWindow({ focused: true });
    await settle(TTL_MS);
    expect(daemon.requests).toHaveLength(1);

    // "Try again" is one more cached read, nothing else.
    daemon.failWith = null;
    await act(async () => view.result.current.retry());
    await settle();
    expect(daemon.modes()).toEqual(["cached", "cached"]);
    expect(view.result.current.snapshot.phase).toBe("ready");
  });

  it("a failed touch with nothing on screen shows the error and retries only as cached", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    daemon.failWith = new Error("boom");
    const view = renderHookWithDaemon(() => useUsageReports({ touch: true }), daemon, { initialProps: null });
    await settle();
    expect(daemon.modes()).toEqual(["touch"]);
    expect(view.result.current.snapshot.phase).toBe("error");
    daemon.failWith = null;
    await act(async () => view.result.current.retry());
    await settle();
    expect(daemon.modes()).toEqual(["touch", "cached"]);
    expect(view.result.current.snapshot.phase).toBe("ready");
  });

  it("a disabled reader (a chat whose provider has no plan windows) sends nothing", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    renderHookWithDaemon(() => useUsageReports({ enabled: false }), daemon, { initialProps: null });
    await settle(TTL_MS);
    expect(daemon.requests).toHaveLength(0);
  });
});
