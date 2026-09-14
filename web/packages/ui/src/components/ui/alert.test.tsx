import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { Alert } from "@/components/ui/alert";

describe("Alert", () => {
  it("renders a title and description", () => {
    render(<Alert variant="warning" title="Heads up" description="Something needs attention" />);
    expect(screen.getByText("Heads up")).toBeInTheDocument();
    expect(screen.getByText("Something needs attention")).toBeInTheDocument();
  });

  it("error gets role=alert; every other variant gets role=status", () => {
    const { rerender } = render(<Alert testId="a" variant="error" description="failed" />);
    expect(screen.getByRole("alert")).toBe(screen.getByTestId("a"));

    for (const variant of ["default", "info", "success", "warning"] as const) {
      rerender(<Alert testId="a" variant={variant} description="ok" />);
      expect(screen.getByRole("status")).toBe(screen.getByTestId("a"));
    }
  });

  it("renders children as the action slot", () => {
    render(
      <Alert variant="warning" title="Conflict">
        <button>Reload</button>
      </Alert>,
    );
    expect(screen.getByRole("button", { name: "Reload" })).toBeInTheDocument();
  });
});
