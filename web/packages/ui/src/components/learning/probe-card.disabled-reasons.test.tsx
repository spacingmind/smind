import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { ProbeReasonCode } from "@/lib/probe-types";
import { estimate } from "@/test/probe-fixtures";
import { renderProbeCard } from "@/test/probe-render";

const BUTTON_REASONS: ProbeReasonCode[] = [
  "not_pending",
  "provider_not_claude_native",
  "no_prompt",
  "no_base_sha",
  "dirty_checkout",
  "continuation_run",
  "source_cost_unknown",
  "sandbox_unavailable",
  "budget",
  "quota",
  "busy",
  "learning_off",
];

describe("ProbeCard ineligible", () => {
  it.each(BUTTON_REASONS)("%s shows the daemon's message and disables the button", async (code) => {
    await renderProbeCard({ estimate: estimate({ eligible: false, reasons: [{ code, message: `Message for ${code}.` }] }) });
    expect(screen.getByTestId("probe-reasons").textContent).toBe(`Message for ${code}.`);
    expect((screen.getByTestId("probe-run") as HTMLButtonElement).disabled).toBe(true);
    // The card never invents the reason: no estimate text on a disabled button.
    expect(screen.getByTestId("probe-run").textContent).toBe("Run probe");
  });

  it.each(["platform_unsupported", "windows"] as const)("%s hides the button and only explains", async (code) => {
    await renderProbeCard({ estimate: estimate({ eligible: false, reasons: [{ code, message: `Probes are not available here (${code}).` }] }) });
    expect(screen.queryByTestId("probe-run")).toBeNull();
    expect(screen.getByTestId("probe-unsupported").textContent).toBe(`Probes are not available here (${code}).`);
  });

  it("lists every reason when there are several", async () => {
    await renderProbeCard({
      estimate: estimate({
        eligible: false,
        reasons: [
          { code: "budget", message: "Over the per-probe budget." },
          { code: "quota", message: "Account quota is nearly used." },
        ],
      }),
    });
    expect(screen.getAllByTestId("probe-reasons")[0]?.querySelectorAll("li")).toHaveLength(2);
  });
});
