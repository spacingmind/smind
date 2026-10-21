import { act } from "react";
import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { runningProbe } from "@/test/probe-fixtures";
import { flush, renderProbeCard } from "@/test/probe-render";

describe("ProbeCard error", () => {
  it("a probe that failed shows the infra verdict and its reason, quietly, with a re-run", async () => {
    await renderProbeCard({
      probe: runningProbe({ status: "failed", verdict: "probe_failed", verdictReason: "The sandbox could not start." }),
    });
    const failed = screen.getByTestId("probe-failed");
    expect(failed.textContent).toContain("Probe failed (infra)");
    expect(failed.textContent).toContain("The sandbox could not start.");
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.getByTestId("probe-run").textContent).toContain("Run probe again");
  });

  it("a failed read shows the message and Retry re-reads the probe", async () => {
    const v = await renderProbeCard({ probe: new Error("connection lost") });
    expect(screen.getByTestId("probe-error").textContent).toContain("The probe could not be loaded.");
    expect(screen.getByTestId("probe-error").textContent).not.toContain("connection lost");
    const before = v.calls.filter((c) => c.method === "learning.probe.get").length;
    await act(async () => screen.getByTestId("probe-retry").click());
    await flush();
    expect(v.calls.filter((c) => c.method === "learning.probe.get").length).toBe(before + 1);
  });

  it("a failed estimate says so instead of an empty card", async () => {
    await renderProbeCard({ estimate: new Error("estimate unavailable") });
    expect(screen.getByTestId("probe-estimate-error").textContent).toBe("estimate unavailable");
    expect(screen.queryByTestId("probe-run")).toBeNull();
  });
});
