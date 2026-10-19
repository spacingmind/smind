import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { LoadingRows } from "@/components/ui/loading-rows";

describe("LoadingRows", () => {
  it("is one polite status named Loading…, with skeleton rows hidden from assistive tech", () => {
    const { container } = render(<LoadingRows rows={4} />);
    const status = screen.getByRole("status");
    expect(status).toHaveAttribute("aria-busy", "true");
    expect(screen.getByText("Loading…")).toBeInTheDocument();
    const rows = container.querySelectorAll('[data-slot="skeleton"]');
    expect(rows).toHaveLength(4);
    rows.forEach((row) => expect(row).toHaveAttribute("aria-hidden", "true"));
  });

  it("takes a custom label", () => {
    render(<LoadingRows label="Loading agents…" />);
    expect(screen.getByText("Loading agents…")).toBeInTheDocument();
  });
});
