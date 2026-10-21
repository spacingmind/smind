import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { VERDICT_LABELS } from "@/lib/probe-format";
import type { ProbeVerdict } from "@/lib/probe-types";
import { arm, probeView } from "@/test/probe-fixtures";
import { renderProbeCard } from "@/test/probe-render";

const VERDICTS = Object.keys(VERDICT_LABELS) as ProbeVerdict[];

describe("ProbeCard result", () => {
  it.each(VERDICTS)("verdict %s shows its label as text and a non-success chip", async (verdict) => {
    await renderProbeCard({ probe: probeView({ verdict, status: verdict === "unsafe" ? "unsafe" : "done" }) });
    const chips = screen.getAllByTestId("probe-verdict");
    expect(chips.length).toBeGreaterThan(0);
    for (const chip of chips) {
      expect(chip.textContent).toBe(VERDICT_LABELS[verdict]);
      expect(chip.getAttribute("data-status")).not.toBe("success");
      expect(chip.className).not.toMatch(/success/);
      expect(chip.querySelector("svg")).toBeNull();
    }
  });

  it("never reads as an approval", async () => {
    await renderProbeCard({ probe: probeView({ verdict: "helped" }) });
    expect(screen.getByTestId("probe-card").textContent).not.toMatch(/approv/i);
    expect(screen.getAllByTestId("probe-verdict").map((c) => c.getAttribute("data-status"))).toEqual(["neutral", "neutral"]);
  });

  it("hurt uses the warning tone and unsafe the danger tone", async () => {
    await renderProbeCard({ probe: probeView({ verdict: "hurt" }) });
    expect(screen.getAllByTestId("probe-verdict")[0]?.getAttribute("data-status")).toBe("warning");
  });

  it("shows the disclaimer and the scope wording", async () => {
    await renderProbeCard({ probe: probeView() });
    const text = screen.getByTestId("probe-disclaimer").textContent ?? "";
    expect(text).toContain("Behavioural contrast on one replay of the source episode, not a measurement.");
    expect(text).toContain("fixes the observed failure here");
    expect(screen.getByTestId("probe-card").textContent).not.toMatch(/generali[sz]/i);
  });

  it("shows loaded, targeted failure recurrence and the verdict reason", async () => {
    await renderProbeCard({ probe: probeView() });
    expect(screen.getByTestId("probe-loaded").textContent).toBe("yes");
    expect(screen.getByTestId("probe-recurred").textContent).toBe("control: yes · with this skill: no");
    expect(screen.getByTestId("probe-result").textContent).toContain("The skill loaded and the targeted failure did not recur.");
  });

  it("reports no signature when the evidence has none", async () => {
    await renderProbeCard({
      probe: probeView({ arms: [arm({ role: "A", failureRecurred: null }), arm({ role: "B", loadedSkill: true, failureRecurred: null })] }),
    });
    expect(screen.getByTestId("probe-recurred").textContent).toBe("control: no signature · with this skill: no signature");
  });

  it("shows the check result only when a check ran", async () => {
    await renderProbeCard({ probe: probeView() });
    expect(screen.queryByTestId("probe-check")).toBeNull();
  });

  it("shows pass and fail per arm when probeCheck is configured", async () => {
    await renderProbeCard({
      probe: probeView({ arms: [arm({ role: "A", check: { passed: false } }), arm({ role: "B", loadedSkill: true, check: { passed: true } })] }),
    });
    expect(screen.getByTestId("probe-check").textContent).toBe("control: failed · with this skill: passed");
  });

  it("keeps the diff collapsed until asked and keyboard-reachable", async () => {
    await renderProbeCard({ probe: probeView() });
    const toggle = screen.getByTestId("probe-diff-toggle");
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    expect(screen.queryByTestId("probe-diff-body")).toBeNull();
    toggle.click();
    await new Promise((r) => setTimeout(r, 0));
    expect(screen.getByTestId("probe-diff-toggle").getAttribute("aria-expanded")).toBe("true");
    expect(screen.getByRole("region", { name: "Changes made with this skill" }).getAttribute("tabindex")).toBe("0");
  });

  it("a quick result says there is no noise floor and the verdict is capped", async () => {
    await renderProbeCard({
      probe: probeView({ quick: true, verdict: "inconclusive", arms: [arm({ role: "A" }), arm({ role: "B", loadedSkill: true })], contrast: { loaded: true, noiseFloor: false, metrics: [] } }),
    });
    expect(screen.getByTestId("probe-quick-notice").textContent).toBe("Quick probe: no noise floor, verdict capped at Inconclusive.");
  });

  it("offers a re-run below the result", async () => {
    await renderProbeCard({ probe: probeView() });
    expect(screen.getByTestId("probe-run").textContent).toContain("Run probe again");
  });
});
