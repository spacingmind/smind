import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { UsageWindowBar } from "@/components/account-usage/usage-window-bar";
import type { UsageWindow } from "@/lib/account-usage/types";

const win = (over: Partial<UsageWindow> = {}): UsageWindow => ({ id: "five_hour", label: "5-hour", usedPct: 31, ...over });

const fill = (c: HTMLElement) => c.querySelector<HTMLElement>("[data-slot=usage-meter-fill]")!;

describe("UsageWindowBar", () => {
  it("shows label, value and a meter of the used share", () => {
    const { container } = render(<UsageWindowBar window={win()} displayAs="used" />);
    const bar = screen.getByTestId("usage-window-five_hour");
    expect(bar).toHaveTextContent("5-hour");
    expect(bar).toHaveTextContent("31%");
    const meter = screen.getByRole("progressbar", { name: "5-hour" });
    expect(meter).toHaveAttribute("aria-valuenow", "31");
    expect(fill(container).style.width).toBe("31%");
    expect(fill(container)).toHaveClass("bg-success");
  });

  it("colours warning and danger windows", () => {
    expect(fill(render(<UsageWindowBar window={win({ usedPct: 75 })} displayAs="used" />).container)).toHaveClass("bg-warning");
    expect(fill(render(<UsageWindowBar window={win({ usedPct: 95 })} displayAs="used" />).container)).toHaveClass("bg-destructive");
  });

  it("remaining mode reads '69% left' and fills with what is left", () => {
    const { container } = render(<UsageWindowBar window={win({ remainingPct: 69 })} displayAs="remaining" />);
    expect(screen.getByTestId("usage-window-five_hour")).toHaveTextContent("69% left");
    expect(fill(container).style.width).toBe("69%");
  });

  it("a null percent shows an em dash over an empty default-toned meter", () => {
    const { container } = render(<UsageWindowBar window={win({ usedPct: null })} displayAs="used" />);
    expect(screen.getByTestId("usage-window-five_hour")).toHaveTextContent("—");
    expect(screen.getByRole("progressbar")).not.toHaveAttribute("aria-valuenow");
    expect(fill(container).style.width).toBe("0%");
    expect(fill(container)).toHaveClass("bg-foreground-muted");
  });

  it("prints the reset countdown, and 'resetting now' once due", () => {
    const soon = new Date(Date.now() + 2 * 3_600_000 + 60_000).toISOString();
    const { unmount } = render(<UsageWindowBar window={win({ resetsAt: soon })} displayAs="used" />);
    expect(screen.getByTestId("usage-window-five_hour")).toHaveTextContent("resets 2h");
    unmount();
    render(<UsageWindowBar window={win({ resetsAt: new Date(Date.now() - 1000).toISOString() })} displayAs="used" />);
    expect(screen.getByTestId("usage-window-five_hour")).toHaveTextContent("resetting now");
  });

  it("does not throw on a reshaped window", () => {
    const weird = { id: "x" } as unknown as UsageWindow;
    render(<UsageWindowBar window={weird} displayAs="remaining" />);
    expect(screen.getByTestId("usage-window-x")).toHaveTextContent("—");
  });
});
