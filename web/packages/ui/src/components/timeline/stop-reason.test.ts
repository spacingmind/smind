import { describe, expect, it } from "vitest";

import { describeStopReason } from "@/components/timeline/stop-reason";

describe("describeStopReason", () => {
  it("labels capped and refused runs as warnings", () => {
    expect(describeStopReason("max_turn_requests")).toEqual({
      label: "Turn limit reached",
      tone: "warning",
      hint: "Send a message to continue",
    });
    expect(describeStopReason("max_tokens")?.tone).toBe("warning");
    expect(describeStopReason("refusal")?.label).toBe("Agent refused");
  });

  it("hides the normal end_turn and names a cancel", () => {
    expect(describeStopReason("end_turn")).toBeNull();
    expect(describeStopReason("cancelled")).toEqual({ label: "Stopped", tone: "neutral" });
  });

  it("shows other stop reasons verbatim", () => {
    expect(describeStopReason("whatever")).toEqual({ label: "whatever", tone: "neutral" });
  });
});
