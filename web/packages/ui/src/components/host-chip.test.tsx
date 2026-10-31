import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { HostChip } from "@/components/host-chip";

describe("HostChip", () => {
  it("renders the alias with an owner title", () => {
    render(<HostChip alias="box" />);
    const chip = screen.getByTestId("host-chip");
    expect(chip.textContent).toBe("box");
    expect(chip.title).toBe("Owned by box");
  });

  it("renders nothing for an empty alias", () => {
    const { container } = render(<HostChip alias="" />);
    expect(container.firstChild).toBeNull();
  });
});
