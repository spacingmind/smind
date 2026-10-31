import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { HomeIllustration } from "./home-illustration";

describe("HomeIllustration", () => {
  it("renders aria-hidden svg without external assets", () => {
    const { container } = render(<HomeIllustration />);
    const svg = container.querySelector("svg");
    expect(svg).toHaveAttribute("aria-hidden", "true");
    expect(container.querySelector("image")).toBeNull();
    for (const el of container.querySelectorAll("*")) {
      expect(el.getAttribute("href") ?? "").not.toMatch(/^http/);
      for (const a of ["fill", "stroke"]) {
        expect(el.getAttribute(a) ?? "").not.toMatch(/#|rgb/);
      }
    }
  });
});
