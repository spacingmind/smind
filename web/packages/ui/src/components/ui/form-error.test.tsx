import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { FormError } from "@/components/ui/form-error";

describe("FormError", () => {
  it("renders nothing for a null message", () => {
    const { container } = render(<FormError message={null} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("renders nothing for an undefined message", () => {
    const { container } = render(<FormError message={undefined} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("renders nothing for an empty-string message", () => {
    const { container } = render(<FormError message="" />);
    expect(container).toBeEmptyDOMElement();
  });

  it("renders the message with role=alert and a testid", () => {
    render(<FormError message="Something failed" testId="form-error" />);
    const el = screen.getByTestId("form-error");
    expect(el).toHaveRole("alert");
    expect(el).toHaveTextContent("Something failed");
  });

  it("uses text-ui-base by default and text-ui-sm for size=sm", () => {
    const { rerender } = render(<FormError message="Boom" testId="err" />);
    expect(screen.getByTestId("err")).toHaveClass("text-ui-base");
    rerender(<FormError message="Boom" testId="err" size="sm" />);
    expect(screen.getByTestId("err")).toHaveClass("text-ui-sm");
  });

  it("merges the destructive token with a custom className", () => {
    render(<FormError message="Boom" testId="err" className="mt-2" />);
    const el = screen.getByTestId("err");
    expect(el).toHaveClass("text-destructive", "mt-2");
  });
});
