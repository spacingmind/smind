import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { AccountUsageLines } from "@/components/providers/account-usage-lines";
import { claudeAccount, problemAccount } from "@/test/account-usage-fixtures";
import type { AccountUsage } from "@/lib/account-usage/types";

const NOW = Date.parse("2026-10-12T12:00:00Z");

describe("AccountUsageLines", () => {
  it("usage lines show the summary windows with reset and the freshness", () => {
    const usage = claudeAccount({}, NOW);
    render(<AccountUsageLines usage={usage} displayAs="used" now={NOW} />);
    const lines = screen.getByTestId("account-usage-lines");
    expect(lines.querySelectorAll("[data-testid^='usage-window-']")).toHaveLength(2);
    expect(lines).toHaveTextContent("5-hour");
    expect(lines).toHaveTextContent("Weekly");
    expect(lines).toHaveTextContent(/resets |resetting now/);
    expect(screen.getByTestId("account-usage-fresh").textContent).toMatch(/ago/);
  });

  it("usage lines fall back to the first two windows", () => {
    const usage = claudeAccount(
      {
        report: {
          ...claudeAccount({}, NOW).report!,
          windows: [
            { id: "a", label: "A", usedPct: 10 },
            { id: "b", label: "B", usedPct: 20 },
            { id: "c", label: "C", usedPct: 30 },
          ],
        },
      },
      NOW,
    );
    render(<AccountUsageLines usage={usage} displayAs="used" now={NOW} />);
    const lines = screen.getByTestId("account-usage-lines");
    expect(lines).toHaveTextContent("A");
    expect(lines).toHaveTextContent("B");
    expect(lines).not.toHaveTextContent("C");
  });

  it("usage lines render nothing without an available report", () => {
    const { container } = render(
      <>
        <AccountUsageLines usage={problemAccount("expired", {}, NOW)} displayAs="used" now={NOW} />
        <AccountUsageLines usage={{ ...claudeAccount({}, NOW), report: undefined } as AccountUsage} displayAs="used" now={NOW} />
      </>,
    );
    expect(container.querySelector("[data-testid='account-usage-lines']")).toBeNull();
  });

  it("usage lines survive reshaped reports", () => {
    const usage = {
      ...claudeAccount({}, NOW),
      report: { ...claudeAccount({}, NOW).report!, windows: null, balances: [42] },
    } as unknown as AccountUsage;
    render(<AccountUsageLines usage={usage} displayAs="used" now={NOW} />);
    expect(screen.getByTestId("account-usage-lines")).toBeInTheDocument();
    expect(screen.getByTestId("account-usage-fresh")).toBeInTheDocument();
  });
});
