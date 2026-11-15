import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { AutoAcceptToggle } from "@/components/pickers/auto-accept-toggle";

/** The pill's lucide glyph classes, e.g. "lucide lucide-check-check". */
function iconClasses(): string {
  return document.querySelector('[data-testid="composer-auto-accept"] svg')?.getAttribute("class") ?? "";
}

describe("AutoAcceptToggle pill", () => {
  it("auto-accept-chip-states-on-or-off-in-text", () => {
    const { unmount } = render(<AutoAcceptToggle checked={false} onChange={() => {}} variant="pill" />);
    const off = screen.getByTestId("composer-auto-accept");
    expect(off).toHaveAttribute("aria-pressed", "false");
    expect(off).toHaveAttribute("aria-label", "Auto-accept: off");
    expect(off).toHaveTextContent("Auto-accept: off");
    unmount();

    render(<AutoAcceptToggle checked onChange={() => {}} variant="pill" />);
    const on = screen.getByTestId("composer-auto-accept");
    expect(on).toHaveAttribute("aria-pressed", "true");
    expect(on).toHaveAttribute("aria-label", "Auto-accept: on");
    expect(on).toHaveTextContent("Auto-accept: on");
  });

  it("chip-state-never-by-colour-alone", () => {
    const { unmount } = render(<AutoAcceptToggle checked={false} onChange={() => {}} variant="pill" />);
    const off = screen.getByTestId("composer-auto-accept");
    const offIcon = iconClasses();
    const offText = off.textContent;
    expect(offIcon).toContain("lucide-minus");
    unmount();

    render(<AutoAcceptToggle checked onChange={() => {}} variant="pill" />);
    const on = screen.getByTestId("composer-auto-accept");
    const onIcon = iconClasses();
    expect(onIcon).toContain("lucide-check-check");
    expect(onIcon).not.toBe(offIcon);
    expect(on.textContent).not.toBe(offText);
  });
});
