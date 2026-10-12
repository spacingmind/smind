import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { UsageRefreshAllButton, UsageRefreshButton } from "@/components/account-usage/usage-refresh-button";
import { REFRESH_COOLDOWN_MS } from "@/hooks/use-account-usage";
import { claudeAccount } from "@/test/account-usage-fixtures";

const NOW = Date.parse("2026-10-12T12:00:00Z");

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(NOW);
});
afterEach(() => vi.useRealTimers());

describe("UsageRefreshAllButton", () => {
  it("fires onRefresh and is labelled", () => {
    const onRefresh = vi.fn();
    render(<UsageRefreshAllButton busy={false} onRefresh={onRefresh} />);
    fireEvent.click(screen.getByRole("button", { name: "Refresh all" }));
    expect(onRefresh).toHaveBeenCalledTimes(1);
  });

  it("busy shows a spinner and disables", () => {
    render(<UsageRefreshAllButton busy onRefresh={vi.fn()} />);
    expect(screen.getByTestId("usage-refresh-all")).toBeDisabled();
    expect(screen.getByRole("status")).toBeInTheDocument();
  });
});

describe("UsageRefreshButton", () => {
  const usage = claudeAccount({}, NOW);

  it("is labelled with the account and refreshes on click", () => {
    const onRefresh = vi.fn();
    render(<UsageRefreshButton usage={usage} state="idle" onRefresh={onRefresh} />);
    const button = screen.getByRole("button", { name: "Refresh Claude" });
    expect(button).toBeEnabled();
    fireEvent.click(button);
    expect(onRefresh).toHaveBeenCalledTimes(1);
  });

  it("pending shows a spinner and disables", () => {
    render(<UsageRefreshButton usage={usage} state="pending" onRefresh={vi.fn()} />);
    expect(screen.getByTestId("usage-refresh")).toBeDisabled();
    expect(screen.getByRole("status")).toBeInTheDocument();
  });

  it("refresh_button_disabled_30s_after_press", () => {
    render(<UsageRefreshButton usage={usage} state="idle" pressedAt={NOW} onRefresh={vi.fn()} />);
    expect(screen.getByTestId("usage-refresh")).toBeDisabled();
    act(() => {
      vi.advanceTimersByTime(REFRESH_COOLDOWN_MS - 1);
    });
    expect(screen.getByTestId("usage-refresh")).toBeDisabled();
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(screen.getByTestId("usage-refresh")).toBeEnabled();
  });

  it("a press older than the cooldown does not disable", () => {
    render(<UsageRefreshButton usage={usage} state="idle" pressedAt={NOW - REFRESH_COOLDOWN_MS} onRefresh={vi.fn()} />);
    expect(screen.getByTestId("usage-refresh")).toBeEnabled();
  });

  it("clears its timer on unmount", () => {
    const { unmount } = render(<UsageRefreshButton usage={usage} state="idle" pressedAt={NOW} onRefresh={vi.fn()} />);
    unmount();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("tooltip says when the report was fetched while enabled", () => {
    render(<UsageRefreshButton usage={usage} state="idle" onRefresh={vi.fn()} />);
    fireEvent.focus(screen.getByTestId("usage-refresh-hint"));
    expect(screen.getByRole("tooltip")).toHaveTextContent("Updated 3m ago");
  });

  it.each([
    ["backoff", "Rate limited, retry in 12 min"],
    ["floor", "Refreshed just now"],
    ["budget", "Refresh limit reached, next in 12 min"],
    ["rejected", "Reconnect this account to refresh"],
  ] as const)("refresh_button_disabled_with_reason_when_daemon_reports_refresh_blocked (%s)", (blocked, text) => {
    const blockedUsage = claudeAccount({ refreshBlocked: blocked, nextRefreshAt: new Date(NOW + 12 * 60_000).toISOString() }, NOW);
    render(<UsageRefreshButton usage={blockedUsage} state="idle" onRefresh={vi.fn()} />);
    expect(screen.getByTestId("usage-refresh")).toBeDisabled();
    fireEvent.focus(screen.getByTestId("usage-refresh-hint"));
    expect(screen.getByRole("tooltip")).toHaveTextContent(text);
  });
});
