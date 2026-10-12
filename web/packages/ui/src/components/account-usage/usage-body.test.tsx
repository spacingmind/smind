import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { UsageBody } from "@/components/account-usage/usage-body";
import type { UsageView } from "@/lib/account-usage/types";
import { claudeAccount, codexAccount } from "@/test/account-usage-fixtures";

const body = (view: UsageView, onRetry = vi.fn()) =>
  render(
    <UsageBody
      view={view}
      displayAs="used"
      refreshing={new Set()}
      failed={new Set()}
      pressedAt={{}}
      onRefreshOne={vi.fn()}
      onRetry={onRetry}
    />,
  );

describe("UsageBody", () => {
  it("unavailable and disabled show their message", () => {
    const { unmount } = body({ kind: "unavailable", message: "Update the daemon to see plan usage" });
    expect(screen.getByTestId("usage-unavailable")).toHaveTextContent("Update the daemon to see plan usage");
    unmount();
    body({ kind: "disabled", message: "Plan usage is turned off." });
    expect(screen.getByTestId("usage-disabled")).toHaveTextContent("Plan usage is turned off.");
  });

  it("loading shows the loading copy", () => {
    body({ kind: "loading" });
    expect(screen.getByRole("status")).toHaveTextContent("Loading plan usage…");
  });

  it("error shows an alert and Try again calls onRetry", () => {
    const onRetry = vi.fn();
    body({ kind: "error", message: "boom" }, onRetry);
    expect(screen.getByRole("alert")).toHaveTextContent("boom");
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  it("ready with no reports says there are none", () => {
    body({ kind: "ready", reports: [] });
    expect(screen.getByTestId("usage-empty-list")).toHaveTextContent("No accounts with plan usage yet");
  });

  it("ready lists one card per account", () => {
    body({ kind: "ready", reports: [claudeAccount(), codexAccount()] });
    expect(screen.getAllByTestId(/^usage-report-/)).toHaveLength(2);
  });
});
