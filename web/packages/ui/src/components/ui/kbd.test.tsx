import { render } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { Kbd } from "@/components/ui/kbd";

function caps(keys: string): HTMLElement[] {
  const { container } = render(<Kbd keys={keys} />);
  return Array.from(container.querySelectorAll("kbd"));
}

describe("Kbd", () => {
  it("renders each key as a cap", () => {
    expect(caps("Ctrl+Alt+T").map((c) => c.textContent)).toEqual(["Ctrl", "Alt", "T"]);
  });

  it("renders a mac combo as one cap", () => {
    const rendered = caps("⌘⌥T");
    expect(rendered).toHaveLength(1);
    expect(rendered[0]!.textContent).toBe("⌘⌥T");
  });
});
