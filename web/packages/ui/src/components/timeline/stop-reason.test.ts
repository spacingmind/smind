import { describe, expect, it } from "vitest";

import { describeStopReason } from "@/components/timeline/stop-reason";

describe("describeStopReason", () => {
  it("labels capped and refused runs as warnings", () => {
    expect(describeStopReason("max_turn_requests")).toEqual({
      label: "Turn limit reached",
      tone: "warning",
      hint: "Send a message to continue",
    });
    expect(describeStopReason("max_tokens").tone).toBe("warning");
    expect(describeStopReason("refusal").label).toBe("Agent refused");
  });

  it("shows other stop reasons verbatim", () => {
    for (const r of ["end_turn", "cancelled", "whatever"]) {
      expect(describeStopReason(r)).toEqual({ label: r, tone: "neutral" });
    }
  });
});
