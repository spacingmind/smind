import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { PlanUsageCompact } from "@/components/account-usage/plan-usage-compact";
import { ChatUsageTotal } from "@/components/chat-usage-total";
import { closeUsageModal } from "@/hooks/use-usage-modal";
import { STORAGE_KEYS } from "@/lib/storage";
import type { UsageCooldown, UsageWindow } from "@/lib/account-usage/types";
import { claudeAccount, FIXTURE_NOW } from "@/test/account-usage-fixtures";
import { renderWithDaemon } from "@/test/render-with-daemon";
import { FakeUsageDaemon } from "@/test/usage-test-client";

const MIN = 60_000;

const cooldown = (over: Partial<UsageCooldown> = {}): UsageCooldown => ({
  until: new Date(FIXTURE_NOW + 10 * MIN).toISOString(),
  scope: "account",
  reason: "rejected",
  window: "five_hour",
  ...over,
});

function withWindows(windows: UsageWindow[], over = {}) {
  const base = claudeAccount();
  return claudeAccount({ report: { ...base.report!, windows }, ...over });
}

const statusOnly = (over: Partial<UsageWindow> = {}): UsageWindow => ({
  id: "five_hour",
  label: "5-hour",
  shortLabel: "5h",
  summary: true,
  status: "rejected",
  source: "observed",
  ...over,
});

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(FIXTURE_NOW);
  localStorage.removeItem(STORAGE_KEYS.usage);
});

afterEach(() => {
  act(() => closeUsageModal());
  localStorage.removeItem(STORAGE_KEYS.usage);
  vi.useRealTimers();
});

