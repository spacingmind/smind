import { createRef } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { Input } from "@/components/ui/input";

describe("Input on Astryx", () => {
  it("input_forwards_props_and_aria_invalid", () => {
    const seen: string[] = [];
    const onChange = vi.fn((e: { target: { value: string } }) => seen.push(e.target.value));
    const ref = createRef<HTMLInputElement>();
    render(
      <Input
        ref={ref}
        aria-label="Name"
        type="email"
        value="a@b.c"
        placeholder="who"
        className="my-extra"
        aria-invalid
        onChange={onChange}
      />,
    );
    const el = screen.getByRole("textbox", { name: "Name" }) as HTMLInputElement;
    expect(el.tagName).toBe("INPUT");
    expect(ref.current).toBe(el);
    expect(el).toHaveValue("a@b.c");
    expect(el).toHaveAttribute("type", "email");
    expect(el).toHaveAttribute("placeholder", "who");
    expect(el).toHaveAttribute("aria-invalid", "true");
    expect(el).toHaveAttribute("data-slot", "input");
    expect(document.querySelector(".my-extra")).not.toBeNull();
    fireEvent.change(el, { target: { value: "x" } });
    expect(onChange).toHaveBeenCalledTimes(1);
    expect(seen).toEqual(["x"]);
  });

  it("disabled and uncontrolled and non-text types", () => {
    const { rerender } = render(<Input aria-label="N" disabled defaultValue="d" />);
    const el = screen.getByRole("textbox", { name: "N" });
    expect(el).toHaveValue("d");
    expect(el.hasAttribute("disabled") || el.getAttribute("aria-disabled") === "true").toBe(true);
    rerender(<Input aria-label="Num" type="number" className="my-extra" />);
    const num = screen.getByRole("spinbutton", { name: "Num" });
    expect(num).toHaveClass("my-extra");
  });
});
