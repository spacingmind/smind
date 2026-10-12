import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { UsageCard } from "@/components/account-usage/usage-card";
import type { AccountUsage, UsageWindow } from "@/lib/account-usage/types";
import { FIXTURE_NOW, claudeAccount, codexAccount, problemAccount } from "@/test/account-usage-fixtures";

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(FIXTURE_NOW);
});
afterEach(() => vi.useRealTimers());

const idle = { state: "idle" as const, onRefresh: vi.fn() };
const card = (usage: AccountUsage, refresh: Parameters<typeof UsageCard>[0]["refresh"] = idle) =>
  render(<UsageCard usage={usage} displayAs="used" refresh={refresh} />);
const root = (id = 1) => screen.getByTestId(`usage-report-${id}`);

describe("UsageCard", () => {
  it("renders an available account: name, plan, windows, refresh", () => {
    card(claudeAccount());
    expect(root()).toHaveTextContent("Claude");
    expect(root()).toHaveTextContent("max");
    expect(screen.getByTestId("usage-window-five_hour")).toHaveTextContent("31%");
    expect(screen.getByTestId("usage-window-weekly")).toHaveTextContent("54%");
    expect(screen.getByRole("button", { name: "Refresh Claude" })).toBeInTheDocument();
    expect(screen.queryByTestId("usage-problem")).toBeNull();
    expect(screen.queryByTestId("usage-provider")).toBeNull();
  });

  it("account_titles_use_the_label_and_fall_back_to_provider_default", () => {
    card(claudeAccount({ displayName: "Work" }));
    expect(root()).toHaveTextContent("Work");
    expect(screen.getByTestId("usage-provider")).toHaveTextContent("anthropic");
    card(codexAccount());
    expect(screen.queryAllByTestId("usage-provider")).toHaveLength(1);
    expect(root(2)).toHaveTextContent("Codex");
  });

  it("shows the freshness inline when there is no refresh button", () => {
    card(claudeAccount(), null);
    expect(screen.queryByTestId("usage-refresh")).toBeNull();
    expect(screen.getByTestId("usage-freshness")).toHaveTextContent("Updated 3m ago");
  });

  it("each_problem_state_is_explained", () => {
    const { unmount } = card(problemAccount("expired"));
    expect(screen.getByTestId("usage-problem")).toHaveTextContent(/Login expired .*Reconnect it in Providers/);
    expect(root()).toHaveTextContent("Unavailable");
    expect(screen.queryByTestId("usage-window-five_hour")).toBeNull();
    unmount();
    card(problemAccount("rejected"));
    expect(screen.getByTestId("usage-problem")).toHaveTextContent("Login rejected (HTTP 401). Reconnect it in Providers.");
    cleanup();
    card(problemAccount("error"));
    expect(screen.getByTestId("usage-problem")).toHaveAttribute("data-tone", "problem");
    expect(screen.getByTestId("usage-problem")).toHaveClass("text-destructive");
    cleanup();
    card(problemAccount("error"));
    expect(screen.getByTestId("usage-problem")).toHaveTextContent("Couldn't read usage: timeout, retry in 2 min");
    expect(root()).toHaveTextContent("Error");
    cleanup();
    card(claudeAccount({ report: null }));
    expect(root()).toHaveTextContent("Claude");
    expect(screen.getByTestId("usage-empty")).toHaveTextContent("—");
    expect(screen.queryByTestId("usage-problem")).toBeNull();
  });

  it("no_quota_is_information_not_a_failure (muted, no status, nothing to refresh)", () => {
    card(problemAccount("no_quota"));
    const problem = screen.getByTestId("usage-problem");
    expect(problem).toHaveTextContent("No plan usage for this account.");
    expect(problem).toHaveAttribute("data-tone", "info");
    expect(problem).toHaveClass("text-muted-foreground");
    expect(problem).not.toHaveClass("text-destructive");
    expect(root()).not.toHaveTextContent("Unavailable");
    expect(root()).not.toHaveTextContent("Error");
    expect(screen.queryByTestId("usage-refresh")).toBeNull();
    expect(screen.queryByTestId("usage-freshness")).toBeNull();
  });

  it("an_account_that_recovers_shows_only_its_windows", () => {
    const { rerender } = card(problemAccount("rejected"));
    expect(screen.getByTestId("usage-problem")).toBeInTheDocument();
    rerender(<UsageCard usage={claudeAccount()} displayAs="used" refresh={idle} />);
    expect(screen.queryByTestId("usage-problem")).toBeNull();
    expect(root()).not.toHaveTextContent("Unavailable");
    expect(screen.getByTestId("usage-window-five_hour")).toBeInTheDocument();
  });

  it("refresh_failure_keeps_previous_report_and_says_so", () => {
    card(claudeAccount(), { state: "failed", onRefresh: vi.fn() });
    expect(screen.getByTestId("usage-refresh-error")).toHaveTextContent("Unable to refresh usage");
    expect(screen.getByTestId("usage-window-five_hour")).toHaveTextContent("31%");
  });

  it("pressing refresh calls onRefresh", () => {
    const onRefresh = vi.fn();
    card(claudeAccount(), { state: "idle", onRefresh });
    fireEvent.click(screen.getByTestId("usage-refresh"));
    expect(onRefresh).toHaveBeenCalledTimes(1);
  });

  it("reshaped_report_renders_without_throwing", () => {
    const reshaped = claudeAccount({
      provider: "mystery",
      displayName: "",
      report: {
        status: "available",
        windows: [{ id: "a", label: "A", usedPct: null, tone: "purple" } as unknown as UsageWindow, { id: "b" } as UsageWindow],
        balances: "nope" as unknown as undefined,
        fetchedAt: "not a date",
      },
    });
    card(reshaped);
    expect(screen.getByTestId("usage-window-a")).toHaveTextContent("—");
    expect(screen.getByTestId("usage-window-b")).toBeInTheDocument();
    cleanup();
    card({ accountId: 1 } as unknown as AccountUsage);
    expect(root()).toBeInTheDocument();
  });

  it("codex_window_shapes_use_provider_durations_on_card", () => {
    const windows: UsageWindow[] = [
      { id: "five_hour", label: "5-hour", usedPct: 7 },
      { id: "weekly", label: "Weekly", usedPct: 12 },
      { id: "unknown_primary", label: "Primary", usedPct: 3 },
      { id: "feature:gpt-5-codex", label: "GPT-5 Codex", scope: { kind: "feature", id: "gpt-5-codex" }, usedPct: 40 },
    ];
    card(codexAccount({ report: { status: "available", planLabel: "plus", windows, fetchedAt: new Date(FIXTURE_NOW).toISOString() } }));
    for (const w of windows) expect(screen.getByTestId(`usage-window-${w.id}`)).toHaveTextContent(w.label);
    expect(screen.getAllByRole("progressbar")).toHaveLength(4);
  });

  it("renders balances and detail rows", () => {
    const usage = claudeAccount({
      report: {
        status: "available",
        windows: [],
        balances: [{ id: "extra", label: "Extra usage", unit: "usd", used: 5, limit: 20 }],
        details: [{ id: "org", label: "Organization", value: "Acme" }],
        fetchedAt: new Date(FIXTURE_NOW).toISOString(),
      },
    });
    card(usage);
    expect(screen.getByTestId("usage-balance-extra")).toHaveTextContent("$5.00 / $20.00");
    expect(screen.getByTestId("usage-detail-org")).toHaveTextContent("Acme");
  });
});
