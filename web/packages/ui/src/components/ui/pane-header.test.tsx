import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { PaneHeader } from "@/components/ui/pane-header";

describe("PaneHeader", () => {
  it("renders the title and the action slot", () => {
    render(<PaneHeader testId="header" title="Diff" actions={<button>Refresh</button>} />);
    const header = screen.getByTestId("header");
    expect(header).toHaveTextContent("Diff");
    expect(header.querySelector("button")).toHaveTextContent("Refresh");
  });

  it("renders an optional subtitle", () => {
    render(<PaneHeader title="Task A" subtitle="active" />);
    expect(screen.getByText("active")).toBeInTheDocument();
  });

  it("omits the action slot entirely when none is given", () => {
    const { container } = render(<PaneHeader title="Task A" />);
    // Only the title wrapper div should be present -- no empty actions div.
    expect(container.querySelectorAll('[data-slot="pane-header"] > div')).toHaveLength(1);
  });
});
