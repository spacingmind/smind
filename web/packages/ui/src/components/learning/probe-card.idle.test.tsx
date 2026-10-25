import { act } from "react";
import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { estimate, runningProbe } from "@/test/probe-fixtures";
import { flush, renderProbeCard } from "@/test/probe-render";
import { renderWithDaemon } from "@/test/render-with-daemon";
import { learningDaemon } from "@/test/learning-test-client";
import { ProbeCard } from "@/components/learning/probe-card";

describe("ProbeCard idle", () => {
  it("is collapsed by default and asks the daemon for nothing but the probe", async () => {
    const d = learningDaemon(() => null);
    renderWithDaemon(<ProbeCard id={9} workspaceId={1} />, d);
    await flush();
    expect(screen.getByTestId("probe-toggle").getAttribute("aria-expanded")).toBe("false");
    expect(screen.queryByTestId("probe-body")).toBeNull();
    expect(d.countCalls("learning.probe.estimate")).toBe(0);
  });

  it("shows the estimate and expected duration on the run button", async () => {
    await renderProbeCard();
    const run = screen.getByTestId("probe-run") as HTMLButtonElement;
    expect(run.textContent).toBe("Run probe (about $1.44, about 8 min)");
    expect(run.disabled).toBe(false);
    expect(screen.queryByTestId("probe-reasons")).toBeNull();
  });

  it("start sends the proposal id and no quick flag by default", async () => {
    const v = await renderProbeCard({ start: runningProbe() });
    await act(async () => screen.getByTestId("probe-run").click());
    await flush();
    expect(v.calls.find((c) => c.method === "learning.probe.start")?.params).toEqual({ id: 9 });
  });

  it("the quick toggle re-queries the estimate with quick:true and starts a quick probe", async () => {
    const v = await renderProbeCard({ start: runningProbe() });
    await act(async () => screen.getByRole("radio", { name: /Quick/ }).click());
    await flush();
    const estimates = v.calls.filter((c) => c.method === "learning.probe.estimate").map((c) => c.params);
    expect(estimates).toEqual([{ id: 9, quick: false }, { id: 9, quick: true }]);
    await act(async () => screen.getByTestId("probe-run").click());
    await flush();
    expect(v.calls.find((c) => c.method === "learning.probe.start")?.params).toEqual({ id: 9, quick: true });
  });

  it("keeps the start block on screen while the estimate for the other size loads (no flash to Estimating)", async () => {
    let release: (e: ReturnType<typeof estimate>) => void = () => {};
    const d = learningDaemon((method, params) => {
      if (method === "learning.probe.get") return null;
      if (method === "learning.probe.estimate") {
        return (params as { quick: boolean }).quick ? new Promise((resolve) => (release = resolve)) : estimate();
      }
      return null;
    });
    renderWithDaemon(<ProbeCard id={9} workspaceId={1} />, d);
    await flush();
    await act(async () => screen.getByTestId("probe-toggle").click());
    await flush();
    await act(async () => screen.getByRole("radio", { name: /Quick/ }).click());
    await flush();
    expect(screen.getByTestId("probe-start")).toBeTruthy();
    expect(screen.queryByText("Estimating…")).toBeNull();
    await act(async () => release(estimate({ quick: true })));
    await flush();
    expect(screen.getByTestId("probe-start")).toBeTruthy();
  });

  it("a failed start shows the daemon's message and keeps the button", async () => {
    await renderProbeCard({ start: new Error("Another probe is running.") });
    await act(async () => screen.getByTestId("probe-run").click());
    await flush();
    expect(screen.getByTestId("probe-start-error").textContent).toBe("Another probe is running.");
    expect(screen.getByTestId("probe-run")).toBeTruthy();
  });

  it("uses a lower estimate for a quick probe", async () => {
    await renderProbeCard({ estimate: estimate({ quick: true, arms: 2, estUSD: 0.96, expectedMinutes: 5 }) });
    expect(screen.getByTestId("probe-run").textContent).toBe("Run probe (about $0.96, about 5 min)");
  });
});
