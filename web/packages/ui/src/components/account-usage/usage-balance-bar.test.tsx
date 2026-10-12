import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { UsageBalanceBar } from "@/components/account-usage/usage-balance-bar";
import type { UsageBalance } from "@/lib/account-usage/types";

const bal = (over: Partial<UsageBalance>): UsageBalance => ({ id: "b", label: "Extra usage", unit: "usd", ...over });
const hasMeter = (c: HTMLElement) => c.querySelector("[data-slot=usage-meter]") !== null;

describe("UsageBalanceBar", () => {
  it("usd with a limit reads '$x / $y' with a meter", () => {
    const { container } = render(<UsageBalanceBar balance={bal({ used: 12.5, limit: 50 })} />);
    expect(screen.getByTestId("usage-balance-b")).toHaveTextContent("Extra usage");
    expect(screen.getByTestId("usage-balance-b")).toHaveTextContent("$12.50 / $50.00");
    expect(container.querySelector<HTMLElement>("[data-slot=usage-meter-fill]")!.style.width).toBe("25%");
  });

  it("derives used from remaining and the limit", () => {
    render(<UsageBalanceBar balance={bal({ remaining: 40, limit: 50 })} />);
    expect(screen.getByTestId("usage-balance-b")).toHaveTextContent("$10.00 / $50.00");
  });

  it("credits without a limit read 'x left' and have no meter", () => {
    const { container } = render(<UsageBalanceBar balance={bal({ unit: "credits", remaining: 1200 })} />);
    expect(screen.getByTestId("usage-balance-b")).toHaveTextContent("1.2K left");
    expect(hasMeter(container)).toBe(false);
  });

  it("a limit with no used amount has no meter", () => {
    const { container } = render(<UsageBalanceBar balance={bal({ limit: 50 })} />);
    expect(screen.getByTestId("usage-balance-b")).toHaveTextContent("— / $50.00");
    expect(hasMeter(container)).toBe(false);
  });

  it("missing numbers read an em dash without throwing", () => {
    const { container } = render(<UsageBalanceBar balance={bal({})} />);
    expect(screen.getByTestId("usage-balance-b")).toHaveTextContent("—");
    expect(hasMeter(container)).toBe(false);
  });
});