describe("plan usage compact: passive cooldown and observed windows", () => {
  it("compact_card_shows_cooldown_line_and_hides_at_until", () => {
    render(<PlanUsageCompact usage={claudeAccount({ cooldown: cooldown() })} displayAs="used" />);
    const row = screen.getByTestId("plan-usage-1");
    const line = within(row).getByTestId("plan-usage-cooldown");
    expect(line).toHaveTextContent(/^Rate limited until /);
    expect(line).toHaveClass("text-destructive");
    expect(line.getAttribute("title")).toContain("seen on a proxied request");
    // Above the windows, which still render.
    expect(line.compareDocumentPosition(within(row).getByTestId("plan-usage-window-five_hour"))).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING,
    );

    act(() => {
      vi.advanceTimersByTime(10 * MIN - 1);
    });
    expect(screen.getByTestId("plan-usage-cooldown")).toBeInTheDocument();
    act(() => {
      vi.advanceTimersByTime(2);
    });
    expect(screen.queryByTestId("plan-usage-cooldown")).not.toBeInTheDocument();
    expect(screen.getByTestId("plan-usage-window-five_hour")).toHaveTextContent("31%");
  });

  it("compact_card_cooldown_malformed_or_expired_hidden", () => {
    const bad: unknown[] = [
      null,
      "soon",
      [],
      {},
      { until: 12 },
      { until: "not a date" },
      cooldown({ until: new Date(FIXTURE_NOW - MIN).toISOString() }),
      cooldown({ until: new Date(FIXTURE_NOW).toISOString() }),
    ];
    for (const value of bad) {
      const { unmount } = render(
        <PlanUsageCompact usage={claudeAccount({ cooldown: value as UsageCooldown })} displayAs="used" />,
      );
      expect(screen.queryByTestId("plan-usage-cooldown")).not.toBeInTheDocument();
      expect(screen.getByTestId("plan-usage-window-five_hour")).toBeInTheDocument();
      unmount();
    }
  });

  it("compact_card_observed_window_hint_only_for_observed", () => {
    const fetched: UsageWindow = { id: "weekly", label: "Weekly", shortLabel: "wk", summary: true, usedPct: 54, source: "fetched" };
    const plain: UsageWindow = { id: "seven_day_opus", label: "Opus", shortLabel: "opus", summary: true, usedPct: 10 };
    const observed: UsageWindow = {
      id: "five_hour",
      label: "5-hour",
      shortLabel: "5h",
      summary: true,
      usedPct: 31,
      source: "observed",
    };
    render(<PlanUsageCompact usage={withWindows([observed, fetched, plain])} displayAs="used" />);
    expect(within(screen.getByTestId("plan-usage-window-five_hour")).getByTestId("plan-usage-window-observed")).toHaveTextContent(
      "from recent traffic",
    );
    expect(within(screen.getByTestId("plan-usage-window-weekly")).queryByTestId("plan-usage-window-observed")).toBeNull();
    expect(within(screen.getByTestId("plan-usage-window-seven_day_opus")).queryByTestId("plan-usage-window-observed")).toBeNull();
    expect(screen.getAllByTestId("plan-usage-window-observed")).toHaveLength(1);
  });

  it("compact_card_status_only_window_renders_status_word", () => {
    render(
      <PlanUsageCompact
        usage={withWindows([
          statusOnly(),
          statusOnly({ id: "weekly", label: "Weekly", shortLabel: "wk", status: "allowed_warning" }),
          // Neither a percent nor a limiting status: nothing to say, not shown.
          statusOnly({ id: "weekly_opus", label: "Opus", status: "allowed" }),
        ])}
        displayAs="used"
      />,
    );
    const rejected = screen.getByTestId("plan-usage-window-five_hour");
    expect(within(rejected).getByTestId("plan-usage-window-status")).toHaveTextContent("Limit reached");
    expect(within(rejected).getByTestId("plan-usage-window-status")).toHaveAttribute("data-tone", "danger");
    expect(within(rejected).getByTestId("plan-usage-window-status")).toHaveClass("text-destructive");
    expect(within(rejected).queryByRole("progressbar")).toBeNull();
    const warning = screen.getByTestId("plan-usage-window-weekly");
    expect(within(warning).getByTestId("plan-usage-window-status")).toHaveTextContent("Near limit");
    expect(within(warning).getByTestId("plan-usage-window-status")).toHaveAttribute("data-tone", "warning");
    expect(screen.queryByTestId("plan-usage-window-weekly_opus")).not.toBeInTheDocument();
  });

  it("compact_card_never_triggers_fetch_for_cooldown", async () => {
    vi.useRealTimers();
    const daemon = new FakeUsageDaemon([
      claudeAccount({ cooldown: cooldown({ until: new Date(Date.now() + 10 * MIN).toISOString() }) }),
    ]);
    renderWithDaemon(
      <ChatUsageTotal runs={[{ usage: { inputTokens: 1, outputTokens: 1, source: "claude_result" } }]} provider="claude-native" />,
      daemon,
    );
    await act(async () => {
      for (let i = 0; i < 4; i++) await Promise.resolve();
    });
    await act(async () => {
      fireEvent.focus(screen.getByTestId("chat-usage-total"));
    });
    const card = await screen.findByTestId("chat-usage-total-popover", {}, { timeout: 3000 });
    expect(within(card).getByTestId("plan-usage-cooldown")).toHaveTextContent("Rate limited until");
    expect(daemon.requests.length).toBeGreaterThan(0);
    expect(daemon.modes().every((mode) => mode === "cached")).toBe(true);
    expect(daemon.count("touch")).toBe(0);
    expect(daemon.count("refresh")).toBe(0);
  });

  it("compact_card_long_model_name_truncates", () => {
    const model = "claude-opus-4-with-a-rather-long-model-identifier-for-wrapping";
    render(
      <PlanUsageCompact
        usage={claudeAccount({
          displayName: "A very long account display name for the narrow hover card",
          cooldown: cooldown({ scope: "model", model, window: "weekly_model" }),
        })}
        displayAs="used"
      />,
    );
    const line = screen.getByTestId("plan-usage-cooldown");
    expect(line).toHaveClass("min-w-0");
    expect(within(line).getByText(/Rate limited until/)).toHaveClass("truncate");
    // The model text is capped by the copy helper, never the raw 60+ characters.
    expect(line.getAttribute("title")).toContain("weekly limit for claude-opus-4-with-a-rather-long-model-…");
    expect(line.getAttribute("title")).not.toContain(model);
    expect(screen.getByText(/A very long account/)).toHaveClass("truncate");
  });
});
