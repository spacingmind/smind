import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { AutomationChip } from "@/components/automations/automation-chip";

describe("AutomationChip", () => {
  it("renders the name on hover", () => {
    render(<AutomationChip task={{ automationId: 3, automationName: "Nightly sweep" }} />);
    const chip = screen.getByTestId("automation-chip");
    expect(chip).toHaveTextContent("Automation");
    expect(chip).toHaveAttribute("title", "Started by automation: Nightly sweep");
    expect(chip).toHaveAccessibleName("Started by automation: Nightly sweep");
  });

  it("falls back to a nameless label", () => {
    render(<AutomationChip task={{ automationId: 3 }} />);
    expect(screen.getByTestId("automation-chip")).toHaveAttribute("title", "Started by an automation");
  });

  it("absent without a link", () => {
    const { container } = render(<AutomationChip task={{}} />);
    expect(screen.queryByTestId("automation-chip")).not.toBeInTheDocument();
    expect(container).toBeEmptyDOMElement();
  });
});
