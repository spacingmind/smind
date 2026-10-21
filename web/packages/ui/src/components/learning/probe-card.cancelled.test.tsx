import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { runningProbe } from "@/test/probe-fixtures";
import { renderProbeCard } from "@/test/probe-render";

describe("ProbeCard cancelled", () => {
  it("says it was cancelled, what it cost, shows no verdict and offers a re-run", async () => {
    await renderProbeCard({ probe: runningProbe({ status: "cancelled", costUSD: 0.62, elapsedMs: 130_000 }) });
    expect(screen.getByTestId("probe-cancelled").textContent).toContain("Probe cancelled after 2m 10s; $0.6200 was spent. No verdict.");
    expect(screen.queryByTestId("probe-verdict")).toBeNull();
    expect(screen.getByTestId("probe-run").textContent).toContain("Run probe again");
    expect(screen.getByTestId("probe-toggle").textContent).toContain("Cancelled");
  });
});
