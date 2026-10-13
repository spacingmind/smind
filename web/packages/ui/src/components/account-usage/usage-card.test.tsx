import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { UsageCard } from "@/components/account-usage/usage-card";
import type { AccountUsage, UsageCooldown, UsageWindow } from "@/lib/account-usage/types";
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

  describe("passive cooldown", () => {
    const MIN = 60_000;
    const cooldown = (over: Partial<UsageCooldown> = {}): UsageCooldown => ({
      until: new Date(FIXTURE_NOW + 10 * MIN).toISOString(),
      scope: "account",
      reason: "rejected",
      ...over,
    });
    const observed = (over: Partial<UsageWindow> = {}): UsageWindow => ({
      id: "five_hour",
      label: "5-hour",
      usedPct: 100,
      source: "observed",
      status: "rejected",
      ...over,
    });
    const observedOnly = (over: Partial<AccountUsage> = {}) =>
      claudeAccount({
        report: { status: "available", planLabel: "max", windows: [observed()], fetchedAt: new Date(FIXTURE_NOW).toISOString() },
        cooldown: cooldown({ window: "five_hour" }),
        ...over,
      });
    const untilClock = (ms: number) =>
      new Intl.DateTimeFormat(undefined, { hour: "numeric", minute: "2-digit" }).format(new Date(ms));

    it("card_shows_rate_limited_until_and_from_recent_traffic_only_when_no_fresh_fetch", () => {
      card(observedOnly());
      const box = screen.getByTestId("usage-cooldown");
      expect(screen.getByTestId("usage-cooldown-until").parentElement).toHaveTextContent(
        `Rate limited until ${untilClock(FIXTURE_NOW + 10 * MIN)} · 5-hour window`,
      );
      expect(screen.getByTestId("usage-cooldown-note")).toHaveTextContent("seen on a proxied request");
      expect(screen.getByTestId("usage-cooldown-badge")).toHaveTextContent("Rate limited");
      expect(screen.getByTestId("usage-cooldown-badge")).toHaveAttribute("data-status", "danger");
      // The badge sits under the title row and above the windows.
      expect(box.compareDocumentPosition(screen.getByTestId("usage-window-five_hour"))).toBe(
        Node.DOCUMENT_POSITION_FOLLOWING,
      );
      expect(screen.getByTestId("usage-window-five_hour")).toHaveTextContent("from recent traffic");
      cleanup();
      // A fresh fetch alone: no cooldown, no observed label.
      card(claudeAccount());
      expect(screen.queryByTestId("usage-cooldown")).toBeNull();
      expect(screen.queryByText("from recent traffic")).toBeNull();
    });

    it("cooldown_badge_disappears_at_until_without_reload", () => {
      card(claudeAccount({ cooldown: cooldown() }));
      expect(screen.getByTestId("usage-cooldown")).toBeInTheDocument();
      act(() => {
        vi.advanceTimersByTime(10 * MIN - 1);
      });
      expect(screen.getByTestId("usage-cooldown")).toBeInTheDocument();
      act(() => {
        vi.advanceTimersByTime(1);
      });
      expect(screen.queryByTestId("usage-cooldown")).toBeNull();
      // The windows stay.
      expect(screen.getByTestId("usage-window-five_hour")).toHaveTextContent("31%");
    });

    it("the cooldown timer is one timeout, replaced on change and cleared on unmount", () => {
      const before = vi.getTimerCount();
      const { rerender, unmount } = card(claudeAccount({ cooldown: cooldown() }));
      expect(vi.getTimerCount()).toBe(before + 1);
      rerender(
        <UsageCard
          usage={claudeAccount({ cooldown: cooldown({ until: new Date(FIXTURE_NOW + 20 * MIN).toISOString() }) })}
          displayAs="used"
          refresh={idle}
        />,
      );
      expect(vi.getTimerCount()).toBe(before + 1);
      act(() => {
        vi.advanceTimersByTime(10 * MIN);
      });
      expect(screen.getByTestId("usage-cooldown")).toBeInTheDocument();
      unmount();
      expect(vi.getTimerCount()).toBe(before);
    });

    it("a cooldown arriving after render shows without a reload", () => {
      const { rerender } = card(claudeAccount());
      expect(screen.queryByTestId("usage-cooldown")).toBeNull();
      rerender(<UsageCard usage={claudeAccount({ cooldown: cooldown() })} displayAs="used" refresh={idle} />);
      expect(screen.getByTestId("usage-cooldown")).toBeInTheDocument();
      rerender(<UsageCard usage={claudeAccount({ cooldown: null })} displayAs="used" refresh={idle} />);
      expect(screen.queryByTestId("usage-cooldown")).toBeNull();
    });

    it("cooldown_hidden_when_expired_or_malformed", () => {
      const cases: unknown[] = [
        cooldown({ until: new Date(FIXTURE_NOW - MIN).toISOString() }),
        cooldown({ until: new Date(FIXTURE_NOW).toISOString() }),
        { scope: "account", reason: "rejected" },
        { until: "soon" },
        { until: 42 },
        "rate limited",
        null,
      ];
      for (const bad of cases) {
        const { unmount } = card(claudeAccount({ cooldown: bad as UsageCooldown }));
        expect(screen.queryByTestId("usage-cooldown")).toBeNull();
        expect(root()).toHaveTextContent("Claude");
        unmount();
      }
    });

    it("cooldown_model_scope_and_fallback_wording", () => {
      card(claudeAccount({ cooldown: cooldown({ scope: "model", model: "claude-opus-4", reason: "fallback" }) }));
      expect(screen.getByTestId("usage-cooldown-until")).toHaveTextContent("claude-opus-4 only · reset time unknown");
      cleanup();
      card(claudeAccount({ cooldown: cooldown({ scope: "model", model: "claude-opus-4", window: "weekly_model" }) }));
      expect(screen.getByTestId("usage-cooldown-until")).toHaveTextContent("weekly limit for claude-opus-4");
      expect(screen.getByTestId("usage-cooldown-until")).not.toHaveTextContent("reset time unknown");
      cleanup();
      card(claudeAccount({ cooldown: cooldown({ window: "weekly" }) }));
      expect(screen.getByTestId("usage-cooldown-until")).toHaveTextContent("weekly window");
    });

    it("cooldown_model_text_is_escaped_and_truncated", () => {
      const evil = `<img src=x onerror="alert(1)">${"x".repeat(300)}`;
      const { container } = card(claudeAccount({ cooldown: cooldown({ scope: "model", model: evil }) }));
      expect(container.querySelector("img")).toBeNull();
      const line = screen.getByTestId("usage-cooldown-until");
      expect(line).toHaveTextContent('<img src=x onerror="alert(1)">');
      expect(line).toHaveTextContent("…");
      expect(line.textContent ?? "").not.toContain("x".repeat(100));
      expect((line.textContent ?? "").length).toBeLessThan(120);
    });

    it("observed_window_status_tone_without_percent", () => {
      const windows = [
        observed({ id: "five_hour", usedPct: null, status: "rejected" }),
        observed({ id: "weekly", label: "Weekly", usedPct: null, status: "allowed_warning" }),
        observed({ id: "other", label: "Other", usedPct: null, status: "allowed" }),
      ];
      card(claudeAccount({ report: { status: "available", windows, fetchedAt: new Date(FIXTURE_NOW).toISOString() } }));
      const rejected = within(screen.getByTestId("usage-window-five_hour")).getByTestId("usage-window-status");
      expect(rejected).toHaveTextContent("Limit reached");
      expect(rejected).toHaveAttribute("data-tone", "danger");
      expect(rejected).toHaveClass("text-destructive");
      const warning = within(screen.getByTestId("usage-window-weekly")).getByTestId("usage-window-status");
      expect(warning).toHaveTextContent("Near limit");
      expect(warning).toHaveAttribute("data-tone", "warning");
      expect(warning).toHaveClass("text-warning");
      // No percent and nothing to say: the old em dash over an empty meter.
      expect(within(screen.getByTestId("usage-window-other")).queryByTestId("usage-window-status")).toBeNull();
      expect(screen.getByTestId("usage-window-other")).toHaveTextContent("—");
      // The status word replaces the empty meter; the percent-less "allowed" row keeps it.
      expect(within(screen.getByTestId("usage-window-five_hour")).queryByRole("progressbar")).toBeNull();
      expect(within(screen.getByTestId("usage-window-other")).getByRole("progressbar")).toBeInTheDocument();
    });

    it("a percent wins over the status word", () => {
      const windows = [observed({ usedPct: 40, status: "rejected" })];
      card(claudeAccount({ report: { status: "available", windows, fetchedAt: new Date(FIXTURE_NOW).toISOString() } }));
      const row = screen.getByTestId("usage-window-five_hour");
      expect(row).toHaveTextContent("40%");
      expect(within(row).queryByTestId("usage-window-status")).toBeNull();
    });

    it("observed_label_not_shown_for_fetched_windows", () => {
      const windows = [
        { id: "five_hour", label: "5-hour", usedPct: 31, source: "fetched", status: "allowed" },
        { id: "weekly", label: "Weekly", usedPct: 54 },
      ];
      card(claudeAccount({ report: { status: "available", windows, fetchedAt: new Date(FIXTURE_NOW).toISOString() } }));
      expect(screen.queryByText("from recent traffic")).toBeNull();
      expect(screen.queryByTestId("usage-window-observed")).toBeNull();
    });

    it("long names wrap or truncate instead of widening the card", () => {
      const model = "claude-opus-4-with-an-unreasonably-long-model-identifier-1234567890";
      card(
        claudeAccount({
          displayName: "A very long account display name that keeps going and going",
          cooldown: cooldown({ scope: "model", model, window: "weekly_model", reason: "fallback" }),
        }),
      );
      expect(screen.getByTestId("usage-cooldown-until")).toHaveClass("break-words", "min-w-0");
      expect(screen.getByText(/A very long account/)).toHaveClass("truncate");
    });
  });
});
