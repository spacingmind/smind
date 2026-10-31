import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { PlanCard } from "@/components/timeline/plan-card";
import type { PlanEntry } from "@/hooks/use-run-timeline";

describe("PlanCard", () => {
  it("shows n of m done", () => {
    render(
      <PlanCard
        items={[
          { content: "Read the code", status: "completed", priority: "high" },
          { content: "Write tests", status: "completed", priority: "medium" },
          { content: "Ship it", status: "pending", priority: "low" },
        ]}
      />,
    );
    expect(screen.getByTestId("plan-progress")).toHaveTextContent("2 of 3 done");
  });

  it("renders status and priority per entry", () => {
    render(
      <PlanCard
        items={[
          { content: "Read the code", status: "completed", priority: "low" },
          { content: "Write tests", status: "in_progress", priority: "medium" },
          { content: "Ship it", status: "pending", priority: "high" },
        ]}
      />,
    );

    const rows = screen.getAllByTestId("plan-entry");
    expect(rows.map((r) => r.getAttribute("data-status"))).toEqual(["completed", "in_progress", "pending"]);
    expect(rows.map((r) => r.getAttribute("data-priority"))).toEqual(["low", "medium", "high"]);
    // One sr-only status label per entry, matching its row.
    expect(within(rows[0]).getByText("Completed")).toBeInTheDocument();
    expect(within(rows[1]).getByText("In progress")).toBeInTheDocument();
    expect(within(rows[2]).getByText("Pending")).toBeInTheDocument();
    // The priority label is only for high.
    expect(screen.getAllByText("High")).toHaveLength(1);
    expect(within(rows[2]).getByText("High")).toBeInTheDocument();
  });

  it("collapsed when all done and open otherwise", () => {
    const details = () => screen.getByTestId("timeline-plan").querySelector("details")!;
    const { unmount } = render(<PlanCard items={[{ content: "All done", status: "completed", priority: "low" }]} />);
    expect(details().open).toBe(false);
    unmount();
    render(
      <PlanCard
        items={[
          { content: "Done", status: "completed", priority: "low" },
          { content: "Left", status: "pending", priority: "low" },
        ]}
      />,
    );
    expect(details().open).toBe(true);
  });
});
