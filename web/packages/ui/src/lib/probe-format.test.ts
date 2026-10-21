import { describe, expect, it } from "vitest";

import {
  armStatusLabel,
  formatBytes,
  formatCostUSD,
  formatDelta,
  formatDurationMs,
  formatEstimateUSD,
  formatMetric,
  formatSpendLine,
  isProbeActive,
  VERDICT_LABELS,
  VERDICT_TONES,
} from "@/lib/probe-format";

describe("probe formatting", () => {
  it("formats durations", () => {
    expect(formatDurationMs(0)).toBe("0s");
    expect(formatDurationMs(400)).toBe("<1s");
    expect(formatDurationMs(58_000)).toBe("58s");
    expect(formatDurationMs(72_000)).toBe("1m 12s");
    expect(formatDurationMs(3_720_000)).toBe("1h 02m");
    expect(formatDurationMs(Number.NaN)).toBe("0s");
  });

  it("formats cost with four decimals under 1 and two above", () => {
    expect(formatCostUSD(0.41)).toBe("$0.4100");
    expect(formatCostUSD(0)).toBe("$0.0000");
    expect(formatCostUSD(1.214)).toBe("$1.21");
    expect(formatCostUSD(-1)).toBe("$0.0000");
  });

  it("formats the estimate for the button", () => {
    expect(formatEstimateUSD(1.44)).toBe("$1.44");
    expect(formatEstimateUSD(0.96)).toBe("$0.96");
    expect(formatEstimateUSD(0.0013)).toBe("$0.0013");
    expect(formatEstimateUSD(0)).toBe("$0.00");
  });

  it("formats bytes and metrics in their units", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(640)).toBe("640 B");
    expect(formatBytes(2048)).toBe("2.0 KB");
    expect(formatMetric("toolCalls", 20)).toBe("20");
    expect(formatMetric("durationMs", 72_000)).toBe("1m 12s");
    expect(formatMetric("costUSD", 0.4)).toBe("$0.4000");
  });

  it("signs deltas and leaves zero unsigned", () => {
    expect(formatDelta("toolCalls", -6)).toBe("−6");
    expect(formatDelta("costUSD", 0.01)).toBe("+$0.0100");
    expect(formatDelta("turns", 0)).toBe("0");
  });

  it("names the seven verdicts and never gives one a success tone", () => {
    expect(Object.values(VERDICT_LABELS)).toEqual([
      "Helped (consistent)",
      "No observed effect",
      "Never loaded",
      "Hurt / regressed",
      "Inconclusive",
      "Probe failed (infra)",
      "Unsafe (tripwire)",
    ]);
    expect(Object.values(VERDICT_TONES)).not.toContain("success");
    expect(VERDICT_TONES.hurt).toBe("warning");
    expect(VERDICT_TONES.unsafe).toBe("danger");
  });

  it("labels arm statuses and knows active probes", () => {
    expect(armStatusLabel("pending")).toBe("Waiting");
    expect(isProbeActive("queued")).toBe(true);
    expect(isProbeActive("running")).toBe(true);
    expect(isProbeActive("done")).toBe(false);
    expect(isProbeActive(undefined)).toBe(false);
  });
});

describe("formatSpendLine", () => {
  it("one dollar format; under the estimate it shows the estimate in brackets", () => {
    expect(formatSpendLine(0.3, 0.36)).toEqual({ over: false, text: "$0.30 so far (estimate $0.36)" });
  });
  it("past the estimate it says so instead of showing an 'of about'", () => {
    expect(formatSpendLine(0.45, 0.36)).toEqual({ over: true, text: "$0.45 so far, over the $0.36 estimate" });
  });
  it("no estimate is never 'over'", () => {
    expect(formatSpendLine(0.45, 0).over).toBe(false);
  });
});
