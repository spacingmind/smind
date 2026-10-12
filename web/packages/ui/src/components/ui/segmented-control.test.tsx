import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { SegmentedControl } from "@/components/ui/segmented-control";

const OPTIONS = [
  { value: "used", label: "Used" },
  { value: "remaining", label: "Remaining" },
] as const;

function setup(value: "used" | "remaining" = "used") {
  const onChange = vi.fn();
  render(<SegmentedControl label="Percentages" options={[...OPTIONS]} value={value} onChange={onChange} />);
  return onChange;
}

describe("segmented_control_a11y", () => {
  it("is a named group whose segments report the selected state", () => {
    setup("remaining");
    expect(screen.getByRole("group", { name: "Percentages" })).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: "Remaining" })).toHaveAttribute("aria-checked", "true");
    expect(screen.getByRole("radio", { name: "Used" })).toHaveAttribute("aria-checked", "false");
  });

  it("selects another segment on click", () => {
    const onChange = setup("used");
    fireEvent.click(screen.getByRole("radio", { name: "Remaining" }));
    expect(onChange).toHaveBeenCalledWith("remaining");
  });

  it("never clears the choice when the selected segment is pressed again", () => {
    const onChange = setup("used");
    fireEvent.click(screen.getByRole("radio", { name: "Used" }));
    expect(onChange).not.toHaveBeenCalled();
  });
});
