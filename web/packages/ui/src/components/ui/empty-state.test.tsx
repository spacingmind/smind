import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { EmptyState } from "@/components/ui/empty-state";

describe("EmptyState", () => {
  it("renders a title", () => {
    render(<EmptyState title="No runs yet" />);
    expect(screen.getByText("No runs yet")).toBeInTheDocument();
  });

  it("renders an optional description and a single action", () => {
    render(
      <EmptyState
        title="No workspaces yet"
        description="Create one to start"
        action={<button>New workspace</button>}
      />,
    );
    expect(screen.getByText("Create one to start")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "New workspace" })).toBeInTheDocument();
  });

  it("omits the description and action when not given", () => {
    const { container } = render(<EmptyState title="No changes" />);
    expect(container.querySelectorAll("p")).toHaveLength(1);
    expect(container.querySelector("button")).not.toBeInTheDocument();
  });
});
