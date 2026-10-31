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

  // jsdom has no container queries, so this asserts the class the fix introduces.
  it("hides only the label below 18rem, keeping the icon and full aria-label (S-01)", () => {
    render(<AutomationChip task={{ automationId: 1, automationName: "Nightly deps" }} />);
    const chip = screen.getByTestId("automation-chip");
    expect(chip).toHaveAttribute("aria-label", "Started by automation: Nightly deps");
    expect(chip).toHaveAttribute("title", "Started by automation: Nightly deps");
    expect(chip.className).toContain("shrink-0");
    expect(chip.querySelector("svg")).not.toBeNull();
    expect(screen.getByText("Automation").className).toContain("@max-[18rem]/sb:hidden");
  });
});
