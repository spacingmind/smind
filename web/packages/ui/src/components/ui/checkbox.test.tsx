import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { Checkbox } from "@/components/ui/checkbox";

describe("checkbox", () => {
  it("Checkbox toggles and reports checked state", () => {
    const onCheckedChange = vi.fn();
    render(<Checkbox aria-label="Agree" onCheckedChange={onCheckedChange} />);

    const box = screen.getByRole("checkbox", { name: "Agree" });
    expect(box).toHaveAttribute("aria-checked", "false");
    fireEvent.click(box);
    expect(box).toHaveAttribute("aria-checked", "true");
    expect(onCheckedChange).toHaveBeenCalledWith(true);
  });

  it("Checkbox is keyboard operable (Space) and focusable", () => {
    const onCheckedChange = vi.fn();
    render(<Checkbox aria-label="Agree" onCheckedChange={onCheckedChange} />);

    const box = screen.getByRole("checkbox", { name: "Agree" });
    box.focus();
    expect(box).toHaveFocus();
    // jsdom does not synthesize the click a real browser fires when Space
    // activates a <button>, so the keydown and the activation it produces
    // are both dispatched here.
    fireEvent.keyDown(box, { key: " " });
    fireEvent.click(box);
    expect(box).toHaveAttribute("aria-checked", "true");
    expect(onCheckedChange).toHaveBeenCalledWith(true);
  });

  it("Checkbox disabled does not toggle and exposes its hint as title", () => {
    const onCheckedChange = vi.fn();
    render(<Checkbox aria-label="Read" disabled hint="Reading is always allowed" onCheckedChange={onCheckedChange} />);

    const box = screen.getByRole("checkbox", { name: "Read" });
    fireEvent.click(box);
    expect(onCheckedChange).not.toHaveBeenCalled();
    expect(box).toHaveAttribute("aria-checked", "false");
    expect(box).toHaveAttribute("title", "Reading is always allowed");
  });

  it("a label click toggles the checkbox", () => {
    const onCheckedChange = vi.fn();
    render(
      <>
        <Checkbox id="c" onCheckedChange={onCheckedChange} />
        <label htmlFor="c">Allow cleartext</label>
      </>
    );

    fireEvent.click(screen.getByText("Allow cleartext"));
    expect(screen.getByRole("checkbox", { name: "Allow cleartext" })).toHaveAttribute("aria-checked", "true");
    expect(onCheckedChange).toHaveBeenCalledWith(true);
  });

  it("checked and disabled keeps aria-checked true", () => {
    render(<Checkbox aria-label="Read" checked disabled />);
    expect(screen.getByRole("checkbox", { name: "Read" })).toHaveAttribute("aria-checked", "true");
  });
});
