import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { InlineSpinner } from "@/components/ui/inline-spinner";

describe("InlineSpinner", () => {
  it("renders the given label next to the spinner", () => {
    render(<InlineSpinner label="Loading runs…" />);
    expect(screen.getByRole("status")).toHaveTextContent("Loading runs…");
  });

  it("renders with no label -- a bare spinner next to something with its own label", () => {
    render(<InlineSpinner />);
    expect(screen.getByRole("status")).toHaveTextContent("");
  });
});
