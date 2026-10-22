import type { ProbeArmView, ProbeEstimate, ProbeMetricRow, ProbeView } from "@/lib/probe-types";

export function arm(over: Partial<ProbeArmView> = {}): ProbeArmView {
  return {
    role: "A",
    status: "done",
    runId: "run-a",
    startedAt: "2026-10-22T10:00:00Z",
    elapsedMs: 72_000,
    costUSD: 0.41,
    turns: 12,
    toolCalls: 20,
    failedToolCalls: 3,
    permissionDenials: 0,
    producedDiff: true,
    diffBytes: 640,
    ...over,
  };
}

export function estimate(over: Partial<ProbeEstimate> = {}): ProbeEstimate {
  return {
    eligible: true,
    reasons: [],
    quick: false,
    arms: 3,
    estUSD: 1.44,
    estArmUSD: 0.48,
    expectedMinutes: 8,
    perProbeCapUSD: 1.5,
    dailyCapUSD: 3,
    spentTodayUSD: 0.2,
    quotaPercent: 12,
    ...over,
  };
}

export function metric(over: Partial<ProbeMetricRow> = {}): ProbeMetricRow {
  return { key: "toolCalls", a: 20, a2: 21, b: 14, deltaAB: -6, noise: 1, worse: false, ...over };
}

/** A finished three-arm probe where the skill loaded and the failure did not recur. */
export function probeView(over: Partial<ProbeView> = {}): ProbeView {
  return {
    id: 5,
    proposalId: 9,
    workspaceId: 1,
    quick: false,
    status: "done",
    verdict: "helped",
    verdictReason: "The skill loaded and the targeted failure did not recur.",
    estUSD: 1.44,
    costUSD: 1.21,
    startedAt: "2026-10-22T10:00:00Z",
    finishedAt: "2026-10-22T10:07:00Z",
    elapsedMs: 420_000,
    arms: [
      arm({ role: "A", runId: "run-a", failureRecurred: true }),
      arm({ role: "A2", runId: "run-a2", failureRecurred: true, elapsedMs: 75_000, costUSD: 0.4 }),
      arm({ role: "B", runId: "run-b", loadedSkill: true, failureRecurred: false, elapsedMs: 64_000, costUSD: 0.4, diff: "diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-old\n+new\n", finalMessage: "Done." }),
    ],
    contrast: {
      loaded: true,
      noiseFloor: true,
      metrics: [
        metric(),
        metric({ key: "failedToolCalls", a: 3, a2: 3, b: 0, deltaAB: -3, noise: 0 }),
        metric({ key: "costUSD", a: 0.41, a2: 0.4, b: 0.4, deltaAB: -0.01, noise: 0.01 }),
        metric({ key: "durationMs", a: 72_000, a2: 75_000, b: 64_000, deltaAB: -8_000, noise: 3_000 }),
      ],
    },
    ...over,
  };
}

/** A running probe: control done, replicate running, skill arm pending. */
export function runningProbe(over: Partial<ProbeView> = {}): ProbeView {
  return probeView({
    status: "running",
    verdict: null,
    verdictReason: "",
    finishedAt: undefined,
    contrast: undefined,
    costUSD: 0.62,
    elapsedMs: 130_000,
    arms: [arm({ role: "A" }), arm({ role: "A2", status: "running", runId: "run-a2", elapsedMs: 58_000, costUSD: 0.21 }), arm({ role: "B", status: "pending", runId: undefined, elapsedMs: 0, costUSD: 0, turns: 0, toolCalls: 0, failedToolCalls: 0, producedDiff: false, diffBytes: 0 })],
    ...over,
  });
}
