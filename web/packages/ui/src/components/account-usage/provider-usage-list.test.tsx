import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ProviderUsageList } from "@/components/account-usage/provider-usage-list";
import type { ProviderUsageRow } from "@/lib/account-usage/provider-usage-types";
import { claudeAccount } from "@/test/account-usage-fixtures";

function row(instanceId: string, extra: Partial<ProviderUsageRow> = {}): ProviderUsageRow {
  const base = claudeAccount();
  const windows = base.report?.windows?.map((w) => ({ ...w, usedPct: w.id === "five_hour" ? 42 : 7 })) ?? [];
  return {
    ...base,
    accountId: 0,
    key: `instance-${instanceId}`,
    provider: "zai",
    displayName: `Z.AI ${instanceId}`,
    report: base.report ? { ...base.report, status: "available", planLabel: "pro", windows } : null,
    instanceId,
    ...extra,
  };
}

const props = { displayAs: "used" as const, refreshing: new Set<string>(), failed: new Set<string>(), pressedAt: {} };

describe("ProviderUsageList", () => {
  it("renders one card per instance with five_hour and weekly bars", () => {
    render(<ProviderUsageList {...props} rows={[row("a"), row("b")]} onRefreshOne={vi.fn()} />);
    expect(screen.getByTestId("provider-usage-list")).toBeInTheDocument();
    const a = screen.getByTestId("provider-usage-a");
    expect(a).toHaveTextContent("Z.AI a");
    expect(a).toHaveTextContent("pro");
    expect(a).toHaveTextContent("42%");
    expect(a).toHaveTextContent("7%");
    expect(screen.getByTestId("provider-usage-b")).toBeInTheDocument();
    expect(screen.getAllByTestId("usage-window-five_hour")).toHaveLength(2);
    expect(screen.getAllByTestId("usage-window-weekly")).toHaveLength(2);
  });

  it("shows the cooldown line only while cooldownUntil is in the future", () => {
    const future = new Date(Date.now() + 3_600_000).toISOString();
    const past = new Date(Date.now() - 3_600_000).toISOString();
    render(
      <ProviderUsageList
        {...props}
        rows={[row("a", { cooldownUntil: future }), row("b", { cooldownUntil: past }), row("c", { cooldownUntil: null })]}
        onRefreshOne={vi.fn()}
      />,
    );
    expect(screen.getByTestId("provider-usage-cooldown-a")).toHaveTextContent(/^Rate limited until \d/);
    expect(screen.queryByTestId("provider-usage-cooldown-b")).toBeNull();
    expect(screen.queryByTestId("provider-usage-cooldown-c")).toBeNull();
  });

  it("refresh button calls onRefreshOne with the instance id", () => {
    const onRefreshOne = vi.fn();
    render(<ProviderUsageList {...props} rows={[row("a"), row("b")]} onRefreshOne={onRefreshOne} />);
    fireEvent.click(screen.getByRole("button", { name: "Refresh Z.AI b" }));
    expect(onRefreshOne).toHaveBeenCalledExactlyOnceWith("b");
  });

  it("renders nothing for no rows", () => {
    const { container } = render(<ProviderUsageList {...props} rows={[]} onRefreshOne={vi.fn()} />);
    expect(container).toBeEmptyDOMElement();
  });
});
