import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { UsageMeter } from "@/components/account-usage/usage-meter";

function fillOf(container: HTMLElement): HTMLElement {
  return container.querySelector<HTMLElement>("[data-slot=usage-meter-fill]")!;
}

describe("UsageMeter", () => {
  it("fills to the percent and colours by tone", () => {
    const { container } = render(<UsageMeter percent={31} tone="ok" />);
    expect(fillOf(container).style.width).toBe("31%");
    expect(fillOf(container)).toHaveClass("bg-success");
  });

  it("maps warning, danger and default tones to distinct fills", () => {
    expect(fillOf(render(<UsageMeter percent={80} tone="warning" />).container)).toHaveClass("bg-warning");
    expect(fillOf(render(<UsageMeter percent={95} tone="danger" />).container)).toHaveClass("bg-destructive");
    expect(fillOf(render(<UsageMeter percent={5} tone="default" />).container)).toHaveClass("bg-foreground-muted");
  });

  it("clamps out-of-range and non-finite percents", () => {
    expect(fillOf(render(<UsageMeter percent={140} tone="ok" />).container).style.width).toBe("100%");
    expect(fillOf(render(<UsageMeter percent={-3} tone="ok" />).container).style.width).toBe("0%");
    expect(fillOf(render(<UsageMeter percent={Number.NaN} tone="ok" />).container).style.width).toBe("0%");
  });

  it("quiet drops the tone colour", () => {
    const { container } = render(<UsageMeter percent={50} tone="danger" quiet />);
    expect(fillOf(container)).not.toHaveClass("bg-destructive");
  });
});
