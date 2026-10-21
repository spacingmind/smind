import { act } from "react";
import { screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { arm, runningProbe } from "@/test/probe-fixtures";
import { flush, renderProbeCard } from "@/test/probe-render";

describe("ProbeCard running", () => {
  afterEach(() => vi.useRealTimers());

  it("shows the three rows with status, elapsed, cost so far and a run link", async () => {
    await renderProbeCard({ probe: runningProbe() });
    const rows = screen.getAllByTestId("probe-arm");
    expect(rows.map((r) => r.getAttribute("data-role"))).toEqual(["A", "A2", "B"]);
    expect(rows.map((r) => r.textContent)).toEqual([
      expect.stringContaining("Control"),
      expect.stringContaining("Control replicate"),
      expect.stringContaining("With this skill"),
    ]);
    const [a, a2, b] = rows as [HTMLElement, HTMLElement, HTMLElement];
    expect(within(a).getByTestId("probe-arm-status").textContent).toBe("Done");
    expect(within(a).getByTestId("probe-arm-elapsed").textContent).toBe("1m 12s");
    expect(within(a).getByTestId("probe-arm-cost").textContent).toBe("$0.4100");
    expect(within(a2).getByTestId("probe-arm-status").textContent).toBe("Running");
    expect(within(a2).getByTestId("probe-arm-elapsed").textContent).toBe("58s");
    expect(within(a2).getByTestId("probe-arm-cost").textContent).toBe("$0.2100");
    // A pending arm has not started: no elapsed, no cost, no run to open.
    expect(within(b).getByTestId("probe-arm-status").textContent).toBe("Waiting");
    expect(within(b).getByTestId("probe-arm-elapsed").textContent).toBe("–");
    expect(within(b).getByTestId("probe-arm-cost").textContent).toBe("–");
    expect(b.textContent).not.toContain("run ");
    // Runs known to the card are shown as a labelled run reference.
    expect(a.textContent).toContain("run run-a");
    expect(screen.getByTestId("probe-running").textContent).toContain("2m 10s elapsed · $0.62 so far (estimate $1.44)");
  });

  it("keeps the elapsed time moving between polls (footer and the running arm, not the finished one)", async () => {
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval", "Date"] });
    await renderProbeCard({ probe: runningProbe() });
    expect(screen.getByTestId("probe-running").textContent).toContain("2m 10s elapsed");
    await act(async () => {
      vi.advanceTimersByTime(1000); // below the 2 s refetch: only the local tick fires
    });
    expect(screen.getByTestId("probe-running").textContent).toContain("2m 11s elapsed");
    const [a, a2] = screen.getAllByTestId("probe-arm") as [HTMLElement, HTMLElement];
    expect(within(a2).getByTestId("probe-arm-elapsed").textContent).toBe("59s");
    expect(within(a).getByTestId("probe-arm-elapsed").textContent).toBe("1m 12s");
  });

  it("announces progress politely and offers no start button", async () => {
    await renderProbeCard({ probe: runningProbe() });
    expect(screen.getByTestId("probe-running").getAttribute("aria-live")).toBe("polite");
    expect(screen.queryByTestId("probe-run")).toBeNull();
  });

  it("shows every non-happy arm status in words", async () => {
    await renderProbeCard({
      probe: runningProbe({ arms: [arm({ role: "A", status: "capped" }), arm({ role: "A2", status: "timeout" }), arm({ role: "B", status: "killed" })] }),
    });
    expect(screen.getAllByTestId("probe-arm-status").map((e) => e.textContent)).toEqual([
      "Stopped: cost cap",
      "Stopped: timed out",
      "Stopped",
    ]);
  });

  it("a queued probe is shown as running with its pending arms", async () => {
    await renderProbeCard({ probe: runningProbe({ status: "queued", arms: [arm({ role: "A", status: "pending" }), arm({ role: "B", status: "pending" })] }) });
    expect(screen.getAllByTestId("probe-arm")).toHaveLength(2);
    expect(screen.getByTestId("probe-cancel")).toBeTruthy();
  });

  it("Cancel calls learning.probe.cancel for the proposal", async () => {
    const v = await renderProbeCard({ probe: runningProbe() });
    await act(async () => screen.getByTestId("probe-cancel").click());
    await flush();
    expect(v.calls.find((c) => c.method === "learning.probe.cancel")?.params).toEqual({ id: 9 });
  });
});

describe("ProbeCard arm that never ran", () => {
  it("shows an arm stopped before it started as not started, not as 0s and $0.0000", async () => {
    await renderProbeCard({
      probe: runningProbe({ arms: [arm({ role: "A", status: "done" }), arm({ role: "B", status: "killed", startedAt: undefined, elapsedMs: 0, costUSD: 0 })] }),
    });
    const [, b] = screen.getAllByTestId("probe-arm") as [HTMLElement, HTMLElement];
    expect(within(b).getByTestId("probe-arm-status").textContent).toBe("Stopped");
    expect(within(b).getByTestId("probe-arm-elapsed").textContent).toBe("–");
    expect(within(b).getByTestId("probe-arm-cost").textContent).toBe("–");
  });
});

describe("ProbeCard running past the estimate", () => {
  it("says so in words and explains why on hover, in one dollar format", async () => {
    await renderProbeCard({ probe: runningProbe({ costUSD: 0.45, estUSD: 0.36 }) });
    const spend = screen.getByTestId("probe-spend");
    expect(spend.textContent).toContain("$0.45 so far, over the $0.36 estimate");
    expect(spend.textContent).not.toContain("USD");
    expect(spend.getAttribute("title")).toMatch(/twice its estimate/);
  });
  it("under the estimate there is no hover hint", async () => {
    await renderProbeCard({ probe: runningProbe({ costUSD: 0.1, estUSD: 0.36 }) });
    expect(screen.getByTestId("probe-spend").getAttribute("title")).toBeNull();
  });
});
