import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { UsageList } from "@/components/account-usage/usage-list";
import { claudeAccount, codexAccount } from "@/test/account-usage-fixtures";

const props = { displayAs: "used" as const, refreshing: new Set<number>(), failed: new Set<number>(), pressedAt: {}, onRefreshOne: vi.fn() };

describe("UsageList", () => {
  it("renders one card per account", () => {
    render(<UsageList {...props} reports={[claudeAccount(), codexAccount()]} />);
    expect(screen.getByTestId("usage-report-1")).toHaveTextContent("Claude");
    expect(screen.getByTestId("usage-report-2")).toHaveTextContent("Codex");
    expect(screen.getAllByRole("button", { name: /^Refresh / })).toHaveLength(2);
  });

  it("refreshes the pressed account only", () => {
    const onRefreshOne = vi.fn();
    render(<UsageList {...props} onRefreshOne={onRefreshOne} reports={[claudeAccount(), codexAccount()]} />);
    fireEvent.click(screen.getByRole("button", { name: "Refresh Codex" }));
    expect(onRefreshOne).toHaveBeenCalledExactlyOnceWith(2);
  });

  it("maps refreshing, failed and pressedAt per account", () => {
    render(
      <UsageList
        {...props}
        refreshing={new Set([1])}
        failed={new Set([2])}
        pressedAt={{ 2: Date.now() }}
        reports={[claudeAccount(), codexAccount()]}
      />,
    );
    expect(screen.getByRole("button", { name: "Refresh Claude" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Refresh Codex" })).toBeDisabled();
    expect(screen.getByTestId("usage-report-2")).toHaveTextContent("Unable to refresh usage");
    expect(screen.getByTestId("usage-report-1")).not.toHaveTextContent("Unable to refresh usage");
  });
});
