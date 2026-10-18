import { act, fireEvent, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { UsageModalRoot } from "@/components/account-usage/usage-modal";
import { closeUsageModal, openUsageModal } from "@/hooks/use-usage-modal";
import { STORAGE_KEYS } from "@/lib/storage";
import { claudeAccount, codexAccount, FIXTURE_NOW, problemAccount } from "@/test/account-usage-fixtures";
import { renderWithDaemon } from "@/test/render-with-daemon";
import { FakeUsageDaemon } from "@/test/usage-test-client";

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

function setViewportWidth(width: number): void {
  Object.defineProperty(window, "innerWidth", { configurable: true, writable: true, value: width });
}

async function openModal(daemon: FakeUsageDaemon): Promise<void> {
  renderWithDaemon(<UsageModalRoot />, daemon);
  act(() => openUsageModal());
  await flush();
}

beforeEach(() => {
  setViewportWidth(1440);
  vi.spyOn(document, "hasFocus").mockReturnValue(true);
});

afterEach(() => {
  act(() => closeUsageModal());
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("UsageModalRoot", () => {
  it("no_account_usage_call_while_modal_is_closed_and_footer_off", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    renderWithDaemon(<UsageModalRoot />, daemon);
    await flush();
    expect(screen.queryByTestId("usage-modal")).not.toBeInTheDocument();
    expect(daemon.requests).toHaveLength(0);
  });

  it("wide_usage_opens_as_dialog_over_the_workspace_and_closes_back_to_it (mounts on open, unmounts after close)", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount(), codexAccount()]);
    const hash = window.location.hash;
    await openModal(daemon);
    const modal = screen.getByTestId("usage-modal");
    expect(modal).toHaveAttribute("data-slot", "dialog-content");
    expect(screen.getByRole("dialog", { name: "Usage" })).toBe(modal);
    expect(screen.getByTestId("usage-report-1")).toBeInTheDocument();
    expect(screen.getByTestId("usage-report-2")).toBeInTheDocument();

    fireEvent.keyDown(modal, { key: "Escape" });
    await flush();
    expect(screen.queryByTestId("usage-modal")).not.toBeInTheDocument();
    expect(window.location.hash).toBe(hash);
  });

  it("narrow_footer_entry_points_open_usage_as_sheet", async () => {
    setViewportWidth(390);
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    await openModal(daemon);
    const modal = screen.getByTestId("usage-modal");
    expect(modal).toHaveAttribute("data-slot", "sheet-content");
    expect(modal).toHaveAttribute("data-side", "bottom");
    expect(modal.className).toContain("max-h-[80vh]");
  });

  it("opening_modal_sends_one_touch", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    await openModal(daemon);
    expect(daemon.modes()).toEqual(["touch"]);
  });

  it("opens_on_the_accounts_reports_with_no_filter_for_one_account", async () => {
    const one = new FakeUsageDaemon([claudeAccount()]);
    await openModal(one);
    expect(screen.queryByTestId("usage-account-filter-trigger")).not.toBeInTheDocument();
    act(() => closeUsageModal());

    const two = new FakeUsageDaemon([claudeAccount(), codexAccount()]);
    await openModal(two);
    expect(screen.getByTestId("usage-account-filter-trigger")).toHaveTextContent("All accounts");
  });

  it("refreshes_one_report_from_its_card (request is that account only; other cards untouched)", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount(), codexAccount()]);
    await openModal(daemon);
    const codexBefore = screen.getByTestId("usage-report-2").textContent;

    daemon.onRequest = (p) =>
      p.mode === "refresh"
        ? [
            claudeAccount({ report: { ...claudeAccount().report!, fetchedAt: "2026-10-12T12:20:00Z", windows: [{ id: "five_hour", label: "5-hour", usedPct: 64, remainingPct: 36 }] } }),
            codexAccount(),
          ]
        : undefined;
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Refresh Claude" }));
    });
    await flush();

    expect(daemon.requests.at(-1)).toEqual({ accountIds: [1], mode: "refresh" });
    expect(within(screen.getByTestId("usage-report-1")).getByText("64%")).toBeInTheDocument();
    expect(screen.getByTestId("usage-report-2").textContent).toBe(codexBefore);
    // Pressed button is disabled for the cooldown.
    expect(screen.getByRole("button", { name: "Refresh Claude" })).toBeDisabled();
  });

  it("refresh_all_forces_a_fresh_report (exactly one forced request; plain loads never force)", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount({}), codexAccount()]);
    await openModal(daemon);
    expect(daemon.count("refresh")).toBe(0);

    daemon.onRequest = (p) =>
      p.mode === "refresh"
        ? [
            claudeAccount({ report: { ...claudeAccount().report!, fetchedAt: "2026-10-12T12:20:00Z", windows: [{ id: "five_hour", label: "5-hour", usedPct: 64, remainingPct: 36 }] } }),
            codexAccount(),
          ]
        : undefined;
    await act(async () => {
      fireEvent.click(screen.getByTestId("usage-refresh-all"));
    });
    await flush();
    expect(daemon.count("refresh")).toBe(1);
    expect(daemon.requests.at(-1)).toEqual({ mode: "refresh" });
    expect(screen.getByText("64%")).toBeInTheDocument();
  });

  it("account_picked_in_the_modal_is_the_next_opens_account_after_a_reload", async () => {
    window.localStorage.setItem(STORAGE_KEYS.usage, JSON.stringify({ displayAs: "used", accountKey: "openai-2", inSidebar: false, pins: null }));
    const daemon = new FakeUsageDaemon([claudeAccount(), codexAccount()]);
    await openModal(daemon);
    expect(screen.queryByTestId("usage-report-1")).not.toBeInTheDocument();
    expect(screen.getByTestId("usage-report-2")).toBeInTheDocument();
    expect(screen.getByTestId("usage-account-filter-trigger")).toHaveTextContent("Codex");
  });

  it("vanished_account_pick_falls_back_to_all", async () => {
    window.localStorage.setItem(STORAGE_KEYS.usage, JSON.stringify({ displayAs: "used", accountKey: "anthropic-99", inSidebar: false, pins: null }));
    const daemon = new FakeUsageDaemon([claudeAccount(), codexAccount()]);
    await openModal(daemon);
    expect(screen.getByTestId("usage-report-1")).toBeInTheDocument();
    expect(screen.getByTestId("usage-report-2")).toBeInTheDocument();
    expect(screen.getByTestId("usage-account-filter-trigger")).toHaveTextContent("All accounts");
  });

  it("tells_the_user_to_update_a_daemon_without_account_usage", async () => {
    const daemon = new FakeUsageDaemon([]);
    daemon.makeOld();
    await openModal(daemon);
    expect(screen.getByTestId("usage-unavailable")).toHaveTextContent("Update the daemon to see plan usage");
    expect(daemon.requests).toHaveLength(1);
    expect(screen.queryByTestId("usage-refresh-all")).not.toBeInTheDocument();
  });

  it("polling disabled says so and offers nothing to refresh", async () => {
    const daemon = new FakeUsageDaemon([]);
    daemon.enabled = false;
    await openModal(daemon);
    expect(screen.getByTestId("usage-disabled")).toHaveTextContent("accountUsage.enabled");
    expect(screen.queryByTestId("usage-refresh-all")).not.toBeInTheDocument();
  });

  it("each_problem_state_is_explained and an_account_that_recovers_shows_only_its_windows", async () => {
    vi.useFakeTimers({ toFake: ["Date"], now: FIXTURE_NOW });
    const daemon = new FakeUsageDaemon([
      problemAccount("expired", { accountId: 4, key: "anthropic-4", displayName: "Old" }),
      problemAccount("rejected", { accountId: 5, key: "anthropic-5", displayName: "Rej" }),
    ]);
    await openModal(daemon);
    expect(within(screen.getByTestId("usage-report-4")).getByTestId("usage-problem")).toHaveTextContent("Login expired 2h ago. Reconnect it in Providers.");
    expect(within(screen.getByTestId("usage-report-5")).getByTestId("usage-problem")).toHaveTextContent("Login rejected (HTTP 401). Reconnect it in Providers.");

    // The account recovers: a live event replaces its message with windows.
    await act(async () => daemon.emit("account.usage", { usage: claudeAccount({ accountId: 4, key: "anthropic-4", displayName: "Old", report: { ...claudeAccount().report!, fetchedAt: new Date().toISOString() } }) }));
    expect(within(screen.getByTestId("usage-report-4")).queryByTestId("usage-problem")).not.toBeInTheDocument();
    expect(within(screen.getByTestId("usage-report-4")).getByText("31%")).toBeInTheDocument();
  });

  it("refresh_failure_keeps_previous_report_and_says_so", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    await openModal(daemon);
    daemon.failWith = new Error("boom");
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Refresh Claude" }));
    });
    await flush();
    const card = screen.getByTestId("usage-report-1");
    expect(within(card).getByTestId("usage-refresh-error")).toHaveTextContent("Unable to refresh usage");
    expect(within(card).getByText("31%")).toBeInTheDocument();
  });

  it("account_usage_event_upserts_without_refetch_and_removal_drops_the_card", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount(), codexAccount()]);
    await openModal(daemon);
    const requests = daemon.requests.length;
    await act(async () => daemon.emit("account.removed", { id: 2 }));
    expect(screen.queryByTestId("usage-report-2")).not.toBeInTheDocument();
    expect(screen.getByTestId("usage-report-1")).toBeInTheDocument();
    expect(daemon.requests).toHaveLength(requests);
  });

  it("settings_control_opens_with_switch_and_percentages (Remaining flips the cards and persists)", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount()]);
    await openModal(daemon);
    expect(screen.getByText("31%")).toBeInTheDocument();

    fireEvent.click(screen.getByTestId("usage-options-menu"));
    await flush();
    const sw = screen.getByRole("switch", { name: "Show summary in sidebar" });
    expect(sw).toHaveAttribute("aria-checked", "false"); // default off
    fireEvent.click(screen.getByRole("radio", { name: "Remaining" }));
    await flush();
    expect(screen.getByText("69% left")).toBeInTheDocument();
    expect(JSON.parse(window.localStorage.getItem(STORAGE_KEYS.usage)!).displayAs).toBe("remaining");

    fireEvent.click(sw);
    expect(JSON.parse(window.localStorage.getItem(STORAGE_KEYS.usage)!).inSidebar).toBe(true);
  });

  it("controls_have_accessible_names", async () => {
    const daemon = new FakeUsageDaemon([claudeAccount(), codexAccount()]);
    await openModal(daemon);
    expect(screen.getByRole("button", { name: "Refresh all" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Usage settings" })).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Account" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Close" })).toBeInTheDocument();
  });

  it("reshaped_report_renders_without_throwing", async () => {
    const weird = {
      accountId: 9,
      key: "mystery-9",
      provider: "mystery",
      displayName: "",
      credentialType: "oauth",
      stale: false,
      nextRefreshAt: null,
      refreshBlocked: null,
      inFlight: false,
      lastError: null,
      report: { status: "available", windows: [{ id: "w", label: "Odd", usedPct: null, tone: "purple" }, null], fetchedAt: "not-a-date" },
    };
    const daemon = new FakeUsageDaemon([weird as never]);
    await openModal(daemon);
    expect(screen.getByTestId("usage-report-9")).toBeInTheDocument();
  });
});
