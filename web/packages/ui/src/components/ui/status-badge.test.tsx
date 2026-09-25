import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { StatusBadge } from "@/components/ui/status-badge";

describe("StatusBadge", () => {
  it("renders its label with the status-tinted classes", () => {
    render(<StatusBadge status="warning">Needs review</StatusBadge>);
    const badge = screen.getByText("Needs review");
    expect(badge).toHaveClass("text-warning");
  });

  it("optionally renders a matching StatusDot before the label", () => {
    render(
      <StatusBadge status="running" dot>
        Running
      </StatusBadge>,
    );
    const badge = screen.getByText("Running").closest('[data-slot="status-badge"]')!;
    expect(badge.querySelector('[data-slot="status-dot"]')).toHaveAttribute("data-status", "running");
  });

  it("renders no dot by default", () => {
    render(<StatusBadge status="success">Ok</StatusBadge>);
    const badge = screen.getByText("Ok").closest('[data-slot="status-badge"]')!;
    expect(badge.querySelector('[data-slot="status-dot"]')).not.toBeInTheDocument();
  });
});
