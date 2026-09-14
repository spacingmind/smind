import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { StatusDot } from "@/components/ui/status-dot";

describe("StatusDot", () => {
  it("renders with the status-tinted dot class for the given status", () => {
    render(<StatusDot status="success" data-testid="dot" />);
    expect(screen.getByTestId("dot")).toHaveClass("bg-status-dot-success");
  });

  it("running gets a pulse animation, the other statuses don't", () => {
    render(<StatusDot status="running" data-testid="running-dot" />);
    render(<StatusDot status="danger" data-testid="danger-dot" />);
    expect(screen.getByTestId("running-dot")).toHaveClass("animate-pulse");
    expect(screen.getByTestId("danger-dot")).not.toHaveClass("animate-pulse");
  });

  it("neutral doesn't reach for a fifth status hue -- it's the plain muted scale", () => {
    render(<StatusDot status="neutral" data-testid="dot" />);
    const el = screen.getByTestId("dot");
    expect(el.className).not.toMatch(/status-dot-(success|danger|warning|running)/);
  });
});
