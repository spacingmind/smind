import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { AutomationChip } from "@/components/automations/automation-chip";

describe("AutomationChip", () => {
  // jsdom has no container queries, so this asserts the class the fix introduces.
  it("hides only the label below 18rem, keeping the icon and full aria-label (S-01)", () => {
    render(<AutomationChip task={{ automationId: 1, automationName: "Nightly deps" }} />);
    const chip = screen.getByTestId("automation-chip");
    expect(chip).toHaveAttribute("aria-label", "Started by automation: Nightly deps");
    expect(chip).toHaveAttribute("title", "Started by automation: Nightly deps");
    expect(chip.className).toContain("shrink-0");
    expect(chip.querySelector("svg")).not.toBeNull();
    const label = screen.getByText("Automation");
    expect(label.className).toContain("@max-[18rem]/sb:hidden");
  });

  it("renders nothing for a task with no automation link", () => {
    render(<AutomationChip task={{}} />);
    expect(screen.queryByTestId("automation-chip")).toBeNull();
  });
});
